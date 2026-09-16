// Package usersession tracks the client sessions of an inbound, so that the
// sessions of a user who was just removed can actually be cut.
//
// Protocols that authenticate once per session (the QUIC ones, anytls, and
// anything carried over sing-mux) keep serving an already authenticated client
// after its user is gone from the inbound: swapping the auth map only affects
// new sessions, and closing the routed connections is not enough because the
// client simply opens another stream on the session it already has. Reaching
// the session itself is what this registry is for.
//
// This lives at the inbound layer rather than in ConnTracker on purpose. A
// tracker-level gate sees a connection only after routing, so it misses the
// ones the router answers itself (hijack-dns), and refusing there still costs
// one real dial to the destination before the copy fails. Refusing here happens
// before any of that. The two layers stay separate: IP limits keep their gate
// in ConnTracker, because that policy has to outlive a core restart, while
// everything here is per-inbound and goes away with the Box.
package usersession

import (
	"io"
	"sync"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

const (
	// A source that has not opened a connection for this long is forgotten;
	// its session is either gone or idle enough to be re-learned on use.
	idleTimeout = 10 * time.Minute
	// Backstop for blocks: a client whose session is really dead stops being
	// blocked, so the address is reusable even if the user is never re-added.
	blockTimeout = 10 * time.Minute
	// A kicked session is muted only while it keeps trying. Once it has been
	// quiet this long, the next attempt from the address is a new session and
	// is let through, so a disconnect does not turn into a lockout.
	kickQuietWindow = 30 * time.Second
)

type entry struct {
	user     string
	lastSeen time.Time
	closer   io.Closer
}

// block mutes one client address. A removal keeps it muted until the backstop;
// a kick, which must not lock a still-enabled user out, lifts as soon as the
// muted session stops trying.
type block struct {
	at          time.Time
	lastAttempt time.Time
	kick        bool
}

// Registry maps a client address to the user it authenticated as. One instance
// per inbound.
//
// The key is the full source address including the port, never a normalized
// one: it identifies a single session, and two sessions from one subscriber
// must not share an entry. (ConnTracker deliberately does the opposite -- it
// masks IPv6 to a prefix -- because an IP limit counts subscribers, not
// sessions. Keying this map that way would mute an entire /64 when one client
// in it is removed.)
type Registry struct {
	access    sync.Mutex
	sources   map[string]*entry
	blocked   map[string]*block
	lastSweep time.Time
}

func NewRegistry() *Registry {
	return &Registry{
		sources:   make(map[string]*entry),
		blocked:   make(map[string]*block),
		lastSweep: time.Now(),
	}
}

func (r *Registry) load(source string) *entry {
	e, loaded := r.sources[source]
	if !loaded {
		e = &entry{}
		r.sources[source] = e
	}
	e.lastSeen = time.Now()
	return e
}

// Bind records which user the session at source authenticated as. Called for
// every connection the session opens, which doubles as a liveness ping.
func (r *Registry) Bind(user string, source string) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	e := r.load(source)
	if user != "" {
		e.user = user
	}
	r.sweepLocked(e.lastSeen)
}

// sweepLocked drops entries nothing will come back for. The QUIC inbounds only
// ever Bind -- a QUIC session ends without a callback this package could hang
// Untrack on -- so without this, sources would grow with every session the
// listener has ever seen. Riding on Bind keeps it to one walk per idleTimeout
// and needs no cron job of its own.
func (r *Registry) sweepLocked(now time.Time) {
	if now.Sub(r.lastSweep) < idleTimeout {
		return
	}
	r.lastSweep = now
	for source, e := range r.sources {
		if idleLost(e, now) {
			delete(r.sources, source)
			delete(r.blocked, source)
		}
	}
	for source, b := range r.blocked {
		if now.Sub(b.at) > blockTimeout {
			delete(r.blocked, source)
		}
	}
}

// idleLost reports whether an entry is one nothing will come back for.
//
// A tracked session is never that, however long it has been quiet: lastSeen
// only moves when the session opens another connection, so one carrying a
// single long-lived stream -- an ssh session, a download, a long poll -- looks
// idle here while it is perfectly alive. Dropping it would discard the closer,
// which is the only handle on it there is, and the next removal would then find
// nothing to cut and let the session run on. Tracked entries are cleaned up by
// their own Untrack instead, which the inbound defers for exactly that.
func idleLost(e *entry, now time.Time) bool {
	return e.closer == nil && now.Sub(e.lastSeen) > idleTimeout
}

// BindAndTrack records the user and the session transport in one go, for a
// carrier that arrives with both already known.
//
// The two must land under a single lock. A CloseUsers that ran in between --
// seeing the user but not yet the closer -- would file the session as one it
// cannot close and mute it instead, and a mux carrier is never gated, so the
// mute would do nothing at all while the session kept being served.
func (r *Registry) BindAndTrack(user string, source string, closer io.Closer) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	e := r.load(source)
	if user != "" {
		e.user = user
	}
	if closer != nil {
		e.closer = closer
	}
	r.sweepLocked(e.lastSeen)
}

// Track stores the session transport, for protocols whose session has a closer
// of its own. Without one the session can only be muted, not closed.
func (r *Registry) Track(source string, closer io.Closer) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	r.load(source).closer = closer
}

func (r *Registry) Untrack(source string) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	delete(r.sources, source)
	delete(r.blocked, source)
}

// Allowed reports whether connections from source may still be routed. A
// session that cannot be closed is muted here instead: nothing it opens is
// routed any more, so the removed user's traffic stops.
func (r *Registry) Allowed(source string) bool {
	if source == "" {
		return true
	}
	r.access.Lock()
	defer r.access.Unlock()
	b, blocked := r.blocked[source]
	if !blocked {
		return true
	}
	now := time.Now()
	if now.Sub(b.at) > blockTimeout {
		delete(r.blocked, source)
		return true
	}
	// A gap this long means the muted session gave up; whatever is connecting
	// now is a new one. Only for a kick: a removed user stays muted until the
	// backstop, because there is nothing to let back in.
	if b.kick && now.Sub(b.lastAttempt) > kickQuietWindow {
		delete(r.blocked, source)
		return true
	}
	b.lastAttempt = now
	return false
}

// CloseUsers cuts the sessions of every user not in keep and lifts the block on
// the sessions of users that are in keep, so re-enabling a user takes effect
// without waiting for their session to die. Returns the number of sessions cut.
func (r *Registry) CloseUsers(keep map[string]struct{}) int {
	now := time.Now()

	r.access.Lock()
	var closers []io.Closer
	cut := 0
	for source, e := range r.sources {
		if idleLost(e, now) {
			delete(r.sources, source)
			delete(r.blocked, source)
			continue
		}
		if e.user == "" {
			continue
		}
		if _, ok := keep[e.user]; ok {
			delete(r.blocked, source)
			continue
		}
		cut++
		if e.closer != nil {
			closers = append(closers, e.closer)
			delete(r.sources, source)
			delete(r.blocked, source)
			continue
		}
		r.blocked[source] = &block{at: now, lastAttempt: now}
	}
	for source, b := range r.blocked {
		if now.Sub(b.at) > blockTimeout {
			delete(r.blocked, source)
		}
	}
	r.lastSweep = now
	r.access.Unlock()

	// Outside the lock: closing a tracked session runs the inbound's own close
	// handler, which comes back through Untrack.
	for _, closer := range closers {
		_ = closer.Close()
	}
	return cut
}

// KickUserSessions disconnects a user who is still enabled. A session with a
// closer is cut outright; one without is muted, which is the only way to stop a
// QUIC session that the protocol gives us no handle on. The mute lifts as soon
// as that session stops trying, so the client reconnects on its own.
func (r *Registry) KickUserSessions(user string) int {
	if user == "" {
		return 0
	}
	now := time.Now()

	r.access.Lock()
	var closers []io.Closer
	kicked := 0
	for source, e := range r.sources {
		if e.user != user {
			continue
		}
		kicked++
		if e.closer != nil {
			delete(r.sources, source)
			delete(r.blocked, source)
			closers = append(closers, e.closer)
			continue
		}
		r.blocked[source] = &block{at: now, lastAttempt: now, kick: true}
	}
	r.access.Unlock()

	for _, closer := range closers {
		_ = closer.Close()
	}
	return kicked
}

// KeepSet turns the user-name list an inbound just installed into the set
// CloseUsers takes. The names an in-place update is given are exactly the
// clients still enabled on that inbound, so nothing else has to be looked up.
func KeepSet(names []string) map[string]struct{} {
	keep := make(map[string]struct{}, len(names))
	for _, name := range names {
		keep[name] = struct{}{}
	}
	return keep
}

// ErrRemoved is reported to the close handler of a connection that a muted
// session tried to open.
var ErrRemoved = E.New("user removed from inbound")

// Reject drops a connection opened by a session whose user is gone.
func Reject(conn io.Closer, onClose N.CloseHandlerFunc) {
	common.Close(conn)
	if onClose != nil {
		onClose(ErrRemoved)
	}
}
