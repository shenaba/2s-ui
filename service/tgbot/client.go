package tgbot

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/database"
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
		{Text: t("btn.links", nil), CallbackData: staticPrefix + "self.links"},
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

// sendBindPrompt offers both ways to name the person behind a client.
//
// The contact picker costs the operator least: Telegram hands the id straight
// back, so nobody has to talk the customer through running /id and reading a
// number out. It only reaches people the operator can already find in Telegram,
// which is why typing an id stays available underneath it.
//
// The client's own row id rides along as the picker's correlation id, so the
// answer identifies its client on its own. The form state is still set for the
// typed path, but the picked one does not depend on it surviving however long
// the operator spends browsing their contacts.
func sendBindPrompt(ctx context.Context, b *bot.Bot, chatID int64, c model.Client) {
	var markup models.ReplyMarkup
	if c.Id > 0 && uint64(c.Id) <= math.MaxInt32 {
		markup = &models.ReplyKeyboardMarkup{
			Keyboard: [][]models.KeyboardButton{{{
				Text: t("bind.pick", nil),
				RequestUsers: &models.KeyboardButtonRequestUsers{
					RequestID:   int32(c.Id),
					MaxQuantity: 1,
				},
			}}},
			ResizeKeyboard: true,
			// Telegram hides it again as soon as it is used, so there is no
			// second message here just to take the keyboard away.
			OneTimeKeyboard: true,
		}
	}
	reply(ctx, b, chatID, t("bind.prompt", p("name", c.Name)), markup)
}

// onUsersShared completes a binding the operator answered with the picker.
func onUsersShared(ctx context.Context, b *bot.Bot, chatID int64, shared *models.UsersShared) {
	forms.clear(chatID)
	if len(shared.Users) == 0 {
		reply(ctx, b, chatID, t("bind.pickEmpty", nil), mainMenu())
		return
	}
	var c model.Client
	err := database.GetDB().Model(model.Client{}).
		Where("id = ?", shared.RequestID).First(&c).Error
	if err != nil {
		// The client was deleted while the picker was open, or the reply came
		// from some other request this bot never made.
		reply(ctx, b, chatID, t("bind.pickStale", nil), mainMenu())
		return
	}
	bindClient(ctx, b, chatID, c.Name, shared.Users[0].UserID)
}

// bindClient attaches a Telegram id to a client, or clears it when id is 0.
//
// Admin-only, and it goes through the same save path as every other write so
// the change is audited like one.
//
// The outcome takes the picker keyboard away with it: whether the operator
// tapped a contact or typed an id, the "choose from contacts" button under the
// input box has served its purpose, and an inline keyboard does not dismiss it.
func bindClient(ctx context.Context, b *bot.Bot, chatID int64, name string, tgID int64) {
	markup := &models.ReplyKeyboardRemove{RemoveKeyboard: true}
	applyClientWith(ctx, b, chatID, name, markup, func(c *model.Client) (string, map[string]string) {
		c.TgId = tgID
		if tgID == 0 {
			return "bind.removed", nil
		}
		return "bind.done", p("id", strconv.FormatInt(tgID, 10))
	})
}
