package output

import (
	"encoding/json"
	"testing"
)

func TestFilterFields_EmptyFields(t *testing.T) {
	input := map[string]interface{}{"a": 1, "b": 2}
	result := FilterFields(input, "")
	m := result.(map[string]interface{})
	if len(m) != 2 {
		t.Errorf("empty fields should return original, got %d keys", len(m))
	}
}

func TestFilterFields_SingleObject(t *testing.T) {
	input := map[string]interface{}{"name": "test", "schema": "s", "extra": "x"}
	result := FilterFields(input, "name,schema")
	m := toMap(t, result)
	if len(m) != 2 {
		t.Errorf("expected 2 keys, got %d: %v", len(m), m)
	}
	if m["name"] != "test" || m["schema"] != "s" {
		t.Errorf("wrong values: %v", m)
	}
	if _, ok := m["extra"]; ok {
		t.Error("extra should be filtered out")
	}
}

func TestFilterFields_ListEnvelope(t *testing.T) {
	input := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "a", "size": 10, "extra": "x"},
			map[string]interface{}{"name": "b", "size": 20, "extra": "y"},
		},
		"source":          "BigQuery",
		"next_page_token": "tok",
	}

	result := FilterFields(input, "name")
	m := toMap(t, result)

	// Envelope keys preserved.
	if m["source"] != "BigQuery" {
		t.Errorf("source should be preserved, got %v", m["source"])
	}
	if m["next_page_token"] != "tok" {
		t.Errorf("next_page_token should be preserved, got %v", m["next_page_token"])
	}

	// Items filtered.
	items := m["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	item0 := items[0].(map[string]interface{})
	if item0["name"] != "a" {
		t.Errorf("item[0].name = %v", item0["name"])
	}
	if _, ok := item0["extra"]; ok {
		t.Error("item extra should be filtered out")
	}
	if _, ok := item0["size"]; ok {
		t.Error("item size should be filtered out")
	}
}

func TestFilterFields_WhitespaceInFields(t *testing.T) {
	input := map[string]interface{}{"a": 1, "b": 2, "c": 3}
	result := FilterFields(input, " a , c ")
	m := toMap(t, result)
	if len(m) != 2 {
		t.Errorf("expected 2 keys, got %v", m)
	}
}

func TestFilterFields_NoMatchingFields(t *testing.T) {
	input := map[string]interface{}{"a": 1}
	result := FilterFields(input, "x,y")
	m := toMap(t, result)
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestFilterFields_StructInput(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
		Extra string `json:"extra"`
	}
	input := testStruct{Name: "test", Value: 42, Extra: "x"}
	result := FilterFields(input, "name,value")
	m := toMap(t, result)
	if m["name"] != "test" {
		t.Errorf("name = %v", m["name"])
	}
	if _, ok := m["extra"]; ok {
		t.Error("extra should be filtered out")
	}
}

func toMap(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}
