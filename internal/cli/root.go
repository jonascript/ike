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

// NewRootCmd builds the whole command tree against open.
func NewRootCmd(open opener) *cobra.Command {
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
	)
	return root
}

// Execute runs the CLI and exits non-zero on error.
func Execute() {
	if err := NewRootCmd(store.Open).Execute(); err != nil {
		os.Exit(1)
	}
}
