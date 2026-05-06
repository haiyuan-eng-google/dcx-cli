package mcp

import (
	"testing"

	"github.com/haiyuan-eng-google/dcx-cli/internal/contracts"
)

func testRegistry() *contracts.Registry {
	r := contracts.NewRegistry()
	r.Register(contracts.BuildContract("datasets list", "bigquery", "List datasets", nil, false, false))
	r.Register(contracts.BuildContract("datasets delete", "bigquery", "Delete a dataset", nil, true, false))
	r.Register(contracts.BuildContract("spanner databases get-ddl", "spanner", "Get DDL", nil, false, false))
	r.Register(contracts.BuildContract("spanner operations wait", "spanner", "Wait for operation", nil, false, false))
	r.Register(contracts.BuildContract("auth check", "auth", "Check auth", nil, false, false))
	r.Register(contracts.BuildContract("meta commands", "meta", "List commands", nil, false, false))
	r.Register(contracts.BuildContract("completion", "cli", "Shell completion", nil, false, false))
	r.Register(contracts.BuildContract("mcp serve", "mcp", "MCP server", nil, false, false))
	r.Register(contracts.BuildContract("profiles list", "profiles", "List profiles", nil, false, false))

	// CA ask with positional question arg.
	caFlags := []contracts.FlagContract{
		{Name: "question", Type: "string", Description: "Question", Required: true, Positional: true},
		{Name: "agent", Type: "string", Description: "Agent"},
		{Name: "tables", Type: "string", Description: "Tables"},
	}
	r.Register(contracts.BuildContract("ca ask", "ca", "Ask a question", caFlags, false, false))

	return r
}

func TestCanExecuteMCPCommand_AllowsReadOnly(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")

	tests := []struct {
		input string
	}{
		{"datasets list"},
		{"dcx datasets list"},
		{"  datasets   list  "},
		{"spanner databases get-ddl"},
		{"ca ask"},
	}
	for _, tt := range tests {
		c, err := s.CanExecuteMCPCommand(tt.input)
		if err != nil {
			t.Errorf("CanExecuteMCPCommand(%q) = error %v, want allowed", tt.input, err)
		}
		if c == nil {
			t.Errorf("CanExecuteMCPCommand(%q) returned nil contract", tt.input)
		}
	}
}

func TestCanExecuteMCPCommand_RejectsBlocked(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")

	tests := []struct {
		input   string
		wantMsg string
	}{
		{"nonexistent", "unknown command"},
		{"datasets delete", "read-only"},
		{"spanner operations wait", "long-polling"},
		{"auth check", "not available via MCP"},
		{"meta commands", "not available via MCP"},
		{"completion", "not available via MCP"},
		{"mcp serve", "not available via MCP"},
		{"profiles list", "not available via MCP"},
	}
	for _, tt := range tests {
		_, err := s.CanExecuteMCPCommand(tt.input)
		if err == nil {
			t.Errorf("CanExecuteMCPCommand(%q) = nil error, want rejection", tt.input)
			continue
		}
		if !contains(err.Error(), tt.wantMsg) {
			t.Errorf("CanExecuteMCPCommand(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.wantMsg)
		}
	}
}

func TestCanExecuteMCPCommand_NormalizesWhitespace(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")

	// All should resolve to the same contract.
	variants := []string{
		"datasets list",
		"dcx datasets list",
		"  datasets   list  ",
		"dcx  datasets  list",
	}
	for _, v := range variants {
		c, err := s.CanExecuteMCPCommand(v)
		if err != nil {
			t.Errorf("CanExecuteMCPCommand(%q) = error %v", v, err)
			continue
		}
		if c.Command != "dcx datasets list" {
			t.Errorf("CanExecuteMCPCommand(%q) resolved to %q, want %q", v, c.Command, "dcx datasets list")
		}
	}
}

func TestBuildToolCallArgs_DeterministicOrder(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")
	contract, _ := s.CanExecuteMCPCommand("datasets list")

	args := map[string]interface{}{
		"project-id": "P",
		"location":   "US",
		"dataset-id": "D",
	}

	// Run multiple times to verify determinism.
	for i := 0; i < 10; i++ {
		result := s.buildArgs(contract, "dcx_datasets_list", args)
		// Flags should be in sorted order after the command segments and --format.
		// Expected: ["datasets", "list", "--format", "json-minified", "--dataset-id", "D", "--location", "US", "--project-id", "P"]
		expected := []string{"datasets", "list", "--format", "json-minified", "--dataset-id", "D", "--location", "US", "--project-id", "P"}
		if len(result) != len(expected) {
			t.Fatalf("buildArgs attempt %d: got %v, want %v", i, result, expected)
		}
		for j := range expected {
			if result[j] != expected[j] {
				t.Fatalf("buildArgs attempt %d: position %d got %q, want %q\nfull: %v", i, j, result[j], expected[j], result)
			}
		}
	}
}

func TestBuildToolCallArgs_PositionalInContractOrder(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")
	contract, _ := s.CanExecuteMCPCommand("ca ask")

	args := map[string]interface{}{
		"question": "how many rows?",
		"agent":    "my-agent",
		"tables":   "p.d.t",
	}

	result := s.buildArgs(contract, "dcx_ca_ask", args)
	// Flags sorted, then positional at end.
	// Expected: ["ca", "ask", "--format", "json-minified", "--agent", "my-agent", "--tables", "p.d.t", "how many rows?"]
	last := result[len(result)-1]
	if last != "how many rows?" {
		t.Errorf("positional arg should be last, got %q at end\nfull: %v", last, result)
	}
	// "question" should NOT appear as "--question"
	for _, a := range result {
		if a == "--question" {
			t.Errorf("positional arg 'question' should not be a flag\nfull: %v", result)
		}
	}
}

func TestBuildToolCallArgs_MissingRequiredPositional(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx")
	contract, _ := s.CanExecuteMCPCommand("ca ask")

	// Missing "question" which is required + positional.
	args := map[string]interface{}{
		"agent":  "my-agent",
		"tables": "p.d.t",
	}

	err := s.validateRequiredPositionals(contract, args)
	if err == nil {
		t.Error("expected error for missing required positional, got nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
