package service

import (
	"sync"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type onlines struct {
	Inbound  []string `json:"inbound,omitempty"`
	User     []string `json:"user,omitempty"`
	Outbound []string `json:"outbound,omitempty"`
}

// Guards onlineResources. The flush below rewrites it every 10s while readers
// run on unrelated goroutines — gin handlers for api/load, and the websocket
// readPump when a client subscribes. Confirmed as a data race by the race
// detector against a live panel.
var (
	onlineMu        sync.RWMutex
	onlineResources = &onlines{}
)

// setOnlines publishes a freshly built snapshot in one guarded swap; readers
// therefore never observe a half-rebuilt list.
func setOnlines(o onlines) {
	onlineMu.Lock()
	*onlineResources = o
	onlineMu.Unlock()
}

type StatsService struct {
}

func (s *StatsService) SaveStats(enableTraffic bool, bucketSeconds int64) error {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	box := corePtr.GetInstance()
	if box == nil {
		return nil
	}
	st := box.StatsTracker()
	if st == nil {
		return nil
	}
	stats := st.GetStats()

	// Built locally, then published in one swap at the end — writing the live
	// struct field by field is what raced with readers.
	var fresh onlines

	if len(*stats) == 0 {
		setOnlines(fresh)
		return nil
	}

	var err error
	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	now := time.Now().Unix()

	// Aggregate per-resource so each active inbound/outbound/user is reported
	// online once (a tag may now appear in both directions), and each user's
	// up+down collapse into a single UPDATE.
	type traffic struct{ up, down int64 }
	userTraffic := map[string]*traffic{}
	seenInbound := map[string]bool{}
	seenOutbound := map[string]bool{}
	for _, stat := range *stats {
		switch stat.Resource {
		case "inbound":
			if !seenInbound[stat.Tag] {
				seenInbound[stat.Tag] = true
				fresh.Inbound = append(fresh.Inbound, stat.Tag)
			}
		case "outbound":
			if !seenOutbound[stat.Tag] {
				seenOutbound[stat.Tag] = true
				fresh.Outbound = append(fresh.Outbound, stat.Tag)
			}
		case "user":
			t, ok := userTraffic[stat.Tag]
			if !ok {
				t = &traffic{}
				userTraffic[stat.Tag] = t
				fresh.User = append(fresh.User, stat.Tag)
			}
			if stat.Direction {
				t.up += stat.Traffic
			} else {
				t.down += stat.Traffic
			}
		}
	}

	// Publish before the DB work below: the online lists are complete now, and
	// a failed traffic write must not leave readers on a stale set.
	setOnlines(fresh)

	for name, t := range userTraffic {
		update := map[string]interface{}{"online_at": now}
		if t.up > 0 {
			update["up"] = gorm.Expr("up + ?", t.up)
		}
		if t.down > 0 {
			update["down"] = gorm.Expr("down + ?", t.down)
		}
		err = tx.Model(model.Client{}).Where("name = ?", name).Updates(update).Error
		if err != nil {
			return err
		}
	}

	if !enableTraffic {
		return nil
	}

	// Round each sample down to its bucket and upsert, so all 10s cycles within
	// the same bucket accumulate into one row per (resource, tag, direction).
	if bucketSeconds < 1 {
		bucketSeconds = 1
	}
	bucket := now - (now % bucketSeconds)
	for i := range *stats {
		(*stats)[i].DateTime = bucket
	}
	err = tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource"}, {Name: "tag"}, {Name: "date_time"}, {Name: "direction"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"traffic": gorm.Expr("stats.traffic + excluded.traffic")}),
	}).Create(&stats).Error
	return err
}

func (s *StatsService) GetStats(resource string, tag string, period string) ([]model.Stats, error) {
	now := time.Now().Unix()
	var bucketSec int64
	var startTime int64
	switch period {
	case "day":
		bucketSec = 3600
		startTime = now - 86400
	case "month":
		bucketSec = 86400
		startTime = now - 86400*30
	case "60day":
		bucketSec = 86400
		startTime = now - 86400*60
	case "90day":
		bucketSec = 86400
		startTime = now - 86400*90
	default: // "hour"
		bucketSec = 60
		startTime = now - 3600
	}

	// Never read with a finer resolution than samples are stored at
	// (statsBucketSeconds is user-configurable): most read buckets would be
	// empty and the chart would render blank/jagged.
	if storedBucket, _ := (&SettingService{}).GetStatsBucketSeconds(); storedBucket > bucketSec {
		bucketSec = storedBucket
	}

	db := database.GetDB()
	resources := []string{resource}
	if resource == "endpoint" {
		resources = []string{"inbound", "outbound"}
	}

	type bucketRow struct {
		Bucket    int64
		Direction bool
		Traffic   int64
	}
	var rows []bucketRow
	err := db.Raw(
		`SELECT (date_time / ?) * ? AS bucket, direction, SUM(traffic) AS traffic
		 FROM stats
		 WHERE resource IN ? AND tag = ? AND date_time > ? AND date_time <= ?
		 GROUP BY bucket, direction
		 ORDER BY bucket`,
		bucketSec, bucketSec, resources, tag, startTime, now,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Build lookup map
	type key struct {
		bucket    int64
		direction bool
	}
	lookup := make(map[key]int64, len(rows))
	for _, r := range rows {
		lookup[key{r.Bucket, r.Direction}] = r.Traffic
	}

	// Fill all buckets including empty ones so x-axis is evenly distributed
	firstBucket := (startTime / bucketSec) * bucketSec
	var result []model.Stats
	for b := firstBucket; b <= now; b += bucketSec {
		for _, dir := range []bool{false, true} {
			result = append(result, model.Stats{
				DateTime:  b,
				Resource:  resource,
				Tag:       tag,
				Direction: dir,
				Traffic:   lookup[key{b, dir}],
			})
		}
	}
	return result, nil
}

// TagTotal is one tag's traffic over a window, both directions summed.
type TagTotal struct {
	Tag     string
	Traffic int64
}

// TopTags ranks the busiest tags of one resource since a point in time.
//
// GetStats answers "how did this one tag move over the last day", which is what
// a chart needs. Nothing answered "which ones moved most", which is what a
// report needs, and doing it by pulling every series and sorting in Go would
// read the whole table to keep ten rows.
//
// resource is one of the values SaveStats writes: inbound, outbound or user.
func (s *StatsService) TopTags(resource string, since int64, limit int) ([]TagTotal, error) {
	var rows []TagTotal
	err := database.GetDB().Raw(
		`SELECT tag, SUM(traffic) AS traffic
		 FROM stats
		 WHERE resource = ? AND date_time > ?
		 GROUP BY tag
		 HAVING traffic > 0
		 ORDER BY traffic DESC
		 LIMIT ?`,
		resource, since, limit,
	).Scan(&rows).Error
	return rows, err
}

func (s *StatsService) GetOnlines() (onlines, error) {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	return *onlineResources, nil
}
func (s *StatsService) DelOldStats(days int) error {
	oldTime := time.Now().AddDate(0, 0, -(days)).Unix()
	db := database.GetDB()
	return db.Where("date_time < ?", oldTime).Delete(model.Stats{}).Error
}
