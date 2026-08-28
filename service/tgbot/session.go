package tgbot

import (
	"sync"
	"time"

	"github.com/shenaba/2s-ui/util/common"
)

const (
	// payloadTTL bounds how long a button stays live. Long enough to read a
	// list and decide, short enough that the map cannot accumulate.
	payloadTTL = 30 * time.Minute
	// formTTL is how long a half-finished form is kept. Someone who wandered
	// off mid-form should come back to a clean slate, not to question four.
	formTTL    = 10 * time.Minute
	pruneEvery = 5 * time.Minute
)

// payloadStore maps short keys to real callback payloads.
//
// Telegram caps callback_data at 64 bytes, which is not enough for anything
// carrying a client name and an action. Buttons therefore carry an opaque key
// and the payload lives here.
//
// Being in memory means a panel restart invalidates every button already on
// someone's screen. That is handled rather than ignored: a miss answers "this
// button has expired, open the menu again" instead of doing nothing, which is
// indistinguishable from the bot being broken.
type payloadStore struct {
	mu        sync.Mutex
	items     map[string]payloadEntry
	lastPrune time.Time
}

type payloadEntry struct {
	data string
	at   time.Time
}

var payloads = &payloadStore{items: map[string]payloadEntry{}}

func (s *payloadStore) put(data string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	// 12 chars out of the alphabet common.Random uses, well inside the 64-byte
	// cap even once a prefix is added.
	key := common.Random(12)
	s.items[key] = payloadEntry{data: data, at: time.Now()}
	return key
}

func (s *payloadStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[key]
	if !ok || time.Since(e.at) > payloadTTL {
		return "", false
	}
	return e.data, true
}

func (s *payloadStore) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	s.lastPrune = now
	for k, e := range s.items {
		if now.Sub(e.at) > payloadTTL {
			delete(s.items, k)
		}
	}
}

// formStep is where a multi-step conversation currently is.
type formStep string

const (
	stepClientName   formStep = "client:name"
	stepClientVolume formStep = "client:volume"
	stepClientExpiry formStep = "client:expiry"
	// stepBindTgId reuses clientDraft.Name for the client being bound; nothing
	// else on the draft applies to it.
	stepBindTgId formStep = "client:bind"
	// stepClientSearch carries no draft at all -- the next message is the
	// search term. It goes through the form store anyway so that a search and
	// a half-finished new-client form cannot both be live in one chat.
	stepClientSearch formStep = "client:search"
)

// clientDraft is a client being assembled across several messages.
type clientDraft struct {
	Name       string
	VolumeGB   int64
	ExpiryDays int
	Inbounds   []uint
}

type formState struct {
	Step  formStep
	Draft clientDraft
	at    time.Time
}

// formStore holds one in-progress form per chat.
//
// Locked because updates are dispatched on worker goroutines -- the SDK runs
// handlers concurrently by default, so two messages from the same chat can be
// in flight at once. 3x-ui shipped this unlocked and had to fix the race
// afterwards.
type formStore struct {
	mu        sync.Mutex
	items     map[int64]*formState
	lastPrune time.Time
}

var forms = &formStore{items: map[int64]*formState{}}

func (s *formStore) set(chatID int64, step formStep, draft clientDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	s.items[chatID] = &formState{Step: step, Draft: draft, at: time.Now()}
}

// get returns the live form for a chat, treating an expired one as absent.
func (s *formStore) get(chatID int64) (formState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.items[chatID]
	if !ok || time.Since(f.at) > formTTL {
		return formState{}, false
	}
	return *f, true
}

func (s *formStore) clear(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, chatID)
}

func (s *formStore) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	s.lastPrune = now
	for id, f := range s.items {
		if now.Sub(f.at) > formTTL {
			delete(s.items, id)
		}
	}
}
