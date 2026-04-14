package ca

import (
	"encoding/json"
	"testing"

	"github.com/haiyuan-eng-google/dcx-cli/internal/profiles"
)

func TestSourceName(t *testing.T) {
	tests := []struct {
		st   profiles.SourceType
		want string
	}{
		{profiles.BigQuery, "BigQuery"},
		{profiles.Spanner, "Spanner"},
		{profiles.AlloyDB, "AlloyDB"},
		{profiles.CloudSQL, "Cloud SQL"},
		{profiles.Looker, "Looker"},
	}
	for _, tt := range tests {
		got := sourceName(tt.st)
		if got != tt.want {
			t.Errorf("sourceName(%s) = %s, want %s", tt.st, got, tt.want)
		}
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient(nil)
	if c.HTTPClient == nil {
		t.Error("NewClient(nil) should set default HTTP client")
	}
}

func TestParseBQTableRef(t *testing.T) {
	tests := []struct {
		input string
		want  BigQueryTableReference
	}{
		{"proj.ds.tbl", BigQueryTableReference{ProjectID: "proj", DatasetID: "ds", TableID: "tbl"}},
		{"ds.tbl", BigQueryTableReference{DatasetID: "ds", TableID: "tbl"}},
		{"tbl", BigQueryTableReference{TableID: "tbl"}},
	}
	for _, tt := range tests {
		got := parseBQTableRef(tt.input)
		if got != tt.want {
			t.Errorf("parseBQTableRef(%q) = %+v, want %+v", tt.input, got, tt.want)
		}
	}
}

func TestDataAgentJSONShape(t *testing.T) {
	// Verify the DataAgent struct produces the documented API shape.
	agent := DataAgent{
		DisplayName: "test-agent",
		DataAnalyticsAgent: &DataAnalyticsAgent{
			StagingContext: &AgentContext{
				SystemInstruction: "You help analyze data.",
				DatasourceReferences: &DatasourceReferences{
					BQ: &BigQueryTableReferences{
						TableReferences: []BigQueryTableReference{
							{ProjectID: "proj", DatasetID: "ds", TableID: "events"},
						},
					},
				},
				ExampleQueries: []ExampleQuery{
					{NaturalLanguageQuestion: "How many events?", SQLQuery: "SELECT COUNT(*) FROM events"},
				},
			},
		},
	}

	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	// Verify nested structure matches documented API.
	daa, ok := raw["dataAnalyticsAgent"].(map[string]interface{})
	if !ok {
		t.Fatal("missing dataAnalyticsAgent")
	}
	sc, ok := daa["stagingContext"].(map[string]interface{})
	if !ok {
		t.Fatal("missing stagingContext")
	}
	if sc["systemInstruction"] != "You help analyze data." {
		t.Errorf("systemInstruction = %v", sc["systemInstruction"])
	}
	eqs, ok := sc["exampleQueries"].([]interface{})
	if !ok || len(eqs) != 1 {
		t.Fatalf("exampleQueries should have 1 entry, got %v", sc["exampleQueries"])
	}
	eq := eqs[0].(map[string]interface{})
	if eq["naturalLanguageQuestion"] != "How many events?" {
		t.Errorf("naturalLanguageQuestion = %v", eq["naturalLanguageQuestion"])
	}
	if eq["sqlQuery"] != "SELECT COUNT(*) FROM events" {
		t.Errorf("sqlQuery = %v", eq["sqlQuery"])
	}
}

func TestProfileIsQueryDataSource(t *testing.T) {
	tests := []struct {
		st   profiles.SourceType
		want bool
	}{
		{profiles.Spanner, true},
		{profiles.AlloyDB, true},
		{profiles.CloudSQL, true},
		{profiles.BigQuery, false},
		{profiles.Looker, false},
	}
	for _, tt := range tests {
		p := profiles.Profile{SourceType: tt.st}
		if got := p.IsQueryDataSource(); got != tt.want {
			t.Errorf("Profile{%s}.IsQueryDataSource() = %v, want %v", tt.st, got, tt.want)
		}
	}
}
