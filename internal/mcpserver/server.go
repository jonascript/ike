// Package mcpserver exposes ike's task store as an MCP server so AI agents
// can read and manage the Eisenhower matrix.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joncrockett/ike/internal/store"
	"github.com/joncrockett/ike/internal/task"
)

const quadrantDoc = "Eisenhower quadrant: 1=Do (urgent+important), 2=Schedule (important, not urgent), 3=Delegate (urgent, not important), 4=Eliminate (neither)"

type taskOut struct {
	ID       int    `json:"id" jsonschema:"task id"`
	Title    string `json:"title"`
	Quadrant int    `json:"quadrant" jsonschema:"quadrant 1-4"`
	Label    string `json:"quadrant_label" jsonschema:"quadrant action name (Do/Schedule/Delegate/Eliminate)"`
	Created  string `json:"created_at"`
	Done     string `json:"done_at,omitempty" jsonschema:"completion time, only set for archived tasks"`
}

func toOut(t task.Task) taskOut {
	o := taskOut{
		ID:       t.ID,
		Title:    t.Title,
		Quadrant: int(t.Quadrant),
		Label:    t.Quadrant.Label(),
		Created:  t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.DoneAt != nil {
		o.Done = t.DoneAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return o
}

func toOuts(ts []task.Task) []taskOut {
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = toOut(t)
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

type tasksOut struct {
	Tasks []taskOut `json:"tasks"`
}

type emptyIn struct{}

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
		return nil, tasksOut{Tasks: toOuts(ts)}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_task",
		Description: "Add a task to the matrix. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Add(in.Title, task.Quadrant(in.Quadrant))
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task done. It moves from the active matrix to the archive.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Complete(in.ID)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "move_task",
		Description: "Move a task to a different quadrant. " + quadrantDoc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Move(in.ID, task.Quadrant(in.Quadrant))
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_task",
		Description: "Rename a task (change its title).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Rename(in.ID, in.Title)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete an active task permanently. Unlike complete_task, the task is NOT archived. Use for tasks that should never have existed; prefer complete_task for finished work.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, taskOut, error) {
		t, err := s.Delete(in.ID)
		if err != nil {
			return nil, taskOut{}, err
		}
		return nil, toOut(t), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_archive",
		Description: "List completed (archived) tasks, most recently completed first.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in emptyIn) (*mcp.CallToolResult, tasksOut, error) {
		ts, err := s.ListArchive()
		if err != nil {
			return nil, tasksOut{}, err
		}
		return nil, tasksOut{Tasks: toOuts(ts)}, nil
	})

	return srv
}

// Run serves MCP over stdio until the context ends or the client disconnects.
func Run(ctx context.Context, s *store.Store, version string) error {
	return NewServer(s, version).Run(ctx, &mcp.StdioTransport{})
}
