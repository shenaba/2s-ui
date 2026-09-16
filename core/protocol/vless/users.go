package vless

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
)

// UpdateUsers swaps the user table of a running inbound and cuts the multiplex
// sessions of everyone who just left it. VLESS authenticates per connection, so
// the table alone already shuts a removed user out of new ones -- except on a
// multiplex session, where one authenticated carrier connection keeps serving
// every stream opened after the change.
func (h *Inbound) UpdateUsers(users []option.VLESSUser) error {
	h.service.UpdateUsers(common.Map(users, func(it option.VLESSUser) string {
		return it.Name
	}), common.Map(users, func(it option.VLESSUser) string {
		return it.UUID
	}), common.Map(users, func(it option.VLESSUser) string {
		return it.Flow
	}))
	// Unclosable is deliberately not checked: a mux carrier is a net.Conn and
	// is cut outright, so a removed user never costs this inbound the rebuild a
	// QUIC one does. The only thing that can be counted here is a packet conn
	// addressed to the mux destination -- not a carrier, and not worth
	// disconnecting everyone else for.
	h.sessions().CloseUsers(usersession.KeepSet(common.Map(users, func(it option.VLESSUser) string {
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

// sessions reaches the registry withUserSessions installed. The assertion is
// bare on purpose: reordering NewInbound so that it no longer holds would
// otherwise turn every hook here into a silent no-op.
func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.RouterEx).Registry()
}
