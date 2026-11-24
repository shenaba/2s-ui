package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	ae "github.com/shenaba/2s-ui/internal/androidexclusions"
	"gorm.io/gorm"
)

func TestAndroidV4Lifecycle(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "android.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { InvalidateAndroidExclusions(); _ = database.CloseDBForTest() })
	InvalidateAndroidExclusions()
	db := database.GetDB()
	s := &SettingService{}
	if _, err := s.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	const u = "https://example.com/list.txt"
	const opts = `{"enabled":true,"manual":"com.manual,com.rule","sources":["https://example.com/list.txt"],"interval":0}`
	const raw = `{"rules":[{"package_name":"com.rule"}]}`
	save := func(values map[string]string) {
		t.Helper()
		b, _ := json.Marshal(values)
		androidDataMu.Lock()
		defer androidDataMu.Unlock()
		if err := db.Transaction(func(tx *gorm.DB) error { return s.Save(tx, b) }); err != nil {
			t.Fatal(err)
		}
		androidSourceGen++
		InvalidateAndroidExclusions()
	}
	save(map[string]string{androidOptionsKey: opts, "subJsonExt": raw})
	check := func(want ...string) {
		t.Helper()
		out, e := s.EffectiveSubJsonExt()
		if e != nil {
			t.Fatal(e)
		}
		var cfg struct {
			Inbounds []struct {
				Exclude []string `json:"exclude_package"`
			}
		}
		if e = json.Unmarshal([]byte(out), &cfg); e != nil || len(cfg.Inbounds) != 2 || !slices.Equal(cfg.Inbounds[0].Exclude, want) {
			t.Fatal(out, e, want)
		}
	}
	check("com.manual")
	// Legacy data and metadata migrate into the two-key representation.
	if e := db.Transaction(func(tx *gorm.DB) error {
		for k, v := range map[string]string{androidCacheKey: `{"https://example.com/list.txt":["com.old"]}`, androidMetaKey: `{"sources":{"https://example.com/list.txt":{"checked":10,"attempted":20}}}`, androidPreparedKey: "obsolete"} {
			if e := androidUpsert(tx, k, v); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := MigrateAndroidExclusions(); e != nil {
		t.Fatal(e)
	}
	check("com.manual", "com.old")
	var n int64
	if e := db.Model(&model.Setting{}).Where("key IN ?", []string{androidMetaKey, androidPreparedKey, androidRestartKey}).Count(&n).Error; e != nil || n != 0 {
		t.Fatal(n, e)
	}
	calls := 0
	fetch := func(context.Context, string) ([]string, error) {
		calls++
		return []string{"com.remote", "com.rule"}, nil
	}
	if e := refreshAndroidExclusions(false, fetch); e != nil || calls != 0 {
		t.Fatal(e, calls)
	}
	if e := MarkAndroidExclusionsRestart(); e != nil {
		t.Fatal(e)
	}
	if e := refreshAndroidExclusions(false, fetch); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	check("com.manual", "com.remote")
	if e := refreshAndroidExclusions(true, func(context.Context, string) ([]string, error) { return nil, errors.New("offline") }); e != nil {
		t.Fatal(e)
	}
	check("com.manual", "com.remote")
	st, e := GetAndroidExclusionStatus()
	if e != nil || !st.Sources[u].Failed || st.Total != 2 || st.Removed != 1 {
		t.Fatal(st, e)
	}
	// Removing and re-adding a URL during a download invalidates its result.
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- refreshAndroidExclusions(true, func(context.Context, string) ([]string, error) {
			close(started)
			<-release
			return []string{"com.stale"}, nil
		})
	}()
	<-started
	save(map[string]string{androidOptionsKey: `{"enabled":false,"manual":"com.manual,com.rule","sources":[],"interval":0}`})
	save(map[string]string{androidOptionsKey: opts})
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	_, _, cache, e := androidLoad(db)
	if e != nil || len(cache) != 0 {
		t.Fatal(cache, e)
	}
	check("com.manual")
	if e := refreshAndroidExclusions(false, fetch); e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
	check("com.manual", "com.remote")
	// Settings cannot overwrite service bookkeeping.
	for _, k := range []string{androidCacheKey, androidMetaKey, androidPreparedKey, androidRestartKey} {
		b, _ := json.Marshal(map[string]string{k: "bad"})
		if e := db.Transaction(func(tx *gorm.DB) error { return s.Save(tx, b) }); e == nil {
			t.Fatal(k)
		}
	}
	all, e := s.GetAllSetting()
	if e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{androidCacheKey, androidMetaKey, androidPreparedKey, androidRestartKey} {
		if _, ok := (*all)[k]; ok {
			t.Fatal(k)
		}
	}
	stored, e := s.GetSubJsonExt()
	if e != nil || stored != raw {
		t.Fatal(stored, e)
	}
	if e := db.Transaction(func(tx *gorm.DB) error { return androidSaveCache(tx, ae.Cache{}) }); e != nil {
		t.Fatal(e)
	}
	InvalidateAndroidExclusions()
	check("com.manual")
}
