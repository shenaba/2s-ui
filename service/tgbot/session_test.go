package tgbot

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// Telegram caps callback_data at 64 bytes, so every button that carries an
// argument sends a key into this store instead. If a key does not survive a
// round trip the button does nothing when pressed.
func TestPayloadStoreRoundTrip(t *testing.T) {
	s := &payloadStore{items: map[string]payloadEntry{}}

	key := s.put("toggle|some-client")
	if len(staticPrefix+key) > 64 || len(payloadPrefix+key) > 64 {
		t.Fatalf("key %q does not fit in callback_data once prefixed", key)
	}
	got, ok := s.get(key)
	if !ok || got != "toggle|some-client" {
		t.Fatalf("round trip returned (%q, %v)", got, ok)
	}

	// Two puts must not collide, or one button would fire another's action.
	if s.put("a") == s.put("b") {
		t.Fatal("two payloads were given the same key")
	}
	if _, ok := s.get("never-issued"); ok {
		t.Fatal("an unissued key resolved")
	}
}

// The store is in memory, so buttons outlive their payloads across a restart
// and eventually by age. A miss has to be distinguishable from a hit, because
// the caller turns it into "that button has expired" rather than silence.
func TestPayloadStoreExpiry(t *testing.T) {
	s := &payloadStore{items: map[string]payloadEntry{}}
	key := s.put("client|alice")

	// Age the entry rather than sleeping past the TTL.
	s.mu.Lock()
	e := s.items[key]
	e.at = time.Now().Add(-payloadTTL - time.Minute)
	s.items[key] = e
	s.mu.Unlock()

	if _, ok := s.get(key); ok {
		t.Fatal("an expired payload still resolved")
	}
}

// Keys accumulate one per button ever rendered -- a client listing alone emits
// twenty. Without pruning that is unbounded growth in a process expected to run
// for months.
func TestPayloadStorePrune(t *testing.T) {
	s := &payloadStore{items: map[string]payloadEntry{}}

	stale := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		stale = append(stale, s.put("client|"+strconv.Itoa(i)))
	}
	s.mu.Lock()
	for _, k := range stale {
		e := s.items[k]
		e.at = time.Now().Add(-payloadTTL - time.Minute)
		s.items[k] = e
	}
	// Pruning is throttled; move the clock back so the next put runs one.
	s.lastPrune = time.Now().Add(-pruneEvery - time.Minute)
	s.mu.Unlock()

	fresh := s.put("client|current")
	s.mu.Lock()
	size := len(s.items)
	s.mu.Unlock()
	if size != 1 {
		t.Fatalf("after pruning, %d entries remain, want 1", size)
	}
	if _, ok := s.get(fresh); !ok {
		t.Fatal("pruning dropped the entry that was just added")
	}
}

func TestFormStoreLifecycle(t *testing.T) {
	s := &formStore{items: map[int64]*formState{}}

	s.set(1, stepClientName, clientDraft{})
	s.set(1, stepClientVolume, clientDraft{Name: "alice"})
	f, ok := s.get(1)
	if !ok || f.Step != stepClientVolume || f.Draft.Name != "alice" {
		t.Fatalf("form did not advance: %+v (ok=%v)", f, ok)
	}

	// Chats are independent, or two admins filling forms would overwrite each
	// other's answers.
	if _, ok := s.get(2); ok {
		t.Fatal("a form leaked into another chat")
	}

	s.clear(1)
	if _, ok := s.get(1); ok {
		t.Fatal("clear did not remove the form")
	}
}

// A half-finished form has to expire: someone who wandered off mid-form should
// come back to a clean slate rather than to question three.
func TestFormStoreExpiry(t *testing.T) {
	s := &formStore{items: map[int64]*formState{}}
	s.set(7, stepClientVolume, clientDraft{Name: "bob"})

	s.mu.Lock()
	s.items[7].at = time.Now().Add(-formTTL - time.Minute)
	s.lastPrune = time.Now().Add(-pruneEvery - time.Minute)
	s.mu.Unlock()

	if _, ok := s.get(7); ok {
		t.Fatal("an expired form was still live")
	}
	// And it is actually dropped, not merely hidden.
	s.set(8, stepClientName, clientDraft{})
	s.mu.Lock()
	_, stillThere := s.items[7]
	s.mu.Unlock()
	if stillThere {
		t.Fatal("the expired form was not pruned")
	}
}

// The SDK dispatches updates on worker goroutines, so two messages from one
// chat can be in flight at once. 3x-ui shipped its equivalent unlocked and had
// to fix the race afterwards; this is the test that would have caught it.
func TestFormStoreConcurrent(t *testing.T) {
	s := &formStore{items: map[int64]*formState{}}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := int64(i % 4)
			s.set(id, stepClientName, clientDraft{Name: strconv.Itoa(i)})
			s.get(id)
			if i%8 == 0 {
				s.clear(id)
			}
		}(i)
	}
	wg.Wait()
}

func TestPayloadStoreConcurrent(t *testing.T) {
	s := &payloadStore{items: map[string]payloadEntry{}}

	var wg sync.WaitGroup
	keys := make(chan string, 100)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys <- s.put("client|" + strconv.Itoa(i))
		}(i)
	}
	wg.Wait()
	close(keys)

	for k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			s.get(k)
		}(k)
	}
	wg.Wait()
}
