package database

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The database file holds every client's credentials and the panel's session
// secret. The directory mode was written as 01740, where the leading 01000 is
// not Go's sticky bit (os.ModeSticky is 1<<24) -- it was masked away and the
// directory came out 0740, readable by the group.
func TestOpenDBTightensPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chmod only moves the read-only bit on Windows; the directory ACL is what governs access there")
	}

	dir := filepath.Join(t.TempDir(), "db")
	// Pre-created with a loose mode, which is what an install from before this
	// looks like: MkdirAll does nothing to a directory that already exists, so
	// only the explicit Chmod repairs it.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pre-create: %v", err)
	}

	dbPath := filepath.Join(dir, "s-ui.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		if err := CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("db directory mode = %#o, want 0700", got)
	}

	// The sidecars hold the same pages as the database itself.
	for _, name := range []string{"s-ui.db", "s-ui.db-wal", "s-ui.db-shm"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue // -wal and -shm only exist while a connection is open
			}
			t.Errorf("stat %s: %v", name, err)
			continue
		}
		if got := info.Mode().Perm(); got&^fs.FileMode(0o600) != 0 {
			t.Errorf("%s mode = %#o, want no bits outside 0600", name, got)
		}
	}
}
