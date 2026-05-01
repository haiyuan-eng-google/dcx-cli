package cli

import (
	"context"
	"fmt"
	"os"
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
	authCmd.AddCommand(a.authLoginCmd())
	authCmd.AddCommand(a.authLogoutCmd())

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
	a.Registry.Register(contracts.BuildContract(
		"auth login", "auth",
		"Log in via OAuth2 browser flow and store credentials",
		nil, false, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"auth logout", "auth",
		"Remove stored OAuth2 credentials",
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
				if renderErr := a.Render(format, result); renderErr != nil {
					dcxerrors.Emit(dcxerrors.Internal, renderErr.Error(), "")
				}
				dcxerrors.Emit(dcxerrors.AuthError, "Authentication failed: "+result.Error, "Run 'dcx auth login' or set DCX_TOKEN")
				return nil
			}

			return a.Render(format, result)
		},
	}
}

func (a *App) authLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in via OAuth2 browser flow and store credentials",
		Long: `Opens a browser for Google OAuth2 authorization. After consent,
stores a refresh token in ~/.config/dcx/credentials.json.

Subsequent dcx commands will use this token automatically
(Tier 3 in the auth resolution chain, after --token and
--credentials-file but before ADC).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			token, err := auth.Login(ctx)
			if err != nil {
				dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "")
				return nil
			}

			fmt.Fprintf(os.Stderr, "Logged in successfully. Credentials saved to ~/.config/dcx/credentials.json\n")
			fmt.Fprintf(os.Stderr, "Token expires: %s\n", token.Expiry.Format("2006-01-02 15:04:05"))

			format, fmtErr := a.OutputFormat()
			if fmtErr != nil {
				return nil
			}
			return a.Render(format, map[string]interface{}{
				"authenticated": true,
				"method":        "oauth_login",
				"source":        "~/.config/dcx/credentials.json",
			})
		},
	}
}

func (a *App) authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored OAuth2 credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.Logout(); err != nil {
				dcxerrors.Emit(dcxerrors.Internal, err.Error(), "")
				return nil
			}

			fmt.Fprintf(os.Stderr, "Logged out. Stored credentials removed.\n")

			format, fmtErr := a.OutputFormat()
			if fmtErr != nil {
				return nil
			}
			return a.Render(format, map[string]interface{}{
				"logged_out": true,
			})
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
			return a.Render(format, result)
		},
	}
}
