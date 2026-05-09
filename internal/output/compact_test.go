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
