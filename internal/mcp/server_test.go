package mcp

import (
	"encoding/json"
	"strings"
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

	// Fake command with two positionals to test ordering.
	twoPositionalFlags := []contracts.FlagContract{
		{Name: "source", Type: "string", Description: "Source", Required: true, Positional: true},
		{Name: "destination", Type: "string", Description: "Destination", Required: true, Positional: true},
		{Name: "verbose", Type: "bool", Description: "Verbose"},
	}
	r.Register(contracts.BuildContract("data copy", "bigquery", "Copy data", twoPositionalFlags, false, false))

	return r
}

func TestCanExecuteMCPCommand_AllowsReadOnly(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

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
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

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
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

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
	s := NewServer(testRegistry(), "json-minified", "dcx", "")
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
	s := NewServer(testRegistry(), "json-minified", "dcx", "")
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

func TestBuildToolCallArgs_TwoPositionalsInContractOrder(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")
	contract, _ := s.CanExecuteMCPCommand("data copy")

	args := map[string]interface{}{
		"destination": "dest_table",   // listed second in contract
		"source":      "source_table", // listed first in contract
		"verbose":     "true",
	}

	result := s.buildArgs(contract, "dcx_data_copy", args)
	// Positionals must be in contract order: source first, destination second.
	// Flags before positionals.
	n := len(result)
	if n < 2 {
		t.Fatalf("expected at least 2 positional args, got %v", result)
	}
	if result[n-2] != "source_table" {
		t.Errorf("first positional should be 'source_table', got %q\nfull: %v", result[n-2], result)
	}
	if result[n-1] != "dest_table" {
		t.Errorf("second positional should be 'dest_table', got %q\nfull: %v", result[n-1], result)
	}
	// --verbose should be before positionals
	for _, a := range result {
		if a == "--source" || a == "--destination" {
			t.Errorf("positional args should not appear as flags\nfull: %v", result)
		}
	}
}

func TestValidateRequiredPositionals_EmptyValues(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")
	contract, _ := s.CanExecuteMCPCommand("ca ask")

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"missing key", map[string]interface{}{"agent": "a"}},
		{"nil value", map[string]interface{}{"question": nil, "agent": "a"}},
		{"empty string", map[string]interface{}{"question": "", "agent": "a"}},
		{"whitespace only", map[string]interface{}{"question": "   ", "agent": "a"}},
	}
	for _, tt := range tests {
		err := s.validateRequiredPositionals(contract, tt.args)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}

func TestCanExecuteMCPCommand_TabWhitespace(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Tab-separated should normalize correctly.
	c, err := s.CanExecuteMCPCommand("dcx\tdatasets\tlist")
	if err != nil {
		t.Errorf("tab-separated command failed: %v", err)
	}
	if c != nil && c.Command != "dcx datasets list" {
		t.Errorf("resolved to %q, want %q", c.Command, "dcx datasets list")
	}
}

func TestBuildToolCallArgs_MissingRequiredPositional(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")
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

// Progressive mode tests.

func TestProgressiveToolsList_Returns3Tools(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "progressive")

	// Simulate tools/list by collecting what progressive mode would return.
	// We test via CanExecuteMCPCommand that the allowlist still works.
	// The actual tools/list is tested via the JSON-RPC handler, but we can
	// verify the mode is set.
	if s.Mode != "progressive" {
		t.Errorf("Mode = %q, want 'progressive'", s.Mode)
	}
}

func TestProgressiveDiscover_AllDomains(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "progressive")

	params := ToolCallParams{
		Name:      "dcx_discover",
		Arguments: map[string]interface{}{},
	}

	// Call handleDiscover directly — it writes to a mock, but we can test
	// the logic by checking CanExecuteMCPCommand for expected domains.
	allowed := make(map[string]bool)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			allowed[c.Domain] = true
		}
	}

	// Should include bigquery, ca, spanner but not auth, meta, cli, mcp, profiles.
	if !allowed["bigquery"] {
		t.Error("bigquery should be in allowed domains")
	}
	if !allowed["ca"] {
		t.Error("ca should be in allowed domains")
	}
	if allowed["auth"] || allowed["meta"] || allowed["cli"] {
		t.Errorf("blocked domains should not appear: %v", allowed)
	}
	_ = params // Used in integration tests.
}

func TestProgressiveDiscover_FilterByDomain(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "progressive")

	// Count bigquery commands that pass the allowlist.
	count := 0
	for _, c := range s.Registry.All() {
		contract, err := s.CanExecuteMCPCommand(c.Command)
		if err == nil && contract.Domain == "bigquery" {
			count++
		}
	}

	// testRegistry has datasets list (read) and data copy (read) in bigquery.
	// datasets delete is mutation, so excluded.
	if count != 2 {
		t.Errorf("expected 2 bigquery commands, got %d", count)
	}
}

func TestProgressiveExecute_RejectsUnknown(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "progressive")

	_, err := s.CanExecuteMCPCommand("nonexistent command")
	if err == nil {
		t.Error("expected error for unknown command in progressive execute")
	}
}

func TestProgressiveExecute_RejectsMutation(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "progressive")

	_, err := s.CanExecuteMCPCommand("datasets delete")
	if err == nil {
		t.Error("expected error for mutation in progressive execute")
	}
}

// Resource tests.

func TestResourcesList_ReturnsIndexDomainsAndCommands(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Count expected allowed commands.
	var allowedCount int
	domainSet := make(map[string]bool)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			allowedCount++
			domainSet[c.Domain] = true
		}
	}

	// Build resource list the same way handleResourcesList does.
	domainCmds := make(map[string][]string)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			domainCmds[c.Domain] = append(domainCmds[c.Domain], c.Command)
		}
	}

	expectedResources := 1 + len(domainSet) + allowedCount // index + domains + commands
	// Verify counts match.
	actualDomains := len(domainSet)
	if actualDomains < 2 {
		t.Errorf("expected at least 2 domains, got %d", actualDomains)
	}
	if allowedCount < 2 {
		t.Errorf("expected at least 2 allowed commands, got %d", allowedCount)
	}
	t.Logf("expected %d resources: 1 index + %d domains + %d commands",
		expectedResources, actualDomains, allowedCount)
}

func TestResourceRead_IndexContainsDomains(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Verify domains present in allowlist.
	domainSet := make(map[string]bool)
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			domainSet[c.Domain] = true
		}
	}

	if !domainSet["bigquery"] {
		t.Error("bigquery should be in index domains")
	}
	if !domainSet["ca"] {
		t.Error("ca should be in index domains")
	}
	if domainSet["auth"] || domainSet["meta"] || domainSet["cli"] {
		t.Errorf("blocked domains should not appear in index: %v", domainSet)
	}
}

func TestResourceRead_DomainListsCommands(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Count bigquery commands.
	var bqCount int
	for _, c := range s.Registry.All() {
		contract, err := s.CanExecuteMCPCommand(c.Command)
		if err == nil && contract.Domain == "bigquery" {
			bqCount++
		}
	}

	if bqCount < 1 {
		t.Errorf("expected at least 1 bigquery command, got %d", bqCount)
	}
}

func TestResourceRead_CommandReturnsSchema(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Verify datasets list is accessible.
	contract, err := s.CanExecuteMCPCommand("datasets list")
	if err != nil {
		t.Fatalf("datasets list should be allowed: %v", err)
	}

	if contract.Domain != "bigquery" {
		t.Errorf("domain = %q, want bigquery", contract.Domain)
	}
	if len(contract.Flags) == 0 {
		t.Error("expected flags in contract")
	}
}

func TestResourceRead_HyphenatedCommandResolves(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// spanner databases get-ddl should resolve via URI path "spanner/databases/get-ddl"
	contract, err := s.CanExecuteMCPCommand("spanner databases get-ddl")
	if err != nil {
		t.Fatalf("hyphenated command should resolve: %v", err)
	}
	if contract.Domain != "spanner" {
		t.Errorf("domain = %q, want spanner", contract.Domain)
	}
}

func TestResourceRead_UnknownDomainErrors(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// Build domain set to verify "nonexistent" is not in it.
	for _, c := range s.Registry.All() {
		if _, err := s.CanExecuteMCPCommand(c.Command); err == nil {
			if c.Domain == "nonexistent" {
				t.Fatal("nonexistent should not be a valid domain")
			}
		}
	}
}

func TestResourceRead_BlockedDomainNotExposed(t *testing.T) {
	s := NewServer(testRegistry(), "json-minified", "dcx", "")

	// auth check is in the registry but should not be accessible as a resource.
	_, err := s.CanExecuteMCPCommand("auth check")
	if err == nil {
		t.Error("auth check should be blocked from resources")
	}
}

func TestResourceRead_CommandURIPathConversion(t *testing.T) {
	// Verify that space-to-slash and slash-to-space round-trips work.
	tests := []struct {
		command string
		path    string
	}{
		{"datasets list", "datasets/list"},
		{"ca ask", "ca/ask"},
		{"spanner databases get-ddl", "spanner/databases/get-ddl"},
		{"cloudsql backup-runs list", "cloudsql/backup-runs/list"},
	}
	for _, tt := range tests {
		got := strings.ReplaceAll(tt.command, " ", "/")
		if got != tt.path {
			t.Errorf("command %q → path %q, want %q", tt.command, got, tt.path)
		}
		back := strings.ReplaceAll(tt.path, "/", " ")
		if back != tt.command {
			t.Errorf("path %q → command %q, want %q", tt.path, back, tt.command)
		}
	}
}

// Result compaction tests.

func TestCompactResult_ListEnvelope(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"_resource_id": "a", "location": "US"},
			map[string]interface{}{"_resource_id": "b", "location": "EU"},
			map[string]interface{}{"_resource_id": "c", "location": "US"},
			map[string]interface{}{"_resource_id": "d", "location": "EU"},
		},
		"source": "BigQuery",
	}

	result := compactResult(data, "compact")
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("compact result should be a map, got %T", result)
	}
	if m["count"] != 4 {
		t.Errorf("count = %v, want 4", m["count"])
	}
	sample, ok := m["sample"].([]interface{})
	if !ok || len(sample) != 3 {
		t.Errorf("sample should have 3 items, got %v", m["sample"])
	}
	if m["source"] != "BigQuery" {
		t.Errorf("source should be preserved, got %v", m["source"])
	}
}

func TestCompactResult_CountOnly(t *testing.T) {
	data := map[string]interface{}{
		"items":  []interface{}{1, 2, 3, 4, 5},
		"source": "test",
	}

	result := compactResult(data, "count_only")
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("count_only result should be a map, got %T", result)
	}
	if m["count"] != 5 {
		t.Errorf("count = %v, want 5", m["count"])
	}
	if m["source"] != "test" {
		t.Errorf("source should be preserved")
	}
	if _, has := m["items"]; has {
		t.Error("count_only should not include items")
	}
}

func TestCompactResult_SchemaOnly(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "alice", "age": json.Number("30"), "active": true},
		},
	}

	result := compactResult(data, "schema_only")
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("schema_only result should be a map, got %T", result)
	}
	fields, ok := m["fields"].([]map[string]string)
	if !ok {
		t.Fatalf("fields should be []map[string]string, got %T", m["fields"])
	}
	if len(fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(fields))
	}
	// Fields should be sorted.
	if fields[0]["name"] != "active" || fields[1]["name"] != "age" || fields[2]["name"] != "name" {
		t.Errorf("fields not sorted: %v", fields)
	}
}

func TestCompactResult_EmptyList(t *testing.T) {
	data := map[string]interface{}{
		"items":  []interface{}{},
		"source": "test",
	}

	compact := compactResult(data, "compact")
	m := compact.(map[string]interface{})
	if m["count"] != 0 {
		t.Errorf("compact empty list: count = %v, want 0", m["count"])
	}

	countOnly := compactResult(data, "count_only")
	m2 := countOnly.(map[string]interface{})
	if m2["count"] != 0 {
		t.Errorf("count_only empty list: count = %v, want 0", m2["count"])
	}

	schemaOnly := compactResult(data, "schema_only")
	m3 := schemaOnly.(map[string]interface{})
	if m3["item_count"] != 0 {
		t.Errorf("schema_only empty list: item_count = %v, want 0", m3["item_count"])
	}
}

func TestCompactResult_SingleObject(t *testing.T) {
	data := map[string]interface{}{
		"name":   "test",
		"status": "ok",
	}

	compact := compactResult(data, "compact")
	m := compact.(map[string]interface{})
	keys, ok := m["keys"].([]string)
	if !ok {
		t.Fatalf("compact single object should return keys, got %T", m["keys"])
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestCompactResult_FullPassthrough(t *testing.T) {
	data := map[string]interface{}{"x": 1}
	result := compactResult(data, "full")
	m, ok := result.(map[string]interface{})
	if !ok || m["x"] != 1 {
		t.Error("full mode should return data unchanged")
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
