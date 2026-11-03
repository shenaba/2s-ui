package service

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// Issue #268. The driver stores json.RawMessage as BLOB, and SQLite reads a
// BLOB whose header fits its length as JSONB: every 6-byte array ("[1, 2]",
// "[1,23]", "[1234]") and 8-byte object failed with "malformed JSON". It failed
// the whole statement, so one such client broke every query that scans the
// table -- config generation among them, which kept the core from starting.
func TestClientJSONColumnsReadAsTextWhateverTheirLength(t *testing.T) {
	db := newTestDB(t)
	var svc InboundService

	vless := func(name string) json.RawMessage {
		return json.RawMessage(`{"vless":{"name":"` + name + `","uuid":"u-` + name + `"}}`)
	}
	seed := []model.Client{
		// Each of the first three is exactly 6 bytes, the length "[" claims.
		{Name: "spaced", Inbounds: json.RawMessage(`[1, 2]`)},
		{Name: "compact", Inbounds: json.RawMessage(`[1,23]`)},
		{Name: "elsewhere", Inbounds: json.RawMessage(`[1234]`)},
		{Name: "indented", Inbounds: json.RawMessage("[\n  1,\n  2\n]")},
		{Name: "short", Inbounds: json.RawMessage(`[1,2]`)},
		// An 8-byte config, the length "{" claims, with no vless block: it
		// must be skipped, not fail the query for everyone else.
		{Name: "no-vless", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"a":12}`)},
	}
	for _, client := range seed {
		client.Enable = true
		client.Links = json.RawMessage(`[]`)
		if client.Config == nil {
			client.Config = vless(client.Name)
		}
		if err := db.Create(&client).Error; err != nil {
			t.Fatalf("seed %s: %v", client.Name, err)
		}
	}
	var stored string
	if err := db.Raw("SELECT typeof(inbounds) FROM clients WHERE name = 'spaced'").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != "blob" {
		t.Fatalf("the test needs the column stored as a blob to mean anything, got %s", stored)
	}

	inbound := model.Inbound{
		Type: "vless", Tag: "vless-in",
		Options: json.RawMessage(`{"listen":"::","listen_port":443}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if inbound.Id != 1 {
		t.Fatalf("seeded clients name inbound 1, got id %d", inbound.Id)
	}
	want := []string{"compact", "indented", "short", "spaced"}

	// The config the core starts from.
	configs, err := svc.GetAllConfig(db)
	if err != nil {
		t.Fatalf("config generation: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("want one inbound config, got %d", len(configs))
	}
	var decoded struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.Unmarshal(configs[0], &decoded); err != nil {
		t.Fatal(err)
	}
	var users []string
	for _, user := range decoded.Users {
		users = append(users, user.Name)
	}
	assertNames(t, "config users", users, want)

	// The in-place update's list of whom to keep connected.
	keep, err := svc.enabledClientNames(db, inbound.Id)
	if err != nil {
		t.Fatalf("enabled client names: %v", err)
	}
	var kept []string
	for name := range keep {
		kept = append(kept, name)
	}
	assertNames(t, "enabled client names", kept, append(want, "no-vless"))

	// Inbound edit, then delete: both find their clients the same way.
	if err := svc.ClientService.UpdateLinksByInboundChange(db, &[]model.Inbound{inbound}, "example.com", ""); err != nil {
		t.Fatalf("links on inbound change: %v", err)
	}
	if err := svc.ClientService.UpdateClientsOnInboundDelete(db, inbound.Id, inbound.Tag); err != nil {
		t.Fatalf("clients on inbound delete: %v", err)
	}
	var spaced model.Client
	if err := db.Where("name = ?", "spaced").First(&spaced).Error; err != nil {
		t.Fatal(err)
	}
	var left []uint
	if err := json.Unmarshal(spaced.Inbounds, &left); err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0] != 2 {
		t.Errorf("deleting inbound 1 must leave [2] on the client, got %v", left)
	}
}

func assertNames(t *testing.T, what string, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Errorf("%s: got %v, want %v", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: got %v, want %v", what, got, want)
			return
		}
	}
}
