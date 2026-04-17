package cli

import (
	"strings"

	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) addMetaCommands() {
	metaCmd := &cobra.Command{
		Use:   "meta",
		Short: "Introspection commands for the dcx contract",
	}

	metaCmd.AddCommand(a.metaCommandsCmd())
	metaCmd.AddCommand(a.metaDescribeCmd())

	a.Root.AddCommand(metaCmd)
}

func (a *App) metaCommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "List all registered commands and their domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil // unreachable after Emit exits
			}

			summaries := a.Registry.ListCommands()

			switch format {
			case output.Table:
				// Convert to []map[string]interface{} for table rendering.
				rows := make([]map[string]interface{}, len(summaries))
				for i, s := range summaries {
					rows[i] = map[string]interface{}{
						"command":     s.Command,
						"domain":      s.Domain,
						"description": s.Description,
					}
				}
				return a.Render(format, rows)
			default:
				return a.Render(format, summaries)
			}
		},
	}
}

func (a *App) metaDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe [command...]",
		Short: "Show the full contract for a given command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			commandPath := strings.Join(args, " ")
			contract, err := a.Registry.Describe(commandPath)
			if err != nil {
				dcxerrors.Emit(dcxerrors.UnknownCommand, err.Error(), "Run 'dcx meta commands' to see available commands")
				return nil
			}

			switch format {
			case output.Table:
				// For table, show contract as key-value pairs.
				kv := map[string]interface{}{
					"command":          contract.Command,
					"contract_version": contract.ContractVersion,
					"domain":           contract.Domain,
					"description":      contract.Description,
					"supports_dry_run": contract.SupportsDryRun,
					"is_mutation":      contract.IsMutation,
				}
				return a.Render(format, kv)
			default:
				return a.Render(format, contract)
			}
		},
	}
}
