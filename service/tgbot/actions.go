package tgbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"gorm.io/gorm"
)

// clientListLimit bounds how many buttons one listing renders. Telegram caps a
// keyboard's size, and a panel with hundreds of clients would blow past it.
// Whatever is dropped is always reported -- a silently truncated list reads as
// if it were the whole thing.
const clientListLimit = 20

func onCallback(ctx context.Context, b *bot.Bot, q *models.CallbackQuery) {
	chatID := callbackChat(q)
	r, boundClient := roleOf(chatID)
	if chatID == 0 || r == roleNone {
		logger.Warning("tgbot: ignoring a callback from unauthorised chat ", chatID)
		return
	}
	// Answered before the action runs, not after it: an unanswered callback
	// leaves a spinner on the button until Telegram times it out, and the
	// actions worth pressing are the slow ones -- a database upload, a QR
	// render, a core restart. Deferring this until they finished meant the
	// answer arrived after the spinner had already timed out, against a
	// callback id Telegram had expired. Failing to answer is cosmetic, so it
	// must not stop the action either.
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}); err != nil {
		logger.Debug("tgbot: answering the callback failed: ", err)
	}

	data := q.Data
	if r == roleClient {
		onClientCallback(ctx, b, chatID, boundClient, data)
		return
	}

	switch {
	case strings.HasPrefix(data, staticPrefix):
		onStaticCallback(ctx, b, chatID, strings.TrimPrefix(data, staticPrefix))
	case strings.HasPrefix(data, payloadPrefix):
		payload, ok := payloads.get(strings.TrimPrefix(data, payloadPrefix))
		if !ok {
			// The store is in memory, so a panel restart invalidates every
			// button already on someone's screen. Say so rather than doing
			// nothing, which is indistinguishable from a broken bot.
			reply(ctx, b, chatID, t("expired", nil), mainMenu())
			return
		}
		onPayloadCallback(ctx, b, chatID, payload)
	}
}

// onClientCallback handles the buttons an end user is sent.
//
// None of them carries a client name: every one acts on the client roleOf
// resolved from the chat. That is the same rule the message path follows -- with
// no name in the input there is nothing to point at somebody else's account --
// and it is why the link buttons here carry only an index.
func onClientCallback(ctx context.Context, b *bot.Bot, chatID int64, name, data string) {
	switch {
	case data == staticPrefix+"self":
		sendSelfUsage(ctx, b, chatID, name)
	case data == staticPrefix+"self.links":
		sendClientLinks(ctx, b, chatID, name, true)
	case strings.HasPrefix(data, payloadPrefix):
		payload, ok := payloads.get(strings.TrimPrefix(data, payloadPrefix))
		if !ok {
			reply(ctx, b, chatID, t("expired", nil), nil)
			return
		}
		verb, arg, _ := strings.Cut(payload, "|")
		if verb != "selflink" {
			return
		}
		index, err := strconv.Atoi(arg)
		if err != nil {
			return
		}
		sendClientLink(ctx, b, chatID, name, index, true)
	}
}

func onStaticCallback(ctx context.Context, b *bot.Bot, chatID int64, action string) {
	switch action {
	case "status":
		reply(ctx, b, chatID, service.StatusDigest(botLang()), mainMenu())
	case "nodes":
		reply(ctx, b, chatID, nodesText(), mainMenu())
	case "clients":
		sendClientList(ctx, b, chatID, "", 0)
	case "client.search":
		forms.set(chatID, stepClientSearch, clientDraft{})
		reply(ctx, b, chatID, t("client.searchPrompt", nil), nil)
	case "online":
		reply(ctx, b, chatID, onlineText(), mainMenu())
	case "inbounds":
		reply(ctx, b, chatID, inboundsText(), mainMenu())
	case "traffic":
		reply(ctx, b, chatID, trafficText(), mainMenu())
	case "bans":
		reply(ctx, b, chatID, bansText(), bansMenu())
	case "bans.confirm":
		reply(ctx, b, chatID, t("bans.confirmClear", nil),
			confirmKeyboard(staticPrefix+"bans.clear"))
	case "bans.clear":
		var guard service.LoginGuardService
		if err := guard.ClearAll(); err != nil {
			reply(ctx, b, chatID, t("err.save", p("detail", err.Error())), mainMenu())
			return
		}
		reply(ctx, b, chatID, t("bans.cleared", nil), mainMenu())
	case "backup":
		sendBackup(ctx, b, chatID)
	case "core.confirm":
		reply(ctx, b, chatID, t("core.confirm", nil),
			confirmKeyboard(staticPrefix+"core.restart"))
	case "core.restart":
		var configService service.ConfigService
		if err := configService.RestartCore(); err != nil {
			reply(ctx, b, chatID, t("core.failed", p("detail", err.Error())), mainMenu())
			return
		}
		reply(ctx, b, chatID, t("core.restarted", nil), mainMenu())
	case "client.new":
		forms.set(chatID, stepClientName, clientDraft{})
		reply(ctx, b, chatID, t("form.name", nil), nil)
	case "cancel":
		// A bind prompt leaves a reply keyboard behind, and an inline markup
		// does not dismiss one -- so cancelling that particular flow has to
		// answer with a removal rather than with the menu, or the contact
		// picker stays under the input box after the question was withdrawn.
		// The card that raised the prompt still carries its own buttons, so
		// nothing is stranded. Every other flow keeps the menu.
		var markup models.ReplyMarkup = mainMenu()
		if form, live := forms.get(chatID); live && form.Step == stepBindTgId {
			markup = &models.ReplyKeyboardRemove{RemoveKeyboard: true}
		}
		forms.clear(chatID)
		reply(ctx, b, chatID, t("cancelled", nil), markup)
	}
}

// onPayloadCallback handles the actions that carry an argument. The payload is
// verb|argument; a trailing ! on the verb means the operator has confirmed.
func onPayloadCallback(ctx context.Context, b *bot.Bot, chatID int64, payload string) {
	verb, arg, _ := strings.Cut(payload, "|")
	switch verb {
	case "client":
		sendClientCard(ctx, b, chatID, arg)

	case "clients":
		// offset|query, and the query may itself contain a separator.
		offsetText, query, _ := strings.Cut(arg, "|")
		offset, err := strconv.Atoi(offsetText)
		if err != nil {
			offset = 0
		}
		sendClientList(ctx, b, chatID, query, offset)

	case "toggle":
		c, err := findClient(arg)
		if err != nil {
			reply(ctx, b, chatID, err.Error(), mainMenu())
			return
		}
		key := "client.confirmToggleOff"
		if !c.Enable {
			key = "client.confirmToggleOn"
		}
		reply(ctx, b, chatID, t(key, p("name", c.Name)),
			confirmKeyboard(payloadPrefix+payloads.put("toggle!|"+arg)))
	case "toggle!":
		applyClient(ctx, b, chatID, arg, func(c *model.Client) (string, map[string]string) {
			c.Enable = !c.Enable
			if c.Enable {
				return "client.doneEnabled", nil
			}
			return "client.doneDisabled", nil
		})

	case "reset":
		reply(ctx, b, chatID, t("client.confirmReset", p("name", arg)),
			confirmKeyboard(payloadPrefix+payloads.put("reset!|"+arg)))
	case "reset!":
		applyClient(ctx, b, chatID, arg, func(c *model.Client) (string, map[string]string) {
			c.Up, c.Down = 0, 0
			return "client.doneReset", nil
		})

	case "links":
		sendClientLinks(ctx, b, chatID, arg, false)
	case "link":
		// index|name, and the name goes last because it is the part that may
		// itself contain the separator.
		indexText, name, _ := strings.Cut(arg, "|")
		index, err := strconv.Atoi(indexText)
		if err != nil {
			reply(ctx, b, chatID, t("expired", nil), mainMenu())
			return
		}
		sendClientLink(ctx, b, chatID, name, index, false)

	case "bind":
		c, err := findClient(arg)
		if err != nil {
			reply(ctx, b, chatID, err.Error(), mainMenu())
			return
		}
		// Stored on the draft's Name because that is the field a typed answer
		// resolves against; nothing else about the draft is used here. The
		// picker answer carries its own client id and does not need it.
		forms.set(chatID, stepBindTgId, clientDraft{Name: c.Name})
		sendBindPrompt(ctx, b, chatID, *c)

	case "client.create!":
		createClient(ctx, b, chatID)
	}
}

func confirmKeyboard(confirmData string) models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: t("btn.confirm", nil), CallbackData: confirmData},
			{Text: t("btn.cancel", nil), CallbackData: staticPrefix + "cancel"},
		}},
	}
}

// callbackChat prefers the chat the message was posted in; a callback whose
// message is inaccessible (too old for Telegram to include) still carries the
// user, and for a private bot the two are the same.
func callbackChat(q *models.CallbackQuery) int64 {
	if q.Message.Message != nil {
		return q.Message.Message.Chat.ID
	}
	return q.From.ID
}

func findClient(name string) (*model.Client, error) {
	var c model.Client
	if err := database.GetDB().Model(model.Client{}).Where("name = ?", name).First(&c).Error; err != nil {
		return nil, errors.New(t("client.notFound", p("name", name)))
	}
	return &c, nil
}

// applyClient mutates one client and saves it.
//
// The write goes through ConfigService.Save, the same entry point the HTTP API
// uses. Writing the row directly would skip the Changes audit entry, skip the
// LastUpdate bump that pushes the new state to every open panel over the
// websocket, and skip the inbound reload -- leaving the panel showing one thing
// while the core enforces another.
func applyClient(ctx context.Context, b *bot.Bot, chatID int64, name string, mutate func(*model.Client) (string, map[string]string)) {
	applyClientWith(ctx, b, chatID, name, mainMenu(), mutate)
}

// applyClientWith is applyClient with the keyboard the outcome carries.
//
// The binding flow needs its own: it puts a reply keyboard up to offer the
// contact picker, and a reply keyboard is not dismissed by an inline one -- so
// without a ReplyKeyboardRemove on the way out, a "choose from contacts" button
// sits under the input box long after the binding is done.
func applyClientWith(ctx context.Context, b *bot.Bot, chatID int64, name string, markup models.ReplyMarkup, mutate func(*model.Client) (string, map[string]string)) {
	c, err := findClient(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), markup)
		return
	}
	doneKey, extra := mutate(c)

	data, err := json.Marshal(c)
	if err != nil {
		reply(ctx, b, chatID, t("err.save", p("detail", err.Error())), markup)
		return
	}
	if err := save(chatID, "clients", "edit", data); err != nil {
		reply(ctx, b, chatID, t("err.save", p("detail", err.Error())), markup)
		return
	}
	params := p("name", c.Name)
	for k, v := range extra {
		params[k] = v
	}
	reply(ctx, b, chatID, t(doneKey, params), markup)
}

func save(chatID int64, obj, act string, data json.RawMessage) error {
	var configService service.ConfigService
	var nodeSync service.NodeSyncService

	actor := "tgbot:" + strconv.FormatInt(chatID, 10)
	if _, err := configService.Save(obj, act, data, "", actor, botHostname()); err != nil {
		return err
	}
	// The same fan-out the v1 API performs, for the same reason: without it a
	// client change reaches the nodes only at the next hourly reconcile.
	if obj == "clients" || obj == "inbounds" {
		nodeSync.MarkAllDirty()
		go nodeSync.ReconcileDirtyOnline()
	}
	return nil
}

// botHostname is what generated share links are built against.
//
// There is no HTTP request here to take a Host header from, so the configured
// web domain is the only real answer. Falling back to the machine name keeps
// links from being built against an empty string, but such a link is only
// useful on a LAN -- a panel handing out client links should have its domain
// set regardless.
func botHostname() string {
	var settingService service.SettingService
	if domain, err := settingService.GetWebDomain(); err == nil && domain != "" {
		return domain
	}
	return notify.Host()
}

// sendClientList shows the clients closest to needing attention, which is what
// an operator reaching for this on their phone is nearly always after.
// sendClientList renders one page of clients, optionally narrowed by a search
// term. Past a couple of hundred clients the rest were simply unreachable from
// the bot: the first page was all anyone could see.
func sendClientList(ctx context.Context, b *bot.Bot, chatID int64, query string, offset int) {
	if offset < 0 {
		offset = 0
	}
	// Rebuilt per use rather than shared: a GORM chain that has already run a
	// Count carries state into the next call on it.
	matching := func() *gorm.DB {
		q := database.GetDB().Model(model.Client{})
		if query != "" {
			// LIKE reads % and _ as wildcards, so an unescaped search for
			// "user_01" would also match "userX01" and read as a broken search.
			like := "%" + likeEscape(query) + "%"
			q = q.Where(
				"name LIKE ? ESCAPE '\\' OR remark LIKE ? ESCAPE '\\' OR `group` LIKE ? ESCAPE '\\'",
				like, like, like)
		}
		return q
	}

	var total int64
	if err := matching().Count(&total).Error; err != nil {
		reply(ctx, b, chatID, t("err.read", p("detail", err.Error())), mainMenu())
		return
	}
	if total == 0 {
		key := "client.listEmpty"
		if query != "" {
			key = "client.searchEmpty"
		}
		reply(ctx, b, chatID, t(key, p("query", query)), mainMenu())
		return
	}
	if offset >= int(total) {
		offset = 0
	}

	var clients []model.Client
	// Disabled first, then soonest to expire. The CASE keeps unlimited clients
	// (expiry 0) at the end instead of sorting them to the front as the
	// smallest value. Name breaks the tie so paging is stable -- without a
	// total order two pages can repeat one row and skip another.
	err := matching().
		Order("enable ASC, CASE WHEN expiry > 0 THEN expiry ELSE 9223372036854775807 END ASC, name ASC").
		Offset(offset).Limit(clientListLimit).Find(&clients).Error
	if err != nil {
		reply(ctx, b, chatID, t("err.read", p("detail", err.Error())), mainMenu())
		return
	}

	var rows [][]models.InlineKeyboardButton
	var text strings.Builder
	if query != "" {
		text.WriteString(t("client.searchTitle", p("query", query)))
	} else {
		text.WriteString(t("client.listTitle", nil))
	}
	picks := make([]models.InlineKeyboardButton, 0, len(clients))
	for _, c := range clients {
		text.WriteString("\n" + clientLine(c))
		picks = append(picks, models.InlineKeyboardButton{
			Text:         c.Name,
			CallbackData: payloadPrefix + payloads.put("client|"+c.Name),
		})
	}
	rows = append(rows, buttonGrid(picks)...)

	end := offset + len(clients)
	if int64(end) < total || offset > 0 {
		text.WriteString("\n\n" + t("client.range", p(
			"from", strconv.Itoa(offset+1), "to", strconv.Itoa(end),
			"total", strconv.FormatInt(total, 10))))
	}

	var nav []models.InlineKeyboardButton
	if offset > 0 {
		nav = append(nav, models.InlineKeyboardButton{
			Text:         t("btn.prev", nil),
			CallbackData: payloadPrefix + payloads.put(clientPagePayload(query, offset-clientListLimit)),
		})
	}
	if int64(end) < total {
		nav = append(nav, models.InlineKeyboardButton{
			Text:         t("btn.next", nil),
			CallbackData: payloadPrefix + payloads.put(clientPagePayload(query, end)),
		})
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}

	rows = append(rows, []models.InlineKeyboardButton{
		{Text: t("btn.search", nil), CallbackData: staticPrefix + "client.search"},
		{Text: t("btn.menu", nil), CallbackData: staticPrefix + "status"},
	})
	reply(ctx, b, chatID, text.String(), &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

// buttonGrid packs picker buttons into rows.
//
// Three across while the list is short, two once it is long enough that names
// start crowding each other -- the same rule 3x-ui uses, and for the same
// reason: a full page of one-button rows is a screen of scrolling to reach the
// last name, and client names are short enough to sit two or three abreast.
func buttonGrid(buttons []models.InlineKeyboardButton) [][]models.InlineKeyboardButton {
	cols := 3
	if len(buttons) >= 6 {
		cols = 2
	}
	rows := make([][]models.InlineKeyboardButton, 0, (len(buttons)+cols-1)/cols)
	for i := 0; i < len(buttons); i += cols {
		end := i + cols
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, buttons[i:end])
	}
	return rows
}

// clientPagePayload encodes one page button. The search term goes last because
// it is the only part that may contain the separator.
func clientPagePayload(query string, offset int) string {
	return "clients|" + strconv.Itoa(offset) + "|" + query
}

// likeEscape neutralises the LIKE wildcards in a search term the operator
// typed. The backslash goes first, or escaping the wildcards re-introduces it.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func sendClientCard(ctx context.Context, b *bot.Bot, chatID int64, name string) {
	c, err := findClient(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), mainMenu())
		return
	}
	toggle := t("btn.disable", nil)
	if !c.Enable {
		toggle = t("btn.enable", nil)
	}
	markup := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: toggle, CallbackData: payloadPrefix + payloads.put("toggle|"+c.Name)},
			{Text: t("btn.reset", nil), CallbackData: payloadPrefix + payloads.put("reset|"+c.Name)},
		},
		{
			{Text: t("btn.links", nil), CallbackData: payloadPrefix + payloads.put("links|"+c.Name)},
			{Text: bindLabel(c.TgId), CallbackData: payloadPrefix + payloads.put("bind|"+c.Name)},
		},
		{{Text: t("btn.back", nil), CallbackData: staticPrefix + "clients"}},
	}}
	reply(ctx, b, chatID, clientDetail(*c), markup)
}

func clientLine(c model.Client) string {
	state := t("client.on", nil)
	if !c.Enable {
		state = t("client.off", nil)
	}
	quota := t("client.unlimited", nil)
	if c.Volume > 0 {
		quota = humanBytes(c.Up+c.Down) + "/" + humanBytes(c.Volume)
	}
	return c.Name + " [" + state + "] " + quota
}

func clientDetail(c model.Client) string {
	var b strings.Builder
	b.WriteString(clientLine(c))
	if c.Expiry > 0 {
		b.WriteString("\n" + t("client.expires", p("when", timeText(c.Expiry))))
	} else {
		b.WriteString("\n" + t("client.never", nil))
	}
	b.WriteString("\n" + t("client.upDown", p("up", humanBytes(c.Up), "down", humanBytes(c.Down))))
	if c.Group != "" {
		b.WriteString("\n" + t("client.group", p("group", c.Group)))
	}
	if c.TgId != 0 {
		b.WriteString("\n" + t("client.telegram", p("id", strconv.FormatInt(c.TgId, 10))))
	}
	if c.Desc != "" {
		b.WriteString("\n" + t("client.desc", p("desc", c.Desc)))
	}
	return b.String()
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTP"[exp])
}

func timeText(unix int64) string {
	return time.Unix(unix, 0).Format("2006-01-02 15:04")
}

func bindLabel(tgID int64) string {
	if tgID == 0 {
		return t("btn.bind", nil)
	}
	return t("btn.rebind", nil)
}
