package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/mcpserver"
	"github.com/jonascript/ike/internal/store"
)

// mcpDisabledMsg explains how to switch agent access on. It is worded for the
// person reading their MCP client's error log, who may not know ike well.
const mcpDisabledMsg = "MCP access is off for this matrix.\n" +
	"ike does not expose your tasks to AI agents until you allow it:\n\n" +
	"  ike mcp enable\n\n" +
	"Run `ike mcp status` to see the current setting and which data file it applies to."

func newMCPCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio so AI agents can manage the matrix",
		Long: "Runs an MCP (Model Context Protocol) server on stdin/stdout.\n\n" +
			"Access is off until you enable it, so registering ike with an MCP client\n" +
			"is not enough on its own:\n\n" +
			"  ike mcp enable\n" +
			"  claude mcp add ike -- ike mcp\n\n" +
			"The setting is remembered per data file, and `ike mcp disable` revokes it.",
		Args: cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			enabled, err := s.MCPEnabled()
			if err != nil {
				return err
			}
			if !enabled {
				// stdout carries JSON-RPC, so this must not go there. Returning
				// an error routes it to stderr and exits non-zero, which the
				// client surfaces as a failed connection rather than an empty one.
				//
				// Deliberately multi-line and sentence-punctuated, against the
				// usual convention for error strings: the reader is someone
				// staring at an MCP client's log, and the whole value of this
				// message is that it tells them the exact command to run.
				//nolint:staticcheck // ST1005: formatted for a human, not a caller.
				return errors.New(mcpDisabledMsg)
			}
			// ForMCP re-checks the gate on every read and mutation, so
			// `ike mcp disable` revokes this session even while it stays
			// connected. The check above only covers startup.
			err = mcpserver.Run(cmd.Context(), s.ForMCP(), version)
			// A client disconnect is a normal shutdown, but the SDK surfaces
			// it as an error whose sentinel lives in an internal package, so
			// match by message.
			if err != nil && (errors.Is(err, io.EOF) || strings.Contains(err.Error(), "is closing")) {
				return nil
			}
			return err
		}),
	}
	cmd.AddCommand(newMCPSetCmd(open, true), newMCPSetCmd(open, false), newMCPStatusCmd(open))
	return cmd
}

// newMCPSetCmd builds `ike mcp enable` or `ike mcp disable`. They differ only
// in the value they write and the words they print, so they are one function:
// two near-identical command bodies is how the two drift apart.
func newMCPSetCmd(open opener, on bool) *cobra.Command {
	use, short := "disable", "Revoke MCP access to this matrix"
	if on {
		use, short = "enable", "Allow AI agents to read and manage this matrix over MCP"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			changed, err := s.SetMCPEnabled(on)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !changed {
				fmt.Fprintf(out, "MCP access was already %s for %s\n", mcpState(on), s.Path())
				return nil
			}
			fmt.Fprintf(out, "MCP access is now %s for %s\n", mcpState(on), s.Path())
			if on {
				fmt.Fprintln(out, "Register ike with a client if you have not already:\n\n  claude mcp add ike -- ike mcp")
			}
			return nil
		}),
	}
}

// newMCPStatusCmd reports the setting. It deliberately uses the ungated store,
// so it still answers after access has been revoked.
func newMCPStatusCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether MCP access is enabled, and for which data file",
		Args:  cobra.NoArgs,
		RunE: withStore(open, func(cmd *cobra.Command, args []string, s *store.Store) error {
			enabled, err := s.MCPEnabled()
			if err != nil {
				return err
			}
			hint := "ike mcp enable"
			if enabled {
				hint = "ike mcp disable"
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "MCP access: %s\n", mcpState(enabled))
			fmt.Fprintf(out, "data file:  %s\n", s.Path())
			fmt.Fprintf(out, "change it:  %s\n", hint)
			return nil
		}),
	}
}

func mcpState(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
