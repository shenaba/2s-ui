package core

import (
	"testing"

	"github.com/sagernet/sing-box/experimental/clashmode"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/service"
)

// sing-box 1.14.1 split the clash mode state out of the clash API server into
// its own manager, registered by box.go rather than by the server. A `clash_mode`
// route rule resolves that manager at Start and, finding nothing, simply never
// matches -- so dropping the registration from core.NewBox costs no compile
// error and no start failure, only rules that quietly stop working.
func TestClashModeManagerIsRegistered(t *testing.T) {
	// An `api` service is what puts this box on the branch that registers the
	// manager; listen_port 0 takes an ephemeral port so the test binds nothing fixed.
	const config = `{
		"log": {"disabled": true},
		"services": [{"type": "api", "tag": "api-in", "listen": "127.0.0.1", "listen_port": 0}],
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"route": {"rules": [{"clash_mode": "Direct", "outbound": "direct"}]}
	}`

	c := NewCore()
	if err := c.Start([]byte(config)); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })

	manager := service.PtrFromContext[clashmode.Manager](c.GetInstance().ctx)
	if manager == nil {
		t.Fatal("clash mode manager is not registered; clash_mode rules will never match")
	}
	// The mode list is built from the rules, so seeing the rule's own mode in it
	// is what says the manager was handed this config rather than an empty one.
	if !common.Contains(manager.ModeList(), "Direct") {
		t.Errorf("mode list = %v, want it to carry the rule's mode", manager.ModeList())
	}
}
