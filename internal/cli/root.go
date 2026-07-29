// Package cli defines ike's command-line interface. The bare `ike` command
// launches the TUI; subcommands provide scriptable access to the same store.
package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/store"
	"github.com/jonascript/ike/internal/tui"
)

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
	// space backs the --space flag. It is scoped to this call rather than being
	// a package-level variable, so building a second tree — which every test
	// does — cannot inherit a value from the first.
	var space string

	// Commands receive an opener that applies the flag, so none of them has to
	// know the flag exists. InSpace("") follows the file's current space, which
	// is what an unflagged Store already does, so there is nothing to branch on.
	// It stays lazy: the flag is read inside RunE, after cobra has parsed it.
	open := opener(func() (*store.Store, error) {
		s, err := outer()
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
	root.PersistentFlags().StringVarP(&space, "space", "s", "",
		"act on this space instead of the current one")
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
		// The space commands act on the document, so they take the store as it
		// was opened, without the --space flag applied.
		newSpaceCmd(outer),
	)
	return root
}

// Execute runs the CLI and exits non-zero on error.
func Execute() {
	if err := NewRootCmd(store.Open).Execute(); err != nil {
		os.Exit(1)
	}
}
