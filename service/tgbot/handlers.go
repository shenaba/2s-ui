package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// publicCommands is what the in-app command menu offers everybody, and it is
// deliberately the harmless four: a stranger reading /nodes and /backup off the
// menu learns more about what is behind this bot than any of its answers give
// away. The descriptions come from the message table at call time, because the
// language is a setting.
var publicCommands = []string{"start", "help", "status", "id"}

// adminCommands is the full menu, published per admin chat rather than
// globally. Everything here works from any admin chat regardless of what the
// menu lists -- the menu is a convenience, not the permission check.
var adminCommands = []string{
	"start", "status", "nodes", "clients", "online", "traffic", "inbounds", "bans", "backup", "id",
}

// callback_data prefixes. Telegram caps that field at 64 bytes, so anything
// carrying an argument stores it in payloads and sends the key instead.
const (
	staticPrefix  = "s:"
	payloadPrefix = "p:"
)

// dispatch is the single entry point the SDK calls for every update.
func dispatch(ctx context.Context, b *bot.Bot, update *models.Update) {
	switch {
	case update.CallbackQuery != nil:
		onCallback(ctx, b, update.CallbackQuery)
	case update.Message != nil:
		onMessage(ctx, b, update.Message)
	}
}

func onMessage(ctx context.Context, b *bot.Bot, msg *models.Message) {
	chatID := msg.Chat.ID
	r, boundClient := roleOf(chatID)
	text := strings.TrimSpace(msg.Text)

	// A form in progress consumes plain text; a command cancels it, so a stuck
	// conversation is always escapable by typing /start. Only admins ever have
	// one -- nothing an end user can reach asks a question.
	if r == roleAdmin {
		if !strings.HasPrefix(text, "/") {
			if form, ok := forms.get(chatID); ok {
				onFormInput(ctx, b, chatID, form, text)
				return
			}
		}
		forms.clear(chatID)
	}

	cmd, arg := parseCommand(text)

	// Answered for everybody, before the role check. Binding a client needs
	// their numeric id, and the person who has it is the one who cannot read
	// it: sending them to a third-party bot to find out is the alternative.
	//
	// From, not Chat: in a group the chat id belongs to the group, while the
	// id worth reporting is the one a private message would arrive under --
	// which is what roleOf matches against.
	if cmd == "id" {
		reply(ctx, b, chatID, t("id.reply", p("id", strconv.FormatInt(senderID(msg), 10))), nil)
		return
	}

	switch r {
	case roleClient:
		// End users get one thing regardless of what they typed: their own
		// usage. No command here takes a name, so there is no argument to aim
		// at somebody else's account.
		onClientMessage(ctx, b, chatID, boundClient)
		return
	case roleNone:
		onStrangerMessage(ctx, b, chatID, cmd)
		return
	}

	switch cmd {
	case "start", "help", "menu":
		reply(ctx, b, chatID, "2S-UI\n"+service.StatusDigest(botLang()), mainMenu())
	case "status":
		reply(ctx, b, chatID, service.StatusDigest(botLang()), mainMenu())
	case "nodes":
		reply(ctx, b, chatID, nodesText(), mainMenu())
	case "clients":
		sendClientList(ctx, b, chatID, arg, 0)
	case "online":
		reply(ctx, b, chatID, onlineText(), mainMenu())
	case "inbounds":
		reply(ctx, b, chatID, inboundsText(), mainMenu())
	case "inbound":
		if arg == "" {
			reply(ctx, b, chatID, inboundsText(), mainMenu())
			return
		}
		reply(ctx, b, chatID, inboundText(arg), mainMenu())
	case "traffic":
		reply(ctx, b, chatID, trafficText(), mainMenu())
	case "bans":
		reply(ctx, b, chatID, bansText(), bansMenu())
	case "backup":
		sendBackup(ctx, b, chatID)
	default:
		reply(ctx, b, chatID, t("unknownCmd", nil), mainMenu())
	}
}

// onStrangerMessage answers a chat the panel does not know.
//
// It answers rather than staying silent, but with nothing that says which panel
// this is: no product name, no hostname, no figures, and no keyboard naming the
// management commands. All a stranger learns here is that some bot is on the
// other end, which they already knew from being able to open the chat.
func onStrangerMessage(ctx context.Context, b *bot.Bot, chatID int64, cmd string) {
	// No keyboard: a menu of buttons is itself a list of what this bot can do.
	reply(ctx, b, chatID, strangerReply(cmd), nil)
}

// strangerReply is the whole of what an unknown chat can get out of the bot.
//
// Split out as a pure function so the rule can be pinned by a test rather than
// only by reading: every string it can return has to be one that says nothing
// about which panel is behind the bot.
func strangerReply(cmd string) string {
	switch cmd {
	case "start", "help", "menu":
		return t("greet", nil)
	case "status":
		return t("alive", nil)
	default:
		return t("unknownCmd", nil)
	}
}

// parseCommand splits "/clients bob" into its verb and the rest. Text that is
// not a command yields an empty verb, which falls through to the same "unknown"
// answer a mistyped command gets.
func parseCommand(text string) (cmd, arg string) {
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	cmd, arg, _ = strings.Cut(strings.TrimPrefix(text, "/"), " ")
	// Group chats address commands as /status@thebot.
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd, strings.TrimSpace(arg)
}

// senderID is who sent the message, falling back to the chat for the updates
// that carry no sender (a channel post, an anonymous group admin).
func senderID(msg *models.Message) int64 {
	if msg.From != nil {
		return msg.From.ID
	}
	return msg.Chat.ID
}

func mainMenu() models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: t("btn.status", nil), CallbackData: staticPrefix + "status"},
				{Text: t("btn.nodes", nil), CallbackData: staticPrefix + "nodes"},
			},
			{
				{Text: t("btn.clients", nil), CallbackData: staticPrefix + "clients"},
				{Text: t("btn.online", nil), CallbackData: staticPrefix + "online"},
			},
			{
				{Text: t("btn.inbounds", nil), CallbackData: staticPrefix + "inbounds"},
				{Text: t("btn.traffic", nil), CallbackData: staticPrefix + "traffic"},
			},
			{
				{Text: t("btn.bans", nil), CallbackData: staticPrefix + "bans"},
				{Text: t("btn.backup", nil), CallbackData: staticPrefix + "backup"},
			},
			{
				{Text: t("btn.restart", nil), CallbackData: staticPrefix + "core.confirm"},
				{Text: t("btn.new", nil), CallbackData: staticPrefix + "client.new"},
			},
		},
	}
}

// bansMenu adds the one action that belongs only on the ban list. Clearing is
// offered here rather than only in the CLI because the operator locked out by
// their own limiter cannot reach the panel to do it -- the bot is what is still
// answering. See LoginGuardService.ClearAll on why that does not weaken it.
func bansMenu() models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: t("btn.bansClear", nil), CallbackData: staticPrefix + "bans.confirm"}},
			{{Text: t("btn.menu", nil), CallbackData: staticPrefix + "status"}},
		},
	}
}

// reply sends text, splitting it the same way alerts are split and attaching
// the keyboard only to the final page.
func reply(ctx context.Context, b *bot.Bot, chatID int64, text string, markup models.ReplyMarkup) {
	pages := notify.Paginate(text, 2000)
	for i, page := range pages {
		params := &bot.SendMessageParams{ChatID: chatID, Text: page}
		if i == len(pages)-1 {
			params.ReplyMarkup = markup
		}
		if _, err := b.SendMessage(ctx, params); err != nil {
			logger.Warning("tgbot: send failed: ", err)
			return
		}
	}
}

func nodesText() string {
	var nodeService service.NodeService
	statuses := nodeService.GetStatuses()
	if len(statuses) == 0 {
		return t("nodes.none", nil)
	}
	var nodes []model.Node
	if err := database.GetDB().Model(model.Node{}).Find(&nodes).Error; err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	var b strings.Builder
	b.WriteString(t("nodes.title", p("count", strconv.Itoa(len(nodes)))))
	for _, n := range nodes {
		s, known := statuses[n.Id]
		state := t("nodes.disabled", nil)
		switch {
		case !n.Enable:
		case !known:
			state = t("nodes.unknown", nil)
		default:
			state = nodeStateText(s.State)
		}
		b.WriteString("\n" + n.Name + " — " + state)
		if s.State == "online" && s.Latency > 0 {
			b.WriteString(fmt.Sprintf(" (%d ms)", s.Latency))
		}
		if s.Error != "" {
			b.WriteString("\n  " + s.Error)
		}
	}
	return b.String()
}

func onlineText() string {
	var statsService service.StatsService
	o, err := statsService.GetOnlines()
	if err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	if len(o.User) == 0 {
		return t("online.none", nil)
	}
	return t("online.title", p("count", strconv.Itoa(len(o.User)))) + "\n" + strings.Join(o.User, "\n")
}

func sendBackup(ctx context.Context, b *bot.Bot, chatID int64) {
	data, err := database.GetDb("")
	if err != nil {
		reply(ctx, b, chatID, t("backup.failed", p("detail", err.Error())), mainMenu())
		return
	}
	name := "2s-ui-" + notify.Host() + ".db"
	if _, err := b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: name, Data: strings.NewReader(string(data))},
	}); err != nil {
		reply(ctx, b, chatID, t("backup.failed", p("detail", err.Error())), mainMenu())
	}
}
