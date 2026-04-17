package output

import (
	"encoding/json"
	"strings"
)

// FilterFields filters a value to include only the specified fields.
// Fields is a comma-separated list of top-level keys (e.g., "name,schema").
//
// For single objects: keeps only matching top-level keys.
// For list envelopes (objects with "items" array): filters each item
// in the array and preserves envelope keys like "source" and "next_page_token".
// Returns the original value unchanged if fields is empty.
func FilterFields(value interface{}, fields string) interface{} {
	if fields == "" {
		return value
	}

	fieldSet := parseFieldSet(fields)

	// JSON round-trip to get a generic map.
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return value
	}

	return filterValue(raw, fieldSet)
}

func parseFieldSet(fields string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			set[f] = true
		}
	}
	return set
}

func filterValue(raw interface{}, fields map[string]bool) interface{} {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return raw
	}

	// List envelope: filter each item, preserve envelope keys.
	if items, ok := m["items"]; ok {
		if arr, ok := items.([]interface{}); ok {
			return filterListEnvelope(m, arr, fields)
		}
	}

	// Single object: keep only matching keys.
	return filterMap(m, fields)
}

func filterListEnvelope(envelope map[string]interface{}, items []interface{}, fields map[string]bool) map[string]interface{} {
	// Envelope keys to always preserve.
	envelopeKeys := map[string]bool{
		"source": true, "next_page_token": true, "items": true,
	}

	result := make(map[string]interface{})
	for k, v := range envelope {
		if envelopeKeys[k] {
			result[k] = v
		}
	}

	// Filter each item.
	filtered := make([]interface{}, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			filtered = append(filtered, filterMap(m, fields))
		} else {
			filtered = append(filtered, item)
		}
	}
	result["items"] = filtered

	return result
}

func filterMap(m map[string]interface{}, fields map[string]bool) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		if fields[k] {
			result[k] = v
		}
	}
	return result
}
