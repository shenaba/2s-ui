package hysteria

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

// UpdateUsers swaps the user table of a running inbound and disconnects
// everyone who just left it -- see the note in tuic/users.go, hysteria reuses
// an authenticated session the same way, and a removed user who is still
// connected costs the inbound a rebuild for the same reason.
func (h *Inbound) UpdateUsers(users []option.HysteriaUser) error {
	userList := make([]string, 0, len(users))
	userPasswordList := make([]string, 0, len(users))
	for _, user := range users {
		userList = append(userList, user.Name)
		var password string
		if user.AuthString != "" {
			password = user.AuthString
		} else {
			password = string(user.Auth)
		}
		userPasswordList = append(userPasswordList, password)
	}
	h.service.UpdateUsers(userList, userPasswordList)
	return h.sessions().CloseUsers(usersession.KeepSet(userList)).RestartRequired()
}

// withUserSessions installs the session registry in front of the router. This
// inbound holds the full adapter.Router, so it takes the Router shim.
func withUserSessions(router adapter.Router) adapter.Router {
	return usersession.WrapRouter(router, usersession.BindOnly)
}

func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.Router).Registry()
}
