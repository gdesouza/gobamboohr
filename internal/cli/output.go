package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// Format is an output encoding.
type Format string

// The output formats every command supports.
const (
	// FormatTable renders columns aligned for reading in a terminal.
	FormatTable Format = "table"
	// FormatJSON renders the API response, so nothing the API returned is
	// lost to the table's chosen columns.
	FormatJSON Format = "json"
	// FormatCSV renders the same columns as the table, quoted for a
	// spreadsheet or a pipeline.
	FormatCSV Format = "csv"
)

// parseFormat validates a --output value.
func parseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatTable:
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatCSV:
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("unknown output format %q: want table, json or csv", s)
	}
}

// Table is a set of rows ready to render. The same Table drives the table and
// CSV formats, so the two never drift apart.
type Table struct {
	Headers []string
	Rows    [][]string
	// DropEmptyColumns removes columns that are empty in every row, but only
	// when rendering a table. It is for listings whose column set comes from
	// the response rather than being chosen: BambooHR returns every field its
	// directory is configured with, and the ones a company does not use are
	// dead width on screen.
	//
	// CSV keeps every column even when it is empty, so that a script reading
	// the output sees the same schema whatever a given page happens to
	// contain.
	DropEmptyColumns bool
}

// AddRow appends a row. Values are rendered through bamboohr.FormatValue, so a
// caller can pass whatever a JSON response decoded into.
func (t *Table) AddRow(values ...any) {
	row := make([]string, 0, len(values))
	for _, v := range values {
		row = append(row, bamboohr.FormatValue(v))
	}
	t.Rows = append(t.Rows, row)
}

// printer renders command results in the format the user asked for.
type printer struct {
	out    io.Writer
	errOut io.Writer
	format Format
}

// Print renders a result. In JSON it writes data verbatim; in the other formats
// it writes the table.
//
// Passing both means a command describes its output once and gets all three
// formats, with JSON keeping the fields the table leaves out.
func (p *printer) Print(data any, table *Table) error {
	if p.format == FormatJSON {
		return p.JSON(data)
	}
	if table == nil {
		return p.JSON(data)
	}
	if p.format == FormatCSV {
		return p.csv(table)
	}
	return p.table(table)
}

// JSON writes v as indented JSON.
func (p *printer) JSON(v any) error {
	enc := json.NewEncoder(p.out)
	enc.SetIndent("", "  ")
	// Field values can contain characters the encoder would otherwise escape
	// into \u form, which is unhelpful to read.
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (p *printer) table(t *Table) error {
	if len(t.Rows) == 0 {
		fmt.Fprintln(p.errOut, "no results")
		return nil
	}
	if t.DropEmptyColumns {
		t = withoutEmptyColumns(t)
	}
	tw := tabwriter.NewWriter(p.out, 0, 8, 2, ' ', 0)
	if len(t.Headers) > 0 {
		fmt.Fprintln(tw, strings.Join(upper(t.Headers), "\t"))
	}
	for _, row := range t.Rows {
		fmt.Fprintln(tw, strings.Join(clean(row), "\t"))
	}
	return tw.Flush()
}

func (p *printer) csv(t *Table) error {
	w := csv.NewWriter(p.out)
	if len(t.Headers) > 0 {
		if err := w.Write(t.Headers); err != nil {
			return err
		}
	}
	for _, row := range t.Rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// keyValue renders a single record as one "field  value" row per field, which
// reads better than a very wide single-row table.
func (p *printer) keyValue(data any, pairs [][2]string) error {
	if p.format == FormatJSON {
		return p.JSON(data)
	}
	t := &Table{Headers: []string{"field", "value"}}
	for _, kv := range pairs {
		t.Rows = append(t.Rows, []string{kv[0], kv[1]})
	}
	if p.format == FormatCSV {
		return p.csv(t)
	}
	return p.table(t)
}

// mapTable renders records whose columns are not known ahead of time, such as
// an employee table row or a report row.
//
// preferred names the columns to show first, in order; the rest follow
// alphabetically so the output is stable between runs.
func mapTable(records []map[string]any, preferred []string) *Table {
	return mapTableOmitting(records, preferred, nil)
}

// mapTableOmitting is mapTable without the named columns.
//
// It is for fields the API returns whose value is not worth a column in a
// listing — a signed photo URL runs to hundreds of characters and pushes every
// useful column off the screen. Omitting a column only affects the table and
// CSV formats; --output json still carries the whole response.
func mapTableOmitting(records []map[string]any, preferred, omit []string) *Table {
	headers := orderedKeys(records, preferred, omit...)
	// These columns come from the response, so a company that does not use a
	// field should not pay for its column.
	t := &Table{Headers: headers, DropEmptyColumns: true}
	for _, rec := range records {
		row := make([]string, 0, len(headers))
		for _, h := range headers {
			row = append(row, bamboohr.FormatValue(rec[h]))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// orderedKeys collects every key across records: the preferred ones first in
// the order given, then the remainder sorted. Keys named in omit are left out
// entirely, even when they appear in preferred.
func orderedKeys(records []map[string]any, preferred []string, omit ...string) []string {
	seen := make(map[string]bool)
	for _, rec := range records {
		for k := range rec {
			seen[k] = true
		}
	}
	for _, k := range omit {
		delete(seen, k)
	}

	headers := make([]string, 0, len(seen))
	for _, k := range preferred {
		if seen[k] {
			headers = append(headers, k)
			delete(seen, k)
		}
	}
	rest := make([]string, 0, len(seen))
	for k := range seen {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(headers, rest...)
}

func upper(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.ToUpper(v)
	}
	return out
}

// clean keeps a tab-aligned row on one line: a newline or tab inside a value
// would break the column alignment.
func clean(row []string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		v = strings.ReplaceAll(v, "\t", " ")
		v = strings.ReplaceAll(v, "\r\n", " ")
		v = strings.ReplaceAll(v, "\n", " ")
		if v == "" {
			v = "-"
		}
		out[i] = v
	}
	return out
}

// withoutEmptyColumns returns t without the columns that are empty in every
// row. A table whose every column is empty is returned unchanged, so that the
// caller still sees the shape of what came back rather than nothing at all.
func withoutEmptyColumns(t *Table) *Table {
	keep := make([]int, 0, len(t.Headers))
	for i := range t.Headers {
		for _, row := range t.Rows {
			if i < len(row) && strings.TrimSpace(row[i]) != "" {
				keep = append(keep, i)
				break
			}
		}
	}
	if len(keep) == len(t.Headers) || len(keep) == 0 {
		return t
	}

	out := &Table{Headers: make([]string, 0, len(keep))}
	for _, i := range keep {
		out.Headers = append(out.Headers, t.Headers[i])
	}
	for _, row := range t.Rows {
		trimmed := make([]string, 0, len(keep))
		for _, i := range keep {
			if i < len(row) {
				trimmed = append(trimmed, row[i])
			} else {
				trimmed = append(trimmed, "")
			}
		}
		out.Rows = append(out.Rows, trimmed)
	}
	return out
}
