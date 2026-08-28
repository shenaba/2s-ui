package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

var testGuard = loginGuardConfig{maxFailures: 5, window: 300, ban: 900}

// The shipped policy: five failures inside five minutes, then fifteen minutes
// of refusal.
func TestPlanLoginFailureBansOnTheFifth(t *testing.T) {
	row := model.LoginAttempt{}
	const start = 1_000_000

	for i := 1; i < testGuard.maxFailures; i++ {
		row = planLoginFailure(row, start+int64(i), testGuard)
		if row.BannedUntil != 0 {
			t.Fatalf("banned after %d failures, want no ban before %d", i, testGuard.maxFailures)
		}
		if row.Failures != i {
			t.Fatalf("after %d failures the count reads %d", i, row.Failures)
		}
	}

	last := int64(start + testGuard.maxFailures)
	row = planLoginFailure(row, last, testGuard)
	want := last + testGuard.ban
	if row.BannedUntil != want {
		t.Errorf("BannedUntil = %d, want %d", row.BannedUntil, want)
	}
}

// Failures spread wider than the window never accumulate: each one opens a
// fresh window instead of adding to a stale tally.
func TestPlanLoginFailureForgetsOutsideTheWindow(t *testing.T) {
	row := model.LoginAttempt{}
	at := int64(1_000_000)

	for i := 0; i < testGuard.maxFailures*2; i++ {
		row = planLoginFailure(row, at, testGuard)
		if row.BannedUntil != 0 {
			t.Fatalf("attempt %d at t=%d produced a ban", i, at)
		}
		if row.Failures != 1 {
			t.Fatalf("attempt %d left the count at %d, want a fresh window", i, row.Failures)
		}
		at += testGuard.window
	}
}

// Hammering during a ban must not push its end further out, or an attacker
// could hold the panel's only administrator out for as long as they cared to
// keep trying.
func TestPlanLoginFailureDoesNotExtendAnActiveBan(t *testing.T) {
	row := model.LoginAttempt{BannedUntil: 2_000_000}

	for _, now := range []int64{1_999_000, 1_999_500, 1_999_999} {
		row = planLoginFailure(row, now, testGuard)
		if row.BannedUntil != 2_000_000 {
			t.Fatalf("ban moved to %d while still in force", row.BannedUntil)
		}
	}
}

// Once served, the ban is gone and counting starts over -- a single failure
// after it lifts must not re-ban on the strength of the old tally.
func TestPlanLoginFailureCountsAfreshOnceTheBanIsServed(t *testing.T) {
	row := planLoginFailure(model.LoginAttempt{Scope: loginScopeIP, BannedUntil: 2_000_000}, 2_000_001, testGuard)

	if row.BannedUntil > 2_000_001 {
		t.Fatalf("re-banned immediately after the ban lifted: until %d", row.BannedUntil)
	}
	if row.Failures != 1 {
		t.Errorf("Failures = %d after the first post-ban failure, want 1", row.Failures)
	}
}

// A public username must not be a permanent lockout switch. Once its first ban
// has been served, a continued distributed spray is left to the per-IP rows;
// the username row stays disarmed until either a login succeeds or the attack
// goes quiet long enough to treat a later burst as a new incident.
func TestPlanLoginFailureDoesNotRenewServedUsernameBan(t *testing.T) {
	row := model.LoginAttempt{
		Scope:       loginScopeUser,
		BannedUntil: 2_000_000,
		WindowStart: 2_000_000 - testGuard.ban,
	}

	for now := int64(2_000_001); now < 2_001_000; now++ {
		row = planLoginFailure(row, now, testGuard)
		if row.BannedUntil > now {
			t.Fatalf("username re-banned at %d until %d", now, row.BannedUntil)
		}
		if row.Failures != 0 {
			t.Fatalf("served username ban resumed counting at %d: %d", now, row.Failures)
		}
	}
}

func TestPlanLoginFailureRearmsUsernameAfterQuietPeriod(t *testing.T) {
	row := model.LoginAttempt{
		Scope:       loginScopeUser,
		BannedUntil: 2_000_000,
		WindowStart: 2_000_000,
	}
	now := row.WindowStart + int64(loginAttemptRetention.Seconds())

	row = planLoginFailure(row, now, testGuard)
	if row.BannedUntil != 0 {
		t.Fatalf("served ban was not cleared after quiet period: %d", row.BannedUntil)
	}
	if row.Failures != 1 || row.WindowStart != now {
		t.Errorf("username did not rearm as a fresh window: %+v", row)
	}
}

func TestLoginBanRemaining(t *testing.T) {
	cases := []struct {
		name string
		row  model.LoginAttempt
		now  int64
		want int64
	}{
		{"never banned", model.LoginAttempt{}, 1_000_000, 0},
		{"ban in force", model.LoginAttempt{BannedUntil: 1_000_060}, 1_000_000, 60},
		{"ban expiring now", model.LoginAttempt{BannedUntil: 1_000_000}, 1_000_000, 0},
		{"ban long served", model.LoginAttempt{BannedUntil: 999_000}, 1_000_000, 0},
	}
	for _, c := range cases {
		if got := loginBanRemaining(c.row, c.now); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// Any knob at zero turns the limiter off rather than leaving a degenerate one
// that refuses nobody or expires every count instantly.
func TestLoginGuardConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  loginGuardConfig
		want bool
	}{
		{"shipped defaults", testGuard, true},
		{"no failure budget", loginGuardConfig{0, 300, 900}, false},
		{"no window", loginGuardConfig{5, 0, 900}, false},
		{"no ban", loginGuardConfig{5, 300, 0}, false},
		{"negative budget", loginGuardConfig{-1, 300, 900}, false},
	}
	for _, c := range cases {
		if got := c.cfg.enabled(); got != c.want {
			t.Errorf("%s: enabled() = %v, want %v", c.name, got, c.want)
		}
	}
}

// Both identities are counted, and the username is folded so that alternating
// admin/Admin does not buy an attacker two separate budgets. An empty username
// contributes no row at all.
func TestLoginKeys(t *testing.T) {
	keys := loginKeys("10.0.0.1", "  Admin ")
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2: %+v", len(keys), keys)
	}
	if keys[0].Scope != loginScopeIP || keys[0].Key != "10.0.0.1" {
		t.Errorf("unexpected ip key: %+v", keys[0])
	}
	if keys[1].Scope != loginScopeUser || keys[1].Key != "admin" {
		t.Errorf("unexpected user key: %+v", keys[1])
	}

	if got := loginKeys("10.0.0.1", "   "); len(got) != 1 || got[0].Scope != loginScopeIP {
		t.Errorf("a blank username produced %+v, want the ip key alone", got)
	}
	if got := loginKeys("", "admin"); len(got) != 1 || got[0].Scope != loginScopeUser {
		t.Errorf("a blank ip produced %+v, want the user key alone", got)
	}
}

// The IP axis has to fold an address to the same identity the per-client IP
// limit uses, or a host with one IPv6 prefix spends a fresh failure budget per
// address it rotates through.
func TestNormalizeLoginIP(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"v4 is untouched", "203.0.113.7", "203.0.113.7"},
		{"v4-mapped folds onto the v4 identity", "::ffff:203.0.113.7", "203.0.113.7"},
		{"v6 masks to its /64", "2001:db8:1:2:3:4:5:6", "2001:db8:1:2::"},
		{"another address in that /64 folds onto it", "2001:db8:1:2:dead:beef:0:1", "2001:db8:1:2::"},
		{"a different /64 stays distinct", "2001:db8:1:3::1", "2001:db8:1:3::"},
		{"loopback v6", "::1", "::"},
		{"garbage is kept verbatim", "not-an-address", "not-an-address"},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		if got := normalizeLoginIP(c.in); got != c.want {
			t.Errorf("%s: normalizeLoginIP(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}

	// The point of the fold, stated as the property that matters: two addresses
	// a rotating host would use land on one key.
	a := loginKeys("2001:db8:aa:bb:1::1", "admin")
	b := loginKeys("2001:db8:aa:bb:9999::7", "admin")
	if a[0].Key != b[0].Key {
		t.Errorf("two addresses in one /64 produced different keys: %q vs %q", a[0].Key, b[0].Key)
	}
}

// ActiveBans is what the bot shows an operator during an attack, and it has one
// trap: BannedUntil stays positive after a username ban is served, so reading
// the column alone reports bans that are over as if they were live.
func TestActiveBans(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "bans.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	db := database.GetDB()

	now := time.Now().Unix()
	rows := []model.LoginAttempt{
		{Scope: loginScopeIP, Key: "1.2.3.4", Failures: 5, BannedUntil: now + 900},
		{Scope: loginScopeUser, Key: "admin", Failures: 5, BannedUntil: now + 300},
		{Scope: loginScopePrompt, Key: "5.6.7.8", Failures: 50, BannedUntil: now + 60},
		// Served: the row is kept so the username axis can rearm, and its
		// BannedUntil keeps the old, now-past value.
		{Scope: loginScopeUser, Key: "served", BannedUntil: now - 1},
		// Counting up, not yet banned.
		{Scope: loginScopeIP, Key: "9.9.9.9", Failures: 3, WindowStart: now},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed %s/%s: %v", row.Scope, row.Key, err)
		}
	}

	var guard LoginGuardService
	got, err := guard.ActiveBans(10)
	if err != nil {
		t.Fatalf("ActiveBans: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d bans, want 3: %+v", len(got), got)
	}
	// Longest first, so the worst offender is at the top of the message.
	if got[0].Key != "1.2.3.4" || got[2].Key != "5.6.7.8" {
		t.Errorf("bans are not ordered longest first: %+v", got)
	}
	if got[0].Remaining < 14*time.Minute || got[0].Remaining > 15*time.Minute {
		t.Errorf("remaining time is %v, want just under 15m", got[0].Remaining)
	}
	if got[0].Failures != 5 {
		t.Errorf("failure count lost: %d", got[0].Failures)
	}
	for _, ban := range got {
		if ban.Key == "served" {
			t.Error("a served ban is reported as active")
		}
		if ban.Key == "9.9.9.9" {
			t.Error("a client that is only counting up is reported as banned")
		}
	}

	// The limit is a limit, not a suggestion: the bot renders one message.
	limited, err := guard.ActiveBans(1)
	if err != nil {
		t.Fatalf("ActiveBans(1): %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("ActiveBans(1) returned %d rows", len(limited))
	}
}
