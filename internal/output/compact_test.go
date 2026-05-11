package output

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactJSON_FullPassthrough(t *testing.T) {
	data := []byte(`{"items":[{"id":"a"},{"id":"b"}],"source":"test"}`)
	result := CompactJSON(data, "full")
	if string(result) != string(data) {
		t.Errorf("full mode should return data unchanged")
	}
}

func TestCompactJSON_EmptyMode(t *testing.T) {
	data := []byte(`{"x":1}`)
	result := CompactJSON(data, "")
	if string(result) != string(data) {
		t.Error("empty mode should return data unchanged")
	}
}

func TestCompactJSON_CompactListEnvelope(t *testing.T) {
	data := []byte(`{"items":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"}],"source":"BQ"}`)
	result := CompactJSON(data, "compact")

	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("compact result not valid JSON: %v", err)
	}
	// Should have count, sample (3), fields, source.
	if m["count"] != float64(4) {
		t.Errorf("count = %v, want 4", m["count"])
	}
	sample, ok := m["sample"].([]interface{})
	if !ok || len(sample) != 3 {
		t.Errorf("sample should have 3 items, got %v", m["sample"])
	}
	if m["source"] != "BQ" {
		t.Errorf("source not preserved: %v", m["source"])
	}
}

func TestCompactJSON_CountOnly(t *testing.T) {
	data := []byte(`{"items":[1,2,3,4,5],"source":"test"}`)
	result := CompactJSON(data, "count_only")

	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["count"] != float64(5) {
		t.Errorf("count = %v, want 5", m["count"])
	}
	if m["source"] != "test" {
		t.Error("source not preserved")
	}
	if _, has := m["items"]; has {
		t.Error("count_only should not include items")
	}
}

func TestCompactJSON_SchemaOnly(t *testing.T) {
	data := []byte(`{"items":[{"name":"alice","age":30,"active":true}]}`)
	result := CompactJSON(data, "schema_only")

	var m map[string]interface{}
	json.Unmarshal(result, &m)
	fields, ok := m["fields"].([]interface{})
	if !ok {
		t.Fatalf("fields should be an array, got %T", m["fields"])
	}
	if len(fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(fields))
	}
}

func TestCompactJSON_NonJSON(t *testing.T) {
	data := []byte("not json at all")
	result := CompactJSON(data, "compact")
	if string(result) != string(data) {
		t.Error("non-JSON should return raw data")
	}
}

func TestCompactJSON_TrailingContent(t *testing.T) {
	data := []byte(`{"x":1} extra trailing content`)
	result := CompactJSON(data, "compact")
	if string(result) != string(data) {
		t.Error("trailing content should return raw data")
	}
}

func TestCompactJSON_LargeIntegerPreserved(t *testing.T) {
	// json.Number should be preserved through compaction.
	data := []byte(`{"items":[{"big_id":9007199254740993}],"source":"test"}`)
	result := CompactJSON(data, "compact")
	if !strings.Contains(string(result), "9007199254740993") {
		t.Errorf("large integer corrupted: %s", string(result))
	}
}

func TestCompactJSON_InvalidMode(t *testing.T) {
	if ValidResultModes["invalid"] {
		t.Error("invalid should not be a valid mode")
	}
	if !ValidResultModes["compact"] {
		t.Error("compact should be valid")
	}
}

func TestResultModeNames(t *testing.T) {
	names := ResultModeNames()
	if len(names) != 4 {
		t.Errorf("expected 4 mode names, got %d", len(names))
	}
	// Should list all modes.
	expected := map[string]bool{"full": true, "compact": true, "count_only": true, "schema_only": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected mode name: %s", n)
		}
	}
}

// CA-aware compaction tests.

func TestCompactCAResult_Compact(t *testing.T) {
	data := map[string]interface{}{
		"question":    "top events",
		"source":      "BigQuery",
		"agent":       "my-agent",
		"sql":         "SELECT event_type, COUNT(*) ...",
		"explanation": "The top events are...",
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"event_type": "LLM_RESPONSE", "count": float64(54)},
				map[string]interface{}{"event_type": "LLM_REQUEST", "count": float64(52)},
				map[string]interface{}{"event_type": "AGENT_COMPLETED", "count": float64(35)},
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "event_type", "type": "STRING"},
					map[string]interface{}{"name": "count", "type": "INTEGER"},
				},
			},
			"name": "top_events",
		},
	}

	result := CompactResult(data, "compact")
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("compact CA result should be a map, got %T", result)
	}
	if m["question"] != "top events" {
		t.Errorf("question not preserved: %v", m["question"])
	}
	if m["source"] != "BigQuery" {
		t.Errorf("source not preserved: %v", m["source"])
	}
	if m["sql"] == nil {
		t.Error("sql should be preserved")
	}
	nested, ok := m["results"].(map[string]interface{})
	if !ok {
		t.Fatalf("results should be a map, got %T", m["results"])
	}
	if nested["count"] != 3 {
		t.Errorf("results.count = %v, want 3", nested["count"])
	}
	if nested["name"] != "top_events" {
		t.Errorf("results.name not preserved: %v", nested["name"])
	}
	// Fields should come from schema, not first-row inference.
	fields, ok := nested["fields"].([]string)
	if !ok {
		t.Fatalf("fields should be []string, got %T", nested["fields"])
	}
	// Schema order: event_type, count (not sorted alphabetically).
	if len(fields) != 2 || fields[0] != "event_type" || fields[1] != "count" {
		t.Errorf("fields should come from schema order: %v", fields)
	}
}

func TestCompactCAResult_CountOnly(t *testing.T) {
	data := map[string]interface{}{
		"question": "how many?",
		"source":   "BigQuery",
		"agent":    "my-agent",
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"total": float64(318)},
			},
		},
	}

	result := CompactResult(data, "count_only")
	m := result.(map[string]interface{})
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
	if m["source"] != "BigQuery" {
		t.Errorf("source not preserved")
	}
	if m["agent"] != "my-agent" {
		t.Errorf("agent not preserved")
	}
}

func TestCompactCAResult_SchemaOnly(t *testing.T) {
	data := map[string]interface{}{
		"question": "top events",
		"source":   "BigQuery",
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"event_type": "X", "count": float64(1)},
				map[string]interface{}{"event_type": "Y", "count": float64(2)},
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "event_type", "type": "STRING"},
					map[string]interface{}{"name": "count", "type": "INTEGER"},
				},
			},
		},
	}

	result := CompactResult(data, "schema_only")
	m := result.(map[string]interface{})
	fields, ok := m["fields"].([]map[string]string)
	if !ok {
		t.Fatalf("fields should be []map[string]string, got %T", m["fields"])
	}
	if len(fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fields))
	}
	// Should use schema field types (STRING, INTEGER), not JSON type inference.
	if fields[0]["type"] != "STRING" {
		t.Errorf("field type should be STRING from schema, got %q", fields[0]["type"])
	}
}

func TestCompactCAResult_ZeroRows(t *testing.T) {
	data := map[string]interface{}{
		"question": "empty",
		"source":   "BigQuery",
		"results": map[string]interface{}{
			"data": []interface{}{},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "x", "type": "STRING"},
				},
			},
		},
	}

	result := CompactResult(data, "compact")
	m := result.(map[string]interface{})
	nested := m["results"].(map[string]interface{})
	if nested["count"] != 0 {
		t.Errorf("zero-row count = %v, want 0", nested["count"])
	}
}

func TestCompactCAResult_AnswerOnly(t *testing.T) {
	// CA response with explanation but no tabular results.
	data := map[string]interface{}{
		"question":    "what is this?",
		"source":      "BigQuery",
		"explanation": "This is a dataset about...",
		"results":     map[string]interface{}{},
	}

	result := CompactResult(data, "compact")
	m := result.(map[string]interface{})
	if m["question"] != "what is this?" {
		t.Errorf("question not preserved: %v", m["question"])
	}
	if m["source"] != "BigQuery" {
		t.Errorf("source not preserved")
	}
	if m["explanation"] != "This is a dataset about..." {
		t.Errorf("explanation not preserved: %v", m["explanation"])
	}
	// Should not return keys-only generic output.
	if _, hasKeys := m["keys"]; hasKeys {
		t.Error("should not fall through to generic keys-only compaction")
	}
}

func TestCompactCAResult_NoResultsKey(t *testing.T) {
	// Regression: AskResult.Results is omitempty. When CA returns only
	// a natural-language answer, results is omitted after JSON round-trip.
	data := map[string]interface{}{
		"question":    "explain this dataset",
		"source":      "BigQuery",
		"explanation": "This dataset contains agent event logs...",
		"agent":       "my-agent",
	}

	result := CompactResult(data, "compact")
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("compact should return a map, got %T", result)
	}
	if m["question"] != "explain this dataset" {
		t.Errorf("question not preserved: %v", m["question"])
	}
	if m["explanation"] != "This dataset contains agent event logs..." {
		t.Errorf("explanation not preserved: %v", m["explanation"])
	}
	if m["source"] != "BigQuery" {
		t.Errorf("source not preserved")
	}
	if _, hasKeys := m["keys"]; hasKeys {
		t.Error("should not fall through to generic keys-only compaction")
	}
	if _, hasType := m["type"]; hasType {
		t.Error("should not return generic object type")
	}
}

func TestIsCAResult(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		want bool
	}{
		{"CA result with results", map[string]interface{}{"results": map[string]interface{}{}, "question": "x"}, true},
		{"CA result with source", map[string]interface{}{"results": map[string]interface{}{}, "source": "BQ"}, true},
		{"list envelope", map[string]interface{}{"items": []interface{}{}, "source": "BQ"}, false},
		{"answer-only no results (question+explanation)", map[string]interface{}{"question": "x", "explanation": "y"}, true},
		{"answer-only no results (question+source)", map[string]interface{}{"question": "x", "source": "BQ"}, true},
		{"question only (no second CA field)", map[string]interface{}{"question": "x"}, false},
		{"no question no results", map[string]interface{}{"sql": "y", "source": "BQ"}, false},
		{"results but no CA fields", map[string]interface{}{"results": map[string]interface{}{}, "foo": "bar"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCAResult(tt.m); got != tt.want {
				t.Errorf("isCAResult = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONTypeOf(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"hello", "string"},
		{json.Number("42"), "number"},
		{float64(3.14), "number"},
		{true, "boolean"},
		{nil, "null"},
		{map[string]interface{}{}, "object"},
		{[]interface{}{}, "array"},
	}
	for _, tt := range tests {
		got := JSONTypeOf(tt.input)
		if got != tt.want {
			t.Errorf("JSONTypeOf(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
