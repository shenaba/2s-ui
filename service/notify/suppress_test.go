package notify

import (
	"strconv"
	"testing"
	"time"
)

func mustSend(t *testing.T, s *Suppressor, e Event, now time.Time, want bool, what string) Decision {
	t.Helper()
	d := s.Decide(e, now)
	if d.Send != want {
		t.Fatalf("%s: Send = %v, want %v", what, d.Send, want)
	}
	return d
}

// State events are delivered on a transition and nowhere else. Without this a
// node that stays unreachable produces one alert every 5s (NodesJob's cadence)
// and a core that will not start produces one every 5s too (checkCoreJob's),
// which is the fastest way to get the whole feature muted.
func TestSuppressStateTransitions(t *testing.T) {
	s := NewSuppressor()
	now := time.Unix(1_700_000_000, 0)

	// An unseen subject counts as healthy, so a panel whose nodes are all fine
	// says nothing at startup.
	mustSend(t, s, Event{Kind: NodeUp, Subject: "n1"}, now, false, "first NodeUp on a healthy panel")

	mustSend(t, s, Event{Kind: NodeDown, Subject: "n1"}, now, true, "first NodeDown")
	mustSend(t, s, Event{Kind: NodeDown, Subject: "n1"}, now.Add(5*time.Second), false, "repeated NodeDown")
	mustSend(t, s, Event{Kind: NodeDown, Subject: "n1"}, now.Add(time.Hour), false, "NodeDown an hour later")

	mustSend(t, s, Event{Kind: NodeUp, Subject: "n1"}, now.Add(2*time.Hour), true, "recovery")
	mustSend(t, s, Event{Kind: NodeUp, Subject: "n1"}, now.Add(3*time.Hour), false, "repeated NodeUp")

	// Subjects are tracked independently.
	mustSend(t, s, Event{Kind: NodeDown, Subject: "n2"}, now.Add(3*time.Hour), true, "a second node going down")

	// The core has its own family, so a node transition cannot mask it.
	mustSend(t, s, Event{Kind: CoreCrash}, now, true, "first CoreCrash")
	mustSend(t, s, Event{Kind: CoreCrash}, now.Add(5*time.Second), false, "CoreCrash on the next restart attempt")
	mustSend(t, s, Event{Kind: CoreUp}, now.Add(time.Minute), true, "core recovery")
}

// The three login kinds each get different treatment, and the differences are
// the point: a ban must always be delivered, a success is infrequent enough to
// deliver as-is, and failures are the only ones that can flood.
func TestSuppressLoginTiering(t *testing.T) {
	s := NewSuppressor()
	now := time.Unix(1_700_000_000, 0)

	// Bans bypass rate limiting entirely. Somebody working through a password
	// list is exactly when withholding the alert would hurt most.
	for i := 0; i < 5; i++ {
		mustSend(t, s, Event{Kind: LoginBanned, Subject: "1.2.3.4"}, now.Add(time.Duration(i)*time.Second),
			true, "LoginBanned #"+strconv.Itoa(i))
	}

	// Successes always go out; an operator has to hear about a sign-in they did
	// not perform.
	mustSend(t, s, Event{Kind: LoginSuccess, Subject: "admin"}, now, true, "first LoginSuccess")
	mustSend(t, s, Event{Kind: LoginSuccess, Subject: "admin"}, now.Add(time.Second), true, "second LoginSuccess")

	// Failures alert immediately, then fold into a count.
	d := mustSend(t, s, Event{Kind: LoginFailed, Subject: "5.6.7.8"}, now, true, "first failure")
	if d.Failures != 1 {
		t.Fatalf("first failure: Failures = %d, want 1", d.Failures)
	}
	for i := 1; i <= 4; i++ {
		mustSend(t, s, Event{Kind: LoginFailed, Subject: "5.6.7.8"},
			now.Add(time.Duration(i)*time.Second), false, "failure inside the window")
	}
	// Past the window the next attempt reports itself plus the four swallowed.
	d = mustSend(t, s, Event{Kind: LoginFailed, Subject: "5.6.7.8"},
		now.Add(loginCooldown+time.Second), true, "failure after the window")
	if d.Failures != 5 {
		t.Fatalf("after the window: Failures = %d, want 5 (4 swallowed + this one)", d.Failures)
	}

	// A different source has its own budget, so spreading attempts across IPs
	// does not buy silence on any one of them.
	d = mustSend(t, s, Event{Kind: LoginFailed, Subject: "9.9.9.9"},
		now.Add(time.Second), true, "failure from another IP")
	if d.Failures != 1 {
		t.Fatalf("other IP: Failures = %d, want 1", d.Failures)
	}
}

// Expiry warnings ride on DepleteJob, which runs every minute; without a long
// cooldown a client three days from expiry would be reported 4320 times.
func TestSuppressCooldowns(t *testing.T) {
	s := NewSuppressor()
	now := time.Unix(1_700_000_000, 0)

	mustSend(t, s, Event{Kind: ClientExpiring, Subject: "alice"}, now, true, "first expiry warning")
	mustSend(t, s, Event{Kind: ClientExpiring, Subject: "alice"}, now.Add(time.Minute), false, "a minute later")
	mustSend(t, s, Event{Kind: ClientExpiring, Subject: "alice"}, now.Add(23*time.Hour), false, "23h later")
	mustSend(t, s, Event{Kind: ClientExpiring, Subject: "alice"}, now.Add(expiringCooldown+time.Second), true, "the next day")

	mustSend(t, s, Event{Kind: CPUHigh, Subject: Host()}, now, true, "first CPU alert")
	mustSend(t, s, Event{Kind: CPUHigh, Subject: Host()}, now.Add(time.Minute), false, "CPU still high a minute later")
	mustSend(t, s, Event{Kind: CPUHigh, Subject: Host()}, now.Add(metricCooldown+time.Second), true, "CPU alert after the cooldown")

	// Depletion is already one event per DepleteJob pass, and a disabled client
	// is not selected again, so it passes straight through.
	mustSend(t, s, Event{Kind: ClientDepleted, Subject: "batch"}, now, true, "first depletion")
	mustSend(t, s, Event{Kind: ClientDepleted, Subject: "batch"}, now.Add(time.Minute), true, "next depletion pass")
}

// The cooldown map is keyed by client name and source IP, so without pruning it
// grows without bound in a process expected to run for months.
func TestSuppressPrune(t *testing.T) {
	s := NewSuppressor()
	now := time.Unix(1_700_000_000, 0)

	for i := 0; i < 500; i++ {
		s.Decide(Event{Kind: LoginFailed, Subject: "10.0.0." + strconv.Itoa(i)}, now)
	}
	if got := len(s.until); got != 500 {
		t.Fatalf("expected 500 live cooldowns, got %d", got)
	}
	// Swallow one more attempt per IP so the failure tallies are populated too.
	for i := 0; i < 500; i++ {
		s.Decide(Event{Kind: LoginFailed, Subject: "10.0.0." + strconv.Itoa(i)}, now.Add(time.Second))
	}
	if len(s.failures) == 0 {
		t.Fatal("expected swallowed failures to be tallied")
	}

	// Anything whose cooldown has elapsed can no longer change a decision, so
	// it must not still be held.
	s.Decide(Event{Kind: LoginFailed, Subject: "fresh"}, now.Add(metricCooldown+time.Minute))
	if got := len(s.until); got != 1 {
		t.Fatalf("after pruning, expected only the fresh entry, got %d", got)
	}
	if got := len(s.failures); got != 0 {
		t.Fatalf("after pruning, expected no stale failure tallies, got %d", got)
	}
}
