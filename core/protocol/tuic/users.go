package tuic

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"

	"github.com/gofrs/uuid/v5"
)

// UpdateUsers swaps the user table of a running inbound and disconnects
// everyone who just left it. Both halves are needed: the table alone only
// decides who may open a new session, while a client that authenticated before
// the change keeps opening streams on the one it already has.
//
// The second half is not something this layer can do to a QUIC session, so when
// a removed user is still connected it reports ErrRestartRequired and the
// caller rebuilds the inbound instead. That disconnects everyone on it once,
// which is why it is reported rather than done unconditionally: an update that
// removes nobody, or removes nobody who is connected, still costs nothing.
func (h *Inbound) UpdateUsers(users []option.TUICUser) error {
	userList := make([]string, 0, len(users))
	userUUIDList := make([][16]byte, 0, len(users))
	userPasswordList := make([]string, 0, len(users))
	for index, user := range users {
		if user.UUID == "" {
			return E.New("missing uuid for user ", index)
		}
		userUUID, err := uuid.FromString(user.UUID)
		if err != nil {
			return E.Cause(err, "invalid uuid for user ", index)
		}
		userList = append(userList, user.Name)
		userUUIDList = append(userUUIDList, userUUID)
		userPasswordList = append(userPasswordList, user.Password)
	}
	h.server.UpdateUsers(userList, userUUIDList, userPasswordList)
	return h.sessions().CloseUsers(usersession.KeepSet(userList)).RestartRequired()
}

// withUserSessions installs the session registry in front of the router. TUIC
// authenticates once per QUIC session and every stream after that rides it, so
// the registry is here to answer one question at removal time: is the user
// being removed actually connected? It cannot do more than that -- sing-quic
// hands out neither a handle on the session nor a stable name for it. See
// core/usersession.
func withUserSessions(router adapter.ConnectionRouterEx) adapter.ConnectionRouterEx {
	return usersession.WrapRouterEx(router, usersession.BindOnly)
}

// sessions reaches the registry withUserSessions installed. The assertion is
// bare on purpose: reordering NewInbound so that it no longer holds would
// otherwise turn every hook here into a silent no-op.
func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.RouterEx).Registry()
}
