package usersession

import (
	"errors"
	"strconv"
	"testing"
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

// What the QUIC inbounds turn a CloseUsers result into. Asking for a restart
// when nothing was left connected would disconnect a whole inbound on every
// ordinary save; not asking when something was leaves the removed user online.
func TestResultRestartRequired(t *testing.T) {
	for _, c := range []struct {
		result Result
		want   error
	}{
		{Result{}, nil},
		{Result{Cut: 3}, nil},
		{Result{Unclosable: 1}, ErrRestartRequired},
		{Result{Cut: 2, Unclosable: 1}, ErrRestartRequired},
	} {
		if got := c.result.RestartRequired(); !errors.Is(got, c.want) {
			t.Errorf("%+v.RestartRequired() = %v, want %v", c.result, got, c.want)
		}
	}
}

func TestCloseUsersClosesTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("gone", "1.2.3.4:1000")

	got := r.CloseUsers(keep("stays"))
	if got != (Result{Cut: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Cut:1}", got)
	}
	if !conn.closed {
		t.Error("a tracked session must be closed")
	}
}

// The QUIC shape: nothing was tracked, so there is no session to cut. Saying so
// is the whole contract -- it is what makes the inbound ask to be rebuilt.
func TestCloseUsersReportsUntrackedUser(t *testing.T) {
	r := NewRegistry()
	r.Bind("gone", "1.2.3.4:1000")

	got := r.CloseUsers(keep("stays"))
	if got != (Result{Unclosable: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Unclosable:1}", got)
	}
}

// THE regression this package exists for, and the one an earlier revision got
// wrong twice. A QUIC session that has routed nothing for a long time is not a
// session that ended: quic-go keeps it alive with a 10s PING, and the streams
// it opens later never re-check the user table. Nothing may therefore expire a
// user out of the seen set -- only their removal takes them out of it.
//
// There is no clock to advance here, which is the point: the fix was to delete
// the notion of an idle session rather than to tune it. What stands in for
// elapsed time is unbounded unrelated activity.
func TestIdleUserIsNeverForgotten(t *testing.T) {
	r := NewRegistry()
	r.Bind("idle", "1.2.3.4:1000")

	// Lots of other traffic and lots of ordinary saves, none of which concern
	// "idle" -- who sends nothing at all for the whole stretch.
	for i := 0; i < 500; i++ {
		r.Bind("busy", "5.6.7.8:"+strconv.Itoa(2000+i))
		r.CloseUsers(keep("idle", "busy"))
	}

	got := r.CloseUsers(keep("busy"))
	if got != (Result{Unclosable: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Unclosable:1} -- an idle user was forgotten", got)
	}
}

// The seen set is keyed by user, not by address, and that is what makes it safe
// to keep forever: it is bounded by the client count, not by how many sessions
// or source ports a client burns through. A QUIC client whose address moves
// (NAT rebind, DisablePathManager) is also still the same one entry.
func TestSeenSetIsKeyedByUser(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 1000; i++ {
		r.Bind("roamer", "1.2.3.4:"+strconv.Itoa(1000+i))
	}

	r.access.Lock()
	size := len(r.seen)
	sources := len(r.sources)
	r.access.Unlock()
	if size != 1 {
		t.Errorf("len(seen) = %d after 1000 addresses, want 1", size)
	}
	if sources != 0 {
		t.Errorf("len(sources) = %d, want 0 -- an untracked user must not allocate per address", sources)
	}

	got := r.CloseUsers(keep())
	if got != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v, want {Unclosable:1} -- one user is one report", got)
	}
}

// Re-enabling a user has to work: their next connection puts them back.
func TestRemovedUserIsRecordedAgainOnReconnect(t *testing.T) {
	r := NewRegistry()
	r.Bind("flip", "1.2.3.4:1000")
	if got := r.CloseUsers(keep()); got != (Result{Unclosable: 1}) {
		t.Fatalf("precondition: CloseUsers = %+v, want {Unclosable:1}", got)
	}
	// Reported once and dropped, so an unrelated later save says nothing.
	if got := r.CloseUsers(keep()); got != (Result{}) {
		t.Fatalf("CloseUsers = %+v on the second pass, want an empty result", got)
	}

	r.Bind("flip", "1.2.3.4:2000")
	if got := r.CloseUsers(keep()); got != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v after reconnect, want {Unclosable:1}", got)
	}
}

func TestCloseUsersLeavesUnauthenticatedSourceAlone(t *testing.T) {
	r := NewRegistry()
	// Bound with no user name: the connection reached the handler before
	// authentication produced one. It belongs to nobody, so a removal has
	// nothing to say about it.
	r.Bind("", "1.2.3.4:1000")

	got := r.CloseUsers(keep("stays"))
	if got != (Result{}) {
		t.Fatalf("CloseUsers = %+v, want an empty result", got)
	}
}

// A user still on the inbound is not touched, however many sessions they have.
func TestCloseUsersKeepsEnabledUsers(t *testing.T) {
	r := NewRegistry()
	first := &fakeCloser{}
	second := &fakeCloser{}
	r.BindAndTrack("stays", "1.2.3.4:1000", first)
	r.BindAndTrack("stays", "1.2.3.4:1001", second)
	r.Bind("stays", "5.6.7.8:2000")

	got := r.CloseUsers(keep("stays"))
	if got != (Result{}) {
		t.Fatalf("CloseUsers = %+v, want an empty result", got)
	}
	if first.closed || second.closed {
		t.Error("a user who is still enabled must keep their sessions")
	}
}

func TestKickClosesTrackedSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("noisy", "1.2.3.4:1000")

	got := r.KickUserSessions("noisy")
	if got != (Result{Cut: 1}) {
		t.Fatalf("KickUserSessions = %+v, want {Cut:1}", got)
	}
	if !conn.closed {
		t.Error("a tracked session must be closed on kick")
	}
}

// A kick cannot reach a QUIC session either. It says so rather than pretending,
// and leaves what to do about it to the caller.
func TestKickReportsUntrackedUser(t *testing.T) {
	r := NewRegistry()
	r.Bind("noisy", "1.2.3.4:1000")

	got := r.KickUserSessions("noisy")
	if got != (Result{Unclosable: 1}) {
		t.Fatalf("KickUserSessions = %+v, want {Unclosable:1}", got)
	}
	// Unlike a removal, a kick leaves the user in the set: they are still on
	// the inbound, so a removal later still has to report them.
	if got := r.CloseUsers(keep()); got != (Result{Unclosable: 1}) {
		t.Errorf("CloseUsers = %+v after a kick, want {Unclosable:1} -- the kick dropped a user who is still enabled", got)
	}
}

// The other shape a kick can fail to reach: an entry that was tracked but
// carries no closer (a packet conn addressed to the mux destination). It has to
// be reported for the same reason the seen set is.
func TestKickReportsATrackedEntryWithNoCloser(t *testing.T) {
	r := NewRegistry()
	r.BindAndTrack("live", "1.2.3.4:1000", nil)

	got := r.KickUserSessions("live")
	if got != (Result{Unclosable: 1}) {
		t.Fatalf("KickUserSessions = %+v, want {Unclosable:1}", got)
	}
}

func TestKickIgnoresOtherUsers(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("bystander", "1.2.3.4:1000", conn)
	r.Bind("other", "5.6.7.8:2000")

	got := r.KickUserSessions("noisy")
	if got != (Result{}) {
		t.Fatalf("KickUserSessions = %+v, want an empty result", got)
	}
	if conn.closed {
		t.Error("a kick must not touch another user's session")
	}
}

func TestUntrackForgetsSession(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.Track("1.2.3.4:1000", conn)
	r.Bind("gone", "1.2.3.4:1000")
	r.Untrack("1.2.3.4:1000")

	got := r.CloseUsers(keep())
	if got != (Result{}) {
		t.Fatalf("CloseUsers = %+v, want an empty result -- the session was already gone", got)
	}
	if conn.closed {
		t.Error("an untracked session must not be closed again")
	}
}

// A tracked session is alive however quiet it has been -- it carries a single
// long-lived stream, an ssh session or a download, and opens nothing new. It
// must stay closable, which is why nothing ages sources out: Untrack is the
// only thing that removes an entry, and it runs when the session really ends.
func TestQuietTrackedSessionStaysClosable(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("quiet", "1.2.3.4:1000", conn)

	// Plenty of unrelated activity while "quiet" routes nothing.
	for i := 0; i < 500; i++ {
		r.Bind("busy", "5.6.7.8:"+strconv.Itoa(2000+i))
		r.CloseUsers(keep("quiet", "busy"))
	}

	got := r.CloseUsers(keep("busy"))
	if got != (Result{Cut: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Cut:1} -- the quiet session was lost", got)
	}
	if !conn.closed {
		t.Error("a quiet tracked session must still be closable")
	}
}

// This pins the outcome -- a carrier registered this way is cut, not reported.
// The atomicity it exists for cannot be asserted here: it comes from doing both
// writes under one lock, and a sequential Bind-then-Track would pass this test
// just the same. What that split would lose is the interleaving where a
// CloseUsers lands between the two, sees a user with no session yet, and puts
// them in the seen set -- costing a mux inbound a restart it never needed.
func TestBindAndTrackCutsRatherThanReports(t *testing.T) {
	r := NewRegistry()
	conn := &fakeCloser{}
	r.BindAndTrack("live", "1.2.3.4:1000", conn)

	got := r.CloseUsers(keep())
	if got != (Result{Cut: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Cut:1}", got)
	}
	if !conn.closed {
		t.Error("the carrier must be closed")
	}
}

// A packet conn cannot be a mux carrier, so the router passes a nil closer. It
// must stay nil: a nil net.Conn placed in an io.Closer is not a nil interface,
// and would register as a closer that panics when the session is cut.
func TestBindAndTrackWithNilCloserReportsUnclosable(t *testing.T) {
	r := NewRegistry()
	r.BindAndTrack("live", "1.2.3.4:1000", nil)

	got := r.CloseUsers(keep())
	if got != (Result{Unclosable: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Unclosable:1}", got)
	}
}

// Sessions are counted one by one because each is a separate transport to
// close; users without one are counted once, because one restart ends all of
// theirs at the same time.
func TestCloseUsersCountsSessionsAndUsers(t *testing.T) {
	r := NewRegistry()
	first := &fakeCloser{}
	second := &fakeCloser{}
	r.BindAndTrack("gone", "1.2.3.4:1000", first)
	r.BindAndTrack("gone", "1.2.3.4:1001", second)
	r.Bind("gone", "1.2.3.4:1002")
	r.Bind("gone", "1.2.3.4:1003")

	got := r.CloseUsers(keep())
	if got != (Result{Cut: 2, Unclosable: 1}) {
		t.Fatalf("CloseUsers = %+v, want {Cut:2 Unclosable:1}", got)
	}
	if !first.closed || !second.closed {
		t.Error("both tracked sessions must be cut")
	}
}
