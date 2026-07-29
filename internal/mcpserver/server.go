// Package mcpserver exposes ike's task store as an MCP server so AI agents
// can read and manage the Eisenhower matrix.
package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

const quadrantDoc = "Eisenhower quadrant: 1=urgent and important, 2=important but not urgent, " +
	"3=urgent but not important, 4=neither. The numbers are fixed; each quadrant's display name " +
	"is user-customizable, so use the number to classify and quadrant_label only for display."

const spaceDoc = " Optionally set space to work in a space other than the user's current one; " +
	"omit it to use whichever space they are on. It must already exist — call list_spaces to see " +
	"them. There is no tool for creating, renaming, deleting, or switching spaces: those are the " +
	"user's to make."

// spaceIn is embedded in every tool's input, giving them all the same optional
// space argument. It is embedded by value: a pointer left nil by a request that
// omits space would panic in spaceName, and a panic inside a handler surfaces
// as a protocol error rather than the tool error a bad argument deserves.
type spaceIn struct {
	Space string `json:"space,omitempty" jsonschema:"optional space to act on; omit for the user's current space"`
}

func (s spaceIn) spaceName() string { return s.Space }

// spaceArg is what taskTool and listTool need of an input: which space it names.
type spaceArg interface{ spaceName() string }

type taskOut struct {
	ID       int    `json:"id" jsonschema:"task id"`
	Title    string `json:"title"`
	Quadrant int    `json:"quadrant" jsonschema:"quadrant 1-4"`
	Label    string `json:"quadrant_label" jsonschema:"the quadrant's display name, which the user may have customized"`
	Space    string `json:"space" jsonschema:"the space this task lives in"`
	Created  string `json:"created_at"`
	Done     string `json:"done_at,omitempty" jsonschema:"completion time, only set for archived tasks"`
}

// Title is deliberately the raw stored title, not DisplayTitle: this output is
// JSON, and encoding/json escapes control characters itself. Label goes through
// Labels.Of, which sanitizes — that is the store's render choke point, not a
// policy chosen here.
//
// Both the label and the space name come from the Data the operation returned,
// so they describe the state that operation produced rather than a later one.
// Reporting the space matters most when it was defaulted: the agent then learns
// which matrix it just wrote to without having to ask.
func toOut(t task.Task, d store.Data) taskOut {
	o := taskOut{
		ID:       t.ID,
		Title:    t.Title,
		Quadrant: int(t.Quadrant),
		Label:    d.Labels.Of(t.Quadrant),
		Space:    task.SanitizeDisplay(d.Space),
		Created:  t.CreatedAt.Format(time.RFC3339),
	}
	if t.DoneAt != nil {
		o.Done = t.DoneAt.Format(time.RFC3339)
	}
	return o
}

func toOuts(ts []task.Task, d store.Data) []taskOut {
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = toOut(t, d)
	}
	return out
}

type listIn struct {
	spaceIn
	Quadrant int `json:"quadrant,omitempty" jsonschema:"optional filter, 1-4; omit or 0 for all quadrants"`
}

type addIn struct {
	spaceIn
	Title    string `json:"title" jsonschema:"the task title"`
	Quadrant int    `json:"quadrant" jsonschema:"required quadrant 1-4; pick based on urgency and importance"`
}

type idIn struct {
	spaceIn
	ID int `json:"id" jsonschema:"the task id"`
}

type moveIn struct {
	spaceIn
	ID       int `json:"id" jsonschema:"the task id"`
	Quadrant int `json:"quadrant" jsonschema:"destination quadrant 1-4"`
}

type updateIn struct {
	spaceIn
	ID    int    `json:"id" jsonschema:"the task id"`
	Title string `json:"title" jsonschema:"the new task title"`
}

type reorderIn struct {
	spaceIn
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
	spaceIn
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

// spaceOnlyIn is for tools whose only argument is which space to act on.
type spaceOnlyIn struct {
	spaceIn
}

// emptyIn is for tools that take nothing at all — currently only list_spaces,
// which describes the whole file.
type emptyIn struct{}

type spaceOut struct {
	Name     string `json:"name"`
	Active   int    `json:"active" jsonschema:"number of active tasks"`
	Archived int    `json:"archived" jsonschema:"number of completed tasks in the archive"`
	Current  bool   `json:"current" jsonschema:"whether this is the space the user is currently on"`
}

type spacesOut struct {
	Spaces []spaceOut `json:"spaces"`
}

// spaceStore picks the store a tool call acts on.
//
// A server launched with an explicit space is a hard pin: a request naming a
// different one is refused rather than honored. The user scoped this server to
// one matrix, and an agent reaching outside it would defeat the point of having
// said so.
func spaceStore(s *store.Store, want string) (*store.Store, error) {
	if want == "" {
		return s, nil
	}
	if pin := s.Pinned(); pin != "" && !strings.EqualFold(pin, want) {
		return nil, fmt.Errorf("this server is limited to the %q space and cannot act on %q", pin, want)
	}
	return s.InSpace(want), nil
}

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
//
// op receives the store already resolved to the requested space, so the space
// argument is applied in one place rather than being re-read in each of the
// eight closures — where forgetting it once would silently write to the wrong
// matrix.
func taskTool[In spaceArg](srv *mcp.Server, s *store.Store, spec *mcp.Tool, op func(*store.Store, In) (task.Task, store.Data, error)) {
	mcp.AddTool(srv, spec, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, taskOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, taskOut{}, err
		}
		t, d, err := op(sp, in)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t, d), nil
	})
}

// listTool registers a tool that returns a list of tasks drawn from one read of
// the store, so the tasks and the labels describing them agree.
func listTool[In spaceArg](srv *mcp.Server, s *store.Store, spec *mcp.Tool, sel func(store.Data, In) []task.Task) {
	mcp.AddTool(srv, spec, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, tasksOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, tasksOut{}, err
		}
		d, err := sp.Load()
		if err != nil {
			return nil, tasksOut{}, err
		}
		return nil, tasksOut{Tasks: toOuts(sel(d, in), d)}, nil
	})
}

// NewServer builds the MCP server around s. version is the ike build version.
func NewServer(s *store.Store, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "ike", Version: version}, nil)

	listTool(srv, s, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List active tasks in the Eisenhower matrix, ordered by quadrant. " + quadrantDoc + spaceDoc,
	}, func(d store.Data, in listIn) []task.Task { return d.List(task.Quadrant(in.Quadrant)) })

	listTool(srv, s, &mcp.Tool{
		Name:        "list_archive",
		Description: "List completed (archived) tasks, most recently completed first." + spaceDoc,
	}, func(d store.Data, in spaceOnlyIn) []task.Task { return d.ListArchive() })

	taskTool(srv, s, &mcp.Tool{
		Name:        "add_task",
		Description: "Add a task to the matrix. " + quadrantDoc + spaceDoc,
	}, func(sp *store.Store, in addIn) (task.Task, store.Data, error) {
		return sp.Add(in.Title, task.Quadrant(in.Quadrant))
	})

	taskTool(srv, s, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task done. It moves from the active matrix to the archive." + spaceDoc,
	}, func(sp *store.Store, in idIn) (task.Task, store.Data, error) { return sp.Complete(in.ID) })

	taskTool(srv, s, &mcp.Tool{
		Name:        "move_task",
		Description: "Move a task to a different quadrant. " + quadrantDoc + spaceDoc,
	}, func(sp *store.Store, in moveIn) (task.Task, store.Data, error) {
		return sp.Move(in.ID, task.Quadrant(in.Quadrant))
	})

	taskTool(srv, s, &mcp.Tool{
		Name:        "update_task",
		Description: "Rename a task (change its title)." + spaceDoc,
	}, func(sp *store.Store, in updateIn) (task.Task, store.Data, error) { return sp.Rename(in.ID, in.Title) })

	taskTool(srv, s, &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete an active task permanently. Unlike complete_task, the task is NOT archived. Use for tasks that should never have existed; prefer complete_task for finished work." + spaceDoc,
	}, func(sp *store.Store, in idIn) (task.Task, store.Data, error) { return sp.Delete(in.ID) })

	taskTool(srv, s, &mcp.Tool{
		Name:        "restore_task",
		Description: "Un-archive a completed task: it returns to the active matrix in the quadrant it was completed in, at the bottom, with its completion time cleared." + spaceDoc,
	}, func(sp *store.Store, in idIn) (task.Task, store.Data, error) { return sp.Restore(in.ID) })

	taskTool(srv, s, &mcp.Tool{
		Name:        "reorder_task",
		Description: "Change a task's position within its own quadrant. This does not change which quadrant it is in — use move_task for that." + spaceDoc,
	}, func(sp *store.Store, in reorderIn) (task.Task, store.Data, error) {
		delta, err := directionDelta(in.Direction)
		if err != nil {
			return task.Task{}, store.Data{}, err
		}
		return sp.Reorder(in.ID, delta)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "undo",
		Description: "Revert the single most recent change to this space, whichever frontend made it. Returns a description of what was undone. Call repeatedly to walk further back, and use redo to re-apply. History is per space, so this never reverts a change made in a different one." + spaceDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in spaceOnlyIn) (*mcp.CallToolResult, undoOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, undoOut{}, err
		}
		label, _, err := sp.Undo()
		if err != nil {
			return nil, undoOut{}, err
		}
		return nil, undoOut{Undone: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "redo",
		Description: "Re-apply the most recently undone change in this space. Only available until the next change to that space, which discards the redo history — so making any edit after an undo permanently gives up the redo." + spaceDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in spaceOnlyIn) (*mcp.CallToolResult, undoOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, undoOut{}, err
		}
		label, _, err := sp.Redo()
		if err != nil {
			return nil, undoOut{}, err
		}
		return nil, undoOut{Redone: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_quadrants",
		Description: "List the four quadrants and their current display names. " +
			"Useful before renaming one, or to show the user their own wording. " + quadrantDoc + spaceDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in spaceOnlyIn) (*mcp.CallToolResult, labelsOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, labelsOut{}, err
		}
		labels, err := sp.QuadrantLabels()
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
			"move tasks or change what the quadrant means. Pass an empty label to restore the default." + spaceDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in labelIn) (*mcp.CallToolResult, quadrantOut, error) {
		sp, err := spaceStore(s, in.spaceName())
		if err != nil {
			return nil, quadrantOut{}, err
		}
		label, _, err := sp.SetQuadrantLabel(task.Quadrant(in.Quadrant), in.Label)
		if err != nil {
			return nil, quadrantOut{}, err
		}
		return nil, quadrantOut{Quadrant: in.Quadrant, Label: label}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_spaces",
		Description: "List the spaces in the user's data file. A space is an independent matrix " +
			"with its own tasks, archive, quadrant names, and undo history — for example separate " +
			"work and personal matrices. Every other tool acts on the user's current space unless " +
			"given a space argument. This is read-only: spaces are created, renamed, deleted, and " +
			"switched by the user, never by a tool.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, spacesOut, error) {
		// Through the gated store, so a revoked client learns nothing about the
		// file — not even how many matrices it holds or what they are called.
		spaces, err := s.ListSpaces()
		if err != nil {
			return nil, spacesOut{}, err
		}
		out := spacesOut{Spaces: make([]spaceOut, 0, len(spaces))}
		for _, sp := range spaces {
			// A server pinned to one space reports only that one: the pin is
			// meant to scope what the agent can see, not just what it can write.
			if pin := s.Pinned(); pin != "" && !strings.EqualFold(pin, sp.Name) {
				continue
			}
			out.Spaces = append(out.Spaces, spaceOut{
				Name:     task.SanitizeDisplay(sp.Name),
				Active:   sp.Active,
				Archived: sp.Archived,
				Current:  sp.Current,
			})
		}
		return nil, out, nil
	})

	return srv
}

// Run serves MCP over stdio until the context ends or the client disconnects.
func Run(ctx context.Context, s *store.Store, version string) error {
	return NewServer(s, version).Run(ctx, &mcp.StdioTransport{})
}
