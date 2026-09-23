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

	seedClient(t, &model.Client{
		Enable: true, Name: "delayed-monthly",
		DelayStart: true, AutoReset: true, ResetDays: 0, ResetDayOfMonth: 15,
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
	if err := db.Where("name = ?", "delayed-monthly").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if c.DelayStart {
		t.Error("delay_start should be cleared once the client has traffic")
	}
	// No plan length: the reset clock starts, the expiry is left as set.
	if c.Expiry != 0 {
		t.Errorf("expiry = %d, want 0 -- a client with no plan length has no time limit", c.Expiry)
	}
	want := at(2026, time.October, 15, 0, 0).Unix()
	if c.NextReset != want {
		t.Errorf("next_reset = %s, want %s",
			time.Unix(c.NextReset, 0).UTC().Format(time.RFC3339),
			time.Unix(want, 0).UTC().Format(time.RFC3339))
	}
}

// Delay start and auto reset are two independent clocks, and both start on
// first use. This is the combination the old two-block version could not
// express: with auto reset on, the plan length was ignored, so a "90 days from
// first use, traffic resets on the 15th" client never expired.
func TestResetClientsDelayStartRunsBothClocks(t *testing.T) {
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "both",
		DelayStart: true, PlanDays: 90,
		AutoReset: true, ResetDayOfMonth: 15,
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
	if err := db.Where("name = ?", "both").First(&c).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := now + 90*86400; c.Expiry != want {
		t.Errorf("expiry = %d, want %d (90 days from first use)", c.Expiry, want)
	}
	if want := at(2026, time.October, 15, 0, 0).Unix(); c.NextReset != want {
		t.Errorf("next_reset = %d, want %d (the next 15th)", c.NextReset, want)
	}
	if c.DelayStart {
		t.Error("delay_start should clear on first use")
	}
}

// No delay-start row may stay delayed once traffic arrives, whatever else it
// carries. Each of these shapes used to be able to match neither first-use
// block -- a fifth review round found two more ways in after four rounds of
// closing them one by one -- so the property is asserted over all of them at
// once instead of per path.
func TestResetClientsNoRowStaysDelayed(t *testing.T) {
	shapes := []model.Client{
		{Name: "plan-only", PlanDays: 30},
		{Name: "nothing-at-all"},
		{Name: "auto-no-period", AutoReset: true},
		{Name: "auto-days", AutoReset: true, ResetDays: 30},
		{Name: "auto-monthly", AutoReset: true, ResetDayOfMonth: 15},
		{Name: "plan-and-auto", PlanDays: 30, AutoReset: true, ResetDays: 7},
		// The fifth round's shape: a pre-upgrade delay+auto client (no plan
		// length after the migration) whose auto reset was switched off.
		{Name: "was-auto-now-off"},
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
		// And never the failure the old guard existed for: an expiry of
		// right now, killing the client on its first byte.
		if c.Expiry == now {
			t.Errorf("%s: expiry set to the moment of first use", shape.Name)
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

// The request contract moved the plan length from resetDays to planDays. The
// old shape is what the panel itself sent before the split -- so it is what
// apiv2 integrations copied, and what a tab left open across the upgrade keeps
// sending -- and stored untranslated it has no plan length: the client never
// expires.
func TestSaveDecodesTheLegacyPlanLength(t *testing.T) {
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

	// Old contract, delay start without auto reset: resetDays was the plan.
	if c := save("legacy", map[string]interface{}{
		"delayStart": true, "autoReset": false, "resetDays": 30,
	}); c.PlanDays != 30 || c.ResetDays != 0 {
		t.Errorf("legacy: planDays=%d resetDays=%d, want 30/0", c.PlanDays, c.ResetDays)
	}
	// Old contract with auto reset: resetDays was the period then too, so the
	// shape already means the same thing and must not be touched.
	if c := save("legacy-auto", map[string]interface{}{
		"delayStart": true, "autoReset": true, "resetDays": 30,
	}); c.PlanDays != 0 || c.ResetDays != 30 {
		t.Errorf("legacy-auto: planDays=%d resetDays=%d, want 0/30", c.PlanDays, c.ResetDays)
	}
	// New contract: a plan length is present, nothing to translate.
	if c := save("current", map[string]interface{}{
		"delayStart": true, "autoReset": false, "planDays": 45, "resetDays": 0,
	}); c.PlanDays != 45 {
		t.Errorf("current: planDays=%d, want 45", c.PlanDays)
	}
}

// Under a plan length the expiry is decided at first use; an absolute one left
// from before would get the client disabled before it ever connected. With no
// plan length the absolute expiry is the only time limit there is, and stays.
func TestSaveClearsExpiryOnlyUnderAPlanLength(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	past := time.Now().Unix() - 86400

	for _, c := range []struct {
		name       string
		planDays   int
		wantExpiry int64
	}{
		{"with-plan", 30, 0},
		{"no-plan", 0, past},
	} {
		payload, err := json.Marshal(map[string]interface{}{
			"name": c.name, "enable": true, "config": map[string]interface{}{},
			"inbounds": []uint{}, "links": []interface{}{},
			"delayStart": true, "autoReset": true, "resetDays": 30,
			"planDays": c.planDays, "expiry": past,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		tx := db.Begin()
		if _, err := svc.Save(tx, "new", payload, ""); err != nil {
			tx.Rollback()
			t.Fatalf("Save %s: %v", c.name, err)
		}
		tx.Commit()
		var got model.Client
		if err := db.Where("name = ?", c.name).First(&got).Error; err != nil {
			t.Fatalf("read back %s: %v", c.name, err)
		}
		if got.Expiry != c.wantExpiry {
			t.Errorf("%s: expiry = %d, want %d", c.name, got.Expiry, c.wantExpiry)
		}
	}
}

// Delay start no longer comes with a plan length pre-filled, so on the save side
// the question is only what an explicit 0 does: it must stay 0. A migrated
// client could not be set to "no time limit" while its old plan length sat in
// reset_days, because the save boundary read that back as the legacy shape.
func TestSaveLetsAMigratedClientGoUnlimited(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	// A delay-start client that has not started, with the old plan length still
	// in reset_days as well -- what the migration left before it cleared it.
	// The explicit 0 below must win even then: the request sends planDays, so it
	// is not the legacy shape, whatever reset_days holds.
	seedClient(t, &model.Client{Name: "migrated", Enable: true,
		DelayStart: true, AutoReset: false, PlanDays: 30, ResetDays: 30})

	var row model.Client
	if err := db.Where("name = ?", "migrated").First(&row).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	row.PlanDays = 0
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save: %v", err)
	}
	tx.Commit()
	if err := db.Where("name = ?", "migrated").First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.PlanDays != 0 {
		t.Errorf("planDays = %d after saving 0, want 0", row.PlanDays)
	}
}

// No period without auto reset, on every write. The row that made this matter:
// a client that started on main keeps its plan length in reset_days, the
// drawer hides the field with both toggles off, and the whole row goes back on
// save -- so a leftover 99999 failed validation on an edit of the description.
func TestSaveClearsThePeriodWithoutAutoReset(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "started-lifetime", Enable: true,
		ResetDays: 99999, Expiry: time.Now().Unix() + 99999*86400})

	var row model.Client
	if err := db.Where("name = ?", "started-lifetime").First(&row).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	row.Desc = "only the description changed"
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("an edit that never touched the period failed: %v", err)
	}
	tx.Commit()
	if err := db.Where("name = ?", "started-lifetime").First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.ResetDays != 0 || row.ResetDayOfMonth != 0 {
		t.Errorf("period survived without auto reset: resetDays=%d dom=%d", row.ResetDays, row.ResetDayOfMonth)
	}
	// The expiry the client started with is its time limit; it must survive.
	if row.Expiry == 0 {
		t.Error("expiry was cleared on a client with no plan length")
	}
}

// The cap exists for the arithmetic, not as policy: values operators actually
// used on main for "lifetime" must still save.
func TestSaveAcceptsLifetimeStyleDayCounts(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	for _, c := range []struct {
		name string
		body map[string]interface{}
		ok   bool
	}{
		{"period-99999", map[string]interface{}{"autoReset": true, "resetDays": 99999}, true},
		{"plan-99999", map[string]interface{}{"delayStart": true, "planDays": 99999}, true},
		{"period-too-far", map[string]interface{}{"autoReset": true, "resetDays": 20_000_000}, false},
		{"plan-too-far", map[string]interface{}{"delayStart": true, "planDays": 20_000_000}, false},
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

// The bulk action keeps the same invariant as a single save.
func TestSaveResetPolicyClearsThePeriodWhenOff(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "a", Enable: true, AutoReset: true, ResetDays: 30})
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
	if c.AutoReset || c.ResetDays != 0 || c.ResetDayOfMonth != 0 {
		t.Errorf("autoReset=%v resetDays=%d dom=%d, want false/0/0", c.AutoReset, c.ResetDays, c.ResetDayOfMonth)
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

// A delay-start client's plan length lives in its own column now, so no reset
// schedule change can reach it. This used to be reset_days on such a row, which
// made every way of clearing a period a way of destroying a plan length -- four
// separate call sites got that wrong, in both directions, before the split.
//
// Both routes that used to break it are walked here: switching auto reset off
// (which sends resetDays 0), and setting a monthly day (which also sends
// resetDays 0, but with auto reset ON so the first guard did not apply).
func TestSaveResetPolicyCannotTouchPlanDays(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()

	seedClient(t, &model.Client{Name: "delayed", Enable: true, DelayStart: true, PlanDays: 30})
	seedClient(t, &model.Client{Name: "plain", Enable: true, AutoReset: true, ResetDays: 7})

	var ids []uint
	for _, name := range []string{"delayed", "plain"} {
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		ids = append(ids, c.Id)
	}
	apply := func(body map[string]interface{}) {
		t.Helper()
		body["ids"] = ids
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		tx := db.Begin()
		if _, err := svc.Save(tx, "resetpolicy", payload, ""); err != nil {
			tx.Rollback()
			t.Fatalf("Save resetpolicy: %v", err)
		}
		tx.Commit()
	}
	read := func(name string) model.Client {
		t.Helper()
		var c model.Client
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		return c
	}

	apply(map[string]interface{}{"autoReset": true, "resetDays": 0, "resetDayOfMonth": 15})
	if d := read("delayed"); d.PlanDays != 30 {
		t.Fatalf("after the monthly switch: plan_days = %d, want 30", d.PlanDays)
	}
	apply(map[string]interface{}{"autoReset": false, "resetDays": 0, "resetDayOfMonth": 0})

	delayed := read("delayed")
	if delayed.PlanDays != 30 {
		t.Errorf("plan_days = %d, want 30 after both routes", delayed.PlanDays)
	}
	// What the plan length is for: the delay-start branch still matches, so
	// Expiry gets written on the client's first bytes.
	if !delayed.DelayStart || delayed.AutoReset {
		t.Errorf("delayed: delay_start=%v auto_reset=%v, want true/false",
			delayed.DelayStart, delayed.AutoReset)
	}
	if plain := read("plain"); plain.PlanDays != 0 {
		t.Errorf("plain picked up a plan length it never had: %d", plain.PlanDays)
	}
}

// The delay-start branch reads plan_days, and writing Expiry is the whole
// reason it exists.
func TestResetClientsSetsExpiryFromPlanDays(t *testing.T) {
	svc := newResetDB(t)
	now := at(2026, time.September, 22, 14, 37).Unix()

	seedClient(t, &model.Client{
		Enable: true, Name: "delayed",
		DelayStart: true, AutoReset: false, PlanDays: 30,
		// A period in the other column must not stand in for a plan length.
		ResetDays: 0,
		Up:        1, Down: 1,
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
	if c.DelayStart {
		t.Error("delay_start should be cleared once the client has traffic")
	}
	if want := now + 30*86400; c.Expiry != want {
		t.Errorf("expiry = %d, want %d", c.Expiry, want)
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
