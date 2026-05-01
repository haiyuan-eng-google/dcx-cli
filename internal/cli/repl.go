package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/peterh/liner"
	"github.com/spf13/cobra"
)

// replContext holds session state across REPL commands.
type replContext struct {
	ProjectID string
	DatasetID string
	Location  string
	Profile   string
	Agent     string
	Format    string // session output format (default: text)
}

// blockedCommands are interactive/server commands that shouldn't run in the REPL.
var blockedCommands = map[string]bool{
	"repl":       true,
	"mcp":        true,
	"completion": true,
}

func (a *App) addREPLCommand() {
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Start an interactive Data Cloud session",
		Long: `Start an interactive REPL for iterative Data Cloud work.

Any dcx command can be run by typing it without the "dcx" prefix.
Bare SELECT/WITH SQL is auto-routed to jobs query.

Context commands:
  set project P       Set default --project-id
  set dataset D       Set default --dataset-id
  set location L      Set default --location
  set profile P       Set default --profile
  set agent A         Set default --agent
  show context        Show current session context
  clear context       Reset all context
  /format text        Change session output format

Examples:
  dcx> datasets list
  dcx> set dataset analytics
  dcx> SELECT COUNT(*) FROM events
  dcx> ca ask "top errors yesterday"
  dcx> exit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runREPL(a)
		},
	}

	a.Root.AddCommand(cmd)
	// Intentionally NOT registered in contract registry.
	// repl is interactive/human-only, not agent-discoverable.
}

func runREPL(app *App) error {
	dcxBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	ctx := replContext{
		ProjectID: app.Opts.ProjectID,
		DatasetID: app.Opts.DatasetID,
		Location:  app.Opts.Location,
		Format:    "text", // human-readable default for REPL
	}

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)

	fmt.Fprintf(os.Stderr, "dcx interactive session. Type 'help' for commands, 'exit' to quit.\n")
	if ctx.ProjectID != "" {
		fmt.Fprintf(os.Stderr, "  project: %s\n", ctx.ProjectID)
	}

	for {
		prompt := "dcx> "
		input, err := line.Prompt(prompt)
		if err != nil {
			// Ctrl-D or error
			fmt.Fprintln(os.Stderr)
			return nil
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		line.AppendHistory(input)

		// Handle builtins.
		if handleBuiltin(input, &ctx) {
			continue
		}

		// Handle exit.
		if input == "exit" || input == "quit" {
			return nil
		}

		// Handle bare SQL.
		var args []string
		if isBareReadOnlySQL(input) {
			sql := collectMultilineSQL(input, line)
			args = []string{"jobs", "query", "--query", sql}
		} else if strings.HasPrefix(strings.ToLower(input), "dry-run ") && isBareReadOnlySQL(input[8:]) {
			sql := collectMultilineSQL(input[8:], line)
			args = []string{"jobs", "query", "--query", sql, "--dry-run"}
		} else {
			args = parseLineToArgv(input)
		}

		// Strip "dcx" prefix if user types it.
		if len(args) > 0 && args[0] == "dcx" {
			args = args[1:]
		}

		if len(args) == 0 {
			continue
		}

		// Block interactive/server commands.
		if isBlockedCommand(args) {
			fmt.Fprintf(os.Stderr, "  command not available in REPL: %s\n", args[0])
			continue
		}

		// Inject session context (contract-aware).
		args = injectContext(args, ctx, app)

		// Append format if not already specified.
		args = appendFormatIfMissing(args, ctx.Format)

		// Execute as subprocess.
		execCmd := exec.CommandContext(context.Background(), dcxBinary, args...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		execCmd.Stdin = os.Stdin
		execCmd.Run() // ignore exit code — errors printed via stderr
		fmt.Println() // blank line after output for readability
	}
}

func handleBuiltin(input string, ctx *replContext) bool {
	lower := strings.ToLower(input)

	if lower == "help" || lower == "?" {
		printREPLHelp()
		return true
	}

	if lower == "show context" {
		printContext(ctx)
		return true
	}

	if lower == "clear context" {
		*ctx = replContext{Format: ctx.Format}
		fmt.Fprintln(os.Stderr, "  context cleared")
		return true
	}

	if strings.HasPrefix(lower, "set ") {
		handleSet(input[4:], ctx)
		return true
	}

	if strings.HasPrefix(lower, "/format ") {
		ctx.Format = strings.TrimSpace(input[8:])
		fmt.Fprintf(os.Stderr, "  format: %s\n", ctx.Format)
		return true
	}

	return false
}

func handleSet(rest string, ctx *replContext) {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	if len(parts) != 2 {
		fmt.Fprintln(os.Stderr, "  usage: set <key> <value>")
		fmt.Fprintln(os.Stderr, "  keys: project, dataset, location, profile, agent")
		return
	}

	key := strings.ToLower(parts[0])
	value := strings.TrimSpace(parts[1])

	switch key {
	case "project":
		ctx.ProjectID = value
	case "dataset":
		ctx.DatasetID = value
	case "location":
		ctx.Location = value
	case "profile":
		ctx.Profile = value
	case "agent":
		ctx.Agent = value
	default:
		fmt.Fprintf(os.Stderr, "  unknown context key: %s\n", key)
		return
	}
	fmt.Fprintf(os.Stderr, "  %s: %s\n", key, value)
}

func printContext(ctx *replContext) {
	fmt.Fprintln(os.Stderr, "  session context:")
	if ctx.ProjectID != "" {
		fmt.Fprintf(os.Stderr, "    project:  %s\n", ctx.ProjectID)
	}
	if ctx.DatasetID != "" {
		fmt.Fprintf(os.Stderr, "    dataset:  %s\n", ctx.DatasetID)
	}
	if ctx.Location != "" {
		fmt.Fprintf(os.Stderr, "    location: %s\n", ctx.Location)
	}
	if ctx.Profile != "" {
		fmt.Fprintf(os.Stderr, "    profile:  %s\n", ctx.Profile)
	}
	if ctx.Agent != "" {
		fmt.Fprintf(os.Stderr, "    agent:    %s\n", ctx.Agent)
	}
	fmt.Fprintf(os.Stderr, "    format:   %s\n", ctx.Format)
	if ctx.ProjectID == "" && ctx.DatasetID == "" && ctx.Location == "" && ctx.Profile == "" && ctx.Agent == "" {
		fmt.Fprintln(os.Stderr, "    (no context set)")
	}
}

func printREPLHelp() {
	fmt.Fprintln(os.Stderr, `  dcx interactive session

  Commands:
    <any dcx command>           Run without "dcx" prefix
    SELECT ... / WITH ...       Bare SQL routed to jobs query
    dry-run SELECT ...          SQL dry-run

  Context:
    set project <P>             Set default --project-id
    set dataset <D>             Set default --dataset-id
    set location <L>            Set default --location
    set profile <P>             Set default --profile
    set agent <A>               Set default --agent
    show context                Show current context
    clear context               Reset all context

  Session:
    /format <fmt>               Change output format (text, json, table, json-minified)
    help / ?                    Show this help
    exit / quit / Ctrl-D        Exit`)
}

// isBareReadOnlySQL detects SELECT/WITH SQL statements.
func isBareReadOnlySQL(input string) bool {
	upper := strings.ToUpper(strings.TrimSpace(input))
	return strings.HasPrefix(upper, "SELECT ") ||
		strings.HasPrefix(upper, "SELECT\n") ||
		strings.HasPrefix(upper, "WITH ") ||
		strings.HasPrefix(upper, "WITH\n") ||
		upper == "SELECT" ||
		upper == "WITH"
}

// collectMultilineSQL continues reading if the SQL doesn't end with ;
func collectMultilineSQL(initial string, line *liner.State) string {
	sql := initial
	for !strings.HasSuffix(strings.TrimSpace(sql), ";") {
		more, err := line.Prompt("  ...> ")
		if err != nil {
			break
		}
		line.AppendHistory(more)
		sql += "\n" + more
	}
	// Strip trailing semicolon.
	sql = strings.TrimSpace(sql)
	if strings.HasSuffix(sql, ";") {
		sql = sql[:len(sql)-1]
	}
	return strings.TrimSpace(sql)
}

// parseLineToArgv splits input into arguments, respecting quotes.
func parseLineToArgv(input string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
		} else if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func isBlockedCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if blockedCommands[args[0]] {
		return true
	}
	// Block "spanner operations wait" and similar wait commands.
	if len(args) >= 2 && args[len(args)-1] == "wait" {
		return true
	}
	// More precise: check last subcommand segment.
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] == "wait" && !strings.HasPrefix(args[i], "--") {
			return true
		}
		if strings.HasPrefix(args[i], "--") {
			continue
		}
		break
	}
	return false
}

// injectContext adds session context flags to the command, but only if
// the command's contract accepts them.
func injectContext(args []string, ctx replContext, app *App) []string {
	// Build the command path to look up the contract.
	// Try progressively shorter prefixes to find the matching contract
	// (handles positional args that aren't part of the command path).
	var cmdParts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		cmdParts = append(cmdParts, a)
	}

	var contract *contracts.CommandContract
	var ok bool
	for i := len(cmdParts); i >= 1; i-- {
		commandPath := "dcx " + strings.Join(cmdParts[:i], " ")
		contract, ok = app.Registry.Get(commandPath)
		if ok {
			break
		}
	}
	if !ok {
		// Unknown command — inject global flags anyway, let cobra handle errors.
		return injectGlobalContext(args, ctx)
	}

	// Build flag set from contract.
	acceptedFlags := make(map[string]bool)
	for _, f := range contract.Flags {
		acceptedFlags[f.Name] = true
	}

	result := make([]string, len(args))
	copy(result, args)

	// Only inject if the flag is accepted AND not already present.
	present := flagsPresent(args)

	if ctx.ProjectID != "" && acceptedFlags["project-id"] && !present["project-id"] {
		result = append(result, "--project-id", ctx.ProjectID)
	}
	if ctx.DatasetID != "" && acceptedFlags["dataset-id"] && !present["dataset-id"] {
		result = append(result, "--dataset-id", ctx.DatasetID)
	}
	if ctx.Location != "" && acceptedFlags["location"] && !present["location"] {
		result = append(result, "--location", ctx.Location)
	}
	if ctx.Profile != "" && acceptedFlags["profile"] && !present["profile"] {
		result = append(result, "--profile", ctx.Profile)
	}
	if ctx.Agent != "" && acceptedFlags["agent"] && !present["agent"] {
		result = append(result, "--agent", ctx.Agent)
	}

	return result
}

func injectGlobalContext(args []string, ctx replContext) []string {
	present := flagsPresent(args)
	if ctx.ProjectID != "" && !present["project-id"] {
		args = append(args, "--project-id", ctx.ProjectID)
	}
	if ctx.DatasetID != "" && !present["dataset-id"] {
		args = append(args, "--dataset-id", ctx.DatasetID)
	}
	if ctx.Location != "" && !present["location"] {
		args = append(args, "--location", ctx.Location)
	}
	return args
}

func flagsPresent(args []string) map[string]bool {
	present := make(map[string]bool)
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			name := strings.TrimPrefix(a, "--")
			if before, _, ok := strings.Cut(name, "="); ok {
				name = before
			}
			present[name] = true
		}
	}
	return present
}

func appendFormatIfMissing(args []string, format string) []string {
	if flagsPresent(args)["format"] {
		return args
	}
	return append(args, "--format", format)
}
