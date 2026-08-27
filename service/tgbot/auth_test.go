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

// roleOf is the only thing standing between a stranger who found the bot token
// and the panel. Every case below is a way that could go wrong.
func TestRoleOf(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "auth.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()

	if err := db.Create(&model.Setting{Key: "notifyEnable", Value: "true"}).Error; err != nil {
		t.Fatalf("seed notifyEnable: %v", err)
	}
	if err := db.Create(&model.Setting{Key: "notifyTgChatId", Value: "111, 222"}).Error; err != nil {
		t.Fatalf("seed admins: %v", err)
	}

	seed := func(name string, tgID int64, enable bool) {
		t.Helper()
		c := model.Client{
			Name: name, Enable: enable, TgId: tgID,
			Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		}
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("alice", 900, true)
	seed("bob", 901, false) // disabled
	seed("carol", 0, true)  // never bound
	seed("dave", 111, true) // also an admin id

	cases := []struct {
		name     string
		chatID   int64
		wantRole role
		wantName string
	}{
		{"admin from the list", 111, roleAdmin, ""},
		{"second admin, whitespace in the setting", 222, roleAdmin, ""},
		{"bound client", 900, roleClient, "alice"},
		{"stranger", 555, roleNone, ""},
		// A disabled client cannot query itself: the account is off, and the
		// answer would be the operator's business to give.
		{"bound but disabled", 901, roleNone, ""},
		// The decisive one. tg_id defaults to 0, so a zero chat id must not
		// match every unbound row and hand out somebody else's usage.
		{"zero chat id", 0, roleNone, ""},
		// Admin wins, so an operator who also has a client does not get
		// demoted to the read-only view.
		{"admin who is also a client", 111, roleAdmin, ""},
	}

	for _, c := range cases {
		gotRole, gotName := roleOf(c.chatID)
		if gotRole != c.wantRole || gotName != c.wantName {
			t.Errorf("%s: roleOf(%d) = (%v, %q), want (%v, %q)",
				c.name, c.chatID, gotRole, gotName, c.wantRole, c.wantName)
		}
	}

	// Emptying the admin list must actually revoke, not merely stop granting.
	if err := db.Model(model.Setting{}).Where("key = ?", "notifyTgChatId").
		Update("value", "").Error; err != nil {
		t.Fatalf("clear admins: %v", err)
	}
	if r, _ := roleOf(222); r != roleNone {
		t.Errorf("a removed admin still resolves as %v", r)
	}
	// The bound client is unaffected by the admin list.
	if r, n := roleOf(900); r != roleClient || n != "alice" {
		t.Errorf("clearing the admin list broke client binding: (%v, %q)", r, n)
	}
}
