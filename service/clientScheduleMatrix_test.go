package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// Every writer that edits a whole client, crossed with every schedule shape a
// stored client can have. Each cell makes the smallest possible edit -- the
// description -- and asserts that nothing about the reset schedule moved.
//
// Seven review rounds each found one more writer or one more shape that this
// PR had not been checked against, so the check is a table instead of a list of
// paths. A writer or shape added later belongs in these tables, not in a new
// one-off test.

// What each writer puts on the wire for an edit of row c, which it read as
// opened.
var matrixWriters = map[string]func(c, opened model.Client) interface{}{
	// The Telegram bot and any JSON round-trip: every field.
	"whole row": func(c, _ model.Client) interface{} { return c },

	// The panel's client drawer.
	"drawer": func(c, opened model.Client) interface{} { return drawerPayload(c, opened) },

	// An integration typed against main: every field main had, none added here.
	"typed against main": func(c, _ model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "enable": c.Enable, "name": c.Name, "config": c.Config,
			"inbounds": c.Inbounds, "links": c.Links, "volume": c.Volume,
			"expiry": c.Expiry, "desc": c.Desc, "group": c.Group, "limitIp": c.LimitIp,
			"delayStart": c.DelayStart, "autoReset": c.AutoReset,
			"resetDays": c.ResetDays, "nextReset": c.NextReset,
		}
	},

	// An integration that sends only what it manages.
	"minimal": func(c, _ model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "enable": c.Enable, "name": c.Name, "config": c.Config,
			"inbounds": c.Inbounds, "links": c.Links, "volume": c.Volume,
			"expiry": c.Expiry, "desc": c.Desc, "group": c.Group,
		}
	},

	// The master's cluster push, as expectedClients builds it.
	"cluster push": func(c, _ model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "name": c.Name, "enable": c.Enable, "config": c.Config,
			"inbounds": c.Inbounds, "links": json.RawMessage("[]"), "volume": c.Volume,
			"expiry": c.Expiry, "group": c.Group, "desc": c.Desc, "limitIp": c.LimitIp,
		}
	},
}

// drawerPayload is what the client drawer sends for an edit: the id and every
// field that differs from the row it opened with. The traffic counters and
// nextReset go only with an explicit reset or date edit, which no test using
// this makes.
func drawerPayload(edited, opened model.Client) map[string]interface{} {
	now, before := jsonFields(edited), jsonFields(opened)
	out := map[string]interface{}{"id": edited.Id}
	for k, v := range now {
		if !reflect.DeepEqual(v, before[k]) {
			out[k] = v
		}
	}
	for _, k := range []string{"up", "down", "totalUp", "totalDown", "nextReset"} {
		delete(out, k)
	}
	return out
}

func jsonFields(c model.Client) map[string]interface{} {
	raw, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(err)
	}
	return m
}

func matrixShapes(now int64) map[string]model.Client {
	future := now + 10*86400
	return map[string]model.Client{
		"delay start, auto reset by days":   {DelayStart: true, AutoReset: true, ResetDays: 30},
		"delay start, auto reset monthly":   {DelayStart: true, AutoReset: true, ResetDayOfMonth: 15},
		"delay start, with expiry":          {DelayStart: true, AutoReset: true, ResetDays: 30, Expiry: future},
		"started, auto reset by days":       {AutoReset: true, ResetDays: 30, NextReset: future, Up: 500, Down: 500},
		"started, auto reset monthly":       {AutoReset: true, ResetDayOfMonth: 15, NextReset: future, Up: 500, Down: 500},
		"started, no schedule, with expiry": {Expiry: future, Up: 500, Down: 500},
	}
}

func TestEditMatrixKeepsTheSchedule(t *testing.T) {
	for writer, build := range matrixWriters {
		for shapeName, shape := range matrixShapes(time.Now().Unix()) {
			t.Run(writer+" / "+shapeName, func(t *testing.T) {
				svc := newResetDB(t)
				db := database.GetDB()
				seed := shape
				seed.Name, seed.Enable = "c", true
				seedClient(t, &seed)

				var before model.Client
				if err := db.Where("name = ?", "c").First(&before).Error; err != nil {
					t.Fatalf("read: %v", err)
				}
				edited := before
				edited.Desc = "edited by " + writer
				payload, err := json.Marshal(build(edited, before))
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				tx := db.Begin()
				if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
					tx.Rollback()
					t.Fatalf("Save: %v", err)
				}
				tx.Commit()

				var after model.Client
				if err := db.Where("name = ?", "c").First(&after).Error; err != nil {
					t.Fatalf("read back: %v", err)
				}
				if after.Desc != edited.Desc {
					t.Fatalf("the edit itself did not land: desc = %q", after.Desc)
				}
				checkScheduleKept(t, before, after)

				// And one tick of the job: every NextReset here is in the
				// future and no delayed row has traffic, so nothing may move.
				tx = db.Begin()
				if _, _, _, err := svc.ResetClients(tx, time.Now().Unix(), time.UTC); err != nil {
					tx.Rollback()
					t.Fatalf("ResetClients: %v", err)
				}
				tx.Commit()
				if err := db.Where("name = ?", "c").First(&after).Error; err != nil {
					t.Fatalf("read back: %v", err)
				}
				if after.Up != before.Up || after.Down != before.Down {
					t.Errorf("usage cleared by the next tick: %d/%d, was %d/%d", after.Up, after.Down, before.Up, before.Down)
				}
				checkScheduleKept(t, before, after)
			})
		}
	}
}

func checkScheduleKept(t *testing.T, before, after model.Client) {
	t.Helper()
	type sched struct {
		DelayStart, AutoReset bool
		ResetDays, ResetDom   int
		NextReset, Expiry     int64
	}
	b := sched{before.DelayStart, before.AutoReset, before.ResetDays, before.ResetDayOfMonth, before.NextReset, before.Expiry}
	a := sched{after.DelayStart, after.AutoReset, after.ResetDays, after.ResetDayOfMonth, after.NextReset, after.Expiry}
	if a != b {
		t.Errorf("schedule moved:\n  before %+v\n  after  %+v", b, a)
	}
}

// Bulk edits send the client list projection, which carries none of the
// schedule columns; fillOmitted restores them from the stored row. Same shapes,
// same assertion.
func TestEditBulkMatrixKeepsTheSchedule(t *testing.T) {
	for shapeName, shape := range matrixShapes(time.Now().Unix()) {
		t.Run(shapeName, func(t *testing.T) {
			svc := newResetDB(t)
			db := database.GetDB()
			seed := shape
			seed.Name, seed.Enable = "c", true
			seedClient(t, &seed)

			var before model.Client
			if err := db.Where("name = ?", "c").First(&before).Error; err != nil {
				t.Fatalf("read: %v", err)
			}
			row := map[string]interface{}{
				"id": before.Id, "enable": before.Enable, "name": before.Name,
				"desc": "bulk", "group": before.Group, "inbounds": before.Inbounds,
				"up": before.Up, "down": before.Down, "volume": before.Volume,
				"expiry": before.Expiry, "limitIp": before.LimitIp,
			}
			payload, err := json.Marshal([]interface{}{row})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			tx := db.Begin()
			if _, err := svc.Save(tx, "editbulk", payload, ""); err != nil {
				tx.Rollback()
				t.Fatalf("Save editbulk: %v", err)
			}
			tx.Commit()
			var after model.Client
			if err := db.Where("name = ?", "c").First(&after).Error; err != nil {
				t.Fatalf("read back: %v", err)
			}
			checkScheduleKept(t, before, after)
		})
	}
}

// Creates, by the same writers, single and bulk. main's plan-length shape --
// delay start without auto reset, the length in resetDays -- asked for a
// feature that is gone and comes out as a client with no schedule; with auto
// reset on, the same fields keep their meaning.
func TestCreateMatrix(t *testing.T) {
	base := func(name string) map[string]interface{} {
		return map[string]interface{}{
			"name": name, "enable": true, "config": map[string]interface{}{},
			"inbounds": []uint{}, "links": []interface{}{},
		}
	}
	with := func(m map[string]interface{}, kv ...interface{}) map[string]interface{} {
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	type want struct {
		delay bool
		days  int
		auto  bool
	}
	cases := []struct {
		name string
		body map[string]interface{}
		want want
	}{
		{"typed against main, delay start only", with(base("a"), "delayStart", true, "autoReset", false, "resetDays", 30), want{false, 0, false}},
		{"typed against main, delay start and auto reset", with(base("b"), "delayStart", true, "autoReset", true, "resetDays", 30), want{true, 30, true}},
		// The bot's create form: a whole model.Client, schedule left at zero.
		{"bot create", func() map[string]interface{} {
			raw, err := json.Marshal(model.Client{Enable: true, Name: "e", Config: json.RawMessage("{}"),
				Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]")})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var m map[string]interface{}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			return m
		}(), want{false, 0, false}},
	}
	svc := newResetDB(t)
	db := database.GetDB()
	check := func(name string, w want) {
		t.Helper()
		var got model.Client
		if err := db.Where("name = ?", name).First(&got).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		if g := (want{got.DelayStart, got.ResetDays, got.AutoReset}); g != w {
			t.Errorf("%s: delayStart/resetDays/autoReset = %+v, want %+v", name, g, w)
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, err := json.Marshal(c.body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			tx := db.Begin()
			if _, err := svc.Save(tx, "new", payload, ""); err != nil {
				tx.Rollback()
				t.Fatalf("Save: %v", err)
			}
			tx.Commit()
			check(c.body["name"].(string), c.want)
		})
	}

	payload, err := json.Marshal([]map[string]interface{}{
		with(base("bulk-main-plan"), "delayStart", true, "resetDays", 30),
		with(base("bulk-periodic"), "delayStart", true, "autoReset", true, "resetDays", 30),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "addbulk", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save addbulk: %v", err)
	}
	tx.Commit()
	check("bulk-main-plan", want{false, 0, false})
	check("bulk-periodic", want{true, 30, true})
}

// The drawer's next-reset field has a clear button that writes 0. Stored as 0 on
// an auto-reset client, the periodic branch would match on its next tick and
// clear the usage; 0 has to mean "compute one", which is also what the field
// now says it means.
func TestEditClearedNextResetComputesOne(t *testing.T) {
	now := time.Now().Unix()
	for name, shape := range map[string]model.Client{
		"by days": {AutoReset: true, ResetDays: 30, NextReset: now + 10*86400, Up: 500, Down: 500},
		"monthly": {AutoReset: true, ResetDayOfMonth: 15, NextReset: now + 10*86400, Up: 500, Down: 500},
	} {
		t.Run(name, func(t *testing.T) {
			svc := newResetDB(t)
			db := database.GetDB()
			seed := shape
			seed.Name, seed.Enable = "c", true
			seedClient(t, &seed)
			var row model.Client
			if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
				t.Fatalf("read: %v", err)
			}
			row.NextReset = 0
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
			tx = db.Begin()
			if _, _, _, err := svc.ResetClients(tx, time.Now().Unix(), time.UTC); err != nil {
				tx.Rollback()
				t.Fatalf("ResetClients: %v", err)
			}
			tx.Commit()
			if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
				t.Fatalf("read back: %v", err)
			}
			if row.NextReset <= time.Now().Unix() {
				t.Errorf("nextReset = %d, want a future boundary computed from the schedule", row.NextReset)
			}
			if row.Up != 500 || row.Down != 500 {
				t.Errorf("usage cleared by clearing the date field: %d/%d", row.Up, row.Down)
			}
		})
	}
}

// Switching auto reset on together with a first reset date: the date is the
// operator's anchor and must be kept, not replaced by now + period.
func TestEditKeepsAnExplicitFirstReset(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "c", Enable: true})
	var row model.Client
	if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	anchor := time.Now().Unix() + 3*86400
	row.AutoReset, row.ResetDays, row.NextReset = true, 30, anchor
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
	if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.NextReset != anchor {
		t.Errorf("nextReset = %d, want the submitted anchor %d", row.NextReset, anchor)
	}
}

// Re-applying a schedule in bulk must not re-anchor the rows already on it.
// The usual selection is "everyone", to switch auto reset on for the rest.
func TestResetPolicyKeepsTheBoundaryOfAnUnchangedSchedule(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	now := time.Now().Unix()
	dueSoon := now + 2*86400
	seedClient(t, &model.Client{Name: "same", Enable: true, AutoReset: true, ResetDays: 30, NextReset: dueSoon})
	seedClient(t, &model.Client{Name: "other-period", Enable: true, AutoReset: true, ResetDays: 7, NextReset: dueSoon})
	seedClient(t, &model.Client{Name: "off", Enable: true})
	// Same schedule but no usable boundary: it gets one.
	seedClient(t, &model.Client{Name: "same-no-boundary", Enable: true, AutoReset: true, ResetDays: 30})

	var ids []uint
	for _, n := range []string{"same", "other-period", "off", "same-no-boundary"} {
		var c model.Client
		if err := db.Where("name = ?", n).First(&c).Error; err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		ids = append(ids, c.Id)
	}
	payload, err := json.Marshal(map[string]interface{}{"ids": ids, "autoReset": true, "resetDays": 30, "resetDayOfMonth": 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "resetpolicy", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save resetpolicy: %v", err)
	}
	tx.Commit()

	read := func(n string) model.Client {
		var c model.Client
		if err := db.Where("name = ?", n).First(&c).Error; err != nil {
			t.Fatalf("read back %s: %v", n, err)
		}
		return c
	}
	if c := read("same"); c.NextReset != dueSoon {
		t.Errorf("same: nextReset moved from %d to %d", dueSoon, c.NextReset)
	}
	for _, n := range []string{"other-period", "off", "same-no-boundary"} {
		c := read(n)
		if c.NextReset <= now || c.NextReset == dueSoon {
			t.Errorf("%s: nextReset = %d, want a boundary computed from now", n, c.NextReset)
		}
	}
}

// A changed N-day period moves the current boundary by the difference: the
// panel's way to lengthen or shorten one period without restarting it. The
// backend applies it to the stored boundary whenever the request leaves
// nextReset out; a caller that sends nextReset gets exactly what it sent.
func TestEditPeriodShiftsTheStoredBoundary(t *testing.T) {
	const day = int64(86400)
	now := time.Now().Unix()
	boundary := now + 10*day
	periodic := model.Client{AutoReset: true, ResetDays: 30, NextReset: boundary, Up: 500, Down: 500}
	cases := []struct {
		name  string
		shape model.Client
		edit  func(row model.Client) interface{}
		want  int64 // 0: computed from now, want the new period from the save
	}{
		{"drawer, longer period", periodic, func(row model.Client) interface{} {
			e := row
			e.ResetDays = 31
			return drawerPayload(e, row)
		}, boundary + day},
		{"drawer, shorter period", periodic, func(row model.Client) interface{} {
			e := row
			e.ResetDays = 25
			return drawerPayload(e, row)
		}, boundary - 5*day},
		{"minimal writer", periodic, func(row model.Client) interface{} {
			m := matrixWriters["minimal"](row, row).(map[string]interface{})
			m["resetDays"] = 31
			return m
		}, boundary + day},
		// Controls: these already behaved this way.
		{"sent nextReset is kept, not shifted again", periodic, func(row model.Client) interface{} {
			row.ResetDays, row.NextReset = 31, boundary+3*day
			return row
		}, boundary + 3*day},
		{"typed against main sends the old boundary", periodic, func(row model.Client) interface{} {
			row.ResetDays = 31
			return matrixWriters["typed against main"](row, row)
		}, boundary},
		{"same period, nothing moves", periodic, func(row model.Client) interface{} {
			return drawerPayload(row, row)
		}, boundary},
		{"a day of the month has no period to shift",
			model.Client{AutoReset: true, ResetDayOfMonth: 15, NextReset: boundary},
			func(row model.Client) interface{} {
				e := row
				e.ResetDays = 7
				return drawerPayload(e, row)
			}, boundary},
		{"auto reset switched on starts a period from now",
			model.Client{},
			func(row model.Client) interface{} {
				e := row
				e.AutoReset, e.ResetDays = true, 31
				return drawerPayload(e, row)
			}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := newResetDB(t)
			db := database.GetDB()
			seed := c.shape
			seed.Name, seed.Enable = "c", true
			seedClient(t, &seed)
			var row model.Client
			if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
				t.Fatalf("read: %v", err)
			}
			payload, err := json.Marshal(c.edit(row))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			start := time.Now().Unix()
			tx := db.Begin()
			if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
				tx.Rollback()
				t.Fatalf("Save: %v", err)
			}
			tx.Commit()
			end := time.Now().Unix()
			if err := db.Where("name = ?", "c").First(&row).Error; err != nil {
				t.Fatalf("read back: %v", err)
			}
			if c.want != 0 {
				if row.NextReset != c.want {
					t.Errorf("nextReset = %d, want %d (%+d days from the stored boundary)",
						row.NextReset, c.want, (c.want-boundary)/day)
				}
				return
			}
			if row.NextReset < start+31*day || row.NextReset > end+31*day {
				t.Errorf("nextReset = %d, want 31 days from the save (%d..%d)", row.NextReset, start+31*day, end+31*day)
			}
		})
	}
}

// The drawer left open across a boundary, then the period changed. The shift
// has to start from where the job moved the boundary, not from the one the
// drawer read when it opened: that one plus a day is tomorrow, a second reset
// one day after the first.
func TestEditPeriodShiftStartsFromTheJobsBoundary(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	now := time.Now().Unix()
	seedClient(t, &model.Client{Name: "c", Enable: true, AutoReset: true, ResetDays: 30, NextReset: now - 60, Up: 500, Down: 500})
	var opened model.Client
	if err := db.Where("name = ?", "c").First(&opened).Error; err != nil {
		t.Fatalf("read: %v", err)
	}

	// The tick the drawer sat through.
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, now, time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()
	var moved model.Client
	if err := db.Where("name = ?", "c").First(&moved).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if moved.NextReset <= now || moved.Up != 0 {
		t.Fatalf("setup: the tick did not reset the client: nextReset=%d up=%d", moved.NextReset, moved.Up)
	}

	edited := opened
	edited.ResetDays = 31
	payload, err := json.Marshal(drawerPayload(edited, opened))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx = db.Begin()
	if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save: %v", err)
	}
	tx.Commit()

	var after model.Client
	if err := db.Where("name = ?", "c").First(&after).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := moved.NextReset + 86400; after.NextReset != want {
		t.Errorf("nextReset = %d, want the job's boundary plus a day, %d (the drawer read %d)",
			after.NextReset, want, opened.NextReset)
	}
	if after.Up != 0 || after.Down != 0 {
		t.Errorf("counters the drawer read were written back: %d/%d", after.Up, after.Down)
	}
}

// An edit is decoded over the stored row: a request that names two fields
// changes those two, and every other column keeps its stored value -- counters,
// the Telegram binding, the quota and expiry, the schedule, the timestamps.
func TestEditKeepsWhatTheRequestOmits(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	now := time.Now().Unix()
	seedClient(t, &model.Client{
		Name: "full", Enable: true, Volume: 50 << 30, Expiry: now + 40*86400,
		Up: 7, Down: 9, TotalUp: 70, TotalDown: 90, LimitIp: 3, Group: "g", Remark: "r",
		TgId: 4242, CreatedAt: now - 86400, OnlineAt: now - 60,
		AutoReset: true, ResetDayOfMonth: 15, NextReset: now + 5*86400,
		Inbounds: json.RawMessage("[]"),
	})
	var before model.Client
	if err := db.Where("name = ?", "full").First(&before).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	payload, err := json.Marshal(map[string]interface{}{"id": before.Id, "desc": "only this"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx := db.Begin()
	if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save: %v", err)
	}
	tx.Commit()
	var after model.Client
	if err := db.Where("id = ?", before.Id).First(&after).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.Desc != "only this" {
		t.Fatalf("desc = %q, the edit did not land", after.Desc)
	}
	// Links are regenerated from the inbounds on every save; everything else
	// must be exactly as stored.
	after.Desc, after.Links, before.Links = before.Desc, nil, nil
	if !reflect.DeepEqual(jsonFields(after), jsonFields(before)) {
		t.Errorf("an edit of the description moved other fields:\n  before %+v\n  after  %+v", before, after)
	}
}

// The drawer opened on a delay-start client that makes its first connection
// before the operator saves. The drawer sends only what the operator changed,
// so the state the job wrote in between -- delay start cleared, the first
// boundary set, the traffic -- survives the save. As a whole row it wrote
// delay start back on, and the reset clock started over on the next tick.
func TestEditAfterFirstConnectionKeepsWhatTheJobWrote(t *testing.T) {
	svc := newResetDB(t)
	db := database.GetDB()
	seedClient(t, &model.Client{Name: "c", Enable: true, DelayStart: true, AutoReset: true, ResetDays: 30})
	var opened model.Client
	if err := db.Where("name = ?", "c").First(&opened).Error; err != nil {
		t.Fatalf("read: %v", err)
	}

	// First bytes arrive, and the job runs, while the drawer is open.
	if err := db.Model(model.Client{}).Where("id = ?", opened.Id).
		UpdateColumns(map[string]interface{}{"up": 100, "down": 200}).Error; err != nil {
		t.Fatalf("add traffic: %v", err)
	}
	tx := db.Begin()
	if _, _, _, err := svc.ResetClients(tx, time.Now().Unix(), time.UTC); err != nil {
		tx.Rollback()
		t.Fatalf("ResetClients: %v", err)
	}
	tx.Commit()
	var started model.Client
	if err := db.Where("id = ?", opened.Id).First(&started).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if started.DelayStart || started.NextReset == 0 {
		t.Fatalf("setup: first use did not happen: %+v", started)
	}

	edited := opened
	edited.Desc = "renamed plan"
	payload, err := json.Marshal(drawerPayload(edited, opened))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tx = db.Begin()
	if _, err := svc.Save(tx, "edit", payload, ""); err != nil {
		tx.Rollback()
		t.Fatalf("Save: %v", err)
	}
	tx.Commit()
	var after model.Client
	if err := db.Where("id = ?", opened.Id).First(&after).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if after.DelayStart || after.NextReset != started.NextReset || after.Up != 100 || after.Down != 200 {
		t.Errorf("the save wrote the drawer's stale state back: delayStart=%v nextReset=%d (want %d) up/down=%d/%d",
			after.DelayStart, after.NextReset, started.NextReset, after.Up, after.Down)
	}
}
