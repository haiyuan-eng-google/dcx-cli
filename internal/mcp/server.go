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

// Server holds the MCP server state.
type Server struct {
	Registry  *contracts.Registry
	Format    string // output format for tool calls
	DcxBinary string // path to dcx binary for subprocess calls
}

// NewServer creates an MCP server.
func NewServer(registry *contracts.Registry, format, dcxBinary string) *Server {
	if format == "" {
		format = "json-minified"
	}
	return &Server{
		Registry:  registry,
		Format:    format,
		DcxBinary: dcxBinary,
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

func (s *Server) handleToolsList(req JSONRPCRequest) {
	all := s.Registry.All()
	var tools []MCPTool

	for _, c := range all {
		// Skip non-data commands — not useful as MCP tools.
		if c.Domain == "meta" || c.Domain == "auth" || c.Domain == "profiles" || c.Domain == "mcp" {
			continue
		}
		// Only expose read-only commands.
		if c.IsMutation {
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

func (s *Server) handleToolsCall(req JSONRPCRequest) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	// Convert tool name back to command args.
	cmdArgs := toolNameToArgs(params.Name)

	// Build subprocess args, stringifying non-string values.
	// "question" is a positional arg for ca ask — append it without a flag prefix.
	args := append(cmdArgs, "--format", s.Format)
	var positionalArgs []string
	for k, v := range params.Arguments {
		sv := fmt.Sprintf("%v", v)
		if sv == "" {
			continue
		}
		if k == "question" {
			positionalArgs = append(positionalArgs, sv)
		} else {
			args = append(args, "--"+k, sv)
		}
	}
	args = append(args, positionalArgs...)

	// Execute subprocess.
	cmd := exec.Command(s.DcxBinary, args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		result := ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: string(output)}},
			IsError: true,
		}
		s.writeResult(req.ID, result)
		return
	}

	result := ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(output)}},
	}
	s.writeResult(req.ID, result)
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

// commandToToolName converts "dcx datasets list" to "dcx_datasets_list".
func commandToToolName(command string) string {
	return strings.ReplaceAll(command, " ", "_")
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
