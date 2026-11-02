package service

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shenaba/2s-ui/core"
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

// ClientRate is one client's throughput over the last rate sample, in bytes
// per second, summed across every inbound it is on -- the same scope as its
// up/down totals.
type ClientRate struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// Per-client rates from the last sample, guarded by the online lists' lock.
// Replaced wholesale on every sample and never written in place, so a reader
// may hand the map out as it is.
var clientRates = map[string]ClientRate{}

func setClientRates(rates map[string]ClientRate) {
	onlineMu.Lock()
	clientRates = rates
	onlineMu.Unlock()
}

// minRateWindow is the shortest window a sample closes (see SampleUsers). A
// tick landing right after another -- or right after the baseline the first
// subscriber takes -- leaves the published rates alone and measures next time.
const minRateWindow = time.Second

// maxRateWindow is the longest window still published as a current speed.
// Sampling pauses while no tab is watching, and the first sample after that
// would otherwise show an average over however long the panel sat closed.
const maxRateWindow = 5 * time.Second

// ratesOf turns one sample into per-client rates over the window it covered.
// A window outside (0, maxRateWindow] is no current speed, and nothing is
// published for it rather than a guess.
//
// The rate is sampled from the core's counters on its own short tick
// (TickClientRates), not taken from the stats flush: that one runs every 10s
// for the database's sake, and a rate that only moved that often read as
// stuck. Nor is it derived from the clients' up/down: a node's traffic is
// folded into those a minute at a time, and a traffic reset sends them back to
// zero.
func ratesOf(moved map[string]core.UserBytes, window time.Duration) map[string]ClientRate {
	rates := map[string]ClientRate{}
	if window <= 0 || window > maxRateWindow {
		return rates
	}
	seconds := window.Seconds()
	for name, b := range moved {
		rates[name] = ClientRate{
			Up:   int64(float64(b.Up) / seconds),
			Down: int64(float64(b.Down) / seconds),
		}
	}
	return rates
}

// Whether rate sampling is on: set by the first subscriber after a pause
// (resumeClientRates) or by a tick that finds one, cleared by the tick that
// finds none (pauseClientRates). True does not mean a tick has sampled yet --
// right after a subscribe only the baseline has. Sampling walks every user
// under the lock each new connection's routing takes, so it only runs while a
// tab is watching (TickClientRates).
var ratesLive atomic.Bool

// sampleClientRates publishes every client's throughput since the previous
// sample. With the core down there is nothing to sample and the rates go
// empty, as they would for a core with no traffic; a window too short to
// close leaves the published rates as they are.
func sampleClientRates(now time.Time) {
	rates := map[string]ClientRate{}
	if corePtr != nil && corePtr.IsRunning() {
		if box := corePtr.GetInstance(); box != nil {
			if st := box.StatsTracker(); st != nil {
				moved, window, ok := st.SampleUsers(now, minRateWindow)
				if !ok {
					return
				}
				rates = ratesOf(moved, window)
			}
		}
	}
	setClientRates(rates)
}

// pauseClientRates stops sampling once the last tab is gone, emptying the
// rates so api/load does not serve the last ones as current. Only the tick
// that finds sampling live does anything; the idle ones after it touch
// nothing. Reports whether it paused.
func pauseClientRates() bool {
	if !ratesLive.CompareAndSwap(true, false) {
		return false
	}
	setClientRates(map[string]ClientRate{})
	return true
}

// resumeClientRates runs when a tab subscribes. On the first one after a pause
// it samples at once: that sample spans the pause and is dropped by
// maxRateWindow, but it starts a window, so the next tick already has a speed
// to show rather than the one after it. Later subscribers find sampling live
// and leave the window alone.
func resumeClientRates(now time.Time) {
	if ratesLive.CompareAndSwap(false, true) {
		sampleClientRates(now)
	}
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
	pendingMu          sync.Mutex
	pendingStats       []model.Stats
	pendingTargetStats []model.TargetStats
)

// maxPendingStats bounds that buffer. A panel whose database has been refusing
// writes for hours should lose the oldest accounting rather than grow until it
// is killed for it. SaveStats folds what it holds back to a row per active
// (resource, tag, direction), so only the drains carried over between two
// flushes stack up here; the cap is a backstop, not something a slow database
// reaches.
const maxPendingStats = 50000

// takePending returns the rows still owed to the database and clears the buffer.
func takePending() []model.Stats {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	held := pendingStats
	pendingStats = nil
	return held
}

func takePendingTargets() []model.TargetStats {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	held := pendingTargetStats
	pendingTargetStats = nil
	return held
}

// holdPending keeps a batch that failed to commit. It goes in front of whatever
// was carried over while it ran (carryOverStats), which a plain replace would
// throw away; the batch is older, being what the flush took first.
func holdPending(batch []model.Stats) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	keepPendingLocked(append(batch, pendingStats...))
}

func holdPendingTargets(batch []model.TargetStats) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	keepPendingTargetsLocked(append(batch, pendingTargetStats...))
}

// carryPending adds rows newer than everything already held.
func carryPending(rows []model.Stats) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	keepPendingLocked(append(pendingStats, rows...))
}

func carryPendingTargets(rows []model.TargetStats) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	keepPendingTargetsLocked(append(pendingTargetStats, rows...))
}

// keepPendingLocked stores batch, oldest first, dropping the oldest rows if it
// has grown past the cap. The caller holds pendingMu.
func keepPendingLocked(batch []model.Stats) {
	if len(batch) > maxPendingStats {
		dropped := len(batch) - maxPendingStats
		logger.Warning("stats: dropping ", dropped,
			" undelivered rows; the database has been refusing writes for a while")
		batch = batch[dropped:]
	}
	pendingStats = batch
}

func keepPendingTargetsLocked(batch []model.TargetStats) {
	if len(batch) > maxPendingStats {
		dropped := len(batch) - maxPendingStats
		batch = batch[dropped:]
	}
	pendingTargetStats = batch
}

// carryOverStats takes what the running core has counted since the last flush
// and leaves it for the next one, which commits it from the pending buffer.
// Called just before the core is stopped: Box.Close resets the tracker, so what
// it held would otherwise be lost -- up to ten seconds of every client's
// traffic, on every restart: the restart button, a route/DNS settings save, an
// edit to a group's member, the watchdog.
//
// The per-inbound totals are folded here, as SaveStats folds them from each
// drain exactly once; the database rows and the per-client counters, which
// SaveStats takes from the whole batch, ride along in the buffer.
func carryOverStats() {
	if corePtr == nil {
		return
	}
	box := corePtr.GetInstance()
	if box == nil || box.StatsTracker() == nil {
		return
	}
	drained := *box.StatsTracker().GetStats()
	if len(drained) > 0 {
		inboundDelta := map[string]int64{}
		for _, stat := range drained {
			if stat.Resource == "inbound" {
				inboundDelta[stat.Tag] += stat.Traffic
			}
		}
		addInboundTraffic(inboundDelta)
		carryPending(drained)
	}
	drainedTargets := *box.StatsTracker().GetTargetStats()
	if len(drainedTargets) > 0 {
		carryPendingTargets(drainedTargets)
	}
}

// statsFlush is the stats job's own run, registered by the cron job, which is
// what knows the settings a flush needs (traffic retention, bucket size).
var statsFlush atomic.Pointer[func()]

func SetStatsFlush(flush func()) {
	statsFlush.Store(&flush)
}

// FlushStats commits what the core has counted, now, while it is still
// running. For the exits that leave no next flush -- shutdown and the exec
// restart of an in-place update: carryOverStats only hands the traffic to a
// flush that will never come there.
func FlushStats() {
	if flush := statsFlush.Load(); flush != nil && *flush != nil {
		(*flush)()
	}
}

func (s *StatsService) SaveStats(enableTraffic bool, bucketSeconds int64) error {
	// With the core down there is nothing to drain, but the pending rows still
	// go in: stopping the core carries its last counts there
	// (carryOverStats), and a core that will not start again -- or a shutdown
	// with the core already down -- would otherwise hold them until they are
	// lost with the process.
	drained := &[]model.Stats{}
	drainedTargets := &[]model.TargetStats{}
	if corePtr != nil && corePtr.IsRunning() {
		if box := corePtr.GetInstance(); box != nil {
			if st := box.StatsTracker(); st != nil {
				drained = st.GetStats()
				drainedTargets = st.GetTargetStats()
			}
		}
	}
	// Built locally, then published in one swap at the end — writing the live
	// struct field by field is what raced with readers.
	var fresh onlines

	// What the database is owed: anything a previous cycle could not commit,
	// then this drain, folded per key (mergeStats) -- only when something was
	// carried over, as a drain alone already has a row per key. The online
	// lists and the per-inbound deltas below come from the drain alone.
	batch := *drained
	if pending := takePending(); len(pending) > 0 {
		batch = mergeStats(append(pending, batch...))
	}
	targetBatch := *drainedTargets
	if pending := takePendingTargets(); len(pending) > 0 {
		targetBatch = mergeTargetStats(append(pending, targetBatch...))
	}
	if len(batch) == 0 && len(targetBatch) == 0 {
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
			if len(targetBatch) > 0 {
				holdPendingTargets(targetBatch)
			}
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
	if len(batch) > 0 {
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
		if err != nil {
			return err
		}
	}

	if len(targetBatch) > 0 {
		targetBucket := now - (now % 3600)
		for i := range targetBatch {
			targetBatch[i].DateTime = targetBucket
		}
		targetBatch = mergeTargetStats(targetBatch)
		err = tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user"}, {Name: "target"}, {Name: "date_time"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"up":   gorm.Expr("target_stats.up + excluded.up"),
				"down": gorm.Expr("target_stats.down + excluded.down"),
			}),
		}).Create(&targetBatch).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func mergeTargetStats(rows []model.TargetStats) []model.TargetStats {
	type key struct {
		user   string
		target string
	}
	index := make(map[key]int, len(rows))
	merged := make([]model.TargetStats, 0, len(rows))
	for _, row := range rows {
		k := key{row.User, row.Target}
		if i, ok := index[k]; ok {
			merged[i].Up += row.Up
			merged[i].Down += row.Down
			continue
		}
		index[k] = len(merged)
		row.Id = 0
		merged = append(merged, row)
	}
	return merged
}

// mergeStats folds the rows of each (resource, tag, direction) into one, in
// order of first appearance. A failed cycle comes back in front of the next
// drain, which has a row for most of the same keys; folded, the batch stays
// one row per key however many cycles fail in a row. The date is dropped with
// the duplicates, as every row is stamped with the current bucket before the
// insert anyway, and so is the id: a rolled-back insert may have filled it in,
// and the retry must not bind it.
func mergeStats(rows []model.Stats) []model.Stats {
	type key struct {
		resource  string
		tag       string
		direction bool
	}
	index := make(map[key]int, len(rows))
	merged := make([]model.Stats, 0, len(rows))
	for _, row := range rows {
		k := key{row.Resource, row.Tag, row.Direction}
		if i, ok := index[k]; ok {
			merged[i].Traffic += row.Traffic
			continue
		}
		index[k] = len(merged)
		row.Id = 0
		merged = append(merged, row)
	}
	return merged
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

// GetClientRates reports the throughput of each client that moved traffic in
// the last rate sample. Local core only: a node's clients reach this panel's
// totals through NodeTrafficJob a minute at a time, which is no basis for a
// rate, so a client a node serves has only its local share here -- and one
// served by nodes alone shows online with no rate at all.
func (s *StatsService) GetClientRates() map[string]ClientRate {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	return clientRates
}

const nodeOnlineTTL = 20 * time.Second

// GetClusterOnlines adds recent node snapshots to the local stats window.
// A node's apiv2/onlines still uses GetOnlines, so it never returns an
// already-aggregated list to its own master.
func (s *StatsService) GetClusterOnlines() (onlines, error) {
	result, err := s.GetOnlines()
	if err != nil {
		return onlines{}, err
	}

	nodeStatusMu.RLock()
	var nodeUsers []string
	now := time.Now().Unix()
	for _, status := range nodeStatuses {
		if status.State == "online" && status.onlineCheckedAt > 0 &&
			now-status.onlineCheckedAt <= int64(nodeOnlineTTL.Seconds()) {
			nodeUsers = append(nodeUsers, status.onlineUsers...)
		}
	}
	nodeStatusMu.RUnlock()
	if len(nodeUsers) == 0 {
		return result, nil
	}
	sort.Strings(nodeUsers)

	// Nodes may also host their own clients. Only names owned by this panel
	// belong in its online count -- asked about the node users alone rather
	// than reading every client name, since this runs on every live push.
	var names []string
	if err := database.GetDB().Model(model.Client{}).Where("name IN ?", nodeUsers).
		Pluck("name", &names).Error; err != nil {
		return onlines{}, err
	}
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	seen := make(map[string]bool, len(result.User))
	result.User = append([]string(nil), result.User...)
	for _, name := range result.User {
		seen[name] = true
	}
	for _, name := range nodeUsers {
		if known[name] && !seen[name] {
			result.User = append(result.User, name)
			seen[name] = true
		}
	}
	return result, nil
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
			break
		}
	}
	for {
		res := db.Where("id IN (?)",
			db.Model(model.TargetStats{}).Select("id").Where("date_time < ?", oldTime).Limit(delOldStatsChunk),
		).Delete(model.TargetStats{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected < delOldStatsChunk {
			return nil
		}
	}
}

type TargetStatsQuery struct {
	User      string `json:"user"`
	Period    string `json:"period"` // "hour", "day", "month", "60day", "90day"
	Search    string `json:"search"`
	SortField string `json:"sort"`  // "up", "down", "total"
	SortOrder string `json:"order"` // "desc", "asc"
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
}

type TargetStatItem struct {
	Target string `json:"target"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
	Total  int64  `json:"total"`
}

type TargetStatsSummary struct {
	TotalUp    int64 `json:"totalUp"`
	TotalDown  int64 `json:"totalDown"`
	Total      int64 `json:"total"`
	TotalSites int64 `json:"totalSites"`
}

type TargetStatsResponse struct {
	Items   []TargetStatItem   `json:"items"`
	Summary TargetStatsSummary `json:"summary"`
	Total   int64              `json:"total"`
	Page    int                `json:"page"`
	Limit   int                `json:"limit"`
}

func (s *StatsService) GetTargetStats(query TargetStatsQuery) (*TargetStatsResponse, error) {
	now := time.Now().Unix()
	var startTime int64
	switch query.Period {
	case "hour":
		startTime = now - 3600
	case "day":
		startTime = now - 86400
	case "month":
		startTime = now - 86400*30
	case "60day":
		startTime = now - 86400*60
	case "90day":
		startTime = now - 86400*90
	default:
		startTime = now - 86400
	}

	db := database.GetDB()
	filterQuery := func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("date_time >= ?", startTime)
		if query.User != "" {
			tx = tx.Where("user = ?", query.User)
		}
		if query.Search != "" {
			tx = tx.Where("target LIKE ?", "%"+strings.ToLower(strings.TrimSpace(query.Search))+"%")
		}
		return tx
	}

	var summary TargetStatsSummary
	type sumResult struct {
		TotalUp    int64 `gorm:"column:total_up"`
		TotalDown  int64 `gorm:"column:total_down"`
		TotalSites int64 `gorm:"column:total_sites"`
	}
	var sr sumResult
	err := filterQuery(db.Model(&model.TargetStats{})).Select(
		"COALESCE(SUM(up), 0) AS total_up, COALESCE(SUM(down), 0) AS total_down, COUNT(DISTINCT target) AS total_sites",
	).Scan(&sr).Error
	if err != nil {
		return nil, err
	}
	summary.TotalUp = sr.TotalUp
	summary.TotalDown = sr.TotalDown
	summary.Total = sr.TotalUp + sr.TotalDown
	summary.TotalSites = sr.TotalSites

	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	} else if limit > 200 {
		limit = 200
	}
	offset := (page - 1) * limit

	sortCol := "(SUM(up) + SUM(down))"
	switch query.SortField {
	case "up":
		sortCol = "SUM(up)"
	case "down":
		sortCol = "SUM(down)"
	case "total":
		sortCol = "(SUM(up) + SUM(down))"
	default:
		sortCol = "(SUM(up) + SUM(down))"
	}
	sortOrder := "DESC"
	if strings.ToUpper(query.SortOrder) == "ASC" {
		sortOrder = "ASC"
	}

	var items []TargetStatItem
	err = filterQuery(db.Model(&model.TargetStats{})).Select(
		"target, SUM(up) AS up, SUM(down) AS down, (SUM(up) + SUM(down)) AS total",
	).Group("target").Order(sortCol + " " + sortOrder).Offset(offset).Limit(limit).Scan(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []TargetStatItem{}
	}

	return &TargetStatsResponse{
		Items:   items,
		Summary: summary,
		Total:   summary.TotalSites,
		Page:    page,
		Limit:   limit,
	}, nil
}
