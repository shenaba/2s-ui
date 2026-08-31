package tgbot

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"

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
//
// What ends a binding early is therefore the keyboard itself going away, not
// the form expiring: cancelling sends a ReplyKeyboardRemove, and once Telegram
// has taken a custom keyboard down the operator cannot bring it back. That is
// the whole disarm mechanism, and it is why the cancel path answers with a
// removal instead of the menu.
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

// disarmBindPrompt takes the contact picker away when this chat has a binding
// in progress, reporting whether it had one.
//
// The picker is a reply keyboard: an inline markup does not dismiss one, and
// OneTimeKeyboard only hides it once a button is actually used. So it outlives
// the prompt that raised it, and sending a removal is the only thing that ends
// a binding early -- clearing the form does not, because onUsersShared
// deliberately does not consult it.
//
// Every path that abandons the flow on purpose has to call this. Passive expiry
// deliberately does not: an operator who spent a while choosing a contact still
// wants the binding they asked for.
func disarmBindPrompt(ctx context.Context, b *bot.Bot, chatID int64) bool {
	form, live := forms.get(chatID)
	if !live || form.Step != stepBindTgId {
		return false
	}
	reply(ctx, b, chatID, t("cancelled", nil), &models.ReplyKeyboardRemove{RemoveKeyboard: true})
	return true
}

// onUsersShared completes a binding the operator answered with the picker.
//
// It does not consult the form, deliberately -- see sendBindPrompt. What
// withdraws a binding is the picker keyboard going away, which is what the
// cancel path sends; gating on the form instead would expire a pick the
// operator was still in the middle of making, and there is no honest thing to
// say to them when that happens.
//
// Only a completed binding removes the keyboard, which bindClient does. The two
// failures below keep it and answer with the menu instead: Telegram allows one
// markup per message, and leaving the operator with neither a picker to retry
// with nor a menu to move on from is the worse of the two.
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

// bindMu serialises the bind flow's check-then-write. See bindClient.
var bindMu sync.Mutex

// bindClient attaches a Telegram id to a client, or clears it when id is 0.
//
// Admin-only, and it goes through the same save path as every other write so
// the change is audited like one.
//
// The outcome takes the picker keyboard away with it: whether the operator
// tapped a contact or typed an id, the "choose from contacts" button under the
// input box has served its purpose, and an inline keyboard does not dismiss it.
func bindClient(ctx context.Context, b *bot.Bot, chatID int64, name string, tgID int64) {
	reply(ctx, b, chatID, applyBinding(chatID, name, tgID),
		&models.ReplyKeyboardRemove{RemoveKeyboard: true})
}

// applyBinding is the serialised half of bindClient: the conflict check and the
// write, and nothing else.
//
// The check is a read followed by a write and tg_id carries no unique index, so
// something has to close the gap. A lock is enough because the bot is the only
// writer of that column -- the panel's client form has no field for it, and
// preserveServerManagedFields keeps a save from touching it. Nothing that talks
// to Telegram happens inside it: the reply is a round trip on a 60s client, and
// a stalled edge would otherwise hold every other bind behind it for a minute.
// The write itself can still restart the affected inbounds, which is inherent
// to serialising it and is at least bounded.
func applyBinding(chatID int64, name string, tgID int64) string {
	bindMu.Lock()
	defer bindMu.Unlock()

	// One Telegram id has to resolve to exactly one client. roleOf takes the
	// first matching row, so a second binding does not give that person a
	// second account -- it makes one of the two invisible to them. Refuse
	// instead, naming what already holds the id so the operator knows what to
	// unbind.
	other, err := clientBoundTo(tgID, name)
	if err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	if other != "" {
		return t("bind.taken", p("id", strconv.FormatInt(tgID, 10), "name", other))
	}
	return editClient(chatID, name, func(c *model.Client) (string, map[string]string) {
		c.TgId = tgID
		if tgID == 0 {
			return "bind.removed", nil
		}
		return "bind.done", p("id", strconv.FormatInt(tgID, 10))
	})
}

// clientBoundTo names the other clients already holding tgID, or "" when it is
// free. exclude is the client being bound, so re-confirming an id a client
// already has is not reported as a conflict with itself.
//
// All of them, not the first one the query plan happens to reach: an install
// from before this check can carry more than one, and naming them one at a time
// would send the operator round the loop once per duplicate. Ordered the way
// roleOf resolves them so the client that would actually answer in the bot
// comes first, and capped because the answer is read on a phone.
//
// Disabled clients count: roleOf resolves those too, and a client that ran out
// is precisely the one whose owner is about to open the bot.
func clientBoundTo(tgID int64, exclude string) (string, error) {
	// 0 is the column default, so it is what every client that was never bound
	// carries -- a lookup for it matches all of them and would report the first
	// unbound client as a conflict. Unbinding is the one call that passes 0,
	// and it can never collide with anything, so the invariant belongs here
	// rather than in whichever caller happens to remember it.
	if tgID == 0 {
		return "", nil
	}
	var names []string
	err := database.GetDB().Model(model.Client{}).
		Where("tg_id = ? AND name <> ?", tgID, exclude).
		Order("enable DESC, id ASC").
		Limit(clientBoundToLimit).Pluck("name", &names).Error
	if err != nil {
		return "", err
	}
	return strings.Join(names, ", "), nil
}

// clientBoundToLimit bounds how many holders of one Telegram id the refusal
// names. Duplicates can only be legacy rows now, so more than a couple means
// something else is wrong and the list stops being the useful part.
const clientBoundToLimit = 5
