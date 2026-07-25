package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/joncrockett/ike/internal/mcpserver"
)

func init() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio so AI agents can manage the matrix",
		Long: "Runs an MCP (Model Context Protocol) server on stdin/stdout.\n" +
			"Register it with an MCP client, e.g.:\n\n  claude mcp add ike -- ike mcp",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
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
	rootCmd.AddCommand(mcpCmd)
}
