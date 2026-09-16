package anytls

import (
	"context"
	"net"

	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	anytls "github.com/anytls/sing-anytls"
)

// UpdateUsers swaps the user table of a running inbound, and cuts the sessions
// of everyone who just left it. Both halves are needed: the table alone only
// decides who may open a *new* session, while an anytls client that
// authenticated before the change keeps opening streams on the one it has.
func (h *Inbound) UpdateUsers(users []option.AnyTLSUser) error {
	h.service.UpdateUsers(common.Map(users, func(it option.AnyTLSUser) anytls.User {
		return (anytls.User)(it)
	}))
	keep := make(map[string]struct{}, len(users))
	for _, user := range users {
		keep[user.Name] = struct{}{}
	}
	h.sessions().CloseUsers(keep)
	return nil
}

// withUserSessions installs the session registry. anytls only needs the router
// to record who is connected -- the session itself is reached in
// newSessionConnection below, which is where it can be closed.
func withUserSessions(router adapter.ConnectionRouterEx) adapter.ConnectionRouterEx {
	return usersession.WrapRouterEx(router, usersession.BindOnly)
}

// sessions reaches the registry installed by withUserSessions. NewInbound wraps
// the router right after building the struct and nothing replaces the field
// afterwards, so this holds. It is a bare assertion on purpose: someone
// reordering that would otherwise turn every session hook here into a silent
// no-op, and a panic on the save path is the lesser outcome.
func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.RouterEx).Registry()
}

// newSessionConnection stands in for h.service.NewConnection at the one call
// site that owns an anytls session for its whole life. Registering the session
// here is what makes it closable; the streams it later opens reach the router
// individually and cannot be used to find it.
//
// Returning an error rather than closing the connection reuses the rejection
// the call site already has (N.CloseOnHandshakeFailure).
func (h *Inbound) newSessionConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	key := source.String()
	// A session with a closer is cut outright rather than muted, so this only
	// fires if closing one failed -- keeping the mute as the backstop.
	if !h.sessions().Allowed(key) {
		return usersession.ErrRemoved
	}
	h.sessions().Track(key, conn)
	defer h.sessions().Untrack(key)
	return h.service.NewConnection(ctx, conn, source, onClose)
}
