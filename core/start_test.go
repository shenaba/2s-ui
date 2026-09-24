package core

import (
	"strings"
	"testing"
)

// A config sing-box cannot parse used to be logged and then ignored: opt stayed
// at its zero value, NewBox built a box with no inbounds at all, Start returned
// nil and IsRunning reported true. The five-second watchdog then saw a healthy
// core and never retried, so the panel served nobody while looking fine.
func TestStartRejectsAnUnparseableConfig(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"not json at all", `this is not json`},
		{"truncated", `{"inbounds":[`},
		{"an unknown inbound type", `{"inbounds":[{"type":"definitely-not-a-protocol","tag":"x"}]}`},
		{"a field of the wrong type", `{"inbounds":[{"type":"mixed","tag":"x","listen_port":"not-a-port"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCore()
			err := c.Start([]byte(tt.config))
			if err == nil {
				_ = c.Stop()
				t.Fatal("Start reported success on a config it could not read")
			}
			if c.IsRunning() {
				_ = c.Stop()
				t.Error("the core must not report itself running after a failed start")
			}
			if c.GetInstance() != nil {
				t.Error("no box may be published from a failed start")
			}
		})
	}
}

// The error has to name what went wrong, since it is what the operator reads in
// the log and in the CoreCrash notification.
func TestStartErrorNamesTheProblem(t *testing.T) {
	c := NewCore()
	err := c.Start([]byte(`{"inbounds":[`))
	if err == nil {
		_ = c.Stop()
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, want it to say the config could not be read", err)
	}
}

func TestStartAcceptsPreferIPv4DNS(t *testing.T) {
	config := `{
		"dns": {
			"strategy": "prefer_ipv4",
			"servers": [],
			"rules": []
		},
		"outbounds": [
			{"type": "direct", "tag": "direct"}
		]
	}`
	c := NewCore()
	err := c.Start([]byte(config))
	if err != nil {
		t.Fatalf("Start failed with prefer_ipv4 DNS: %v", err)
	}
	defer c.Stop()
	if !c.IsRunning() {
		t.Error("expected core to be running")
	}
}
