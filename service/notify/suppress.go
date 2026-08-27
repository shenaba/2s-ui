package notify

import (
	"strings"
	"sync"
	"time"
)

// Cooldowns for the one-shot kinds. State and login kinds are not listed --
// they are decided by their own rules below.
const (
	expiringCooldown = 24 * time.Hour
	metricCooldown   = 15 * time.Minute
	loginCooldown    = time.Minute
)

// Decision is what the suppressor concluded about one event.
type Decision struct {
	Send bool
	// Failures is how many attempts this single notification stands for, and
	// is only set for LoginFailed. It is 1 for the first alert in a window and
	// higher when attempts were folded into it.
	Failures int
}

// Suppressor decides which events actually reach the channels.
//
// It sits in front of the bus rather than inside each subscriber, so every
// channel is told about the same events -- an operator comparing their Telegram
// history against a webhook receiver should not find them disagreeing.
//
// All of its state is in memory. A restart therefore re-arms everything: a
// still-relevant expiry warning goes out a second time, and a node that was
// already known down announces itself again. Persisting it would mean a SQLite
// write per event to save at most one duplicate message per restart, which is
// the wrong trade -- but it does mean event sources whose underlying condition
// repeats every few seconds (the core restart loop, node probes) must debounce
// on their own rather than relying on this to be the only guard.
type Suppressor struct {
	mu sync.Mutex
	// health per "family:subject" -- see stateChangedLocked. Absent means
	// healthy, so a panel that starts up with everything already fine stays
	// quiet, while one that starts up with a node down reports it.
	health map[string]bool
	// until["kind:subject"] is when that pair may next be notified about.
	until map[string]time.Time
	// failures counts LoginFailed attempts swallowed since the last alert.
	failures  map[string]int
	lastPrune time.Time
}

func NewSuppressor() *Suppressor {
	return &Suppressor{
		health:   make(map[string]bool),
		until:    make(map[string]time.Time),
		failures: make(map[string]int),
	}
}

// Decide reports whether e should be delivered. now is a parameter so the
// decision is testable without sleeping.
func (s *Suppressor) Decide(e Event, now time.Time) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	switch e.Kind {
	// Transitions only. Publishing NodeDown every 5s for as long as a node is
	// unreachable is the single fastest way to get the whole feature muted.
	case NodeUp:
		return Decision{Send: s.stateChangedLocked("node:"+e.Subject, true)}
	case NodeDown:
		return Decision{Send: s.stateChangedLocked("node:"+e.Subject, false)}
	case CoreUp:
		return Decision{Send: s.stateChangedLocked("core:"+e.Subject, true)}
	case CoreCrash:
		return Decision{Send: s.stateChangedLocked("core:"+e.Subject, false)}

	// A ban is the one alert that must never be withheld: it means someone is
	// actively working on the password, which is precisely when a rate limit on
	// the alerting would hide what the operator needs to see.
	case LoginBanned:
		return Decision{Send: true}

	// Successes are infrequent by nature (one per admin session) and their
	// value is in being told about every one -- a login the operator did not
	// perform is the whole point.
	case LoginSuccess:
		return Decision{Send: true}

	// Failures are the noisy one, and 3x-ui exempts them from rate limiting
	// entirely, which turns a password-guessing run into a message flood. Alert
	// on the first failure in a window so the operator hears about it right
	// away, swallow the rest, and let the next alert carry the count of what
	// was swallowed. No timer to manage, and nothing is lost but the timing of
	// the individual attempts.
	case LoginFailed:
		key := string(e.Kind) + ":" + e.Subject
		n := s.failures[key] + 1
		if s.allowLocked(key, loginCooldown, now) {
			delete(s.failures, key)
			return Decision{Send: true, Failures: n}
		}
		s.failures[key] = n
		return Decision{Send: false}

	case ClientExpiring:
		return Decision{Send: s.allowLocked(string(e.Kind)+":"+e.Subject, expiringCooldown, now)}

	case CPUHigh, MemoryHigh:
		return Decision{Send: s.allowLocked(string(e.Kind)+":"+e.Subject, metricCooldown, now)}

	// ClientDepleted and anything added later pass through. Depletion is
	// already batched by its job into one event per pass, and a client that was
	// disabled in that pass is not selected by the next one, so there is
	// nothing here to suppress.
	default:
		return Decision{Send: true}
	}
}

// stateChangedLocked reports whether family just flipped, recording the new
// state. An unseen family is treated as healthy: the alternative would have
// every panel announce each of its healthy nodes once at startup.
func (s *Suppressor) stateChangedLocked(family string, healthy bool) bool {
	prev, seen := s.health[family]
	if !seen {
		prev = true
	}
	if prev == healthy {
		return false
	}
	s.health[family] = healthy
	return true
}

// allowLocked is the cooldown gate. It stores when a key may next fire rather
// than when it last did, so pruning does not have to know which cooldown any
// given entry was admitted under.
func (s *Suppressor) allowLocked(key string, cooldown time.Duration, now time.Time) bool {
	if until, ok := s.until[key]; ok && now.Before(until) {
		return false
	}
	s.until[key] = now.Add(cooldown)
	return true
}

// pruneLocked drops expired cooldowns.
//
// Without it the map keeps one entry per (kind, subject) ever seen, and the
// subject is a client name or a source IP -- on a busy panel that is unbounded
// growth in a process that is expected to run for months. An expired entry
// cannot change what allowLocked returns, so dropping it is free.
//
// Throttled so a burst of events does not walk the whole map each time.
func (s *Suppressor) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < metricCooldown {
		return
	}
	s.lastPrune = now
	for k, until := range s.until {
		if now.Before(until) {
			continue
		}
		delete(s.until, k)
		// The swallowed-failure tally is only meaningful while its cooldown is
		// live; keeping it would attribute an old run's attempts to a new one.
		if strings.HasPrefix(k, string(LoginFailed)+":") {
			delete(s.failures, k)
		}
	}
}
