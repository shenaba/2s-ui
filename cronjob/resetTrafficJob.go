package cronjob

import (
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
)

type ResetTrafficJob struct {
	service.ClientService
	service.InboundService
	service.SettingService
	service.NodeSyncService
}

func NewResetTrafficJob() *ResetTrafficJob {
	return &ResetTrafficJob{}
}

// Run reads the spec on every tick rather than being scheduled on it. A cron
// entry's schedule is fixed at registration, and the spec used to be read once
// at panel start, so a saved change did nothing until the panel restarted.
//
// globalResetLast holds the next armed boundary despite its name; 0 means none
// is armed, which is what SettingService.Save leaves behind whenever the spec or
// the timezone changes.
func (s *ResetTrafficJob) Run() {
	spec, err := s.SettingService.GetGlobalReset()
	if err != nil {
		logger.Warning("ResetTrafficJob: get schedule failed: ", err)
		return
	}
	if spec == "" || spec == "off" {
		return
	}
	// Save rejects a bad spec, so this only trips on one stored before that
	// check existed. Debug, not Warning: it would repeat every minute.
	schedule, err := service.CronParser.Parse(spec)
	if err != nil {
		logger.Debug("ResetTrafficJob: invalid cron spec <", spec, ">: ", err)
		return
	}

	loc, err := s.SettingService.GetTimeLocation()
	if err != nil {
		logger.Warning("ResetTrafficJob: get time location failed: ", err)
		return
	}
	now := time.Now().In(loc)

	next, err := s.SettingService.GetGlobalResetLast()
	if err != nil {
		logger.Warning("ResetTrafficJob: get next reset time failed: ", err)
		return
	}
	// A new or changed schedule: arm its first boundary rather than reset now.
	// Resetting on the spot would charge an operator who merely edited the
	// schedule a reset they did not ask for.
	if next == 0 {
		next = schedule.Next(now).Unix()
		if err = s.SettingService.SetGlobalResetLast(next); err != nil {
			logger.Warning("ResetTrafficJob: set next reset time failed: ", err)
			return
		}
		logger.Info("ResetTrafficJob: next reset at ", time.Unix(next, 0).In(loc).Format(time.RFC3339))
		return
	}
	if next > now.Unix() {
		return
	}

	inboundIds, err := s.ClientService.ResetAllClientsTraffic()
	if err != nil {
		logger.Warning("ResetTrafficJob: reset all clients failed: ", err)
		return
	}
	// The reset re-enables depleted clients; fan it out like DepleteJob's
	// disable so nodes stop rejecting them before the hourly safety net.
	s.NodeSyncService.MarkAllDirty()
	go s.NodeSyncService.ReconcileDirtyOnline()

	// Before the bookkeeping write, not after: the clients were just re-enabled
	// in the database but the running core still holds the old user list, and
	// a failed write used to return early and leave them disconnected.
	//
	// In place, as DepleteJob does, rather than by restarting the core. A
	// restart dropped every connection on the panel, and left every QUIC client
	// waiting out its idle timeout on a session the server had silently
	// discarded. Only the inbounds of re-enabled clients change at all.
	if len(inboundIds) > 0 {
		if err = s.InboundService.UpdateInboundsUsers(database.GetDB(), inboundIds); err != nil {
			logger.Error("ResetTrafficJob: unable to update inbound users: ", err)
		}
	}

	// Advance to the next boundary. schedule.Next returns the nearest upcoming
	// occurrence, so if several periods were missed (e.g. downtime) it snaps
	// forward instead of resetting once per missed period.
	after := schedule.Next(now)
	if err = s.SettingService.SetGlobalResetLast(after.Unix()); err != nil {
		logger.Warning("ResetTrafficJob: set next reset time failed: ", err)
		return
	}
	logger.Info("ResetTrafficJob: traffic reset for all clients; next reset at ", after.Format(time.RFC3339))
}
