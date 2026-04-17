package cli

import (
	"context"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
)

func (a *App) addProfilesCommands() {
	profilesCmd := &cobra.Command{
		Use:   "profiles",
		Short: "Manage dcx source profiles",
	}

	profilesCmd.AddCommand(a.profilesListCmd())
	profilesCmd.AddCommand(a.profilesValidateCmd())
	profilesCmd.AddCommand(a.profilesTestCmd())

	a.Root.AddCommand(profilesCmd)

	// Register contracts.
	a.Registry.Register(contracts.BuildContract(
		"profiles list", "profiles",
		"List all configured source profiles",
		nil, false, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"profiles validate", "profiles",
		"Validate all configured source profiles",
		nil, false, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"profiles test", "profiles",
		"Test source connectivity for all configured profiles",
		nil, false, false,
	))
}

func (a *App) profilesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured source profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			all, err := profiles.LoadAll()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Check "+profiles.ProfilesDir())
				return nil
			}

			if all == nil {
				all = []profiles.Profile{}
			}

			result := map[string]interface{}{
				"items":        all,
				"profiles_dir": profiles.ProfilesDir(),
				"count":        len(all),
			}

			return a.Render(format, result)
		},
	}
}

func (a *App) profilesValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate all configured source profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			all, err := profiles.LoadAll()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Check "+profiles.ProfilesDir())
				return nil
			}

			var results []profiles.ValidationResult
			allValid := true
			for _, p := range all {
				issues := p.Validate()
				valid := len(issues) == 0
				if !valid {
					allValid = false
				}
				results = append(results, profiles.ValidationResult{
					Name:       p.Name,
					SourceType: string(p.SourceType),
					Valid:      valid,
					Issues:     issues,
				})
			}

			result := map[string]interface{}{
				"profiles":  results,
				"all_valid": allValid,
				"count":     len(results),
			}

			if err := a.Render(format, result); err != nil {
				return err
			}

			if !allValid {
				dcxerrors.Emit(dcxerrors.InvalidConfig, "one or more profiles have validation errors", "Run 'dcx profiles validate' for details")
			}

			return nil
		},
	}
}

// ProfileTestResult holds the result of a profile connectivity test.
type ProfileTestResult struct {
	Name          string `json:"name"`
	SourceType    string `json:"source_type"`
	Valid         bool   `json:"valid"`
	Authenticated bool   `json:"authenticated"`
	Error         string `json:"error,omitempty"`
}

func (a *App) profilesTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Test source connectivity for all configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			all, err := profiles.LoadAll()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Check "+profiles.ProfilesDir())
				return nil
			}

			ctx := context.Background()
			authResult := auth.Check(ctx, a.AuthConfig())

			var results []ProfileTestResult
			allPassed := true
			for _, p := range all {
				issues := p.Validate()
				valid := len(issues) == 0

				tr := ProfileTestResult{
					Name:          p.Name,
					SourceType:    string(p.SourceType),
					Valid:         valid,
					Authenticated: authResult.Authenticated,
				}

				if !valid {
					tr.Error = "validation failed: " + strings.Join(issues, "; ")
					allPassed = false
				} else if !authResult.Authenticated {
					tr.Error = "authentication failed: " + authResult.Error
					allPassed = false
				}

				results = append(results, tr)
			}

			result := map[string]interface{}{
				"profiles":   results,
				"all_passed": allPassed,
				"count":      len(results),
			}

			return a.Render(format, result)
		},
	}
}
