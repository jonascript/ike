// Package mcpserver exposes ike's task store as an MCP server so AI agents
// can read and manage the Eisenhower matrix.
package mcpserver

import (
	"context"
	"fmt"

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

func toOut(t task.Task, labels store.Labels) taskOut {
	o := taskOut{
		ID:       t.ID,
		Title:    t.Title,
		Quadrant: int(t.Quadrant),
		Label:    labels.Of(t.Quadrant),
		Created:  t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.DoneAt != nil {
		o.Done = t.DoneAt.Format("2006-01-02T15:04:05Z07:00")
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

// labelsOf resolves the quadrant headings for tool output. A read failure
// falls back to the defaults rather than failing the tool call, since the
// labels are cosmetic.
func labelsOf(s *store.Store) store.Labels {
	l, err := s.QuadrantLabels()
	if err != nil {
		return nil
	}
	return l
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

// NewServer builds the MCP server around s. version is the ike build version.
func NewServer(s *store.Store, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "ike", Version: version}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List active tasks in the Eisenhower matrix, ordered by quadrant. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, tasksOut, error) {
		ts, err := s.List(task.Quadrant(in.Quadrant))
		if err != nil {
			return nil, tasksOut{}, err
		}
		return nil, tasksOut{Tasks: toOuts(ts, labelsOf(s))}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_task",
		Description: "Add a task to the matrix. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Add(in.Title, task.Quadrant(in.Quadrant))
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task done. It moves from the active matrix to the archive.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Complete(in.ID)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "move_task",
		Description: "Move a task to a different quadrant. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Move(in.ID, task.Quadrant(in.Quadrant))
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_task",
		Description: "Rename a task (change its title).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Rename(in.ID, in.Title)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete an active task permanently. Unlike complete_task, the task is NOT archived. Use for tasks that should never have existed; prefer complete_task for finished work.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Delete(in.ID)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "restore_task",
		Description: "Un-archive a completed task: it returns to the active matrix in the quadrant it was completed in, at the bottom, with its completion time cleared.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Restore(in.ID)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reorder_task",
		Description: "Change a task's position within its own quadrant. This does not change which quadrant it is in — use move_task for that.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in reorderIn) (*mcp.CallToolResult, taskOut, error) {
		delta, err := directionDelta(in.Direction)
		if err != nil {
			return nil, taskOut{}, err
		}
		t, err := s.Reorder(in.ID, delta)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, labelsOf(s)), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "undo",
		Description: "Revert the single most recent change to the matrix, whichever frontend made it. Returns a description of what was undone. Call repeatedly to walk further back, and use redo to re-apply.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, undoOut, error) {
		label, err := s.Undo()
		if err != nil {
			return nil, undoOut{}, err
		}
		return nil, undoOut{Undone: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "redo",
		Description: "Re-apply the most recently undone change. Only available until the next change to the matrix, which discards the redo history — so making any edit after an undo permanently gives up the redo.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, undoOut, error) {
		label, err := s.Redo()
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
		label, err := s.SetQuadrantLabel(task.Quadrant(in.Quadrant), in.Label)
		if err != nil {
			return nil, quadrantOut{}, err
		}
		return nil, quadrantOut{Quadrant: in.Quadrant, Label: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_archive",
		Description: "List completed (archived) tasks, most recently completed first.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, tasksOut, error) {
		ts, err := s.ListArchive()
		if err != nil {
			return nil, tasksOut{}, err
		}
		return nil, tasksOut{Tasks: toOuts(ts, labelsOf(s))}, nil
	})

	return srv
}

// Run serves MCP over stdio until the context ends or the client disconnects.
func Run(ctx context.Context, s *store.Store, version string) error {
	return NewServer(s, version).Run(ctx, &mcp.StdioTransport{})
}
