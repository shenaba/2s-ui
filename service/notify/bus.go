package notify

import (
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"
)

const (
	// busBuffer bounds what Publish can hand off before it starts dropping.
	busBuffer = 256
	// queueSize bounds what one subscriber may hold undelivered. Small on
	// purpose: a channel that has to hold hundreds of alerts is one whose
	// receiver is not coming back, and the newest alert matters more than the
	// backlog.
	queueSize = 64
)

// subscriber is one delivery channel plus the worker that drains it.
type subscriber struct {
	name   string
	handle func(Event)
	queue  chan Event
	quit   chan struct{}
}

// Bus is an in-process fan-out: Publish never blocks, and every subscriber gets
// every event on its own goroutine.
//
// The two-level queueing is the whole point. Senders here block on network I/O
// for as long as their timeout allows, and a single shared worker would let a
// Telegram call that is waiting on a dead connection hold up the Webhook
// delivery of the same alert -- which is exactly the delivery that was supposed
// to cover for Telegram being unreachable. Per-subscriber queues also bound the
// damage: a wedged subscriber fills its own 64 slots and drops, instead of
// backing up into the bus or spawning a goroutine per event.
//
// Filtering is the subscriber's job. The bus does not know which events an
// operator enabled.
type Bus struct {
	ch      chan Event
	mu      sync.RWMutex
	subs    []*subscriber
	done    chan struct{}
	wg      sync.WaitGroup
	stopped bool
}

// NewBus starts the dispatch loop. Callers own the returned bus and must Stop it.
func NewBus() *Bus {
	b := &Bus{
		ch:   make(chan Event, busBuffer),
		done: make(chan struct{}),
	}
	b.wg.Add(1)
	go b.dispatch()
	return b
}

// Subscribe registers handle under name, replacing any subscriber already using
// that name. Each subscriber is driven by its own worker, so handle is never
// called concurrently with itself and sees events in publication order.
func (b *Bus) Subscribe(name string, handle func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.removeLocked(name)
	s := &subscriber{
		name:   name,
		handle: handle,
		queue:  make(chan Event, queueSize),
		quit:   make(chan struct{}),
	}
	b.subs = append(b.subs, s)
	b.wg.Add(1)
	go b.runWorker(s)
}

// Unsubscribe stops and drops the named subscriber. Unknown names are ignored,
// which is what lets the reload path unsubscribe unconditionally.
func (b *Bus) Unsubscribe(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeLocked(name)
}

func (b *Bus) removeLocked(name string) {
	for i, s := range b.subs {
		if s.name == name {
			close(s.quit)
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

// Publish hands an event to the bus and returns immediately.
//
// It is called from cron jobs and from the login path, so it must never block:
// a full buffer drops the event and logs it. Notifications are not a ledger --
// losing one is better than stalling a login behind an unreachable Telegram.
func (b *Bus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	select {
	case b.ch <- e:
	default:
		logger.Warning("notify: bus buffer full, dropping ", string(e.Kind))
	}
}

// dispatch fans each event out to every subscriber's own queue, never blocking
// on a slow one.
func (b *Bus) dispatch() {
	defer b.wg.Done()
	for {
		select {
		case e := <-b.ch:
			b.mu.RLock()
			for _, s := range b.subs {
				select {
				case s.queue <- e:
				default:
					logger.Warning("notify: subscriber ", s.name, " queue full, dropping ", string(e.Kind))
				}
			}
			b.mu.RUnlock()
		case <-b.done:
			return
		}
	}
}

// runWorker delivers one subscriber's queue serially.
func (b *Bus) runWorker(s *subscriber) {
	defer b.wg.Done()
	for {
		select {
		case e := <-s.queue:
			safeCall(s.name, s.handle, e)
		case <-s.quit:
			return
		case <-b.done:
			return
		}
	}
}

// safeCall keeps a panicking handler from taking the process down. Events reach
// handlers from cron jobs and from the login path, and the scheduler is built
// without cron.Recover -- a nil dereference while formatting a message would
// otherwise kill the panel over a notification.
func safeCall(name string, handle func(Event), e Event) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("notify: subscriber ", name, " panicked on ", string(e.Kind), ": ", r)
		}
	}()
	handle(e)
}

// Stop shuts the bus down. Buffered and queued events may be dropped; handlers
// already running are waited for. After Stop, Subscribe is a no-op -- which is
// also what keeps its wg.Add from racing Wait, since both go through b.mu.
func (b *Bus) Stop() {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.stopped = true
	b.mu.Unlock()
	close(b.done)
	b.wg.Wait()
}
