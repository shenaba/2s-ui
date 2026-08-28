package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
// CPU, MEM and DISK are left untranslated on purpose -- they read the same in
// every locale this panel ships, and a percentage beside them needs no help.
// The words around the counts do get translated, because "Clients 5 (3 enabled,
// 0 online)" does not.
func StatusDigest(lang string) string {
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
	b.WriteString(fmt.Sprintf("\n%s %d (%d %s, %d %s)",
		notify.Label(lang, "digest.clients"), total,
		enabled, notify.Label(lang, "digest.enabled"),
		online, notify.Label(lang, "digest.online")))

	// Only when this panel actually manages nodes -- on a single-panel install
	// the line would always read "Nodes 0/0" and train the reader to skip it.
	if statuses := nodeService.GetStatuses(); len(statuses) > 0 {
		up := 0
		for _, s := range statuses {
			if s.State == "online" {
				up++
			}
		}
		b.WriteString(fmt.Sprintf("\n%s %d/%d %s",
			notify.Label(lang, "digest.nodes"), up, len(statuses),
			notify.Label(lang, "digest.nodesOnline")))
	}

	core := notify.Label(lang, "digest.stopped")
	if configService.CoreRunning() {
		core = notify.Label(lang, "digest.running")
	}
	b.WriteString("\n" + notify.Label(lang, "digest.core") + " " + core)

	return b.String()
}

// digestListLimit bounds each list in ClientAlertDigest. A panel whose whole
// client base expired on the same day would otherwise mail out thousands of
// names, and whatever is dropped is always counted -- a silently truncated list
// reads as if it were the whole thing.
const digestListLimit = 20

// ClientAlertDigest lists the clients that have run out and the ones about to,
// or "" when there is nothing to say.
//
// Separate from StatusDigest, which is also the bot's /status and has to stay
// one screen. This is for the scheduled report, where the names are the point:
// an operator reading "Clients 120 (118 enabled)" over breakfast cannot tell
// which two stopped working.
//
// The "expiring" half is read through ClientService.findExpiringClients so the
// report and the alerts agree on what "about to" means -- both then honour the
// notifyExpireDays / notifyVolumeGB thresholds, including the operator's
// decision to switch either of them off.
func ClientAlertDigest(lang string) string {
	db := database.GetDB()
	now := time.Now().Unix()

	var b strings.Builder

	var depleted []struct{ Name string }
	err := db.Model(model.Client{}).
		Where("enable = false AND ((volume > 0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", now).
		Order("name").Select("name").Scan(&depleted).Error
	if err != nil {
		logger.Warning("digest: depleted clients: ", err)
	} else if len(depleted) > 0 {
		names := make([]string, 0, len(depleted))
		for _, row := range depleted {
			names = append(names, row.Name)
		}
		writeDigestSection(&b, lang, "digest.depleted", names)
	}

	var clientService ClientService
	expiring, err := clientService.findExpiringClients(db, now)
	if err != nil {
		logger.Warning("digest: expiring clients: ", err)
	} else if len(expiring) > 0 {
		names := make([]string, 0, len(expiring))
		for _, c := range expiring {
			names = append(names, c.Name)
		}
		sort.Strings(names)
		writeDigestSection(&b, lang, "digest.expiring", names)
	}

	return b.String()
}

func writeDigestSection(b *strings.Builder, lang, titleKey string, names []string) {
	b.WriteString("\n\n" + notify.Label(lang, titleKey))
	shown := names
	if len(shown) > digestListLimit {
		shown = shown[:digestListLimit]
	}
	for _, name := range shown {
		b.WriteString("\n" + name)
	}
	if dropped := len(names) - len(shown); dropped > 0 {
		b.WriteString("\n" + notify.LabelWith(lang, "digest.more",
			map[string]string{"count": strconv.Itoa(dropped)}))
	}
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
