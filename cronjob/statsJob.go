package cronjob

import (
	"sync"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
)

type StatsJob struct {
	service.StatsService
	bucketSeconds int64
	running       sync.Mutex
}

func NewStatsJob(bucketSeconds int64) *StatsJob {
	return &StatsJob{
		bucketSeconds: bucketSeconds,
	}
}

func (s *StatsJob) Run() {
	// robfig/cron does not wait for the previous run, and SaveStats drains the
	// core's counters destructively — two overlapping runs would split one
	// sample between them and race on the shared online lists. The push work
	// added below makes a slow run likelier, so skip instead of overlapping.
	if !s.running.TryLock() {
		return
	}
	defer s.running.Unlock()

	// Traffic history is on exactly when trafficAge > 0, and the switch is read
	// on every run rather than when this job was registered: a cron entry's
	// parameters are fixed at registration, so a value captured there left a
	// saved 0 -- and the way back -- without effect until the panel restarted.
	err := s.StatsService.SaveStatsForCurrentSettings(s.bucketSeconds)
	if err != nil {
		logger.Warning("Get stats failed: ", err)
	}
	// Push even after a failed flush — the HTTP poll served the stale onlines
	// in that case too. Must run on this goroutine: the payload snapshots
	// onlineResources right after SaveStats rewrote it.
	service.HubAfterStatsFlush()
	// Same goroutine for the same reason: presence is judged on the list this
	// flush just published.
	service.ObservePresence()
}
