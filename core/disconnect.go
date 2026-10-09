package core

import (
	"github.com/shenaba/2s-ui/core/usersession"
)

// userKicker is what the protocol copies in core/protocol/ expose in their
// users.go: a way to reach the session registry from outside the inbound.
type userKicker interface {
	KickUser(user string) usersession.Result
}

// DisconnectResult is what disconnecting one user managed to do.
type DisconnectResult struct {
	// Connections counts the routed connections closed.
	Connections int `json:"connections"`
	// Sessions counts the multiplex / anytls sessions cut outright.
	Sessions int `json:"sessions"`
	// Unclosable counts the inbounds where the user is known to hold a session
	// this layer cannot reach -- the QUIC ones. Their routed connections are
	// closed, but the client opens new streams on the session it still has.
	Unclosable int `json:"unclosable"`
}

// DisconnectUser drops every live connection of a user who stays enabled: an
// operator action, so nothing stops them from connecting again.
//
// It deliberately never rebuilds an inbound. That is the only way to end a
// QUIC session here (see core/usersession), and it would disconnect everyone
// else on the inbound too -- a price a removal pays, but not a button that is
// meant for looking at one client. Unclosable says when that limit was hit.
func (c *Core) DisconnectUser(user string) (DisconnectResult, error) {
	var result DisconnectResult
	box, err := c.running()
	if err != nil {
		return result, err
	}
	// Sessions first: cutting a carrier ends the streams on it, so fewer of
	// them are left for the tracker to close one by one.
	for _, inbound := range box.Inbound().Inbounds() {
		kicker, ok := inbound.(userKicker)
		if !ok {
			continue
		}
		r := kicker.KickUser(user)
		result.Sessions += r.Cut
		result.Unclosable += r.Unclosable
	}
	result.Connections = box.ConnTracker().CloseConnByUser(user)
	return result, nil
}
