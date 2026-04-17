package cli

import (
	"context"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/assets"
	"github.com/haiyuan-eng-google/dcx-cli/internal/bigquery"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/haiyuan-eng-google/dcx-cli/internal/discovery"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/spf13/cobra"
)

// registerBigQueryDiscoveryCommands registers BigQuery dynamic commands
// (datasets list/get, tables list/get) from the embedded Discovery Document.
func (a *App) registerBigQueryDiscoveryCommands() error {
	svc := discovery.BigQueryConfig()
	opts := a.discoveryOpts()
	return discovery.RegisterCommands(a.Root, a.Registry, assets.BigQueryDiscovery, svc, opts)
}

// registerJobsQueryCommand registers the static `jobs query` command.
func (a *App) registerJobsQueryCommand() {
	// Find or create the "jobs" group command.
	var jobsCmd *cobra.Command
	for _, child := range a.Root.Commands() {
		if child.Name() == "jobs" {
			jobsCmd = child
			break
		}
	}
	if jobsCmd == nil {
		jobsCmd = &cobra.Command{
			Use:   "jobs",
			Short: "BigQuery job commands",
		}
		a.Root.AddCommand(jobsCmd)
	}

	var queryStr string
	var maxResults int

	queryCmd := &cobra.Command{
		Use:   "query",
		Short: "Run a BigQuery SQL query",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "Use --format with: "+strings.Join(output.FormatNames(), ", "))
				return nil
			}

			return bigquery.ExecuteQuery(
				context.Background(),
				a.AuthConfig(),
				a.Opts.ProjectID,
				queryStr,
				a.Opts.Location,
				a.Opts.DryRun,
				maxResults,
				format,
				a.Opts.OutputFields,
			)
		},
	}

	queryCmd.Flags().StringVar(&queryStr, "query", "", "SQL query to execute (required)")
	queryCmd.Flags().IntVar(&maxResults, "max-results", 0, "Maximum number of results to return")

	jobsCmd.AddCommand(queryCmd)

	// Register contract.
	a.Registry.Register(contracts.BuildContract(
		"jobs query", "bigquery",
		"Run a BigQuery SQL query",
		[]contracts.FlagContract{
			{Name: "query", Type: "string", Description: "SQL query to execute", Required: true},
			{Name: "max-results", Type: "int", Description: "Maximum number of results to return"},
		},
		false, // not a mutation (reads data)
		true,  // supports --dry-run
	))
}

// discoveryOpts creates a CLIOpts from the app's global options.
func (a *App) discoveryOpts() *discovery.CLIOpts {
	return &discovery.CLIOpts{
		Format:          &a.Opts.Format,
		ProjectID:       &a.Opts.ProjectID,
		DatasetID:       &a.Opts.DatasetID,
		Location:        &a.Opts.Location,
		Token:           &a.Opts.Token,
		CredentialsFile: &a.Opts.CredentialsFile,
		DryRun:          &a.Opts.DryRun,
		OutputFields:    &a.Opts.OutputFields,
	}
}
