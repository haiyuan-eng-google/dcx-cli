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

// RenderShaped writes value to stdout with result shaping (compact/count_only/schema_only)
// applied before field filtering. Pipeline: value → shape → filter → render.
func RenderShaped(format Format, value interface{}, resultMode, fields string) error {
	if resultMode != "" && resultMode != "full" {
		// JSON round-trip to normalize the value for compaction.
		// Use json.Decoder with UseNumber to preserve large integers.
		data, err := json.Marshal(value)
		if err == nil {
			shaped := CompactJSON(data, resultMode)
			var parsed interface{}
			dec := json.NewDecoder(strings.NewReader(string(shaped)))
			dec.UseNumber()
			if dec.Decode(&parsed) == nil {
				value = parsed
			}
		}
	}
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
	// Try BQ query result format first.
	if renderBQQueryTable(value) {
		return nil
	}

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
		rows = append(rows, []string{Sanitize(k), Sanitize(fmt.Sprintf("%v", m[k]))})
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
				row[j] = Sanitize(fmt.Sprintf("%v", v))
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
	// Try BQ query result format first.
	if renderBQQueryText(value) {
		return nil
	}

	switch v := value.(type) {
	case string:
		fmt.Fprintln(os.Stdout, Sanitize(v))
	case []byte:
		fmt.Fprintln(os.Stdout, Sanitize(string(v)))
	default:
		// Normalize through JSON to use struct tags and produce
		// readable output instead of raw Go %v representation.
		data, err := json.Marshal(v)
		if err != nil {
			fmt.Fprintln(os.Stdout, Sanitize(fmt.Sprintf("%v", v)))
			return nil
		}

		var normalized interface{}
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		if err := dec.Decode(&normalized); err != nil {
			fmt.Fprintln(os.Stdout, Sanitize(string(data)))
			return nil
		}

		renderTextValue(os.Stdout, normalized, 0)
	}
	return nil
}

// renderTextValue recursively renders a JSON-normalized value as
// human-readable text with indentation.
func renderTextValue(w *os.File, value interface{}, indent int) {
	prefix := strings.Repeat("  ", indent)

	switch v := value.(type) {
	case map[string]interface{}:
		// Check for CA result shape: {data: [...], schema: {fields: [...]}}
		if renderCAResultTable(w, v, prefix) {
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := v[k]
			switch val.(type) {
			case map[string]interface{}, []interface{}:
				fmt.Fprintf(w, "%s%s:\n", prefix, Sanitize(k))
				renderTextValue(w, val, indent+1)
			default:
				fmt.Fprintf(w, "%s%s: %s\n", prefix, Sanitize(k), Sanitize(formatScalar(val)))
			}
		}
	case []interface{}:
		for i, item := range v {
			switch item.(type) {
			case map[string]interface{}:
				if i > 0 {
					fmt.Fprintf(w, "%s---\n", prefix)
				}
				renderTextValue(w, item, indent)
			default:
				fmt.Fprintf(w, "%s- %s\n", prefix, Sanitize(formatScalar(item)))
			}
		}
	default:
		fmt.Fprintf(w, "%s%s\n", prefix, Sanitize(formatScalar(v)))
	}
}

func formatScalar(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
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
	default:
		return fmt.Sprintf("%v", val)
	}
}

// isBQQueryResult checks if value has the BigQuery query response shape:
// {"rows": [...], "schema": {"fields": [...]}, "totalRows": "N"}
// Requires at least one query-response discriminator beyond just schema.fields
// to avoid false-triggering on schema-only objects.
func isBQQueryResult(value interface{}) (headers []string, rows [][]string, totalRows string, ok bool) {
	m, isMap := value.(map[string]interface{})
	if !isMap {
		return nil, nil, "", false
	}

	// Require at least one query-response discriminator to avoid matching
	// arbitrary objects that happen to have schema.fields (e.g., --output-fields=schema).
	_, hasRows := m["rows"]
	_, hasTotalRows := m["totalRows"]
	_, hasJobComplete := m["jobComplete"]
	_, hasJobRef := m["jobReference"]
	_, hasCacheHit := m["cacheHit"]
	if !hasRows && !hasTotalRows && !hasJobComplete && !hasJobRef && !hasCacheHit {
		return nil, nil, "", false
	}

	// Must have schema.fields.
	schemaRaw, hasSchema := m["schema"]
	if !hasSchema {
		return nil, nil, "", false
	}
	schema, isSchemaMap := schemaRaw.(map[string]interface{})
	if !isSchemaMap {
		return nil, nil, "", false
	}
	fieldsRaw, hasFields := schema["fields"]
	if !hasFields {
		return nil, nil, "", false
	}
	fields, isSlice := fieldsRaw.([]interface{})
	if !isSlice || len(fields) == 0 {
		return nil, nil, "", false
	}

	// Extract column names from schema.
	for _, f := range fields {
		fm, ok := f.(map[string]interface{})
		if !ok {
			return nil, nil, "", false
		}
		name, _ := fm["name"].(string)
		if name == "" {
			return nil, nil, "", false
		}
		headers = append(headers, Sanitize(name))
	}

	// Extract totalRows.
	if tr, ok := m["totalRows"]; ok {
		totalRows = fmt.Sprintf("%v", tr)
	}

	// Extract rows (may be absent for 0-row results).
	rowsRaw, hasRows := m["rows"]
	if !hasRows {
		return headers, nil, totalRows, true
	}
	rowSlice, isRowSlice := rowsRaw.([]interface{})
	if !isRowSlice {
		return nil, nil, "", false
	}

	for _, row := range rowSlice {
		rm, ok := row.(map[string]interface{})
		if !ok {
			return nil, nil, "", false
		}
		fArr, ok := rm["f"].([]interface{})
		if !ok {
			return nil, nil, "", false
		}
		cells := make([]string, len(headers))
		for i, cell := range fArr {
			if i >= len(headers) {
				break
			}
			cm, ok := cell.(map[string]interface{})
			if !ok {
				cells[i] = ""
				continue
			}
			v := cm["v"]
			if v == nil {
				cells[i] = "NULL"
			} else {
				cells[i] = escapeCellValue(Sanitize(fmt.Sprintf("%v", v)))
			}
		}
		rows = append(rows, cells)
	}

	return headers, rows, totalRows, true
}

// escapeCellValue replaces newlines and tabs with visible escape sequences
// for tabular display. Keeps raw values unchanged for JSON formats.
func escapeCellValue(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// renderCAResultTable detects the CA result shape {data: [...], schema: {fields: [...]}}
// and renders the data array as an inline table. Returns true if it handled the rendering.
func renderCAResultTable(w *os.File, m map[string]interface{}, prefix string) bool {
	dataRaw, hasData := m["data"]
	schemaRaw, hasSchema := m["schema"]
	if !hasData || !hasSchema {
		return false
	}
	data, dataOk := dataRaw.([]interface{})
	if !dataOk {
		return false
	}
	schema, schemaOk := schemaRaw.(map[string]interface{})
	if !schemaOk {
		return false
	}
	fieldsRaw, hasFields := schema["fields"]
	if !hasFields {
		return false
	}
	fields, fieldsOk := fieldsRaw.([]interface{})
	if !fieldsOk || len(fields) == 0 {
		return false
	}
	// Non-empty data must have map rows.
	if len(data) > 0 {
		if _, ok := data[0].(map[string]interface{}); !ok {
			return false
		}
	}

	// Extract column names from schema.fields.
	var headers []string
	for _, f := range fields {
		fm, ok := f.(map[string]interface{})
		if !ok {
			return false
		}
		name, _ := fm["name"].(string)
		if name == "" {
			return false
		}
		headers = append(headers, Sanitize(name))
	}

	// Handle zero-row case.
	if len(data) == 0 {
		fmt.Fprintf(w, "%s(0 rows)\n", prefix)
		renderCAMetadata(w, m, prefix)
		return true
	}

	// Extract rows.
	var rows [][]string
	for _, item := range data {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		cells := make([]string, len(headers))
		for i, h := range headers {
			v, exists := row[h]
			if !exists || v == nil {
				cells[i] = "NULL"
			} else {
				cells[i] = escapeCellValue(Sanitize(fmt.Sprintf("%v", v)))
			}
		}
		rows = append(rows, cells)
	}

	// Render as aligned text table.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header.
	var line strings.Builder
	for i, h := range headers {
		if i > 0 {
			line.WriteString(" | ")
		}
		line.WriteString(fmt.Sprintf("%-*s", widths[i], h))
	}
	fmt.Fprintf(w, "%s%s\n", prefix, line.String())

	// Separator.
	line.Reset()
	for i, wi := range widths {
		if i > 0 {
			line.WriteString("-+-")
		}
		line.WriteString(strings.Repeat("-", wi))
	}
	fmt.Fprintf(w, "%s%s\n", prefix, line.String())

	// Rows.
	for _, row := range rows {
		line.Reset()
		for i, cell := range row {
			if i > 0 {
				line.WriteString(" | ")
			}
			line.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
		}
		fmt.Fprintf(w, "%s%s\n", prefix, line.String())
	}
	fmt.Fprintf(w, "%s(%d rows)\n", prefix, len(rows))

	renderCAMetadata(w, m, prefix)
	return true
}

// renderCAMetadata renders non-data/schema keys from a CA result map.
func renderCAMetadata(w *os.File, m map[string]interface{}, prefix string) {
	// Collect and sort keys for stable output.
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "data" || k == "schema" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	indent := len(prefix) / 2
	for _, k := range keys {
		v := m[k]
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			fmt.Fprintf(w, "%s%s:\n", prefix, Sanitize(k))
			renderTextValue(w, v, indent+1)
		default:
			fmt.Fprintf(w, "%s%s: %s\n", prefix, Sanitize(k), Sanitize(formatScalar(v)))
		}
	}
}

// renderBQQueryTable renders a BQ query result as a formatted table.
func renderBQQueryTable(value interface{}) bool {
	headers, rows, totalRows, ok := isBQQueryResult(value)
	if !ok {
		return false
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(headers)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)
	table.SetRowLine(false)

	for _, row := range rows {
		table.Append(row)
	}
	table.Render()

	if totalRows != "" {
		fmt.Fprintf(os.Stdout, "(%s rows)\n", totalRows)
	}
	return true
}

// renderBQQueryText renders a BQ query result as aligned text columns.
func renderBQQueryText(value interface{}) bool {
	headers, rows, totalRows, ok := isBQQueryResult(value)
	if !ok {
		return false
	}

	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "(0 rows)")
		return true
	}

	// Calculate column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header.
	var line strings.Builder
	for i, h := range headers {
		if i > 0 {
			line.WriteString(" | ")
		}
		line.WriteString(fmt.Sprintf("%-*s", widths[i], h))
	}
	fmt.Fprintln(os.Stdout, line.String())

	// Print separator.
	line.Reset()
	for i, w := range widths {
		if i > 0 {
			line.WriteString("-+-")
		}
		line.WriteString(strings.Repeat("-", w))
	}
	fmt.Fprintln(os.Stdout, line.String())

	// Print rows.
	for _, row := range rows {
		line.Reset()
		for i, cell := range row {
			if i > 0 {
				line.WriteString(" | ")
			}
			line.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
		}
		fmt.Fprintln(os.Stdout, line.String())
	}

	if totalRows != "" {
		fmt.Fprintf(os.Stdout, "(%s rows)\n", totalRows)
	}
	return true
}
