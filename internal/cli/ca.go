package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/ca"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func (a *App) addCACommands() {
	caCmd := &cobra.Command{
		Use:   "ca",
		Short: "Conversational Analytics commands",
	}

	caCmd.AddCommand(a.caAskCmd())
	caCmd.AddCommand(a.caCreateAgentCmd())
	caCmd.AddCommand(a.caListAgentsCmd())
	caCmd.AddCommand(a.caAddVerifiedQueryCmd())
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
	a.Registry.Register(contracts.BuildContract(
		"ca create-agent", "ca",
		"Create a BigQuery data agent with table refs and optional verified queries",
		[]contracts.FlagContract{
			{Name: "name", Type: "string", Description: "Agent ID: lowercase letters, digits, hyphens; must start with a letter (regex: ^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$)", Required: true},
			{Name: "tables", Type: "string", Description: "Comma-separated fully qualified table refs", Required: true},
			{Name: "views", Type: "string", Description: "Comma-separated view refs as additional data sources"},
			{Name: "verified-queries", Type: "string", Description: "Path to verified queries YAML file"},
			{Name: "instructions", Type: "string", Description: "System instructions for the agent"},
		},
		true, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"ca list-agents", "ca",
		"List data agents in the current project",
		nil, false, false,
	))
	a.Registry.Register(contracts.BuildContract(
		"ca add-verified-query", "ca",
		"Add a verified query to an existing data agent",
		[]contracts.FlagContract{
			{Name: "agent", Type: "string", Description: "Data agent name", Required: true},
			{Name: "question", Type: "string", Description: "Natural language question", Required: true},
			{Name: "query", Type: "string", Description: "SQL query", Required: true},
		},
		true, false,
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

func (a *App) caCreateAgentCmd() *cobra.Command {
	var name, tables, views, verifiedQueriesPath, instructions string

	cmd := &cobra.Command{
		Use:   "create-agent",
		Short: "Create a BigQuery data agent with table refs and optional verified queries",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if name == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --name is missing", "")
				return nil
			}
			if !isValidAgentID(name) {
				dcxerrors.Emit(dcxerrors.InvalidIdentifier,
					fmt.Sprintf("invalid agent ID %q: must match ^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$", name),
					"Use lowercase letters, digits, and hyphens; must start with a letter")
				return nil
			}
			if tables == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --tables is missing", "")
				return nil
			}

			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
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

			projectID := a.Opts.ProjectID
			if projectID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --project-id is missing", "")
				return nil
			}

			opts := ca.CreateAgentOpts{
				AgentID:      name,
				DisplayName:  name,
				Tables:       splitCSV(tables),
				Instructions: instructions,
			}
			if views != "" {
				opts.Views = splitCSV(views)
			}
			if verifiedQueriesPath != "" {
				eqs, err := loadExampleQueries(verifiedQueriesPath)
				if err != nil {
					dcxerrors.Emit(dcxerrors.InvalidConfig, fmt.Sprintf("loading verified queries: %v", err), "")
					return nil
				}
				opts.ExampleQueries = eqs
			}

			client := ca.NewClient(nil)
			result, err := client.CreateAgent(ctx, tok.AccessToken, projectID, a.Opts.Location, opts)
			if err != nil {
				dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
				return nil
			}

			return output.Render(format, result)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent ID: lowercase, digits, hyphens; starts with letter (required)")
	cmd.Flags().StringVar(&tables, "tables", "", "Comma-separated fully qualified table refs (required)")
	cmd.Flags().StringVar(&views, "views", "", "Comma-separated view refs")
	cmd.Flags().StringVar(&verifiedQueriesPath, "verified-queries", "", "Path to verified queries YAML file")
	cmd.Flags().StringVar(&instructions, "instructions", "", "System instructions for the agent")

	return cmd
}

func (a *App) caListAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-agents",
		Short: "List data agents in the current project",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
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

			projectID := a.Opts.ProjectID
			if projectID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --project-id is missing", "")
				return nil
			}

			client := ca.NewClient(nil)
			result, err := client.ListAgents(ctx, tok.AccessToken, projectID, a.Opts.Location)
			if err != nil {
				dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
				return nil
			}

			return output.Render(format, result)
		},
	}
}

func (a *App) caAddVerifiedQueryCmd() *cobra.Command {
	var agentName, question, query string

	cmd := &cobra.Command{
		Use:   "add-verified-query",
		Short: "Add a verified query to an existing data agent",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if agentName == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --agent is missing", "")
				return nil
			}
			if question == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --question is missing", "")
				return nil
			}
			if query == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --query is missing", "")
				return nil
			}

			format, err := a.OutputFormat()
			if err != nil {
				dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
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

			projectID := a.Opts.ProjectID
			if projectID == "" {
				dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --project-id is missing", "")
				return nil
			}

			client := ca.NewClient(nil)
			result, err := client.AddVerifiedQuery(ctx, tok.AccessToken, projectID, a.Opts.Location, ca.PatchAgentOpts{
				AgentName: agentName,
				ExampleQueries: []ca.ExampleQuery{
					{NaturalLanguageQuestion: question, SQLQuery: query},
				},
			})
			if err != nil {
				dcxerrors.Emit(dcxerrors.APIError, err.Error(), "")
				return nil
			}

			return output.Render(format, result)
		},
	}

	cmd.Flags().StringVar(&agentName, "agent", "", "Data agent name (required)")
	cmd.Flags().StringVar(&question, "question", "", "Natural language question (required)")
	cmd.Flags().StringVar(&query, "query", "", "SQL query (required)")

	return cmd
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// loadExampleQueries reads a YAML file containing verified queries.
// The YAML uses the user-facing "verified_queries" key with "question"
// and "query" fields, which map to ExampleQuery's YAML tags.
func loadExampleQueries(path string) ([]ca.ExampleQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var doc struct {
		VerifiedQueries []ca.ExampleQuery `yaml:"verified_queries"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return doc.VerifiedQueries, nil
}

// agentIDPattern matches the documented dataAgentId format.
var agentIDPattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// isValidAgentID checks if an agent ID matches the API-documented format.
func isValidAgentID(id string) bool {
	return agentIDPattern.MatchString(id)
}
