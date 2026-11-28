package core

import (
	suiTrojan "github.com/shenaba/2s-ui/core/protocol/trojan"
	suiVless "github.com/shenaba/2s-ui/core/protocol/vless"

	"github.com/sagernet/sing-box/adapter"
)

// The copies under core/protocol implement interfaces sing-box only ever
// type-asserts, so a signature that drifts from upstream still compiles and the
// method silently stops being called. 1.14.6 did exactly that to References
// (a second result, for DNS servers): the copies kept the old one, and the
// reference manager stopped seeing what their fallbacks detour through. These
// make the next such change a build failure instead.
var (
	_ adapter.Referrer = (*suiTrojan.Inbound)(nil)
	_ adapter.Referrer = (*suiVless.Inbound)(nil)
)
