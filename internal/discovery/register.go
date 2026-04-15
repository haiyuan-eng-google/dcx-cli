package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/auth"
	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
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
	boolFlagValues := make(map[string]*bool)
	pageToken := ""
	pageAll := false
	bodyFlag := ""
	forceFlag := false
	isMutation := cmd.Method.IsMutation()

	leafCmd := &cobra.Command{
		Use:   leafName,
		Short: cmd.Method.Description,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			format, err := output.ParseFormat(*opts.Format)
			if err != nil {
				return err
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
			for name, val := range boolFlagValues {
				if *val {
					queryParams[name] = "true"
				}
			}
			if pageToken != "" {
				queryParams["pageToken"] = pageToken
			}

			// For mutations: validate body, check confirmation, handle dry-run.
			if isMutation {
				return executeMutationFlow(executor, cmd, *opts, globalFlags, queryParams, bodyFlag, forceFlag, format)
			}

			authCfg := auth.Config{
				Token:           *opts.Token,
				CredentialsFile: *opts.CredentialsFile,
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

	// Register command-specific flags with correct types.
	for _, flag := range cmd.CommandFlags {
		flagName := camelToKebab(flag.Name)
		if flag.Type == "boolean" {
			val := new(bool)
			boolFlagValues[flag.Name] = val
			leafCmd.Flags().BoolVar(val, flagName, false, flag.Description)
		} else {
			val := new(string)
			flagValues[flag.Name] = val
			leafCmd.Flags().StringVar(val, flagName, "", flag.Description)
		}
	}

	// Add pagination flags only for list commands whose API supports pagination.
	if cmd.Method.Action == "list" && methodSupportsPagination(cmd) {
		leafCmd.Flags().StringVar(&pageToken, "page-token", "", "Page token for pagination")
		leafCmd.Flags().BoolVar(&pageAll, "page-all", false, "Fetch all pages")
	}

	// Add mutation-specific flags.
	if isMutation {
		if cmd.Method.AcceptsBody() {
			leafCmd.Flags().StringVar(&bodyFlag, "body", "", "JSON request body (or @file.json)")
		}
		if cmd.Method.RequiresConfirmation() {
			leafCmd.Flags().BoolVar(&forceFlag, "force", false, "Skip confirmation prompt")
		}
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
	if isMutation && cmd.Method.AcceptsBody() {
		contractFlags = append(contractFlags,
			contracts.FlagContract{Name: "body", Type: "string", Description: "JSON request body (or @file.json)", Required: true},
		)
	}
	if isMutation && cmd.Method.RequiresConfirmation() {
		contractFlags = append(contractFlags,
			contracts.FlagContract{Name: "force", Type: "bool", Description: "Skip confirmation prompt"},
		)
	}

	registry.Register(contracts.BuildContract(
		genCmd.CommandPath,
		svc.Domain,
		cmd.Method.Description,
		contractFlags,
		isMutation,
		isMutation, // mutations support --dry-run
	))
}

// executeMutationFlow handles the full lifecycle of a mutation command:
// body validation → auth → path resolution → dry-run or confirmation → execute.
func executeMutationFlow(
	executor *Executor,
	cmd GeneratedCommand,
	opts CLIOpts,
	globalFlags map[string]string,
	queryParams map[string]string,
	bodyFlag string,
	force bool,
	format output.Format,
) error {
	// 1. Load and validate body if the method accepts one.
	var bodyBytes []byte
	if cmd.Method.AcceptsBody() {
		if bodyFlag == "" {
			dcxerrors.Emit(dcxerrors.MissingArgument, "required flag --body is missing", "Provide JSON body or @file.json")
			return nil
		}
		var err error
		bodyBytes, err = LoadBody(bodyFlag)
		if err != nil {
			dcxerrors.Emit(dcxerrors.InvalidConfig, err.Error(), "")
			return nil
		}
	}

	// 2. Resolve auth (validates config shape even for dry-run).
	ctx := context.Background()
	authCfg := auth.Config{
		Token:           *opts.Token,
		CredentialsFile: *opts.CredentialsFile,
	}
	resolved, err := auth.Resolve(ctx, authCfg)
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, err.Error(), "Run 'dcx auth check' to verify credentials")
		return nil
	}

	// 3. Resolve path params.
	pathParams, err := ResolvePathParams(cmd, globalFlags)
	if err != nil {
		dcxerrors.Emit(dcxerrors.MissingArgument, err.Error(), "")
		return nil
	}
	if validErr := validateRequiredParams(cmd, globalFlags); validErr != nil {
		dcxerrors.Emit(dcxerrors.MissingArgument, validErr.Error(), "")
		return nil
	}

	// 4. Dry-run: show what would be sent, skip network call.
	if opts.DryRun != nil && *opts.DryRun {
		resolvedURL, err := ResolveURL(cmd, pathParams, queryParams)
		if err != nil {
			dcxerrors.Emit(dcxerrors.Internal, err.Error(), "")
			return nil
		}
		return RenderDryRun(format, cmd, resolvedURL, bodyBytes)
	}

	// 5. Confirmation for destructive operations.
	if cmd.Method.RequiresConfirmation() {
		if err := ConfirmDelete(cmd, force); err != nil {
			dcxerrors.Emit(dcxerrors.MissingArgument, err.Error(), "Use --force to skip confirmation")
			return nil
		}
	}

	// 6. Get token and execute.
	tok, err := resolved.TokenSource.Token()
	if err != nil {
		dcxerrors.Emit(dcxerrors.AuthError, fmt.Sprintf("failed to obtain token: %v", err), "")
		return nil
	}

	if execErr := executor.ExecuteMutation(cmd, pathParams, queryParams, tok.AccessToken, bodyBytes, format); execErr != nil {
		dcxerrors.Emit(dcxerrors.APIError, execErr.Error(), "")
		return nil
	}

	return nil
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
