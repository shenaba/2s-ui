package cronjob

import (
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"gorm.io/gorm"
)

type ResetTrafficJob struct {
	service.ConfigService
	service.NodeSyncService
	// The last invalid spec warned about, so a bad one is reported once
	// rather than every minute.
	warnedSpec string
}

func NewResetTrafficJob() *ResetTrafficJob {
	return &ResetTrafficJob{}
}

// globalResetStep is what one tick of the job does.
type globalResetStep int

const (
	globalResetWait globalResetStep = iota
	// No boundary for the current schedule yet: record its first one. Never
	// reset on the spot -- that would charge an operator who merely edited the
	// schedule a reset they did not ask for.
	globalResetArm
	globalResetRun
)

// planGlobalReset decides one tick. armedFor is the schedule the stored
// boundary was armed under and want the current one (GlobalResetArmedFor);
// when they differ the boundary belongs to an old schedule or zone, and
// keeping it would leave a monthly-to-daily switch dead for up to a month.
//
// A boundary under the current schedule that is already past is run, however
// far past: one the panel was down for is caught up at the first tick after
// start, once, and the next one is taken from now. A panel upgraded from a
// version that did not record armedFor re-arms instead, so the upgrade itself
// never resets anyone.
func planGlobalReset(next int64, armedFor, want string, now time.Time) globalResetStep {
	if armedFor != want || next == 0 {
		return globalResetArm
	}
	if next > now.Unix() {
		return globalResetWait
	}
	return globalResetRun
}

// Run reads the spec on every tick rather than being scheduled on it. A cron
// entry's schedule is fixed at registration, and the spec used to be read once
// at panel start, so a saved change did nothing until the panel restarted.
func (s *ResetTrafficJob) Run() {
	settings := &s.ConfigService.SettingService
	spec, err := settings.GetGlobalReset()
	if err != nil {
		logger.Warning("ResetTrafficJob: get schedule failed: ", err)
		return
	}
	if spec == "" || spec == "off" {
		return
	}
	// Save rejects a bad spec, so this only trips on one stored before that
	// check existed.
	schedule, err := service.CronParser.Parse(spec)
	if err != nil {
		if spec != s.warnedSpec {
			logger.Warning("ResetTrafficJob: invalid cron spec <", spec, ">, the global traffic reset will not run: ", err)
			s.warnedSpec = spec
		}
		return
	}

	loc, err := settings.GetTimeLocation()
	if err != nil {
		logger.Warning("ResetTrafficJob: get time location failed: ", err)
		return
	}
	now := time.Now().In(loc)
	next, armedFor, err := settings.GetGlobalResetArmed()
	if err != nil {
		logger.Warning("ResetTrafficJob: get next reset time failed: ", err)
		return
	}
	want := service.GlobalResetArmedFor(spec, loc)
	// schedule.Next returns the nearest upcoming occurrence, so if several
	// periods were missed (e.g. downtime) it snaps forward instead of
	// resetting once per missed period.
	after := schedule.Next(now)

	switch planGlobalReset(next, armedFor, want, now) {
	case globalResetWait:
		return
	case globalResetArm:
		if err = settings.ArmGlobalReset(database.GetDB(), after.Unix(), want); err != nil {
			logger.Warning("ResetTrafficJob: set next reset time failed: ", err)
			return
		}
		logger.Info("ResetTrafficJob: next reset at ", after.Format(time.RFC3339))
		return
	}

	if next < now.Add(-time.Minute).Unix() {
		logger.Info("ResetTrafficJob: catching up the reset due at ",
			time.Unix(next, 0).In(loc).Format(time.RFC3339))
	}
	// The next boundary commits with the reset or not at all: written apart, a
	// failed write left the boundary in the past and every later tick reset
	// everyone again.
	inboundIds, err := s.ConfigService.ClientService.ResetAllClientsTraffic(func(tx *gorm.DB) error {
		return settings.ArmGlobalReset(tx, after.Unix(), want)
	})
	if err != nil {
		logger.Warning("ResetTrafficJob: reset all clients failed, retrying next tick: ", err)
		return
	}

	// The reset re-enables depleted clients. DepleteJob fanned the disable out
	// to nodes, so fan the re-enable out too — otherwise nodes keep rejecting
	// paid-up users until the hourly safety net.
	s.NodeSyncService.MarkAllDirty()
	go s.NodeSyncService.ReconcileDirtyOnline()

	// In place, as DepleteJob does, rather than by restarting the core. A
	// restart dropped every connection on the panel, and left every QUIC client
	// waiting out its idle timeout on a session the server had silently
	// discarded. Only the inbounds of re-enabled clients change at all.
	if err = s.ConfigService.ApplyReenabledUsers(inboundIds); err != nil {
		logger.Error("ResetTrafficJob: unable to update the core: ", err)
	}
	logger.Info("ResetTrafficJob: traffic reset for all clients; next reset at ", after.Format(time.RFC3339))
}
