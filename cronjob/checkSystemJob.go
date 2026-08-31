package cronjob

import (
	"time"

	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
)

// cpuSampleWindow is how long the job watches the CPU before deciding.
//
// Its own window, not gopsutil's shared "since the last call" baseline: the hub
// broadcasts status every 2s to any open panel tab and the bot's /status
// samples it as well, all through the same package-global, so a
// since-last-call reading here would be whatever interval those left behind --
// a two-second sliver during which one burst reads as sustained load. Three
// seconds is long enough that a single spike cannot fill it and short enough to
// sit comfortably inside a minute-cadence job.
const cpuSampleWindow = 3 * time.Second

// CheckSystemJob samples CPU and memory and reports threshold breaches.
//
// It exists because nothing else samples them on a schedule: ServerService
// computes them on demand, when a panel page asks. An operator who is not
// looking at the panel is exactly who this is for.
type CheckSystemJob struct {
	service.ServerService
	settingService service.SettingService
}

func NewCheckSystemJob() *CheckSystemJob {
	return new(CheckSystemJob)
}

func (j *CheckSystemJob) Run() {
	th := j.settingService.GetNotifyThresholds()
	// Either threshold at zero turns that half off; both off makes the whole
	// job a no-op, which is the default.
	if th.Cpu <= 0 && th.Memory <= 0 {
		return
	}

	// Sampled only when the CPU half is switched on: this blocks for the width
	// of the window, and a memory-only setup has no reason to wait for it.
	if th.Cpu > 0 {
		if cpuPct := j.ServerService.GetCpuPercentOver(cpuSampleWindow); cpuPct > float64(th.Cpu) {
			notify.Publish(notify.Event{
				Kind:    notify.CPUHigh,
				Subject: notify.Host(),
				Data:    &notify.MetricData{Percent: cpuPct, Threshold: th.Cpu},
			})
		}
	}

	if th.Memory > 0 {
		if pct, ok := service.UsageRatio(j.ServerService.GetMemInfo()); ok && pct > float64(th.Memory) {
			notify.Publish(notify.Event{
				Kind:    notify.MemoryHigh,
				Subject: notify.Host(),
				Data:    &notify.MetricData{Percent: pct, Threshold: th.Memory},
			})
		}
	}
}
