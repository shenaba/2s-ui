package service

// Remote Android package lists for the JSON subscription (#326).
//
// The operator lists HTTPS URLs; a background refresh downloads each one and
// stores the IDs per URL in a protected setting. The subscription generator
// only reads that setting -- it never fetches -- and merges the IDs into the
// TUN inbound's exclude_package (sub.mergeExcludePkgs). Nothing here touches
// the core or ConfigService.Save's locking: the cache is written by the one
// refresh worker alone, outside any save.
//
// A failed or empty download keeps the last good IDs for that URL, so a list
// host being down never silently puts its apps back into the VPN. Removing a
// URL drops its IDs at once: reads only consider URLs that are still
// configured, and the next refresh prunes the rest from the cache.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"
)

const (
	excludePkgURLsKey     = "subJsonExcludePkgUrls"
	excludePkgIntervalKey = "subJsonExcludePkgInterval"
	excludePkgCacheKey    = "subJsonExcludePkgCache"

	excludePkgMaxInterval = 365 // days
)

// parseExcludePkgURLs reads the stored setting: one URL per line. Blank lines
// and repeats are dropped; anything that fails validatePkgListURL is an error.
func parseExcludePkgURLs(raw string) ([]string, error) {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || containsStr(out, s) {
			continue
		}
		if len(s) > pkgListMaxURLLen {
			return nil, fmt.Errorf("package list URL is longer than %d characters", pkgListMaxURLLen)
		}
		u, err := url.Parse(s)
		if err == nil {
			err = validatePkgListURL(u)
		}
		if err != nil {
			return nil, fmt.Errorf("package list URL %q: %v", s, err)
		}
		out = append(out, s)
	}
	if len(out) > pkgListMaxSources {
		return nil, fmt.Errorf("at most %d package list URLs", pkgListMaxSources)
	}
	return out, nil
}

func parseExcludePkgInterval(raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 || n > excludePkgMaxInterval {
		return 0, fmt.Errorf("package list update interval must be 0 to %d days", excludePkgMaxInterval)
	}
	return n, nil
}

func containsStr(list []string, s string) bool { return slices.Contains(list, s) }

// pkgListSource is what is known about one URL. The time of the last attempt
// is kept apart from the last success, so a broken URL is retried on the
// interval rather than on every refresh.
type pkgListSource struct {
	IDs   []string `json:"ids,omitempty"`
	OK    int64    `json:"ok,omitempty"`
	Tried int64    `json:"tried,omitempty"`
	Err   string   `json:"err,omitempty"`
}

type pkgListCache map[string]pkgListSource

func decodePkgListCache(raw string) pkgListCache {
	c := pkgListCache{}
	if raw != "" && json.Unmarshal([]byte(raw), &c) != nil {
		return pkgListCache{}
	}
	if c == nil {
		c = pkgListCache{}
	}
	return c
}

// record stores one download. A failure keeps the IDs from the last success.
func (c pkgListCache) record(u string, ids []string, err error, at time.Time) {
	s := c[u]
	s.Tried = at.Unix()
	if err != nil {
		msg := err.Error()
		if len(msg) > 200 {
			// On a rune boundary, or the stored message ends in U+FFFD.
			n := 200
			for n > 0 && !utf8.RuneStart(msg[n]) {
				n--
			}
			msg = msg[:n]
		}
		s.Err = msg
	} else {
		s.IDs, s.OK, s.Err = ids, at.Unix(), ""
	}
	c[u] = s
}

// prune drops URLs that are no longer configured.
func (c pkgListCache) prune(urls []string) {
	for u := range c {
		if !containsStr(urls, u) {
			delete(c, u)
		}
	}
}

// due reports whether a URL should be downloaded now: never tried, forced, or
// its interval has passed since the last attempt. Interval 0 means only a
// manual refresh (or a newly added URL) downloads.
func pkgListDue(s pkgListSource, intervalDays int, now time.Time, force bool) bool {
	if force || s.Tried == 0 {
		return true
	}
	return intervalDays > 0 && now.Unix()-s.Tried >= int64(intervalDays)*86400
}

// mergedPkgIDs is every ID from the configured URLs, unique and sorted.
func mergedPkgIDs(urls []string, c pkgListCache) []string {
	var all []string
	for _, u := range urls {
		all = append(all, c[u].IDs...)
	}
	return normalizePkgIDs(all)
}

// readSettings reads several keys in one query, falling back to
// defaultValueMap for missing rows.
func readSettings(keys ...string) (map[string]string, error) {
	var rows []model.Setting
	if err := database.GetDB().Model(model.Setting{}).Where("key in ?", keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = defaultValueMap[k]
	}
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}

// GetExcludePkgsForSub returns the remote package IDs a JSON subscription
// should exclude, or nil when no list is configured. A stored URL setting that
// no longer parses yields nil rather than failing the subscription.
func (s *SettingService) GetExcludePkgsForSub() ([]string, error) {
	m, err := readSettings(excludePkgURLsKey, excludePkgCacheKey)
	if err != nil {
		return nil, err
	}
	urls, err := parseExcludePkgURLs(m[excludePkgURLsKey])
	if err != nil || len(urls) == 0 {
		return nil, nil
	}
	return mergedPkgIDs(urls, decodePkgListCache(m[excludePkgCacheKey])), nil
}

// ---- refresh -------------------------------------------------------------

// One worker at a time. A request while it runs is folded into one follow-up
// pass, so a URL added mid-refresh is still fetched without waiting an hour.
var pkgRefresh struct {
	sync.Mutex
	running, pending, pendingForce bool
}

// RequestExcludePkgRefresh starts a refresh in the background and returns at
// once. force downloads every URL; otherwise only the ones that are due.
func RequestExcludePkgRefresh(force bool) {
	pkgRefresh.Lock()
	if pkgRefresh.running {
		pkgRefresh.pending = true
		pkgRefresh.pendingForce = pkgRefresh.pendingForce || force
		pkgRefresh.Unlock()
		return
	}
	pkgRefresh.running = true
	pkgRefresh.Unlock()
	go func() {
		for {
			if err := runPkgRefresh(force, fetchPkgList); err != nil {
				logger.Warning("package list refresh: ", err)
			}
			pkgRefresh.Lock()
			if !pkgRefresh.pending {
				pkgRefresh.running = false
				pkgRefresh.Unlock()
				return
			}
			force = pkgRefresh.pendingForce
			pkgRefresh.pending, pkgRefresh.pendingForce = false, false
			pkgRefresh.Unlock()
		}
	}()
}

func excludePkgRefreshing() bool {
	pkgRefresh.Lock()
	defer pkgRefresh.Unlock()
	return pkgRefresh.running
}

// runPkgRefresh downloads what is due and writes the cache. The settings are
// read again after the downloads, so a URL removed meanwhile is not written
// back. It recovers its own panics: it runs on a bare goroutine, where one
// would take the whole panel down.
func runPkgRefresh(force bool, fetch func(context.Context, string) ([]string, error)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	m, err := readSettings(excludePkgURLsKey, excludePkgIntervalKey, excludePkgCacheKey)
	if err != nil {
		return err
	}
	urls, err := parseExcludePkgURLs(m[excludePkgURLsKey])
	if err != nil {
		return err
	}
	interval, err := parseExcludePkgInterval(m[excludePkgIntervalKey])
	if err != nil {
		interval = 0
	}
	cache := decodePkgListCache(m[excludePkgCacheKey])
	now := time.Now()
	var due []string
	for _, u := range urls {
		if pkgListDue(cache[u], interval, now, force) {
			due = append(due, u)
		}
	}
	stale := false
	for u := range cache {
		stale = stale || !containsStr(urls, u)
	}
	if len(due) == 0 && !stale {
		return nil
	}

	type result struct {
		ids []string
		err error
		at  time.Time
	}
	results := make([]result, len(due))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, u := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[i] = result{err: errors.New("fetch panicked"), at: time.Now()}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), pkgListTimeout)
			defer cancel()
			ids, err := fetch(ctx, u)
			results[i] = result{ids: ids, err: err, at: time.Now()}
		}()
	}
	wg.Wait()

	// Re-read: the URLs may have changed while the downloads ran.
	m, err = readSettings(excludePkgURLsKey, excludePkgCacheKey)
	if err != nil {
		return err
	}
	if urls, err = parseExcludePkgURLs(m[excludePkgURLsKey]); err != nil {
		return err
	}
	cache = decodePkgListCache(m[excludePkgCacheKey])
	cache.prune(urls)
	for i, u := range due {
		if containsStr(urls, u) {
			cache.record(u, results[i].ids, results[i].err, results[i].at)
		}
	}
	b, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	var ss SettingService
	return ss.saveSetting(excludePkgCacheKey, string(b))
}

// ---- status for the settings page ------------------------------------------

type ExcludePkgSourceStatus struct {
	URL   string `json:"url"`
	Count int    `json:"count"`
	OK    int64  `json:"ok"`    // last successful download, unix seconds
	Tried int64  `json:"tried"` // last attempt
	Err   string `json:"err,omitempty"`
}

type ExcludePkgStatus struct {
	Sources    []ExcludePkgSourceStatus `json:"sources"`
	Total      int                      `json:"total"`
	Refreshing bool                     `json:"refreshing"`
}

// GetExcludePkgStatus describes the saved URLs in their saved order.
func GetExcludePkgStatus() (*ExcludePkgStatus, error) {
	m, err := readSettings(excludePkgURLsKey, excludePkgCacheKey)
	if err != nil {
		return nil, err
	}
	st := &ExcludePkgStatus{Sources: []ExcludePkgSourceStatus{}, Refreshing: excludePkgRefreshing()}
	urls, err := parseExcludePkgURLs(m[excludePkgURLsKey])
	if err != nil {
		return st, nil
	}
	cache := decodePkgListCache(m[excludePkgCacheKey])
	for _, u := range urls {
		s := cache[u]
		st.Sources = append(st.Sources, ExcludePkgSourceStatus{URL: u, Count: len(s.IDs), OK: s.OK, Tried: s.Tried, Err: s.Err})
	}
	st.Total = len(mergedPkgIDs(urls, cache))
	return st, nil
}
