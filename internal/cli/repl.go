package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/peterh/liner"
	"github.com/spf13/cobra"
)

// commandResult captures the outcome of a subprocess command.
type commandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	JSON     interface{} // parsed JSON from stdout, nil if not valid JSON
}

// replContext holds session state across REPL commands.
type replContext struct {
	ProjectID       string
	DatasetID       string
	Location        string
	Profile         string
	Agent           string
	Format          string         // session output format (default: text)
	LastResult      *commandResult // last successful command result ($last)
	Token           string         // forwarded from parent --token
	CredentialsFile string         // forwarded from parent --credentials-file
	Retry           int            // forwarded from parent --retry
	OutputFields    string         // forwarded from parent --output-fields
	Transcript      *os.File       // transcript log file (nil when not recording)
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
			formatExplicit := cmd.Flags().Changed("format") ||
				cmd.InheritedFlags().Changed("format")
			return runREPL(a, formatExplicit)
		},
	}

	a.Root.AddCommand(cmd)
	// Intentionally NOT registered in contract registry.
	// repl is interactive/human-only, not agent-discoverable.
}

func runREPL(app *App, formatExplicit bool) error {
	dcxBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	ctx := replContext{
		ProjectID:       app.Opts.ProjectID,
		DatasetID:       app.Opts.DatasetID,
		Location:        app.Opts.Location,
		Token:           app.Opts.Token,
		CredentialsFile: app.Opts.CredentialsFile,
		Retry:           app.Opts.Retry,
		OutputFields:    app.Opts.OutputFields,
		Format:          replDefaultFormat(app.Opts.Format, formatExplicit),
	}

	// Close transcript on exit if still open.
	defer func() {
		if ctx.Transcript != nil {
			fmt.Fprintf(ctx.Transcript, "\n# dcx transcript ended %s\n", time.Now().Format(time.RFC3339))
			ctx.Transcript.Close()
		}
	}()

	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)

	// Wire tab completion from the contract registry.
	completer := buildCompleter(app.Registry)
	line.SetWordCompleter(completer)

	fmt.Fprintf(os.Stderr, "dcx interactive session. Type 'help' for commands, 'exit' to quit.\n")
	if ctx.ProjectID != "" {
		fmt.Fprintf(os.Stderr, "  project: %s\n", ctx.ProjectID)
	}

	for {
		prompt := "dcx> "
		input, err := line.Prompt(prompt)
		if err != nil {
			if err == liner.ErrPromptAborted {
				// Ctrl-C at prompt — clear line and continue.
				continue
			}
			// Ctrl-D or other error — exit.
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
		lower := strings.ToLower(input)
		if isBareReadOnlySQL(input) {
			sql := collectMultilineSQL(input, line)
			args = []string{"jobs", "query", "--query", sql}
		} else if strings.HasPrefix(lower, "dry-run ") && isAnySQL(input[8:]) {
			sql := collectMultilineSQL(input[8:], line)
			args = []string{"jobs", "query", "--query", sql, "--dry-run"}
		} else if strings.HasPrefix(lower, "run ") && isAnySQL(input[4:]) {
			sql := collectMultilineSQL(input[4:], line)
			args = []string{"jobs", "query", "--query", sql}
		} else if lower == "run" || lower == "dry-run" {
			fmt.Fprintln(os.Stderr, "  usage: run <SQL statement>")
			continue
		} else if isWriteSQL(input) {
			fmt.Fprintf(os.Stderr, "  DML/DDL requires explicit prefix: run %s\n", strings.Fields(input)[0])
			fmt.Fprintln(os.Stderr, "  Use 'dry-run ...' to validate without executing.")
			continue
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

		// Expand $last references in arguments.
		var expandErr error
		args, expandErr = expandLastRefs(args, ctx.LastResult)
		if expandErr != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", expandErr)
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

		// Execute as subprocess with capture and signal handling.
		cmdStart := time.Now()
		result := executeWithSignal(dcxBinary, args)
		cmdDuration := time.Since(cmdStart)
		fmt.Println() // blank line after output for readability

		// Log to transcript if active.
		transcriptLog(&ctx, input, &result, cmdDuration)

		// Update $last: successful JSON → update; successful non-JSON → clear; error → keep.
		if result.ExitCode == 0 {
			if result.JSON != nil {
				ctx.LastResult = &result
			} else {
				ctx.LastResult = nil // non-JSON clears $last
			}
		}
		// On error, LastResult is unchanged (don't lose good state)
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
		// Clear resource context only. Preserve auth/config forwarding
		// and session settings so --token/--credentials-file sessions
		// aren't broken by clearing context.
		ctx.ProjectID = ""
		ctx.DatasetID = ""
		ctx.Location = ""
		ctx.Profile = ""
		ctx.Agent = ""
		fmt.Fprintln(os.Stderr, "  context cleared")
		return true
	}

	if lower == "set" || strings.HasPrefix(lower, "set ") {
		rest := ""
		if len(input) > 4 {
			rest = input[4:]
		}
		handleSet(rest, ctx)
		return true
	}

	if strings.HasPrefix(lower, "/format ") {
		ctx.Format = strings.TrimSpace(input[8:])
		fmt.Fprintf(os.Stderr, "  format: %s\n", ctx.Format)
		return true
	}

	if strings.HasPrefix(lower, "/transcript") {
		handleTranscript(strings.TrimSpace(input[len("/transcript"):]), ctx)
		return true
	}

	if strings.HasPrefix(lower, "/output-fields") {
		rest := strings.TrimSpace(input[len("/output-fields"):])
		if rest == "" || rest == "clear" {
			ctx.OutputFields = ""
			fmt.Fprintln(os.Stderr, "  output-fields: (all)")
		} else {
			ctx.OutputFields = rest
			fmt.Fprintf(os.Stderr, "  output-fields: %s\n", ctx.OutputFields)
		}
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

// isAnySQL returns true if the input looks like any SQL statement (read or write).
func isAnySQL(input string) bool {
	return isBareReadOnlySQL(input) || isWriteSQL(input)
}

// isWriteSQL detects DML/DDL statements that modify data or schema.
func isWriteSQL(input string) bool {
	upper := strings.ToUpper(strings.TrimSpace(input))
	prefixes := []string{
		"INSERT ", "UPDATE ", "DELETE ", "MERGE ",
		"CREATE ", "DROP ", "ALTER ", "TRUNCATE ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
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

	// Always forward auth/config globals (these are accepted by all commands).
	result = appendAuthAndConfigFlags(result, ctx, present)

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
	args = appendAuthAndConfigFlags(args, ctx, present)
	return args
}

// appendAuthAndConfigFlags forwards auth/config globals to subprocess commands.
func appendAuthAndConfigFlags(args []string, ctx replContext, present map[string]bool) []string {
	if ctx.Token != "" && !present["token"] {
		args = append(args, "--token", ctx.Token)
	}
	if ctx.CredentialsFile != "" && !present["credentials-file"] {
		args = append(args, "--credentials-file", ctx.CredentialsFile)
	}
	if ctx.Retry > 0 && !present["retry"] {
		args = append(args, "--retry", fmt.Sprintf("%d", ctx.Retry))
	}
	if ctx.OutputFields != "" && !present["output-fields"] {
		args = append(args, "--output-fields", ctx.OutputFields)
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

// replDefaultFormat returns the session format: use explicit --format if set,
// otherwise default to "text" for human-readable REPL output.
func replDefaultFormat(optsFormat string, explicit bool) string {
	if explicit {
		return optsFormat
	}
	return "text"
}

func appendFormatIfMissing(args []string, format string) []string {
	if flagsPresent(args)["format"] {
		return args
	}
	return append(args, "--format", format)
}

// expandLastRefs replaces $last and $last.path.to.field references in args.
func expandLastRefs(args []string, last *commandResult) ([]string, error) {
	result := make([]string, len(args))
	for i, arg := range args {
		if !strings.Contains(arg, "$last") {
			result[i] = arg
			continue
		}
		if last == nil {
			return nil, fmt.Errorf("$last: no JSON result available; use /format json first")
		}
		expanded, err := resolveLastRef(arg, last.JSON)
		if err != nil {
			return nil, err
		}
		result[i] = expanded
	}
	return result, nil
}

// resolveLastRef resolves a single $last reference within an argument string.
// Supports: $last, $last.field, $last.field[0], $last.field[0].subfield
func resolveLastRef(arg string, jsonData interface{}) (string, error) {
	idx := strings.Index(arg, "$last")
	if idx < 0 {
		return arg, nil
	}

	prefix := arg[:idx]
	path := arg[idx+5:] // after "$last"

	value := jsonData

	// Parse path segments: .field or [N]
	for len(path) > 0 {
		if path[0] == '.' {
			path = path[1:]
			// Extract field name (until next . or [ or end)
			end := strings.IndexAny(path, ".[")
			if end < 0 {
				end = len(path)
			}
			fieldName := path[:end]
			path = path[end:]

			m, ok := value.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("$last: cannot access .%s on non-object", fieldName)
			}
			value, ok = m[fieldName]
			if !ok {
				return "", fmt.Errorf("$last: field %q not found", fieldName)
			}
		} else if path[0] == '[' {
			end := strings.Index(path, "]")
			if end < 0 {
				return "", fmt.Errorf("$last: unclosed bracket in path")
			}
			indexStr := path[1:end]
			path = path[end+1:]

			arr, ok := value.([]interface{})
			if !ok {
				return "", fmt.Errorf("$last: cannot index non-array")
			}
			if !isDigitsOnly(indexStr) {
				return "", fmt.Errorf("$last: invalid index [%s]", indexStr)
			}
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return "", fmt.Errorf("$last: invalid index [%s]", indexStr)
			}
			if index >= len(arr) {
				return "", fmt.Errorf("$last: index [%d] out of range (length %d)", index, len(arr))
			}
			value = arr[index]
		} else {
			break // remaining text is suffix
		}
	}

	// Format the resolved value as a string.
	var valueStr string
	switch v := value.(type) {
	case string:
		valueStr = v
	case json.Number:
		valueStr = v.String()
	case float64:
		if v == float64(int64(v)) {
			valueStr = fmt.Sprintf("%d", int64(v))
		} else {
			valueStr = fmt.Sprintf("%g", v)
		}
	case bool:
		valueStr = fmt.Sprintf("%t", v)
	case nil:
		valueStr = ""
	default:
		data, _ := json.Marshal(v)
		valueStr = string(data)
	}

	return prefix + valueStr + path, nil
}

// isDigitsOnly returns true if s is non-empty and contains only 0-9.
func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// executeWithSignal runs a subprocess with Ctrl-C forwarding.
// Ctrl-C during execution cancels the child process and returns to the prompt.
func executeWithSignal(binary string, args []string) commandResult {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdoutBuf, stderrBuf bytes.Buffer

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	cmd.Stdin = os.Stdin

	// Intercept SIGINT during child execution.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	// Start the command.
	if err := cmd.Start(); err != nil {
		return commandResult{ExitCode: 1, Stderr: err.Error()}
	}

	// Wait for command or signal.
	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()

	var err error
	select {
	case err = <-doneCh:
		// Command finished normally.
	case <-sigCh:
		// Ctrl-C received — kill the child.
		cancel()
		err = <-doneCh // wait for cleanup
		fmt.Fprintln(os.Stderr, "\n  interrupted")
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	stdout := stdoutBuf.String()

	// Try to parse stdout as JSON for $last.
	var parsed interface{}
	trimmed := strings.TrimSpace(stdout)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if dec.Decode(&parsed) == nil {
			var extra json.RawMessage
			if dec.Decode(&extra) != io.EOF {
				parsed = nil
			}
		} else {
			parsed = nil
		}
	}

	return commandResult{
		Stdout:   stdout,
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
		JSON:     parsed,
	}
}

// handleTranscript processes /transcript start|stop commands.
func handleTranscript(rest string, ctx *replContext) {
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		if ctx.Transcript != nil {
			fmt.Fprintf(os.Stderr, "  transcript: recording to %s\n", ctx.Transcript.Name())
		} else {
			fmt.Fprintln(os.Stderr, "  transcript: off")
			fmt.Fprintln(os.Stderr, "  usage: /transcript start <path>")
		}
		return
	}

	switch strings.ToLower(parts[0]) {
	case "start":
		if len(parts) < 2 {
			fmt.Fprintln(os.Stderr, "  usage: /transcript start <path>")
			return
		}
		if ctx.Transcript != nil {
			fmt.Fprintf(os.Stderr, "  transcript already recording to %s (stop first)\n", ctx.Transcript.Name())
			return
		}
		path := parts[1]
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  transcript error: %v\n", err)
			return
		}
		ctx.Transcript = f
		fmt.Fprintf(f, "# dcx transcript started %s\n\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(os.Stderr, "  transcript: recording to %s\n", path)

	case "stop":
		if ctx.Transcript == nil {
			fmt.Fprintln(os.Stderr, "  transcript: not recording")
			return
		}
		fmt.Fprintf(ctx.Transcript, "\n# dcx transcript ended %s\n", time.Now().Format(time.RFC3339))
		ctx.Transcript.Close()
		fmt.Fprintf(os.Stderr, "  transcript: stopped\n")
		ctx.Transcript = nil

	default:
		fmt.Fprintln(os.Stderr, "  usage: /transcript start <path> | /transcript stop")
	}
}

// transcriptLog writes a command and its result to the transcript file.
func transcriptLog(ctx *replContext, input string, result *commandResult, duration time.Duration) {
	if ctx.Transcript == nil {
		return
	}
	f := ctx.Transcript

	// Per-command timestamp and duration.
	fmt.Fprintf(f, "# %s (%s)\n", time.Now().Format(time.RFC3339), duration.Round(time.Millisecond))

	// Redact secrets from the input command line only.
	fmt.Fprintf(f, "dcx> %s\n", redactInputLine(input))

	// Stdout/stderr are logged verbatim — they don't contain user-supplied
	// secrets (tokens come from flags/env, not API responses).
	if result.Stdout != "" {
		fmt.Fprintf(f, "%s\n", result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprintf(f, "[stderr] %s\n", result.Stderr)
	}
	fmt.Fprintf(f, "[exit %d]\n\n", result.ExitCode)
}

// redactInputLine redacts secret flag values from a REPL input command line.
// Only operates on the user-typed command, not on stdout/stderr output.
func redactInputLine(input string) string {
	fields := strings.Fields(input)
	for i, f := range fields {
		// --token=VALUE
		if strings.HasPrefix(f, "--token=") {
			fields[i] = "--token=***"
		}
		// --token VALUE
		if f == "--token" && i+1 < len(fields) {
			fields[i+1] = "***"
		}
		// --credentials-file=PATH
		if strings.HasPrefix(f, "--credentials-file=") {
			fields[i] = "--credentials-file=***"
		}
		// --credentials-file PATH
		if f == "--credentials-file" && i+1 < len(fields) {
			fields[i+1] = "***"
		}
	}
	return strings.Join(fields, " ")
}

// buildCompleter creates a tab completion function from the contract registry.
func buildCompleter(registry *contracts.Registry) func(line string, pos int) (string, []string, string) {
	// Build command index: strip "dcx " prefix from all registered commands.
	type cmdEntry struct {
		segments []string // e.g. ["ca", "ask"]
		flags    []string // e.g. ["--agent", "--tables"]
	}
	var commands []cmdEntry
	for _, c := range registry.All() {
		path := strings.TrimPrefix(c.Command, "dcx ")
		segs := strings.Fields(path)
		if len(segs) == 0 {
			continue
		}
		// Skip blocked commands.
		if blockedCommands[segs[0]] {
			continue
		}
		var flags []string
		for _, f := range c.Flags {
			if !f.Positional {
				flags = append(flags, "--"+f.Name)
			}
		}
		// Add global flags.
		for _, gf := range contracts.GlobalFlags() {
			flags = append(flags, "--"+gf.Name)
		}
		commands = append(commands, cmdEntry{segments: segs, flags: flags})
	}

	// Builtins for completion.
	builtins := []string{
		"set project", "set dataset", "set location", "set profile", "set agent",
		"show context", "clear context",
		"/format json", "/format text", "/format table",
		"/output-fields", "/output-fields clear",
		"help", "exit", "quit",
	}

	return func(line string, pos int) (string, []string, string) {
		// Only complete at end of line.
		head := line[:pos]
		tail := line[pos:]

		// Check if we're completing a flag (after "--").
		fields := strings.Fields(head)
		lastWord := ""
		if len(head) > 0 && head[len(head)-1] != ' ' && len(fields) > 0 {
			lastWord = fields[len(fields)-1]
			fields = fields[:len(fields)-1]
		}

		// Flag completion: if lastWord starts with "--", suggest flags for the matched command.
		if strings.HasPrefix(lastWord, "--") {
			// Find the command by longest registered prefix match,
			// ignoring positional args beyond the command path.
			prefix := lastWord
			var suggestions []string
			var bestMatch *cmdEntry
			bestLen := 0
			for i := range commands {
				cmd := &commands[i]
				if len(cmd.segments) <= len(fields) && matchesPrefix(fields[:len(cmd.segments)], cmd.segments) {
					if len(cmd.segments) > bestLen {
						bestLen = len(cmd.segments)
						bestMatch = cmd
					}
				}
			}
			if bestMatch != nil {
				for _, f := range bestMatch.flags {
					if strings.HasPrefix(f, prefix) {
						suggestions = append(suggestions, f)
					}
				}
			}
			dedupSort(&suggestions)
			headPrefix := head[:len(head)-len(lastWord)]
			return headPrefix, suggestions, tail
		}

		// Command/builtin completion.
		typed := strings.ToLower(strings.TrimSpace(head))
		var suggestions []string

		// Match builtins.
		for _, b := range builtins {
			if strings.HasPrefix(b, typed) {
				suggestions = append(suggestions, b)
			}
		}

		// Match commands — suggest the next segment.
		for _, cmd := range commands {
			full := strings.Join(cmd.segments, " ")
			if strings.HasPrefix(full, typed) {
				// Suggest only the next segment beyond what's typed.
				typedParts := strings.Fields(typed)
				if len(typedParts) <= len(cmd.segments) {
					suggestion := strings.Join(cmd.segments[:len(typedParts)], " ")
					if len(typedParts) < len(cmd.segments) {
						suggestion = strings.Join(cmd.segments[:len(typedParts)+1], " ")
					}
					suggestions = append(suggestions, suggestion)
				}
			}
		}

		dedupSort(&suggestions)
		return "", suggestions, tail
	}
}

// matchesPrefix checks if typed fields match the command segments as a prefix.
func matchesPrefix(typed []string, segments []string) bool {
	if len(typed) > len(segments) {
		return false
	}
	for i, t := range typed {
		if !strings.EqualFold(t, segments[i]) {
			return false
		}
	}
	return true
}

// dedupSort removes duplicates and sorts a string slice in place.
func dedupSort(s *[]string) {
	seen := make(map[string]bool)
	result := (*s)[:0]
	for _, v := range *s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	*s = result
}
