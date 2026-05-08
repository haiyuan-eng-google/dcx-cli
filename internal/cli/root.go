// Package cli builds the dcx command tree using cobra.
//
// Global flags are defined on the root command and propagated to all
// subcommands. The contract registry is the single source of truth for
// all registered commands.
package cli

import (
	"fmt"

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
	OutputFields    string
	Select          string
	Retry           int
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Resolve --select / --output-fields conflict.
			selectSet := cmd.Flags().Changed("select") || cmd.InheritedFlags().Changed("select")
			fieldsSet := cmd.Flags().Changed("output-fields") || cmd.InheritedFlags().Changed("output-fields")
			if selectSet && fieldsSet {
				return fmt.Errorf("--select and --output-fields cannot be used together")
			}
			if selectSet {
				opts.OutputFields = opts.Select
			}
			return nil
		},
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
	pf.StringVar(&opts.OutputFields, "output-fields", "", "Comma-separated list of fields to include in output (e.g., name,schema)")
	pf.StringVar(&opts.Select, "select", "", "Alias for --output-fields (cannot be used together)")
	pf.IntVar(&opts.Retry, "retry", 0, "Number of retries on 429/transport errors (0=no retry, 3=recommended)")

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

	// Register generate-skills command (must be after meta commands).
	app.addGenerateSkillsCommand()

	// Register Discovery-driven commands.
	app.registerBigQueryDiscoveryCommands()
	app.registerDataCloudDiscoveryCommands()

	// Register static commands.
	app.registerJobsQueryCommand()

	// Register profiles commands.
	app.addProfilesCommands()

	// Register CA commands.
	app.addCACommands()

	// Register static Spanner commands.
	app.registerSpannerUpdateDdlCommand()
	app.registerSpannerOperationsCommands()

	// Register Data Cloud helper commands (schema describe, databases list via QueryData).
	app.registerDataCloudHelperCommands()

	// Register Looker Admin SDK commands (explores, dashboards).
	app.registerLookerSDKCommands()

	// Register MCP commands.
	app.addMCPCommands()

	// Register shell completion command.
	app.addCompletionCommand()

	// Register interactive REPL (not in contract registry — human-only).
	app.addREPLCommand()

	return app
}

// OutputFormat parses the current --format flag value into an output.Format.
func (a *App) OutputFormat() (output.Format, error) {
	return output.ParseFormat(a.Opts.Format)
}

// Render outputs a value using the configured format and field filtering.
func (a *App) Render(format output.Format, value interface{}) error {
	return output.RenderFiltered(format, value, a.Opts.OutputFields)
}

// AuthConfig returns an auth.Config from the current global flags.
func (a *App) AuthConfig() auth.Config {
	return auth.Config{
		Token:           a.Opts.Token,
		CredentialsFile: a.Opts.CredentialsFile,
	}
}
