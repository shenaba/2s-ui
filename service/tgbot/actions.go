package tgbot

import (
	"context"
	"encoding/json"
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
)

// clientListLimit bounds how many buttons one listing renders. Telegram caps a
// keyboard's size, and a panel with hundreds of clients would blow past it.
// Whatever is dropped is always reported -- a silently truncated list reads as
// if it were the whole thing.
const clientListLimit = 20

func onCallback(ctx context.Context, b *bot.Bot, q *models.CallbackQuery) {
	chatID := callbackChat(q)
	if chatID == 0 || roleOf(chatID) != roleAdmin {
		logger.Warning("tgbot: ignoring a callback from unauthorised chat ", chatID)
		return
	}
	// Always answer, even when the action fails: an unanswered callback leaves
	// a spinner on the button until Telegram times it out.
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}); err != nil {
			logger.Debug("tgbot: answering the callback failed: ", err)
		}
	}()

	data := q.Data
	switch {
	case strings.HasPrefix(data, staticPrefix):
		onStaticCallback(ctx, b, chatID, strings.TrimPrefix(data, staticPrefix))
	case strings.HasPrefix(data, payloadPrefix):
		payload, ok := payloads.get(strings.TrimPrefix(data, payloadPrefix))
		if !ok {
			// The store is in memory, so a panel restart invalidates every
			// button already on someone's screen. Say so rather than doing
			// nothing, which is indistinguishable from a broken bot.
			reply(ctx, b, chatID, "That button has expired. Open the menu again with /start.", mainMenu())
			return
		}
		onPayloadCallback(ctx, b, chatID, payload)
	}
}

func onStaticCallback(ctx context.Context, b *bot.Bot, chatID int64, action string) {
	switch action {
	case "status":
		reply(ctx, b, chatID, service.StatusDigest(), mainMenu())
	case "nodes":
		reply(ctx, b, chatID, nodesText(), mainMenu())
	case "clients":
		sendClientList(ctx, b, chatID)
	case "online":
		reply(ctx, b, chatID, onlineText(), mainMenu())
	case "backup":
		sendBackup(ctx, b, chatID)
	case "core.confirm":
		reply(ctx, b, chatID, "Restart the sing-box core? Connected clients will be dropped.",
			confirmKeyboard(staticPrefix+"core.restart"))
	case "core.restart":
		var configService service.ConfigService
		if err := configService.RestartCore(); err != nil {
			reply(ctx, b, chatID, "Core restart failed: "+err.Error(), mainMenu())
			return
		}
		reply(ctx, b, chatID, "Core restarted.", mainMenu())
	case "client.new":
		forms.set(chatID, stepClientName, clientDraft{})
		reply(ctx, b, chatID, "New client -- send a name.\nSend /start at any point to cancel.", nil)
	case "cancel":
		forms.clear(chatID)
		reply(ctx, b, chatID, "Cancelled.", mainMenu())
	}
}

// onPayloadCallback handles the actions that carry an argument. The payload is
// verb|argument; a trailing ! on the verb means the operator has confirmed.
func onPayloadCallback(ctx context.Context, b *bot.Bot, chatID int64, payload string) {
	verb, arg, _ := strings.Cut(payload, "|")
	switch verb {
	case "client":
		sendClientCard(ctx, b, chatID, arg)

	case "toggle":
		c, err := findClient(arg)
		if err != nil {
			reply(ctx, b, chatID, err.Error(), mainMenu())
			return
		}
		word := "Disable"
		if !c.Enable {
			word = "Enable"
		}
		reply(ctx, b, chatID, fmt.Sprintf("%s client %s?", word, c.Name),
			confirmKeyboard(payloadPrefix+payloads.put("toggle!|"+arg)))
	case "toggle!":
		applyClient(ctx, b, chatID, arg, func(c *model.Client) string {
			c.Enable = !c.Enable
			if c.Enable {
				return "enabled"
			}
			return "disabled"
		})

	case "reset":
		reply(ctx, b, chatID, "Reset the traffic counters for "+arg+"?",
			confirmKeyboard(payloadPrefix+payloads.put("reset!|"+arg)))
	case "reset!":
		applyClient(ctx, b, chatID, arg, func(c *model.Client) string {
			c.Up, c.Down = 0, 0
			return "traffic reset"
		})

	case "client.create!":
		createClient(ctx, b, chatID)
	}
}

func confirmKeyboard(confirmData string) models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "Confirm", CallbackData: confirmData},
			{Text: "Cancel", CallbackData: staticPrefix + "cancel"},
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
		return nil, fmt.Errorf("no client named %s", name)
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
func applyClient(ctx context.Context, b *bot.Bot, chatID int64, name string, mutate func(*model.Client) string) {
	c, err := findClient(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), mainMenu())
		return
	}
	what := mutate(c)

	data, err := json.Marshal(c)
	if err != nil {
		reply(ctx, b, chatID, "Could not encode the client: "+err.Error(), mainMenu())
		return
	}
	if err := save(chatID, "clients", "edit", data); err != nil {
		reply(ctx, b, chatID, "Save failed: "+err.Error(), mainMenu())
		return
	}
	reply(ctx, b, chatID, c.Name+": "+what+".", mainMenu())
}

// save is the bot's single write path. actor records which Telegram chat asked,
// so the panel's change log can attribute it.
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
func sendClientList(ctx context.Context, b *bot.Bot, chatID int64) {
	var clients []model.Client
	// Disabled first, then soonest to expire. The CASE keeps unlimited clients
	// (expiry 0) at the end instead of sorting them to the front as the
	// smallest value.
	err := database.GetDB().Model(model.Client{}).
		Order("enable ASC, CASE WHEN expiry > 0 THEN expiry ELSE 9223372036854775807 END ASC").
		Limit(clientListLimit + 1).Find(&clients).Error
	if err != nil {
		reply(ctx, b, chatID, "Could not read the client list: "+err.Error(), mainMenu())
		return
	}
	if len(clients) == 0 {
		reply(ctx, b, chatID, "No clients yet.", mainMenu())
		return
	}

	truncated := false
	if len(clients) > clientListLimit {
		clients = clients[:clientListLimit]
		truncated = true
	}

	var rows [][]models.InlineKeyboardButton
	var text strings.Builder
	text.WriteString("Clients")
	for _, c := range clients {
		text.WriteString("\n" + clientLine(c))
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         c.Name,
			CallbackData: payloadPrefix + payloads.put("client|"+c.Name),
		}})
	}
	if truncated {
		var total int64
		if err := database.GetDB().Model(model.Client{}).Count(&total).Error; err != nil {
			logger.Warning("tgbot: client count: ", err)
		}
		text.WriteString(fmt.Sprintf("\n\nShowing %d of %d -- open the panel for the rest.",
			clientListLimit, total))
	}
	rows = append(rows, []models.InlineKeyboardButton{{Text: "Menu", CallbackData: staticPrefix + "status"}})
	reply(ctx, b, chatID, text.String(), &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func sendClientCard(ctx context.Context, b *bot.Bot, chatID int64, name string) {
	c, err := findClient(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), mainMenu())
		return
	}
	toggle := "Disable"
	if !c.Enable {
		toggle = "Enable"
	}
	markup := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: toggle, CallbackData: payloadPrefix + payloads.put("toggle|"+c.Name)},
			{Text: "Reset traffic", CallbackData: payloadPrefix + payloads.put("reset|"+c.Name)},
		},
		{{Text: "Back", CallbackData: staticPrefix + "clients"}},
	}}
	reply(ctx, b, chatID, clientDetail(*c), markup)
}

func clientLine(c model.Client) string {
	state := "on"
	if !c.Enable {
		state = "OFF"
	}
	quota := "unlimited"
	if c.Volume > 0 {
		quota = humanBytes(c.Up+c.Down) + "/" + humanBytes(c.Volume)
	}
	return c.Name + " [" + state + "] " + quota
}

func clientDetail(c model.Client) string {
	var b strings.Builder
	b.WriteString(clientLine(c))
	if c.Expiry > 0 {
		b.WriteString("\nExpires: " + time.Unix(c.Expiry, 0).Format("2006-01-02 15:04"))
	} else {
		b.WriteString("\nExpires: never")
	}
	b.WriteString("\nUp " + humanBytes(c.Up) + " · Down " + humanBytes(c.Down))
	if c.Group != "" {
		b.WriteString("\nGroup: " + c.Group)
	}
	if c.Desc != "" {
		b.WriteString("\n" + c.Desc)
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
