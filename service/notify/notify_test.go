package notify

import (
	"strings"
	"testing"
	"time"
)

// newTestNotifier builds a notifier wired to a channel instead of the real
// senders, so the publish path can be exercised without any network.
func newTestNotifier(cfg Config) (*Notifier, <-chan Event) {
	got := make(chan Event, 16)
	n := &Notifier{
		bus: NewBus(),
		sup: NewSuppressor(),
		cfg: func() Config { return cfg },
	}
	n.bus.Subscribe("capture", func(e Event) { got <- e })
	return n, got
}

func recv(t *testing.T, ch <-chan Event, what string) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return Event{}
	}
}

func expectNothing(t *testing.T, ch <-chan Event, what string) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("%s: unexpectedly delivered %s", what, e.Kind)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestPublishHonoursTheEnabledEvents(t *testing.T) {
	cfg := Config{
		Enable: true,
		Events: map[Kind]bool{NodeDown: true},
	}
	n, got := newTestNotifier(cfg)
	defer n.bus.Stop()

	n.publish(Event{Kind: NodeDown, Subject: "n1"})
	if e := recv(t, got, "an enabled kind"); e.Kind != NodeDown {
		t.Fatalf("got %s, want %s", e.Kind, NodeDown)
	}

	// NodeUp is not in the map, so the recovery is not announced.
	n.publish(Event{Kind: NodeUp, Subject: "n1"})
	expectNothing(t, got, "a kind the operator did not enable")
}

func TestPublishRespectsTheMasterSwitch(t *testing.T) {
	cfg := Config{
		Enable: false,
		Events: map[Kind]bool{NodeDown: true, LoginBanned: true},
	}
	n, got := newTestNotifier(cfg)
	defer n.bus.Stop()

	n.publish(Event{Kind: NodeDown, Subject: "n1"})
	// Even a ban, which nothing else suppresses, stays in when notifications
	// are off altogether.
	n.publish(Event{Kind: LoginBanned, Subject: "1.2.3.4"})
	expectNothing(t, got, "notifications disabled")
}

// The suppressor folds swallowed attempts into a count, and that count has to
// reach the renderer or the alert understates what happened.
func TestPublishCarriesTheFoldedFailureCount(t *testing.T) {
	cfg := Config{
		Enable: true,
		Lang:   "en",
		Events: map[Kind]bool{LoginFailed: true},
	}
	n, got := newTestNotifier(cfg)
	defer n.bus.Stop()

	base := time.Unix(1_700_000_000, 0)
	// The caller's payload, reused across the loop the way a real event source
	// would -- publish must not write through to it.
	caller := &LoginData{Username: "admin", IP: "1.2.3.4", Failures: 1}

	n.publish(Event{Kind: LoginFailed, Subject: "1.2.3.4", Data: caller, At: base})
	first := recv(t, got, "the first failure")
	if d, ok := first.Data.(*LoginData); !ok || d.Failures != 1 {
		t.Fatalf("first alert: %+v, want Failures 1", first.Data)
	}

	for i := 1; i <= 3; i++ {
		n.publish(Event{Kind: LoginFailed, Subject: "1.2.3.4", Data: caller, At: base.Add(time.Duration(i) * time.Second)})
	}
	expectNothing(t, got, "failures inside the merge window")

	n.publish(Event{Kind: LoginFailed, Subject: "1.2.3.4", Data: caller, At: base.Add(loginCooldown + time.Second)})
	merged := recv(t, got, "the merged alert")
	d, ok := merged.Data.(*LoginData)
	if !ok {
		t.Fatalf("merged alert lost its payload: %+v", merged.Data)
	}
	if d.Failures != 4 {
		t.Fatalf("merged alert reports %d failures, want 4 (3 swallowed + this one)", d.Failures)
	}
	if caller.Failures != 1 {
		t.Fatalf("publish wrote through to the caller's payload: Failures = %d", caller.Failures)
	}
	if text := Render(merged, "en"); !strings.Contains(text, "4 failed logins") {
		t.Fatalf("rendered alert does not carry the count: %q", text)
	}
}

// Every event source calls Publish unconditionally; before Start and after Stop
// it has to be a no-op rather than a nil dereference.
func TestPackagePublishIsSafeWhenNotRunning(t *testing.T) {
	Stop() // no-op, nothing was started
	Publish(Event{Kind: CoreCrash})

	Start(func() Config { return Config{} })
	Publish(Event{Kind: CoreCrash})
	Stop()
	Publish(Event{Kind: CoreCrash})
}

func TestConfigEnabledChecks(t *testing.T) {
	if (TelegramConfig{Token: "t"}).enabled() {
		t.Error("a telegram config with no chat id counts as enabled")
	}
	if (TelegramConfig{ChatIDs: []string{"1"}}).enabled() {
		t.Error("a telegram config with no token counts as enabled")
	}
	if !(TelegramConfig{Token: "t", ChatIDs: []string{"1"}}).enabled() {
		t.Error("a complete telegram config does not count as enabled")
	}
	if (SMTPConfig{Host: "h", Port: 25, From: "a@b"}).enabled() {
		t.Error("an smtp config with no recipients counts as enabled")
	}
	if !(SMTPConfig{Host: "h", Port: 25, From: "a@b", To: []string{"c@d"}}).enabled() {
		t.Error("a complete smtp config does not count as enabled")
	}
}
