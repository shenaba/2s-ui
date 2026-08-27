package cronjob

import (
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
)

// CheckSystemJob samples CPU and memory and reports threshold breaches.
//
// It exists because nothing else samples them on a schedule: ServerService
// computes them on demand, when a panel page asks. An operator who is not
// looking at the panel is exactly who this is for.
type CheckSystemJob struct {
	service.ServerService
	settingService service.SettingService
	// primed guards the first reading. gopsutil's cpu.Percent(0, false)
	// reports the average since the previous call, so the first one covers
	// everything since boot (or since whenever a panel page last asked) and
	// says nothing about the last minute. Alerting on it would mean a busy
	// startup pages someone about load that has already passed.
	primed bool
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

	cpuPct := j.ServerService.GetCpuPercent()
	if !j.primed {
		j.primed = true
		return
	}

	if th.Cpu > 0 && cpuPct > float64(th.Cpu) {
		notify.Publish(notify.Event{
			Kind:    notify.CPUHigh,
			Subject: notify.Host(),
			Data:    &notify.MetricData{Percent: cpuPct, Threshold: th.Cpu},
		})
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
