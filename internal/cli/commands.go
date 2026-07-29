package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

func parseID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid task id %q", s)
	}
	return id, nil
}

func parseQuadrant(s string) (task.Quadrant, error) {
	q, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid quadrant %q", s)
	}
	return task.Quadrant(q), nil
}

// inSpace names the space a command acted on, but only when --space was given.
//
// Without the flag the answer is always "the current space", which the user
// just chose and does not need repeating. With it, the write went somewhere
// other than where a bare `ike list` looks, and saying so is the difference
// between capturing a task and losing track of it. Silent either way was the
// alternative, and it makes `ike -s mistyped done 3` indistinguishable from the
// command you meant.
func inSpace(cmd *cobra.Command, d store.Data) string {
	if !spaceFlagged(cmd) {
		return ""
	}
	return " in " + task.SanitizeDisplay(d.Space)
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printGrouped(w io.Writer, tasks []task.Task, labels store.Labels) {
	byQ := map[task.Quadrant][]task.Task{}
	for _, t := range tasks {
		byQ[t.Quadrant] = append(byQ[t.Quadrant], t)
	}
	for q := task.Do; q <= task.Eliminate; q++ {
		if len(byQ[q]) == 0 {
			continue
		}
		fmt.Fprintf(w, "%d · %s\n", q, labels.Of(q))
		for _, t := range byQ[q] {
			fmt.Fprintf(w, "  %3d  %s\n", t.ID, t.DisplayTitle())
		}
	}
}

func newAddCmd(open opener) *cobra.Command {
	var quadrant int
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a task",
		Args:  cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			t, d, err := s.Add(args[0], task.Quadrant(quadrant))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %d [%s] %s%s\n",
				t.ID, d.Labels.Of(t.Quadrant), t.DisplayTitle(), inSpace(cmd, d))
			return nil
		}),
	}
	cmd.Flags().IntVarP(&quadrant, "quadrant", "q", 2,
		"quadrant: 1=urgent+important, 2=important, 3=urgent, 4=neither")
	return cmd
}

func newListCmd(open opener) *cobra.Command {
	var quadrant int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active tasks",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			d, err := s.Load()
			if err != nil {
				return err
			}
			tasks := d.List(task.Quadrant(quadrant))
			out := cmd.OutOrStdout()
			if asJSON {
				return printJSON(out, tasks)
			}
			if len(tasks) == 0 {
				fmt.Fprintln(out, "no active tasks")
				return nil
			}
			printGrouped(out, tasks, d.QuadrantLabels())
			return nil
		}),
	}
	cmd.Flags().IntVarP(&quadrant, "quadrant", "q", 0, "filter to quadrant 1-4 (0 = all)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newDoneCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>",
		Short: "Complete a task (moves it to the archive)",
		Args:  cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			t, d, err := s.Complete(id)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "done %d  %s%s\n", t.ID, t.DisplayTitle(), inSpace(cmd, d))
			return nil
		}),
	}
}

func newMvCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "mv <id> <quadrant>",
		Short: "Move a task to another quadrant",
		Args:  cobra.ExactArgs(2),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			q, err := parseQuadrant(args[1])
			if err != nil {
				return err
			}
			t, d, err := s.Move(id, q)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "moved %d to %d · %s%s\n",
				t.ID, t.Quadrant, d.Labels.Of(t.Quadrant), inSpace(cmd, d))
			return nil
		}),
	}
}

func newRmCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a task permanently (does not archive)",
		Args:  cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			t, d, err := s.Delete(id)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %d  %s%s\n", t.ID, t.DisplayTitle(), inSpace(cmd, d))
			return nil
		}),
	}
}

func newRestoreCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: "Un-archive a completed task, putting it back in its quadrant",
		Args:  cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			t, d, err := s.Restore(id)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restored %d to %d · %s  %s%s\n",
				t.ID, t.Quadrant, d.Labels.Of(t.Quadrant), t.DisplayTitle(), inSpace(cmd, d))
			return nil
		}),
	}
}

func newReorderCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:       "reorder <id> <up|down|top|bottom>",
		Short:     "Move a task up or down within its quadrant",
		Args:      cobra.ExactArgs(2),
		ValidArgs: []string{"up", "down", "top", "bottom"},
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			var delta int
			var where string
			switch args[1] {
			case "up":
				delta, where = -1, "up in"
			case "down":
				delta, where = 1, "down in"
			case "top":
				delta, where = store.ToTop, "to the top of"
			case "bottom":
				delta, where = store.ToBottom, "to the bottom of"
			default:
				return fmt.Errorf("invalid direction %q: want up, down, top, or bottom", args[1])
			}
			t, d, err := s.Reorder(id, delta)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "moved %d %s %d · %s%s\n",
				t.ID, where, t.Quadrant, d.Labels.Of(t.Quadrant), inSpace(cmd, d))
			return nil
		}),
	}
}

func newUndoCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Undo the last change to this space, from any frontend (TUI, CLI, or MCP)",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			label, d, err := s.Undo()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "undid %s%s\n", label, inSpace(cmd, d))
			return nil
		}),
	}
}

func newRedoCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "redo",
		Short: "Re-apply the last change undone in this space (until the next change discards it)",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			label, d, err := s.Redo()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "redid %s%s\n", label, inSpace(cmd, d))
			return nil
		}),
	}
}

func newLabelCmd(open opener) *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "label [quadrant] [name]",
		Short: "Show or change the quadrant headings",
		Long: "With no arguments, print the four quadrant headings.\n" +
			"With a quadrant and a name, rename that quadrant.\n" +
			"Pass --reset (or an empty name) to restore the built-in default.",
		Args: cobra.MaximumNArgs(2),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if len(args) == 0 {
				return printLabels(cmd, s, reset)
			}
			q, err := parseQuadrant(args[0])
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			if reset && name != "" {
				return fmt.Errorf("pass a name or --reset, not both")
			}
			if !reset && len(args) < 2 {
				return fmt.Errorf("give a name to set, or --reset to restore the default")
			}
			result, d, err := s.SetQuadrantLabel(q, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "quadrant %d is now %s%s\n", q, result, inSpace(cmd, d))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "restore the built-in default name")
	return cmd
}

// printLabels handles `ike label` with no arguments.
func printLabels(cmd *cobra.Command, s *store.Store, reset bool) error {
	if reset {
		return fmt.Errorf("--reset needs a quadrant: ike label <1-4> --reset")
	}
	labels, err := s.QuadrantLabels()
	if err != nil {
		return err
	}
	for q := task.Do; q <= task.Eliminate; q++ {
		marker := ""
		if labels.IsCustom(q) {
			marker = fmt.Sprintf("  (default: %s)", q.Label())
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d · %s%s\n", q, labels.Of(q), marker)
	}
	return nil
}

func newArchiveCmd(open opener) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "List completed tasks, newest first",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			arch, err := s.ListArchive()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return printJSON(out, arch)
			}
			if len(arch) == 0 {
				fmt.Fprintln(out, "archive is empty")
				return nil
			}
			for _, t := range arch {
				fmt.Fprintln(out, t.ArchiveRow(t.DisplayTitle()))
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}
