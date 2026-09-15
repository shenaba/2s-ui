package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// minimalConfig binds nothing: the default route is enough for a box to start,
// and these tests are about which box a call reaches, not about traffic.
const minimalConfig = `{"log":{"disabled":true}}`

func startedCore(t *testing.T) *Core {
	t.Helper()
	c := NewCore()
	if err := c.Start([]byte(minimalConfig)); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c
}

// The managers used to live in package-level vars that Start overwrote, so they
// belonged to whichever box started last rather than to the box the call was
// made on. In production that is a restart replacing the box under an in-flight
// hot reload; here it is two cores, which is the same thing said deterministically.
func TestHotReloadReachesTheCoreItWasCalledOn(t *testing.T) {
	a := startedCore(t)
	b := startedCore(t)

	outbound := func(tag string) []byte {
		raw, err := json.Marshal(map[string]interface{}{"type": "direct", "tag": tag})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return raw
	}

	if err := a.AddOutbound(outbound("a-out")); err != nil {
		t.Fatalf("AddOutbound on a: %v", err)
	}
	if err := b.AddOutbound(outbound("b-out")); err != nil {
		t.Fatalf("AddOutbound on b: %v", err)
	}

	// Each core sees its own outbound and not the other's.
	if _, found := a.GetInstance().Outbound().Outbound("a-out"); !found {
		t.Error("a's own outbound is missing from a")
	}
	if _, found := a.GetInstance().Outbound().Outbound("b-out"); found {
		t.Error("b's outbound landed in a")
	}
	if _, found := b.GetInstance().Outbound().Outbound("a-out"); found {
		t.Error("a's outbound landed in b")
	}

	// And a removal goes to the right one: before, this reached whichever
	// manager Start had assigned last, which is b.
	if err := a.RemoveOutbound("a-out"); err != nil {
		t.Errorf("RemoveOutbound on a: %v", err)
	}
	if _, found := b.GetInstance().Outbound().Outbound("b-out"); !found {
		t.Error("removing a's outbound removed b's instead")
	}
}

// Every hot-reload entry point has to refuse once the core is down, rather than
// operating on a manager left over from the box that has been closed.
func TestHotReloadRefusesAStoppedCore(t *testing.T) {
	c := startedCore(t)
	if err := c.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	direct := []byte(`{"type":"direct","tag":"x"}`)
	calls := map[string]func() error{
		"AddInbound":     func() error { return c.AddInbound([]byte(`{"type":"mixed","tag":"x"}`)) },
		"RemoveInbound":  func() error { return c.RemoveInbound("x") },
		"AddOutbound":    func() error { return c.AddOutbound(direct) },
		"RemoveOutbound": func() error { return c.RemoveOutbound("x") },
		"AddEndpoint":    func() error { return c.AddEndpoint([]byte(`{"type":"wireguard","tag":"x"}`)) },
		"RemoveEndpoint": func() error { return c.RemoveEndpoint("x") },
		"AddService":     func() error { return c.AddService([]byte(`{"type":"resolved","tag":"x"}`)) },
		"RemoveService":  func() error { return c.RemoveService("x") },
		"UpdateInboundUsers": func() error {
			_, err := c.UpdateInboundUsers([]byte(`{"type":"vless","tag":"x"}`))
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("accepted on a stopped core")
			}
			if !strings.Contains(err.Error(), "not running") {
				t.Errorf("error = %q, want it to say the core is not running", err)
			}
		})
	}

	if got := c.CheckOutbound("x", "https://example.com"); got.OK || !strings.Contains(got.Error, "not running") {
		t.Errorf("CheckOutbound on a stopped core = %+v, want a not-running error", got)
	}
}
