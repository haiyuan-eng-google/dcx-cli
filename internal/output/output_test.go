package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    Format
		wantErr bool
	}{
		{"json", JSON, false},
		{"json-minified", JSONMinified, false},
		{"table", Table, false},
		{"text", Text, false},
		{"csv", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseFormat(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("ParseFormat(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestRenderJSON(t *testing.T) {
	value := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}

	out := captureStdout(t, func() {
		if err := Render(JSON, value); err != nil {
			t.Fatal(err)
		}
	})

	// Pretty JSON should have newlines and indentation.
	if !strings.Contains(out, "\n") {
		t.Error("JSON format should produce pretty output with newlines")
	}

	// Verify valid JSON.
	var parsed interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestRenderJSONMinified(t *testing.T) {
	value := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}

	out := captureStdout(t, func() {
		if err := Render(JSONMinified, value); err != nil {
			t.Fatal(err)
		}
	})

	trimmed := strings.TrimSpace(out)
	// Minified should be a single line.
	if strings.Count(trimmed, "\n") > 0 {
		t.Error("json-minified should produce single-line output")
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestRenderText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := Render(Text, "hello world"); err != nil {
			t.Fatal(err)
		}
	})

	if strings.TrimSpace(out) != "hello world" {
		t.Errorf("Text output = %q, want 'hello world'", strings.TrimSpace(out))
	}
}

func TestRenderTextStruct(t *testing.T) {
	type Result struct {
		Name   string `json:"name"`
		Count  int    `json:"count"`
		Active bool   `json:"active"`
	}

	out := captureStdout(t, func() {
		if err := Render(Text, Result{Name: "test", Count: 42, Active: true}); err != nil {
			t.Fatal(err)
		}
	})

	// Should NOT contain raw Go struct syntax.
	if strings.Contains(out, "&{") || strings.Contains(out, ":{") {
		t.Errorf("Text output contains raw Go struct: %q", out)
	}
	// Should contain key-value pairs from JSON tags.
	if !strings.Contains(out, "name: test") {
		t.Errorf("missing 'name: test' in: %q", out)
	}
	if !strings.Contains(out, "count: 42") {
		t.Errorf("missing 'count: 42' in: %q", out)
	}
	if !strings.Contains(out, "active: true") {
		t.Errorf("missing 'active: true' in: %q", out)
	}
}

func TestRenderTextStructWithSlice(t *testing.T) {
	type Item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type ListResult struct {
		Items  []Item `json:"items"`
		Source string `json:"source"`
	}

	out := captureStdout(t, func() {
		if err := Render(Text, ListResult{
			Items:  []Item{{ID: "a", Name: "Alice"}, {ID: "b", Name: "Bob"}},
			Source: "test",
		}); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(out, "&{") {
		t.Errorf("Text output contains raw Go struct: %q", out)
	}
	if !strings.Contains(out, "source: test") {
		t.Errorf("missing 'source: test' in: %q", out)
	}
	if !strings.Contains(out, "id: a") {
		t.Errorf("missing 'id: a' in: %q", out)
	}
}

func TestRenderTextMap(t *testing.T) {
	out := captureStdout(t, func() {
		if err := Render(Text, map[string]interface{}{
			"status": "success",
			"count":  float64(3),
		}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "status: success") {
		t.Errorf("missing 'status: success' in: %q", out)
	}
	if !strings.Contains(out, "count: 3") {
		t.Errorf("missing 'count: 3' in: %q", out)
	}
}

func TestRenderTextLargeInt64(t *testing.T) {
	// Regression: int64 above 2^53 must not be silently rounded
	// by float64 conversion during JSON normalization.
	type Stats struct {
		BytesProcessed int64 `json:"bytes_processed"`
	}

	out := captureStdout(t, func() {
		if err := Render(Text, Stats{BytesProcessed: 9007199254740993}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "bytes_processed: 9007199254740993") {
		t.Errorf("large int64 was corrupted: %q", out)
	}
}

func TestRenderTable(t *testing.T) {
	value := []map[string]interface{}{
		{"name": "events", "location": "US"},
		{"name": "logs", "location": "EU"},
	}

	out := captureStdout(t, func() {
		if err := Render(Table, value); err != nil {
			t.Fatal(err)
		}
	})

	// Table should contain headers and data.
	if !strings.Contains(out, "location") || !strings.Contains(out, "name") {
		t.Error("Table output should contain column headers")
	}
	if !strings.Contains(out, "events") || !strings.Contains(out, "logs") {
		t.Error("Table output should contain row data")
	}
}

func TestRenderTableFromEnvelope(t *testing.T) {
	value := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"id": "1", "status": "ok"},
			map[string]interface{}{"id": "2", "status": "err"},
		},
		"source": "BigQuery",
	}

	out := captureStdout(t, func() {
		if err := Render(Table, value); err != nil {
			t.Fatal(err)
		}
	})

	// Should extract items from envelope.
	if !strings.Contains(out, "id") || !strings.Contains(out, "status") {
		t.Error("Table should extract items from envelope")
	}
}

func TestRenderTableFromStructEnvelope(t *testing.T) {
	// Simulates the ListEnvelope struct from discovery/executor.go.
	type ListEnvelope struct {
		Items         interface{} `json:"items"`
		Source        string      `json:"source"`
		NextPageToken string      `json:"next_page_token,omitempty"`
	}

	value := ListEnvelope{
		Items: []interface{}{
			map[string]interface{}{"id": "1", "name": "foo"},
			map[string]interface{}{"id": "2", "name": "bar"},
		},
		Source: "BigQuery",
	}

	out := captureStdout(t, func() {
		if err := Render(Table, value); err != nil {
			t.Fatal(err)
		}
	})

	// Should extract items from the struct envelope and render tabular rows.
	if !strings.Contains(out, "id") || !strings.Contains(out, "name") {
		t.Errorf("Table should render struct envelope items as columns; got:\n%s", out)
	}
	if !strings.Contains(out, "foo") || !strings.Contains(out, "bar") {
		t.Errorf("Table should render struct envelope item values; got:\n%s", out)
	}
}

func TestFormatNames(t *testing.T) {
	names := FormatNames()
	if len(names) != 4 {
		t.Errorf("FormatNames() returned %d names, want 4", len(names))
	}
}

func TestRenderCAResultTable_MultiRow(t *testing.T) {
	value := map[string]interface{}{
		"question": "top events",
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"event_type": "LLM_RESPONSE", "count": float64(54)},
				map[string]interface{}{"event_type": "LLM_REQUEST", "count": float64(52)},
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

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "event_type") || !strings.Contains(out, "count") {
		t.Errorf("missing table headers in: %s", out)
	}
	if !strings.Contains(out, "LLM_RESPONSE") || !strings.Contains(out, "54") {
		t.Errorf("missing row data in: %s", out)
	}
	if !strings.Contains(out, "(2 rows)") {
		t.Errorf("missing row count in: %s", out)
	}
	if !strings.Contains(out, "name: top_events") {
		t.Errorf("missing metadata 'name' in: %s", out)
	}
}

func TestRenderCAResultTable_SingleRow(t *testing.T) {
	value := map[string]interface{}{
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"total": float64(318)},
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "total", "type": "INTEGER"},
				},
			},
		},
	}

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "total") {
		t.Errorf("missing header in: %s", out)
	}
	if !strings.Contains(out, "318") {
		t.Errorf("missing value in: %s", out)
	}
	if !strings.Contains(out, "(1 rows)") {
		t.Errorf("missing row count in: %s", out)
	}
}

func TestRenderCAResultTable_ZeroRows(t *testing.T) {
	value := map[string]interface{}{
		"results": map[string]interface{}{
			"data": []interface{}{},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "x", "type": "STRING"},
				},
			},
			"name": "empty_result",
		},
	}

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "(0 rows)") {
		t.Errorf("missing (0 rows) in: %s", out)
	}
	if !strings.Contains(out, "name: empty_result") {
		t.Errorf("missing metadata in: %s", out)
	}
}

func TestRenderCAResultTable_NullAndMissingFields(t *testing.T) {
	value := map[string]interface{}{
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"a": "hello", "b": nil},
				map[string]interface{}{"a": "world"}, // "b" missing
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "a", "type": "STRING"},
					map[string]interface{}{"name": "b", "type": "STRING"},
				},
			},
		},
	}

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Count(out, "NULL") != 2 {
		t.Errorf("expected 2 NULLs (nil + missing), got: %s", out)
	}
}

func TestRenderCAResultTable_EscapesNewlines(t *testing.T) {
	value := map[string]interface{}{
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"msg": "line1\nline2\ttab"},
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "msg", "type": "STRING"},
				},
			},
		},
	}

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, `line1\nline2\ttab`) {
		t.Errorf("newline/tab not escaped in: %s", out)
	}
}

func TestRenderCAResultTable_NestedMetadata(t *testing.T) {
	value := map[string]interface{}{
		"results": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"x": float64(1)},
			},
			"schema": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "x", "type": "INTEGER"},
				},
			},
			"name": "test",
			"metadata": map[string]interface{}{
				"nested_key": "nested_value",
			},
		},
	}

	out := captureStdout(t, func() {
		if err := Render(Text, value); err != nil {
			t.Fatal(err)
		}
	})

	// Nested metadata should NOT show as map[...]
	if strings.Contains(out, "map[") {
		t.Errorf("nested metadata rendered as raw map: %s", out)
	}
	if !strings.Contains(out, "nested_key: nested_value") {
		t.Errorf("missing nested metadata in: %s", out)
	}
}
