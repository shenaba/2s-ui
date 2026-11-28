//go:build with_gvisor

package core

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/adapter"
)

// A wireguard endpoint needs gVisor, so these only build with with_gvisor.

const testWireguard = `{"type":"wireguard","tag":"wg","address":["10.9.0.1/32"],"private_key":"yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=","peers":[]}`

// An endpoint a selector names is protected like an outbound: the selector
// resolved it at Start. The dependency check used to look at outbounds only,
// so a wireguard endpoint in a selector was closed under it.
func TestAnEndpointASelectorUsesCannotBeRemoved(t *testing.T) {
	c := startedCore(t)
	if err := c.AddEndpoint([]byte(testWireguard)); err != nil {
		t.Fatalf("AddEndpoint: %v", err)
	}
	addOutbound(t, c, `{"type":"selector","tag":"sel","outbounds":["wg"]}`)

	if err := c.RemoveEndpoint("wg"); err == nil || !strings.Contains(err.Error(), "depended by sel") {
		t.Fatalf("removing an endpoint a selector uses must be refused, got %v", err)
	}
	if err := c.AddEndpoint([]byte(testWireguard)); err == nil || !strings.Contains(err.Error(), "depended by sel") {
		t.Fatalf("replacing an endpoint a selector uses must be refused, got %v", err)
	}
	if got := c.EndpointDependents("wg"); len(got) != 1 || got[0] != "sel" {
		t.Errorf("EndpointDependents = %v, want [sel]", got)
	}
	if _, found := c.GetInstance().Endpoint().Get("wg"); !found {
		t.Fatal("a refused removal must leave the endpoint in place")
	}
}

// route.final can name an endpoint, which lives in the endpoint manager. The
// default used to be the object found at start, so editing that endpoint left
// every unmatched connection on the closed instance.
func TestAnEndpointRouteFinalFollowsAnEdit(t *testing.T) {
	c := NewCore()
	if err := c.Start([]byte(`{"log":{"disabled":true},"endpoints":[` + testWireguard + `],"route":{"final":"wg"}}`)); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	box := c.GetInstance()
	before := box.Outbound().Default()
	if before == nil || before.Tag() != "wg" {
		t.Fatalf("default = %v, want wg", before)
	}

	if err := c.AddEndpoint([]byte(testWireguard)); err != nil {
		t.Fatalf("edit the endpoint: %v", err)
	}
	if got := box.Outbound().Default(); got == nil || got == before {
		t.Fatalf("default after editing the route.final endpoint = %v, want the new instance", got)
	}
	if err := c.RemoveEndpoint("wg"); err == nil || !strings.Contains(err.Error(), "route.final") {
		t.Fatalf("removing the route.final endpoint must be refused, got %v", err)
	}
}

// ValidateEndpoint must not build the endpoint: a wireguard endpoint creates
// its gVisor stack in its constructor, and only Start registers the cleanup.
func TestValidateEndpointBuildsNothing(t *testing.T) {
	c := startedCore(t)
	box := c.GetInstance()
	build := box.endpoint.build
	builds := 0
	box.endpoint.build = func(r recipe) (adapter.Endpoint, error) {
		builds++
		return build(r)
	}

	if err := c.ValidateEndpoint([]byte(testWireguard)); err != nil {
		t.Fatalf("validate: %v", err)
	}
	detoured := strings.Replace(testWireguard, `"peers":[]`, `"peers":[],"detour":"gone"`, 1)
	if err := c.ValidateEndpoint([]byte(detoured)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("validate an endpoint with a missing detour = %v, want it refused", err)
	}
	if builds != 0 {
		t.Errorf("ValidateEndpoint built %d endpoints, want none", builds)
	}
}

// Renaming an endpoint a group uses is refused like removing it: the group
// names the old tag.
func TestRenamingAnEndpointASelectorUsesIsRefused(t *testing.T) {
	c := startedCore(t)
	if err := c.AddEndpoint([]byte(testWireguard)); err != nil {
		t.Fatalf("AddEndpoint: %v", err)
	}
	addOutbound(t, c, `{"type":"selector","tag":"sel","outbounds":["wg"]}`)

	renamed := strings.Replace(testWireguard, `"tag":"wg"`, `"tag":"wg2"`, 1)
	if err := c.RenameEndpoint("wg", []byte(renamed)); err == nil || !strings.Contains(err.Error(), "depended by sel") {
		t.Fatalf("renaming an endpoint a selector uses = %v, want it refused", err)
	}
	if _, found := c.GetInstance().Endpoint().Get("wg"); !found {
		t.Fatal("a refused rename must leave the endpoint in place")
	}
}

// A route rule may name an endpoint (the router looks outbounds up through the
// endpoints), and since sing-box 1.14.6 a box whose rule names a missing one
// does not build. So removing or renaming one a rule names is refused like
// route.final; an edit that keeps the tag is not.
func TestAnEndpointARuleNamesCannotBeRemovedOrRenamed(t *testing.T) {
	c := NewCore()
	if err := c.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"endpoints":[` + testWireguard + `],"route":{"rules":[{"domain":["example.com"],"outbound":"wg"}]}}`)); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })

	if err := c.RemoveEndpoint("wg"); err == nil || !strings.Contains(err.Error(), "named by route rule #1") {
		t.Fatalf("removing an endpoint a route rule names must be refused, got %v", err)
	}
	renamed := strings.Replace(testWireguard, `"tag":"wg"`, `"tag":"wg2"`, 1)
	if err := c.RenameEndpoint("wg", []byte(renamed)); err == nil || !strings.Contains(err.Error(), "named by route rule #1") {
		t.Fatalf("renaming an endpoint a route rule names must be refused, got %v", err)
	}
	if _, found := c.GetInstance().Endpoint().Get("wg"); !found {
		t.Fatal("a refused removal or rename must leave the endpoint in place")
	}
	if err := c.AddEndpoint([]byte(testWireguard)); err != nil {
		t.Fatalf("an edit that keeps the tag must still go through: %v", err)
	}
}
