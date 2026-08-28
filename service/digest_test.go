package service

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service/notify"

	"github.com/op/go-logging"
)

// UsageRatio feeds both the memory threshold alert and the periodic report. Its
// failure mode is quiet: gopsutil returns an empty map when the syscall fails,
// and a zero total divides into a NaN that compares false against every
// threshold -- silently disabling the alert -- and formats as "NaN%" in the
// report.
func TestUsageRatio(t *testing.T) {
	cases := []struct {
		name  string
		in    any
		want  float64
		valid bool
	}{
		{"half used", map[string]interface{}{"current": uint64(512), "total": uint64(1024)}, 50, true},
		{"full", map[string]interface{}{"current": uint64(1024), "total": uint64(1024)}, 100, true},
		{"gopsutil failed", map[string]interface{}{}, 0, false},
		{"zero total", map[string]interface{}{"current": uint64(1), "total": uint64(0)}, 0, false},
		{"missing total", map[string]interface{}{"current": uint64(1)}, 0, false},
		{"wrong numeric type", map[string]interface{}{"current": 512, "total": 1024}, 0, false},
		{"not a map", "nope", 0, false},
		{"nil", nil, 0, false},
	}
	for _, c := range cases {
		got, ok := UsageRatio(c.in)
		if ok != c.valid {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.valid)
			continue
		}
		if ok && got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
		if !ok && math.IsNaN(got) {
			t.Errorf("%s: returned NaN, which would reach the report as \"NaN%%\"", c.name)
		}
	}

	// The report formats with %.0f; make sure a rejected value cannot slip
	// through as a plausible-looking zero percent.
	if _, ok := UsageRatio(map[string]interface{}{}); ok {
		t.Fatal("an empty reading was accepted")
	}
	if s := fmt.Sprintf("%.0f", math.NaN()); s != "NaN" {
		t.Skip("NaN no longer formats as NaN; the guard above is moot")
	}
}

// ClientAlertDigest carries the only hand-written SQL in the report path, and
// its predicate has to agree with the one DepleteClients disables on -- a
// disabled client that is not over any limit was switched off by the operator
// and does not belong on a list of accounts that ran out.
func TestClientAlertDigest(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "digest.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()

	for key, value := range map[string]string{
		"notifyExpireDays": "3",
		"notifyVolumeGB":   "5",
	} {
		if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatalf("seed setting %s: %v", key, err)
		}
	}

	const gib = int64(1) << 30
	now := time.Now().Unix()
	day := int64(86400)

	seed := func(c model.Client) {
		t.Helper()
		c.Inbounds = json.RawMessage(`[]`)
		c.Links = json.RawMessage(`[]`)
		c.Config = json.RawMessage(`{}`)
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}

	seed(model.Client{Name: "out-of-traffic", Enable: false, Volume: 10 * gib, Up: 11 * gib})
	seed(model.Client{Name: "out-of-time", Enable: false, Expiry: now - day})
	seed(model.Client{Name: "expires-tomorrow", Enable: true, Expiry: now + day})
	// Switched off by hand, not by running out: not a depleted account.
	seed(model.Client{Name: "paused-by-operator", Enable: false})
	seed(model.Client{Name: "healthy", Enable: true})

	got := ClientAlertDigest("en")

	for _, name := range []string{"out-of-traffic", "out-of-time", "expires-tomorrow"} {
		if !strings.Contains(got, name) {
			t.Errorf("%s is missing from the digest:\n%s", name, got)
		}
	}
	for _, name := range []string{"paused-by-operator", "healthy"} {
		if strings.Contains(got, name) {
			t.Errorf("%s should not be in the digest:\n%s", name, got)
		}
	}
	// Both headings, so a reader can tell "already cut off" from "about to be".
	for _, key := range []string{"digest.depleted", "digest.expiring"} {
		if label := notify.Label("en", key); !strings.Contains(got, label) {
			t.Errorf("the %q heading is missing:\n%s", label, got)
		}
	}
}

// Nothing to report is reported as nothing, so the scheduled digest does not
// grow two empty headings every day on a healthy panel.
func TestClientAlertDigestIsEmptyWhenNothingIsWrong(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "quiet.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	if got := ClientAlertDigest("en"); got != "" {
		t.Errorf("a healthy panel produced a digest section:\n%q", got)
	}
}
