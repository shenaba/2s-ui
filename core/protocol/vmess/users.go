package vmess

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
)

// UpdateUsers swaps the user table of a running inbound and cuts the multiplex
// sessions of everyone who just left it -- see the note in vless/users.go.
// Sessions are only cut once the table actually changed, so a rejected update
// leaves the inbound exactly as it was.
func (h *Inbound) UpdateUsers(users []option.VMessUser) error {
	err := h.service.UpdateUsers(common.Map(users, func(it option.VMessUser) string {
		return it.Name
	}), common.Map(users, func(it option.VMessUser) string {
		return it.UUID
	}), common.Map(users, func(it option.VMessUser) int {
		return it.AlterId
	}))
	if err != nil {
		return err
	}
	h.sessions().CloseUsers(usersession.KeepSet(common.Map(users, func(it option.VMessUser) string {
		return it.Name
	})))
	return nil
}

// withUserSessions installs the session registry in front of the router. Only
// the multiplex carrier is tracked: every other connection authenticates on its
// own and is already covered by ConnTracker. See core/usersession.
func withUserSessions(router adapter.ConnectionRouterEx) adapter.ConnectionRouterEx {
	return usersession.WrapRouterEx(router, usersession.TrackMuxCarrier)
}

func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.RouterEx).Registry()
}
