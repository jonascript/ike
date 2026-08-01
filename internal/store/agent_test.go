package store

import (
	"path/filepath"
	"testing"

	"github.com/jonascript/ike/internal/task"
)

func TestAgentDisabledByDefault(t *testing.T) {
	s := testStore(t)
	on, err := s.AgentEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Error("a fresh matrix should have delegation off")
	}
	// Ordinary writes must not switch it on by side effect.
	s.Add("a task", task.Do)
	if on, _ := s.AgentEnabled(); on {
		t.Error("delegation turned on by an unrelated write")
	}
}

func TestSetAgentEnabledPersistsAndReportsChange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")

	changed, err := OpenAt(p).SetAgentEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("enabling from the default should report a change")
	}
	// A second process reading the same file sees it.
	if on, _ := OpenAt(p).AgentEnabled(); !on {
		t.Error("the setting did not persist")
	}
	// Setting it again is not a change.
	if changed, _ := OpenAt(p).SetAgentEnabled(true); changed {
		t.Error("enabling an already-enabled file should report no change")
	}
	if changed, _ := OpenAt(p).SetAgentEnabled(false); !changed {
		t.Error("disabling should report a change")
	}
}

// The two gates are separate decisions with different blast radii: letting an
// agent edit the task list is not letting ike start a process that edits files.
// Neither may turn the other on.
func TestTheTwoGatesAreIndependent(t *testing.T) {
	s := testStore(t)

	if _, err := s.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.AgentEnabled(); on {
		t.Error("enabling MCP access also enabled delegation")
	}

	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.AgentEnabled(); !on {
		t.Error("revoking MCP access also revoked delegation")
	}
}

// The twin of TestUndoCannotReopenRevokedAccess. AgentEnabled is deliberately
// not in Snapshot, so no sequence of undo or redo can hand back a permission
// the owner closed.
func TestUndoCannotReopenRevokedDelegation(t *testing.T) {
	s := testStore(t)
	if _, err := s.SetAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	a, _, _ := s.Add("a task", task.Do)
	s.SetPlan(a.ID, "a plan")
	s.SetDir(a.ID, "--dir", t.TempDir())
	if _, err := s.SetAgentEnabled(false); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if _, _, err := s.Undo(); err != nil {
			break
		}
		if on, _ := s.AgentEnabled(); on {
			t.Fatalf("undo %d re-enabled revoked delegation", i)
		}
	}
	for i := 0; i < 5; i++ {
		if _, _, err := s.Redo(); err != nil {
			break
		}
		if on, _ := s.AgentEnabled(); on {
			t.Fatalf("redo %d re-enabled revoked delegation", i)
		}
	}
}

// Consent is a property of the file, so the setting must be reachable even when
// the current space is missing or --space names something that is not there —
// the same guarantee SetMCPEnabled has.
func TestAgentGateWorksWithoutAResolvableSpace(t *testing.T) {
	s := testStore(t)
	s.Add("a task", task.Do)

	missing := s.InSpace("no-such-space")
	if _, err := missing.Load(); err == nil {
		t.Fatal("expected the missing space to fail a normal read")
	}
	if _, err := missing.SetAgentEnabled(true); err != nil {
		t.Errorf("SetAgentEnabled should not need a resolvable space: %v", err)
	}
	if on, err := missing.AgentEnabled(); err != nil || !on {
		t.Errorf("AgentEnabled() = %v, %v; should not need a resolvable space", on, err)
	}
}
