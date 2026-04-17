package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/looker"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
)

// registerLookerSDKCommands adds Looker Admin SDK commands that are not
// available through the Discovery pipeline.
func (a *App) registerLookerSDKCommands() {
	lookerCmd := findOrCreateCmd(a.Root, "looker")

	// explores list
	exploresCmd := findOrCreateCmd(lookerCmd, "explores")
	a.addExploresListCmd(exploresCmd)

	// dashboards get
	dashboardsCmd := findOrCreateCmd(lookerCmd, "dashboards")
	a.addDashboardsGetCmd(dashboardsCmd)
}

func (a *App) addExploresListCmd(parent *cobra.Command) {
	var profileName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List explores from a Looker instance",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			ctx := context.Background()
			client, err := a.resolveLookerClient(ctx, profileName)
			if err != nil {
				return nil
			}

			result, err := client.ListExplores(ctx)
			if err != nil {
				dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
				return nil
			}

			return a.Render(format, result)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "Looker profile name or path (required)")
	cmd.MarkFlagRequired("profile")

	parent.AddCommand(cmd)

	a.Registry.Register(contracts.BuildContract(
		"looker explores list", "looker",
		"List explores from a Looker instance",
		[]contracts.FlagContract{
			{Name: "profile", Type: "string", Description: "Looker profile name or path", Required: true},
		},
		false, false,
	))
}

func (a *App) addDashboardsGetCmd(parent *cobra.Command) {
	var profileName, dashboardID string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a Looker dashboard by ID",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if dashboardID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --dashboard-id is missing", "")
				return nil
			}

			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			ctx := context.Background()
			client, err := a.resolveLookerClient(ctx, profileName)
			if err != nil {
				return nil
			}

			result, err := client.GetDashboard(ctx, dashboardID)
			if err != nil {
				dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
				return nil
			}

			return a.Render(format, result)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "Looker profile name or path (required)")
	cmd.Flags().StringVar(&dashboardID, "dashboard-id", "", "Dashboard ID (required)")
	cmd.MarkFlagRequired("profile")
	cmd.MarkFlagRequired("dashboard-id")

	parent.AddCommand(cmd)

	a.Registry.Register(contracts.BuildContract(
		"looker dashboards get", "looker",
		"Get a Looker dashboard by ID",
		[]contracts.FlagContract{
			{Name: "profile", Type: "string", Description: "Looker profile name or path", Required: true},
			{Name: "dashboard-id", Type: "string", Description: "Dashboard ID", Required: true},
		},
		false, false,
	))
}

// resolveLookerClient loads a profile and creates a Looker client.
func (a *App) resolveLookerClient(ctx context.Context, profileName string) (*looker.Client, error) {
	profile, err := profiles.LoadByName(profileName)
	if err != nil {
		dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
		return nil, err
	}

	if profile.LookerInstanceURL == "" {
		dcxerrors.Emit(dcxerrors.InvalidConfig,
			"looker_instance_url is required in the Looker profile",
			fmt.Sprintf("Add looker_instance_url to profile %s", profileName))
		return nil, fmt.Errorf("missing looker_instance_url")
	}

	resolved, err := auth.Resolve(ctx, a.AuthConfig())
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "")
		return nil, err
	}
	tok, err := resolved.TokenSource.Token()
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
		return nil, err
	}

	return looker.NewClient(nil, profile.LookerInstanceURL, tok.AccessToken), nil
}
