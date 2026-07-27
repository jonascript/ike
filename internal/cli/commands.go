package cli

import (
	"encoding/json"
	"fmt"
	"os"
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

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// quadrantLabels resolves the display names of the four quadrants. On a read
// error it returns nil, which still resolves to the built-in defaults — a
// label lookup should never be the thing that fails a command.
func quadrantLabels(s *store.Store) store.Labels {
	l, err := s.QuadrantLabels()
	if err != nil {
		return nil
	}
	return l
}

func printGrouped(tasks []task.Task, labels store.Labels) {
	byQ := map[task.Quadrant][]task.Task{}
	for _, t := range tasks {
		byQ[t.Quadrant] = append(byQ[t.Quadrant], t)
	}
	for q := task.Do; q <= task.Eliminate; q++ {
		if len(byQ[q]) == 0 {
			continue
		}
		fmt.Printf("%d · %s\n", q, labels.Of(q))
		for _, t := range byQ[q] {
			fmt.Printf("  %3d  %s\n", t.ID, t.Title)
		}
	}
}

func init() {
	var addQuadrant int
	addCmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Add(args[0], task.Quadrant(addQuadrant))
			if err != nil {
				return err
			}
			fmt.Printf("added %d [%s] %s\n", t.ID, quadrantLabels(s).Of(t.Quadrant), t.Title)
			return nil
		},
	}
	addCmd.Flags().IntVarP(&addQuadrant, "quadrant", "q", 2,
		"quadrant: 1=urgent+important, 2=important, 3=urgent, 4=neither")

	var listQuadrant int
	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List active tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			tasks, err := s.List(task.Quadrant(listQuadrant))
			if err != nil {
				return err
			}
			if listJSON {
				return printJSON(tasks)
			}
			if len(tasks) == 0 {
				fmt.Println("no active tasks")
				return nil
			}
			printGrouped(tasks, quadrantLabels(s))
			return nil
		},
	}
	listCmd.Flags().IntVarP(&listQuadrant, "quadrant", "q", 0, "filter to quadrant 1-4 (0 = all)")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "output JSON")

	doneCmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Complete a task (moves it to the archive)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Complete(id)
			if err != nil {
				return err
			}
			fmt.Printf("done %d  %s\n", t.ID, t.Title)
			return nil
		},
	}

	mvCmd := &cobra.Command{
		Use:   "mv <id> <quadrant>",
		Short: "Move a task to another quadrant",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			q, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid quadrant %q", args[1])
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Move(id, task.Quadrant(q))
			if err != nil {
				return err
			}
			fmt.Printf("moved %d to %d · %s\n", t.ID, q, quadrantLabels(s).Of(t.Quadrant))
			return nil
		},
	}

	rmCmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a task permanently (does not archive)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Delete(id)
			if err != nil {
				return err
			}
			fmt.Printf("deleted %d  %s\n", t.ID, t.Title)
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Un-archive a completed task, putting it back in its quadrant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Restore(id)
			if err != nil {
				return err
			}
			fmt.Printf("restored %d to %d · %s  %s\n", t.ID, t.Quadrant, quadrantLabels(s).Of(t.Quadrant), t.Title)
			return nil
		},
	}

	reorderCmd := &cobra.Command{
		Use:       "reorder <id> <up|down|top|bottom>",
		Short:     "Move a task up or down within its quadrant",
		Args:      cobra.ExactArgs(2),
		ValidArgs: []string{"up", "down", "top", "bottom"},
		RunE: func(cmd *cobra.Command, args []string) error {
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
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Reorder(id, delta)
			if err != nil {
				return err
			}
			fmt.Printf("moved %d %s %d · %s\n", t.ID, where, t.Quadrant, quadrantLabels(s).Of(t.Quadrant))
			return nil
		},
	}

	undoCmd := &cobra.Command{
		Use:   "undo",
		Short: "Undo the last change made from any frontend (TUI, CLI, or MCP)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			label, err := s.Undo()
			if err != nil {
				return err
			}
			fmt.Printf("undid %s\n", label)
			return nil
		},
	}

	redoCmd := &cobra.Command{
		Use:   "redo",
		Short: "Re-apply the last undone change (until the next change discards it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			label, err := s.Redo()
			if err != nil {
				return err
			}
			fmt.Printf("redid %s\n", label)
			return nil
		},
	}

	var labelReset bool
	labelCmd := &cobra.Command{
		Use:   "label [quadrant] [name]",
		Short: "Show or change the quadrant headings",
		Long: "With no arguments, print the four quadrant headings.\n" +
			"With a quadrant and a name, rename that quadrant.\n" +
			"Pass --reset (or an empty name) to restore the built-in default.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if labelReset {
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
					fmt.Printf("%d · %s%s\n", q, labels.Of(q), marker)
				}
				return nil
			}

			q, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid quadrant %q", args[0])
			}
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			if labelReset {
				if name != "" {
					return fmt.Errorf("pass a name or --reset, not both")
				}
			} else if len(args) < 2 {
				return fmt.Errorf("give a name to set, or --reset to restore the default")
			}

			result, err := s.SetQuadrantLabel(task.Quadrant(q), name)
			if err != nil {
				return err
			}
			fmt.Printf("quadrant %d is now %s\n", q, result)
			return nil
		},
	}
	labelCmd.Flags().BoolVar(&labelReset, "reset", false, "restore the built-in default name")

	var archiveJSON bool
	archiveCmd := &cobra.Command{
		Use:   "archive",
		Short: "List completed tasks, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			arch, err := s.ListArchive()
			if err != nil {
				return err
			}
			if archiveJSON {
				return printJSON(arch)
			}
			if len(arch) == 0 {
				fmt.Println("archive is empty")
				return nil
			}
			for _, t := range arch {
				when := ""
				if t.DoneAt != nil {
					when = t.DoneAt.Local().Format("2006-01-02")
				}
				fmt.Printf("  %3d  %s  %s\n", t.ID, when, t.Title)
			}
			return nil
		},
	}
	archiveCmd.Flags().BoolVar(&archiveJSON, "json", false, "output JSON")

	rootCmd.AddCommand(addCmd, listCmd, doneCmd, mvCmd, rmCmd, archiveCmd,
		restoreCmd, reorderCmd, undoCmd, redoCmd, labelCmd)
}
