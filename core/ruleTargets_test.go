package core

import (
	"strings"
	"testing"
)

// sing-box 1.14.6 refuses to build a box whose route rules name a missing
// outbound or DNS server (router.ValidateRuleActions). 1.14.4 built it, so
// these pin both what NewBox now does and the two places the panel keeps
// itself out of that state.

func TestABoxWithARuleNamingAMissingOutboundDoesNotStart(t *testing.T) {
	c := NewCore()
	err := c.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"route":{"rules":[{"domain":["example.com"],"outbound":"gone"}]}}`))
	if err == nil {
		_ = c.Stop()
		t.Fatal("a rule naming a missing outbound must fail the build, as upstream's ValidateRuleActions does")
	}
	if !strings.Contains(err.Error(), "outbound not found: gone") {
		t.Fatalf("unexpected error %v", err)
	}
}

// Removing an outbound a rule names would leave the running core fine and the
// next restart failing on a loop, so it is refused like route.final. Another
// outbound still goes, and a replacement in place (an edit) is untouched.
func TestAnOutboundARuleNamesCannotBeRemoved(t *testing.T) {
	c := NewCore()
	if err := c.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"},{"type":"direct","tag":"ruled"},{"type":"direct","tag":"bypassed"},{"type":"direct","tag":"free"}],"route":{"rules":[{"domain":["example.com"],"outbound":"ruled"},{"type":"logical","mode":"or","rules":[{"domain":["example.org"]}],"action":"bypass","outbound":"bypassed"}]}}`)); err != nil {
		t.Fatalf("start core: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })

	if err := c.RemoveOutbound("ruled"); err == nil || !strings.Contains(err.Error(), "named by route rule #1") {
		t.Fatalf("removing an outbound a route rule names must be refused, got %v", err)
	}
	if err := c.RemoveOutbound("bypassed"); err == nil || !strings.Contains(err.Error(), "named by route rule #2") {
		t.Fatalf("removing an outbound a logical rule's bypass names must be refused, got %v", err)
	}
	addOutbound(t, c, `{"type":"direct","tag":"ruled"}`)
	if err := c.RemoveOutbound("free"); err != nil {
		t.Fatalf("removing an outbound nothing names: %v", err)
	}
}

func TestCheckRuleActions(t *testing.T) {
	c := NewCore()
	cases := []struct {
		name      string
		config    string
		outbounds []string
		endpoints []string
		want      string // substring of the error; empty for none
	}{
		{"known outbound", `{"route":{"rules":[{"domain":["a"],"outbound":"x"}]}}`, []string{"x"}, nil, ""},
		{"endpoint", `{"route":{"rules":[{"domain":["a"],"outbound":"wg"}]}}`, []string{"x"}, []string{"wg"}, ""},
		{"missing outbound", `{"route":{"rules":[{"domain":["a"],"outbound":"x"},{"domain":["b"],"outbound":"gone"}]}}`, []string{"x"}, nil, "rule #2: outbound not found: gone"},
		{"logical bypass", `{"route":{"rules":[{"type":"logical","mode":"or","rules":[{"domain":["a"]}],"action":"bypass","outbound":"gone"}]}}`, []string{"x"}, nil, "rule #1: outbound not found: gone"},
		{"direct fallback", `{"route":{"rules":[{"domain":["a"],"outbound":"direct"}]}}`, nil, nil, ""},
		{"no fallback with outbounds", `{"route":{"rules":[{"domain":["a"],"outbound":"direct"}]}}`, []string{"x"}, nil, "outbound not found: direct"},
		{"endpoints do not stop the fallback", `{"route":{"rules":[{"domain":["a"],"outbound":"direct"}]}}`, nil, []string{"wg"}, ""},
		{"no fallback with route.final", `{"route":{"final":"x","rules":[{"domain":["a"],"outbound":"direct"}]}}`, nil, nil, "outbound not found: direct"},
		{"resolve known", `{"dns":{"servers":[{"type":"local","tag":"dns-a"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"dns-a"}]}}`, []string{"x"}, nil, ""},
		{"resolve untagged server", `{"dns":{"servers":[{"type":"local"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"0"}]}}`, []string{"x"}, nil, ""},
		{"resolve local fallback", `{"route":{"rules":[{"domain":["a"],"action":"resolve","server":"local"}]}}`, []string{"x"}, nil, ""},
		{"resolve missing", `{"dns":{"servers":[{"type":"local","tag":"dns-a"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"gone"}]}}`, []string{"x"}, nil, "rule #1: DNS server not found: gone"},
		{"other actions name nothing", `{"route":{"rules":[{"domain":["a"],"action":"reject"},{"protocol":"dns","action":"hijack-dns"}]}}`, []string{"x"}, nil, ""},
		{"dns rule known server", `{"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"domain":["a"],"server":"dns-a"}]}}`, []string{"x"}, nil, ""},
		{"dns rule local fallback", `{"dns":{"rules":[{"domain":["a"],"server":"local"}]}}`, []string{"x"}, nil, ""},
		{"dns rule missing server", `{"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"domain":["a"],"server":"dns-a"},{"domain":["b"],"server":"gone"}]}}`, []string{"x"}, nil, "dns rule #2: server not found: gone"},
		{"dns logical rule missing server", `{"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"type":"logical","mode":"or","rules":[{"domain":["a"]}],"server":"gone"}]}}`, []string{"x"}, nil, "dns rule #1: server not found: gone"},
		{"dns evaluate on fakeip", `{"dns":{"servers":[{"type":"local","tag":"dns-a"},{"type":"fakeip","tag":"fake","inet4_range":"198.18.0.0/15"}],"rules":[{"domain":["a"],"action":"evaluate","server":"fake"}]}}`, []string{"x"}, nil, "evaluate action cannot use fakeip server: fake"},
		{"dns reject names nothing", `{"dns":{"rules":[{"domain":["a"],"action":"reject"}]}}`, []string{"x"}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.CheckRuleActions([]byte(tc.config), tc.outbounds, tc.endpoints)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// Every case CheckRuleActions accepts must be one NewBox accepts too, or a save
// would pass and the restart fail. The fallbacks are where the two could part.
func TestCheckRuleActionsAgreesWithTheBoxOnTheFallbacks(t *testing.T) {
	for _, config := range []string{
		`{"log":{"disabled":true},"route":{"rules":[{"domain":["a"],"outbound":"direct"}]}}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"x"}],"route":{"rules":[{"domain":["a"],"action":"resolve","server":"local"}]}}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"x"}],"dns":{"servers":[{"type":"local"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"0"}]}}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"x"}],"dns":{"rules":[{"domain":["a"],"server":"local"}]}}`,
		`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"x"}],"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"domain":["a"],"server":"dns-a"}]}}`,
	} {
		c := NewCore()
		if err := c.Start([]byte(config)); err != nil {
			t.Fatalf("NewBox refused a config CheckRuleActions accepts: %v\n%s", err, config)
		}
		_ = c.Stop()
	}
}

// The check has to run before NewBox builds anything: a wireguard endpoint
// built and dropped is never freed, and the watchdog retries a failing config
// every few seconds. An inbound that fails in its constructor shows which ran
// first -- the build would report the bad method, the check the rule.
func TestRuleActionsAreCheckedBeforeAnythingIsBuilt(t *testing.T) {
	c := NewCore()
	err := c.Start([]byte(`{"log":{"disabled":true},"inbounds":[{"type":"shadowsocks","tag":"ss","listen_port":0,"method":"no-such-method","password":"x"}],"outbounds":[{"type":"direct","tag":"a"}],"route":{"rules":[{"domain":["example.com"],"outbound":"gone"}]}}`))
	if err == nil {
		_ = c.Stop()
		t.Fatal("the config must not start")
	}
	if !strings.Contains(err.Error(), "outbound not found: gone") {
		t.Fatalf("the inbound was built before the rules were checked: %v", err)
	}

	c = NewCore()
	err = c.Start([]byte(`{"log":{"disabled":true},"inbounds":[{"type":"shadowsocks","tag":"ss","listen_port":0,"method":"no-such-method","password":"x"}],"outbounds":[{"type":"direct","tag":"a"}]}`))
	if err == nil || strings.Contains(err.Error(), "not found") {
		_ = c.Stop()
		t.Fatalf("without the bad rule the build itself must fail, or the case above proves nothing: %v", err)
	}
}

func TestRuleNamedOutbounds(t *testing.T) {
	named, err := NewCore().RuleNamedOutbounds([]byte(`{"route":{"rules":[{"domain":["a"],"outbound":"x"},{"domain":["b"],"action":"bypass","outbound":"y"},{"domain":["c"],"outbound":"x"},{"domain":["d"],"action":"reject"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 2 || named["x"] != 0 || named["y"] != 1 {
		t.Fatalf("named = %v, want x at rule 0 and y at rule 1", named)
	}
}

// An untagged outbound is tagged by its index in the box, and a rule may name
// it that way.
func TestCheckConfigRuleActionsTagsUntaggedOutbounds(t *testing.T) {
	c := NewCore()
	if err := c.CheckConfigRuleActions([]byte(`{"outbounds":[{"type":"direct"}],"route":{"rules":[{"domain":["a"],"outbound":"0"}]}}`)); err != nil {
		t.Fatalf("a rule naming an untagged outbound by index was refused: %v", err)
	}
	if err := c.CheckConfigRuleActions([]byte(`{"outbounds":[{"type":"direct"}],"route":{"rules":[{"domain":["a"],"outbound":"1"}]}}`)); err == nil {
		t.Fatal("an index past the outbounds must be refused")
	}
}

// Core.Start stops at checkRuleActions, so no test through it ever reaches
// NewBox's own verdict. This one asks NewBox directly: every config the
// pre-check refuses must be one sing-box refuses too, or the panel would keep
// a core down that sing-box would run.
func TestNewBoxRefusesWhatThePreCheckRefuses(t *testing.T) {
	c := NewCore()
	for name, config := range map[string]string{
		"route to a missing outbound":   `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"route":{"rules":[{"domain":["a"],"outbound":"gone"}]}}`,
		"bypass to a missing outbound":  `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"route":{"rules":[{"domain":["a"],"action":"bypass","outbound":"gone"}]}}`,
		"direct with outbounds present": `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"route":{"rules":[{"domain":["a"],"outbound":"direct"}]}}`,
		"resolve to a missing server":   `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"dns":{"servers":[{"type":"local","tag":"dns-a"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"gone"}]}}`,
		"local with servers present":    `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"dns":{"servers":[{"type":"local","tag":"dns-a"}]},"route":{"rules":[{"domain":["a"],"action":"resolve","server":"local"}]}}`,
		"dns rule to a missing server":  `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"domain":["a"],"server":"gone"}]}}`,
		"dns evaluate on fakeip":        `{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"a"}],"dns":{"servers":[{"type":"local","tag":"dns-a"},{"type":"fakeip","tag":"fake","inet4_range":"198.18.0.0/15"}],"rules":[{"domain":["a"],"action":"evaluate","server":"fake"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			opt, err := c.parseConfig([]byte(config))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if checkBuiltRuleActions(opt) == nil {
				t.Fatal("the pre-check must refuse this config, or the case proves nothing")
			}
			box, err := NewBox(Options{Context: c.ctx, Options: opt})
			if err == nil {
				_ = box.Close()
				t.Fatal("the pre-check refuses a config sing-box builds")
			}
		})
	}
}

// The DNS router can fail on more than a missing server -- here a rule-set a
// DNS rule names that the route does not define -- and those failures must
// come before anything is built too. box.go initializes it ahead of the
// endpoints and inbounds for that; at 1.14.6's place, after them, the inbound
// would be built first and fail on its bad method instead.
func TestDNSRouterFailuresComeBeforeAnythingIsBuilt(t *testing.T) {
	c := NewCore()
	err := c.Start([]byte(`{"log":{"disabled":true},"inbounds":[{"type":"shadowsocks","tag":"ss","listen_port":0,"method":"no-such-method","password":"x"}],"outbounds":[{"type":"direct","tag":"a"}],"dns":{"servers":[{"type":"local","tag":"dns-a"}],"rules":[{"rule_set":["gone"],"server":"dns-a"}]}}`))
	if err == nil {
		_ = c.Stop()
		t.Fatal("the config must not start")
	}
	if !strings.Contains(err.Error(), "dns router") {
		t.Fatalf("the inbound was built before the DNS router was initialized: %v", err)
	}
}
