package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// Adding a protocol to hasUser makes every client whose config predates it
// return NULL from json_extract, and Scan into []string fails the whole query
// on that rather than skipping the row -- so the first inbound of the new type
// comes up with no users at all, and the panel only says so in a log line.
// Found on a real panel: every client there predated snell.
func TestFetchUsersSkipsClientsWithoutTheProtocol(t *testing.T) {
	db := newTestDB(t)
	var svc InboundService

	seed := []model.Client{
		{
			Name: "has-snell", Enable: true,
			Config:   json.RawMessage(`{"snell":{"name":"has-snell","userkey":"key-1"},"vless":{"name":"has-snell","uuid":"u1"}}`),
			Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		},
		{
			// Written before snell existed.
			Name: "no-snell", Enable: true,
			Config:   json.RawMessage(`{"vless":{"name":"no-snell","uuid":"u2"}}`),
			Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		},
		{
			// The key is there but empty, which is not valid JSON for a user.
			Name: "empty-snell", Enable: true,
			Config:   json.RawMessage(`{"snell":""}`),
			Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		},
	}
	for _, client := range seed {
		if err := db.Create(&client).Error; err != nil {
			t.Fatalf("seed %s: %v", client.Name, err)
		}
	}

	users, err := svc.fetchUsers(db, "snell", "1=1", map[string]interface{}{})
	if err != nil {
		t.Fatalf("a client without the protocol must not fail the query: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected only the client that has one, got %d: %v", len(users), users)
	}
	var user map[string]any
	if err := json.Unmarshal(users[0], &user); err != nil {
		t.Fatalf("the user must be valid JSON: %v", err)
	}
	if user["userkey"] != "key-1" {
		t.Errorf("unexpected user: %v", user)
	}
}

// The initUsers request field used to be split on commas and joined straight
// back -- a round trip that changed nothing -- and formatted into the WHERE
// clause, so it reached SQLite verbatim.
func TestInitUsersRejectsInjectedClientIds(t *testing.T) {
	db := newTestDB(t)
	var svc InboundService
	for _, name := range []string{"alice", "bob"} {
		if err := db.Create(&model.Client{
			Name: name, Enable: true,
			Config:   json.RawMessage(`{"vless":{"name":"` + name + `","uuid":"u"}}`),
			Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	inbound := []byte(`{"type":"vless","tag":"v","listen":"::","listen_port":443}`)

	// Would have become `id IN (1) OR 1=1 --)` and selected every client.
	_, err := svc.initUsers(db, inbound, "1) OR 1=1 --", "vless")
	if err == nil {
		t.Fatal("an injected client id must be rejected, not run")
	}
	if !strings.Contains(err.Error(), "invalid client id") {
		t.Errorf("unexpected error: %v", err)
	}

	// The honest case still selects exactly the ids it was given.
	out, err := svc.initUsers(db, inbound, "1", "vless")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Users) != 1 || decoded.Users[0]["name"] != "alice" {
		t.Errorf("want only client 1, got %v", decoded.Users)
	}

	// An empty field is the normal "no users yet" case, not an error.
	if _, err := svc.initUsers(db, inbound, "", "vless"); err != nil {
		t.Errorf("an empty initUsers must be accepted: %v", err)
	}
}
