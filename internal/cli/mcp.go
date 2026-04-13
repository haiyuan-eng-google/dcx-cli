package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/mcp"
	"github.com/spf13/cobra"
)

func (a *App) addMCPCommands() {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server",
	}

	mcpCmd.AddCommand(a.mcpServeCmd())
	a.Root.AddCommand(mcpCmd)

	a.Registry.Register(contracts.BuildContract(
		"mcp serve", "mcp",
		"Start MCP server over stdio (JSON-RPC 2.0)",
		nil, false, false,
	))
}

func (a *App) mcpServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start MCP server over stdio (JSON-RPC 2.0)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine format from DCX_MCP_FORMAT or default to json-minified.
			format := os.Getenv("DCX_MCP_FORMAT")
			if format == "" {
				format = "json-minified"
			}

			// Validate format.
			validFormats := dcxerrors.AllowedMCPFormats
			valid := false
			for _, f := range validFormats {
				if format == f {
					valid = true
					break
				}
			}
			if !valid {
				fmt.Fprintf(os.Stderr, "invalid DCX_MCP_FORMAT=%q; allowed: %s\n",
					format, strings.Join(validFormats, ", "))
				os.Exit(1)
			}

			// Find dcx binary path.
			dcxBinary, err := os.Executable()
			if err != nil {
				dcxBinary = "dcx"
			}

			server := dcxerrors.NewServer(a.Registry, format, dcxBinary)
			return server.Serve()
		},
	}
}
