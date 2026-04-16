package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
	"github.com/spf13/cobra"
)

func (a *App) addGenerateSkillsCommand() {
	var outDir string
	var domains []string

	// Find or create the "meta" group command.
	var metaCmd *cobra.Command
	for _, child := range a.Root.Commands() {
		if child.Name() == "meta" {
			metaCmd = child
			break
		}
	}
	if metaCmd == nil {
		return
	}

	cmd := &cobra.Command{
		Use:   "generate-skills",
		Short: "Generate SKILL.md files from the command contract registry",
		Long: `Generate SKILL.md files for each domain from the machine-readable
contract registry. Each skill file includes command routing tables,
flag details, and decision rules derived from the live contracts.

By default writes to stdout. Use --out-dir to write files to a directory.`,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			all := a.Registry.All()
			if len(all) == 0 {
				dcxerrors.Emit(dcxerrors.Internal, "no contracts registered", "")
				return nil
			}

			// Group by domain.
			byDomain := groupByDomain(all)

			// Filter domains if specified.
			if len(domains) > 0 {
				filtered := make(map[string][]*contracts.CommandContract)
				for _, d := range domains {
					if cmds, ok := byDomain[d]; ok {
						filtered[d] = cmds
					}
				}
				byDomain = filtered
			}

			if outDir != "" {
				return writeSkillFiles(byDomain, outDir)
			}

			// Write to stdout.
			for _, domain := range sortedDomainKeys(byDomain) {
				content := renderSkill(domain, byDomain[domain])
				fmt.Println(content)
				fmt.Println("---")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&outDir, "out-dir", "", "Directory to write SKILL.md files (default: stdout)")
	cmd.Flags().StringSliceVar(&domains, "domains", nil, "Comma-separated domains to generate (default: all)")

	metaCmd.AddCommand(cmd)

	a.Registry.Register(contracts.BuildContract(
		"meta generate-skills", "meta",
		"Generate SKILL.md files from the command contract registry",
		[]contracts.FlagContract{
			{Name: "out-dir", Type: "string", Description: "Directory to write SKILL.md files"},
			{Name: "domains", Type: "string", Description: "Comma-separated domains to generate"},
		},
		false, false,
	))
}

func groupByDomain(all []*contracts.CommandContract) map[string][]*contracts.CommandContract {
	result := make(map[string][]*contracts.CommandContract)
	for _, c := range all {
		// Skip meta/internal domains from skill generation.
		if c.Domain == "meta" || c.Domain == "mcp" || c.Domain == "cli" {
			continue
		}
		result[c.Domain] = append(result[c.Domain], c)
	}
	return result
}

func sortedDomainKeys(m map[string][]*contracts.CommandContract) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeSkillFiles(byDomain map[string][]*contracts.CommandContract, outDir string) error {
	for _, domain := range sortedDomainKeys(byDomain) {
		content := renderSkill(domain, byDomain[domain])
		dir := filepath.Join(outDir, "dcx-"+domain)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d commands)\n", path, len(byDomain[domain]))
	}
	return nil
}

func renderSkill(domain string, cmds []*contracts.CommandContract) string {
	var b strings.Builder

	domainTitle := domainDisplayName(domain)

	// Frontmatter.
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("name: dcx-%s\n", domain))
	b.WriteString(fmt.Sprintf("description: %s commands for dcx — auto-generated from the contract registry.\n", domainTitle))
	b.WriteString("---\n\n")

	// Separate reads and mutations.
	var reads, mutations []*contracts.CommandContract
	for _, c := range cmds {
		if c.IsMutation {
			mutations = append(mutations, c)
		} else {
			reads = append(reads, c)
		}
	}

	// When to use.
	b.WriteString("## When to use this skill\n\n")
	b.WriteString(fmt.Sprintf("Use when the user wants to work with %s resources via dcx.\n\n", domainTitle))

	// Command routing table.
	b.WriteString("## Commands\n\n")
	if len(reads) > 0 {
		b.WriteString("### Read commands\n\n")
		b.WriteString("| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")
		for _, c := range reads {
			desc := truncateDesc(c.Description, 80)
			b.WriteString(fmt.Sprintf("| `%s` | %s |\n", c.Command, desc))
		}
		b.WriteString("\n")
	}

	if len(mutations) > 0 {
		b.WriteString("### Mutation commands\n\n")
		b.WriteString("| Command | Description | Flags |\n")
		b.WriteString("|---------|-------------|-------|\n")
		for _, c := range mutations {
			desc := truncateDesc(c.Description, 60)
			mutFlags := mutationFlagSummary(c)
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", c.Command, desc, mutFlags))
		}
		b.WriteString("\n")
		b.WriteString("All mutation commands support `--dry-run` to preview the request without executing.\n\n")
	}

	// Flag reference for commands with non-global flags.
	b.WriteString("## Flag reference\n\n")
	for _, c := range cmds {
		cmdFlags := nonGlobalFlags(c)
		if len(cmdFlags) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("### `%s`\n\n", c.Command))
		b.WriteString("| Flag | Type | Required | Description |\n")
		b.WriteString("|------|------|----------|-------------|\n")
		for _, f := range cmdFlags {
			req := ""
			if f.Required {
				req = "Yes"
			}
			b.WriteString(fmt.Sprintf("| `--%s` | %s | %s | %s |\n", f.Name, f.Type, req, truncateDesc(f.Description, 60)))
		}
		b.WriteString("\n")
	}

	// Decision rules.
	b.WriteString("## Decision rules\n\n")
	b.WriteString(fmt.Sprintf("- All %s commands require `--project-id` (or `DCX_PROJECT`)\n", domainTitle))
	b.WriteString("- Use `--format json` for automation, `--format table` for visual scanning\n")
	if len(mutations) > 0 {
		b.WriteString("- Mutation commands require `--body` (POST) or `--force` (DELETE)\n")
		b.WriteString("- Use `--dry-run` to preview mutations before executing\n")
		b.WriteString("- DELETE commands require `--force` in non-interactive environments\n")
	}
	b.WriteString("- Use `dcx meta describe <command>` for the full contract of any command\n")

	return b.String()
}

func domainDisplayName(domain string) string {
	switch domain {
	case "bigquery":
		return "BigQuery"
	case "spanner":
		return "Spanner"
	case "alloydb":
		return "AlloyDB"
	case "cloudsql":
		return "Cloud SQL"
	case "looker":
		return "Looker"
	case "ca":
		return "Conversational Analytics"
	case "auth":
		return "Authentication"
	case "profiles":
		return "Profiles"
	default:
		return strings.Title(domain)
	}
}

func truncateDesc(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func mutationFlagSummary(c *contracts.CommandContract) string {
	var parts []string
	for _, f := range c.Flags {
		if f.Name == "body" {
			parts = append(parts, "`--body`")
		} else if f.Name == "force" {
			parts = append(parts, "`--force`")
		}
	}
	return strings.Join(parts, ", ")
}

func nonGlobalFlags(c *contracts.CommandContract) []contracts.FlagContract {
	globalFlags := map[string]bool{
		"format": true, "project-id": true, "dataset-id": true,
		"location": true, "token": true, "credentials-file": true,
		"dry-run": true,
	}
	var result []contracts.FlagContract
	for _, f := range c.Flags {
		if !globalFlags[f.Name] {
			result = append(result, f)
		}
	}
	return result
}
