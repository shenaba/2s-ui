package service

import (
	"net/netip"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"gorm.io/gorm"
)

// Which identity a LoginAttempt row counts. Both are tracked on every failure,
// so neither spraying usernames from one address nor spraying addresses at one
// username stays under a limit by spreading itself across the other axis.
const (
	loginScopeIP   = "ip"
	loginScopeUser = "user"
)

// How long a served username ban stays disarmed after the last attack. A
// username lockout can otherwise be renewed forever by five more distributed
// attempts as soon as it expires. IP rows keep rearming; a sustained attack
// therefore remains bounded per source without permanently locking the one
// panel administrator out. After this much quiet the username row rearms too.
const loginAttemptRetention = time.Hour

// loginGuardConfig is the three settings resolved into seconds.
type loginGuardConfig struct {
	maxFailures int
	window      int64
	ban         int64
}

// enabled is false if any knob is cleared. All three are required for the
// limiter to mean anything -- a zero window would expire every count instantly,
// a zero ban would refuse nobody -- so any of them at zero reads as "off"
// rather than as a degenerate limiter that silently lets everything through.
func (c loginGuardConfig) enabled() bool {
	return c.maxFailures > 0 && c.window > 0 && c.ban > 0
}

// loginBanRemaining reports how many seconds a row still refuses logins for.
func loginBanRemaining(row model.LoginAttempt, now int64) int64 {
	if row.BannedUntil <= now {
		return 0
	}
	return row.BannedUntil - now
}

// planLoginFailure decides what a row becomes after one more failed attempt.
// Split out from the DB access because this is the whole policy, and it is the
// half that can be tested on any machine -- the same reason acme_test.go is all
// pure functions.
func planLoginFailure(row model.LoginAttempt, now int64, cfg loginGuardConfig) model.LoginAttempt {
	// Already banned: leave the deadline alone. Extending it on every attempt
	// would let an attacker who keeps hammering hold a legitimate user out
	// indefinitely, which is a denial of service handed over for free.
	if loginBanRemaining(row, now) > 0 {
		return row
	}

	// A served username ban stays disarmed while failures keep arriving. There
	// is no way to hard-lock a public username repeatedly without also handing
	// an attacker a permanent account-lockout primitive, so the IP axis carries
	// the sustained protection after the first user ban. A successful login
	// deletes the row, and a quiet period rearms it for a later attack.
	if row.Scope == loginScopeUser && row.BannedUntil > 0 {
		if row.WindowStart > 0 && now-row.WindowStart < int64(loginAttemptRetention.Seconds()) {
			row.WindowStart = now
			return row
		}
		row.BannedUntil = 0
		row.Failures = 0
		row.WindowStart = 0
	}

	if row.WindowStart == 0 || now-row.WindowStart >= cfg.window {
		row.WindowStart = now
		row.Failures = 1
	} else {
		row.Failures++
	}

	if row.Failures >= cfg.maxFailures {
		row.BannedUntil = now + cfg.ban
		row.Failures = 0
		if row.Scope == loginScopeUser {
			// Keep the start of the served-ban retention period. Post-ban
			// failures move it forward while the attack remains active.
			row.WindowStart = now
		} else {
			row.WindowStart = 0
		}
	}
	return row
}

// LoginGuardService rate-limits password checks. It is deliberately not part of
// UserService: the counting has to happen even when there is no such user at
// all, which is exactly the case UserService answers with a nil user.
type LoginGuardService struct {
	SettingService SettingService
}

func (s *LoginGuardService) config() (loginGuardConfig, error) {
	maxFailures, windowMinutes, banMinutes, err := s.SettingService.GetLoginGuard()
	if err != nil {
		return loginGuardConfig{}, err
	}
	return loginGuardConfig{
		maxFailures: maxFailures,
		window:      int64(windowMinutes) * 60,
		ban:         int64(banMinutes) * 60,
	}, nil
}

// loginKeys is the pair of identities one attempt is counted against. An empty
// username is dropped rather than counted as its own identity: a form posted
// with no user would otherwise share one row and lock out the empty name for
// everybody.
func loginKeys(ip, username string) []model.LoginAttempt {
	keys := make([]model.LoginAttempt, 0, 2)
	if addr := normalizeLoginIP(ip); addr != "" {
		keys = append(keys, model.LoginAttempt{Scope: loginScopeIP, Key: addr})
	}
	if name := normalizeLoginName(username); name != "" {
		keys = append(keys, model.LoginAttempt{Scope: loginScopeUser, Key: name})
	}
	return keys
}

// normalizeLoginIP reduces a source address to the identity this limiter
// counts, on the same terms as the per-client IP limit (core's normalizeSrc).
// Unmap first, so one client arriving as 1.2.3.4 on one path and ::ffff:1.2.3.4
// on another is not two budgets; then mask v6 to /64, because SLAAC privacy
// extensions hand a single host a rotating set of addresses inside its prefix
// and counting those separately makes the IP axis unbounded per device.
//
// An unparseable value is kept verbatim rather than dropped: it can only reach
// here from an X-Forwarded-For that the operator told us to trust, and a
// mangled identity still limits something, where no identity limits nothing.
func normalizeLoginIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if !addr.Is6() {
		return addr.String()
	}
	prefix, err := addr.Prefix(core.IPv6IdentityPrefixBits)
	if err != nil {
		return addr.String()
	}
	return prefix.Addr().String()
}

// normalizeLoginName folds case so that admin/Admin/ADMIN share one count.
// Only the limiter does this -- authentication itself still matches the stored
// username exactly, and must, or folding here would widen who can log in.
func normalizeLoginName(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// BanRemaining reports how long this attempt is refused for, or zero to let it
// through. A DB error lets the attempt through: the limiter exists to slow
// guessing down, and failing closed would turn a broken query into a lockout of
// the panel's only administrator.
func (s *LoginGuardService) BanRemaining(ip, username string) time.Duration {
	cfg, err := s.config()
	if err != nil {
		logger.Warning("login guard: read settings:", err)
		return 0
	}
	if !cfg.enabled() {
		return 0
	}

	now := time.Now().Unix()
	longest := int64(0)
	for _, key := range loginKeys(ip, username) {
		row, err := s.find(key.Scope, key.Key)
		if err != nil {
			logger.Warning("login guard: read attempt:", err)
			continue
		}
		if remaining := loginBanRemaining(row, now); remaining > longest {
			longest = remaining
		}
	}
	return time.Duration(longest) * time.Second
}

// RecordFailure counts one failed attempt against both identities. Errors are
// logged and swallowed: the caller is already on its way to rejecting the
// login, and a bookkeeping failure should not turn that into a 500.
func (s *LoginGuardService) RecordFailure(ip, username string) {
	cfg, err := s.config()
	if err != nil {
		logger.Warning("login guard: read settings:", err)
		return
	}
	if !cfg.enabled() {
		return
	}

	now := time.Now().Unix()
	db := database.GetDB()
	for _, key := range loginKeys(ip, username) {
		// One transaction per identity so the read-modify-write cannot
		// interleave with a concurrent attempt on the same row and lose a
		// count. SQLite has a single writer, so these serialize anyway.
		err := db.Transaction(func(tx *gorm.DB) error {
			row := model.LoginAttempt{Scope: key.Scope, Key: key.Key}
			err := tx.Where("scope = ? AND key = ?", key.Scope, key.Key).First(&row).Error
			if err != nil && !database.IsNotFound(err) {
				return err
			}
			wasFree := loginBanRemaining(row, now) == 0
			row = planLoginFailure(row, now, cfg)
			row.Scope, row.Key = key.Scope, key.Key
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			// Log the transition only, not every attempt that lands while the
			// ban is already in force.
			if wasFree && row.BannedUntil > now {
				logger.Warningf("login guard: %s %q banned for %ds after %d failures",
					key.Scope, key.Key, cfg.ban, cfg.maxFailures)
			}
			return nil
		})
		if err != nil {
			logger.Warning("login guard: record failure:", err)
		}
	}

	s.sweep(now, cfg)
}

// RecordSuccess clears partial tallies and rearms a username row whose ban was
// already served. An active ban is not clearable this way because its request
// never reaches the password check.
func (s *LoginGuardService) RecordSuccess(ip, username string) {
	db := database.GetDB()
	for _, key := range loginKeys(ip, username) {
		err := db.Where("scope = ? AND key = ?", key.Scope, key.Key).
			Delete(&model.LoginAttempt{}).Error
		if err != nil {
			logger.Warning("login guard: clear attempts:", err)
		}
	}
}

// ClearAll removes every partial count and active ban. It is deliberately a
// CLI recovery operation rather than an unauthenticated endpoint: someone who
// can reach the database can already reset the admin credentials, while a web
// endpoint capable of clearing its own limiter would nullify the limiter.
func (s *LoginGuardService) ClearAll() error {
	return database.GetDB().Where("1 = 1").Delete(&model.LoginAttempt{}).Error
}

func (s *LoginGuardService) find(scope, key string) (model.LoginAttempt, error) {
	row := model.LoginAttempt{}
	err := database.GetDB().
		Where("scope = ? AND key = ?", scope, key).
		First(&row).Error
	if database.IsNotFound(err) {
		return model.LoginAttempt{Scope: scope, Key: key}, nil
	}
	return row, err
}

// sweep drops rows that no longer refuse anything, hold a count, or preserve a
// recently served username ban. Run from the failure path rather than a cron
// job: rows only ever appear there, so a panel nobody is attacking never runs
// this at all.
func (s *LoginGuardService) sweep(now int64, cfg loginGuardConfig) {
	cutoff := now - cfg.window - int64(loginAttemptRetention.Seconds())
	err := database.GetDB().
		Where("banned_until <= ? AND window_start <= ?", now, cutoff).
		Delete(&model.LoginAttempt{}).Error
	if err != nil {
		logger.Warning("login guard: sweep:", err)
	}
}
