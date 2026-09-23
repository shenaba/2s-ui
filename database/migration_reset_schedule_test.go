package database

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// main let a delay-start client without auto reset carry a plan length in
// reset_days. That feature is gone: afterwards a row without auto reset has no
// period and no delayed start, and the clients that lose a pending plan length
// are named in the log.
func TestMigrateResetSchedule(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "schedule.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	// InitDB already ran the migration once on an empty table; drop the flag so
	// the seeded rows below go through it.
	if err := db.Where("key = ?", migratedKeyResetSchedule).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("clear flag: %v", err)
	}

	const expiry = int64(1893456000)
	seed := func(c *model.Client) {
		t.Helper()
		c.Config = []byte(`{}`)
		c.Inbounds = []byte(`[]`)
		c.Links = []byte(`[]`)
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}
	for _, c := range []*model.Client{
		// Waiting for its first connection with a 30-day plan: loses it.
		{Name: "waiting-plan", DelayStart: true, AutoReset: false, ResetDays: 30},
		// main's stuck row: switching auto reset off zeroed reset_days and left
		// it delayed forever. No plan to lose, so not named.
		{Name: "stuck", DelayStart: true, AutoReset: false, ResetDays: 0},
		// Started on main: first use set the expiry and cleared delay_start but
		// left the plan length behind.
		{Name: "started-lifetime", DelayStart: false, AutoReset: false, ResetDays: 99999, Expiry: expiry},
		// Delay start with auto reset: the reset clock still waits for the
		// first connection. Untouched.
		{Name: "delayed-periodic", DelayStart: true, AutoReset: true, ResetDays: 7},
		{Name: "monthly", AutoReset: true, ResetDayOfMonth: 15},
		{Name: "periodic", AutoReset: true, ResetDays: 14},
	} {
		seed(c)
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	err := migrateResetSchedule()
	log.SetOutput(os.Stderr)
	if err != nil {
		t.Fatalf("migrateResetSchedule: %v", err)
	}

	type sched struct {
		delay, auto bool
		days, dom   int
		expiry      int64
	}
	want := map[string]sched{
		"waiting-plan":     {false, false, 0, 0, 0},
		"stuck":            {false, false, 0, 0, 0},
		"started-lifetime": {false, false, 0, 0, expiry},
		"delayed-periodic": {true, true, 7, 0, 0},
		"monthly":          {false, true, 0, 15, 0},
		"periodic":         {false, true, 14, 0, 0},
	}
	for name, w := range want {
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		got := sched{c.DelayStart, c.AutoReset, c.ResetDays, c.ResetDayOfMonth, c.Expiry}
		if got != w {
			t.Errorf("%s: got %+v, want %+v", name, got, w)
		}
	}

	logged := buf.String()
	if !strings.Contains(logged, "waiting-plan") {
		t.Errorf("the client losing its plan length is not named in the log: %q", logged)
	}
	for _, name := range []string{"stuck", "started-lifetime", "delayed-periodic"} {
		if strings.Contains(logged, name) {
			t.Errorf("%s lost nothing but is named in the log: %q", name, logged)
		}
	}

	// The panel calls this on every start; once flagged it must not run again.
	seed(&model.Client{Name: "after", DelayStart: true, AutoReset: false, ResetDays: 30})
	if err := migrateResetSchedule(); err != nil {
		t.Fatalf("second migrateResetSchedule: %v", err)
	}
	var after model.Client
	if err := db.Where("name = ?", "after").First(&after).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !after.DelayStart || after.ResetDays != 30 {
		t.Errorf("a second run touched a row: delay_start=%v reset_days=%d", after.DelayStart, after.ResetDays)
	}
}
