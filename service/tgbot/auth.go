package tgbot

import (
	"strconv"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
)

type role int

const (
	// roleNone is anyone the panel does not know. They get an answer, but one
	// that says nothing about which panel is behind the bot -- no product name,
	// no hostname, no figures, no keyboard listing the management commands. See
	// strangerReply, which is split out so a test can pin that rule. Answering
	// at all is what makes /id reachable, and someone who has to be told their
	// Telegram id is exactly someone the panel does not know yet.
	roleNone role = iota
	// roleClient is an end user bound to one client. They can look at their own
	// usage and nothing else.
	roleClient
	roleAdmin
)

// roleOf decides what a chat is allowed to do, returning the bound client name
// for roleClient.
//
// Both lookups happen per update rather than being captured at connect, so
// adding an admin takes effect immediately -- and so does removing one, which
// is the half that matters.
func roleOf(chatID int64) (role, string) {
	var settingService service.SettingService
	cfg := settingService.GetBotConfig()
	id := strconv.FormatInt(chatID, 10)
	for _, admin := range cfg.Admins {
		if admin == id {
			return roleAdmin, ""
		}
	}

	// tg_id defaults to 0, so an unbound client row would match a zero chat id
	// and hand a stranger someone else's usage.
	if chatID == 0 {
		return roleNone, ""
	}
	db := database.GetDB()
	if db == nil {
		// No database, no way to prove anyone is who they claim: refuse.
		return roleNone, ""
	}
	// Disabled clients resolve too, and this reverses an earlier rule rather
	// than overlooking it: the filter used to be `enable = true`. Being cut off
	// is the moment a client most needs the one thing this role can do --
	// selfUsageText has wording for exactly that state in all six languages,
	// which is unreachable otherwise -- and the depletion alert tells them
	// their account is off, so refusing here answered them as a stranger
	// seconds after the panel had messaged them by name. The view stays
	// read-only and scoped to their own row either way. The cost, accepted: a
	// client disabled deliberately rather than by the quota keeps that view.
	//
	// Ordered because the answer must not depend on the query plan: bindClient
	// refuses an id another client already holds, but installs upgrading from
	// before that check may carry a duplicate, and an enabled client is the
	// better guess of the two.
	var name string
	err := db.Model(model.Client{}).
		Where("tg_id = ?", chatID).
		Order("enable DESC, id ASC").
		Limit(1).Pluck("name", &name).Error
	if err != nil {
		logger.Warning("tgbot: resolving the sender failed: ", err)
		return roleNone, ""
	}
	if name != "" {
		return roleClient, name
	}
	return roleNone, ""
}
