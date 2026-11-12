package mieru

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing/common"
)

// UpdateUsers swaps the user table of a running inbound, and cuts the TCP
// underlays of everyone who just left it.
//
// Both halves are needed on TCP: the new table only decides who may open a
// new underlay, while one that authenticated before the change keeps carrying
// sessions. On UDP every session authenticates by itself against the current
// table, so the swap alone locks a removed user out.
//
// It never fails. A user mieru cannot serve is dropped and reported
// (usableUsers) rather than refused: refusing the list sends the caller to
// rebuild the inbound, which the same list fails again, and that takes the
// listener from every other client.
//
// Nor does it ask for a restart. Every TCP underlay is registered before
// mieru sees it (trackingListener), so a removed user always has a closer;
// anything CloseUsers still counts as unclosable is a stray Bind on a source
// that was already gone.
//
// A user who stays but whose password changed has their TCP underlays cut as
// well. Elsewhere a changed credential leaves a live connection alone and
// only refuses the next one, but here the next session rides the underlay the
// old password opened, so without the cut the old password kept opening new
// sessions for as long as the client held that underlay -- a password rotated
// because it leaked went on working. The client reconnects, and the new
// password decides. On UDP the swap alone refuses the old password's next
// session, as it does a removed user's.
//
// One window stays open: an underlay that authenticated with the old password
// just before the swap and reaches its Bind just after it is not cut, since a
// session names its user and not the key it used. That is a handshake in
// flight at the moment of the change, not a client holding on.
func (h *Inbound) UpdateUsers(users []User) error {
	users = h.usable(users)
	rotated := rotatedUsers(h.currentUsers(), users)
	h.storeUsers(users)
	h.access.Lock()
	mux := h.mux
	h.access.Unlock()
	if mux != nil {
		mux.SetServerUsers(serverUsers(users))
	}
	h.sessions.CloseUsers(usersession.KeepSet(common.Map(users, func(it User) string {
		return it.Name
	})))
	if h.transport == TransportTCP {
		for _, name := range rotated {
			h.sessions.KickUserSessions(name)
		}
	}
	return nil
}

// rotatedUsers is the names in both lists whose password differs between them.
func rotatedUsers(before, after []User) []string {
	previous := make(map[string]string, len(before))
	for _, user := range before {
		previous[user.Name] = user.Password
	}
	var rotated []string
	for _, user := range after {
		if password, ok := previous[user.Name]; ok && password != user.Password {
			rotated = append(rotated, user.Name)
		}
	}
	return rotated
}

// KickUser cuts the TCP underlays of a user who stays on the inbound -- see
// Core.DisconnectUser. The sessions themselves are routed connections, which
// the connection tracker closes; on UDP that is all there is.
func (h *Inbound) KickUser(user string) usersession.Result {
	return h.sessions.KickUserSessions(user)
}

func (h *Inbound) storeUsers(users []User) {
	list := append([]User(nil), users...)
	names := make(map[string]struct{}, len(list))
	for _, user := range list {
		names[user.Name] = struct{}{}
	}
	h.users.Store(&names)
	h.userList.Store(&list)
}

func (h *Inbound) currentUsers() []User {
	if list := h.userList.Load(); list != nil {
		return *list
	}
	return nil
}

func (h *Inbound) allowed(user string) bool {
	names := h.users.Load()
	if names == nil {
		return false
	}
	_, ok := (*names)[user]
	return ok
}
