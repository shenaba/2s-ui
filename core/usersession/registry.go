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
// # Two kinds of session
//
// anytls and the sing-mux carrier arrive as a net.Conn that lives exactly as
// long as the session does. Those are tracked by source address and closed
// outright, and that is the whole story for anytls, vless, vmess and trojan:
// the key is a single TCP connection's address, it cannot move, and Untrack
// runs when the session ends.
//
// The QUIC protocols give this layer no handle on the session -- and, the part
// that decides the design, no usable name for one either:
//
//   - sing-quic keeps its session list unexported, and the ctx it hands the
//     handler per stream is the Service's own, shared by every session;
//   - all three services set quic-go's DisablePathManager, which rewrites a
//     connection's remote address the moment a decryptable packet arrives from
//     a new one, with no path validation -- so the source address moves under
//     one NAT rebind;
//   - that address is an ephemeral UDP port, recycled to somebody else once the
//     session ends, and nothing tells this layer that it has been;
//   - and there is no moment at which a QUIC session can be declared gone.
//     quic-go keeps one alive with a 10s PING whether or not a single byte is
//     routed, so an idle session and a dead one look identical from here.
//
// So for QUIC this package does not track sessions at all. It records
// something weaker and completely reliable: **which users have been seen on
// this inbound**. That set is bounded by the client count rather than the
// session count, which is what lets it carry no timeout -- and a set with no
// timeout is the only kind that cannot be wrong about a session it cannot see.
//
// Earlier revisions tried to do better and could not. Muting the source address
// is defeated by the second and third points; ageing a per-session entry out
// after ten idle minutes is defeated by the fourth, and silently let a removed
// user keep an idle session. Both looked like they worked because a test client
// that reconnects every second never exercises either.
//
// # What a removal does
//
// CloseUsers cuts every session it holds a transport for, and reports the rest
// as Unclosable. The QUIC inbounds turn that into ErrRestartRequired and the
// caller rebuilds the inbound, which destroys every QUIC session on it, the
// removed user's included. Everyone on that inbound reconnects once.
//
// Being wrong here is one-sided on purpose: a user who connected and then left
// for good is still in the seen set, so removing them costs one restart that
// bought nothing. The other direction -- deciding a session is gone when it is
// not -- is what issue #175 is, so the cost is paid on that side.
//
// This lives at the inbound layer rather than in ConnTracker on purpose. The
// two stay separate: IP limits keep their gate in ConnTracker, because that
// policy has to outlive a core restart, while everything here is per-inbound
// and goes away with the Box.
package usersession

import (
	"io"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
)

// entry is one session this layer can actually reach: a transport it can close,
// and the user that authenticated it.
type entry struct {
	user   string
	closer io.Closer
}

// Registry is what an inbound knows about who is connected to it. One instance
// per inbound; it goes away with the inbound.
//
// Neither map is consulted to decide whether a connection may pass -- nothing
// in this package refuses anything. They decide whose transport to close, and
// whether a removed user was connected at all.
type Registry struct {
	access sync.RWMutex
	// sources holds the sessions with a closer, keyed by the source address of
	// the one connection that carries them. Bounded by live sessions: every
	// entry is created by Track or BindAndTrack and removed by the Untrack its
	// inbound defers.
	sources map[string]*entry
	// seen holds the users that turned up without a closable session -- the
	// QUIC ones. Keyed by user rather than by address because no address here
	// is stable, and bounded by the client count rather than by the session
	// count, which is what lets it need no expiry. See the package comment.
	seen map[string]struct{}
}

func NewRegistry() *Registry {
	return &Registry{
		sources: make(map[string]*entry),
		seen:    make(map[string]struct{}),
	}
}

// Result is what a removal or a kick managed to do.
type Result struct {
	// Cut counts the sessions closed outright.
	Cut int
	// Unclosable counts the users left connected because this layer has no
	// handle on their session. For a QUIC inbound that is the signal to rebuild
	// the inbound instead; see ErrRestartRequired.
	Unclosable int
}

// RestartRequired is what an inbound whose sessions it cannot close returns
// from UpdateUsers: nil when the removal was carried out in full, and
// ErrRestartRequired when a removed user is still connected by a session that
// only rebuilding the inbound will end.
//
// Only the QUIC inbounds call this. anytls and the mux protocols hold the
// transport of every session they register -- see the notes on their own
// CloseUsers calls.
func (r Result) RestartRequired() error {
	if r.Unclosable > 0 {
		return ErrRestartRequired
	}
	return nil
}

// Bind records that user is connected. If the connection belongs to a session
// something already tracked (anytls registers its transport in NewConnection
// before any stream is routed), the user is attached to that session so it can
// be closed by name. Otherwise there is no session to attach to and the user
// goes into the seen set -- which is the QUIC path.
func (r *Registry) Bind(user string, source string) {
	if user == "" {
		return
	}
	r.access.RLock()
	if e, tracked := r.sources[source]; tracked {
		if e.user == user {
			r.access.RUnlock()
			return
		}
	} else if _, seen := r.seen[user]; seen {
		r.access.RUnlock()
		return
	}
	r.access.RUnlock()

	r.access.Lock()
	defer r.access.Unlock()
	if e, tracked := r.sources[source]; tracked {
		e.user = user
		return
	}
	r.seen[user] = struct{}{}
}

// BindAndTrack records the user and the session transport in one go, for a
// carrier that arrives with both already known.
//
// The two must land under a single lock. A CloseUsers that ran in between --
// seeing the user but not yet the closer -- would put it in the seen set and
// report it Unclosable, costing a restart that the closer it was about to be
// given would have made unnecessary.
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
}

// Track stores the session transport, for a protocol whose session has a closer
// of its own but does not know the user yet. The Bind that follows attaches it.
func (r *Registry) Track(source string, closer io.Closer) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	r.load(source).closer = closer
}

// Untrack forgets a session that has ended. This is what keeps sources bounded,
// and it is why a tracked session never needs to be aged out: its inbound
// defers this call for exactly as long as the session lives.
func (r *Registry) Untrack(source string) {
	if source == "" {
		return
	}
	r.access.Lock()
	defer r.access.Unlock()
	delete(r.sources, source)
}

func (r *Registry) load(source string) *entry {
	e, loaded := r.sources[source]
	if !loaded {
		e = &entry{}
		r.sources[source] = e
	}
	return e
}

// CloseUsers cuts the sessions of every user not in keep, and reports the users
// it could not reach. keep is the user table the inbound has just installed, so
// anything bound to a name outside it belongs to a user who is gone.
func (r *Registry) CloseUsers(keep map[string]struct{}) Result {
	r.access.Lock()
	var closers []io.Closer
	var result Result
	for source, e := range r.sources {
		if e.user == "" {
			continue
		}
		if _, ok := keep[e.user]; ok {
			continue
		}
		if e.closer == nil {
			result.Unclosable++
			continue
		}
		result.Cut++
		closers = append(closers, e.closer)
		delete(r.sources, source)
	}
	for user := range r.seen {
		if _, ok := keep[user]; ok {
			continue
		}
		result.Unclosable++
		// Dropped because the user is off the inbound: either the caller is
		// about to rebuild it, which discards this registry anyway, or it is a
		// protocol that does not rebuild, where reporting the same departed
		// user on every later save would be noise. A user who is re-enabled and
		// connects again is recorded again by Bind.
		delete(r.seen, user)
	}
	r.access.Unlock()

	// Outside the lock: closing a tracked session runs the inbound's own close
	// handler, which comes back through Untrack.
	for _, closer := range closers {
		_ = closer.Close()
	}
	return result
}

// KickUserSessions disconnects a user who is still enabled -- an operator
// action, not a revocation. Only a session with a closer can be cut; a user who
// is merely known to be connected is reported instead, and it is the caller's
// business whether disconnecting one user is worth restarting the inbound
// everyone else is on.
func (r *Registry) KickUserSessions(user string) Result {
	if user == "" {
		return Result{}
	}

	r.access.Lock()
	var closers []io.Closer
	var result Result
	for source, e := range r.sources {
		if e.user != user {
			continue
		}
		if e.closer == nil {
			result.Unclosable++
			continue
		}
		result.Cut++
		closers = append(closers, e.closer)
		delete(r.sources, source)
	}
	// Left in the set: this user is still on the inbound and may still be
	// connected, so a later removal has to report them again.
	if _, ok := r.seen[user]; ok {
		result.Unclosable++
	}
	r.access.Unlock()

	for _, closer := range closers {
		_ = closer.Close()
	}
	return result
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

// ErrRestartRequired reports that a user who was just removed is still
// connected by a session this layer cannot close, so the only way to disconnect
// them is to tear the inbound down and build it again.
//
// The caller already has that path -- it is the same fallback taken by
// protocols with no in-place user update at all -- so returning this error is
// all an inbound has to do. It is not a failure: the user table was swapped
// successfully, and the caller tells the two apart so that this one is not
// logged as one.
var ErrRestartRequired = E.New("removed user is still connected by a session that cannot be cut")
