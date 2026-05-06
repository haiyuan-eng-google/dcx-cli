package jsonpath

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseJSON(s string) interface{} {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	dec.Decode(&v)
	return v
}

func TestResolve_FieldAccess(t *testing.T) {
	data := parseJSON(`{"name":"alice","age":30}`)
	got, err := Resolve(data, ".name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}
}

func TestResolve_ArrayIndex(t *testing.T) {
	data := parseJSON(`[{"id":"a"},{"id":"b"}]`)
	got, err := Resolve(data, "[1].id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("got %q, want %q", got, "b")
	}
}

func TestResolve_NestedPath(t *testing.T) {
	data := parseJSON(`{"items":[{"_resource_id":"analytics"},{"_resource_id":"logs"}]}`)
	got, err := Resolve(data, ".items[0]._resource_id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "analytics" {
		t.Errorf("got %q, want %q", got, "analytics")
	}
}

func TestResolve_EmptyPath(t *testing.T) {
	data := parseJSON(`{"x":1}`)
	got, err := Resolve(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("empty path should return formatted value")
	}
}

func TestResolve_FieldNotFound(t *testing.T) {
	data := parseJSON(`{"name":"alice"}`)
	_, err := Resolve(data, ".nonexistent")
	if err == nil {
		t.Error("expected error for missing field")
	}
}

func TestResolve_IndexOutOfRange(t *testing.T) {
	data := parseJSON(`[1,2,3]`)
	_, err := Resolve(data, "[99]")
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestResolve_InvalidIndex(t *testing.T) {
	data := parseJSON(`[1,2]`)
	tests := []string{"[abc]", "[-1]", "[+0]", "[0:1]"}
	for _, path := range tests {
		_, err := Resolve(data, path)
		if err == nil {
			t.Errorf("expected error for index %q", path)
		}
	}
}

func TestResolve_TrailingJunk(t *testing.T) {
	data := parseJSON(`{"items":[{"name":"x"}]}`)
	tests := []string{
		".items[0]garbage",
		".items[0].name[0]tail",
		".itemsXYZ",
	}
	for _, path := range tests {
		_, err := Resolve(data, path)
		if err == nil {
			t.Errorf("expected error for trailing junk in path %q", path)
		}
	}
}

func TestResolve_AccessFieldOnNonObject(t *testing.T) {
	data := parseJSON(`"just a string"`)
	_, err := Resolve(data, ".field")
	if err == nil {
		t.Error("expected error for field access on non-object")
	}
}

func TestResolve_IndexNonArray(t *testing.T) {
	data := parseJSON(`{"x":1}`)
	_, err := Resolve(data, "[0]")
	if err == nil {
		t.Error("expected error for index on non-array")
	}
}

func TestResolve_UnclosedBracket(t *testing.T) {
	data := parseJSON(`[1,2]`)
	_, err := Resolve(data, "[0")
	if err == nil {
		t.Error("expected error for unclosed bracket")
	}
}

func TestFormatValue_Types(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"hello", "hello"},
		{json.Number("42"), "42"},
		{float64(3.14), "3.14"},
		{float64(100), "100"},
		{true, "true"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := FormatValue(tt.input)
		if got != tt.want {
			t.Errorf("FormatValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
