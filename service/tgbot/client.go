package tgbot

import (
	"context"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// onClientMessage handles an end user asking about their own client.
//
// The whole surface is read-only and scoped to the one client bound to that
// Telegram id: whatever they type, they get their own usage. Nothing here takes
// a client name as input, so there is no argument to point at somebody else.
func onClientMessage(ctx context.Context, b *bot.Bot, chatID int64, name string) {
	sendSelfUsage(ctx, b, chatID, name)
}

func sendSelfUsage(ctx context.Context, b *bot.Bot, chatID int64, name string) {
	c, err := findClient(name)
	if err != nil {
		reply(ctx, b, chatID, t("self.unavailable", nil), nil)
		return
	}
	markup := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: t("btn.refresh", nil), CallbackData: staticPrefix + "self"},
	}}}
	reply(ctx, b, chatID, selfUsageText(*c), markup)
}

// selfUsageText is deliberately not clientDetail: an end user gets their own
// figures and their subscription link, not the operator's view. Group and
// description are internal notes and stay out.
func selfUsageText(c model.Client) string {
	var b strings.Builder
	b.WriteString(c.Name)

	used := c.Up + c.Down
	switch {
	case c.Volume <= 0:
		b.WriteString("\n" + t("self.unlimited", p("used", humanBytes(used))))
	case c.Volume-used > 0:
		b.WriteString("\n" + t("self.left", p(
			"used", humanBytes(used), "total", humanBytes(c.Volume),
			"left", humanBytes(c.Volume-used))))
	default:
		b.WriteString("\n" + t("self.exhausted", p(
			"used", humanBytes(used), "total", humanBytes(c.Volume))))
	}

	if c.Expiry > 0 {
		b.WriteString("\n" + t("client.expires", p("when", timeText(c.Expiry))))
	} else {
		b.WriteString("\n" + t("client.never", nil))
	}
	if !c.Enable {
		b.WriteString("\n" + t("self.disabled", nil))
	}

	// The subscription id is the client name (sub/subService.go), so the link
	// is the configured subscription URI with the name appended.
	var settingService service.SettingService
	if uri, err := settingService.GetFinalSubURI(botHostname()); err == nil && uri != "" {
		b.WriteString("\n\n" + t("self.sub", nil) + "\n" + uri + c.Name)
	}
	return b.String()
}

// bindClient attaches a Telegram id to a client, or clears it when id is 0.
//
// Admin-only, and it goes through the same save path as every other write so
// the change is audited like one.
func bindClient(ctx context.Context, b *bot.Bot, chatID int64, name string, tgID int64) {
	applyClient(ctx, b, chatID, name, func(c *model.Client) (string, map[string]string) {
		c.TgId = tgID
		if tgID == 0 {
			return "bind.removed", nil
		}
		return "bind.done", p("id", strconv.FormatInt(tgID, 10))
	})
}
