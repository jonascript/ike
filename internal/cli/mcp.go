package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonascript/ike/internal/mcpserver"
)

// mcpDisabledMsg explains how to switch agent access on. It is worded for the
// person reading their MCP client's error log, who may not know ike well.
const mcpDisabledMsg = "MCP access is off for this matrix.\n" +
	"ike does not expose your tasks to AI agents until you allow it:\n\n" +
	"  ike mcp enable\n\n" +
	"Run `ike mcp status` to see the current setting and which data file it applies to."

func init() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio so AI agents can manage the matrix",
		Long: "Runs an MCP (Model Context Protocol) server on stdin/stdout.\n\n" +
			"Access is off until you enable it, so registering ike with an MCP client\n" +
			"is not enough on its own:\n\n" +
			"  ike mcp enable\n" +
			"  claude mcp add ike -- ike mcp\n\n" +
			"The setting is remembered per data file, and `ike mcp disable` revokes it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			enabled, err := s.MCPEnabled()
			if err != nil {
				return err
			}
			if !enabled {
				// stdout carries JSON-RPC, so this must not go there. Returning
				// an error routes it to stderr and exits non-zero, which the
				// client surfaces as a failed connection rather than an empty one.
				return errors.New(mcpDisabledMsg)
			}
			// stdout carries JSON-RPC; anything human-facing must go to stderr,
			// which cobra already uses for errors.
			err = mcpserver.Run(cmd.Context(), s, version)
			// A client disconnect is a normal shutdown, but the SDK surfaces
			// it as an error whose sentinel lives in an internal package, so
			// match by message.
			if err != nil && (errors.Is(err, io.EOF) || strings.Contains(err.Error(), "is closing")) {
				return nil
			}
			return err
		},
	}

	enableCmd := &cobra.Command{
		Use:   "enable",
		Short: "Allow AI agents to read and manage this matrix over MCP",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return setMCP(true) },
	}

	disableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Revoke MCP access to this matrix",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return setMCP(false) },
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether MCP access is enabled, and for which data file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			enabled, err := s.MCPEnabled()
			if err != nil {
				return err
			}
			state, hint := "off", "ike mcp enable"
			if enabled {
				state, hint = "on", "ike mcp disable"
			}
			fmt.Printf("MCP access: %s\n", state)
			fmt.Printf("data file:  %s\n", s.Path())
			fmt.Printf("change it:  %s\n", hint)
			return nil
		},
	}

	mcpCmd.AddCommand(enableCmd, disableCmd, statusCmd)
	rootCmd.AddCommand(mcpCmd)
}

// setMCP flips the MCP permission and reports what happened.
func setMCP(on bool) error {
	s, err := openStore()
	if err != nil {
		return err
	}
	changed, err := s.SetMCPEnabled(on)
	if err != nil {
		return err
	}
	state := "off"
	if on {
		state = "on"
	}
	if !changed {
		fmt.Printf("MCP access was already %s for %s\n", state, s.Path())
		return nil
	}
	fmt.Printf("MCP access is now %s for %s\n", state, s.Path())
	if on {
		fmt.Println("Register ike with a client if you have not already:\n\n  claude mcp add ike -- ike mcp")
	}
	return nil
}
