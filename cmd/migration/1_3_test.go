package migration

import (
	"encoding/json"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// The condition used to be chained after Find, where it never reached the
// query: every outbound was loaded and rewritten, not only those with the
// field. An outbound without it must come out byte for byte as it went in.
func TestRemoveOutboundStrategyTouchesOnlyOutboundsWithTheField(t *testing.T) {
	db := newPortMigrationDB(t)

	seed := []model.Outbound{
		{Type: "direct", Tag: "with", Options: json.RawMessage(`{"domain_strategy":"ipv4_only","server":"a"}`)},
		// 8 bytes, the length "{" claims as a JSONB header: read as a BLOB
		// without the CAST it would fail the whole query.
		{Type: "direct", Tag: "eight", Options: json.RawMessage(`{"a":12}`)},
		{Type: "direct", Tag: "without", Options: json.RawMessage(`{"server":"b","server_port":443}`)},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", seed[i].Tag, err)
		}
	}

	if err := remove_outbound_strategy(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	with := rawColumn(t, db, "SELECT CAST(options AS TEXT) FROM outbounds WHERE tag = ?", "with")
	if _, ok := with["domain_strategy"]; ok {
		t.Errorf("domain_strategy left on the outbound that had it: %v", with)
	}
	if with["server"] != "a" {
		t.Errorf("the rest of the options must survive, got %v", with)
	}

	for _, outbound := range seed[1:] {
		var raw string
		if err := db.Raw("SELECT CAST(options AS TEXT) FROM outbounds WHERE tag = ?", outbound.Tag).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if raw != string(outbound.Options) {
			t.Errorf("%s was rewritten without the field: %q, was %q", outbound.Tag, raw, outbound.Options)
		}
	}
}
