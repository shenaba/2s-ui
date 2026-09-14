package logger

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/op/go-logging"
)

func resetBuffer(t *testing.T) {
	t.Helper()
	bufferMu.Lock()
	logBuffer = nil
	bufferMu.Unlock()
}

// GetLogs took `len(output) <= c`, which is still true once the slice already
// holds c entries, so every call returned one line more than it was asked for.
func TestGetLogsReturnsAtMostCountLines(t *testing.T) {
	resetBuffer(t)
	for i := 0; i < 10; i++ {
		addToBuffer("info", fmt.Sprintf("line-%d", i))
	}

	for _, want := range []int{0, 1, 3, 10} {
		got := GetLogs(want, "info")
		if len(got) != want {
			t.Errorf("GetLogs(%d) returned %d lines, want %d", want, len(got), want)
		}
	}

	// Asking for more than there is gives everything, not an error.
	if got := GetLogs(50, "info"); len(got) != 10 {
		t.Errorf("GetLogs(50) returned %d lines, want all 10", len(got))
	}
}

// Newest first, and the level filter keeps anything at or above the requested
// severity (go-logging orders CRITICAL lowest).
func TestGetLogsOrderAndLevel(t *testing.T) {
	resetBuffer(t)
	addToBuffer("debug", "a-debug")
	addToBuffer("error", "an-error")
	addToBuffer("info", "an-info")

	got := GetLogs(3, "debug")
	if len(got) != 3 {
		t.Fatalf("got %d lines %v, want 3", len(got), got)
	}
	if !strings.Contains(got[0], "an-info") {
		t.Errorf("first line = %q, want the newest entry", got[0])
	}

	// At error level the debug and info entries are filtered out.
	got = GetLogs(10, "error")
	if len(got) != 1 || !strings.Contains(got[0], "an-error") {
		t.Errorf("error level gave %v, want only the error entry", got)
	}
}

// The buffer is a ring: the oldest entry is dropped rather than letting it grow
// without bound.
func TestAddToBufferCapsTheRing(t *testing.T) {
	resetBuffer(t)
	for i := 0; i < 10245; i++ {
		addToBuffer("info", fmt.Sprintf("line-%d", i))
	}

	bufferMu.Lock()
	size := len(logBuffer)
	oldest := logBuffer[0].log
	bufferMu.Unlock()

	if size > 10240 {
		t.Errorf("buffer holds %d entries, want at most 10240", size)
	}
	if oldest == "line-0" {
		t.Error("the oldest entry was never evicted")
	}
}

// Every cron job, gin handler and sing-box connection goroutine appends to this
// buffer while the /logs endpoint reads it, and the re-slice above moves every
// element. Run with -race, this fails outright without the mutex.
func TestLogBufferIsSafeUnderConcurrency(t *testing.T) {
	resetBuffer(t)
	InitLogger(logging.ERROR)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				addToBuffer("info", fmt.Sprintf("w%d-%d", w, i))
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				_ = GetLogs(50, "debug")
			}
		}()
	}
	wg.Wait()
}
