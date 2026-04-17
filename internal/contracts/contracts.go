// Package contracts provides the single contract model for dcx.
//
// Every command — discovery-driven, static, or helper — registers through
// the same contract interface. This drives CLI registration, meta commands,
// meta describe, MCP tool schemas, and skill generation.
package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// FlagContract describes a single flag on a command.
type FlagContract struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "bool", "int"
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     string `json:"default,omitempty"`
}

// CommandContract is the machine-readable specification for a single command.
// One instance of this model drives CLI registration, meta describe output,
// MCP tool schemas, and skill generation.
type CommandContract struct {
	ContractVersion string            `json:"contract_version"`
	Command         string            `json:"command"` // e.g. "dcx datasets list"
	Domain          string            `json:"domain"`  // e.g. "bigquery", "spanner"
	Description     string            `json:"description"`
	Flags           []FlagContract    `json:"flags"`
	ExitCodes       map[string]string `json:"exit_codes"`
	SupportsDryRun  bool              `json:"supports_dry_run"`
	IsMutation      bool              `json:"is_mutation"`
	Formats         []string          `json:"formats"` // supported output formats
}

// Registry holds all registered command contracts.
type Registry struct {
	contracts map[string]*CommandContract
}

// NewRegistry creates an empty contract registry.
func NewRegistry() *Registry {
	return &Registry{
		contracts: make(map[string]*CommandContract),
	}
}

// Register adds a command contract to the registry.
func (r *Registry) Register(c *CommandContract) {
	r.contracts[c.Command] = c
}

// Get returns a contract by full command path (e.g. "dcx datasets list").
func (r *Registry) Get(command string) (*CommandContract, bool) {
	c, ok := r.contracts[command]
	return c, ok
}

// All returns all registered contracts sorted by command name.
func (r *Registry) All() []*CommandContract {
	result := make([]*CommandContract, 0, len(r.contracts))
	for _, c := range r.contracts {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Command < result[j].Command
	})
	return result
}

// CommandSummary is the compact representation used by `meta commands`.
type CommandSummary struct {
	Command     string `json:"command"`
	Domain      string `json:"domain"`
	Description string `json:"description"`
}

// ListCommands returns a summary list for `meta commands` output.
func (r *Registry) ListCommands() []CommandSummary {
	all := r.All()
	summaries := make([]CommandSummary, len(all))
	for i, c := range all {
		summaries[i] = CommandSummary{
			Command:     c.Command,
			Domain:      c.Domain,
			Description: c.Description,
		}
	}
	return summaries
}

// Describe returns the full contract for `meta describe` output.
// The command argument can be the full path ("dcx datasets list") or
// just the subcommand portion ("datasets list").
func (r *Registry) Describe(command string) (*CommandContract, error) {
	// Try exact match first.
	if c, ok := r.contracts[command]; ok {
		return c, nil
	}
	// Try with "dcx " prefix.
	prefixed := "dcx " + command
	if c, ok := r.contracts[prefixed]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("unknown command: %s", command)
}

// DefaultExitCodes returns the standard exit code map for a command.
func DefaultExitCodes(isMutation bool) map[string]string {
	codes := map[string]string{
		"0": "success",
		"2": "api_error",
		"3": "auth_error",
		"4": "not_found",
	}
	if isMutation {
		codes["5"] = "conflict"
	}
	return codes
}

// DefaultFormats returns the standard format list.
func DefaultFormats() []string {
	return []string{"json", "json-minified", "table", "text"}
}

// GlobalFlags returns the contract definitions for global flags.
func GlobalFlags() []FlagContract {
	return []FlagContract{
		{Name: "format", Type: "string", Description: "Output format (json, json-minified, table, text)", Default: "json"},
		{Name: "project-id", Type: "string", Description: "Google Cloud project ID"},
		{Name: "dataset-id", Type: "string", Description: "BigQuery dataset ID"},
		{Name: "location", Type: "string", Description: "Google Cloud location/region"},
		{Name: "token", Type: "string", Description: "Bearer access token (overrides all other auth)"},
		{Name: "credentials-file", Type: "string", Description: "Path to service account JSON credentials file"},
		{Name: "output-fields", Type: "string", Description: "Comma-separated list of fields to include in output"},
	}
}

// BuildContract creates a CommandContract with standard defaults.
func BuildContract(command, domain, description string, flags []FlagContract, isMutation, supportsDryRun bool) *CommandContract {
	allFlags := append(GlobalFlags(), flags...)
	return &CommandContract{
		ContractVersion: "1",
		Command:         "dcx " + command,
		Domain:          domain,
		Description:     description,
		Flags:           allFlags,
		ExitCodes:       DefaultExitCodes(isMutation),
		SupportsDryRun:  supportsDryRun,
		IsMutation:      isMutation,
		Formats:         DefaultFormats(),
	}
}

// MetaCommandsContract returns the contract for `meta commands` itself.
func MetaCommandsContract() *CommandContract {
	return &CommandContract{
		ContractVersion: "1",
		Command:         "dcx meta commands",
		Domain:          "meta",
		Description:     "List all registered commands and their domains",
		Flags:           GlobalFlags(),
		ExitCodes:       DefaultExitCodes(false),
		SupportsDryRun:  false,
		IsMutation:      false,
		Formats:         DefaultFormats(),
	}
}

// MetaDescribeContract returns the contract for `meta describe` itself.
func MetaDescribeContract() *CommandContract {
	return &CommandContract{
		ContractVersion: "1",
		Command:         "dcx meta describe",
		Domain:          "meta",
		Description:     "Show the full contract for a given command",
		Flags: append(GlobalFlags(), FlagContract{
			Name:        "command",
			Type:        "string",
			Description: "The command to describe (positional args)",
			Required:    true,
		}),
		ExitCodes:      DefaultExitCodes(false),
		SupportsDryRun: false,
		IsMutation:     false,
		Formats:        DefaultFormats(),
	}
}

// FindByPrefix returns all contracts whose command starts with the given prefix.
func (r *Registry) FindByPrefix(prefix string) []*CommandContract {
	var result []*CommandContract
	normalized := strings.TrimSpace(prefix)
	for _, c := range r.All() {
		if strings.HasPrefix(c.Command, "dcx "+normalized) || strings.HasPrefix(c.Command, normalized) {
			result = append(result, c)
		}
	}
	return result
}
