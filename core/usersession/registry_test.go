package usersession

import (
	"errors"
	"testing"
	"time"
)

type fakeCloser struct {
	closed bool
}

func (c *fakeCloser) Close() error {
	c.closed = true
	return nil
}

func keep(users ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(users))
	for _, user := range users {
		set[user] = struct{}{}
	}
	return set
}

func TestCloseUsersClosesTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("gone", "1.2.3.4:1000")

	if cut := r.CloseUsers(keep("stays")); cut != 1 {
		t.Fatalf("cut = %d, want 1", cut)
	}
	if !conn.closed {
		t.Error("a tracked session must be closed, not muted")
	}
	// Nothing is left muted: the session is gone, so the address is free for
	// whoever connects from it next.
	if !r.Allowed("1.2.3.4:1000") {
		t.Error("address stayed muted after its session was closed")
	}
}

func TestCloseUsersMutesUntrackedSession(t *testing.T) {
	r := NewRegistry()
	// No Track: this is the QUIC shape, where the session has no closer we can
	// reach. Muting is the only thing left.
	r.Bind("gone", "1.2.3.4:1000")

	if cut := r.CloseUsers(keep("stays")); cut != 1 {
		t.Fatalf("cut = %d, want 1", cut)
	}
	if r.Allowed("1.2.3.4:1000") {
		t.Error("a session with no closer must be muted")
	}
	// An address nobody authenticated from is not affected.
	if !r.Allowed("5.6.7.8:2000") {
		t.Error("an unrelated address must stay allowed")
	}
}

func TestCloseUsersLeavesUnauthenticatedSourceAlone(t *testing.T) {
	r := NewRegistry()
	// Bound with no user name: the connection reached the handler before
	// authentication produced one. It belongs to nobody, so a removal has
	// nothing to say about it.
	r.Bind("", "1.2.3.4:1000")

	if cut := r.CloseUsers(keep("stays")); cut != 0 {
		t.Fatalf("cut = %d, want 0", cut)
	}
	if !r.Allowed("1.2.3.4:1000") {
		t.Error("an unauthenticated source must not be muted")
	}
}

func TestCloseUsersLiftsMuteWhenUserComesBack(t *testing.T) {
	r := NewRegistry()
	r.Bind("flip", "1.2.3.4:1000")
	r.CloseUsers(keep())
	if r.Allowed("1.2.3.4:1000") {
		t.Fatal("precondition: the session should be muted")
	}

	// Re-enabling has to take effect now, not when the block times out.
	r.CloseUsers(keep("flip"))
	if !r.Allowed("1.2.3.4:1000") {
		t.Error("re-enabling a user must lift the mute immediately")
	}
}

func TestKickClosesTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("noisy", "1.2.3.4:1000")

	if kicked := r.KickUserSessions("noisy"); kicked != 1 {
		t.Fatalf("kicked = %d, want 1", kicked)
	}
	if !conn.closed {
		t.Error("a tracked session must be closed on kick")
	}
	if !r.Allowed("1.2.3.4:1000") {
		t.Error("a kicked client must be free to reconnect at once")
	}
}

func TestKickMuteLiftsAfterQuietWindow(t *testing.T) {
	r := NewRegistry()
	r.Bind("noisy", "1.2.3.4:1000")
	r.KickUserSessions("noisy")

	// The kicked session keeps trying and keeps being refused.
	for i := 0; i < 3; i++ {
		if r.Allowed("1.2.3.4:1000") {
			t.Fatalf("attempt %d was let through while the session kept trying", i)
		}
	}

	// Once it has been quiet for the window, the next attempt is a new session.
	r.access.Lock()
	r.blocked["1.2.3.4:1000"].lastAttempt = time.Now().Add(-kickQuietWindow - time.Second)
	r.access.Unlock()

	if !r.Allowed("1.2.3.4:1000") {
		t.Error("a kick must not turn into a lockout")
	}
}

func TestRemovalMuteDoesNotLiftOnQuiet(t *testing.T) {
	r := NewRegistry()
	r.Bind("gone", "1.2.3.4:1000")
	r.CloseUsers(keep())

	// Same quiet gap that would lift a kick. A removal is not a kick: the user
	// is no longer on the inbound, so there is nothing to let back in and the
	// mute holds until the backstop.
	r.access.Lock()
	r.blocked["1.2.3.4:1000"].lastAttempt = time.Now().Add(-kickQuietWindow - time.Second)
	r.access.Unlock()

	if r.Allowed("1.2.3.4:1000") {
		t.Error("a removal mute must not lift just because the session went quiet")
	}
}

func TestRemovalMuteLiftsAtBackstop(t *testing.T) {
	r := NewRegistry()
	r.Bind("gone", "1.2.3.4:1000")
	r.CloseUsers(keep())

	r.access.Lock()
	r.blocked["1.2.3.4:1000"].at = time.Now().Add(-blockTimeout - time.Second)
	r.access.Unlock()

	if !r.Allowed("1.2.3.4:1000") {
		t.Error("the backstop must free an address whose session is long dead")
	}
}

func TestUntrackForgetsSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("gone", "1.2.3.4:1000")
	r.Untrack("1.2.3.4:1000")

	if cut := r.CloseUsers(keep()); cut != 0 {
		t.Fatalf("cut = %d, want 0 -- the session was already gone", cut)
	}
	if conn.closed {
		t.Error("an untracked session must not be closed again")
	}
}

// The QUIC inbounds never Untrack, so Bind is the only place that can drop what
// they leave behind.
func TestSweepDropsIdleSources(t *testing.T) {
	r := NewRegistry()
	r.Bind("old", "1.2.3.4:1000")

	r.access.Lock()
	r.sources["1.2.3.4:1000"].lastSeen = time.Now().Add(-idleTimeout - time.Minute)
	r.lastSweep = time.Now().Add(-idleTimeout - time.Minute)
	r.access.Unlock()

	r.Bind("fresh", "5.6.7.8:2000")

	r.access.Lock()
	_, stale := r.sources["1.2.3.4:1000"]
	count := len(r.sources)
	r.access.Unlock()

	if stale {
		t.Error("an idle source must be swept")
	}
	if count != 1 {
		t.Errorf("len(sources) = %d, want 1 (the fresh one)", count)
	}
}

func TestSweepIsRateLimited(t *testing.T) {
	r := NewRegistry()
	r.Bind("old", "1.2.3.4:1000")

	r.access.Lock()
	r.sources["1.2.3.4:1000"].lastSeen = time.Now().Add(-idleTimeout - time.Minute)
	r.access.Unlock()

	// lastSweep is fresh (NewRegistry set it), so this Bind must not walk the
	// map -- the whole point is that the data plane does not pay per call.
	r.Bind("fresh", "5.6.7.8:2000")

	r.access.Lock()
	_, stale := r.sources["1.2.3.4:1000"]
	r.access.Unlock()

	if !stale {
		t.Error("sweep ran on a Bind inside the rate limit window")
	}
}

func TestRejectReportsClose(t *testing.T) {
	conn := &fakeCloser{}
	var reported error
	Reject(conn, func(err error) { reported = err })

	if !conn.closed {
		t.Error("Reject must close the connection")
	}
	if !errors.Is(reported, ErrRemoved) {
		t.Errorf("close handler got %v, want ErrRemoved", reported)
	}
}

func TestRejectWithoutCloseHandler(t *testing.T) {
	conn := &fakeCloser{}
	Reject(conn, nil)
	if !conn.closed {
		t.Error("Reject must close the connection even with no close handler")
	}
}

// A tracked session is alive however quiet it has been: lastSeen only moves
// when it opens another connection, so one carrying a single long-lived stream
// looks idle. Sweeping it would discard the closer, and the removal that came
// next would find nothing to cut -- which is issue #175 all over again.
func TestSweepKeepsTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("quiet", "1.2.3.4:1000", conn)

	r.access.Lock()
	r.sources["1.2.3.4:1000"].lastSeen = time.Now().Add(-idleTimeout - time.Minute)
	r.lastSweep = time.Now().Add(-idleTimeout - time.Minute)
	r.access.Unlock()

	// Another client's traffic, which is what triggers the sweep.
	r.Bind("other", "5.6.7.8:2000")

	if cut := r.CloseUsers(keep("other")); cut != 1 {
		t.Fatalf("cut = %d, want 1 -- the idle session was swept away", cut)
	}
	if !conn.closed {
		t.Error("a tracked session must survive the sweep and stay closable")
	}
}

// Same defect, second site: CloseUsers drops idle entries before it decides
// what to cut, so an idle carrier would be skipped by the very call meant to
// close it.
func TestCloseUsersCutsIdleTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("quiet", "1.2.3.4:1000", conn)

	r.access.Lock()
	r.sources["1.2.3.4:1000"].lastSeen = time.Now().Add(-idleTimeout - time.Minute)
	r.access.Unlock()

	if cut := r.CloseUsers(keep()); cut != 1 {
		t.Fatalf("cut = %d, want 1 -- an idle tracked session was skipped", cut)
	}
	if !conn.closed {
		t.Error("an idle but tracked session must still be closed")
	}
}

// This pins the outcome -- a carrier registered this way is closed, not muted.
// The atomicity it exists for cannot be asserted here: it comes from doing both
// writes under one lock, and a sequential Bind-then-Track would pass this test
// just the same. What that split would lose is the interleaving where a
// CloseUsers lands between the two, sees a user with no closer yet, files the
// session as unclosable and mutes it -- and a mux carrier is never gated, so
// the mute would do nothing.
func TestBindAndTrackClosesRatherThanMutes(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("live", "1.2.3.4:1000", conn)

	if cut := r.CloseUsers(keep()); cut != 1 {
		t.Fatalf("cut = %d, want 1", cut)
	}
	if !conn.closed {
		t.Error("the carrier must be closed, not muted")
	}
	if !r.Allowed("1.2.3.4:1000") {
		t.Error("a closed carrier must not leave a mute behind")
	}
}

// A packet conn cannot be a mux carrier, so the router passes a nil closer. It
// must stay nil: a nil net.Conn placed in an io.Closer is not a nil interface,
// and would register as a closer that panics when the session is cut.
func TestBindAndTrackWithNilCloserMutes(t *testing.T) {
	r := NewRegistry()
	r.BindAndTrack("live", "1.2.3.4:1000", nil)

	if cut := r.CloseUsers(keep()); cut != 1 {
		t.Fatalf("cut = %d, want 1", cut)
	}
	if r.Allowed("1.2.3.4:1000") {
		t.Error("with no closer the session must be muted")
	}
}
