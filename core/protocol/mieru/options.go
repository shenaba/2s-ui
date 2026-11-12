// Package mieru is the panel's mieru inbound (issue #288). Unlike the other
// packages under core/protocol/ it is not a copy of anything in sing-box --
// upstream has no mieru -- so scripts/check-protocol-copies.sh has nothing to
// compare it with, and every file here is ours.
//
// The protocol itself comes from github.com/enfein/mieru/v3. The option shape
// follows enfein/mbox, the mieru author's sing-box fork, so a config written
// for one reads the same in the other.
package mieru

import (
	"github.com/sagernet/sing-box/option"
)

// TypeMieru is the inbound type. sing-box has no constant for it.
const TypeMieru = "mieru"

// The two transports a mieru listener can run on. A listener is one or the
// other; a client that wants both is given two inbounds on the same port.
const (
	TransportTCP = "TCP"
	TransportUDP = "UDP"
)

type InboundOptions struct {
	option.ListenOptions
	Users          []User `json:"users,omitempty"`
	Transport      string `json:"transport,omitempty"`
	TrafficPattern string `json:"traffic_pattern,omitempty"`
	// UserHintIsMandatory makes the server refuse a client that does not send
	// a user hint, which lets it skip trying every user's key in turn. mieru
	// clients from 3.x send one.
	UserHintIsMandatory bool `json:"user_hint_is_mandatory,omitempty"`
}

type User struct {
	Name     string `json:"name,omitempty"`
	Password string `json:"password,omitempty"`
}
