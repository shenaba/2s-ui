package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

func replicaClient() *model.Client {
	return &model.Client{
		Name:   "alice",
		Remark: "alice",
		Config: json.RawMessage(`{"vless":{"uuid":"11111111-2222-3333-4444-555555555555"}}`),
	}
}

func vlessReplica(addrs string) *model.Inbound {
	nodeId := uint(7)
	return &model.Inbound{
		Type: "vless", Tag: "vless-replica", NodeId: &nodeId,
		Options: json.RawMessage(`{"listen_port":443}`),
		OutJson: json.RawMessage(`{"server":"node.example","server_port":443}`),
		Addrs:   json.RawMessage(addrs),
	}
}

// The address book arrives as whatever JSON the node's own panel stored, and a
// null entry in it decodes to a nil map -- which the backfill writes into.
//
// A nil map write is not a type assertion, so safeLinkGenerator's recover never
// sees it: it happens before that call, in a function reached from the node
// reconcile, which runs either on a cron tick or on a bare `go` from an API
// handler. Neither has a recover, so this takes the process down -- and the
// reconcile runs again on the next boot, so it keeps taking it down.
func TestGenNodeReplicaLinksSurvivesANullAddressRow(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	tests := []struct {
		addrs string
		want  []string
	}{
		// Nothing usable is left, which is the same position as an address
		// book that was empty to begin with: fall back to the node's own
		// server and port from the snapshot.
		{`[null]`, []string{"node.example:443"}},
		{`[null,{"server":"alt.example","server_port":8443}]`, []string{"alt.example:8443"}},
		{`[{"server":"alt.example","server_port":8443},null]`, []string{"alt.example:8443"}},
	}

	for _, tt := range tests {
		t.Run(tt.addrs, func(t *testing.T) {
			links := genNodeReplicaLinks(vlessReplica(tt.addrs), replicaClient())
			if len(links) != len(tt.want) {
				t.Fatalf("got %d links %v, want %d", len(links), links, len(tt.want))
			}
			for i, want := range tt.want {
				if !strings.Contains(links[i], want) {
					t.Errorf("link %d = %q, want it to point at %s", i, links[i], want)
				}
			}
		})
	}
}

// Issue #218: an address row with its own tls block (the shape the panel's
// address editor stores -- enabled + server_name only) used to replace the
// snapshot's tls outright, so a Reality inbound lost pbk/sid/fp and the master
// served security=tls. The row overrides field by field instead.
func TestGenNodeReplicaLinksKeepsRealityUnderAnAddressTlsOverride(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	nodeId := uint(7)
	replica := &model.Inbound{
		Type: "vless", Tag: "R-CN", NodeId: &nodeId,
		Options: json.RawMessage(`{"listen_port":8443}`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"R-CN","server":"127.0.0.1","server_port":8443,` +
			`"tls":{"enabled":true,"server_name":"www.cloudflare.com",` +
			`"reality":{"enabled":true,"public_key":"PBK","short_id":"SID"},` +
			`"utls":{"enabled":true,"fingerprint":"chrome"}}}`),
		Addrs: json.RawMessage(`[{"remark":"-01","server":"203.0.113.1","server_port":8443,` +
			`"tls":{"enabled":true,"server_name":"override.example"}}]`),
	}

	links := genNodeReplicaLinks(replica, replicaClient())
	if len(links) != 1 {
		t.Fatalf("got %d links %v, want 1", len(links), links)
	}
	for _, want := range []string{"security=reality", "pbk=PBK", "sid=SID", "fp=chrome", "sni=override.example", "203.0.113.1:8443"} {
		if !strings.Contains(links[0], want) {
			t.Errorf("link %q is missing %q", links[0], want)
		}
	}
}

// The counterpart that keeps the above honest: an address book with no nulls in
// it still produces one link per row.
func TestGenNodeReplicaLinksUsesEveryAddressRow(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	links := genNodeReplicaLinks(
		vlessReplica(`[{"server":"one.example"},{"server":"two.example","server_port":8443}]`),
		replicaClient())
	if len(links) != 2 {
		t.Fatalf("got %d links %v, want 2", len(links), links)
	}
	for _, want := range []string{"one.example", "two.example:8443"} {
		found := false
		for _, l := range links {
			if strings.Contains(l, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no link mentions %q: %v", want, links)
		}
	}
}
