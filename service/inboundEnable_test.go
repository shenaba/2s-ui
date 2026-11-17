package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func seedLinkClient(t *testing.T) model.Client {
	t.Helper()
	c := model.Client{
		Enable:   true,
		Name:     "alice",
		Config:   json.RawMessage(`{"socks":{"username":"alice","password":"pw"},"http":{"username":"alice","password":"pw"}}`),
		Inbounds: json.RawMessage(`[]`),
		Links:    json.RawMessage(`[]`),
	}
	if err := database.GetDB().Create(&c).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	return c
}

// localLinks counts the client's stored local links for one inbound tag.
func localLinks(t *testing.T, clientId uint, tag string) int {
	t.Helper()
	var c model.Client
	if err := database.GetDB().First(&c, clientId).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	var links []map[string]string
	if err := json.Unmarshal(c.Links, &links); err != nil {
		t.Fatalf("links: %v", err)
	}
	n := 0
	for _, l := range links {
		if l["type"] == "local" && l["remark"] == tag {
			n++
		}
	}
	return n
}

func storedInboundEnable(t *testing.T, tag string) bool {
	t.Helper()
	var enable bool
	if err := database.GetDB().Raw("SELECT enable FROM inbounds WHERE tag = ?", tag).Scan(&enable).Error; err != nil {
		t.Fatal(err)
	}
	return enable
}

func inCoreConfig(t *testing.T, s *ConfigService, tag string) bool {
	t.Helper()
	configs, err := s.InboundService.GetAllConfig(database.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range configs {
		var in struct{ Tag string }
		_ = json.Unmarshal(raw, &in)
		if in.Tag == tag {
			return true
		}
	}
	return false
}

// Switching an inbound off takes it out of the running core and of the config
// the next start builds, and takes its links out of every client; an edit that
// does not mention enable keeps it off, and nothing that rebuilds inbounds puts
// it back. Switching it on brings all of it back.
func TestDisablingAnInboundAndBack(t *testing.T) {
	s := runningPanel(t)
	client := seedLinkClient(t)
	cid := strconv.FormatUint(uint64(client.Id), 10)

	if _, err := s.Save("inbounds", "new", []byte(`{"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0}`), cid, "test", "localhost"); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	id := rowId(t, model.Inbound{}, "in")
	sid := strconv.FormatUint(uint64(id), 10)
	if localLinks(t, client.Id, "in") == 0 {
		t.Fatal("no links for the new inbound")
	}

	if err := save(t, s, "inbounds", "edit", `{"id":`+sid+`,"type":"mixed","tag":"in","enable":false,"listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, found := corePtr.GetInstance().Inbound().Get("in"); found {
		t.Error("a disabled inbound is still in the core")
	}
	if inCoreConfig(t, s, "in") {
		t.Error("a disabled inbound is in the config the next start builds")
	}
	if n := localLinks(t, client.Id, "in"); n != 0 {
		t.Errorf("a disabled inbound still has %d links", n)
	}

	// An edit that does not say -- an older API caller, a rebuilt form -- and
	// a user update over it both leave it off.
	if err := save(t, s, "inbounds", "edit", `{"id":`+sid+`,"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("edit without enable: %v", err)
	}
	if storedInboundEnable(t, "in") {
		t.Error("an edit that did not mention enable switched the inbound back on")
	}
	if err := s.InboundService.UpdateInboundsUsers(database.GetDB(), []uint{id}); err != nil {
		t.Fatal(err)
	}
	if err := s.InboundService.RestartInbounds(database.GetDB(), []uint{id}); err != nil {
		t.Fatal(err)
	}
	if _, found := corePtr.GetInstance().Inbound().Get("in"); found {
		t.Error("a user update or reload put the disabled inbound back in the core")
	}

	// Renamed while off, then on: added under the new tag, links back.
	if err := save(t, s, "inbounds", "edit", `{"id":`+sid+`,"type":"mixed","tag":"in2","enable":true,"listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("enable: %v", err)
	}
	box := corePtr.GetInstance()
	if _, found := box.Inbound().Get("in2"); !found {
		t.Error("the re-enabled inbound is not in the core")
	}
	if _, found := box.Inbound().Get("in"); found {
		t.Error("the old tag came back")
	}
	if localLinks(t, client.Id, "in2") == 0 {
		t.Error("the re-enabled inbound has no links")
	}
	if localLinks(t, client.Id, "in") != 0 {
		t.Error("links under the old tag were kept")
	}
}

// A new inbound saved off never reaches the core, and the clients it starts
// with get no links for it until it is switched on.
func TestANewInboundSavedDisabled(t *testing.T) {
	s := runningPanel(t)
	client := seedLinkClient(t)
	cid := strconv.FormatUint(uint64(client.Id), 10)

	if _, err := s.Save("inbounds", "new", []byte(`{"type":"mixed","tag":"off","enable":false,"listen":"127.0.0.1","listen_port":0}`), cid, "test", "localhost"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if storedInboundEnable(t, "off") {
		t.Fatal("a new inbound saved off was stored on")
	}
	if _, found := corePtr.GetInstance().Inbound().Get("off"); found {
		t.Error("a new disabled inbound reached the core")
	}
	if n := localLinks(t, client.Id, "off"); n != 0 {
		t.Errorf("a new disabled inbound gave its client %d links", n)
	}
	// Still assigned, so switching it on serves the client at once.
	var c model.Client
	if err := database.GetDB().First(&c, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var ids []uint
	_ = json.Unmarshal(c.Inbounds, &ids)
	if len(ids) != 1 {
		t.Errorf("client inbounds = %v, want the disabled one kept", ids)
	}
}

// Switching an inbound off is taken out of the core only once the save has
// committed: a save that fails after the inbound's own part -- here, rolled
// back by the caller -- leaves it running, connections and all.
func TestADisableThatDoesNotCommitLeavesTheCoreAlone(t *testing.T) {
	s := runningPanel(t)
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"in","listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	sid := strconv.FormatUint(uint64(rowId(t, model.Inbound{}, "in")), 10)

	tx := database.GetDB().Begin()
	afterCommit, err := s.InboundService.Save(tx, "edit", []byte(`{"id":`+sid+`,"type":"mixed","tag":"in","enable":false,"listen":"127.0.0.1","listen_port":0}`), "", "localhost")
	if err != nil {
		tx.Rollback()
		t.Fatalf("disable: %v", err)
	}
	if afterCommit == nil {
		t.Fatal("a disable left nothing for the core to do after the commit")
	}
	tx.Rollback()
	if _, found := corePtr.GetInstance().Inbound().Get("in"); !found {
		t.Fatal("the inbound left the core inside a save that rolled back")
	}
	if !storedInboundEnable(t, "in") {
		t.Fatal("the rollback did not keep the row on")
	}

	// The same save, committed, does take it out.
	if err := save(t, s, "inbounds", "edit", `{"id":`+sid+`,"type":"mixed","tag":"in","enable":false,"listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, found := corePtr.GetInstance().Inbound().Get("in"); found {
		t.Error("a committed disable left the inbound in the core")
	}

	// A delete is taken out after its commit the same way.
	if err := save(t, s, "inbounds", "new", `{"type":"mixed","tag":"del","listen":"127.0.0.1","listen_port":0}`); err != nil {
		t.Fatal(err)
	}
	tx = database.GetDB().Begin()
	if _, err := s.InboundService.Save(tx, "del", []byte(`"del"`), "", "localhost"); err != nil {
		tx.Rollback()
		t.Fatalf("delete: %v", err)
	}
	tx.Rollback()
	if _, found := corePtr.GetInstance().Inbound().Get("del"); !found {
		t.Error("a delete that rolled back took the inbound out of the core")
	}
	if err := save(t, s, "inbounds", "del", `"del"`); err != nil {
		t.Fatal(err)
	}
	if _, found := corePtr.GetInstance().Inbound().Get("del"); found {
		t.Error("a committed delete left the inbound in the core")
	}
}

// The listener's own detour (a ShadowTLS handing its connections to the
// shadowsocks behind it) is followed by the core per connection, so it is held
// at save time: the target cannot go off under an enabled forwarder, and a
// forwarder cannot come on over a disabled target.
func TestCheckListenDetour(t *testing.T) {
	db := newTestDB(t)
	rows := []model.Inbound{
		{Type: "shadowsocks", Tag: "ss-in", Options: json.RawMessage(`{}`)},
		{Type: "shadowtls", Tag: "stls-in", Options: json.RawMessage(`{"detour":"ss-in"}`)},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	ss, stls := rows[0], rows[1]

	off := &model.Inbound{Id: ss.Id, Type: "shadowsocks", Tag: "ss-in", Enable: false, Options: ss.Options}
	if err := checkListenDetour(db, off, "ss-in"); err == nil || !strings.Contains(err.Error(), "stls-in") {
		t.Errorf("disabling a forwarded-to inbound: err = %v, want a refusal naming stls-in", err)
	}
	if err := db.Model(model.Inbound{}).Where("id = ?", stls.Id).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := checkListenDetour(db, off, "ss-in"); err != nil {
		t.Errorf("disabling one only a disabled inbound forwards to: %v", err)
	}
	if err := db.Model(model.Inbound{}).Where("id = ?", ss.Id).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}

	on := &model.Inbound{Id: stls.Id, Type: "shadowtls", Tag: "stls-in", Enable: true, Options: stls.Options}
	if err := checkListenDetour(db, on, "stls-in"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("enabling a forwarder over a disabled target: err = %v, want a refusal", err)
	}
	// A detour naming no inbound is the core's to report, as before.
	free := &model.Inbound{Id: stls.Id, Type: "shadowtls", Tag: "stls-in", Enable: true, Options: json.RawMessage(`{"detour":"nothing"}`)}
	if err := checkListenDetour(db, free, "stls-in"); err != nil {
		t.Errorf("a detour naming no inbound: %v", err)
	}
}
