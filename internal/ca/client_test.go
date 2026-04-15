package ca

import (
	"encoding/json"
	"strings"
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

// --- parseStreamingChat tests ---

// jsonArrayResponse is a realistic JSON array wire format.
const jsonArrayResponse = `[
  {"systemMessage":{"text":{"parts":["Analyzing context"],"textType":"THINKING"}}},
  {"systemMessage":{"text":{"parts":["Counting rows"],"textType":"THINKING"}}},
  {"systemMessage":{"data":{"generatedSql":"SELECT COUNT(*) FROM t"}}},
  {"systemMessage":{"data":{"result":{"data":[{"count":42}]}}}},
  {"systemMessage":{"text":{"parts":["There are 42 rows."],"textType":"FINAL_RESPONSE"}}},
  {"systemMessage":{"text":{"parts":["How many columns?"],"textType":"SUGGESTED_QUESTION"}}}
]`

// newlineDelimitedResponse is the same content in newline-delimited format.
const newlineDelimitedResponse = `{"systemMessage":{"text":{"parts":["Analyzing context"],"textType":"THINKING"}}}
{"systemMessage":{"text":{"parts":["Counting rows"],"textType":"THINKING"}}}
{"systemMessage":{"data":{"generatedSql":"SELECT COUNT(*) FROM t"}}}
{"systemMessage":{"data":{"result":{"data":[{"count":42}]}}}}
{"systemMessage":{"text":{"parts":["There are 42 rows."],"textType":"FINAL_RESPONSE"}}}
{"systemMessage":{"text":{"parts":["How many columns?"],"textType":"SUGGESTED_QUESTION"}}}`

func TestParseStreamingChat_JSONArray(t *testing.T) {
	result, err := parseStreamingChat(
		strings.NewReader(jsonArrayResponse), "how many?", "my-agent", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAskResult(t, result, "my-agent")
}

func TestParseStreamingChat_NewlineDelimited(t *testing.T) {
	result, err := parseStreamingChat(
		strings.NewReader(newlineDelimitedResponse), "how many?", "my-agent", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertAskResult(t, result, "my-agent")
}

func TestParseStreamingChat_LeadingWhitespace(t *testing.T) {
	// JSON array with leading whitespace — peekNonSpace should skip it.
	result, err := parseStreamingChat(
		strings.NewReader("  \n\t"+jsonArrayResponse), "q", "", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SQL != "SELECT COUNT(*) FROM t" {
		t.Errorf("SQL = %q", result.SQL)
	}
}

func TestParseStreamingChat_EmptyBody(t *testing.T) {
	_, err := parseStreamingChat(
		strings.NewReader(""), "q", "", nil,
	)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestParseStreamingChat_MalformedArrayElement(t *testing.T) {
	// A broken message inside a JSON array should return an error.
	body := `[{"systemMessage":{"text":{"parts":["ok"],"textType":"THINKING"}}}, INVALID]`
	_, err := parseStreamingChat(strings.NewReader(body), "q", "", nil)
	if err == nil {
		t.Fatal("expected error for malformed array element")
	}
}

func TestParseStreamingChat_Callback(t *testing.T) {
	var events []StreamEvent
	cb := func(e StreamEvent) {
		events = append(events, e)
	}

	_, err := parseStreamingChat(
		strings.NewReader(jsonArrayResponse), "q", "", cb,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected events: 2 thinking + 1 SQL + 1 result + 1 answer + 1 thinking (suggested question)
	if len(events) < 5 {
		t.Fatalf("expected at least 5 events, got %d: %+v", len(events), events)
	}

	// First two should be thinking.
	if events[0].Type != EventThinking || events[0].Text != "Analyzing context" {
		t.Errorf("event[0] = %+v, want thinking 'Analyzing context'", events[0])
	}
	if events[1].Type != EventThinking || events[1].Text != "Counting rows" {
		t.Errorf("event[1] = %+v, want thinking 'Counting rows'", events[1])
	}

	// Find the SQL event.
	foundSQL := false
	for _, e := range events {
		if e.Type == EventSQL && e.Text == "SELECT COUNT(*) FROM t" {
			foundSQL = true
		}
	}
	if !foundSQL {
		t.Error("missing EventSQL with expected query")
	}

	// Find the answer event.
	foundAnswer := false
	for _, e := range events {
		if e.Type == EventAnswer && e.Text == "There are 42 rows." {
			foundAnswer = true
		}
	}
	if !foundAnswer {
		t.Error("missing EventAnswer with expected text")
	}
}

func TestParseStreamingChat_NilCallback(t *testing.T) {
	// Ensure nil callback doesn't panic.
	result, err := parseStreamingChat(
		strings.NewReader(jsonArrayResponse), "q", "", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func assertAskResult(t *testing.T, result *AskResult, wantAgent string) {
	t.Helper()
	if result.Question != "how many?" {
		t.Errorf("Question = %q, want 'how many?'", result.Question)
	}
	if result.Agent != wantAgent {
		t.Errorf("Agent = %q, want %q", result.Agent, wantAgent)
	}
	if result.SQL != "SELECT COUNT(*) FROM t" {
		t.Errorf("SQL = %q, want 'SELECT COUNT(*) FROM t'", result.SQL)
	}
	if result.Explanation != "There are 42 rows." {
		t.Errorf("Explanation = %q, want 'There are 42 rows.'", result.Explanation)
	}
	if result.Results == nil {
		t.Error("Results should not be nil")
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
