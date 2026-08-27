package service

import (
	"fmt"
	"strings"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"
)

// StatusDigest builds the one-screen panel summary.
//
// Shared by the scheduled report and the bot's /status, which want exactly the
// same figures -- keeping two copies would let them drift into disagreeing
// about the state of the same panel.
//
// The labels are left in English rather than translated. They are numbers with
// short technical tags (CPU, MEM, DISK) that read the same in every locale this
// panel ships, and routing them through the message table would mean a
// translation round-trip for every future line of something whose whole point
// is being skimmable.
func StatusDigest() string {
	var serverService ServerService
	var statsService StatsService
	var nodeService NodeService
	var configService ConfigService

	var b strings.Builder
	b.WriteString(notify.Host())

	status := serverService.GetStatus("cpu,mem,dsk")
	cpu, _ := (*status)["cpu"].(float64)
	line := fmt.Sprintf("\nCPU %.1f%%", cpu)
	if pct, ok := UsageRatio((*status)["mem"]); ok {
		line += fmt.Sprintf(" · MEM %.0f%%", pct)
	}
	if pct, ok := UsageRatio((*status)["dsk"]); ok {
		line += fmt.Sprintf(" · DISK %.0f%%", pct)
	}
	b.WriteString(line)

	var total, enabled int64
	db := database.GetDB()
	if err := db.Model(model.Client{}).Count(&total).Error; err != nil {
		logger.Warning("digest: client count: ", err)
	}
	if err := db.Model(model.Client{}).Where("enable = ?", true).Count(&enabled).Error; err != nil {
		logger.Warning("digest: enabled count: ", err)
	}
	online := 0
	if o, err := statsService.GetOnlines(); err == nil {
		online = len(o.User)
	}
	b.WriteString(fmt.Sprintf("\nClients %d (%d enabled, %d online)", total, enabled, online))

	// Only when this panel actually manages nodes -- on a single-panel install
	// the line would always read "Nodes 0/0" and train the reader to skip it.
	if statuses := nodeService.GetStatuses(); len(statuses) > 0 {
		up := 0
		for _, s := range statuses {
			if s.State == "online" {
				up++
			}
		}
		b.WriteString(fmt.Sprintf("\nNodes %d/%d online", up, len(statuses)))
	}

	core := "stopped"
	if configService.CoreRunning() {
		core = "running"
	}
	b.WriteString("\nCore " + core)

	return b.String()
}

// UsageRatio turns one of ServerService's {current,total} maps into a
// percentage.
//
// It reports failure rather than guessing: those maps come back empty when the
// gopsutil call failed, and a zero total divides into a NaN that compares false
// against every threshold -- silently disabling the alert that reads it -- and
// formats as "NaN%" in the digest.
func UsageRatio(v any) (float64, bool) {
	info, ok := v.(map[string]interface{})
	if !ok {
		return 0, false
	}
	current, ok := info["current"].(uint64)
	if !ok {
		return 0, false
	}
	total, ok := info["total"].(uint64)
	if !ok || total == 0 {
		return 0, false
	}
	return float64(current) / float64(total) * 100, true
}
