// Package output provides centralized rendering for all dcx command output.
//
// Every command renders through Render(format, value). This guarantees
// consistent stdout formatting across JSON, JSON-minified, table, and text
// output modes.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/olekukonko/tablewriter"
)

// Format controls how output is rendered to stdout.
type Format string

const (
	JSON         Format = "json"
	JSONMinified Format = "json-minified"
	Table        Format = "table"
	Text         Format = "text"
)

// ValidFormats is the list of accepted format values.
var ValidFormats = []Format{JSON, JSONMinified, Table, Text}

// ParseFormat parses a string into a Format, returning an error for unknown values.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case JSON, JSONMinified, Table, Text:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown format %q; valid values: %s", s, strings.Join(FormatNames(), ", "))
	}
}

// FormatNames returns the valid format names as strings.
func FormatNames() []string {
	names := make([]string, len(ValidFormats))
	for i, f := range ValidFormats {
		names[i] = string(f)
	}
	return names
}

// Render writes value to stdout in the specified format.
//
// For JSON and JSONMinified, value is marshaled directly.
// For Table, value should be a slice of maps or structs (rendered as rows).
// For Text, value is printed via fmt.Println.
func Render(format Format, value interface{}) error {
	return RenderFiltered(format, value, "")
}

// RenderFiltered writes value to stdout, filtering to only include the
// specified fields. If fields is empty, renders the full value.
func RenderFiltered(format Format, value interface{}, fields string) error {
	if fields != "" {
		value = FilterFields(value, fields)
	}
	return renderUnfiltered(format, value)
}

func renderUnfiltered(format Format, value interface{}) error {
	switch format {
	case JSON:
		return renderJSON(value, true)
	case JSONMinified:
		return renderJSON(value, false)
	case Table:
		return renderTable(value)
	case Text:
		return renderText(value)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func renderJSON(value interface{}, pretty bool) error {
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

func renderTable(value interface{}) error {
	rows, headers, err := extractTableData(value)
	if err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(headers)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(false)
	table.SetColumnSeparator("")
	table.SetHeaderLine(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("  ")

	for _, row := range rows {
		table.Append(row)
	}
	table.Render()
	return nil
}

// extractTableData converts a value into table headers and rows.
// Accepts:
//   - []map[string]interface{} — keys become headers
//   - map with "items" key containing a slice — items extracted
//   - single map — rendered as key/value pairs
func extractTableData(value interface{}) ([][]string, []string, error) {
	// Try to extract "items" from an envelope.
	if m, ok := value.(map[string]interface{}); ok {
		if items, ok := m["items"]; ok {
			return extractTableData(items)
		}
		// Single map: render as key-value pairs.
		return mapToTable(m)
	}

	// Handle slice types.
	rv := reflect.ValueOf(value)
	// For struct types (like ListEnvelope), JSON round-trip to map and retry.
	if rv.Kind() == reflect.Struct || (rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Struct) {
		data, err := json.Marshal(value)
		if err == nil {
			var m map[string]interface{}
			if json.Unmarshal(data, &m) == nil {
				return extractTableData(m)
			}
		}
	}
	if rv.Kind() == reflect.Slice {
		if rv.Len() == 0 {
			return nil, nil, nil
		}
		return sliceToTable(rv)
	}

	// Fallback: single value as one-cell table.
	return [][]string{{fmt.Sprintf("%v", value)}}, []string{"VALUE"}, nil
}

func mapToTable(m map[string]interface{}) ([][]string, []string, error) {
	headers := []string{"KEY", "VALUE"}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	rows := make([][]string, 0, len(m))
	for _, k := range keys {
		rows = append(rows, []string{k, fmt.Sprintf("%v", m[k])})
	}
	return rows, headers, nil
}

func sliceToTable(rv reflect.Value) ([][]string, []string, error) {
	// Collect all unique keys from all items for consistent columns.
	headerSet := make(map[string]bool)
	var items []map[string]interface{}

	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		m, ok := toStringMap(elem)
		if !ok {
			// Non-map slice: render each element as a single-column row.
			rows := make([][]string, rv.Len())
			for j := 0; j < rv.Len(); j++ {
				rows[j] = []string{fmt.Sprintf("%v", rv.Index(j).Interface())}
			}
			return rows, []string{"VALUE"}, nil
		}
		items = append(items, m)
		for k := range m {
			headerSet[k] = true
		}
	}

	headers := make([]string, 0, len(headerSet))
	for k := range headerSet {
		headers = append(headers, k)
	}
	sort.Strings(headers)

	rows := make([][]string, len(items))
	for i, item := range items {
		row := make([]string, len(headers))
		for j, h := range headers {
			if v, ok := item[h]; ok {
				row[j] = fmt.Sprintf("%v", v)
			}
		}
		rows[i] = row
	}
	return rows, headers, nil
}

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	default:
		// Try JSON round-trip for struct types.
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, false
		}
		return result, true
	}
}

func renderText(value interface{}) error {
	switch v := value.(type) {
	case string:
		fmt.Fprintln(os.Stdout, v)
	case []byte:
		fmt.Fprintln(os.Stdout, string(v))
	default:
		fmt.Fprintln(os.Stdout, fmt.Sprintf("%v", v))
	}
	return nil
}
