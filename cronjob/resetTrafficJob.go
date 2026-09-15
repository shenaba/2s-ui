package cronjob

import (
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/robfig/cron/v3"
)

type ResetTrafficJob struct {
	service.ClientService
	service.ConfigService
	service.SettingService
	service.NodeSyncService
	schedule cron.Schedule
}

func NewResetTrafficJob(schedule cron.Schedule) *ResetTrafficJob {
	return &ResetTrafficJob{schedule: schedule}
}

func (s *ResetTrafficJob) Run() {
	loc, err := s.SettingService.GetTimeLocation()
	if err != nil {
		logger.Warning("ResetTrafficJob: get time location failed: ", err)
		return
	}
	now := time.Now().In(loc)

	last, err := s.SettingService.GetGlobalResetLast()
	if err != nil {
		logger.Warning("ResetTrafficJob: get last reset time failed: ", err)
		return
	}
	// Configured start date / next boundary not reached yet.
	//
	// Logged, because this was a silent return: the setting holds the *next*
	// boundary despite its name, and changing the schedule does not clear it.
	// Someone switching a monthly reset to a daily one would see nothing
	// happen for up to a month with nothing anywhere saying why.
	if last > now.Unix() {
		logger.Debug("ResetTrafficJob: next reset is at ",
			time.Unix(last, 0).In(loc).Format(time.RFC3339), ", nothing to do")
		return
	}

	if err = s.ClientService.ResetAllClientsTraffic(); err != nil {
		logger.Warning("ResetTrafficJob: reset all clients failed: ", err)
		return
	}
	// The reset re-enables depleted clients; fan it out like DepleteJob's
	// disable so nodes stop rejecting them before the hourly safety net.
	s.NodeSyncService.MarkAllDirty()
	go s.NodeSyncService.ReconcileDirtyOnline()

	// Before the bookkeeping write, not after.
	//
	// The clients were just re-enabled in the database, but the running core
	// still holds the old user list. When the write below failed this returned
	// early and the restart never happened, so every client the reset had just
	// paid for stayed disconnected -- and the watchdog does not help, because
	// it only starts a core that is down, not one running a stale config.
	if err = s.ConfigService.RestartCore(); err != nil {
		logger.Error("ResetTrafficJob: unable to restart core: ", err)
	}

	// Advance to the next boundary. schedule.Next returns the nearest upcoming
	// occurrence, so if several periods were missed (e.g. downtime) it snaps
	// forward instead of resetting once per missed period.
	next := s.schedule.Next(now)
	if err = s.SettingService.SetGlobalResetLast(next.Unix()); err != nil {
		logger.Warning("ResetTrafficJob: set last reset time failed: ", err)
		return
	}
	logger.Info("ResetTrafficJob: traffic reset for all clients; next reset at ", next.Format(time.RFC3339))
}
