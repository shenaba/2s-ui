package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
)

// seedRiders stores a ShadowTLS inbound, two client configs riding it (#255),
// one riding something else, and a node replica naming the same tag --
// replicas mirror another panel and are never this panel's to edit.
func seedRiders(t *testing.T, db *gorm.DB) model.Inbound {
	t.Helper()
	nodeId := uint(1)
	rows := []model.Inbound{
		{Type: "shadowtls", Tag: "stls-in", OutJson: json.RawMessage(`{"type":"shadowtls","tag":"stls-in"}`)},
		{Type: "shadowsocks", Tag: "ss-a", OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"stls-in"}`)},
		{Type: "shadowsocks", Tag: "ss-b", OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"stls-in","method":"aes-128-gcm"}`)},
		{Type: "shadowsocks", Tag: "ss-other", OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"elsewhere"}`)},
		{Type: "shadowsocks", Tag: "ss-replica", NodeId: &nodeId, OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"stls-in"}`)},
	}
	for i := range rows {
		rows[i].Options = json.RawMessage(`{}`)
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", rows[i].Tag, err)
		}
	}
	return rows[0]
}

func TestClientDetourRiders(t *testing.T) {
	db := newTestDB(t)
	target := seedRiders(t, db)

	riders, err := clientDetourRiders(db, target.Id, "stls-in")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ss-a", "ss-b"}; !reflect.DeepEqual(riders, want) {
		t.Errorf("riders = %v, want %v: replicas and other detours are not riders", riders, want)
	}
}

// A rename has to carry over to every config riding the old tag, and leave
// the rest of each config -- and every config riding something else -- alone.
func TestRenameClientDetours(t *testing.T) {
	db := newTestDB(t)
	target := seedRiders(t, db)

	if err := renameClientDetours(db, target.Id, "stls-in", "stls-443"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ss-a": "stls-443", "ss-b": "stls-443", "ss-other": "elsewhere", "ss-replica": "stls-in"}
	for tag, detour := range want {
		var row model.Inbound
		if err := db.Where("tag = ?", tag).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(row.OutJson, &out); err != nil {
			t.Fatalf("%s out_json no longer parses: %v", tag, err)
		}
		if out["detour"] != detour {
			t.Errorf("%s detour = %v, want %s", tag, out["detour"], detour)
		}
		if tag == "ss-b" && out["method"] != "aes-128-gcm" {
			t.Errorf("ss-b lost the rest of its config: %v", out)
		}
	}
	if riders, _ := clientDetourRiders(db, target.Id, "stls-in"); len(riders) != 0 {
		t.Errorf("riders still on the old tag: %v", riders)
	}
}

// Deleting an inbound something rides is refused, naming the riders: the
// detour would be left naming nothing, and the subscription would quietly
// serve the rider dialling its own port. The check runs before the core is
// touched, so it needs no core here.
func TestDeleteRefusedWhileRidden(t *testing.T) {
	db := newTestDB(t)
	seedRiders(t, db)

	_, err := (&InboundService{}).Save(db, "del", json.RawMessage(`"stls-in"`), "", "")
	if err == nil || !strings.Contains(err.Error(), "ss-a, ss-b") {
		t.Fatalf("err = %v, want a refusal naming ss-a, ss-b", err)
	}
	var n int64
	db.Model(model.Inbound{}).Where("tag = ?", "stls-in").Count(&n)
	if n != 1 {
		t.Errorf("the ridden inbound was deleted anyway")
	}
}

// A detour no subscription can honour is refused at save: one naming an
// inbound with no client config, or the inbound itself. One naming no inbound
// at all is the operator's own -- it may name an external link's outbound --
// and is let through.
func TestCheckClientDetour(t *testing.T) {
	db := newTestDB(t)
	seedRiders(t, db)
	v2 := model.Inbound{Type: "shadowtls", Tag: "stls-v2", Options: json.RawMessage(`{}`),
		OutJson: json.RawMessage(`{"tls":{"enabled":true}}`)}
	if err := db.Create(&v2).Error; err != nil {
		t.Fatal(err)
	}

	ss := func(detour string) *model.Inbound {
		return &model.Inbound{Id: 100, Type: "shadowsocks", Tag: "ss-new", Enable: true,
			OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"` + detour + `"}`)}
	}
	for _, tt := range []struct {
		detour string
		ok     bool
	}{
		{"stls-in", true},
		{"relay-node", true},
		{"stls-v2", false},
		{"ss-new", false},
	} {
		err := checkClientDetour(db, ss(tt.detour), "ss-new")
		if (err == nil) != tt.ok {
			t.Errorf("detour %s: err = %v, want ok %v", tt.detour, err, tt.ok)
		}
	}

	// From the other side: the ridden ShadowTLS moved back to v2, which
	// FillOutJson empties to its tls block, is refused while ridden.
	downgraded := &model.Inbound{Id: 1, Type: "shadowtls", Tag: "stls-in", Enable: true,
		OutJson: json.RawMessage(`{"tls":{"enabled":true}}`)}
	if err := checkClientDetour(db, downgraded, "stls-in"); err == nil || !strings.Contains(err.Error(), "ss-a, ss-b") {
		t.Errorf("err = %v, want a refusal naming the riders", err)
	}
	if err := checkClientDetour(db, &model.Inbound{Id: v2.Id, Type: "shadowtls", Tag: "stls-v2", Enable: true,
		OutJson: v2.OutJson}, "stls-v2"); err != nil {
		t.Errorf("a v2 nothing rides has to stay editable: %v", err)
	}
}

// saveDetourDB is newTestDB plus what InboundService.Save reads on its way --
// tagFreeAcross checks the endpoints sharing the inbound namespace -- with a
// core that is not running, so Save takes its database-only path end to end.
func saveDetourDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&model.Endpoint{}, &model.Outbound{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	previous := corePtr
	corePtr = core.NewCore()
	t.Cleanup(func() { corePtr = previous })
	return db
}

func saveInbound(db *gorm.DB, act string, payload string) error {
	_, err := (&InboundService{}).Save(db, act, json.RawMessage(payload), "", "panel.example.com")
	return err
}

func detourStored(t *testing.T, db *gorm.DB, tag string) interface{} {
	t.Helper()
	var row model.Inbound
	if err := db.Where("tag = ?", tag).First(&row).Error; err != nil {
		t.Fatalf("read %s: %v", tag, err)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(row.OutJson, &out)
	return out["detour"]
}

const stlsPayload = `{"id":%d,"type":"shadowtls","tag":%q,"listen_port":443,"version":%d,` +
	`"handshake":{"server":"www.example.com","server_port":443},"out_json":{},"addrs":[]}`

// Renaming a ridden inbound through Save carries its riders along.
func TestSaveRenameCarriesRiders(t *testing.T) {
	db := saveDetourDB(t)
	target := seedRiders(t, db)

	if err := saveInbound(db, "edit", fmt.Sprintf(stlsPayload, target.Id, "stls-443", 3)); err != nil {
		t.Fatalf("rename: %v", err)
	}
	for tag, want := range map[string]string{"ss-a": "stls-443", "ss-b": "stls-443", "ss-other": "elsewhere"} {
		if got := detourStored(t, db, tag); got != want {
			t.Errorf("%s detour = %v, want %s", tag, got, want)
		}
	}
}

// Moving a ridden ShadowTLS back to v2 is refused through Save, before
// anything is written.
func TestSaveRefusesDowngradeWhileRidden(t *testing.T) {
	db := saveDetourDB(t)
	target := seedRiders(t, db)

	err := saveInbound(db, "edit", fmt.Sprintf(stlsPayload, target.Id, "stls-in", 2))
	if err == nil || !strings.Contains(err.Error(), "ss-a, ss-b") {
		t.Fatalf("err = %v, want a refusal naming the riders", err)
	}
	var row model.Inbound
	db.First(&row, target.Id)
	if readClientConfig(row.OutJson).Type == "" {
		t.Errorf("the refused save still emptied the client config: %s", row.OutJson)
	}

	// Renamed in the same save: the riders still know it by the old tag, and
	// that is the one they have to be found by.
	err = saveInbound(db, "edit", fmt.Sprintf(stlsPayload, target.Id, "stls-new", 2))
	if err == nil || !strings.Contains(err.Error(), "ss-a, ss-b") {
		t.Fatalf("rename + downgrade: err = %v, want a refusal naming the riders", err)
	}
	if got := detourStored(t, db, "ss-a"); got != "stls-in" {
		t.Errorf("ss-a detour = %v, want it untouched by the refused save", got)
	}
}

// A client config naming an inbound with no client config is refused through
// Save, and a rename refused at the core-free stage leaves the riders alone.
func TestSaveRefusesDetourToV2(t *testing.T) {
	db := saveDetourDB(t)
	seedRiders(t, db)
	v2 := model.Inbound{Type: "shadowtls", Tag: "stls-v2", Options: json.RawMessage(`{}`),
		OutJson: json.RawMessage(`{"tls":{"enabled":true}}`)}
	if err := db.Create(&v2).Error; err != nil {
		t.Fatal(err)
	}
	var ss model.Inbound
	db.Where("tag = ?", "ss-a").First(&ss)

	err := saveInbound(db, "edit", fmt.Sprintf(`{"id":%d,"type":"shadowsocks","tag":"ss-a","listen_port":20000,`+
		`"method":"aes-128-gcm","out_json":{"detour":"stls-v2"},"addrs":[]}`, ss.Id))
	if err == nil || !strings.Contains(err.Error(), "stls-v2") {
		t.Fatalf("err = %v, want a refusal naming stls-v2", err)
	}
	if got := detourStored(t, db, "ss-a"); got != "stls-in" {
		t.Errorf("ss-a detour = %v, want it untouched", got)
	}
}

// One row whose out_json is not JSON must not take the detour queries down
// with it: they run on every delete and rename.
func TestDetourQueriesSurviveMalformedRow(t *testing.T) {
	db := saveDetourDB(t)
	target := seedRiders(t, db)
	if err := db.Exec("UPDATE inbounds SET out_json = ? WHERE tag = ?", "not json", "ss-other").Error; err != nil {
		t.Fatal(err)
	}

	riders, err := clientDetourRiders(db, target.Id, "stls-in")
	if err != nil {
		t.Fatalf("riders: %v", err)
	}
	if want := []string{"ss-a", "ss-b"}; !reflect.DeepEqual(riders, want) {
		t.Errorf("riders = %v, want %v", riders, want)
	}
	if err := saveInbound(db, "edit", fmt.Sprintf(stlsPayload, target.Id, "stls-443", 3)); err != nil {
		t.Fatalf("rename with a malformed row present: %v", err)
	}
	if got := detourStored(t, db, "ss-a"); got != "stls-443" {
		t.Errorf("ss-a detour = %v, want stls-443", got)
	}
}

// One decode for both checks: a field of the wrong type reads as empty without
// taking the other field with it, and anything that is not an object reads as
// no client config at all.
func TestReadClientConfig(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want clientConfigHead
	}{
		{`{"type":"shadowsocks","detour":"stls-in"}`, clientConfigHead{"shadowsocks", "stls-in"}},
		{`{"type":"shadowsocks","detour":7}`, clientConfigHead{Type: "shadowsocks"}},
		{`{"type":3,"detour":"stls-in"}`, clientConfigHead{Detour: "stls-in"}},
		{`{"tls":{"enabled":true}}`, clientConfigHead{}},
		{`null`, clientConfigHead{}},
		{`[1,2]`, clientConfigHead{}},
		{`not json`, clientConfigHead{}},
		{``, clientConfigHead{}},
	} {
		if got := readClientConfig(json.RawMessage(tt.raw)); got != tt.want {
			t.Errorf("readClientConfig(%s) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}

// Switching an inbound off is refused while an enabled client config rides
// it -- the rider would dial its own, often loopback-only, port -- and a rider
// cannot be switched on over a disabled target. Disabled riders do not count.
func TestCheckClientDetourWithDisabledInbounds(t *testing.T) {
	db := newTestDB(t)
	target := seedRiders(t, db)

	off := &model.Inbound{Id: target.Id, Type: "shadowtls", Tag: "stls-in", Enable: false,
		OutJson: target.OutJson}
	if err := checkClientDetour(db, off, "stls-in"); err == nil || !strings.Contains(err.Error(), "ss-a, ss-b") {
		t.Errorf("disabling a ridden inbound: err = %v, want a refusal naming the riders", err)
	}

	// With both riders off, the target may go off too.
	if err := db.Model(model.Inbound{}).Where("tag IN ?", []string{"ss-a", "ss-b"}).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := checkClientDetour(db, off, "stls-in"); err != nil {
		t.Errorf("disabling an inbound only disabled configs ride: %v", err)
	}
	if err := db.Model(model.Inbound{}).Where("tag = ?", "stls-in").Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}

	// And a rider cannot come back on over the disabled target...
	rider := &model.Inbound{Type: "shadowsocks", Tag: "ss-a", Enable: true,
		OutJson: json.RawMessage(`{"type":"shadowsocks","detour":"stls-in"}`)}
	if err := db.Model(model.Inbound{}).Select("id").Where("tag = ?", "ss-a").Scan(&rider.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := checkClientDetour(db, rider, "ss-a"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("enabling a rider of a disabled inbound: err = %v, want a refusal", err)
	}
	// ...though it can be edited while it stays off.
	rider.Enable = false
	if err := checkClientDetour(db, rider, "ss-a"); err != nil {
		t.Errorf("editing a disabled rider of a disabled inbound: %v", err)
	}
}
