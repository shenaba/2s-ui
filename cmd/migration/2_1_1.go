package migration

import (
	"strings"

	"gorm.io/gorm"
)

// certFailedKind is notify.CertRenewFailed. Spelled out rather than imported:
// a migration records what the value was when it ran, and importing service
// code into here would drag the whole service graph into `sui migrate`.
const certFailedKind = "cert.failed"

// to2_1_1 switches the certificate renewal alert on for installs that already
// stored their notifyEvents.
//
// The panel renews acme.sh certificates itself from 2.1.1 on, and a failed
// renewal publishes cert.failed. A kind added to the notifyEvents default only
// reaches installs that never saved that setting -- and the installs this alert
// exists for are precisely the old Docker deployments, every one of which has
// its list stored already. Left to the default, the alert would be on for no
// one who needed it. The operator never saw this kind, so appending it overrides
// no choice they made; turning it off afterwards sticks, since this runs once.
//
// An empty list is left alone: that is an operator who switched every event
// off, and a new one appearing in it would be exactly the override above.
// Idempotent: a list that already names the kind is left untouched.
func to2_1_1(tx *gorm.DB) error {
	type settingRow struct {
		Id    uint
		Value string
	}
	var rows []settingRow
	if err := tx.Raw("SELECT id, value FROM settings WHERE key = ?", "notifyEvents").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		value, changed := appendNotifyKind(row.Value, certFailedKind)
		if !changed {
			continue
		}
		if err := tx.Exec("UPDATE settings SET value = ? WHERE id = ?", value, row.Id).Error; err != nil {
			return err
		}
	}
	return nil
}

// appendNotifyKind adds kind to a stored comma-separated notifyEvents list,
// reporting whether it changed anything. Empty lists and lists that already
// hold kind are returned as they are.
func appendNotifyKind(list, kind string) (string, bool) {
	var kinds []string
	for _, part := range strings.Split(list, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if p == kind {
			return list, false
		}
		kinds = append(kinds, p)
	}
	if len(kinds) == 0 {
		return list, false
	}
	return strings.Join(append(kinds, kind), ","), true
}
