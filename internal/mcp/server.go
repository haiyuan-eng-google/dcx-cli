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
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
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
			"tools": map[string]interface{}{},
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
				},
				"required": []string{"command"},
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
		}
		s.writeError(req.ID, -32601, "Method not found",
			fmt.Sprintf("unknown tool %q; available: dcx_discover, dcx_describe, dcx_execute", params.Name))
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
	domainFilter, _ := params.Arguments["domain"].(string)

	type cmdSummary struct {
		Command     string `json:"command"`
		Domain      string `json:"domain"`
		Description string `json:"description"`
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

	// Extract args from the "args" object.
	cmdArgs := make(map[string]interface{})
	if argsRaw, ok := params.Arguments["args"]; ok {
		if argsMap, ok := argsRaw.(map[string]interface{}); ok {
			cmdArgs = argsMap
		}
	}

	if err := s.validateRequiredPositionals(contract, cmdArgs); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Build the tool name from the canonical command for buildArgs.
	toolName := commandToToolName(contract.Command)
	args := s.buildArgs(contract, toolName, cmdArgs)
	s.executeAndRespond(req.ID, args)
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
