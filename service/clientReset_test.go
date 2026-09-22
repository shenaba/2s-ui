package service

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func newResetDB(t *testing.T) *ClientService {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "reset.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return &ClientService{}
}

func seedClient(t *testing.T, c *model.Client) {
	t.Helper()
	if c.Config == nil {
		c.Config = json.RawMessage(`{}`)
	}
	if c.Inbounds == nil {
		c.Inbounds = json.RawMessage(`[]`)
	}
	if c.Links == nil {
		c.Links = json.RawMessage(`[]`)
	}
	if err := database.GetDB().Create(c).Error; err != nil {
		t.Fatalf("seed client %q: %v", c.Name, err)
	}
}

// reset_days is a period, and zero is not one. A client with auto_reset on and
// reset_days zero got NextReset = dt, which matches again on the very next tick
// -- so its counters were wiped every minute and its volume quota could never
// be reached.
func TestResetClientsIgnoresAZeroPeriod(t *testing.T) {
	svc := newResetDB(t)
	now := time.Now().Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "no-period",
		AutoReset: true, ResetDays: 0,
		NextReset: now - 60,
		Up:        500, Down: 500, Volume: 1000,
	})
	seedClient(t, &model.Client{
		Enable: true, Name: "weekly",
		AutoReset: true, ResetDays: 7,
		NextReset: now - 60,
		Up:        500, Down: 500, Volume: 1000,
	})

	db := database.GetDB()
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, now, time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()

	var zero, weekly model.Client
	if err := db.Where("name = ?", "no-period").First(&zero).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if err := db.Where("name = ?", "weekly").First(&weekly).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}

	if zero.Up != 500 || zero.Down != 500 {
		t.Errorf("a zero period must not reset the counters, got up=%d down=%d", zero.Up, zero.Down)
	}
	if weekly.Up != 0 || weekly.Down != 0 {
		t.Errorf("a real period must still reset, got up=%d down=%d", weekly.Up, weekly.Down)
	}
	if weekly.NextReset <= now {
		t.Errorf("next_reset = %d, want it moved past %d", weekly.NextReset, now)
	}
}

// A delay-start client with no period had its expiry set to the moment it sent
// its first byte, killing it outright.
func TestResetClientsDoesNotExpireAZeroPeriodDelayStart(t *testing.T) {
	svc := newResetDB(t)
	now := time.Now().Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "delayed",
		DelayStart: true, AutoReset: false, ResetDays: 0,
		Up: 1, Down: 1,
	})

	db := database.GetDB()
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, now, time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()

	var c model.Client
	if err := db.Where("name = ?", "delayed").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if c.Expiry != 0 {
		t.Errorf("expiry = %d, want it left unset rather than set to now", c.Expiry)
	}
	if !c.DelayStart {
		t.Error("delay_start must stay on: the row is misconfigured and should stay visible as such")
	}
}

// The Obj column is json.RawMessage. Built by concatenation, a name holding a
// quote or a backslash wrote invalid JSON into the changes log.
func TestChangeObjNameIsValidJSON(t *testing.T) {
	for _, name := range []string{
		"plain",
		`quo"ted`,
		`back\slash`,
		"new\nline",
		"表情 🙂",
		`{"not":"an object"}`,
		"",
	} {
		raw := changeObjName(name)
		var got string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("name %q encoded to invalid JSON %s: %v", name, raw, err)
			continue
		}
		if got != name {
			t.Errorf("name %q round-tripped to %q", name, got)
		}
	}
}
