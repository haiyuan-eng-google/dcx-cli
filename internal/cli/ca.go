package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/ca"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
)

func (a *App) addCACommands() {
	caCmd := &cobra.Command{
		Use:   "ca",
		Short: "Conversational Analytics commands",
	}

	caCmd.AddCommand(a.caAskCmd())
	a.Root.AddCommand(caCmd)

	a.Registry.Register(contracts.BuildContract(
		"ca ask", "ca",
		"Ask a natural-language question across Data Cloud sources",
		[]contracts.FlagContract{
			{Name: "question", Type: "string", Description: "Natural language question (positional argument)", Required: true},
			{Name: "profile", Type: "string", Description: "Source profile name or path"},
			{Name: "agent", Type: "string", Description: "Data agent name (BigQuery)"},
			{Name: "tables", Type: "string", Description: "Comma-separated table refs (BigQuery)"},
		},
		false, false,
	))
}

func (a *App) caAskCmd() *cobra.Command {
	var profileName, agent, tables string

	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a natural-language question across Data Cloud sources",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")

			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			// Resolve auth.
			ctx := context.Background()
			resolved, err := auth.Resolve(ctx, a.AuthConfig())
			if err != nil {
				dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "Run 'dcx auth check' to verify credentials")
				return nil
			}
			tok, err := resolved.TokenSource.Token()
			if err != nil {
				dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
				return nil
			}

			// Load profile if specified.
			var profile *profiles.Profile
			if profileName != "" {
				p, err := profiles.LoadByName(profileName)
				if err != nil {
					dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Check profile name or path")
					return nil
				}
				profile = p
			} else if a.Opts.ProjectID != "" {
				// No profile: use project-id flag for BigQuery inline mode.
				profile = &profiles.Profile{
					Name:       "inline",
					SourceType: profiles.BigQuery,
					Project:    a.Opts.ProjectID,
					DatasetID:  a.Opts.DatasetID,
				}
			} else {
				dcxerrors.Emit(dcxerrors.MissingArgument,
					"either --profile or --project-id is required",
					"Use --profile for multi-source or --project-id for BigQuery inline")
				return nil
			}

			client := ca.NewClient(nil)
			result, err := client.Ask(ctx, tok.AccessToken, profile, question, agent, tables)
			if err != nil {
				code := dcxerrors.APIError
				dcxerrors.Emit(code, err.Error(), "")
				return nil
			}

			return output.Render(format, result)
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "Source profile name or path")
	cmd.Flags().StringVar(&agent, "agent", "", "Data agent name (BigQuery)")
	cmd.Flags().StringVar(&tables, "tables", "", "Comma-separated table refs (BigQuery)")

	return cmd
}
