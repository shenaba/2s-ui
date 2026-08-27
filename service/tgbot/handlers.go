package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// commands is what the in-app command menu offers. Descriptions are English
// for the same reason the digest labels are -- see service.StatusDigest.
var commands = []struct{ name, desc string }{
	{"start", "Show the menu"},
	{"status", "Panel and system status"},
	{"nodes", "Managed nodes"},
	{"clients", "Clients close to a limit"},
	{"online", "Clients online right now"},
	{"backup", "Send a database backup"},
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
	switch r {
	case roleClient:
		// End users get one thing regardless of what they typed: their own
		// usage. No command here takes a name, so there is no argument to aim
		// at somebody else's account.
		onClientMessage(ctx, b, chatID, boundClient)
		return
	case roleAdmin:
	default:
		// Unknown chats get no reply at all. Answering, even to refuse,
		// confirms that a panel bot is reachable at this token to anyone who
		// found it.
		logger.Warning("tgbot: ignoring a message from unauthorised chat ", chatID)
		return
	}

	text := strings.TrimSpace(msg.Text)

	// A form in progress consumes plain text; a command cancels it, so a stuck
	// conversation is always escapable by typing /start.
	if !strings.HasPrefix(text, "/") {
		if form, ok := forms.get(chatID); ok {
			onFormInput(ctx, b, chatID, form, text)
			return
		}
	}
	forms.clear(chatID)

	cmd := strings.TrimPrefix(strings.Fields(text + " ")[0], "/")
	// Group chats address commands as /status@thebot.
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}

	switch cmd {
	case "start", "help", "menu":
		reply(ctx, b, chatID, "2S-UI\n"+service.StatusDigest(), mainMenu())
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
	default:
		reply(ctx, b, chatID, "Unknown command.", mainMenu())
	}
}

func mainMenu() models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Status", CallbackData: staticPrefix + "status"},
				{Text: "Nodes", CallbackData: staticPrefix + "nodes"},
			},
			{
				{Text: "Clients", CallbackData: staticPrefix + "clients"},
				{Text: "Online", CallbackData: staticPrefix + "online"},
			},
			{
				{Text: "Backup", CallbackData: staticPrefix + "backup"},
				{Text: "Restart core", CallbackData: staticPrefix + "core.confirm"},
			},
			{
				{Text: "+ New client", CallbackData: staticPrefix + "client.new"},
			},
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
		return "No managed nodes."
	}
	var nodes []model.Node
	if err := database.GetDB().Model(model.Node{}).Find(&nodes).Error; err != nil {
		return "Could not read the node list: " + err.Error()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Nodes (%d)", len(nodes)))
	for _, n := range nodes {
		s, known := statuses[n.Id]
		state := "disabled"
		switch {
		case !n.Enable:
		case !known:
			state = "unknown"
		default:
			state = s.State
		}
		b.WriteString("\n" + n.Name + " — " + state)
		if state == "online" && s.Latency > 0 {
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
		return "Could not read the online list: " + err.Error()
	}
	if len(o.User) == 0 {
		return "No clients are online."
	}
	return fmt.Sprintf("Online clients (%d)\n%s", len(o.User), strings.Join(o.User, "\n"))
}

func sendBackup(ctx context.Context, b *bot.Bot, chatID int64) {
	data, err := database.GetDb("")
	if err != nil {
		reply(ctx, b, chatID, "Backup failed: "+err.Error(), mainMenu())
		return
	}
	name := "2s-ui-" + notify.Host() + ".db"
	if _, err := b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: name, Data: strings.NewReader(string(data))},
	}); err != nil {
		reply(ctx, b, chatID, "Backup upload failed: "+err.Error(), mainMenu())
	}
}
