package tgbot

import (
	"strconv"

	"github.com/shenaba/2s-ui/service"
)

type role int

const (
	// roleNone is anyone the panel does not know. The bot does not answer them
	// at all -- not even "you are not authorised", which would confirm that a
	// panel bot lives at this token to anyone who found it.
	roleNone role = iota
	roleAdmin
)

// roleOf decides what a chat is allowed to do.
//
// The admin list is re-read per update rather than captured at connect, so
// adding an admin takes effect immediately and removing one takes effect
// immediately too -- which is the half that matters.
func roleOf(chatID int64) role {
	var settingService service.SettingService
	cfg := settingService.GetBotConfig()
	id := strconv.FormatInt(chatID, 10)
	for _, admin := range cfg.Admins {
		if admin == id {
			return roleAdmin
		}
	}
	return roleNone
}
