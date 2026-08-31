package service

import (
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"
)

// NotifyThresholds are the knobs the event sources need, as opposed to the
// delivery settings in notify.Config. They live on this side because the
// decision of whether something is worth reporting belongs with whoever
// observes it -- notify only decides how often to repeat itself.
type NotifyThresholds struct {
	ExpireDays  int
	VolumeBytes int64
	Cpu         int
	Memory      int
	// NodeFlap is how many consecutive failed probes make a node "down". At
	// NodesJob's 5s cadence a single dropped packet would otherwise produce a
	// down/up pair.
	NodeFlap int
}

func isNotifySecret(key string) bool {
	for _, k := range notifySecrets {
		if k == key {
			return true
		}
	}
	return false
}

// isNotifySecretFlag reports whether key is one of the computed has* companions
// GetAllSetting adds for the write-only credentials. They have no row in the
// settings table, but the settings form posts back every field it was handed,
// so they arrive on every save and must be dropped rather than written.
func isNotifySecretFlag(key string) bool {
	for _, k := range notifySecrets {
		if hasKey(k) == key {
			return true
		}
	}
	return false
}

// notifySettings reads every notify* row in one query, falling back to the
// defaults for anything not yet written. Doing it per key would be twenty round
// trips on a path that runs for every event.
func (s *SettingService) notifySettings() map[string]string {
	out := make(map[string]string, len(defaultValueMap))
	for key, def := range defaultValueMap {
		if strings.HasPrefix(key, "notify") {
			out[key] = def
		}
	}
	// Nil before InitDB and after CloseDBForTest. Every production caller runs
	// after app.Init, but degrading to the defaults is the right answer either
	// way -- a settings read is not worth a panic, and the defaults are what an
	// unconfigured panel would report anyway.
	db := database.GetDB()
	if db == nil {
		return out
	}
	var rows []model.Setting
	if err := db.Model(model.Setting{}).
		Where("key LIKE ?", "notify%").Find(&rows).Error; err != nil {
		logger.Warning("notify: read settings:", err)
		return out
	}
	for _, row := range rows {
		if _, known := out[row.Key]; known {
			out[row.Key] = row.Value
		}
	}
	return out
}

// GetNotifyConfig assembles the delivery config. It is called once per event,
// which is what lets a settings change take effect with no reload step.
func (s *SettingService) GetNotifyConfig() notify.Config {
	m := s.notifySettings()

	events := make(map[notify.Kind]bool)
	for _, name := range splitList(m["notifyEvents"]) {
		events[notify.Kind(name)] = true
	}

	lang := m["notifyLang"]
	if lang == "" {
		lang = notify.DefaultLang
	}

	return notify.Config{
		Enable: m["notifyEnable"] == "true",
		Proxy:  m["notifyProxy"],
		Lang:   lang,
		Events: events,
		Telegram: notify.TelegramConfig{
			Token:     m["notifyTgToken"],
			ChatIDs:   splitList(m["notifyTgChatId"]),
			APIServer: m["notifyTgApiServer"],
		},
		Webhook: notify.WebhookConfig{
			URL: m["notifyWebhookUrl"],
		},
		SMTP: notify.SMTPConfig{
			Host:     m["notifySmtpHost"],
			Port:     atoiOr(m["notifySmtpPort"], 587),
			User:     m["notifySmtpUser"],
			Pass:     m["notifySmtpPass"],
			From:     m["notifySmtpFrom"],
			To:       splitList(m["notifySmtpTo"]),
			Security: m["notifySmtpSecurity"],
		},
	}
}

// GetNotifyThresholds reads the observation thresholds.
func (s *SettingService) GetNotifyThresholds() NotifyThresholds {
	m := s.notifySettings()
	return NotifyThresholds{
		ExpireDays:  atoiOr(m["notifyExpireDays"], 0),
		VolumeBytes: int64(atoiOr(m["notifyVolumeGB"], 0)) << 30,
		Cpu:         atoiOr(m["notifyCpu"], 0),
		Memory:      atoiOr(m["notifyMemory"], 0),
		NodeFlap:    atoiOr(m["notifyNodeFlap"], 1),
	}
}

// NotifyEnabled reports whether notifications are on at all, for callers that
// want to skip assembling an event payload nobody will look at.
func (s *SettingService) NotifyEnabled() bool {
	return s.notifySettings()["notifyEnable"] == "true"
}

// NotifyWants reports whether any of these kinds is enabled, in one settings
// read.
//
// notify.Publish reads the settings itself for every event, which is the right
// shape for a source that fires once -- and the wrong one for a source with an
// item loop. The node probe publishes one event per online node every five
// seconds and the depletion pass one per client near a limit every minute, so
// on a panel with the alerts switched off those loops were paying a
// settings-table scan per item to compute "no". Ask once, outside the loop, and
// skip it. Config.Wants exists for the same reason -- see CheckOutboundJob,
// which skips a whole round of proxy handshakes on it.
func (s *SettingService) NotifyWants(kinds ...notify.Kind) bool {
	m := s.notifySettings()
	if m["notifyEnable"] != "true" {
		return false
	}
	enabled := splitList(m["notifyEvents"])
	for _, kind := range kinds {
		for _, name := range enabled {
			if name == string(kind) {
				return true
			}
		}
	}
	return false
}

// NotifyBackupEnabled reports whether the daily database backup should be
// uploaded. Gated on the master switch too: a backup upload is a notification
// like any other, and turning notifications off should stop all of them.
func (s *SettingService) NotifyBackupEnabled() bool {
	m := s.notifySettings()
	return m["notifyEnable"] == "true" && m["notifyBackup"] == "true"
}

// GetNotifyReportSpec returns the cron spec for the periodic report, or "" when
// it is off. Read at scheduler start, so changing it needs a panel restart --
// the same as globalReset, and for the same reason: cron entries are registered
// once with a fixed schedule.
func (s *SettingService) GetNotifyReportSpec() string {
	return strings.TrimSpace(s.notifySettings()["notifyReport"])
}

// BotConfig is what the interactive Telegram bot needs to connect and to decide
// who it will talk to.
type BotConfig struct {
	Enable    bool
	Token     string
	Proxy     string
	APIServer string
	// Admins is notifyTgChatId, reused rather than given its own setting: the
	// chats the panel already reports to are the chats allowed to command it,
	// and two lists would only ever drift apart.
	Admins []string
	// Lang is notifyLang, shared with the alerts for the same reason: an
	// operator who set their alerts to Chinese did not ask for an English
	// console.
	Lang string
}

// Connection reports the fields a reconnect has to react to. The supervisor
// compares this rather than the whole struct, so editing the admin list does
// not drop the polling connection.
func (c BotConfig) Connection() string {
	return c.Token + "\x00" + c.Proxy + "\x00" + c.APIServer
}

func (c BotConfig) Runnable() bool { return c.Enable && c.Token != "" }

func (s *SettingService) GetBotConfig() BotConfig {
	m := s.notifySettings()
	lang := m["notifyLang"]
	if lang == "" {
		lang = notify.DefaultLang
	}
	return BotConfig{
		Lang: lang,
		// Gated on the master switch as well: turning notifications off should
		// not leave a bot answering commands.
		Enable:    m["notifyEnable"] == "true" && m["notifyBotEnable"] == "true",
		Token:     m["notifyTgToken"],
		Proxy:     m["notifyProxy"],
		APIServer: m["notifyTgApiServer"],
		Admins:    splitList(m["notifyTgChatId"]),
	}
}

// splitList parses the comma-separated settings (enabled events, chat ids, mail
// recipients), dropping blanks so a trailing comma is not an empty entry.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}
