package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/jsonsql"
	"github.com/shenaba/2s-ui/database/model"
)

// A client whose config or inbound list is not JSON is left out of every
// query over those columns instead of failing it, and named at start. These
// are the query shapes the services run, on the real driver: the statement
// used to fail as a whole, "malformed JSON", and config generation with it.
func TestUnreadableClientRowsAreSkippedAndReported(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "unreadable.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	const vless = `{"vless":{"name":"n","uuid":"u"},"mieru":{"name":"n","password":"p"}}`
	for _, c := range []model.Client{
		{Name: "good", Config: []byte(vless), Inbounds: []byte(`[1,2]`)},
		{Name: "bad-config", Config: []byte(`{"vless":`), Inbounds: []byte(`[1]`)},
		{Name: "bad-inbounds", Config: []byte(vless), Inbounds: []byte(`[1,`)},
		{Name: "blank", Config: []byte(vless), Inbounds: []byte(``)},
	} {
		c.Enable = true
		c.Links = []byte(`[]`)
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}

	names := func(what, query string, args ...any) string {
		t.Helper()
		var got []string
		if err := db.Raw(query, args...).Scan(&got).Error; err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return strings.Join(got, ",")
	}

	// Who is attached to inbound 1: a join over the list, and a membership
	// test on it.
	attached := names("join", "SELECT clients.name FROM clients, "+jsonsql.Each("clients.inbounds")+
		" AS je WHERE je.value = ? ORDER BY clients.id", 1)
	if attached != "good,bad-config" {
		t.Errorf("attached to inbound 1: %s, want good,bad-config", attached)
	}
	members := names("membership", "SELECT name FROM clients WHERE ? IN (SELECT je.value FROM "+
		jsonsql.Each("clients.inbounds")+" AS je) ORDER BY id", 1)
	if members != attached {
		t.Errorf("membership test found %s, the join %s", members, attached)
	}

	// Who has credentials for a protocol.
	column := jsonsql.Extract("clients.config", "$.vless")
	withVless := names("extract", "SELECT name FROM clients WHERE "+column+" IS NOT NULL ORDER BY id")
	if withVless != "good,bad-inbounds,blank" {
		t.Errorf("clients with a vless block: %s, want good,bad-inbounds,blank", withVless)
	}

	problems := unreadableClients(db)
	if len(problems) != 2 ||
		!strings.Contains(problems[0], `config of client "bad-config"`) ||
		!strings.Contains(problems[1], `inbounds of client "bad-inbounds"`) {
		t.Errorf("reported %q, want bad-config's config and bad-inbounds's inbound list", problems)
	}
}
