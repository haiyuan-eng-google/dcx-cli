// Package cli builds the dcx command tree using cobra.
//
// Global flags are defined on the root command and propagated to all
// subcommands. The contract registry is the single source of truth for
// all registered commands.
package cli

import (
	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/spf13/cobra"
)

// GlobalOpts holds the parsed values of global flags.
type GlobalOpts struct {
	Format          string
	ProjectID       string
	DatasetID       string
	Location        string
	Token           string
	CredentialsFile string
	DryRun          bool
}

// App holds the assembled CLI application state.
type App struct {
	Root     *cobra.Command
	Registry *contracts.Registry
	Opts     *GlobalOpts
}

// NewApp creates the root dcx command with global flags and meta subcommands.
func NewApp() *App {
	opts := &GlobalOpts{}
	registry := contracts.NewRegistry()

	root := &cobra.Command{
		Use:   "dcx",
		Short: "Agent-native Data Cloud CLI",
		Long: `dcx — Agent-Native Data Cloud CLI

One binary for BigQuery, Spanner, AlloyDB, Cloud SQL, and Looker.
Structured output, typed errors, and an MCP bridge for AI agents.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags.
	pf := root.PersistentFlags()
	pf.StringVar(&opts.Format, "format", "json", "Output format (json, json-minified, table, text)")
	pf.StringVar(&opts.ProjectID, "project-id", "", "Google Cloud project ID")
	pf.StringVar(&opts.DatasetID, "dataset-id", "", "BigQuery dataset ID")
	pf.StringVar(&opts.Location, "location", "", "Google Cloud location/region")
	pf.StringVar(&opts.Token, "token", "", "Bearer access token (overrides all other auth)")
	pf.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to service account JSON credentials file")
	pf.BoolVar(&opts.DryRun, "dry-run", false, "Validate and show what would be sent without executing")

	app := &App{
		Root:     root,
		Registry: registry,
		Opts:     opts,
	}

	// Register meta subcommands.
	app.addMetaCommands()

	// Register auth subcommands.
	app.addAuthCommands()

	// Self-register meta contracts.
	registry.Register(contracts.MetaCommandsContract())
	registry.Register(contracts.MetaDescribeContract())

	// Register Discovery-driven commands.
	app.registerBigQueryDiscoveryCommands()
	app.registerDataCloudDiscoveryCommands()

	// Register static commands.
	app.registerJobsQueryCommand()

	// Register profiles commands.
	app.addProfilesCommands()

	// Register MCP commands.
	app.addMCPCommands()

	return app
}

// OutputFormat parses the current --format flag value into an output.Format.
func (a *App) OutputFormat() (output.Format, error) {
	return output.ParseFormat(a.Opts.Format)
}

// AuthConfig returns an auth.Config from the current global flags.
func (a *App) AuthConfig() auth.Config {
	return auth.Config{
		Token:           a.Opts.Token,
		CredentialsFile: a.Opts.CredentialsFile,
	}
}
