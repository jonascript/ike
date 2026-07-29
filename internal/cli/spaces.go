package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/task"
)

// newSpaceCmd builds the `ike space` tree.
//
// It takes the store as it was opened, without the --space flag applied: these
// commands act on the document and name their target as an argument, so
// `ike -s work space use home` would be asking two different questions at once.
// rejectSpaceFlag says so rather than silently picking one.
func newSpaceCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "space",
		Short: "Manage spaces — independent matrices in one data file",
		Long: "A space is a self-contained matrix: its own tasks, archive, quadrant\n" +
			"headings, and undo history. Every command acts on the current space\n" +
			"unless you pass --space.\n\n" +
			"With no subcommand, this lists them.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			return printSpaces(cmd, s, false)
		}),
	}
	cmd.AddCommand(
		newSpaceListCmd(open),
		newSpaceNewCmd(open),
		newSpaceUseCmd(open),
		newSpaceRenameCmd(open),
		newSpaceRmCmd(open),
	)
	return cmd
}

// rejectSpaceFlag stops a space command from being given --space, which would
// name a target twice and in two different ways.
func rejectSpaceFlag(cmd *cobra.Command) error {
	if f := cmd.Flags().Lookup("space"); f != nil && f.Changed {
		return fmt.Errorf("`ike space %s` takes the space as an argument; drop --space", cmd.Name())
	}
	return nil
}

func newSpaceListCmd(open opener) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List spaces, marking the current one",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			return printSpaces(cmd, s, asJSON)
		}),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func printSpaces(cmd *cobra.Command, s *store.Store, asJSON bool) error {
	spaces, err := s.ListSpaces()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(cmd.OutOrStdout(), spaces)
	}
	// Pad to the longest name present rather than to MaxSpaceNameLen, so a file
	// of short names does not print a column of empty space.
	width := 0
	for _, sp := range spaces {
		width = max(width, len([]rune(task.SanitizeDisplay(sp.Name))))
	}
	for _, sp := range spaces {
		marker := " "
		if sp.Current {
			marker = "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %-*s  %s\n",
			marker, width, task.SanitizeDisplay(sp.Name), spaceCounts(sp))
	}
	return nil
}

// spaceSummary names every space in one line, marking the current one, for
// contexts that report on the whole file rather than list it.
func spaceSummary(spaces []store.SpaceInfo) string {
	names := make([]string, len(spaces))
	for i, sp := range spaces {
		names[i] = task.SanitizeDisplay(sp.Name)
		if sp.Current {
			names[i] += " (current)"
		}
	}
	return strings.Join(names, ", ")
}

// spaceCounts describes what a space holds, for a listing.
func spaceCounts(sp store.SpaceInfo) string {
	if sp.Archived == 0 {
		return fmt.Sprintf("%d active", sp.Active)
	}
	return fmt.Sprintf("%d active, %d archived", sp.Active, sp.Archived)
}

func newSpaceNewCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "new <name>",
		Short: "Create an empty space",
		Long: "Create an empty space. This does not switch to it — run\n" +
			"`ike space use <name>` when you want to work in it.",
		Args: cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			if _, err := s.NewSpace(args[0]); err != nil {
				return err
			}
			name := task.SanitizeDisplay(args[0])
			fmt.Fprintf(cmd.OutOrStdout(), "created space %s  (ike space use %s)\n", name, name)
			return nil
		}),
	}
}

func newSpaceUseCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the current space",
		Args:  cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			d, err := s.UseSpace(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "now on space %s\n", task.SanitizeDisplay(d.Space))
			return nil
		}),
	}
}

func newSpaceRenameCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a space",
		Args:  cobra.ExactArgs(2),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			if err := s.RenameSpace(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed %s to %s\n",
				task.SanitizeDisplay(args[0]), task.SanitizeDisplay(args[1]))
			return nil
		}),
	}
}

func newSpaceRmCmd(open opener) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a space and everything in it",
		Long: "Delete a space, its tasks, its archive, and its history.\n\n" +
			"Unlike deleting a task, this cannot be undone — the space has no\n" +
			"history left to undo it from. A space holding anything needs --force,\n" +
			"and the previous file contents remain in tasks.json.bak until the next\n" +
			"change.",
		Args: cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			removed, err := s.RemoveSpace(args[0], force)
			if err != nil {
				return err
			}
			name := task.SanitizeDisplay(removed.Name)
			// Say what was destroyed, not just that something was: the counts
			// are the only record left once the space is gone.
			fmt.Fprintf(cmd.OutOrStdout(), "deleted space %s (%s)\n", name, spaceCounts(removed))
			if removed.Current {
				d, err := s.Load()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "now on space %s\n", task.SanitizeDisplay(d.Space))
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete even if the space still holds tasks")
	return cmd
}
