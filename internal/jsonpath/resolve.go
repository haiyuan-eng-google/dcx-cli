// Package jsonpath provides a small JSON path resolver for dcx.
//
// Supports field access (.field) and array indexing ([N]).
// Used by REPL $last references and MCP batch $prev references.
package jsonpath

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Resolve evaluates a path expression against JSON data.
// Path syntax: .field, [N], chained (e.g., ".items[0]._resource_id").
// Returns the resolved value as a string.
func Resolve(data interface{}, path string) (string, error) {
	value := data

	for len(path) > 0 {
		if path[0] == '.' {
			path = path[1:]
			end := strings.IndexAny(path, ".[")
			if end < 0 {
				end = len(path)
			}
			fieldName := path[:end]
			path = path[end:]

			m, ok := value.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("cannot access .%s on non-object", fieldName)
			}
			value, ok = m[fieldName]
			if !ok {
				return "", fmt.Errorf("field %q not found", fieldName)
			}
		} else if path[0] == '[' {
			end := strings.Index(path, "]")
			if end < 0 {
				return "", fmt.Errorf("unclosed bracket in path")
			}
			indexStr := path[1:end]
			path = path[end+1:]

			arr, ok := value.([]interface{})
			if !ok {
				return "", fmt.Errorf("cannot index non-array")
			}
			if !isDigitsOnly(indexStr) {
				return "", fmt.Errorf("invalid index [%s]", indexStr)
			}
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return "", fmt.Errorf("invalid index [%s]", indexStr)
			}
			if index >= len(arr) {
				return "", fmt.Errorf("index [%d] out of range (length %d)", index, len(arr))
			}
			value = arr[index]
		} else {
			break
		}
	}

	return FormatValue(value), nil
}

// FormatValue converts a JSON value to a string.
func FormatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return ""
	default:
		data, _ := json.Marshal(val)
		return string(data)
	}
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
