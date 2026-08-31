package tgbot

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// One Telegram id has to resolve to exactly one client, because roleOf takes
// the first matching row: a second binding does not give that person a second
// account, it makes one of the two invisible to them.
func TestClientBoundTo(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "bind.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	seed := func(name string, tgID int64, enable bool) {
		t.Helper()
		c := model.Client{
			Name: name, Enable: enable, TgId: tgID,
			Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		}
		if err := database.GetDB().Create(&c).Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("alice", 900, true)
	seed("bob", 0, true)
	// Disabled still counts: re-enabling it would bring the collision back
	// rather than resolve it, and roleOf resolves disabled clients anyway.
	seed("carol", 901, false)

	cases := []struct {
		name    string
		tgID    int64
		exclude string
		want    string
	}{
		{"a free id", 555, "bob", ""},
		{"an id another client holds", 900, "bob", "alice"},
		// Re-confirming the id a client already has is not a conflict with
		// itself, or nobody could ever re-run their own binding.
		{"the client's own id", 900, "alice", ""},
		{"a disabled client still holds its id", 901, "bob", "carol"},
		// 0 is the column default that every never-bound client carries, so a
		// lookup for it would match all of them. Unbinding is the one call that
		// passes 0 and it can never collide, so the helper has to answer "free"
		// rather than name the first unbound row it happens to find.
		{"zero is never a conflict", 0, "alice", ""},
	}

	for _, c := range cases {
		got, err := clientBoundTo(c.tgID, c.exclude)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: clientBoundTo(%d, %q) = %q, want %q", c.name, c.tgID, c.exclude, got, c.want)
		}
	}
}
