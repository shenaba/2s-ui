package service

import (
	"testing"

	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// GetSingboxInfo read IsRunning and then dereferenced GetInstance, two locks
// apart. Start and Stop write both fields under one lock, so "running, but no
// instance" is never a resting state -- it is only ever what a reader sees when
// a stop lands between its two reads. Box.Uptime has no nil receiver guard, so
// that reader panicked.
//
// That reader is the panel's status poll. The websocket hub runs it every two
// seconds for every open tab, from a goroutine with no recover of its own, and
// every config save restarts the core: the two overlap on an ordinary working
// day, and the result is the process going down.
//
// The window is too short to hit deliberately, so the state it produces is set
// directly instead. That is what SetStateForTest exists for.
func TestSingboxInfoSurvivesAStopBetweenTheTwoReads(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	previous := corePtr
	c := core.NewCore()
	NewConfigService(c)
	t.Cleanup(func() { corePtr = previous })

	c.SetStateForTest(true, nil)

	var server ServerService
	info := server.GetSingboxInfo()

	// running has to follow what was actually read, not a flag from a moment
	// that no longer holds.
	if info["running"] != false {
		t.Errorf("running = %v, want false: there is no box", info["running"])
	}
	stats, ok := info["stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("stats missing from %v", info)
	}
	if stats["Uptime"] != uint32(0) {
		t.Errorf("Uptime = %v, want 0", stats["Uptime"])
	}
}

// And the ordinary case still reports a running core, so the above is not
// passing because the flag is hard-wired to false.
func TestSingboxInfoReportsARunningCore(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	previous := corePtr
	c := core.NewCore()
	NewConfigService(c)
	t.Cleanup(func() {
		_ = c.Stop()
		corePtr = previous
	})

	if err := c.Start([]byte(`{"log":{"disabled":true}}`)); err != nil {
		t.Fatalf("start: %v", err)
	}

	var server ServerService
	if info := server.GetSingboxInfo(); info["running"] != true {
		t.Errorf("running = %v, want true", info["running"])
	}
}
