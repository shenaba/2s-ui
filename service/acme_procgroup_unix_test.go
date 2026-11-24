//go:build !windows

package service

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runAcmeCmd's timeout has to bound the whole run, not just the process it started.
// acme.sh backgrounds socat and runs curl as children; before the process group
// was killed, a child holding stdout kept CombinedOutput waiting long past the
// timeout, and a backgrounded one outlived the panel's interest in it entirely.
func TestRunCmdTimeoutKillsChildren(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	start := time.Now()
	// The background sleep stands in for acme.sh's socat: it inherits stdout.
	_, err := runAcmeCmd(time.Second, t.TempDir(), "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; sleep 30")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("runCmd returned after %v; the 1s timeout did not bound it", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "命令超时") {
		t.Fatalf("err = %v, want a timeout", err)
	}

	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("child pid: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil || pid <= 0 {
		t.Fatalf("bad child pid %q", raw)
	}
	// The child was orphaned when sh died. Once SIGKILL lands it is either gone
	// (reaped by whatever adopted it) or a zombie: in a container whose PID 1
	// reaps nothing, the latter is all it will ever be, and kill(pid, 0) still
	// succeeds on a zombie. Either one is dead; only a running child is a miss.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH || isZombie(pid) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("background child %d survived the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// isZombie reports whether pid has exited and is waiting to be reaped. Only
// Linux exposes that cheaply (/proc); elsewhere it reports false, and the test
// relies on the orphan being reaped, which launchd does promptly on macOS.
func isZombie(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// "pid (comm) S ...": comm may itself contain spaces or parentheses, so the
	// state is the first field after the last ')'.
	stat := string(raw)
	i := strings.LastIndexByte(stat, ')')
	if i < 0 || i+2 >= len(stat) {
		return false
	}
	return stat[i+2] == 'Z'
}
