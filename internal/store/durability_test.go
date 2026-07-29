package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/jonascript/ike/internal/task"
)

// The matrix is personal, so it must not be readable by other users on the
// machine. The file previously landed as 0644 inside a 0755 directory.
func TestDataFileAndDirArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ike")
	path := filepath.Join(dir, "tasks.json")
	s := OpenAt(path)
	if _, _, err := s.Add("private", task.Do); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != dataFileMode {
		t.Errorf("data file mode = %#o, want %#o", got, dataFileMode)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != dataDirMode {
		t.Errorf("data dir mode = %#o, want %#o", got, dataDirMode)
	}
}

// A file left at 0644 by an older build must be tightened rather than kept
// world-readable forever. The atomic rename replaces the inode, so the new
// file's mode is what survives.
func TestWriteTightensPermissionsOfAnOlderFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := OpenAt(path)
	if _, _, err := s.Add("first", task.Do); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.Add("second", task.Do); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != dataFileMode {
		t.Errorf("after a write, mode = %#o, want %#o", got, dataFileMode)
	}
}

// The temp file must not be a predictable name next to the data file: a
// pre-created symlink at that name would redirect the write.
func TestWriteDoesNotUsePredictableTempName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("do not clobber"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The old implementation wrote here, following the symlink.
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s := OpenAt(path)
	if _, _, err := s.Add("safe", task.Do); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "do not clobber" {
		t.Errorf("the symlink target was overwritten: %q", got)
	}
	// No stray temp files left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tasks-") {
			t.Errorf("temp file %s was not cleaned up", e.Name())
		}
	}
}

// Losing the data file used to mean hand-editing JSON or starting over.
func TestWriteKeepsABackupOfThePreviousContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := OpenAt(path)

	if _, _, err := s.Add("first", task.Do); err != nil {
		t.Fatal(err)
	}
	// No backup yet: there was nothing to preserve before the first write.
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("unexpected backup after the first write: %v", err)
	}

	if _, _, err := s.Add("second", task.Do); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("no backup after the second write: %v", err)
	}
	if !strings.Contains(string(bak), "first") {
		t.Errorf("backup does not hold the previous state: %s", bak)
	}
	if strings.Contains(string(bak), "second") {
		t.Error("backup holds the new state, not the previous one")
	}
	// The backup is as private as the data file.
	fi, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != dataFileMode {
		t.Errorf("backup mode = %#o, want %#o", got, dataFileMode)
	}
}

// Every byte of a completed write must be on disk before the rename, or a
// crash can leave a truncated file. The observable part is that a write which
// reports success leaves a file that parses.
func TestWrittenFileIsCompleteAndParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := OpenAt(path)
	for i := range 25 {
		if _, _, err := s.Add(strings.Repeat("x", i+1), task.Schedule); err != nil {
			t.Fatal(err)
		}
		d, err := s.Load()
		if err != nil {
			t.Fatalf("after write %d the file does not parse: %v", i, err)
		}
		if len(d.Tasks) != i+1 {
			t.Fatalf("after write %d: %d tasks, want %d", i, len(d.Tasks), i+1)
		}
	}
}

func TestCheckOverrideRejectsFootguns(t *testing.T) {
	existing := t.TempDir()
	notADir := filepath.Join(existing, "afile")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	bad := map[string]string{
		"relative":            "tasks.json",
		"relative with dir":   "sub/tasks.json",
		"dot relative":        "./tasks.json",
		"unexpanded tilde":    "~/tasks.json",
		"missing parent":      filepath.Join(existing, "nope", "tasks.json"),
		"parent is not a dir": filepath.Join(notADir, "tasks.json"),
	}
	for name, p := range bad {
		t.Run(name, func(t *testing.T) {
			t.Setenv("IKE_DATA_FILE", p)
			if got, err := DataFile(); err == nil {
				t.Errorf("DataFile() = %q, want an error for %q", got, p)
			}
		})
	}

	t.Run("absolute path in an existing dir is accepted", func(t *testing.T) {
		want := filepath.Join(existing, "tasks.json")
		t.Setenv("IKE_DATA_FILE", want)
		got, err := DataFile()
		if err != nil || got != want {
			t.Errorf("DataFile() = %q, %v; want %q, nil", got, err, want)
		}
	})
}

// A hung lock holder used to make every later `ike` invocation block forever,
// printing nothing — no message, no hint, nothing to diagnose.
func TestMutateTimesOutInsteadOfHangingForever(t *testing.T) {
	old := lockTimeout
	lockTimeout = 150 * time.Millisecond
	defer func() { lockTimeout = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := OpenAt(path)
	if _, _, err := s.Add("seed", task.Do); err != nil {
		t.Fatal(err)
	}

	// Stand in for another ike process that took the lock and then hung.
	held := flock.New(path + ".lock")
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Unlock() }()

	start := time.Now()
	_, _, err := s.Add("blocked", task.Do)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error while the lock was held")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should say it timed out, got: %v", err)
	}
	// The message has to name the file, or there is nothing to act on.
	if !strings.Contains(err.Error(), "tasks.json.lock") {
		t.Errorf("error should name the lock file, got: %v", err)
	}
	if elapsed > 20*lockTimeout {
		t.Errorf("waited %s, expected to give up near %s", elapsed, lockTimeout)
	}

	// Releasing the lock lets the next attempt through.
	if err := held.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Add("after release", task.Do); err != nil {
		t.Errorf("Add after the lock was released: %v", err)
	}
}
