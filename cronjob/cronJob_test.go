package cronjob

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/service"

	"github.com/robfig/cron/v3"
)

// panicJob panics on its first run and counts every run after that.
type panicJob struct {
	runs atomic.Int32
}

func (j *panicJob) Run() {
	if j.runs.Add(1) == 1 {
		panic("job blew up")
	}
}

// blockingJob holds its run open until released, so a second tick overlaps it.
type blockingJob struct {
	started atomic.Int32
	release chan struct{}
	once    sync.Once
}

func (j *blockingJob) Run() {
	j.started.Add(1)
	<-j.release
}

// newChainedCron builds a scheduler with the same wrapper chain Start uses, so
// the test exercises the configuration rather than a copy of it.
func newChainedCron() *cron.Cron {
	return cron.New(
		cron.WithParser(service.CronParser),
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)
}

// robfig/cron does not recover a panicking job, and a panic in a job goroutine
// takes the whole process down -- gin only covers the HTTP side. Without the
// Recover wrapper this test does not fail, it crashes the test binary.
func TestCronRecoversAPanickingJob(t *testing.T) {
	c := newChainedCron()
	job := &panicJob{}
	if _, err := c.AddJob("@every 1s", job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	c.Start()
	defer c.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if job.runs.Load() >= 3 {
			return // it panicked once and kept being scheduled afterwards
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job ran %d times; the scheduler did not survive the panic", job.runs.Load())
}

// The stats and IP-limit jobs fire every ten seconds and can block that long on
// a database write lock. Overlapping runs of the stats job each drain the core
// traffic counters, so the second records what the first already took.
func TestCronSkipsAnOverlappingRun(t *testing.T) {
	c := newChainedCron()
	job := &blockingJob{release: make(chan struct{})}
	if _, err := c.AddJob("@every 1s", job); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	c.Start()
	defer c.Stop()
	defer job.once.Do(func() { close(job.release) })

	// cron.Every clamps anything under a second to one second, so this is
	// two ticks with the first run still held open.
	time.Sleep(2500 * time.Millisecond)
	if got := job.started.Load(); got != 1 {
		t.Errorf("job started %d times while the first run was still going, want 1", got)
	}

	job.once.Do(func() { close(job.release) })
	time.Sleep(2500 * time.Millisecond)
	if got := job.started.Load(); got < 2 {
		t.Errorf("job started %d times after being released, want it scheduled again", got)
	}
}

// AddJob returns an error that used to be dropped at every call site, so a bad
// spec registered nothing and said nothing.
func TestCronReportsABadSpec(t *testing.T) {
	c := newChainedCron()
	if _, err := c.AddJob("not a spec", &panicJob{}); err == nil {
		t.Fatal("a malformed spec must be reported, not swallowed")
	}
	if got := len(c.Entries()); got != 0 {
		t.Errorf("%d entries registered from a bad spec, want 0", got)
	}
}

// The renewal check runs on a fixed wall-clock minute so a panel restart does not reset
// it; the spec has to parse with the panel's own parser and stay put across calls.
func TestAcmeRenewSpec(t *testing.T) {
	spec := acmeRenewSpec("seed-a")
	if spec != acmeRenewSpec("seed-a") {
		t.Fatalf("acmeRenewSpec() is not stable: %q vs %q", spec, acmeRenewSpec("seed-a"))
	}
	for _, seed := range []string{"seed-a", ""} { // "" falls back to the hostname
		sched, err := service.CronParser.Parse(acmeRenewSpec(seed))
		if err != nil {
			t.Fatalf("acmeRenewSpec(%q) = %q does not parse: %v", seed, acmeRenewSpec(seed), err)
		}
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		first := sched.Next(from)
		if second := sched.Next(first); second.Sub(first) != time.Hour {
			t.Errorf("%q: runs %v apart, want hourly", acmeRenewSpec(seed), second.Sub(first))
		}
	}
	// Different installs must spread out rather than share one minute: across many
	// seeds, far more than one minute has to come up.
	minutes := map[string]bool{}
	for i := 0; i < 200; i++ {
		minutes[acmeRenewSpec(fmt.Sprintf("install-%d", i))] = true
	}
	if len(minutes) < 30 {
		t.Errorf("200 seeds landed on only %d distinct minutes", len(minutes))
	}
}
