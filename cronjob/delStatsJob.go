package cronjob

import (
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
)

type DelStatsJob struct {
	service.StatsService
}

func NewDelStatsJob() *DelStatsJob {
	return &DelStatsJob{}
}

// Run reads trafficAge on every run instead of being given the value when the
// cron entry is registered. It is the same setting that switches traffic
// history on, and it can be lowered, raised or set to 0 without a restart:
// with the value captured at registration, turning history on in a panel that
// started with 0 left the cleanup job unregistered, so the rows that then
// started piling up were never aged out.
func (s *DelStatsJob) Run() {
	trafficAge, err := (&service.SettingService{}).GetTrafficAge()
	if err != nil {
		logger.Warning("Reading trafficAge failed, old statistics were not deleted: ", err)
		return
	}
	// 0 means keep no history at all: SettingService.Save empties the tables
	// when the value is saved and PurgeTrafficHistoryIfDisabled does it on
	// start-up, so there is nothing to age out -- and passing 0 down would
	// delete every row regardless of age.
	if trafficAge <= 0 {
		return
	}
	err = s.StatsService.DelOldStats(trafficAge)
	if err != nil {
		logger.Warning("Deleting old statistics failed: ", err)
		return
	}
	logger.Debug("Stats older than ", trafficAge, " days were deleted")
}
