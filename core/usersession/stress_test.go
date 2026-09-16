package usersession

import (
	"strconv"
	"sync"
	"testing"
)

// Every exported method driven at once from many goroutines. The point is the
// race detector, not the assertions: CloseUsers closes outside the lock and
// Untrack comes back in on the closer's own goroutine, so the two have to be
// safe against each other. Run with -race to mean anything.
func TestRegistryUnderConcurrency(t *testing.T) {
	r := NewRegistry()
	const workers = 8
	const rounds = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			user := "user" + strconv.Itoa(w%3)
			for i := 0; i < rounds; i++ {
				source := "10.0.0." + strconv.Itoa(w) + ":" + strconv.Itoa(1000+i%7)
				switch i % 5 {
				case 0:
					r.Bind(user, source)
				case 1:
					r.BindAndTrack(user, source, &fakeCloser{})
				case 2:
					r.Track(source, &fakeCloser{})
					r.Bind(user, source)
				case 3:
					r.CloseUsers(keep("user0"))
				case 4:
					r.KickUserSessions(user)
				}
				r.Untrack(source)
			}
		}(w)
	}
	wg.Wait()

	// Every source was untracked by its own goroutine, so nothing may be left.
	r.access.Lock()
	remaining := len(r.sources)
	seen := len(r.seen)
	r.access.Unlock()
	if remaining != 0 {
		t.Errorf("len(sources) = %d after every source was untracked, want 0", remaining)
	}
	// seen has no expiry, so what bounds it has to be the user count: three
	// users across 8 goroutines and 1600 addresses.
	if seen > 3 {
		t.Errorf("len(seen) = %d, want at most 3 -- it is growing per address, not per user", seen)
	}
}
