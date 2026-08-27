package cronjob

import (
	"fmt"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/service/notify"
)

// NotifyBackupJob uploads a database backup to Telegram on a schedule.
//
// The panel already has a backup button, but pressing it requires being at the
// panel -- which is exactly the situation an operator is not in when they need
// the backup. This is the same export, delivered somewhere off the box.
type NotifyBackupJob struct {
	settingService service.SettingService
}

func NewNotifyBackupJob() *NotifyBackupJob {
	return new(NotifyBackupJob)
}

func (j *NotifyBackupJob) Run() {
	if !j.settingService.NotifyBackupEnabled() {
		return
	}
	// Same export the panel's own backup button produces, including the tables
	// that used to be missing from it -- an automated backup that silently
	// drops the node credentials would be worse than none, because nobody
	// checks a backup that arrives every day.
	data, err := database.GetDb("")
	if err != nil {
		logger.Warning("notify: backup export failed: ", err)
		return
	}
	name := fmt.Sprintf("2s-ui-%s.db", time.Now().Format("20060102-150405"))
	if err := notify.SendBackup(name, data, notify.Host()); err != nil {
		logger.Warning("notify: backup upload failed: ", err)
	}
}

// NotifyReportJob sends a periodic status digest.
type NotifyReportJob struct {
	settingService service.SettingService
}

func NewNotifyReportJob() *NotifyReportJob {
	return new(NotifyReportJob)
}

func (j *NotifyReportJob) Run() {
	if !j.settingService.NotifyEnabled() {
		return
	}
	if err := notify.DeliverNow(service.StatusDigest(j.settingService.GetNotifyConfig().Lang)); err != nil {
		logger.Warning("notify: report failed: ", err)
	}
}
