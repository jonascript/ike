package cli

import (
	"errors"
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
		newSpaceExportCmd(open),
		newSpaceImportCmd(open),
	)
	return cmd
}

func newSpaceExportCmd(open opener) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "export <name> <path>",
		Short: "Write a space to a standalone data file",
		Long: "Write one space to its own ike data file. The result is a normal data\n" +
			"file: open it directly with --file, or pull it into another one with\n" +
			"`ike space import`. This is how you move a matrix to another machine —\n" +
			"export, copy the one file, import.\n\n" +
			"MCP access is always off in the exported file, whatever it is here:\n" +
			"agent access is a decision about a file on a machine, and an export is\n" +
			"made to travel.",
		Args: cobra.ExactArgs(2),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			info, err := s.ExportSpace(args[0], args[1], force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported space %s (%s) to %s\n",
				task.SanitizeDisplay(info.Name), spaceCounts(info), args[1])
			return nil
		}),
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the destination if it already exists")
	return cmd
}

func newSpaceImportCmd(open opener) *cobra.Command {
	var as string
	var all bool
	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Copy spaces in from another data file",
		Long: "Copy a space out of another ike data file into this one. By default it\n" +
			"takes that file's current space; --all takes every space in it.\n\n" +
			"A name already in use is an error rather than a merge: combining two\n" +
			"matrices would have to reconcile two ID sequences and two histories.\n" +
			"Use --as to bring a space in under a different name.",
		Args: cobra.ExactArgs(1),
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			if err := rejectSpaceFlag(cmd); err != nil {
				return err
			}
			imported, err := s.ImportSpaces(args[0], as, all)
			if err != nil {
				return err
			}
			for _, info := range imported {
				fmt.Fprintf(cmd.OutOrStdout(), "imported space %s (%s)\n",
					task.SanitizeDisplay(info.Name), spaceCounts(info))
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&as, "as", "", "import the space under this name instead")
	cmd.Flags().BoolVar(&all, "all", false, "import every space in the file, not just its current one")
	return cmd
}

// spaceFlagged reports whether --space was given. The flag is looked up by name
// because a persistent flag is only reachable that way from a subcommand, so the
// name is spelled once here rather than at each place that asks.
func spaceFlagged(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup(spaceFlag)
	return f != nil && f.Changed
}

// rejectSpaceFlag stops a space command from being given --space, which would
// name a target twice and in two different ways.
func rejectSpaceFlag(cmd *cobra.Command) error {
	if spaceFlagged(cmd) {
		return fmt.Errorf("`ike space %s` takes the space as an argument; drop --%s", cmd.Name(), spaceFlag)
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

// spaceCounts describes what a space holds as a listing column.
func spaceCounts(sp store.SpaceInfo) string {
	if sp.Archived == 0 {
		return fmt.Sprintf("%d active", sp.Active)
	}
	return fmt.Sprintf("%d active, %d archived", sp.Active, sp.Archived)
}

// spaceHolds describes the same counts as a clause, for a sentence about losing
// them. A column can drop the noun; a sentence cannot.
func spaceHolds(sp store.SpaceInfo) string {
	switch {
	case sp.Active > 0 && sp.Archived > 0:
		return fmt.Sprintf("%s and %s", plural(sp.Active, "active task"), plural(sp.Archived, "archived task"))
	case sp.Archived > 0:
		return plural(sp.Archived, "archived task")
	default:
		return plural(sp.Active, "active task")
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
			var notEmpty *store.NotEmptyError
			if errors.As(err, &notEmpty) {
				return fmt.Errorf("%q still holds %s; pass --force to delete it anyway, or rename it aside",
					task.SanitizeDisplay(notEmpty.Space.Name), spaceHolds(notEmpty.Space))
			}
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
