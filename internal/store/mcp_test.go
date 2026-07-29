package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

// titles returns the titles of q's tasks in display order.

func TestMCPDisabledByDefault(t *testing.T) {
	s := testStore(t)
	on, err := s.MCPEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("a fresh matrix should have MCP access off")
	}
	// Adding tasks must not switch it on by side effect.
	s.Add("a task", task.Do)
	if on, _ := s.MCPEnabled(); on {
		t.Error("MCP access turned on by an unrelated write")
	}
}

func TestMCPEnabledPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tasks.json")

	changed, err := OpenAt(p).SetMCPEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("enabling from the default should report a change")
	}
	// A separate Store instance, as a later process would see it.
	if on, _ := OpenAt(p).MCPEnabled(); !on {
		t.Error("setting did not survive a reload")
	}

	changed, err = OpenAt(p).SetMCPEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("enabling twice should report no change")
	}

	if _, err := OpenAt(p).SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := OpenAt(p).MCPEnabled(); on {
		t.Error("disable did not stick")
	}
}

func TestMCPSettingIsNotUndoable(t *testing.T) {
	s := testStore(t)
	s.Add("a task", task.Do)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}

	// Undo must reach past the permission change to the task change, and must
	// not restore the old permission on the way.
	label, _, err := s.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if label != `add "a task"` {
		t.Errorf("undo label = %q; the MCP setting should not occupy an undo slot", label)
	}
	if on, _ := s.MCPEnabled(); !on {
		t.Error("undo switched MCP access back off")
	}
}

// The gate used to be checked once, before serving. A client that stayed
// connected therefore kept full access after `ike mcp disable`, while
// `ike mcp status` reported "off". These pin the data-layer check that closed
// that window.
func TestMCPGateRevokesLiveSession(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}

	// The session a connected client holds.
	live := s.ForMCP()
	if _, _, err := live.Add("while enabled", task.Do); err != nil {
		t.Fatalf("Add with access on: %v", err)
	}
	if _, err := live.Load(); err != nil {
		t.Fatalf("Load with access on: %v", err)
	}

	// Revoked from another process, on the same file, mid-session.
	if _, err := s.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}

	if _, _, err := live.Add("after revoke", task.Do); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Add after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, err := live.Load(); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Load after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, _, err := live.Complete(1); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Complete after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, _, err := live.Delete(1); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Delete after revoke: err = %v, want ErrMCPDisabled", err)
	}
	if _, _, err := live.Undo(); !errors.Is(err, ErrMCPDisabled) {
		t.Errorf("Undo after revoke: err = %v, want ErrMCPDisabled", err)
	}

	// The revoked mutations must not have landed, and the owner's own access is
	// unaffected — that is what `ike mcp status` and the TUI keep using.
	d, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "while enabled" {
		t.Errorf("tasks = %+v, want only the pre-revoke task", d.Tasks)
	}

	// Re-enabling restores the same session, without reconnecting.
	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Load(); err != nil {
		t.Errorf("Load after re-enable: %v", err)
	}
}

// A store that was never marked for MCP use is unaffected by the setting, so
// the TUI and CLI keep working while agent access is off.

// A store that was never marked for MCP use is unaffected by the setting, so
// the TUI and CLI keep working while agent access is off.
func TestGateDoesNotAffectOwnerAccess(t *testing.T) {
	s := testStore(t)
	if enabled, err := s.MCPEnabled(); err != nil || enabled {
		t.Fatalf("MCPEnabled() = %v, %v; want false, nil", enabled, err)
	}
	if _, _, err := s.Add("owner task", task.Do); err != nil {
		t.Errorf("owner Add with MCP off: %v", err)
	}
	if _, err := s.Load(); err != nil {
		t.Errorf("owner Load with MCP off: %v", err)
	}
}

func TestMCPErrorsOmitDataFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	gated := OpenAt(path).ForMCP()

	_, err := gated.Load()
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error discloses the data file's directory to the agent: %v", err)
	}
	// The file name still appears, so the message stays actionable.
	if !strings.Contains(err.Error(), "tasks.json") {
		t.Errorf("error should still name the file: %v", err)
	}

	// The owner's view keeps the full path, which is what makes a CLI error
	// useful.
	if _, err := OpenAt(path).Load(); err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("ungated error should keep the full path: %v", err)
	}
}

// Archive order is defined once, in Data.ListArchive, and both `ike archive`
// and the TUI's archive view render it. The TUI used to reverse the stored
// slice instead, which agrees only while every archived task carries a
// completion stamp — a file written by an older build, or hand-edited, need
// not. This pins the definition that both now share.
