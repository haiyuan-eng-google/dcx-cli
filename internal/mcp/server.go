// Package mcp implements an MCP (Model Context Protocol) server for dcx.
//
// The server communicates over stdio using JSON-RPC 2.0, exposing read-only
// dcx commands as MCP tools. Each tools/call spawns a dcx subprocess to
// ensure contract parity with the CLI.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
	"github.com/haiyuan-eng-google/dcx-cli/internal/jsonpath"
	"github.com/haiyuan-eng-google/dcx-cli/internal/output"
)

// AllowedMCPFormats are the formats allowed for MCP output.
var AllowedMCPFormats = []string{"json", "json-minified"}

// JSONRPCRequest is an incoming JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPTool describes a tool in the MCP tools/list response.
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ToolsListResult is the result of tools/list.
type ToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// ToolCallParams are the params for tools/call.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCallResult is the result of tools/call.
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content block in a tool call result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AllowedMCPModes are the valid server modes.
var AllowedMCPModes = []string{"classic", "progressive"}

// Server holds the MCP server state.
type Server struct {
	Registry  *contracts.Registry
	Format    string // output format for tool calls
	DcxBinary string // path to dcx binary for subprocess calls
	Mode      string // "classic" (all tools) or "progressive" (3 meta-tools)
}

// NewServer creates an MCP server.
func NewServer(registry *contracts.Registry, format, dcxBinary, mode string) *Server {
	if format == "" {
		format = "json-minified"
	}
	if mode == "" {
		mode = "classic"
	}
	return &Server{
		Registry:  registry,
		Format:    format,
		DcxBinary: dcxBinary,
		Mode:      mode,
	}
}

// Serve runs the MCP server on stdio (blocking).
func (s *Server) Serve() error {
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size for large requests.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.writeError(nil, -32700, "Parse error", err.Error())
			continue
		}

		s.handleRequest(req)
	}

	return scanner.Err()
}

func (s *Server) handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	case "resources/list":
		s.handleResourcesList(req)
	case "resources/read":
		s.handleResourcesRead(req)
	case "notifications/initialized":
		// Client acknowledgement — no response needed.
	default:
		s.writeError(req.ID, -32601, "Method not found", req.Method)
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) {
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "dcx",
			"version": "0.1.0",
		},
	}
	s.writeResult(req.ID, result)
}

// blockedDomains are domains excluded from MCP — not useful as agent tools.
var blockedDomains = map[string]bool{
	"meta": true, "auth": true, "profiles": true, "mcp": true, "cli": true,
}

// CanExecuteMCPCommand validates that a command is allowed via MCP.
// Returns the canonical contract or an error. Used by tools/list, tools/call,
// and future progressive/batch modes.
func (s *Server) CanExecuteMCPCommand(command string) (*contracts.CommandContract, error) {
	// Normalize: tokenize first (splits on all whitespace including tabs),
	// strip leading "dcx" token if present, rejoin with single spaces.
	tokens := strings.Fields(command)
	if len(tokens) > 0 && tokens[0] == "dcx" {
		tokens = tokens[1:]
	}
	cmd := "dcx " + strings.Join(tokens, " ")

	contract, ok := s.Registry.Get(cmd)
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", command)
	}
	if blockedDomains[contract.Domain] {
		return nil, fmt.Errorf("command not available via MCP: %s", command)
	}
	if contract.IsMutation {
		return nil, fmt.Errorf("MCP bridge is read-only; mutation commands are not available")
	}
	if strings.HasSuffix(contract.Command, " wait") {
		return nil, fmt.Errorf("long-polling commands are not available via MCP")
	}
	return contract, nil
}

func (s *Server) handleToolsList(req JSONRPCRequest) {
	if s.Mode == "progressive" {
		s.handleToolsListProgressive(req)
		return
	}
	s.handleToolsListClassic(req)
}

func (s *Server) handleToolsListClassic(req JSONRPCRequest) {
	all := s.Registry.All()
	var tools []MCPTool

	for _, c := range all {
		if _, err := s.CanExecuteMCPCommand(c.Command); err != nil {
			continue
		}
		tool := MCPTool{
			Name:        commandToToolName(c.Command),
			Description: c.Description,
			InputSchema: buildInputSchema(c),
		}
		tools = append(tools, tool)
	}

	s.writeResult(req.ID, ToolsListResult{Tools: tools})
}

func (s *Server) handleToolsListProgressive(req JSONRPCRequest) {
	// Collect available domains for the enum.
	domainSet := make(map[string]bool)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			domainSet[c.Domain] = true
		}
	}
	domains := make([]interface{}, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].(string) < domains[j].(string)
	})

	tools := []MCPTool{
		{
			Name:        "dcx_discover",
			Description: "List available Data Cloud commands. Optionally filter by domain.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Filter by domain (omit for all)",
						"enum":        domains,
					},
				},
			},
		},
		{
			Name:        "dcx_describe",
			Description: "Get the full input schema and flags for a specific dcx command.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command path, e.g. 'datasets list' or 'ca ask'",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "dcx_execute",
			Description: "Execute a read-only dcx command and return the result.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command path, e.g. 'datasets list' or 'ca ask'",
					},
					"args": map[string]interface{}{
						"type":        "object",
						"description": "Command arguments as key-value pairs",
					},
					"result_mode": map[string]interface{}{
						"type":        "string",
						"description": "Result shaping: full (default), compact (count+sample+schema), count_only, schema_only",
						"enum":        []string{"full", "compact", "count_only", "schema_only"},
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "dcx_batch",
			Description: "Execute multiple read-only dcx commands in sequence. Use $prev.path to reference the previous step's JSON result.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"steps": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{
									"type":        "string",
									"description": "Command path, e.g. 'datasets list'",
								},
								"args": map[string]interface{}{
									"type":        "object",
									"description": "Arguments. Use $prev.path to reference previous step result.",
								},
								"result_mode": map[string]interface{}{
									"type":        "string",
									"description": "Result shaping per step: full (default), compact, count_only, schema_only",
									"enum":        []string{"full", "compact", "count_only", "schema_only"},
								},
							},
							"required": []string{"command"},
						},
						"description": "Ordered list of commands (max 10)",
					},
				},
				"required": []string{"steps"},
			},
		},
	}

	s.writeResult(req.ID, ToolsListResult{Tools: tools})
}

func (s *Server) handleToolsCall(req JSONRPCRequest) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Progressive mode meta-tools.
	if s.Mode == "progressive" {
		switch params.Name {
		case "dcx_discover":
			s.handleDiscover(req, params)
			return
		case "dcx_describe":
			s.handleDescribe(req, params)
			return
		case "dcx_execute":
			s.handleExecute(req, params)
			return
		case "dcx_batch":
			s.handleBatch(req, params)
			return
		}
		s.writeError(req.ID, -32601, "Method not found",
			fmt.Sprintf("unknown tool %q; available: dcx_discover, dcx_describe, dcx_execute, dcx_batch", params.Name))
		return
	}

	// Classic mode: resolve tool name to command.
	s.handleClassicToolCall(req, params)
}

func (s *Server) handleClassicToolCall(req JSONRPCRequest, params ToolCallParams) {
	cmdName := toolNameToCommand(params.Name)
	contract, err := s.CanExecuteMCPCommand(cmdName)
	if err != nil {
		s.writeError(req.ID, -32601, "Method not allowed", err.Error())
		return
	}

	if err := s.validateRequiredPositionals(contract, params.Arguments); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	args := s.buildArgs(contract, params.Name, params.Arguments)
	s.executeAndRespond(req.ID, args)
}

// handleDiscover lists available commands, optionally filtered by domain.
func (s *Server) handleDiscover(req JSONRPCRequest, params ToolCallParams) {
	var domainFilter string
	if domainRaw, ok := params.Arguments["domain"]; ok {
		domainStr, isStr := domainRaw.(string)
		if !isStr || domainRaw == nil {
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("\"domain\" must be a string, got %T", domainRaw))
			return
		}
		domainFilter = domainStr
	}

	type cmdSummary struct {
		Command     string `json:"command"`
		Domain      string `json:"domain"`
		Description string `json:"description"`
	}

	// Validate domain against available domains if specified.
	if domainFilter != "" {
		validDomain := false
		for _, c := range s.Registry.All() {
			if _, err := s.CanExecuteMCPCommand(c.Command); err == nil && c.Domain == domainFilter {
				validDomain = true
				break
			}
		}
		if !validDomain {
			var available []string
			seen := make(map[string]bool)
			for _, c := range s.Registry.All() {
				if _, err := s.CanExecuteMCPCommand(c.Command); err == nil && !seen[c.Domain] {
					available = append(available, c.Domain)
					seen[c.Domain] = true
				}
			}
			sort.Strings(available)
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("unknown domain %q; available: %s", domainFilter, strings.Join(available, ", ")))
			return
		}
	}

	var commands []cmdSummary
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err != nil {
			continue
		}
		if domainFilter != "" && c.Domain != domainFilter {
			continue
		}
		// Strip "dcx " prefix for cleaner output.
		cmd := strings.TrimPrefix(c.Command, "dcx ")
		commands = append(commands, cmdSummary{
			Command:     cmd,
			Domain:      c.Domain,
			Description: c.Description,
		})
	}

	data, _ := json.Marshal(commands)
	s.writeResult(req.ID, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(data)}},
	})
}

// handleDescribe returns the full schema for a specific command.
func (s *Server) handleDescribe(req JSONRPCRequest, params ToolCallParams) {
	command, _ := params.Arguments["command"].(string)
	if command == "" {
		s.writeError(req.ID, -32602, "Invalid params", "required argument \"command\" is missing")
		return
	}

	contract, err := s.CanExecuteMCPCommand(command)
	if err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Build a rich description with flags and metadata.
	type flagDesc struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Required    bool   `json:"required,omitempty"`
		Positional  bool   `json:"positional,omitempty"`
	}

	var flags []flagDesc
	for _, f := range contract.Flags {
		// Skip global flags set via env/config.
		if f.Name == "format" || f.Name == "token" || f.Name == "credentials-file" {
			continue
		}
		flags = append(flags, flagDesc{
			Name:        f.Name,
			Type:        f.Type,
			Description: f.Description,
			Required:    f.Required,
			Positional:  f.Positional,
		})
	}

	result := map[string]interface{}{
		"command":     strings.TrimPrefix(contract.Command, "dcx "),
		"domain":      contract.Domain,
		"description": contract.Description,
		"flags":       flags,
		"is_mutation": contract.IsMutation,
		"dry_run":     contract.SupportsDryRun,
	}

	data, _ := json.Marshal(result)
	s.writeResult(req.ID, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(data)}},
	})
}

// handleExecute runs a dcx command with the provided arguments.
func (s *Server) handleExecute(req JSONRPCRequest, params ToolCallParams) {
	command, _ := params.Arguments["command"].(string)
	if command == "" {
		s.writeError(req.ID, -32602, "Invalid params", "required argument \"command\" is missing")
		return
	}

	contract, err := s.CanExecuteMCPCommand(command)
	if err != nil {
		s.writeError(req.ID, -32601, "Method not allowed", err.Error())
		return
	}

	// Extract args from the "args" object. Reject null/non-object.
	cmdArgs := make(map[string]interface{})
	if argsRaw, ok := params.Arguments["args"]; ok {
		if argsRaw == nil {
			s.writeError(req.ID, -32602, "Invalid params", "\"args\" must be an object, got null")
			return
		}
		argsMap, ok := argsRaw.(map[string]interface{})
		if !ok {
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("\"args\" must be an object, got %T", argsRaw))
			return
		}
		cmdArgs = argsMap
	}

	if err := s.validateRequiredPositionals(contract, cmdArgs); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Extract result_mode (default: "full"). Reject non-string.
	resultMode := "full"
	if rmRaw, ok := params.Arguments["result_mode"]; ok {
		if rmRaw == nil {
			s.writeError(req.ID, -32602, "Invalid params", "\"result_mode\" must be a string, got null")
			return
		}
		rmStr, isStr := rmRaw.(string)
		if !isStr {
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("\"result_mode\" must be a string, got %T", rmRaw))
			return
		}
		if rmStr != "" {
			resultMode = rmStr
		}
	}

	// Build the tool name from the canonical command for buildArgs.
	toolName := commandToToolName(contract.Command)
	args := s.buildArgs(contract, toolName, cmdArgs)
	s.executeWithCompaction(req.ID, args, resultMode)
}

// executeAndRespond runs a subprocess and writes the result.
func (s *Server) executeAndRespond(id interface{}, args []string) {
	cmd := exec.Command(s.DcxBinary, args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		s.writeResult(id, ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: string(output)}},
			IsError: true,
		})
		return
	}

	s.writeResult(id, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(output)}},
	})
}

// executeWithCompaction runs a subprocess and optionally compacts the result.
func (s *Server) executeWithCompaction(id interface{}, args []string, resultMode string) {
	if !output.ValidResultModes[resultMode] {
		s.writeError(id, -32602, "Invalid params",
			fmt.Sprintf("invalid result_mode %q; valid: %s", resultMode, strings.Join(output.ResultModeNames(), ", ")))
		return
	}

	cmd := exec.Command(s.DcxBinary, args...)
	cmdOutput, err := cmd.CombinedOutput()

	if err != nil {
		s.writeResult(id, ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: string(cmdOutput)}},
			IsError: true,
		})
		return
	}

	shaped := output.CompactJSON(cmdOutput, resultMode)
	s.writeResult(id, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(shaped)}},
	})
}

// maxBatchSteps is the maximum number of steps in a batch.
const maxBatchSteps = 10

// maxBatchOutputBytes is the maximum total output bytes across all steps.
const maxBatchOutputBytes = 1024 * 1024 // 1 MB

// batchStep is a single step in a batch request.
type batchStep struct {
	Command    string                 `json:"command"`
	Args       map[string]interface{} `json:"args"`
	ResultMode string                 `json:"result_mode"`
}

// batchStepResult is the result of a single batch step.
type batchStepResult struct {
	Step     int         `json:"step"`
	Command  string      `json:"command"`
	ExitCode int         `json:"exit_code"`
	Output   string      `json:"output"`
	JSON     interface{} `json:"json,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// handleBatch executes multiple commands in sequence with $prev references.
func (s *Server) handleBatch(req JSONRPCRequest, params ToolCallParams) {
	stepsRaw, ok := params.Arguments["steps"]
	if !ok || stepsRaw == nil {
		s.writeError(req.ID, -32602, "Invalid params", "required argument \"steps\" is missing")
		return
	}
	stepsArr, ok := stepsRaw.([]interface{})
	if !ok {
		s.writeError(req.ID, -32602, "Invalid params",
			fmt.Sprintf("\"steps\" must be an array, got %T", stepsRaw))
		return
	}
	if len(stepsArr) == 0 {
		s.writeError(req.ID, -32602, "Invalid params", "\"steps\" must not be empty")
		return
	}
	if len(stepsArr) > maxBatchSteps {
		s.writeError(req.ID, -32602, "Invalid params",
			fmt.Sprintf("\"steps\" has %d entries, max is %d", len(stepsArr), maxBatchSteps))
		return
	}

	// Parse steps.
	var steps []batchStep
	for i, raw := range stepsArr {
		m, ok := raw.(map[string]interface{})
		if !ok {
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("step %d must be an object", i))
			return
		}
		command, _ := m["command"].(string)
		if command == "" {
			s.writeError(req.ID, -32602, "Invalid params",
				fmt.Sprintf("step %d: required field \"command\" is missing", i))
			return
		}
		args := make(map[string]interface{})
		if argsRaw, ok := m["args"]; ok {
			if argsRaw == nil {
				s.writeError(req.ID, -32602, "Invalid params",
					fmt.Sprintf("step %d: \"args\" must be an object, got null", i))
				return
			}
			argsMap, ok := argsRaw.(map[string]interface{})
			if !ok {
				s.writeError(req.ID, -32602, "Invalid params",
					fmt.Sprintf("step %d: \"args\" must be an object, got %T", i, argsRaw))
				return
			}
			args = argsMap
		}
		rm := "full"
		if rmRaw, ok := m["result_mode"]; ok && rmRaw != nil {
			rmStr, isStr := rmRaw.(string)
			if !isStr {
				s.writeError(req.ID, -32602, "Invalid params",
					fmt.Sprintf("step %d: \"result_mode\" must be a string, got %T", i, rmRaw))
				return
			}
			if rmStr != "" && !output.ValidResultModes[rmStr] {
				s.writeError(req.ID, -32602, "Invalid params",
					fmt.Sprintf("step %d: invalid result_mode %q; valid: full, compact, count_only, schema_only", i, rmStr))
				return
			}
			if rmStr != "" {
				rm = rmStr
			}
		}
		steps = append(steps, batchStep{Command: command, Args: args, ResultMode: rm})
	}

	// Validate all steps upfront (fail-fast before any execution).
	for i, step := range steps {
		if _, err := s.CanExecuteMCPCommand(step.Command); err != nil {
			s.writeError(req.ID, -32601, "Method not allowed",
				fmt.Sprintf("step %d (%s): %s", i, step.Command, err.Error()))
			return
		}
	}

	// Execute steps sequentially.
	var results []batchStepResult
	var prevJSON interface{}
	totalBytes := 0

	for i, step := range steps {
		contract, _ := s.CanExecuteMCPCommand(step.Command)

		// Resolve $prev references in args.
		resolvedArgs := make(map[string]interface{})
		for k, v := range step.Args {
			sv, ok := v.(string)
			if ok && strings.Contains(sv, "$prev") {
				if prevJSON == nil {
					msg := "$prev referenced but no previous result"
					if i > 0 {
						msg = "$prev referenced but previous step did not produce valid JSON"
					}
					results = append(results, batchStepResult{
						Step:     i,
						Command:  step.Command,
						ExitCode: 1,
						Error:    fmt.Sprintf("step %d: %s", i, msg),
					})
					goto done // fail-fast
				}
				resolved, err := resolvePrevRef(sv, prevJSON)
				if err != nil {
					results = append(results, batchStepResult{
						Step:     i,
						Command:  step.Command,
						ExitCode: 1,
						Error:    fmt.Sprintf("step %d: %s", i, err.Error()),
					})
					goto done
				}
				resolvedArgs[k] = resolved
			} else {
				resolvedArgs[k] = v
			}
		}

		// Validate positionals.
		if err := s.validateRequiredPositionals(contract, resolvedArgs); err != nil {
			results = append(results, batchStepResult{
				Step:     i,
				Command:  step.Command,
				ExitCode: 1,
				Error:    fmt.Sprintf("step %d: %s", i, err.Error()),
			})
			goto done
		}

		// Build and execute.
		{
			toolName := commandToToolName(contract.Command)
			args := s.buildArgs(contract, toolName, resolvedArgs)

			cmd := exec.Command(s.DcxBinary, args...)
			cmdOut, err := cmd.CombinedOutput()

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
			}

			totalBytes += len(cmdOut)
			if totalBytes > maxBatchOutputBytes {
				results = append(results, batchStepResult{
					Step:     i,
					Command:  step.Command,
					ExitCode: 1,
					Error:    fmt.Sprintf("step %d: total output exceeds %d bytes limit", i, maxBatchOutputBytes),
				})
				goto done
			}

			// Parse JSON for $prev. Enforce EOF to reject trailing
			// content (matches $last EOF enforcement in REPL).
			var parsed interface{}
			trimmed := strings.TrimSpace(string(cmdOut))
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
				dec := json.NewDecoder(strings.NewReader(trimmed))
				dec.UseNumber()
				if dec.Decode(&parsed) == nil {
					var extra json.RawMessage
					if dec.Decode(&extra) != io.EOF {
						parsed = nil // trailing content — not clean JSON
					}
				} else {
					parsed = nil
				}
			}

			// Apply result_mode compaction for the response envelope.
			// Keep raw parsed JSON for $prev resolution so chained steps
			// can access the full data (e.g., $prev.items[4] after a
			// compact step that only sampled 3 items).
			outputStr := string(cmdOut)
			var envelopeJSON interface{} = parsed
			if parsed != nil && step.ResultMode != "full" {
				compacted := output.CompactResult(parsed, step.ResultMode)
				compactedData, _ := json.Marshal(compacted)
				outputStr = string(compactedData)
				envelopeJSON = compacted
			}

			result := batchStepResult{
				Step:     i,
				Command:  step.Command,
				ExitCode: exitCode,
				Output:   outputStr,
				JSON:     envelopeJSON,
			}
			if exitCode != 0 {
				result.Error = fmt.Sprintf("command exited with code %d", exitCode)
			}
			results = append(results, result)

			// Fail-fast on error.
			if exitCode != 0 {
				goto done
			}

			// Update $prev with RAW parsed JSON (not compacted) so
			// the next step can resolve full paths like $prev.items[4].
			prevJSON = parsed
		}
	}

done:
	data, _ := json.Marshal(results)
	s.writeResult(req.ID, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(data)}},
	})
}

// resolvePrevRef expands $prev references in a string value.
func resolvePrevRef(value string, prevJSON interface{}) (string, error) {
	idx := strings.Index(value, "$prev")
	if idx < 0 {
		return value, nil
	}

	prefix := value[:idx]
	path := value[idx+5:] // after "$prev"
	suffix := ""

	// If path is empty, return the whole prev as JSON.
	if path == "" || (path[0] != '.' && path[0] != '[') {
		suffix = path
		resolved := jsonpath.FormatValue(prevJSON)
		return prefix + resolved + suffix, nil
	}

	resolved, err := jsonpath.Resolve(prevJSON, path)
	if err != nil {
		return "", fmt.Errorf("$prev%s: %w", path, err)
	}
	return prefix + resolved, nil
}

// MCPResource describes a resource in the MCP resources/list response.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType"`
}

// ResourcesListResult is the result of resources/list.
type ResourcesListResult struct {
	Resources []MCPResource `json:"resources"`
}

// ResourceReadParams are the params for resources/read.
type ResourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceReadResult is the result of resources/read.
type ResourceReadResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent is a content block in a resource read result.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// handleResourcesList returns all available dcx resources.
func (s *Server) handleResourcesList(req JSONRPCRequest) {
	var resources []MCPResource

	// Index resource: summary of all domains.
	resources = append(resources, MCPResource{
		URI:         "dcx://index",
		Name:        "dcx command index",
		Description: "Summary of MCP-available read-only command domains and counts",
		MimeType:    "application/json",
	})

	// Collect domains and commands.
	domainCmds := make(map[string][]string)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err != nil {
			continue
		}
		cmd := strings.TrimPrefix(c.Command, "dcx ")
		domainCmds[c.Domain] = append(domainCmds[c.Domain], cmd)
	}

	// Domain resources.
	domainNames := make([]string, 0, len(domainCmds))
	for d := range domainCmds {
		domainNames = append(domainNames, d)
	}
	sort.Strings(domainNames)

	for _, domain := range domainNames {
		resources = append(resources, MCPResource{
			URI:         "dcx://domains/" + domain,
			Name:        domain + " commands",
			Description: fmt.Sprintf("%d read-only %s commands", len(domainCmds[domain]), domain),
			MimeType:    "application/json",
		})

		// Per-command resources.
		cmds := domainCmds[domain]
		sort.Strings(cmds)
		for _, cmd := range cmds {
			safeName := strings.ReplaceAll(cmd, " ", "/")
			resources = append(resources, MCPResource{
				URI:      "dcx://commands/" + safeName,
				Name:     cmd,
				MimeType: "application/json",
			})
		}
	}

	s.writeResult(req.ID, ResourcesListResult{Resources: resources})
}

// handleResourcesRead returns the content of a specific resource.
func (s *Server) handleResourcesRead(req JSONRPCRequest) {
	var params ResourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	uri := params.URI
	if uri == "" {
		s.writeError(req.ID, -32602, "Invalid params", "required field \"uri\" is missing")
		return
	}

	// Route by URI pattern.
	switch {
	case uri == "dcx://index":
		s.readIndexResource(req, uri)
	case strings.HasPrefix(uri, "dcx://domains/"):
		domain := strings.TrimPrefix(uri, "dcx://domains/")
		s.readDomainResource(req, uri, domain)
	case strings.HasPrefix(uri, "dcx://commands/"):
		cmdPath := strings.TrimPrefix(uri, "dcx://commands/")
		s.readCommandResource(req, uri, cmdPath)
	default:
		s.writeError(req.ID, -32602, "Invalid params",
			fmt.Sprintf("unknown resource URI: %s", uri))
	}
}

func (s *Server) readIndexResource(req JSONRPCRequest, uri string) {
	type domainSummary struct {
		Domain   string `json:"domain"`
		Commands int    `json:"commands"`
		URI      string `json:"uri"`
	}

	domainCounts := make(map[string]int)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err != nil {
			continue
		}
		domainCounts[c.Domain]++
	}

	var summaries []domainSummary
	for d, count := range domainCounts {
		summaries = append(summaries, domainSummary{
			Domain:   d,
			Commands: count,
			URI:      "dcx://domains/" + d,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Domain < summaries[j].Domain
	})

	total := 0
	for _, s := range summaries {
		total += s.Commands
	}

	result := map[string]interface{}{
		"total_commands": total,
		"scope":          "mcp_read_only",
		"domains":        summaries,
	}

	data, _ := json.Marshal(result)
	s.writeResult(req.ID, ResourceReadResult{
		Contents: []ResourceContent{{URI: uri, MimeType: "application/json", Text: string(data)}},
	})
}

func (s *Server) readDomainResource(req JSONRPCRequest, uri, domain string) {
	type cmdInfo struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		URI         string `json:"uri"`
	}

	var commands []cmdInfo
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err != nil {
			continue
		}
		if c.Domain != domain {
			continue
		}
		cmd := strings.TrimPrefix(c.Command, "dcx ")
		safeName := strings.ReplaceAll(cmd, " ", "/")
		commands = append(commands, cmdInfo{
			Command:     cmd,
			Description: c.Description,
			URI:         "dcx://commands/" + safeName,
		})
	}

	if len(commands) == 0 {
		var available []string
		seen := make(map[string]bool)
		for _, c := range s.Registry.All() {
			if _, err := s.CanExecuteMCPCommand(c.Command); err == nil && !seen[c.Domain] {
				available = append(available, c.Domain)
				seen[c.Domain] = true
			}
		}
		sort.Strings(available)
		s.writeError(req.ID, -32602, "Invalid params",
			fmt.Sprintf("unknown domain %q; available: %s", domain, strings.Join(available, ", ")))
		return
	}

	data, _ := json.Marshal(commands)
	s.writeResult(req.ID, ResourceReadResult{
		Contents: []ResourceContent{{URI: uri, MimeType: "application/json", Text: string(data)}},
	})
}

func (s *Server) readCommandResource(req JSONRPCRequest, uri, cmdPath string) {
	// Convert path back to command: "datasets/list" → "datasets list"
	command := strings.ReplaceAll(cmdPath, "/", " ")

	contract, err := s.CanExecuteMCPCommand(command)
	if err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Build rich schema.
	type flagDesc struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Required    bool   `json:"required,omitempty"`
		Positional  bool   `json:"positional,omitempty"`
	}

	var flags []flagDesc
	for _, f := range contract.Flags {
		if f.Name == "format" || f.Name == "token" || f.Name == "credentials-file" {
			continue
		}
		flags = append(flags, flagDesc{
			Name:        f.Name,
			Type:        f.Type,
			Description: f.Description,
			Required:    f.Required,
			Positional:  f.Positional,
		})
	}

	result := map[string]interface{}{
		"command":     strings.TrimPrefix(contract.Command, "dcx "),
		"domain":      contract.Domain,
		"description": contract.Description,
		"flags":       flags,
		"is_mutation": contract.IsMutation,
		"dry_run":     contract.SupportsDryRun,
	}

	data, _ := json.Marshal(result)
	s.writeResult(req.ID, ResourceReadResult{
		Contents: []ResourceContent{{URI: uri, MimeType: "application/json", Text: string(data)}},
	})
}

func (s *Server) writeResult(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}

func (s *Server) writeError(id interface{}, code int, message, data string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	respData, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(respData))
}

// validateRequiredPositionals checks that required positional args are present
// and non-empty (rejects nil, "", and whitespace-only values).
func (s *Server) validateRequiredPositionals(contract *contracts.CommandContract, args map[string]interface{}) error {
	for _, f := range contract.Flags {
		if f.Positional && f.Required {
			v, ok := args[f.Name]
			if !ok || v == nil {
				return fmt.Errorf("required positional argument %q is missing", f.Name)
			}
			if sv := strings.TrimSpace(fmt.Sprintf("%v", v)); sv == "" {
				return fmt.Errorf("required positional argument %q is empty", f.Name)
			}
		}
	}
	return nil
}

// buildArgs constructs deterministic subprocess args from a contract and arguments.
// Non-positional flags are sorted by key. Positional args follow in contract order.
func (s *Server) buildArgs(contract *contracts.CommandContract, toolName string, arguments map[string]interface{}) []string {
	cmdArgs := toolNameToArgs(toolName)
	args := append(cmdArgs, "--format", s.Format)

	// Identify positional flags.
	positionalFlags := make(map[string]bool)
	for _, f := range contract.Flags {
		if f.Positional {
			positionalFlags[f.Name] = true
		}
	}

	// Sorted non-positional flags.
	var flagKeys []string
	for k := range arguments {
		if !positionalFlags[k] {
			flagKeys = append(flagKeys, k)
		}
	}
	sort.Strings(flagKeys)

	for _, k := range flagKeys {
		sv := fmt.Sprintf("%v", arguments[k])
		if sv == "" {
			continue
		}
		args = append(args, "--"+k, sv)
	}

	// Positional args in contract declaration order.
	for _, f := range contract.Flags {
		if !f.Positional {
			continue
		}
		v, ok := arguments[f.Name]
		if !ok {
			continue
		}
		sv := fmt.Sprintf("%v", v)
		if sv != "" {
			args = append(args, sv)
		}
	}

	return args
}

// commandToToolName converts "dcx datasets list" to "dcx_datasets_list".
func commandToToolName(command string) string {
	return strings.ReplaceAll(command, " ", "_")
}

// toolNameToCommand converts "dcx_datasets_list" to "dcx datasets list"
// for registry lookup.
func toolNameToCommand(name string) string {
	return strings.ReplaceAll(name, "_", " ")
}

// toolNameToArgs converts "dcx_datasets_list" to ["datasets", "list"].
func toolNameToArgs(name string) []string {
	parts := strings.Split(name, "_")
	// Skip the "dcx" prefix.
	if len(parts) > 0 && parts[0] == "dcx" {
		parts = parts[1:]
	}
	return parts
}

// buildInputSchema creates a JSON Schema for a command's flags.
func buildInputSchema(c *contracts.CommandContract) map[string]interface{} {
	properties := make(map[string]interface{})
	var required []string

	for _, flag := range c.Flags {
		// Skip global flags that are set via environment/config.
		if flag.Name == "format" || flag.Name == "token" || flag.Name == "credentials-file" {
			continue
		}

		prop := map[string]interface{}{
			"type":        jsonSchemaType(flag.Type),
			"description": flag.Description,
		}
		properties[flag.Name] = prop

		if flag.Required {
			required = append(required, flag.Name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func jsonSchemaType(flagType string) string {
	switch flagType {
	case "bool":
		return "boolean"
	case "int":
		return "integer"
	default:
		return "string"
	}
}
