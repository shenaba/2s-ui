package androidexclusions

// v4: хранятся только Options (что задал оператор) и Cache (скачанные списки).
// Готовый JSON шаблона нигде не хранится: Apply собирает его из этих данных,
// а сервис держит результат в памяти до следующего изменения данных.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

const MaxCacheBytes = 4 << 20
const MaxSources = 20

var ErrLimit = errors.New("Android exclusion limit exceeded")

const MaxInterval = 365 // дней

type Options struct {
	Enabled  bool     `json:"enabled"`
	Manual   string   `json:"manual"`
	Sources  []string `json:"sources"`
	Interval int      `json:"interval"` // дни; 0 = только вручную
}

// Source — всё, что известно об одном источнике. Время ПОПЫТКИ хранится
// отдельно от времени успеха, чтобы битый URL не долбился каждую минуту.
type Source struct {
	IDs   []string `json:"ids,omitempty"`
	OK    int64    `json:"ok,omitempty"`
	Tried int64    `json:"tried,omitempty"`
	Err   string   `json:"err,omitempty"`
}

type Cache map[string]Source

type Stats struct {
	Total   int    `json:"total"`
	Removed int    `json:"removed"`
	Warning string `json:"warning,omitempty"`
}

func DecodeOptions(raw string) (Options, error) {
	o := Options{Sources: []string{}}
	if strings.TrimSpace(raw) == "" {
		return o, nil
	}
	if len(raw) > MaxBytes {
		return o, errors.New("settings too large")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil || !json.Valid([]byte(raw)) || strings.TrimSpace(raw) == "null" {
		return o, errors.New("invalid Android exclusion settings")
	}
	if o.Interval < 0 || o.Interval > MaxInterval || len(o.Sources) > MaxSources {
		return o, errors.New("invalid interval or source count")
	}
	seen := map[string]bool{}
	src := make([]string, 0, len(o.Sources))
	for _, s := range o.Sources {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		u, err := url.Parse(s)
		if err != nil || len(s) > 2048 || validateURL(u) != nil {
			return o, errors.New("invalid HTTPS source URL")
		}
		seen[s] = true
		src = append(src, s)
	}
	o.Sources = src
	if len(Normalize(strings.Split(o.Manual, ","))) > MaxPackages {
		return o, ErrLimit
	}
	return o, nil
}

// Due: пора ли качать источник. Считается по последней ПОПЫТКЕ.
func Due(o Options, s Source, now time.Time, force bool) bool {
	if !o.Enabled {
		return false
	}
	if force || s.Tried == 0 {
		return true
	}
	return o.Interval > 0 && now.Unix()-s.Tried >= int64(o.Interval)*86400
}

// Record кладёт результат одной загрузки. Ошибка/пустой список не стирают
// последний хороший список — меняется только Tried/Err.
func (c Cache) Record(u string, ids []string, err error, at time.Time) {
	s := c[u]
	s.Tried = at.Unix()
	ids = Normalize(ids)
	if err != nil || len(ids) == 0 || len(ids) > MaxPackages {
		s.Err = "fetch"
	} else {
		s.IDs, s.OK, s.Err = ids, at.Unix(), ""
	}
	c[u] = s
}

// Prune выбрасывает кэш источников, которых больше нет в настройках.
func (c Cache) Prune(o Options) bool {
	changed := false
	for u := range c {
		if !slices.Contains(o.Sources, u) {
			delete(c, u)
			changed = true
		}
	}
	return changed
}

// Merge: ручные ID + кэш актуальных источников, без дублей.
func Merge(o Options, c Cache) []string {
	ids := strings.Split(o.Manual, ",")
	for _, u := range o.Sources {
		ids = append(ids, c[u].IDs...)
	}
	return Normalize(ids)
}

// CheckLimits bounds the stored cache and combined unique list.
func CheckLimits(o Options, c Cache) error {
	if len(Merge(o, c)) > MaxPackages {
		return ErrLimit
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(b) > MaxCacheBytes {
		return ErrLimit
	}
	return nil
}

// RecordChecked keeps the last good snapshot when a new list exceeds limits.
func (c Cache) RecordChecked(o Options, u string, ids []string, err error, at time.Time) {
	old := c[u]
	c.Record(u, ids, err, at)
	if CheckLimits(o, c) != nil {
		c[u] = old
		c.Record(u, nil, ErrLimit, at)
	}
}

// DecodeCache also reads the previous release's map[string][]string and metadata.
func DecodeCache(raw, meta string) Cache {
	c := Cache{}
	if raw == "" || len(raw) > MaxCacheBytes {
		return c
	}
	if json.Unmarshal([]byte(raw), &c) != nil || c == nil {
		var legacy map[string][]string
		if json.Unmarshal([]byte(raw), &legacy) != nil {
			return Cache{}
		}
		var m struct {
			Sources map[string]struct {
				Checked, Attempted int64
				Failed             bool
			}
		}
		_ = json.Unmarshal([]byte(meta), &m)
		c = Cache{}
		for u, ids := range legacy {
			st := m.Sources[u]
			src := Source{IDs: ids, OK: st.Checked, Tried: st.Attempted}
			if st.Failed {
				src.Err = "fetch"
			}
			c[u] = src
		}
	}
	for u, src := range c {
		ids := Normalize(src.IDs)
		if len(ids) > MaxPackages || (len(ids) == 0 && src.OK != 0) {
			delete(c, u)
			continue
		}
		src.IDs = ids
		c[u] = src
	}
	return c
}

func decodeObject(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var x map[string]any
	if err := d.Decode(&x); err != nil || x == nil || !json.Valid([]byte(raw)) {
		return nil, errors.New("invalid subscription template")
	}
	return x, nil
}

func idsOf(v any) ([]string, error) {
	switch a := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{a}, nil
	case []any:
		out := make([]string, 0, len(a))
		for _, it := range a {
			s, ok := it.(string)
			if !ok {
				return nil, errors.New("invalid exclude_package")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("invalid exclude_package")
}

// Apply — единственная точка слияния. Чистая функция: ничего не качает и не
// пишет. Приоритет у приложений внутри VPN: ID, упомянутые в правилах
// шаблона/сервера, удаляются из exclude и идут в счётчик Removed.
func Apply(raw, serverRaw string, o Options, c Cache) (string, Stats, error) {
	var st Stats
	if !o.Enabled {
		return raw, st, nil
	}
	x, err := decodeObject(raw)
	if err != nil {
		return "", st, err
	}
	srv, err := decodeObject(serverRaw)
	if err != nil {
		return "", st, err
	}
	if err := CheckLimits(o, c); err != nil {
		st.Warning = "limit"
		return "", st, err
	}
	if _, exists := x["inbounds"]; !exists {
		defaults, err := decodeObject(DefaultTemplate)
		if err != nil {
			return "", st, err
		}
		x["inbounds"] = defaults["inbounds"]
	}
	trees := []any{x["rules"], x["route"], x["dns"], x["rule_set"], srv["route"], srv["dns"]}
	cand := Merge(o, c)
	hasTun, hasInclude, changed := false, false, false
	inbounds, ok := x["inbounds"].([]any)
	if !ok {
		return "", st, errors.New("invalid inbounds")
	}
	applied, blocked := map[string]bool{}, map[string]bool{}
	for _, v := range inbounds {
		tun, ok := v.(map[string]any)
		if !ok || tun["type"] != "tun" {
			continue
		}
		hasTun = true
		inc, e := idsOf(tun["include_package"])
		if e != nil {
			return "", st, e
		}
		if len(inc) > 0 {
			hasInclude = true // include_package несовместим с exclude_package
			if _, has := tun["exclude_package"]; has {
				delete(tun, "exclude_package")
				changed = true
			}
			continue
		}
		cur, e := idsOf(tun["exclude_package"])
		if e != nil {
			return "", st, e
		}
		all := Normalize(append(slices.Clone(cand), cur...))
		if len(all) > MaxPackages {
			st.Warning = "limit"
			return "", st, ErrLimit
		}
		kept, _, e := Filter(all, trees...)
		if e != nil {
			return "", st, e
		}
		for _, id := range kept {
			applied[id] = true
		}
		for _, id := range all {
			if !applied[id] {
				blocked[id] = true
			}
		}
		if len(applied)+len(blocked) > MaxPackages {
			st.Warning = "limit"
			return "", st, ErrLimit
		}
		if !slices.Equal(cur, kept) {
			if len(kept) == 0 {
				delete(tun, "exclude_package")
			} else {
				tun["exclude_package"] = kept
			}
			changed = true
		}
	}
	st.Total, st.Removed = len(applied), len(blocked)
	switch {
	case !hasTun:
		st.Warning = "no_tun"
	case hasInclude:
		st.Warning = "include_package"
	}
	if !changed {
		return raw, st, nil
	}
	b, err := json.Marshal(x)
	if err != nil {
		return "", st, err
	}
	if bytes.Equal(b, []byte(raw)) {
		return raw, st, nil
	}
	return string(b), st, nil
}

// Strip — безопасный запасной вариант при ошибке Apply: шаблон без
// exclude_package (ручные ID тоже не отдаём — они не прошли проверку).
func Strip(raw string) (string, error) {
	x, err := decodeObject(raw)
	if err != nil || len(x) == 0 {
		return raw, err
	}
	inbounds, _ := x["inbounds"].([]any)
	for _, v := range inbounds {
		if tun, ok := v.(map[string]any); ok && tun["type"] == "tun" {
			delete(tun, "exclude_package")
		}
	}
	b, err := json.Marshal(x)
	return string(b), err
}
