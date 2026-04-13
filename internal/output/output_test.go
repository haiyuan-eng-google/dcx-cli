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
