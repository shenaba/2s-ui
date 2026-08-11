package cronjob

import (
	"sync"

	"github.com/shenaba/2s-ui/service"
)

type IpLimitJob struct {
	running sync.Mutex
}

func NewIpLimitJob() *IpLimitJob {
	return &IpLimitJob{}
}

func (j *IpLimitJob) Run() {
	// Separate from StatsJob on purpose: that one drains the core's counters
	// destructively and already skips overlapping runs, and folding a query plus
	// a full tracker walk into its critical path would only make it skip more.
	// The counts this publishes are read by the next stats push, one round later
	// at worst -- invisible against the 10s window they describe.
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()

	service.EnforceIPLimits()
}
