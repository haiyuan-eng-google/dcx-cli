// Compact provides deterministic result shaping for dcx output.
//
// Shared by CLI (--result-mode / --compact) and MCP (result_mode parameter).
// All modes are deterministic — no LLM summarization.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ValidResultModes are the accepted result_mode values.
var ValidResultModes = map[string]bool{
	"full": true, "compact": true, "count_only": true, "schema_only": true,
}

// ResultModeNames returns sorted valid mode names.
func ResultModeNames() []string {
	return []string{"full", "compact", "count_only", "schema_only"}
}

// CompactJSON parses a JSON byte slice and applies result shaping.
// Uses json.Decoder.UseNumber() to preserve large integers.
// Enforces EOF — trailing content causes raw fallback.
// Returns the original data unchanged if mode is "full" or parsing fails.
func CompactJSON(data []byte, mode string) []byte {
	if mode == "full" || mode == "" {
		return data
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return data // not JSON
	}

	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()

	var parsed interface{}
	if dec.Decode(&parsed) != nil {
		return data // parse failed — return raw
	}

	// Enforce EOF — reject trailing content.
	var extra json.RawMessage
	if dec.Decode(&extra) != io.EOF {
		return data // trailing content — return raw
	}

	compacted := CompactResult(parsed, mode)
	result, err := json.Marshal(compacted)
	if err != nil {
		return data
	}
	return result
}

// CompactResult reshapes a parsed JSON value based on the mode.
func CompactResult(data interface{}, mode string) interface{} {
	// Handle bare arrays (e.g., meta commands output).
	if arr, ok := data.([]interface{}); ok {
		return compactArray(arr, mode)
	}

	m, isMap := data.(map[string]interface{})

	switch mode {
	case "compact":
		return compactMode(data, m, isMap)
	case "count_only":
		return countOnlyMode(data, m, isMap)
	case "schema_only":
		return schemaOnlyMode(data, m, isMap)
	default:
		return data
	}
}

// compactArray handles bare array compaction.
func compactArray(arr []interface{}, mode string) interface{} {
	switch mode {
	case "compact":
		result := map[string]interface{}{
			"count": len(arr),
		}
		sampleSize := 3
		if len(arr) < sampleSize {
			sampleSize = len(arr)
		}
		if sampleSize > 0 {
			result["sample"] = arr[:sampleSize]
		}
		if len(arr) > 0 {
			if first, ok := arr[0].(map[string]interface{}); ok {
				fields := make([]string, 0, len(first))
				for k := range first {
					fields = append(fields, k)
				}
				sort.Strings(fields)
				result["fields"] = fields
			}
		}
		return result
	case "count_only":
		return map[string]interface{}{"count": len(arr)}
	case "schema_only":
		if len(arr) > 0 {
			if first, ok := arr[0].(map[string]interface{}); ok {
				fields := make([]map[string]string, 0, len(first))
				keys := make([]string, 0, len(first))
				for k := range first {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fields = append(fields, map[string]string{
						"name": k,
						"type": JSONTypeOf(first[k]),
					})
				}
				return map[string]interface{}{
					"item_count": len(arr),
					"fields":     fields,
				}
			}
		}
		return map[string]interface{}{
			"item_count": len(arr),
			"fields":     []interface{}{},
		}
	default:
		return arr
	}
}

// compactMode returns count + sample (first 3) + field names for list envelopes,
// or top-level keys for single objects.
func compactMode(data interface{}, m map[string]interface{}, isMap bool) interface{} {
	if !isMap {
		return data
	}

	if itemsRaw, ok := m["items"]; ok {
		if items, ok := itemsRaw.([]interface{}); ok {
			result := map[string]interface{}{
				"count": len(items),
			}

			sampleSize := 3
			if len(items) < sampleSize {
				sampleSize = len(items)
			}
			if sampleSize > 0 {
				result["sample"] = items[:sampleSize]
			}

			if len(items) > 0 {
				if first, ok := items[0].(map[string]interface{}); ok {
					fields := make([]string, 0, len(first))
					for k := range first {
						fields = append(fields, k)
					}
					sort.Strings(fields)
					result["fields"] = fields
				}
			}

			for k, v := range m {
				if k == "items" {
					continue
				}
				result[k] = v
			}
			return result
		}
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return map[string]interface{}{
		"type": "object",
		"keys": keys,
	}
}

// countOnlyMode returns just the item count for list envelopes.
func countOnlyMode(data interface{}, m map[string]interface{}, isMap bool) interface{} {
	if !isMap {
		return map[string]interface{}{"count": 1}
	}
	if itemsRaw, ok := m["items"]; ok {
		if items, ok := itemsRaw.([]interface{}); ok {
			result := map[string]interface{}{"count": len(items)}
			if v, ok := m["source"]; ok {
				result["source"] = v
			}
			if v, ok := m["next_page_token"]; ok {
				result["next_page_token"] = v
			}
			return result
		}
	}
	return map[string]interface{}{"count": 1}
}

// schemaOnlyMode returns field names and types from the first item.
func schemaOnlyMode(data interface{}, m map[string]interface{}, isMap bool) interface{} {
	if !isMap {
		return map[string]interface{}{"type": fmt.Sprintf("%T", data)}
	}

	if itemsRaw, ok := m["items"]; ok {
		if items, ok := itemsRaw.([]interface{}); ok {
			if len(items) > 0 {
				if first, ok := items[0].(map[string]interface{}); ok {
					fields := make([]map[string]string, 0, len(first))
					keys := make([]string, 0, len(first))
					for k := range first {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						v := first[k]
						fields = append(fields, map[string]string{
							"name": k,
							"type": JSONTypeOf(v),
						})
					}
					return map[string]interface{}{
						"item_count": len(items),
						"fields":     fields,
					}
				}
			}
			return map[string]interface{}{
				"item_count": 0,
				"fields":     []interface{}{},
			}
		}
	}

	fields := make([]map[string]string, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, map[string]string{
			"name": k,
			"type": JSONTypeOf(m[k]),
		})
	}
	return map[string]interface{}{
		"type":   "object",
		"fields": fields,
	}
}

// JSONTypeOf returns the JSON type name for a value.
func JSONTypeOf(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case json.Number:
		return "number"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return "unknown"
	}
}
