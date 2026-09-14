package service

import (
	"sync"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

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

// Per-inbound traffic totals, published on the live payload and written by the
// flush below, so they race the same way onlineResources does and take the same
// kind of guard. A plain Mutex, not an RWMutex: a read can seed, so there is no
// read-only path to optimise for.
//
// Held in memory rather than aggregated per read. The stats table is the only
// record of how much went through one inbound, and it grows to a row per active
// (tag, bucket, direction) across the whole retention window — so answering
// "total per inbound" from it means re-scanning tens of thousands of rows every
// ten seconds for a figure that moves by a few kilobytes. Seeded once from that
// table instead, then advanced by each flush's own deltas, which the flush
// already has in hand.
var (
	inboundTrafficMu     sync.Mutex
	inboundTraffic       = map[string]int64{}
	inboundTrafficSeeded bool
)

// addInboundTraffic folds one flush's per-inbound deltas into the published
// totals, seeding from the stats table on first use.
//
// The caller must run this BEFORE writing the flush's own rows, so the seed and
// the deltas never cover the same bytes. A failed seed therefore drops this
// flush rather than counting it twice: the rows still land in the table, and
// the next attempt reads them back.
func addInboundTraffic(delta map[string]int64) {
	inboundTrafficMu.Lock()
	defer inboundTrafficMu.Unlock()
	if !seedInboundTrafficLocked() {
		return
	}
	for tag, traffic := range delta {
		inboundTraffic[tag] += traffic
	}
}

func seedInboundTrafficLocked() bool {
	if inboundTrafficSeeded {
		return true
	}
	var rows []TagTotal
	err := database.GetDB().Raw(
		`SELECT tag, SUM(traffic) AS traffic FROM stats WHERE resource = 'inbound' GROUP BY tag`,
	).Scan(&rows).Error
	if err != nil {
		logger.Warning("stats: reading inbound traffic totals failed: ", err)
		return false
	}
	for _, row := range rows {
		inboundTraffic[row.Tag] = row.Traffic
	}
	inboundTrafficSeeded = true
	return true
}

// InvalidateInboundTraffic drops the totals so the next read rebuilds them from
// the stats table. Call it wherever that table is replaced wholesale — clearing
// it when traffic graphs are turned off, or importing a database over a running
// panel. Dropping rather than zeroing is what makes it safe inside a
// transaction that may still roll back: the rebuild reads whatever survived.
func InvalidateInboundTraffic() {
	inboundTrafficMu.Lock()
	inboundTraffic = map[string]int64{}
	inboundTrafficSeeded = false
	inboundTrafficMu.Unlock()
}

type StatsService struct {
}

// GetInboundTraffic reports each inbound tag's total traffic, both directions
// summed. Seeds on first call so a panel that has not flushed yet — or one
// whose core is down — still answers with the history on disk.
func (s *StatsService) GetInboundTraffic() map[string]int64 {
	inboundTrafficMu.Lock()
	defer inboundTrafficMu.Unlock()
	seedInboundTrafficLocked()
	totals := make(map[string]int64, len(inboundTraffic))
	for tag, traffic := range inboundTraffic {
		totals[tag] = traffic
	}
	return totals
}

// Traffic drained from the core that has not reached the database yet.
//
// StatsTracker.GetStats is destructive -- it Swap(0)s every counter -- so a
// flush whose transaction fails has already taken the bytes out of the core and
// nothing else remembers them. One SQLITE_BUSY used to lose the whole ten-second
// window for every client on the panel.
//
// Only the database side is carried: the in-memory per-inbound totals are folded
// from each drain exactly once, whether or not the write lands, so they stay
// correct and simply run ahead of the table until the retry commits. The online
// lists are not carried either -- they answer "who is sending right now", so
// replaying a previous window would keep a client listed after it went away.
var (
	pendingMu    sync.Mutex
	pendingStats []model.Stats
)

// maxPendingStats bounds that buffer. A panel whose database has been refusing
// writes for hours should lose the oldest accounting rather than grow until it
// is killed for it; at roughly a row per active (resource, tag, direction) per
// ten seconds this is a few minutes of a busy panel.
const maxPendingStats = 50000

// takePending returns the rows still owed to the database and clears the buffer.
func takePending() []model.Stats {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	held := pendingStats
	pendingStats = nil
	return held
}

// holdPending keeps a batch that failed to commit, dropping the oldest rows if
// it has grown past the cap.
func holdPending(batch []model.Stats) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if len(batch) > maxPendingStats {
		dropped := len(batch) - maxPendingStats
		logger.Warning("stats: dropping ", dropped,
			" undelivered rows; the database has been refusing writes for a while")
		batch = batch[dropped:]
	}
	pendingStats = batch
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
	drained := st.GetStats()

	// Built locally, then published in one swap at the end — writing the live
	// struct field by field is what raced with readers.
	var fresh onlines

	// What the database is owed: anything a previous cycle could not commit,
	// then this drain. The online lists and the per-inbound deltas below come
	// from the drain alone.
	batch := append(takePending(), (*drained)...)
	if len(batch) == 0 {
		setOnlines(fresh)
		return nil
	}

	var err error
	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			// The commit itself can fail, and its error is the one that
			// decides whether these rows still need retrying.
			if cErr := tx.Commit().Error; cErr != nil {
				err = cErr
			}
		} else {
			tx.Rollback()
		}
		if err != nil {
			holdPending(batch)
		}
	}()

	now := time.Now().Unix()

	// Aggregate per-resource so each active inbound/outbound/user is reported
	// online once (a tag may now appear in both directions), and each user's
	// up+down collapse into a single UPDATE.
	type traffic struct{ up, down int64 }
	userTraffic := map[string]*traffic{}
	inboundDelta := map[string]int64{}
	seenInbound := map[string]bool{}
	seenOutbound := map[string]bool{}
	for _, stat := range *drained {
		switch stat.Resource {
		case "inbound":
			if !seenInbound[stat.Tag] {
				seenInbound[stat.Tag] = true
				fresh.Inbound = append(fresh.Inbound, stat.Tag)
			}
			inboundDelta[stat.Tag] += stat.Traffic
		case "outbound":
			if !seenOutbound[stat.Tag] {
				seenOutbound[stat.Tag] = true
				fresh.Outbound = append(fresh.Outbound, stat.Tag)
			}
		case "user":
			if _, seen := userTraffic[stat.Tag]; !seen {
				userTraffic[stat.Tag] = &traffic{}
				fresh.User = append(fresh.User, stat.Tag)
			}
		}
	}

	// The per-client counters come from the whole batch, so a window that
	// failed to commit is applied by the retry rather than lost. A user in
	// the carried-over rows but not in this drain still gets its bytes, it
	// just is not listed as online.
	for _, stat := range batch {
		if stat.Resource != "user" {
			continue
		}
		t, ok := userTraffic[stat.Tag]
		if !ok {
			t = &traffic{}
			userTraffic[stat.Tag] = t
		}
		if stat.Direction {
			t.up += stat.Traffic
		} else {
			t.down += stat.Traffic
		}
	}

	// Publish before the DB work below: the online lists are complete now, and
	// a failed traffic write must not leave readers on a stale set.
	setOnlines(fresh)

	// Before the upsert below, which is what makes the seed and these deltas
	// disjoint — see addInboundTraffic.
	addInboundTraffic(inboundDelta)

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
	for i := range batch {
		batch[i].DateTime = bucket
	}
	err = tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource"}, {Name: "tag"}, {Name: "date_time"}, {Name: "direction"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"traffic": gorm.Expr("stats.traffic + excluded.traffic")}),
	}).Create(&batch).Error
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

// delOldStatsChunk caps how many rows one DELETE removes, so the write lock is
// released between chunks.
const delOldStatsChunk = 5000

// DelOldStats drops stats past the retention window, in bounded chunks.
//
// One unbounded DELETE over a table that holds a row per active tag, bucket and
// direction across the whole window held the write lock past the ten-second busy
// timeout, which made the daily cleanup itself a cause of the lost traffic
// accounting it shares a database with.
func (s *StatsService) DelOldStats(days int) error {
	oldTime := time.Now().AddDate(0, 0, -(days)).Unix()
	db := database.GetDB()
	for {
		res := db.Where("id IN (?)",
			db.Model(model.Stats{}).Select("id").Where("date_time < ?", oldTime).Limit(delOldStatsChunk),
		).Delete(model.Stats{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected < delOldStatsChunk {
			return nil
		}
	}
}
