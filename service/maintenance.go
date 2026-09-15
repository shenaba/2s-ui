package service

import (
	"strconv"
	"sync/atomic"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
)

// maintenanceKey is the settings row that remembers the switch across a panel
// restart, so a reboot in the middle of maintenance does not quietly put users
// back online.
//
// It is deliberately absent from defaultValueMap: that map is the set of rows
// GetAllSetting seeds and hands to the settings form, and this is runtime state
// with an action of its own rather than a field an operator edits there.
const maintenanceKey = "maintenance"

// maintenanceMode is the authority at runtime; the settings row above is only
// the durable copy of it.
//
// Eight paths start the core -- boot, the five-second watchdog, every config
// save, both restart endpoints, the scheduled traffic reset, the Telegram bot
// and the rollback after a failed self-update -- so the switch is read where
// the core is started rather than at each caller: one any of them could undo
// would not be a switch. Reading it from the database there would put a SELECT
// on the watchdog's five-second tick and, worse, would need an answer for a
// failed read -- treating it as off lets one transient database error undo the
// maintenance, treating it as on takes the panel offline for that same error.
// In memory there is no third answer to give.
var maintenanceMode atomic.Bool

// loadMaintenance primes the flag from the stored row. Called once from
// NewConfigService, which app.Init runs after the database is open and before
// anything can start the core.
func loadMaintenance() {
	var settingService SettingService
	on, err := settingService.GetMaintenance()
	if err != nil {
		// Not fatal: the worst case is a panel that comes up serving clients,
		// which is what it does without this feature at all.
		logger.Warning("cannot read the maintenance setting, starting normally: ", err)
		return
	}
	maintenanceMode.Store(on)
	if on {
		logger.Warning("maintenance mode is on: the core stays stopped and clients cannot connect")
	}
}

// InMaintenance reports whether the operator has taken the core out of service.
// The panel needs it to tell a core that was stopped on purpose from one that
// crashed, which otherwise look identical from outside.
func (s *ConfigService) InMaintenance() bool {
	return maintenanceMode.Load()
}

// SetMaintenance takes the core out of service, or puts it back.
//
// The row is written and the flag published before the core is touched: the
// watchdog fires every five seconds, and one that still read the old value
// would restart what this just stopped.
func (s *ConfigService) SetMaintenance(enabled bool) error {
	if err := s.SettingService.setMaintenance(enabled); err != nil {
		return err
	}
	maintenanceMode.Store(enabled)
	if enabled {
		logger.Warning("maintenance mode on: stopping the core, clients cannot connect until it is switched off")
		return s.StopCore()
	}
	logger.Info("maintenance mode off: starting the core")
	return s.StartCore()
}

// GetMaintenance reads the stored switch. A missing row is not an error: the
// key is not seeded with the other settings, so its absence only means the
// switch has never been used.
func (s *SettingService) GetMaintenance() (bool, error) {
	setting, err := s.getSetting(maintenanceKey)
	if database.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(setting.Value)
}

func (s *SettingService) setMaintenance(enabled bool) error {
	return s.saveSetting(maintenanceKey, strconv.FormatBool(enabled))
}
