package discovery

import (
	"os"
	"sort"
	"testing"
)

func TestParseBigQueryDiscovery(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/bigquery_v2_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := BigQueryConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(commands) != 10 {
		t.Errorf("expected 10 commands, got %d", len(commands))
		for _, cmd := range commands {
			t.Logf("  %s", cmd.CommandPath)
		}
	}

	// Verify expected command paths.
	paths := make(map[string]bool)
	for _, cmd := range commands {
		paths[cmd.CommandPath] = true
	}

	expected := []string{
		"datasets list", "datasets get", "tables list", "tables get",
		"jobs list", "jobs get", "models list", "models get",
		"routines list", "routines get",
	}
	for _, exp := range expected {
		if !paths[exp] {
			t.Errorf("missing expected command: %s", exp)
		}
	}
}

func TestParseBigQueryDatasetsListMethod(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/bigquery_v2_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := BigQueryConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var listCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "datasets list" {
			listCmd = &commands[i]
			break
		}
	}
	if listCmd == nil {
		t.Fatal("datasets list command not found")
	}

	if listCmd.Method.HTTPMethod != "GET" {
		t.Errorf("HTTPMethod = %s, want GET", listCmd.Method.HTTPMethod)
	}
	if listCmd.Method.Path == "" {
		t.Error("Path should not be empty")
	}

	// projectId should be mapped to global flag, not a command flag.
	for _, flag := range listCmd.CommandFlags {
		if flag.Name == "projectId" {
			t.Error("projectId should be mapped to global flag, not command flag")
		}
	}

	// Should have query params as command flags (maxResults, filter, etc.)
	hasFilter := false
	for _, flag := range listCmd.CommandFlags {
		if flag.Name == "filter" {
			hasFilter = true
		}
	}
	if !hasFilter {
		t.Error("expected 'filter' as a command flag")
	}
}

func TestTablesGetExposesTableIdFlag(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/bigquery_v2_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := BigQueryConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var getCmd *GeneratedCommand
	for i := range commands {
		if commands[i].CommandPath == "tables get" {
			getCmd = &commands[i]
			break
		}
	}
	if getCmd == nil {
		t.Fatal("tables get command not found")
	}

	hasTableId := false
	for _, flag := range getCmd.CommandFlags {
		if flag.Name == "tableId" {
			hasTableId = true
			if !flag.Required {
				t.Error("tableId should be required")
			}
			if flag.Location != "path" {
				t.Errorf("tableId location = %s, want 'path'", flag.Location)
			}
		}
	}
	if !hasTableId {
		t.Errorf("tables get must expose tableId as a command flag; got flags: %v",
			func() []string {
				var names []string
				for _, f := range getCmd.CommandFlags {
					names = append(names, f.Name)
				}
				return names
			}())
	}
}

func TestParseSpannerDiscovery(t *testing.T) {
	docJSON, err := os.ReadFile("../../assets/spanner_v1_discovery.json")
	if err != nil {
		t.Fatalf("reading discovery doc: %v", err)
	}

	svc := SpannerConfig()
	commands, err := Parse(docJSON, svc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	paths := make(map[string]bool)
	for _, cmd := range commands {
		paths[cmd.CommandPath] = true
	}

	expected := []string{
		"spanner instances list",
		"spanner instances get",
		"spanner databases list",
		"spanner databases get",
		"spanner databases get-ddl",
	}
	for _, exp := range expected {
		if !paths[exp] {
			t.Errorf("missing expected command: %s (have: %v)", exp, sortedKeys(paths))
		}
	}
}

func TestCamelToKebab(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"getDdl", "get-ddl"},
		{"datasets list", "datasets list"},
		{"spanner databases getDdl", "spanner databases get-ddl"},
		{"maxResults", "max-results"},
	}
	for _, tt := range tests {
		got := camelToKebab(tt.input)
		if got != tt.want {
			t.Errorf("camelToKebab(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
