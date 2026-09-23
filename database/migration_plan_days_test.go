package database

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// The split is only safe if existing rows carry their plan length across. Every
// pre-split delay-start client stored it in reset_days.
func TestMigratePlanDays(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "plandays.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	// InitDB already ran the migration once on an empty table; drop the flag so
	// the seeded rows below go through it.
	if err := db.Where("key = ?", migratedKeyPlanDays).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("clear flag: %v", err)
	}

	rows := []*model.Client{
		// The one shape that carries a plan length.
		{Name: "plan", DelayStart: true, AutoReset: false, ResetDays: 30},
		// Delay start WITH auto reset: reset_days is a period here, and expiry
		// comes from the Expiry column. It must NOT gain a plan length: the
		// first-use step applies one whenever it is set, so copying the period
		// across would make a client that never had a time limit start expiring.
		{Name: "delayed-periodic", DelayStart: true, AutoReset: true, ResetDays: 7},
		// No delay start at all: plainly a period.
		{Name: "periodic", DelayStart: false, AutoReset: true, ResetDays: 14},
		// Zero is not a plan length; leaving it alone keeps a misconfigured row
		// visible rather than inventing a schedule for it.
		{Name: "zero", DelayStart: true, AutoReset: false, ResetDays: 0},
		// Set since the split: a re-run must not overwrite it with reset_days.
		{Name: "already-set", DelayStart: true, AutoReset: false, ResetDays: 30, PlanDays: 45},
		// Started on main: the old first-use step cleared delay_start but left
		// the plan length behind in reset_days. 99999 was a common way to
		// write "lifetime", and the drawer hides the field on such a row.
		{Name: "started-lifetime", DelayStart: false, AutoReset: false, ResetDays: 99999},
	}
	for _, c := range rows {
		c.Config = []byte(`{}`)
		c.Inbounds = []byte(`[]`)
		c.Links = []byte(`[]`)
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}

	if err := migratePlanDays(); err != nil {
		t.Fatalf("migratePlanDays: %v", err)
	}

	// A row that does not auto-reset has no period afterwards, whatever it
	// held: left in place, a plan length in reset_days read back as the legacy
	// request shape and a hidden 99999 failed validation on unrelated edits.
	// Rows that auto-reset keep theirs.
	want := map[string]struct{ plan, period int }{
		"plan":             {30, 0},
		"delayed-periodic": {0, 7},
		"periodic":         {0, 14},
		"zero":             {0, 0},
		"already-set":      {45, 0},
		"started-lifetime": {0, 0},
	}
	for name, w := range want {
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		if c.PlanDays != w.plan || c.ResetDays != w.period {
			t.Errorf("%s: plan_days=%d reset_days=%d, want %d/%d",
				name, c.PlanDays, c.ResetDays, w.plan, w.period)
		}
	}

	// Second run must be a no-op, since the panel calls this on every start.
	if err := migratePlanDays(); err != nil {
		t.Fatalf("second migratePlanDays: %v", err)
	}
	var again model.Client
	if err := db.Where("name = ?", "plan").First(&again).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if again.PlanDays != 30 {
		t.Errorf("after a second run: plan_days = %d, want 30", again.PlanDays)
	}
}
