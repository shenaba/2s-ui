package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// telegramStub stands in for the Bot API, recording what was sent to whom.
func telegramStub(t *testing.T) (*httptest.Server, func() map[string]string) {
	t.Helper()
	var (
		mu   sync.Mutex
		sent = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("unparsable request: %v", err)
		}
		mu.Lock()
		sent[r.FormValue("chat_id")] = r.FormValue("text")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]string, len(sent))
		for k, v := range sent {
			out[k] = v
		}
		return out
	}
}

func TestSendClientAlertsReachesEachBoundClient(t *testing.T) {
	srv, sent := telegramStub(t)
	cfg := Config{
		Lang: "en",
		// Deliberately no ChatIDs: warning customers while the operator hears
		// nothing is a legitimate setup, and this path must not require one.
		Telegram: TelegramConfig{Token: "tok", APIServer: srv.URL},
	}

	err := sendClientAlerts(cfg, Event{
		Kind: ClientDepleted,
		Data: &ClientData{
			Names: []string{"alice", "bob", "carol"},
			Targets: []ClientTarget{
				{Name: "alice", TgId: 111},
				{Name: "bob", TgId: 222},
				// A client whose binding was cleared: no chat to send to.
				{Name: "carol", TgId: 0},
			},
		},
	})
	if err != nil {
		t.Fatalf("sendClientAlerts: %v", err)
	}

	got := sent()
	if len(got) != 2 {
		t.Fatalf("sent %d messages, want 2: %v", len(got), got)
	}
	if text := got["111"]; text == "" || !strings.Contains(text, "alice") {
		t.Errorf("alice was sent %q", text)
	}
	if text := got["222"]; text == "" || !strings.Contains(text, "bob") {
		t.Errorf("bob was sent %q", text)
	}
	if _, ok := got["0"]; ok {
		t.Error("an unbound client was messaged on chat 0")
	}
}

func TestSendClientAlertsStaysSilentWithoutTargets(t *testing.T) {
	srv, sent := telegramStub(t)
	cfg := Config{Lang: "en", Telegram: TelegramConfig{Token: "tok", APIServer: srv.URL}}

	cases := []struct {
		what  string
		event Event
	}{
		{"no targets", Event{Kind: ClientExpiring, Data: &ClientData{DaysLeft: 3}}},
		{"no payload", Event{Kind: ClientExpiring}},
		{"wrong payload type", Event{Kind: ClientExpiring, Data: &NodeData{}}},
		{
			// The operator's alert, which happens to carry a binding. Nothing
			// here concerns the client, so nothing is sent.
			"an operator kind",
			Event{Kind: NodeDown, Data: &ClientData{Targets: []ClientTarget{{Name: "alice", TgId: 111}}}},
		},
	}
	for _, c := range cases {
		if err := sendClientAlerts(cfg, c.event); err != nil {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %d messages, want none: %v", len(got), got)
	}
}
