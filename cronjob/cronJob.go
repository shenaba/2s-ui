package cronjob

import (
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field cron, optional leading seconds (6-field)
// and descriptors (@daily, @weekly, @every 10s, ...). Used both for the cron
// engine and for parsing the user-provided globalReset spec.
var cronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

type CronJob struct {
	cron *cron.Cron
}

func NewCronJob() *CronJob {
	return &CronJob{}
}

func (c *CronJob) Start(loc *time.Location, trafficAge int, statsBucketSeconds int64, globalReset string) error {
	c.cron = cron.New(cron.WithLocation(loc), cron.WithParser(cronParser))
	c.cron.Start()

	go func() {
		// Start stats job
		c.cron.AddJob("@every 10s", NewStatsJob(trafficAge > 0, statsBucketSeconds))
		// Enforce per-client IP limits (no-op unless some client sets one)
		c.cron.AddJob("@every 10s", NewIpLimitJob())
		// Start expiry job
		c.cron.AddJob("@every 1m", NewDepleteJob())
		// Periodic global traffic reset, only when a valid cron spec is configured
		if globalReset != "" && globalReset != "off" {
			schedule, err := cronParser.Parse(globalReset)
			if err != nil {
				logger.Warning("invalid globalReset cron spec <", globalReset, ">: ", err)
			} else {
				c.cron.AddJob(globalReset, NewResetTrafficJob(schedule))
			}
		}
		// Start deleting old stats
		if trafficAge > 0 {
			c.cron.AddJob("@daily", NewDelStatsJob(trafficAge))
		}
		// Start core if it is not running
		c.cron.AddJob("@every 5s", NewCheckCoreJob())
		// Probe managed nodes (in-memory snapshot; no-op with zero nodes)
		c.cron.AddJob("@every 5s", NewNodesJob())
		// Pull + merge node traffic into the master's per-client totals
		c.cron.AddJob("@every 1m", NewNodeTrafficJob())
		// Safety net: reconcile every online node to repair silent node-side drift
		c.cron.AddJob("@every 1h", NewNodeReconcileJob())
		// Sample CPU/memory for threshold alerts (no-op unless a threshold is set)
		c.cron.AddJob("@every 1m", NewCheckSystemJob())
		// Daily database backup to Telegram (no-op unless switched on)
		c.cron.AddJob("@daily", NewNotifyBackupJob())
		// Periodic status digest, only when a valid cron spec is configured.
		// Read here rather than passed in: it is a notification setting, and
		// like globalReset a cron entry's schedule is fixed at registration, so
		// changing it needs a panel restart either way.
		var settingService service.SettingService
		if spec := settingService.GetNotifyReportSpec(); spec != "" && spec != "off" {
			if _, err := cronParser.Parse(spec); err != nil {
				logger.Warning("invalid notifyReport cron spec <", spec, ">: ", err)
			} else {
				c.cron.AddJob(spec, NewNotifyReportJob())
			}
		}
		// database WAL checkpoint
		c.cron.AddJob("@every 10m", NewWALCheckpointJob())
	}()

	return nil
}

func (c *CronJob) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}
