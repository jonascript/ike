package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// connectStore wires an in-process client to a server over a store the caller
// has already set up, so a test can arrange spaces or a pin first.
func connectStore(t *testing.T, s *store.Store) *mcp.ClientSession {
	t.Helper()
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
	return sess
}

// spacesFixture returns a store with a second space, both holding one task.
func spacesFixture(t *testing.T) *store.Store {
	t.Helper()
	s := store.OpenAt(filepath.Join(t.TempDir(), "tasks.json"))
	if _, _, err := s.Add("home task", task.Do); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NewSpace("work"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InSpace("work").Add("work task", task.Schedule); err != nil {
		t.Fatal(err)
	}
	return s
}

func toolNames(t *testing.T, sess *mcp.ClientSession) []string {
	t.Helper()
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func TestListSpacesReportsCountsAndCurrent(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s)

	var got struct {
		Spaces []struct {
			Name     string `json:"name"`
			Active   int    `json:"active"`
			Archived int    `json:"archived"`
			Current  bool   `json:"current"`
		} `json:"spaces"`
	}
	b, err := json.Marshal(call(t, sess, "list_spaces", nil).StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spaces) != 2 {
		t.Fatalf("spaces = %+v, want two", got.Spaces)
	}
	if got.Spaces[0].Name != "default" || !got.Spaces[0].Current || got.Spaces[0].Active != 1 {
		t.Errorf("first = %+v, want default, current, 1 active", got.Spaces[0])
	}
	if got.Spaces[1].Name != "work" || got.Spaces[1].Current {
		t.Errorf("second = %+v, want work and not current", got.Spaces[1])
	}
}

// The gate covers the whole file, so a revoked client must not learn even how
// many matrices there are or what they are called.
func TestListSpacesIsGated(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s.ForMCP())

	res := call(t, sess, "list_spaces", nil)
	if !res.IsError {
		t.Fatal("list_spaces should fail while access is off")
	}
	text := resultText(res)
	if strings.Contains(text, "work") {
		t.Errorf("error %q leaks a space name", text)
	}
}

// Omitting space means the current one; naming a space acts on that one instead.
func TestToolsHonorTheSpaceArgument(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s)

	def := structured(t, call(t, sess, "list_tasks", nil))
	if !strings.Contains(mustJSON(t, def), "home task") || strings.Contains(mustJSON(t, def), "work task") {
		t.Errorf("list_tasks = %v, want only the current space", def)
	}
	work := structured(t, call(t, sess, "list_tasks", map[string]any{"space": "work"}))
	if !strings.Contains(mustJSON(t, work), "work task") || strings.Contains(mustJSON(t, work), "home task") {
		t.Errorf("list_tasks in work = %v, want only the work task", work)
	}

	// A write lands in the named space and reports which one it was.
	added := structured(t, call(t, sess, "add_task", map[string]any{
		"title": "from the agent", "quadrant": 1, "space": "work",
	}))
	if added["space"] != "work" {
		t.Errorf("add_task space = %v, want work", added["space"])
	}
	wd, err := s.InSpace("work").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(wd.Tasks) != 2 {
		t.Errorf("work has %d tasks, want the agent's write to have landed there", len(wd.Tasks))
	}
	if d, err := s.Load(); err != nil || len(d.Tasks) != 1 {
		t.Errorf("default has %d tasks, want it untouched", len(d.Tasks))
	}
}

// Every task-shaped result names its space, so an agent that omitted the
// argument still learns which matrix it touched.
func TestTaskResultsNameTheirSpace(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s)
	out := structured(t, call(t, sess, "add_task", map[string]any{"title": "x", "quadrant": 2}))
	if out["space"] != "default" {
		t.Errorf("space = %v, want the defaulted space reported", out["space"])
	}
}

// Undo is per space, so undoing through a space argument must not touch another.
func TestUndoHonorsTheSpaceArgument(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s)

	if res := call(t, sess, "undo", map[string]any{"space": "work"}); res.IsError {
		t.Fatalf("undo in work: %s", resultText(res))
	}
	wd, err := s.InSpace("work").Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(wd.Tasks) != 0 {
		t.Errorf("work tasks = %d, want the add undone", len(wd.Tasks))
	}
	if d, err := s.Load(); err != nil || len(d.Tasks) != 1 {
		t.Errorf("default tasks = %d, want it untouched", len(d.Tasks))
	}
}

// A space argument selects an existing space; it never creates one. That is
// what keeps "no tool can make a space" true in the presence of a typo.
func TestUnknownSpaceIsAToolErrorAndCreatesNothing(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s)

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"list_tasks", map[string]any{"space": "wrok"}},
		{"add_task", map[string]any{"title": "x", "quadrant": 1, "space": "wrok"}},
		{"complete_task", map[string]any{"id": 1, "space": "wrok"}},
		{"undo", map[string]any{"space": "wrok"}},
		{"set_quadrant_label", map[string]any{"quadrant": 1, "label": "x", "space": "wrok"}},
	} {
		// A domain error must arrive as a tool error, not a protocol error —
		// call() fails the test on the latter.
		res := call(t, sess, c.tool, c.args)
		if !res.IsError {
			t.Errorf("%s with an unknown space should be a tool error", c.tool)
		}
	}
	names, err := s.ListSpaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("spaces = %+v, want the typo not to have created one", names)
	}
}

// Managing spaces is the user's, not the agent's. There must be no tool for it.
func TestNoToolManagesSpaces(t *testing.T) {
	sess, _ := connect(t)
	forbidden := regexp.MustCompile(`space`)
	for _, name := range toolNames(t, sess) {
		if !forbidden.MatchString(name) {
			continue
		}
		if name != "list_spaces" {
			t.Errorf("tool %q manages spaces; only list_spaces should mention them", name)
		}
	}
	for _, name := range []string{
		"new_space", "create_space", "use_space", "switch_space",
		"rename_space", "delete_space", "remove_space", "set_space",
	} {
		res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name})
		if err == nil && !res.IsError {
			t.Errorf("%q should not exist", name)
		}
	}
}

// The space argument is optional everywhere it appears, so an existing client
// that never sends it keeps working.
func TestSpaceArgumentIsOptionalInEverySchema(t *testing.T) {
	sess, _ := connect(t)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		if tl.Name == "list_spaces" {
			continue // describes the whole file; takes no arguments
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal([]byte(mustJSON(t, tl.InputSchema)), &schema); err != nil {
			t.Fatalf("%s schema: %v", tl.Name, err)
		}
		if _, ok := schema.Properties["space"]; !ok {
			t.Errorf("%s has no space property (the embedded struct did not flatten)", tl.Name)
			continue
		}
		for _, req := range schema.Required {
			if req == "space" {
				t.Errorf("%s requires space; it must stay optional", tl.Name)
			}
		}
	}
}

// A server launched for one space is scoped to it: an agent cannot reach
// another matrix by naming it, and cannot see that the others exist.
func TestAPinnedServerCannotReachAnotherSpace(t *testing.T) {
	s := spacesFixture(t)
	sess := connectStore(t, s.InSpace("work"))

	res := call(t, sess, "add_task", map[string]any{
		"title": "escape", "quadrant": 1, "space": "default",
	})
	if !res.IsError {
		t.Fatal("a pinned server should refuse another space")
	}
	if !strings.Contains(resultText(res), "limited to") {
		t.Errorf("error = %q, want it to explain the pin", resultText(res))
	}
	if d, err := s.Load(); err != nil || len(d.Tasks) != 1 {
		t.Errorf("default tasks = %d, want no write to have landed", len(d.Tasks))
	}

	// Omitting the argument still works, and reaches the pinned space.
	out := structured(t, call(t, sess, "add_task", map[string]any{"title": "fine", "quadrant": 1}))
	if out["space"] != "work" {
		t.Errorf("space = %v, want the pinned space", out["space"])
	}

	// And the listing shows only the space it was launched for.
	var got struct {
		Spaces []struct{ Name string } `json:"spaces"`
	}
	b, err := json.Marshal(call(t, sess, "list_spaces", nil).StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spaces) != 1 || got.Spaces[0].Name != "work" {
		t.Errorf("list_spaces = %+v, want only the pinned space", got.Spaces)
	}
}

// resultText joins a tool result's text content, which is where an error
// message lands.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
