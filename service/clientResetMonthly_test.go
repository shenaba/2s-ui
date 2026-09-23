package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func at(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestNextMonthlyReset(t *testing.T) {
	cases := []struct {
		name string
		from time.Time
		dom  int
		want time.Time
	}{
		{"later this month", at(2026, time.September, 22, 14, 37), 25, at(2026, time.September, 25, 0, 0)},
		{"next month", at(2026, time.September, 22, 14, 37), 1, at(2026, time.October, 1, 0, 0)},
		// Strictly after: today's boundary is already behind us, so the answer
		// is next month, not a midnight that has passed.
		{"today's midnight has passed", at(2026, time.September, 22, 0, 1), 22, at(2026, time.October, 22, 0, 0)},
		{"31 clamps to a 30-day month", at(2026, time.September, 22, 14, 37), 31, at(2026, time.September, 30, 0, 0)},
		// The one that decides the whole design: the clamped date must not
		// become the anchor, or every later boundary would be the 30th.
		{"31 returns after a clamp", at(2026, time.September, 30, 0, 0), 31, at(2026, time.October, 31, 0, 0)},
		{"31 in February, common year", at(2026, time.February, 1, 0, 0), 31, at(2026, time.February, 28, 0, 0)},
		{"31 in February, leap year", at(2028, time.February, 1, 0, 0), 31, at(2028, time.February, 29, 0, 0)},
		{"30 in February", at(2026, time.February, 1, 0, 0), 30, at(2026, time.February, 28, 0, 0)},
		{"year rolls over", at(2026, time.December, 20, 9, 0), 5, at(2027, time.January, 5, 0, 0)},
		{"december 31 to january 31", at(2026, time.December, 31, 0, 0), 31, at(2027, time.January, 31, 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextMonthlyReset(c.from, c.dom)
			if !got.Equal(c.want) {
				t.Errorf("nextMonthlyReset(%s, %d) = %s, want %s",
					c.from.Format(time.RFC3339), c.dom,
					got.Format(time.RFC3339), c.want.Format(time.RFC3339))
			}
			if !got.After(c.from) {
				t.Errorf("boundary %s is not after %s", got, c.from)
			}
		})
	}
}

// Why the day of the month is stored and clamped instead of being handed to the
// cron parser as `0 0 31 * *`: cron matches the day exactly, so that spec fires
// in seven months of the year and leaves the other five with no reset at all.
func TestNextMonthlyResetHitsEveryMonth(t *testing.T) {
	cur := at(2026, time.January, 1, 0, 0)
	seen := map[time.Month]bool{}
	for i := 0; i < 12; i++ {
		cur = nextMonthlyReset(cur, 31)
		if seen[cur.Month()] {
			t.Fatalf("month %s reached twice; boundaries are not monthly", cur.Month())
		}
		seen[cur.Month()] = true
	}
	if len(seen) != 12 {
		t.Errorf("covered %d months in 12 boundaries, want 12", len(seen))
	}
	if cur.Year() != 2026 || cur.Month() != time.December {
		t.Errorf("after twelve boundaries: %s, want December 2026", cur.Format("2006-01"))
	}
}

// A monthly client carries reset_days = 0, which the zero-period guard used to
// read as "misconfigured, leave alone".
func TestResetClientsMonthlyDay(t *testing.T) {
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "monthly",
		AutoReset: true, ResetDays: 0, ResetDayOfMonth: 1,
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

	var c model.Client
	if err := db.Where("name = ?", "monthly").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if c.Up != 0 || c.Down != 0 {
		t.Errorf("counters not cleared: up=%d down=%d", c.Up, c.Down)
	}
	if c.TotalUp != 500 || c.TotalDown != 500 {
		t.Errorf("totals not carried: totalUp=%d totalDown=%d", c.TotalUp, c.TotalDown)
	}
	want := at(2026, time.October, 1, 0, 0).Unix()
	if c.NextReset != want {
		t.Errorf("next_reset = %s, want %s",
			time.Unix(c.NextReset, 0).UTC().Format(time.RFC3339),
			time.Unix(want, 0).UTC().Format(time.RFC3339))
	}
}

// A delayed start gets its first boundary from its first bytes, and that
// boundary has to honour the day of the month too.
func TestResetClientsMonthlyDayWithDelayStart(t *testing.T) {
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()
	expiry := at(2027, time.March, 1, 0, 0).Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "delayed-monthly",
		DelayStart: true, AutoReset: true, ResetDays: 0, ResetDayOfMonth: 15,
		Expiry: expiry,
		Up:     1, Down: 1,
	})

	db := database.GetDB()
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, now, time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()

	var c model.Client
	if err := db.Where("name = ?", "delayed-monthly").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if c.DelayStart {
		t.Error("delay_start should be cleared once the client has traffic")
	}
	// Delay start only holds the reset clock; the expiry applies as set.
	if c.Expiry != expiry {
		t.Errorf("expiry = %d, want the one set, %d", c.Expiry, expiry)
	}
	want := at(2026, time.October, 15, 0, 0).Unix()
	if c.NextReset != want {
		t.Errorf("next_reset = %s, want %s",
			time.Unix(c.NextReset, 0).UTC().Format(time.RFC3339),
			time.Unix(want, 0).UTC().Format(time.RFC3339))
	}
}

// No delay-start row may stay delayed once traffic arrives, whatever else it
// carries. On main a delay-start client whose auto reset was switched off
// matched neither first-use block and stayed delayed forever; the write paths
// no longer produce such rows, but the property is asserted over every shape
// the job could still meet, rows written before an upgrade included.
func TestResetClientsNoRowStaysDelayed(t *testing.T) {
	shapes := []model.Client{
		{Name: "nothing-at-all"},
		{Name: "auto-no-period", AutoReset: true},
		{Name: "auto-days", AutoReset: true, ResetDays: 30},
		{Name: "auto-monthly", AutoReset: true, ResetDayOfMonth: 15},
		// main's plan-length shape, should one reach the job unmigrated.
		{Name: "main-plan-length", ResetDays: 30},
	}
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()
	for i := range shapes {
		c := shapes[i]
		c.Enable, c.DelayStart, c.Up, c.Down = true, true, 1, 1
		seedClient(t, &c)
	}

	db := database.GetDB()
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, now, time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()

	for _, shape := range shapes {
		var c model.Client
		if err := db.Where("name = ?", shape.Name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", shape.Name, err)
		}
		if c.DelayStart {
			t.Errorf("%s: still delayed after first use", shape.Name)
		}
		// First use writes no expiry, now that the plan length is gone.
		if c.Expiry != 0 {
			t.Errorf("%s: first use wrote expiry %d", shape.Name, c.Expiry)
		}
	}
}

func TestAlignNextReset(t *testing.T) {
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()
	db := database.GetDB()

	t.Run("new monthly client gets a boundary immediately", func(t *testing.T) {
		c := &model.Client{Name: "fresh", AutoReset: true, ResetDayOfMonth: 5}
		if err := svc.alignNextReset(db, c, true, now, time.UTC); err != nil {
			t.Fatalf("alignNextReset: %v", err)
		}
		if want := at(2026, time.October, 5, 0, 0).Unix(); c.NextReset != want {
			t.Errorf("next_reset = %d, want %d", c.NextReset, want)
		}
	})

	t.Run("auto reset off clears the boundary", func(t *testing.T) {
		c := &model.Client{Name: "off", AutoReset: false, NextReset: now - 86400}
		if err := svc.alignNextReset(db, c, true, now, time.UTC); err != nil {
			t.Fatalf("alignNextReset: %v", err)
		}
		// A boundary left in the past would clear this client's counters on the
		// first tick after auto-reset is switched back on, however much later.
		if c.NextReset != 0 {
			t.Errorf("next_reset = %d, want 0", c.NextReset)
		}
	})

	t.Run("delay start has no boundary yet", func(t *testing.T) {
		c := &model.Client{Name: "later", AutoReset: true, DelayStart: true, ResetDayOfMonth: 5}
		if err := svc.alignNextReset(db, c, true, now, time.UTC); err != nil {
			t.Fatalf("alignNextReset: %v", err)
		}
		if c.NextReset != 0 {
			t.Errorf("next_reset = %d, want 0", c.NextReset)
		}
	})

	t.Run("a changed day of month moves the boundary", func(t *testing.T) {
		stored := &model.Client{
			Name: "moved", AutoReset: true, ResetDayOfMonth: 1,
			NextReset: at(2026, time.October, 1, 0, 0).Unix(),
		}
		seedClient(t, stored)
		edited := *stored
		edited.ResetDayOfMonth = 20
		if err := svc.alignNextReset(db, &edited, false, now, time.UTC); err != nil {
			t.Fatalf("alignNextReset: %v", err)
		}
		// Without this the 20th would only take effect after the 1st came and
		// went -- the new schedule would start a month late.
		if want := at(2026, time.October, 20, 0, 0).Unix(); edited.NextReset != want {
			t.Errorf("next_reset = %d, want %d", edited.NextReset, want)
		}
	})

	t.Run("a changed period keeps the boundary it arrives with", func(t *testing.T) {
		stored := &model.Client{
			Name: "shifted", AutoReset: true, ResetDays: 30,
			NextReset: at(2026, time.October, 1, 0, 0).Unix(),
		}
		seedClient(t, stored)
		edited := *stored
		edited.ResetDays = 60
		// Shifted by the difference, as normalizeResetSchedule does for a
		// request without nextReset (or as the caller sent it); recomputing
		// here would overrule either.
		edited.NextReset = at(2026, time.October, 31, 0, 0).Unix()
		if err := svc.alignNextReset(db, &edited, false, now, time.UTC); err != nil {
			t.Fatalf("alignNextReset: %v", err)
		}
		if want := at(2026, time.October, 31, 0, 0).Unix(); edited.NextReset != want {
			t.Errorf("next_reset = %d, want the submitted %d", edited.NextReset, want)
		}
	})
}

// The per-client save range-checks the day too, or an apiv2 caller could store
// one the drawer cannot represent -- its input carries max=31 while the field
// would display whatever was written.
func TestSaveRejectsOutOfRangeResetDay(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()

	for _, c := range []struct {
		name string
		dom  int
		want bool // want an error
	}{
		{"200", 200, true},
		{"negative", -1, true},
		{"31 is fine", 31, false},
		{"0 means the N-day mode", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]interface{}{
				"name": "probe-" + c.name, "enable": true, "config": map[string]interface{}{},
				"inbounds": []uint{}, "links": []interface{}{},
				"autoReset": true, "resetDays": 30, "resetDayOfMonth": c.dom,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			tx := db.Begin()
			_, err = svc.Save(tx, "new", payload, "")
			tx.Rollback()
			if c.want && err == nil {
				t.Errorf("dom=%d: want an error, got none", c.dom)
			}
			if !c.want && err != nil {
				t.Errorf("dom=%d: unexpected error: %v", c.dom, err)
			}
		})
	}
}

// Auto reset owns the schedule, so a client saved without it has no period and
// no delayed start. main's plan-length shape -- delay start without auto reset,
// the length in resetDays -- is exactly that request, and the feature it asked
// for is gone: it is stored as a client with no time limit. With auto reset on,
// the same fields mean what they always did.
func TestSaveWithoutAutoResetHasNoSchedule(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()

	save := func(name string, body map[string]interface{}) model.Client {
		t.Helper()
		body["name"], body["enable"] = name, true
		body["config"], body["inbounds"], body["links"] = map[string]interface{}{}, []uint{}, []interface{}{}
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		tx := db.Begin()
		if _, err := svc.Save(tx, "new", payload, ""); err != nil {
			tx.Rollback()
			t.Fatalf("Save %s: %v", name, err)
		}
		tx.Commit()
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		return c
	}

	if c := save("main-plan-length", map[string]interface{}{
		"delayStart": true, "autoReset": false, "resetDays": 30,
	}); c.DelayStart || c.ResetDays != 0 || c.Expiry != 0 {
		t.Errorf("main-plan-length: delayStart=%v resetDays=%d expiry=%d, want false/0/0",
			c.DelayStart, c.ResetDays, c.Expiry)
	}
	if c := save("delayed-periodic", map[string]interface{}{
		"delayStart": true, "autoReset": true, "resetDays": 30,
	}); !c.DelayStart || c.ResetDays != 30 {
		t.Errorf("delayed-periodic: delayStart=%v resetDays=%d, want true/30", c.DelayStart, c.ResetDays)
	}
}

// Delay start only holds the reset clock, so an expiry saved with it applies as
// set. It used to be cleared whenever a plan length was sent, because the plan
// length decided the expiry at first use; a planDays key is ignored now.
func TestSaveKeepsTheExpiryWithDelayStart(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	expiry := time.Now().Unix() + 60*86400
	payload, err := json.Marshal(map[string]interface{}{
		"name": "c", "enable": true, "config": map[string]interface{}{},
		"inbounds": []uint{}, "links": []interface{}{},
		"delayStart": true, "autoReset": true, "resetDays": 30,
		"planDays": 30, "expiry": expiry,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "new", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save: %v", err)
	}
	tx.Commit()
	var got model.Client
	if err := db.Where("name = ?", "c").First(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Expiry != expiry {
		t.Errorf("expiry = %d, want the one sent, %d", got.Expiry, expiry)
	}
}

// No period and no delayed start without auto reset, on every write, rows left
// by main included. A client that started on main keeps its old plan length in
// reset_days, the drawer hides the field with both toggles off, and the whole
// row goes back on save -- so a leftover 99999 failed validation on an edit of
// the description. And main's stuck row -- delay start whose auto reset was
// switched off -- must come out of an edit as a plain client.
func TestSaveClearsTheScheduleWithoutAutoReset(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	expiry := time.Now().Unix() + 99999*86400
	seedClient(t, &model.Client{Name: "started-lifetime", Enable: true, ResetDays: 99999, Expiry: expiry})
	seedClient(t, &model.Client{Name: "stuck", Enable: true, DelayStart: true})

	for _, name := range []string{"started-lifetime", "stuck"} {
		var row model.Client
		if err := db.Where("name = ?", name).First(&row).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		row.Desc = "only the description changed"
		payload, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		tx := db.Begin()
		if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
			tx.Rollback()
			t.Fatalf("%s: an edit that never touched the schedule failed: %v", name, err)
		}
		tx.Commit()
		if err := db.Where("name = ?", name).First(&row).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		if row.DelayStart || row.ResetDays != 0 || row.ResetDayOfMonth != 0 {
			t.Errorf("%s: schedule survived without auto reset: delayStart=%v resetDays=%d dom=%d",
				name, row.DelayStart, row.ResetDays, row.ResetDayOfMonth)
		}
	}
	// The expiry the client started with is its time limit; it must survive.
	var row model.Client
	if err := db.Where("name = ?", "started-lifetime").First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.Expiry != expiry {
		t.Errorf("expiry = %d, want %d", row.Expiry, expiry)
	}
}

// The cap exists for the arithmetic, not as policy: a period far longer than
// anyone needs must still save.
func TestSaveAcceptsLongPeriods(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	for _, c := range []struct {
		name string
		body map[string]interface{}
		ok   bool
	}{
		{"period-99999", map[string]interface{}{"autoReset": true, "resetDays": 99999}, true},
		{"period-too-far", map[string]interface{}{"autoReset": true, "resetDays": 20_000_000}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.body["name"], c.body["enable"] = c.name, true
			c.body["config"], c.body["inbounds"], c.body["links"] = map[string]interface{}{}, []uint{}, []interface{}{}
			payload, err := json.Marshal(c.body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			tx := db.Begin()
			_, err = svc.Save(tx, "new", payload, "")
			tx.Rollback()
			if c.ok && err != nil {
				t.Errorf("rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("accepted a value past the bound")
			}
		})
	}
}

// The bulk action keeps the same invariants as a single save: no period and no
// delayed start without auto reset.
func TestSaveResetPolicyClearsTheScheduleWhenOff(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "a", Enable: true, DelayStart: true, AutoReset: true, ResetDays: 30})
	var c model.Client
	if err := db.Where("name = ?", "a").First(&c).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	// An apiv2 caller switching auto reset off but still sending a period.
	payload, err := json.Marshal(map[string]interface{}{
		"ids": []uint{c.Id}, "autoReset": false, "resetDays": 30, "resetDayOfMonth": 15,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "resetpolicy", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save resetpolicy: %v", err)
	}
	tx.Commit()
	if err := db.Where("name = ?", "a").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if c.AutoReset || c.DelayStart || c.ResetDays != 0 || c.ResetDayOfMonth != 0 {
		t.Errorf("autoReset=%v delayStart=%v resetDays=%d dom=%d, want false/false/0/0",
			c.AutoReset, c.DelayStart, c.ResetDays, c.ResetDayOfMonth)
	}
}

// The bulk schedule change exists because editbulk structurally cannot make it:
// findInboundsChanges restores every reset column from the stored row.
func TestSaveResetPolicy(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()

	seedClient(t, &model.Client{Name: "a", Enable: true, AutoReset: true, ResetDays: 30})
	seedClient(t, &model.Client{Name: "b", Enable: true, AutoReset: false})
	seedClient(t, &model.Client{Name: "c", Enable: true, DelayStart: true, AutoReset: true, ResetDays: 30})
	seedClient(t, &model.Client{Name: "untouched", Enable: true, AutoReset: true, ResetDays: 7})

	var ids []uint
	for _, name := range []string{"a", "b", "c"} {
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		ids = append(ids, c.Id)
	}

	payload, err := json.Marshal(map[string]interface{}{
		"ids": ids, "autoReset": true, "resetDays": 0, "resetDayOfMonth": 15,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	tx := db.Begin()
	write, err := svc.Save(tx, "resetpolicy", payload, "")
	if err != nil {
		tx.Rollback()
		t.Fatalf("Save resetpolicy: %v", err)
	}
	tx.Commit()

	if len(write.Ids) != len(ids) {
		t.Errorf("wrote %d ids, want %d", len(write.Ids), len(ids))
	}
	// A schedule change moves nobody in or out of an inbound's user table.
	if len(write.InboundIds) != 0 {
		t.Errorf("InboundIds = %v, want none", write.InboundIds)
	}

	for _, name := range []string{"a", "b", "c"} {
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		// reset_days now means one thing on every row -- the period -- so the
		// monthly schedule empties it uniformly, delay-start or not.
		if !c.AutoReset || c.ResetDayOfMonth != 15 || c.ResetDays != 0 {
			t.Errorf("%s: autoReset=%v resetDays=%d dom=%d, want true/0/15",
				name, c.AutoReset, c.ResetDays, c.ResetDayOfMonth)
		}
		// Timezone-independent: the panel's configured location decides the
		// exact instant, so only the shape is asserted here.
		if name == "c" {
			if c.NextReset != 0 {
				t.Errorf("c: next_reset = %d, want 0 while delay_start is on", c.NextReset)
			}
		} else if c.NextReset <= time.Now().Unix() {
			t.Errorf("%s: next_reset = %d, want a future boundary", name, c.NextReset)
		}
	}

	var other model.Client
	if err := db.Where("name = ?", "untouched").First(&other).Error; err != nil {
		t.Fatalf("read back untouched: %v", err)
	}
	if other.ResetDays != 7 || other.ResetDayOfMonth != 0 {
		t.Errorf("a client outside the selection was changed: resetDays=%d dom=%d",
			other.ResetDays, other.ResetDayOfMonth)
	}
}

func TestSaveResetPolicyRejectsIdsThatMatchNothing(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "a", Enable: true})

	payload, err := json.Marshal(map[string]interface{}{
		"ids": []uint{9001, 9002}, "autoReset": true, "resetDayOfMonth": 15,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	_, err = svc.Save(tx, "resetpolicy", payload, "")
	tx.Rollback()
	if err == nil {
		t.Error("want an error when no id matches, got success")
	}
}

func TestSaveResetPolicyRejectsBadInput(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "a", Enable: true})

	for _, c := range []struct {
		name    string
		payload map[string]interface{}
	}{
		{"no selection", map[string]interface{}{"ids": []uint{}, "resetDayOfMonth": 1}},
		{"day above 31", map[string]interface{}{"ids": []uint{1}, "resetDayOfMonth": 32}},
		{"negative day", map[string]interface{}{"ids": []uint{1}, "resetDayOfMonth": -1}},
		{"negative period", map[string]interface{}{"ids": []uint{1}, "resetDays": -5}},
		// Clearing the number input sends 0 with auto reset still on. Stored,
		// ResetClients would skip the row -- a bulk request answered with a
		// success toast that changed nothing.
		{"auto reset with no period at all", map[string]interface{}{
			"ids": []uint{1}, "autoReset": true, "resetDays": 0, "resetDayOfMonth": 0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			payload, err := json.Marshal(c.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			tx := db.Begin()
			_, err = svc.Save(tx, "resetpolicy", payload, "")
			tx.Rollback()
			if err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}
