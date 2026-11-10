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
func (h *Inbound) UpdateUsers(users []User) error {
	users = h.usable(users)
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
	return nil
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
