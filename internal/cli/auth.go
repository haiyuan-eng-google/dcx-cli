package cli

import (
	"context"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) addAuthCommands() {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}

	authCmd.AddCommand(a.authCheckCmd())
	authCmd.AddCommand(a.authStatusCmd())

	a.Root.AddCommand(authCmd)

	// Register contracts.
	a.Registry.Register(contracts.BuildContract(
		"auth check", "auth",
		"Verify credentials and report auth method (CI/agent preflight)",
		nil, false, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"auth status", "auth",
		"Show current authentication status",
		nil, false, false,
	))
}

func (a *App) authCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Verify credentials and report auth method (CI/agent preflight)",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			ctx := context.Background()
			result := auth.Check(ctx, a.AuthConfig())

			if !result.Authenticated {
				// Write result then exit with auth error code.
				if renderErr := output.Render(format, result); renderErr != nil {
					dcxerrors.Emit(dcxerrors.Internal, renderErr.Error(), "")
				}
				dcxerrors.Emit(dcxerrors.AuthError, "Authentication failed: "+result.Error, "Run 'dcx auth login' or set DCX_TOKEN")
				return nil
			}

			return output.Render(format, result)
		},
	}
}

func (a *App) authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			ctx := context.Background()
			result := auth.Check(ctx, a.AuthConfig())
			return output.Render(format, result)
		},
	}
}
