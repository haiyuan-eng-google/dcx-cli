package discovery

import (
	"context"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
	"github.com/spf13/cobra"
)

// CLIOpts provides access to global flag values for Discovery commands.
type CLIOpts struct {
	Format          *string
	ProjectID       *string
	DatasetID       *string
	Location        *string
	Token           *string
	CredentialsFile *string
	DryRun          *bool
}

// RegisterCommands parses a Discovery Document and registers all allowed
// commands as cobra subcommands on the given parent command.
func RegisterCommands(
	parent *cobra.Command,
	registry *contracts.Registry,
	docJSON []byte,
	svc *ServiceConfig,
	opts *CLIOpts,
) error {
	commands, err := Parse(docJSON, svc)
	if err != nil {
		return err
	}

	executor := NewExecutor(nil)

	for _, genCmd := range commands {
		registerOneCommand(parent, registry, genCmd, svc, opts, executor)
	}

	return nil
}

func registerOneCommand(
	parent *cobra.Command,
	registry *contracts.Registry,
	genCmd GeneratedCommand,
	svc *ServiceConfig,
	opts *CLIOpts,
	executor *Executor,
) {
	// Parse command path: e.g. "datasets list" -> ["datasets", "list"]
	// or "spanner instances list" -> ["spanner", "instances", "list"]
	parts := strings.Fields(genCmd.CommandPath)

	// Find or create intermediate commands.
	current := parent
	for i := 0; i < len(parts)-1; i++ {
		found := false
		for _, child := range current.Commands() {
			if child.Name() == parts[i] {
				current = child
				found = true
				break
			}
		}
		if !found {
			groupCmd := &cobra.Command{
				Use:   parts[i],
				Short: parts[i] + " commands",
			}
			current.AddCommand(groupCmd)
			current = groupCmd
		}
	}

	// Create the leaf command.
	leafName := parts[len(parts)-1]
	cmd := genCmd // capture for closure

	// Build command-specific flags storage.
	flagValues := make(map[string]*string, len(cmd.CommandFlags))
	pageToken := ""
	pageAll := false

	leafCmd := &cobra.Command{
		Use:   leafName,
		Short: cmd.Method.Description,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			format, err := output.ParseFormat(*opts.Format)
			if err != nil {
				return err
			}

			authCfg := auth.Config{
				Token:           *opts.Token,
				CredentialsFile: *opts.CredentialsFile,
			}

			globalFlags := map[string]string{
				"project-id": *opts.ProjectID,
				"dataset-id": *opts.DatasetID,
				"location":   *opts.Location,
			}

			// Separate command-specific flags into path params and query params.
			queryParams := make(map[string]string)
			for name, val := range flagValues {
				if *val != "" {
					if isCommandPathFlag(name, cmd) {
						globalFlags[name] = *val
					} else {
						queryParams[name] = *val
					}
				}
			}
			if pageToken != "" {
				queryParams["pageToken"] = pageToken
			}

			return executor.Execute(
				context.Background(),
				cmd,
				authCfg,
				globalFlags,
				queryParams,
				format,
				pageAll,
			)
		},
	}

	// Register command-specific flags.
	for _, flag := range cmd.CommandFlags {
		val := new(string)
		flagValues[flag.Name] = val
		flagName := camelToKebab(flag.Name)
		leafCmd.Flags().StringVar(val, flagName, "", flag.Description)
	}

	// Add pagination flags only for list commands whose API supports pagination.
	if cmd.Method.Action == "list" && methodSupportsPagination(cmd) {
		leafCmd.Flags().StringVar(&pageToken, "page-token", "", "Page token for pagination")
		leafCmd.Flags().BoolVar(&pageAll, "page-all", false, "Fetch all pages")
	}

	current.AddCommand(leafCmd)

	// Register contract.
	var contractFlags []contracts.FlagContract
	for _, flag := range cmd.CommandFlags {
		contractFlags = append(contractFlags, contracts.FlagContract{
			Name:        camelToKebab(flag.Name),
			Type:        flag.Type,
			Description: flag.Description,
			Required:    flag.Required,
		})
	}
	if cmd.Method.Action == "list" && methodSupportsPagination(cmd) {
		contractFlags = append(contractFlags,
			contracts.FlagContract{Name: "page-token", Type: "string", Description: "Page token for pagination"},
			contracts.FlagContract{Name: "page-all", Type: "bool", Description: "Fetch all pages"},
		)
	}

	registry.Register(contracts.BuildContract(
		genCmd.CommandPath,
		svc.Domain,
		cmd.Method.Description,
		contractFlags,
		false, // Discovery GET commands are not mutations
		false, // No dry-run for Discovery commands
	))
}

// methodSupportsPagination returns true if the API method has a pageToken
// parameter, indicating it supports pagination.
func methodSupportsPagination(cmd GeneratedCommand) bool {
	_, ok := cmd.Method.Parameters["pageToken"]
	return ok
}

// isCommandPathFlag checks if a flag name is a path parameter, checking both
// the Discovery method parameters and the inferred flatPath flags.
func isCommandPathFlag(name string, cmd GeneratedCommand) bool {
	if param, ok := cmd.Method.Parameters[name]; ok && param.Location == "path" {
		return true
	}
	for _, f := range cmd.CommandFlags {
		if f.Name == name && f.Location == "path" {
			return true
		}
	}
	return false
}
