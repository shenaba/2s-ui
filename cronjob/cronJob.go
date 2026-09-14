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
	// Recover: robfig/cron does not recover a panicking job, and a panic in a
	// job goroutine takes the whole process down -- gin only covers the HTTP
	// side. Fourteen jobs run here, several of them doing network I/O against
	// managed nodes and notification endpoints on data this panel does not own.
	//
	// SkipIfStillRunning: the stats and IP-limit jobs fire every ten seconds and
	// can block that long on a database write lock. Overlapping runs of the
	// stats job each drain the core traffic counters, so the second one records
	// what the first already took.
	c.cron = cron.New(
		cron.WithLocation(loc),
		cron.WithParser(cronParser),
		// Order matters, and not in the obvious direction. Chain applies the
		// first wrapper outermost, and SkipIfStillRunning returns its token
		// with a plain `ch <- v` after j.Run() rather than a defer -- so with
		// Recover on the outside a panicking job unwinds straight past that
		// line, the token is never returned, and the job is skipped forever
		// after. Recover has to be the inner one, where it turns the panic
		// into an ordinary return before Skip's bookkeeping runs.
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	// Registered before Start, and not from a goroutine racing it. Every one of
	// these used to be added from a goroutine started after the scheduler was
	// already running, so which jobs existed at any given tick was a race -- and
	// AddJob returns an error that was dropped, so a bad spec registered nothing
	// and said nothing.
	addJob := func(spec string, job cron.Job, name string) {
		if _, err := c.cron.AddJob(spec, job); err != nil {
			logger.Warning("unable to schedule ", name, " <", spec, ">: ", err)
		}
	}

	addJob("@every 10s", NewStatsJob(trafficAge > 0, statsBucketSeconds), "stats job")
	// Enforce per-client IP limits (no-op unless some client sets one)
	addJob("@every 10s", NewIpLimitJob(), "ip limit job")
	addJob("@every 1m", NewDepleteJob(), "deplete job")
	// Periodic global traffic reset, only when a valid cron spec is configured
	if globalReset != "" && globalReset != "off" {
		schedule, err := cronParser.Parse(globalReset)
		if err != nil {
			logger.Warning("invalid globalReset cron spec <", globalReset, ">: ", err)
		} else {
			addJob(globalReset, NewResetTrafficJob(schedule), "traffic reset job")
		}
	}
	if trafficAge > 0 {
		addJob("@daily", NewDelStatsJob(trafficAge), "old stats cleanup")
	}
	// Start core if it is not running
	addJob("@every 5s", NewCheckCoreJob(), "core watchdog")
	// Probe managed nodes (in-memory snapshot; no-op with zero nodes)
	addJob("@every 5s", NewNodesJob(), "node probe")
	// Pull + merge node traffic into the master's per-client totals
	addJob("@every 1m", NewNodeTrafficJob(), "node traffic")
	// Safety net: reconcile every online node to repair silent node-side drift
	addJob("@every 1h", NewNodeReconcileJob(), "node reconcile")
	// Sample CPU/memory for threshold alerts (no-op unless a threshold is set)
	addJob("@every 1m", NewCheckSystemJob(), "system check")
	// Probe outbound reachability (no-op unless the alerts are enabled)
	addJob("@every 5m", NewCheckOutboundJob(), "outbound check")
	// Daily database backup to Telegram (no-op unless switched on)
	addJob("@daily", NewNotifyBackupJob(), "notify backup")
	// Periodic status digest, only when a valid cron spec is configured.
	// Read here rather than passed in: it is a notification setting, and
	// like globalReset a cron entry's schedule is fixed at registration, so
	// changing it needs a panel restart either way.
	var settingService service.SettingService
	if spec := settingService.GetNotifyReportSpec(); spec != "" && spec != "off" {
		if _, err := cronParser.Parse(spec); err != nil {
			logger.Warning("invalid notifyReport cron spec <", spec, ">: ", err)
		} else {
			addJob(spec, NewNotifyReportJob(), "notify report")
		}
	}
	// database WAL checkpoint
	addJob("@every 10m", NewWALCheckpointJob(), "WAL checkpoint")

	c.cron.Start()
	return nil
}

func (c *CronJob) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}
