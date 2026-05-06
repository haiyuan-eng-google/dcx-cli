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
	var mode string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start MCP server over stdio (JSON-RPC 2.0)",
		Long: `Start an MCP server over stdio.

Modes:
  classic      Expose all read-only tools upfront (default)
  progressive  Expose 3 meta-tools: dcx_discover, dcx_describe, dcx_execute
               Agent loads command schemas on demand (~99% fewer tokens)`,
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

			// Validate mode.
			validMode := false
			for _, m := range dcxerrors.AllowedMCPModes {
				if mode == m {
					validMode = true
					break
				}
			}
			if !validMode {
				fmt.Fprintf(os.Stderr, "invalid --mode=%q; allowed: %s\n",
					mode, strings.Join(dcxerrors.AllowedMCPModes, ", "))
				os.Exit(1)
			}

			// Find dcx binary path.
			dcxBinary, err := os.Executable()
			if err != nil {
				dcxBinary = "dcx"
			}

			server := dcxerrors.NewServer(a.Registry, format, dcxBinary, mode)
			return server.Serve()
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "classic", "Server mode: classic (all tools) or progressive (3 meta-tools)")
	return cmd
}
