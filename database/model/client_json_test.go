package model

import (
	"encoding/json"
	"testing"
)

// One client whose columns are not JSON used to fail the encoding of every
// list it was in. It encodes now, as a client with no config, no links and no
// inbounds, and the clients beside it are untouched.
func TestClientMarshalsDamagedColumnsAsEmpty(t *testing.T) {
	clients := []Client{
		{Id: 1, Name: "good", Config: json.RawMessage(`{"vless":{"uuid":"u"}}`), Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`)},
		{Id: 2, Name: "broken", Config: json.RawMessage(`{not json`), Inbounds: json.RawMessage(`oops`), Links: json.RawMessage(`[`)},
		{Id: 3, Name: "projected"},
	}
	out, err := json.Marshal(clients)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if string(got[0]["config"]) != `{"vless":{"uuid":"u"}}` || string(got[0]["inbounds"]) != `[1,2]` {
		t.Fatalf("good client changed: %s", out)
	}
	if _, ok := got[1]["config"]; ok {
		t.Fatalf("damaged config written: %s", got[1]["config"])
	}
	if _, ok := got[1]["links"]; ok {
		t.Fatalf("damaged links written: %s", got[1]["links"])
	}
	if string(got[1]["inbounds"]) != `[]` {
		t.Fatalf("damaged inbounds written as %s, want []", got[1]["inbounds"])
	}
	if string(got[1]["name"]) != `"broken"` {
		t.Fatalf("damaged client lost its name: %s", out)
	}
	// A list projection has no config at all; that stays as it was.
	if string(got[2]["inbounds"]) != `null` {
		t.Fatalf("projected client inbounds %s, want null", got[2]["inbounds"])
	}
	// Through a pointer too, as the services hand them out.
	if _, err := json.Marshal(&clients); err != nil {
		t.Fatalf("marshal pointer: %v", err)
	}
}
