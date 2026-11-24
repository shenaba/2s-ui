package service

// v4: два ключа в таблице settings (androidExcludeOptions, androidExcludeCache),
// один воркер Refresh() под mutex, одна функция слияния ae.Apply().
// Готовый шаблон держится только в памяти и пересчитывается, когда данные
// изменились (счётчик androidGen). После перезапуска собирается заново.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	ae "github.com/shenaba/2s-ui/internal/androidexclusions"
	"github.com/shenaba/2s-ui/logger"
	"gorm.io/gorm"
)

const (
	androidOptionsKey  = "androidExcludeOptions"
	androidCacheKey    = "androidExcludeCache"
	androidMetaKey     = "androidExcludeMeta"
	androidPreparedKey = "androidExcludePrepared"
	androidRestartKey  = "androidExcludeRestartPending"
)

const androidDefaultOptions = `{"enabled":false,"manual":"","sources":[],"interval":0}`

// Создаёт строку, если её ещё нет (у обновлённой с 2.1.0 панели её нет), иначе
// обновляет. Общий для сохранения настроек и кэша.
func androidUpsert(tx *gorm.DB, key, value string) error {
	return tx.Where("key = ?", key).
		Assign(map[string]interface{}{"value": value}).
		FirstOrCreate(&model.Setting{Key: key}).Error
}

// Для Save(): проверить и привести options к каноническому виду до записи.
func validateAndroidOptions(raw string) (string, error) {
	o, err := ae.DecodeOptions(raw)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(o)
	return string(b), err
}

// ---- чтение/запись двух ключей -------------------------------------------

func androidLoad(db *gorm.DB, extra ...string) (map[string]string, ae.Options, ae.Cache, error) {
	var rows []model.Setting
	keys := append([]string{androidOptionsKey, androidCacheKey, androidMetaKey}, extra...)
	if err := db.Where("key in ?", keys).Find(&rows).Error; err != nil {
		return nil, ae.Options{}, nil, err
	}
	m := map[string]string{}
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	o, err := ae.DecodeOptions(m[androidOptionsKey])
	cache := ae.DecodeCache(m[androidCacheKey], m[androidMetaKey])
	cache.Prune(o)
	return m, o, cache, err
}

func androidSaveCache(tx *gorm.DB, c ae.Cache) error {
	b, err := json.Marshal(c)
	if len(b) > ae.MaxCacheBytes {
		return ae.ErrLimit
	}
	if err != nil {
		return err
	}
	return androidUpsert(tx, androidCacheKey, string(b))
}

// Only feature-owned settings are changed. Called inside the settings transaction.
func saveAndroidOptions(tx *gorm.DB, raw string) error {
	canonical, err := validateAndroidOptions(raw)
	if err != nil {
		return err
	}
	o, _ := ae.DecodeOptions(canonical)
	_, _, cache, err := androidLoad(tx)
	if err != nil && cache == nil {
		return err
	}
	if cache == nil {
		cache = ae.Cache{}
	}
	cache.Prune(o)
	if err = ae.CheckLimits(o, cache); err != nil {
		return err
	}
	if err = androidUpsert(tx, androidOptionsKey, canonical); err != nil {
		return err
	}
	return androidSaveCache(tx, cache)
}

// Migrate old cache and remove obsolete bookkeeping, without touching templates.
// Legacy rows stay protected even if this transaction fails.
func MigrateAndroidExclusions() error {
	androidDataMu.Lock()
	defer androidDataMu.Unlock()
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		m, o, c, err := androidLoad(tx, androidPreparedKey, androidRestartKey)
		if err != nil {
			return err
		}
		var current ae.Cache
		_, meta := m[androidMetaKey]
		_, prepared := m[androidPreparedKey]
		_, restart := m[androidRestartKey]
		if !meta && !prepared && !restart && (m[androidCacheKey] == "" || json.Unmarshal([]byte(m[androidCacheKey]), &current) == nil) {
			return nil
		}
		c.Prune(o)
		if m[androidRestartKey] != "" {
			for u, v := range c {
				v.Tried = 0
				c[u] = v
			}
		}
		if err = ae.CheckLimits(o, c); err != nil {
			return err
		}
		if err = androidSaveCache(tx, c); err != nil {
			return err
		}
		return tx.Where("key IN ?", []string{androidMetaKey, androidPreparedKey, androidRestartKey}).Delete(&model.Setting{}).Error
	})
	if err == nil {
		InvalidateAndroidExclusions()
	}
	return err
}

// Serialize feature-affecting writes, but never network downloads.
var androidDataMu sync.Mutex
var androidSourceGen uint64 // guarded by androidDataMu

// Import may swap the database while a download is in flight.
func ImportWithAndroidExclusions(importDB func() error) error {
	androidDataMu.Lock()
	InvalidateAndroidExclusions()
	err := importDB()
	androidSourceGen++
	InvalidateAndroidExclusions()
	androidDataMu.Unlock()
	if err == nil {
		if e := MigrateAndroidExclusions(); e != nil {
			logger.Warning("Android exclusions migration failed")
		}
		RequestAndroidExclusionRefresh(false)
	}
	return err
}

// Persist a pending manual restart by resetting attempt timestamps in the
// existing cache. Last good IDs and success times are not changed.
func MarkAndroidExclusionsRestart() error {
	androidDataMu.Lock()
	defer androidDataMu.Unlock()
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		_, o, c, err := androidLoad(tx)
		if err != nil || !o.Enabled || len(o.Sources) == 0 {
			return err
		}
		for u, v := range c {
			v.Tried = 0
			c[u] = v
		}
		return androidSaveCache(tx, c)
	})
	if err == nil {
		androidSourceGen++
		InvalidateAndroidExclusions()
	}
	return err
}

// ---- готовый результат в памяти ------------------------------------------

type androidReady struct {
	db        *gorm.DB
	gen       uint64
	tpl       string
	st        ae.Stats
	err       error
	cacheable bool
}

var (
	androidGen atomic.Uint64
	androidCur atomic.Pointer[androidReady]
)

func InvalidateAndroidExclusions() { androidGen.Add(1) }

func (s *SettingService) EffectiveSubJsonExt() (string, error) {
	androidDataMu.Lock()
	defer androidDataMu.Unlock()
	for {
		g := androidGen.Load()
		db := database.GetDB()
		if r := androidCur.Load(); r != nil && r.gen == g && r.db == db {
			return r.tpl, r.err
		}
		r := buildAndroidReady(g, db)
		if androidGen.Load() != g {
			continue
		}
		if r.cacheable && r.err == nil {
			androidCur.Store(r)
		}
		return r.tpl, r.err
	}
}

func buildAndroidReady(g uint64, db *gorm.DB) *androidReady {
	r := &androidReady{gen: g, db: db}
	m, o, cache, err := androidLoad(db, "subJsonExt", "config")
	if m == nil {
		r.err = err
		return r
	}
	r.cacheable = true
	raw := m["subJsonExt"]
	if err != nil { // сломанные options: не отдаём ручные ID без проверки
		r.tpl, r.err = ae.Strip(raw)
		r.st.Warning = "options"
		return r
	}
	srv := m["config"]
	if srv == "" {
		srv = defaultConfig
	}
	r.tpl, r.st, r.err = ae.Apply(raw, srv, o, cache)
	if r.err != nil {
		r.tpl, r.err = ae.Strip(raw)
		if r.st.Warning == "" {
			r.st = ae.Stats{Warning: "rules"}
		}
	}
	return r
}

// ---- загрузка списков ----------------------------------------------------

// At most one worker and one coalesced follow-up. Repeated force requests
// during a forced pass are absorbed; settings changes always request a rescan.
var (
	androidWorkMu                                            sync.Mutex
	androidBusy                                              atomic.Bool
	androidRefreshFailed                                     atomic.Bool
	androidPending, androidPendingForce, androidRunningForce bool
)

func RequestAndroidExclusionRefresh(force bool) {
	requestAndroidRefresh(force, false)
}

// A settings save supersedes an in-flight snapshot; preserve its force intent.
func requestAndroidAfterSettingsSave() { requestAndroidRefresh(false, true) }

func requestAndroidRefresh(force, settingsChanged bool) {
	androidWorkMu.Lock()
	if androidBusy.Load() {
		if !force || !androidRunningForce {
			androidPending = true
			androidPendingForce = androidPendingForce || force || (settingsChanged && androidRunningForce)
		}
		androidWorkMu.Unlock()
		return
	}
	androidBusy.Store(true)
	androidRunningForce = force
	androidWorkMu.Unlock()
	go func() {
		for {
			err := refreshAndroidExclusions(force, ae.Fetch)
			androidRefreshFailed.Store(err != nil)
			if err != nil {
				logger.Warning("Android exclusion refresh failed")
			}
			androidWorkMu.Lock()
			if !androidPending {
				androidRunningForce = false
				androidBusy.Store(false)
				androidWorkMu.Unlock()
				return
			}
			force = androidPendingForce
			androidPending = false
			androidPendingForce = false
			androidRunningForce = force
			androidWorkMu.Unlock()
		}
	}()
}

type androidResult struct {
	url string
	ids []string
	err error
	at  time.Time
}

// Refresh: качает только то, что пора (по последней попытке) или force.
// Сеть — вне транзакции; в конце свежие options перечитываются, так что
// правки/удаления источников за время загрузки побеждают.
func refreshAndroidExclusions(force bool, fetch func(context.Context, string) ([]string, error)) error {
	androidDataMu.Lock()
	gen := androidSourceGen
	_, o, cache, err := androidLoad(database.GetDB())
	androidDataMu.Unlock()
	if err != nil || !o.Enabled || len(o.Sources) == 0 {
		return err
	}
	var due []string
	now := time.Now()
	for _, u := range o.Sources {
		if ae.Due(o, cache[u], now, force) {
			due = append(due, u)
		}
	}
	if len(due) == 0 {
		return nil
	}
	results := make([]androidResult, len(due))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, u := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			r := androidResult{url: u}
			defer func() {
				if recover() != nil {
					r.err = errors.New("fetch panic")
				}
				r.at = time.Now()
				results[i] = r
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			r.ids, r.err = fetch(ctx, u)
		}()
	}
	wg.Wait()
	androidDataMu.Lock()
	defer androidDataMu.Unlock()
	if gen != androidSourceGen {
		return nil
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		m, o, cache, err := androidLoad(tx, "subJsonExt", "config")
		if err != nil || !o.Enabled {
			return err
		}
		cache.Prune(o)
		for _, r := range results {
			if slices.Contains(o.Sources, r.url) {
				old := cache[r.url]
				cache.RecordChecked(o, r.url, r.ids, r.err, r.at)
				if _, _, e := ae.Apply(m["subJsonExt"], m["config"], o, cache); errors.Is(e, ae.ErrLimit) {
					cache[r.url] = old
					cache.RecordChecked(o, r.url, nil, e, r.at)
				}
			}
		}
		return androidSaveCache(tx, cache)
	})
	if err == nil {
		InvalidateAndroidExclusions()
	} else {
		logger.Warning("Android exclusion refresh: save failed")
	}
	return err
}

// ---- статус для интерфейса ----------------------------------------------

type AndroidSourceStatus struct {
	Count     int   `json:"count"`
	Checked   int64 `json:"checked"`   // последняя успешная загрузка
	Attempted int64 `json:"attempted"` // последняя попытка
	Failed    bool  `json:"failed"`
}

type AndroidExclusionStatus struct {
	Sources map[string]AndroidSourceStatus `json:"sources"`
	Total   int                            `json:"total"`
	Removed int                            `json:"removed"`
	Warning string                         `json:"warning,omitempty"`
	Busy    bool                           `json:"busy"`
}

func GetAndroidExclusionStatus() (AndroidExclusionStatus, error) {
	st := AndroidExclusionStatus{Sources: map[string]AndroidSourceStatus{}, Busy: androidBusy.Load()}
	androidDataMu.Lock()
	m, o, cache, err := androidLoad(database.GetDB())
	androidDataMu.Unlock()
	if m == nil {
		return st, err
	}
	if err != nil {
		st.Warning = "options"
		return st, nil
	}
	for _, u := range o.Sources {
		s := cache[u]
		st.Sources[u] = AndroidSourceStatus{Count: len(s.IDs), Checked: s.OK, Attempted: s.Tried, Failed: s.Err != ""}
	}
	var ss SettingService
	if _, err = ss.EffectiveSubJsonExt(); err != nil {
		return st, err
	}
	if r := androidCur.Load(); r != nil && r.gen == androidGen.Load() && o.Enabled {
		st.Total, st.Removed, st.Warning = r.st.Total, r.st.Removed, r.st.Warning
	}
	if st.Warning == "" && o.Enabled && androidRefreshFailed.Load() {
		st.Warning = "refresh"
	}
	return st, nil
}
