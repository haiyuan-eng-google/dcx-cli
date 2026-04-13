package cli

import (
	"strings"

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

			return output.Render(format, result)
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

			if err := output.Render(format, result); err != nil {
				return err
			}

			if !allValid {
				dcxerrors.Emit(dcxerrors.InvalidConfig, "one or more profiles have validation errors", "Run 'dcx profiles validate' for details")
			}

			return nil
		},
	}
}
