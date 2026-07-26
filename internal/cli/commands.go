package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

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

func printGrouped(tasks []task.Task) {
	byQ := map[task.Quadrant][]task.Task{}
	for _, t := range tasks {
		byQ[t.Quadrant] = append(byQ[t.Quadrant], t)
	}
	for q := task.Do; q <= task.Eliminate; q++ {
		if len(byQ[q]) == 0 {
			continue
		}
		fmt.Printf("%d · %s — %s\n", q, q.Label(), q.Desc())
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
			fmt.Printf("added %d [%s] %s\n", t.ID, t.Quadrant.Label(), t.Title)
			return nil
		},
	}
	addCmd.Flags().IntVarP(&addQuadrant, "quadrant", "q", 2,
		"quadrant: 1=Do (urgent+important), 2=Schedule (important), 3=Delegate (urgent), 4=Eliminate")

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
			printGrouped(tasks)
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
			fmt.Printf("moved %d to %d · %s\n", t.ID, q, t.Quadrant.Label())
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

	rootCmd.AddCommand(addCmd, listCmd, doneCmd, mvCmd, rmCmd, archiveCmd)
}
