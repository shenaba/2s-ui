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
	// roleNone is anyone the panel does not know. The bot does not answer them
	// at all -- not even "you are not authorised", which would confirm that a
	// panel bot lives at this token to anyone who found it.
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
	var name string
	err := database.GetDB().Model(model.Client{}).
		Where("tg_id = ? AND enable = ?", chatID, true).
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
