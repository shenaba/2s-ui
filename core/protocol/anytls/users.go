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
	// Unclosable is deliberately not checked: newSessionConnection registers
	// the transport of every anytls session, so a removed user's session is
	// always cut outright and never costs the inbound a rebuild. The only thing
	// that can be counted here is a straggler stream that reached the router
	// after its own session had already ended -- not a session, and not worth
	// disconnecting everyone else for. If anytls ever grows a session this
	// inbound does not hold the transport of, that stops being true.
	h.sessions().CloseUsers(usersession.KeepSet(common.Map(users, func(it option.AnyTLSUser) string {
		return it.Name
	})))
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
// The call blocks until the session ends, so the deferred Untrack is what keeps
// the registry to sessions that actually exist.
func (h *Inbound) newSessionConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	key := source.String()
	h.sessions().Track(key, conn)
	defer h.sessions().Untrack(key)
	return h.service.NewConnection(ctx, conn, source, onClose)
}
