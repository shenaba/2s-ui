package service

import (
	"encoding/json"
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

// What each writer puts on the wire for an edit of row c.
var matrixWriters = map[string]func(c model.Client) interface{}{
	// The drawer, the Telegram bot and any JSON round-trip: every field.
	"whole row": func(c model.Client) interface{} { return c },

	// An integration typed against main: every field main had, none added here.
	"typed against main": func(c model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "enable": c.Enable, "name": c.Name, "config": c.Config,
			"inbounds": c.Inbounds, "links": c.Links, "volume": c.Volume,
			"expiry": c.Expiry, "desc": c.Desc, "group": c.Group, "limitIp": c.LimitIp,
			"delayStart": c.DelayStart, "autoReset": c.AutoReset,
			"resetDays": c.ResetDays, "nextReset": c.NextReset,
		}
	},

	// An integration that sends only what it manages.
	"minimal": func(c model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "enable": c.Enable, "name": c.Name, "config": c.Config,
			"inbounds": c.Inbounds, "links": c.Links, "volume": c.Volume,
			"expiry": c.Expiry, "desc": c.Desc, "group": c.Group,
		}
	},

	// The master's cluster push, as expectedClients builds it.
	"cluster push": func(c model.Client) interface{} {
		return map[string]interface{}{
			"id": c.Id, "name": c.Name, "enable": c.Enable, "config": c.Config,
			"inbounds": c.Inbounds, "links": json.RawMessage("[]"), "volume": c.Volume,
			"expiry": c.Expiry, "group": c.Group, "desc": c.Desc, "limitIp": c.LimitIp,
		}
	},
}

func matrixShapes(now int64) map[string]model.Client {
	future := now + 10*86400
	return map[string]model.Client{
		"delay start, plan length":          {DelayStart: true, PlanDays: 30},
		"delay start, auto reset by days":   {DelayStart: true, AutoReset: true, ResetDays: 30},
		"delay start, auto reset monthly":   {DelayStart: true, AutoReset: true, ResetDayOfMonth: 15},
		"delay start, plan and monthly":     {DelayStart: true, PlanDays: 90, AutoReset: true, ResetDayOfMonth: 15},
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
				payload, err := json.Marshal(build(edited))
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
		DelayStart, AutoReset         bool
		PlanDays, ResetDays, ResetDom int
		NextReset, Expiry             int64
	}
	b := sched{before.DelayStart, before.AutoReset, before.PlanDays, before.ResetDays, before.ResetDayOfMonth, before.NextReset, before.Expiry}
	a := sched{after.DelayStart, after.AutoReset, after.PlanDays, after.ResetDays, after.ResetDayOfMonth, after.NextReset, after.Expiry}
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

// Creates, by the same writers. A writer that does not know planDays and sends
// delay start without auto reset meant its plan length in resetDays; one that
// sends planDays -- even 0 -- means exactly what it sent.
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
	cases := []struct {
		name     string
		body     map[string]interface{}
		wantPlan int
		wantDays int
		wantAuto bool
	}{
		{"typed against main, delay start only", with(base("a"), "delayStart", true, "autoReset", false, "resetDays", 30), 30, 0, false},
		{"typed against main, delay start and auto reset", with(base("b"), "delayStart", true, "autoReset", true, "resetDays", 30), 0, 30, true},
		{"new contract, explicit no time limit", with(base("c"), "delayStart", true, "autoReset", false, "planDays", 0, "resetDays", 30), 0, 0, false},
		{"new contract, plan and period", with(base("d"), "delayStart", true, "autoReset", true, "planDays", 90, "resetDays", 7), 90, 7, true},
		// The bot's create form: a whole model.Client, schedule left at zero.
		{"bot create", func() map[string]interface{} {
			raw, _ := json.Marshal(model.Client{Enable: true, Name: "e", Config: json.RawMessage("{}"),
				Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]")})
			var m map[string]interface{}
			json.Unmarshal(raw, &m)
			return m
		}(), 0, 0, false},
	}
	svc := newResetDB(t)
	db := database.GetDB()
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
			var got model.Client
			if err := db.Where("name = ?", c.body["name"]).First(&got).Error; err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got.PlanDays != c.wantPlan || got.ResetDays != c.wantDays || got.AutoReset != c.wantAuto {
				t.Errorf("planDays=%d resetDays=%d autoReset=%v, want %d/%d/%v",
					got.PlanDays, got.ResetDays, got.AutoReset, c.wantPlan, c.wantDays, c.wantAuto)
			}
		})
	}

	// The same rule in a bulk create, which sends an array: each element's own
	// keys decide, not the first element's or none at all.
	payload, err := json.Marshal([]map[string]interface{}{
		with(base("bulk-legacy"), "delayStart", true, "resetDays", 30),
		with(base("bulk-current"), "delayStart", true, "planDays", 0, "resetDays", 30),
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
	for name, want := range map[string]int{"bulk-legacy": 30, "bulk-current": 0} {
		var got model.Client
		if err := db.Where("name = ?", name).First(&got).Error; err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		if got.PlanDays != want {
			t.Errorf("%s: planDays=%d, want %d", name, got.PlanDays, want)
		}
	}
}
