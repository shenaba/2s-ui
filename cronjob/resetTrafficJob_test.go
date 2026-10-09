package cronjob

import (
	"testing"
	"time"
)

func TestPlanGlobalReset(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	const daily = "@daily|UTC"
	for _, tc := range []struct {
		name     string
		next     int64
		armedFor string
		want     globalResetStep
	}{
		{"nothing armed yet", 0, "", globalResetArm},
		// A panel upgraded from a version that did not record the schedule:
		// re-arm, so the upgrade itself never resets anyone.
		{"armed before the schedule was recorded", now.Add(-time.Hour).Unix(), "", globalResetArm},
		// The bug the schedule is recorded for: a monthly boundary left
		// standing after a switch to daily did nothing for up to a month.
		{"armed under another spec", now.Add(20 * 24 * time.Hour).Unix(), "@monthly|UTC", globalResetArm},
		{"armed under another zone", now.Add(time.Hour).Unix(), "@daily|Asia/Shanghai", globalResetArm},
		{"boundary ahead", now.Add(time.Minute).Unix(), daily, globalResetWait},
		{"boundary reached", now.Unix(), daily, globalResetRun},
		// Missed while the panel was down: caught up once at the first tick.
		{"boundary long past", now.Add(-72 * time.Hour).Unix(), daily, globalResetRun},
	} {
		if got := planGlobalReset(tc.next, tc.armedFor, daily, now); got != tc.want {
			t.Errorf("%s: want %d, got %d", tc.name, tc.want, got)
		}
	}
}
