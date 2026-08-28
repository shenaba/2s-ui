package cronjob

import (
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
)

// CheckOutboundJob reports outbounds that stop being reachable, and reports
// them again when they recover.
//
// sing-box exposes no outbound health the panel can read -- urltest picks a
// member for its own routing and keeps the verdict to itself -- so this dials
// each outbound on a schedule through the running core, which is the same
// measurement the panel's own "test outbound" button makes.
//
// That is a full proxy handshake per outbound, which is why it runs every five
// minutes rather than at the node probe's cadence, and why it does nothing at
// all unless the operator asked for these alerts.
type CheckOutboundJob struct {
	settingService  service.SettingService
	outboundService service.OutboundService
	configService   service.ConfigService
}

func NewCheckOutboundJob() *CheckOutboundJob {
	return new(CheckOutboundJob)
}

func (j *CheckOutboundJob) Run() {
	cfg := j.settingService.GetNotifyConfig()
	if !cfg.Wants(notify.OutboundDown) && !cfg.Wants(notify.OutboundUp) {
		return
	}
	// A stopped core answers "core not running" for every outbound, which would
	// report the entire config as down on the way through a restart and then
	// report all of it back up. Nothing is measurable until the core is up, so
	// wait for the next pass.
	if !j.configService.CoreRunning() {
		return
	}

	tags, err := j.outboundService.ProbeTargets()
	if err != nil {
		logger.Warning("notify: listing the outbounds to probe failed: ", err)
		return
	}

	url := j.settingService.GetNotifyOutboundUrl()
	for _, tag := range tags {
		// Serial on purpose: every one of these dials out through the same core
		// the panel's users are on, and probing thirty at once is a burst of
		// handshakes competing with them for an alert nobody is waiting on.
		result := j.configService.CheckOutbound(tag, url)
		if result.OK {
			notify.Publish(notify.Event{
				Kind:    notify.OutboundUp,
				Subject: tag,
				Data:    &notify.OutboundData{LatencyMs: result.Delay},
			})
			continue
		}
		// The core can stop mid-pass. Re-check rather than matching on the
		// error string: from here on every answer would say "core not running",
		// which is a statement about the core, not about the outbound.
		if !j.configService.CoreRunning() {
			return
		}
		notify.Publish(notify.Event{
			Kind:    notify.OutboundDown,
			Subject: tag,
			Data:    &notify.OutboundData{Err: result.Error},
		})
	}
}
