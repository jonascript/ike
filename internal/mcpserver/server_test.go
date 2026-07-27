package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// connect wires an in-process client to the server and returns a call helper.
func connect(t *testing.T) (*mcp.ClientSession, *store.Store) {
	t.Helper()
	s := store.OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
	srv := NewServer(s, "test")

	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	sess, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess, s
}

func call(t *testing.T, sess *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestToolsAreRegistered(t *testing.T) {
	sess, _ := connect(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"list_tasks": false, "add_task": false, "complete_task": false,
		"move_task": false, "update_task": false, "delete_task": false, "list_archive": false,
		"restore_task": false, "reorder_task": false, "undo": false, "redo": false,
		"list_quadrants": false, "set_quadrant_label": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestAddCompleteArchiveFlow(t *testing.T) {
	sess, s := connect(t)

	res := call(t, sess, "add_task", map[string]any{"title": "ship it", "quadrant": 1})
	if res.IsError {
		t.Fatalf("add_task errored: %+v", res.Content)
	}
	added := structured(t, res)
	if added["id"].(float64) != 1 || added["quadrant_label"] != "Do It First" {
		t.Errorf("add_task output = %v", added)
	}

	// The write must be visible on disk to other frontends.
	tasks, _ := s.List(0)
	if len(tasks) != 1 || tasks[0].Title != "ship it" {
		t.Errorf("store after add = %+v", tasks)
	}

	res = call(t, sess, "list_tasks", map[string]any{})
	listed := structured(t, res)
	if n := len(listed["tasks"].([]any)); n != 1 {
		t.Errorf("list_tasks returned %d tasks, want 1", n)
	}

	res = call(t, sess, "complete_task", map[string]any{"id": 1})
	done := structured(t, res)
	if done["done_at"] == nil || done["done_at"] == "" {
		t.Errorf("complete_task output missing done_at: %v", done)
	}

	res = call(t, sess, "list_archive", map[string]any{})
	arch := structured(t, res)
	if n := len(arch["tasks"].([]any)); n != 1 {
		t.Errorf("list_archive returned %d tasks, want 1", n)
	}
	if n, _ := s.List(0); len(n) != 0 {
		t.Error("completed task still active in store")
	}
}

func TestMoveUpdateDelete(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "juggle", "quadrant": 2})

	res := call(t, sess, "move_task", map[string]any{"id": 1, "quadrant": 4})
	if structured(t, res)["quadrant"].(float64) != 4 {
		t.Error("move_task did not change quadrant")
	}

	res = call(t, sess, "update_task", map[string]any{"id": 1, "title": "renamed"})
	if structured(t, res)["title"] != "renamed" {
		t.Error("update_task did not rename")
	}

	call(t, sess, "delete_task", map[string]any{"id": 1})
	tasks, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(tasks) != 0 || len(arch) != 0 {
		t.Error("delete_task should remove permanently without archiving")
	}
}

func TestRestoreTask(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "premature", "quadrant": 3})
	call(t, sess, "complete_task", map[string]any{"id": 1})

	res := call(t, sess, "restore_task", map[string]any{"id": 1})
	if res.IsError {
		t.Fatalf("restore_task errored: %+v", res.Content)
	}
	out := structured(t, res)
	if out["done_at"] != nil && out["done_at"] != "" {
		t.Errorf("restored task still has done_at: %v", out)
	}
	if out["quadrant"].(float64) != 3 {
		t.Errorf("restored to quadrant %v, want 3", out["quadrant"])
	}
	tasks, _ := s.List(0)
	arch, _ := s.ListArchive()
	if len(tasks) != 1 || len(arch) != 0 {
		t.Errorf("after restore: active=%d archive=%d, want 1/0", len(tasks), len(arch))
	}
}

func TestReorderTask(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "a", "quadrant": 1})
	call(t, sess, "add_task", map[string]any{"title": "b", "quadrant": 1})
	call(t, sess, "add_task", map[string]any{"title": "c", "quadrant": 1})

	if res := call(t, sess, "reorder_task", map[string]any{"id": 3, "direction": "top"}); res.IsError {
		t.Fatalf("reorder_task errored: %+v", res.Content)
	}
	tasks, _ := s.List(1)
	if len(tasks) != 3 || tasks[0].Title != "c" {
		t.Errorf("order = %+v, want c first", tasks)
	}

	if res := call(t, sess, "reorder_task", map[string]any{"id": 3, "direction": "sideways"}); !res.IsError {
		t.Error("an invalid direction should set IsError")
	}
}

func TestUndoTool(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "keep", "quadrant": 1})
	call(t, sess, "delete_task", map[string]any{"id": 1})

	res := call(t, sess, "undo", map[string]any{})
	if res.IsError {
		t.Fatalf("undo errored: %+v", res.Content)
	}
	if undone := structured(t, res)["undone"]; undone != `delete "keep"` {
		t.Errorf("undone = %v, want the delete label", undone)
	}
	tasks, _ := s.List(0)
	if len(tasks) != 1 || tasks[0].Title != "keep" {
		t.Errorf("undo did not bring the task back: %+v", tasks)
	}

	// Drain the stack, then undoing again must be a tool error.
	for {
		if res := call(t, sess, "undo", map[string]any{}); res.IsError {
			break
		}
	}
}

func TestRedoTool(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "keep", "quadrant": 1})
	call(t, sess, "delete_task", map[string]any{"id": 1})
	call(t, sess, "undo", map[string]any{})

	res := call(t, sess, "redo", map[string]any{})
	if res.IsError {
		t.Fatalf("redo errored: %+v", res.Content)
	}
	if redone := structured(t, res)["redone"]; redone != `delete "keep"` {
		t.Errorf("redone = %v, want the delete label", redone)
	}
	if tasks, _ := s.List(0); len(tasks) != 0 {
		t.Errorf("redo did not re-apply the delete: %+v", tasks)
	}

	// Nothing left to redo.
	if res := call(t, sess, "redo", map[string]any{}); !res.IsError {
		t.Error("redo with an empty stack should set IsError")
	}
}

func TestRedoInvalidatedByAnotherFrontend(t *testing.T) {
	sess, s := connect(t)
	call(t, sess, "add_task", map[string]any{"title": "one", "quadrant": 1})
	call(t, sess, "delete_task", map[string]any{"id": 1})
	call(t, sess, "undo", map[string]any{})

	// A write straight to the store, as the CLI or TUI would make.
	if _, err := s.Add("from another frontend", task.Schedule); err != nil {
		t.Fatal(err)
	}
	if res := call(t, sess, "redo", map[string]any{}); !res.IsError {
		t.Error("a write from another frontend should invalidate redo")
	}
}

func TestQuadrantLabelTools(t *testing.T) {
	sess, s := connect(t)

	res := call(t, sess, "list_quadrants", map[string]any{})
	if res.IsError {
		t.Fatalf("list_quadrants errored: %+v", res.Content)
	}
	quads := structured(t, res)["quadrants"].([]any)
	if len(quads) != 4 {
		t.Fatalf("list_quadrants returned %d quadrants, want 4", len(quads))
	}
	if first := quads[0].(map[string]any); first["quadrant_label"] != "Do It First" {
		t.Errorf("quadrant 1 label = %v, want the default", first["quadrant_label"])
	}

	res = call(t, sess, "set_quadrant_label", map[string]any{"quadrant": 1, "label": "Firefighting"})
	if res.IsError {
		t.Fatalf("set_quadrant_label errored: %+v", res.Content)
	}
	if got := structured(t, res)["quadrant_label"]; got != "Firefighting" {
		t.Errorf("set returned %v, want Firefighting", got)
	}

	// Task output reflects the custom name.
	call(t, sess, "add_task", map[string]any{"title": "put out fire", "quadrant": 1})
	res = call(t, sess, "list_tasks", map[string]any{})
	listed := structured(t, res)["tasks"].([]any)
	if got := listed[0].(map[string]any)["quadrant_label"]; got != "Firefighting" {
		t.Errorf("task quadrant_label = %v, want Firefighting", got)
	}

	// An empty label resets.
	res = call(t, sess, "set_quadrant_label", map[string]any{"quadrant": 1, "label": ""})
	if got := structured(t, res)["quadrant_label"]; got != "Do It First" {
		t.Errorf("reset returned %v, want the default", got)
	}
	labels, _ := s.QuadrantLabels()
	if labels.IsCustom(task.Do) {
		t.Error("quadrant still custom after reset")
	}

	if res := call(t, sess, "set_quadrant_label", map[string]any{"quadrant": 9, "label": "x"}); !res.IsError {
		t.Error("an invalid quadrant should set IsError")
	}
}

func TestToolErrorsAreToolErrors(t *testing.T) {
	sess, _ := connect(t)

	// Domain errors must come back as tool errors, not protocol failures.
	res := call(t, sess, "complete_task", map[string]any{"id": 999})
	if !res.IsError {
		t.Error("complete_task on missing id should set IsError")
	}
	res = call(t, sess, "add_task", map[string]any{"title": "x", "quadrant": 9})
	if !res.IsError {
		t.Error("add_task with bad quadrant should set IsError")
	}
	res = call(t, sess, "add_task", map[string]any{"title": "   ", "quadrant": 1})
	if !res.IsError {
		t.Error("add_task with blank title should set IsError")
	}
}
