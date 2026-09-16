package hysteria2

import (
	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

// UpdateUsers swaps the user table of a running inbound and shuts out everyone
// who just left it -- see the note in tuic/users.go, hysteria2 reuses an
// authenticated session the same way.
func (h *Inbound) UpdateUsers(users []option.Hysteria2User) error {
	userList := make([]string, 0, len(users))
	userPasswordList := make([]string, 0, len(users))
	for _, user := range users {
		userList = append(userList, user.Name)
		userPasswordList = append(userPasswordList, user.Password)
	}
	h.service.UpdateUsers(userList, userPasswordList)
	h.sessions().CloseUsers(usersession.KeepSet(userList))
	return nil
}

// withUserSessions installs the session registry in front of the router. This
// inbound holds the full adapter.Router, so it takes the Router shim.
func withUserSessions(router adapter.Router) adapter.Router {
	return usersession.WrapRouter(router, usersession.GateAndBind)
}

func (h *Inbound) sessions() *usersession.Registry {
	return h.router.(*usersession.Router).Registry()
}
