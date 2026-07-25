package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joncrockett/ike/internal/store"
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
	if added["id"].(float64) != 1 || added["quadrant_label"] != "Do" {
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
