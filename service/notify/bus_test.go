package notify

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

func init() {
	// The bus logs dropped events through logger, whose handle is nil until
	// something initialises it -- app.Init is the only caller that normally
	// does, so a test that reaches a drop path would panic instead of failing.
	logger.InitLogger(logging.ERROR)
}

// waitFor fails the test if c does not fire. The timeout is generous because it
// only has to be longer than a scheduling hiccup, never longer than real work.
func waitFor(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// The reason for per-subscriber workers. Senders block on network I/O for up to
// sendTimeout, and with one shared worker a Telegram call waiting on a dead
// connection would hold up the webhook delivery of the same alert -- the very
// delivery meant to cover for Telegram being unreachable.
func TestBusBlockingSubscriberDoesNotStallOthers(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	release := make(chan struct{})
	blocked := make(chan struct{})
	var blockedOnce sync.Once
	b.Subscribe("slow", func(Event) {
		blockedOnce.Do(func() { close(blocked) })
		<-release
	})

	got := make(chan struct{})
	var gotOnce sync.Once
	b.Subscribe("fast", func(Event) { gotOnce.Do(func() { close(got) }) })

	b.Publish(Event{Kind: CoreUp})
	waitFor(t, blocked, "the slow subscriber to start blocking")
	waitFor(t, got, "the fast subscriber while the slow one is blocked")
	close(release)
}

// A nil dereference while formatting a message must not take the panel down:
// events are published from cron jobs and from the login path, and the cron
// scheduler is built without cron.Recover.
func TestBusPanicRecovery(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	b.Subscribe("boom", func(Event) { panic("formatting blew up") })

	got := make(chan struct{})
	var once sync.Once
	b.Subscribe("survivor", func(Event) { once.Do(func() { close(got) }) })

	b.Publish(Event{Kind: NodeDown, Subject: "n1"})
	waitFor(t, got, "the surviving subscriber after a panicking one")

	// The panicking worker must still be alive for the next event.
	again := make(chan struct{})
	b.Subscribe("boom", func(Event) { close(again) })
	b.Publish(Event{Kind: NodeUp, Subject: "n1"})
	waitFor(t, again, "the replaced subscriber")
}

// Publish is called from the login path. A wedged subscriber must cost dropped
// notifications, never a stalled login.
func TestBusPublishNeverBlocks(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	var once sync.Once
	b.Subscribe("wedged", func(Event) {
		once.Do(func() { close(started) })
		<-release
	})

	b.Publish(Event{Kind: CoreUp})
	waitFor(t, started, "the wedged subscriber to start")

	// Far more than queueSize, so the subscriber queue is certainly full.
	done := make(chan struct{})
	go func() {
		for i := 0; i < queueSize*4; i++ {
			b.Publish(Event{Kind: NodeDown, Subject: "n1"})
		}
		close(done)
	}()
	waitFor(t, done, "Publish to return with a full subscriber queue")
}

// One subscriber is never run concurrently with itself, so a handler can hold
// per-channel state without locking it.
func TestBusSubscriberRunsSerially(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	const events = 50
	var (
		inFlight atomic.Int32
		overlaps atomic.Int32
		wg       sync.WaitGroup
	)
	wg.Add(events)
	b.Subscribe("serial", func(Event) {
		if inFlight.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(time.Millisecond)
		inFlight.Add(-1)
		wg.Done()
	})

	for i := 0; i < events; i++ {
		b.Publish(Event{Kind: CoreUp})
	}
	wg.Wait()
	if n := overlaps.Load(); n != 0 {
		t.Fatalf("subscriber ran concurrently with itself %d times", n)
	}
}

// Stop must not deadlock or panic when a handler is mid-flight, and must be
// safe to call twice -- APP.Stop and a failed start can both reach it.
func TestBusStopIsIdempotent(t *testing.T) {
	b := NewBus()
	b.Subscribe("noop", func(Event) {})
	b.Publish(Event{Kind: CoreUp})
	b.Stop()
	b.Stop()

	// Subscribing after Stop is a no-op rather than a panic; it is also what
	// keeps Subscribe's wg.Add from racing the Wait inside Stop.
	b.Subscribe("late", func(Event) { t.Error("a subscriber added after Stop was called") })
	b.Publish(Event{Kind: CoreUp})
}
