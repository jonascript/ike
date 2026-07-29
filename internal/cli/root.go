// Package cli defines ike's command-line interface. The bare `ike` command
// launches the TUI; subcommands provide scriptable access to the same store.
package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/tui"
)

// spaceFlag is the name of the --space flag. Subcommands can only reach a
// persistent flag by name, so the name is a constant rather than a literal in
// each place that looks it up.
const spaceFlag = "space"

// version is stamped at build time via -ldflags "-X ...cli.version=...".
var version = "dev"

// opener yields the store a command should act on. It is a parameter rather
// than a direct call to store.Open so tests can build the command tree over a
// scratch data file; that is the whole reason the commands are constructed
// instead of registered from init().
//
// It stays lazy — resolved inside RunE, not when the tree is built — so `ike
// --help` still works when IKE_DATA_FILE is set to something invalid.
type opener func() (*store.Store, error)

// withStore adapts a command body that needs a store into a cobra RunE, so the
// bodies start at the part that differs instead of repeating the same three
// lines of open-and-check.
func withStore(open opener, run func(*cobra.Command, []string, *store.Store) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		s, err := open()
		if err != nil {
			return err
		}
		return run(cmd, args, s)
	}
}

// NewRootCmd builds the whole command tree against outer.
func NewRootCmd(outer opener) *cobra.Command {
	// space and file back the two persistent flags. They are scoped to this call
	// rather than being package-level variables, so building a second tree —
	// which every test does — cannot inherit a value from the first.
	var space, file string

	// openFile applies --file only. It stays lazy: the flag is read inside RunE,
	// after cobra has parsed it, so `ike --help` still works with a broken path.
	openFile := opener(func() (*store.Store, error) {
		if file != "" {
			// --file replaces the whole resolution, so a broken IKE_DATA_FILE
			// must not stop you pointing at a working file.
			return store.OpenPath("--file", file)
		}
		return outer()
	})

	// open adds --space, so no command has to know either flag exists.
	// InSpace("") follows the file's current space, which is what an unflagged
	// Store already does, so there is nothing to branch on.
	open := opener(func() (*store.Store, error) {
		s, err := openFile()
		if err != nil {
			return nil, err
		}
		return s.InSpace(space), nil
	})

	root := &cobra.Command{
		Use:          "ike",
		Short:        "ike — an Eisenhower matrix task manager",
		Long:         "ike is a terminal Eisenhower matrix: run it bare for the interactive TUI,\nor use subcommands for quick capture and scripting.",
		Version:      version,
		SilenceUsage: true,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			return tui.Run(s)
		}),
	}
	root.PersistentFlags().StringVarP(&space, spaceFlag, "s", "",
		"act on this space instead of the current one")
	root.PersistentFlags().StringVarP(&file, "file", "f", "",
		"use this data file instead of IKE_DATA_FILE or the default location")
	root.AddCommand(
		newAddCmd(open),
		newListCmd(open),
		newDoneCmd(open),
		newMvCmd(open),
		newRmCmd(open),
		newRestoreCmd(open),
		newReorderCmd(open),
		newUndoCmd(open),
		newRedoCmd(open),
		newLabelCmd(open),
		newArchiveCmd(open),
		newMCPCmd(open),
		// The space commands name their target as an argument, so they take the
		// opener without --space applied — but they still honor --file, since
		// listing or importing into another document is exactly the point.
		newSpaceCmd(openFile),
	)
	return root
}

// Execute runs the CLI and exits non-zero on error.
func Execute() {
	if err := NewRootCmd(store.Open).Execute(); err != nil {
		os.Exit(1)
	}
}
