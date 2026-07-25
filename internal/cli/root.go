// Package cli defines ike's command-line interface. The bare `ike` command
// launches the TUI; subcommands provide scriptable access to the same store.
package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/joncrockett/ike/internal/store"
	"github.com/joncrockett/ike/internal/tui"
)

// version is stamped at build time via -ldflags "-X ...cli.version=...".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "ike",
	Short:   "ike — an Eisenhower matrix task manager",
	Long:    "ike is a terminal Eisenhower matrix: run it bare for the interactive TUI,\nor use subcommands for quick capture and scripting.",
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		return tui.Run(s)
	},
}

func openStore() (*store.Store, error) {
	return store.Open()
}

// Execute runs the CLI and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
