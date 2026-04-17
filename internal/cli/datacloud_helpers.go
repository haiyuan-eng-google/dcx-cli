package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/ca"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/haiyuan-eng-google/dcx-cli/internal/datacloud"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
)

// registerDataCloudHelperCommands adds profile-driven database helper commands:
//   - spanner schema describe, alloydb schema describe, cloudsql schema describe
//   - alloydb databases list, cloudsql databases list
func (a *App) registerDataCloudHelperCommands() {
	helperFlags := []contracts.FlagContract{
		{Name: "profile", Type: "string", Description: "Source profile name or path", Required: true},
	}

	// Schema describe for each database source.
	for _, ns := range []struct {
		namespace  string
		domain     string
		sourceType profiles.SourceType
	}{
		{"spanner", "spanner", profiles.Spanner},
		{"alloydb", "alloydb", profiles.AlloyDB},
		{"cloudsql", "cloudsql", profiles.CloudSQL},
	} {
		a.addSchemaDescribeCmd(ns.namespace, ns.domain, ns.sourceType, helperFlags)
	}

	// databases list for AlloyDB and CloudSQL (via QueryData).
	for _, ns := range []struct {
		namespace  string
		domain     string
		sourceType profiles.SourceType
	}{
		{"alloydb", "alloydb", profiles.AlloyDB},
		{"cloudsql", "cloudsql", profiles.CloudSQL},
	} {
		a.addDatabasesListHelperCmd(ns.namespace, ns.domain, ns.sourceType, helperFlags)
	}
}

func (a *App) addSchemaDescribeCmd(namespace, domain string, sourceType profiles.SourceType, helperFlags []contracts.FlagContract) {
	// Find the namespace command, then find or create "schema" sub-group.
	nsCmd := findOrCreateCmd(a.Root, namespace)
	schemaCmd := findOrCreateCmd(nsCmd, "schema")

	var profileName string

	describeCmd := &cobra.Command{
		Use:   "describe",
		Short: fmt.Sprintf("Describe schema of a %s database via CA QueryData", domain),
		RunE:  a.schemaDescribeRunE(&profileName, sourceType),
	}
	describeCmd.Flags().StringVar(&profileName, "profile", "", "Source profile name or path")
	describeCmd.MarkFlagRequired("profile")

	schemaCmd.AddCommand(describeCmd)

	a.Registry.Register(contracts.BuildContract(
		namespace+" schema describe", domain,
		fmt.Sprintf("Describe schema of a %s database via CA QueryData", domain),
		helperFlags, false, false,
	))
}

func (a *App) addDatabasesListHelperCmd(namespace, domain string, sourceType profiles.SourceType, helperFlags []contracts.FlagContract) {
	nsCmd := findOrCreateCmd(a.Root, namespace)
	dbCmd := findCmd(nsCmd, "databases")
	if dbCmd == nil {
		// databases group might not exist if this service has no Discovery-driven databases commands.
		dbCmd = &cobra.Command{Use: "databases", Short: "databases commands"}
		nsCmd.AddCommand(dbCmd)
	}

	// Check if "list" already exists (Discovery-driven).
	if findCmd(dbCmd, "list") != nil {
		// Already registered by Discovery. Don't add a duplicate.
		// The Discovery command handles the API call; QueryData is an alternative.
		return
	}

	var profileName string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List databases on a %s instance via CA QueryData", domain),
		RunE:  a.databasesListRunE(&profileName, sourceType),
	}
	listCmd.Flags().StringVar(&profileName, "profile", "", "Source profile name or path")
	listCmd.MarkFlagRequired("profile")

	dbCmd.AddCommand(listCmd)

	a.Registry.Register(contracts.BuildContract(
		namespace+" databases list", domain,
		fmt.Sprintf("List databases on a %s instance via CA QueryData", domain),
		helperFlags, false, false,
	))
}

func (a *App) schemaDescribeRunE(profileName *string, sourceType profiles.SourceType) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		format, err := a.OutputFormat()
		if err != nil {
			dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
			return nil
		}

		ctx := context.Background()
		resolved, err := auth.Resolve(ctx, a.AuthConfig())
		if err != nil {
			dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "")
			return nil
		}
		tok, err := resolved.TokenSource.Token()
		if err != nil {
			dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
			return nil
		}

		profile, err := profiles.LoadByName(*profileName)
		if err != nil {
			dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
			return nil
		}

		client := ca.NewClient(nil)
		result, err := datacloud.SchemaDescribe(ctx, client, tok.AccessToken, profile)
		if err != nil {
			dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
			return nil
		}

		return a.Render(format, result)
	}
}

func (a *App) databasesListRunE(profileName *string, sourceType profiles.SourceType) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		format, err := a.OutputFormat()
		if err != nil {
			dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
			return nil
		}

		ctx := context.Background()
		resolved, err := auth.Resolve(ctx, a.AuthConfig())
		if err != nil {
			dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "")
			return nil
		}
		tok, err := resolved.TokenSource.Token()
		if err != nil {
			dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
			return nil
		}

		profile, err := profiles.LoadByName(*profileName)
		if err != nil {
			dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
			return nil
		}

		client := ca.NewClient(nil)
		result, err := datacloud.DatabasesList(ctx, client, tok.AccessToken, profile)
		if err != nil {
			dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
			return nil
		}

		return a.Render(format, result)
	}
}

func findOrCreateCmd(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	cmd := &cobra.Command{Use: name, Short: name + " commands"}
	parent.AddCommand(cmd)
	return cmd
}

func findCmd(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}
