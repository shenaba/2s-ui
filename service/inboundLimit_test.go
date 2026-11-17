package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

// Per-inbound traffic caps (#289), against a real database and a real core.
// The switch itself -- what "off" means for the core, the config and the
// links -- is #306's and is tested there; these cover what the cap adds.

func inboundRow(t *testing.T, tag string) model.Inbound {
	t.Helper()
	var inb model.Inbound
	if err := database.GetDB().Where("tag = ?", tag).First(&inb).Error; err != nil {
		t.Fatalf("read inbound %s: %v", tag, err)
	}
	return inb
}

func inCore(tag string) bool {
	_, found := corePtr.GetInstance().Inbound().Get(tag)
	return found
}

func setTraffic(t *testing.T, tag string, up, down int64) {
	t.Helper()
	err := database.GetDB().Model(model.Inbound{}).Where("tag = ?", tag).
		Updates(map[string]interface{}{"up": up, "down": down}).Error
	if err != nil {
		t.Fatalf("set traffic: %v", err)
	}
}

// An inbound over its cap is switched off as by hand -- out of the core, its
// links out of the client -- and nothing that rebuilds inbounds puts it back.
// Switching it on is refused until its traffic is reset, and the reset is what
// brings it back, links included.
func TestAnInboundOverItsCapIsSwitchedOffUntilReset(t *testing.T) {
	s := runningPanel(t)
	client := seedLinkClient(t)
	cid := strconv.FormatUint(uint64(client.Id), 10)
	if _, err := s.Save("inbounds", "new", []byte(`{"type":"mixed","tag":"capped","listen":"127.0.0.1","listen_port":0,"volume":1000}`), cid, "test", "localhost"); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"free","listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	links := localLinks(t, client.Id, "capped")
	if links == 0 {
		t.Fatal("no links for the new inbound")
	}

	// At the cap is not over it, as for a client.
	setTraffic(t, "capped", 600, 400)
	setTraffic(t, "free", 1<<40, 0)
	if tags, err := s.InboundService.DepleteInbounds(); err != nil || len(tags) != 0 {
		t.Fatalf("at the cap: switched off %v (%v), want nothing", tags, err)
	}

	setTraffic(t, "capped", 600, 401)
	tags, err := s.InboundService.DepleteInbounds()
	if err != nil {
		t.Fatalf("deplete: %v", err)
	}
	if len(tags) != 1 || tags[0] != "capped" {
		t.Fatalf("switched off %v, want [capped] (an inbound with no cap never runs out)", tags)
	}
	if inboundRow(t, "capped").Enable {
		t.Error("the spent inbound is still on")
	}
	if inCore("capped") {
		t.Error("the spent inbound is still in the core")
	}
	if n := localLinks(t, client.Id, "capped"); n != 0 {
		t.Errorf("the spent inbound still has %d links", n)
	}
	if !inCore("free") {
		t.Error("an inbound with no cap was taken out")
	}
	if again, _ := s.InboundService.DepleteInbounds(); len(again) != 0 {
		t.Errorf("a second pass switched off %v again", again)
	}

	id := inboundRow(t, "capped").Id
	if err := s.InboundService.UpdateInboundsUsers(database.GetDB(), []uint{id}); err != nil {
		t.Fatalf("update users: %v", err)
	}
	if err := s.InboundService.RestartInbounds(database.GetDB(), []uint{id}); err != nil {
		t.Fatalf("restart inbounds: %v", err)
	}
	if inCore("capped") || inCoreConfig(t, s, "capped") {
		t.Error("a user update, a reload or the next start would bring the spent inbound back")
	}

	idStr := strconv.FormatUint(uint64(id), 10)
	err = save(t, s, "inbounds", "edit", `{"id":`+idStr+`,"type":"mixed","tag":"capped","listen":"127.0.0.1","listen_port":0,"enable":true}`)
	if err == nil || !strings.Contains(err.Error(), "traffic limit") {
		t.Fatalf("switching a spent inbound on = %v, want it refused", err)
	}
	if inCore("capped") {
		t.Error("a refused switch-on reached the core")
	}

	if err := save(t, s, "inbounds", "resetTraffic", `"capped"`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	row := inboundRow(t, "capped")
	if !row.Enable || row.Up != 0 || row.Down != 0 {
		t.Errorf("after reset: enable=%v up=%d down=%d, want on and zeroed", row.Enable, row.Up, row.Down)
	}
	if row.Volume != 1000 {
		t.Errorf("the reset changed the cap to %d", row.Volume)
	}
	if !inCore("capped") {
		t.Error("the reset did not start the inbound")
	}
	if n := localLinks(t, client.Id, "capped"); n != links {
		t.Errorf("after reset the client has %d links for it, want %d back", n, links)
	}
}

// An edit that does not carry volume (an older apiv2 caller) keeps the stored
// cap, and no edit writes the counters: those are the flush's.
func TestAnInboundEditKeepsItsCap(t *testing.T) {
	s := runningPanel(t)
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0,"volume":5000}`); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	setTraffic(t, "in", 30, 40)
	id := strconv.FormatUint(uint64(inboundRow(t, "in").Id), 10)

	if err := save(t, s, "inbounds", "edit", `{"id":`+id+`,"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0,"up":0,"down":0}`); err != nil {
		t.Fatalf("edit: %v", err)
	}
	row := inboundRow(t, "in")
	if row.Volume != 5000 {
		t.Errorf("an edit without volume left it at %d, want 5000", row.Volume)
	}
	if row.Up != 30 || row.Down != 40 {
		t.Errorf("an edit wrote the counters: %d/%d, want 30/40", row.Up, row.Down)
	}
	// None of it may reach sing-box, which refuses an unknown field.
	raw, err := row.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range []string{`"volume"`, `"up"`, `"down"`} {
		if strings.Contains(string(raw), k) {
			t.Errorf("the sing-box shape carries %s: %s", k, raw)
		}
	}

	// Lifting the cap is a volume of 0, sent.
	if err := save(t, s, "inbounds", "edit", `{"id":`+id+`,"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0,"volume":0}`); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if v := inboundRow(t, "in").Volume; v != 0 {
		t.Errorf("volume 0 was not stored, got %d", v)
	}
}

// Switched off by hand with traffic to spare, a reset zeroes it and leaves it
// off: nothing about a reset says the operator changed their mind.
func TestAResetLeavesAHandSwitchedOffInboundOff(t *testing.T) {
	s := runningPanel(t)
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"off","listen":"127.0.0.1","listen_port":0,"volume":1000,"enable":false}`); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	setTraffic(t, "off", 5, 5)
	if err := save(t, s, "inbounds", "resetTraffic", `"off"`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if row := inboundRow(t, "off"); row.Enable || row.Up != 0 || row.Down != 0 {
		t.Errorf("reset of a hand-switched-off inbound: enable=%v up=%d down=%d, want off and zeroed", row.Enable, row.Up, row.Down)
	}
	if inCore("off") {
		t.Error("the reset started an inbound switched off by hand")
	}
}

// A save will not switch off an inbound an enabled listener hands its
// connections to. The cap does anyway -- the traffic is spent -- and the chain
// stops with it. Coming back, a reset makes the checks a switch-on makes: one
// whose own detour is still off stays off, with the save refused.
func TestTheCapSwitchesOffADetourTargetAnyway(t *testing.T) {
	s := runningPanel(t)
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"back","listen":"127.0.0.1","listen_port":0,"volume":100}`); err != nil {
		t.Fatalf("save back: %v", err)
	}
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"front","listen":"127.0.0.1","listen_port":0,"detour":"back","volume":100}`); err != nil {
		t.Fatalf("save front: %v", err)
	}
	backId := strconv.FormatUint(uint64(inboundRow(t, "back").Id), 10)
	if err := save(t, s, "inbounds", "edit", `{"id":`+backId+`,"type":"mixed","tag":"back","listen":"127.0.0.1","listen_port":0,"enable":false}`); err == nil {
		t.Fatal("switching off a listener's detour by hand must still be refused")
	}

	setTraffic(t, "back", 101, 0)
	setTraffic(t, "front", 101, 0)
	if tags, err := s.InboundService.DepleteInbounds(); err != nil || len(tags) != 2 {
		t.Fatalf("deplete = %v (%v), want both switched off", tags, err)
	}

	err := save(t, s, "inbounds", "resetTraffic", `"front"`)
	if err == nil || !strings.Contains(err.Error(), "back") {
		t.Fatalf("resetting front while its detour is off = %v, want it refused naming back", err)
	}
	if row := inboundRow(t, "front"); row.Enable || row.Up == 0 {
		t.Error("a refused reset was written")
	}
	if err := save(t, s, "inbounds", "resetTraffic", `"back"`); err != nil {
		t.Fatalf("reset back: %v", err)
	}
	if err := save(t, s, "inbounds", "resetTraffic", `"front"`); err != nil {
		t.Fatalf("reset front once back is on: %v", err)
	}
	if !inCore("back") || !inCore("front") {
		t.Error("both should be running again")
	}
}
