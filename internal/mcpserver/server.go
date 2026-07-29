// Package mcpserver exposes ike's task store as an MCP server so AI agents
// can read and manage the Eisenhower matrix.
package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

const quadrantDoc = "Eisenhower quadrant: 1=urgent and important, 2=important but not urgent, " +
	"3=urgent but not important, 4=neither. The numbers are fixed; each quadrant's display name " +
	"is user-customizable, so use the number to classify and quadrant_label only for display."

type taskOut struct {
	ID       int    `json:"id" jsonschema:"task id"`
	Title    string `json:"title"`
	Quadrant int    `json:"quadrant" jsonschema:"quadrant 1-4"`
	Label    string `json:"quadrant_label" jsonschema:"the quadrant's display name, which the user may have customized"`
	Created  string `json:"created_at"`
	Done     string `json:"done_at,omitempty" jsonschema:"completion time, only set for archived tasks"`
}

// Title is deliberately the raw stored title, not DisplayTitle: this output is
// JSON, and encoding/json escapes control characters itself. Label goes through
// Labels.Of, which sanitizes — that is the store's render choke point, not a
// policy chosen here.
func toOut(t task.Task, labels store.Labels) taskOut {
	o := taskOut{
		ID:       t.ID,
		Title:    t.Title,
		Quadrant: int(t.Quadrant),
		Label:    labels.Of(t.Quadrant),
		Created:  t.CreatedAt.Format(time.RFC3339),
	}
	if t.DoneAt != nil {
		o.Done = t.DoneAt.Format(time.RFC3339)
	}
	return o
}

func toOuts(ts []task.Task, labels store.Labels) []taskOut {
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = toOut(t, labels)
	}
	return out
}

type listIn struct {
	Quadrant int `json:"quadrant,omitempty" jsonschema:"optional filter, 1-4; omit or 0 for all quadrants"`
}

type addIn struct {
	Title    string `json:"title" jsonschema:"the task title"`
	Quadrant int    `json:"quadrant" jsonschema:"required quadrant 1-4; pick based on urgency and importance"`
}

type idIn struct {
	ID int `json:"id" jsonschema:"the task id"`
}

type moveIn struct {
	ID       int `json:"id" jsonschema:"the task id"`
	Quadrant int `json:"quadrant" jsonschema:"destination quadrant 1-4"`
}

type updateIn struct {
	ID    int    `json:"id" jsonschema:"the task id"`
	Title string `json:"title" jsonschema:"the new task title"`
}

type reorderIn struct {
	ID        int    `json:"id" jsonschema:"the task id"`
	Direction string `json:"direction" jsonschema:"where to move it within its quadrant: up, down, top, or bottom"`
}

type tasksOut struct {
	Tasks []taskOut `json:"tasks"`
}

type undoOut struct {
	Undone string `json:"undone,omitempty" jsonschema:"description of the change that was reverted"`
	Redone string `json:"redone,omitempty" jsonschema:"description of the change that was re-applied"`
}

type labelIn struct {
	Quadrant int    `json:"quadrant" jsonschema:"the quadrant to rename, 1-4"`
	Label    string `json:"label" jsonschema:"the new display name; pass an empty string to restore the built-in default"`
}

type quadrantOut struct {
	Quadrant int    `json:"quadrant"`
	Label    string `json:"quadrant_label" jsonschema:"the quadrant's display name after the change"`
}

type labelsOut struct {
	Quadrants []quadrantOut `json:"quadrants"`
}

type emptyIn struct{}

// directionDelta maps a reorder_task direction onto a Reorder delta.
func directionDelta(dir string) (int, error) {
	switch dir {
	case "up":
		return -1, nil
	case "down":
		return 1, nil
	case "top":
		return store.ToTop, nil
	case "bottom":
		return store.ToBottom, nil
	}
	return 0, fmt.Errorf("invalid direction %q: want up, down, top, or bottom", dir)
}

// taskTool registers a tool whose result is a single task. Eight of ike's
// thirteen tools have that shape and had the same five lines of error plumbing
// copied into each; op is the only part that differed.
//
// The store operation returns the post-mutation data alongside the task, so the
// quadrant label rendered here comes from the state the change produced — not
// from a second read that could observe a later one.
func taskTool[In any](srv *mcp.Server, spec *mcp.Tool, op func(In) (task.Task, store.Data, error)) {
	mcp.AddTool(srv, spec, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, taskOut, error) {
		t, d, err := op(in)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, d.Labels), nil
	})
}

// listTool registers a tool that returns a list of tasks drawn from one read of
// the store, so the tasks and the labels describing them agree.
func listTool[In any](srv *mcp.Server, s *store.Store, spec *mcp.Tool, sel func(store.Data, In) []task.Task) {
	mcp.AddTool(srv, spec, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, tasksOut, error) {
		d, err := s.Load()
		if err != nil {
			return nil, tasksOut{}, err
		}
		return nil, tasksOut{Tasks: toOuts(sel(d, in), d.Labels)}, nil
	})
}

// NewServer builds the MCP server around s. version is the ike build version.
func NewServer(s *store.Store, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "ike", Version: version}, nil)

	listTool(srv, s, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List active tasks in the Eisenhower matrix, ordered by quadrant. " + quadrantDoc,
	}, func(d store.Data, in listIn) []task.Task { return d.List(task.Quadrant(in.Quadrant)) })

	listTool(srv, s, &mcp.Tool{
		Name:        "list_archive",
		Description: "List completed (archived) tasks, most recently completed first.",
	}, func(d store.Data, in emptyIn) []task.Task { return d.ListArchive() })

	taskTool(srv, &mcp.Tool{
		Name:        "add_task",
		Description: "Add a task to the matrix. " + quadrantDoc,
	}, func(in addIn) (task.Task, store.Data, error) {
		return s.Add(in.Title, task.Quadrant(in.Quadrant))
	})

	taskTool(srv, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task done. It moves from the active matrix to the archive.",
	}, func(in idIn) (task.Task, store.Data, error) { return s.Complete(in.ID) })

	taskTool(srv, &mcp.Tool{
		Name:        "move_task",
		Description: "Move a task to a different quadrant. " + quadrantDoc,
	}, func(in moveIn) (task.Task, store.Data, error) {
		return s.Move(in.ID, task.Quadrant(in.Quadrant))
	})

	taskTool(srv, &mcp.Tool{
		Name:        "update_task",
		Description: "Rename a task (change its title).",
	}, func(in updateIn) (task.Task, store.Data, error) { return s.Rename(in.ID, in.Title) })

	taskTool(srv, &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete an active task permanently. Unlike complete_task, the task is NOT archived. Use for tasks that should never have existed; prefer complete_task for finished work.",
	}, func(in idIn) (task.Task, store.Data, error) { return s.Delete(in.ID) })

	taskTool(srv, &mcp.Tool{
		Name:        "restore_task",
		Description: "Un-archive a completed task: it returns to the active matrix in the quadrant it was completed in, at the bottom, with its completion time cleared.",
	}, func(in idIn) (task.Task, store.Data, error) { return s.Restore(in.ID) })

	taskTool(srv, &mcp.Tool{
		Name:        "reorder_task",
		Description: "Change a task's position within its own quadrant. This does not change which quadrant it is in — use move_task for that.",
	}, func(in reorderIn) (task.Task, store.Data, error) {
		delta, err := directionDelta(in.Direction)
		if err != nil {
			return task.Task{}, store.Data{}, err
		}
		return s.Reorder(in.ID, delta)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "undo",
		Description: "Revert the single most recent change to the matrix, whichever frontend made it. Returns a description of what was undone. Call repeatedly to walk further back, and use redo to re-apply.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, undoOut, error) {
		label, _, err := s.Undo()
		if err != nil {
			return nil, undoOut{}, err
		}
		return nil, undoOut{Undone: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "redo",
		Description: "Re-apply the most recently undone change. Only available until the next change to the matrix, which discards the redo history — so making any edit after an undo permanently gives up the redo.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, undoOut, error) {
		label, _, err := s.Redo()
		if err != nil {
			return nil, undoOut{}, err
		}
		return nil, undoOut{Redone: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_quadrants",
		Description: "List the four quadrants and their current display names. " +
			"Useful before renaming one, or to show the user their own wording. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, labelsOut, error) {
		labels, err := s.QuadrantLabels()
		if err != nil {
			return nil, labelsOut{}, err
		}
		out := labelsOut{}
		for q := task.Do; q <= task.Eliminate; q++ {
			out.Quadrants = append(out.Quadrants, quadrantOut{Quadrant: int(q), Label: labels.Of(q)})
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "set_quadrant_label",
		Description: "Rename a quadrant's heading. This changes only the display name — it does not " +
			"move tasks or change what the quadrant means. Pass an empty label to restore the default.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in labelIn) (*mcp.CallToolResult, quadrantOut, error) {
		label, _, err := s.SetQuadrantLabel(task.Quadrant(in.Quadrant), in.Label)
		if err != nil {
			return nil, quadrantOut{}, err
		}
		return nil, quadrantOut{Quadrant: in.Quadrant, Label: label}, nil
	})

	return srv
}

// Run serves MCP over stdio until the context ends or the client disconnects.
func Run(ctx context.Context, s *store.Store, version string) error {
	return NewServer(s, version).Run(ctx, &mcp.StdioTransport{})
}
