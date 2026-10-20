package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func newTestPrinter(format Format) (*printer, *strings.Builder, *strings.Builder) {
	var out, errOut strings.Builder
	return &printer{out: &out, errOut: &errOut, format: format}, &out, &errOut
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    Format
		wantErr bool
	}{
		{input: "table", want: FormatTable},
		{input: "json", want: FormatJSON},
		{input: "csv", want: FormatCSV},
		{input: "JSON", want: FormatJSON},
		{input: " csv ", want: FormatCSV},
		{input: "yaml", wantErr: true},
		{input: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFormat(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseFormat(%q) = %q, want an error", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseFormat(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseFormat(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPrintTable(t *testing.T) {
	p, out, _ := newTestPrinter(FormatTable)
	table := &Table{Headers: []string{"id", "name"}}
	table.AddRow(1, "Ada")
	table.AddRow(2, "Grace")

	if err := p.Print(nil, table); err != nil {
		t.Fatalf("Print: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header and two rows:\n%s", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "ID") {
		t.Errorf("header = %q, want it to start with ID", lines[0])
	}
	if !strings.Contains(lines[1], "Ada") || !strings.Contains(lines[2], "Grace") {
		t.Errorf("rows did not render:\n%s", out.String())
	}
}

func TestPrintTableEmptyGoesToStderr(t *testing.T) {
	p, out, errOut := newTestPrinter(FormatTable)
	if err := p.Print(nil, &Table{Headers: []string{"id"}}); err != nil {
		t.Fatalf("Print: %v", err)
	}
	// Stdout must stay empty so that piping an empty result yields nothing.
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty for no results", out.String())
	}
	if !strings.Contains(errOut.String(), "no results") {
		t.Errorf("stderr = %q, want a note about no results", errOut.String())
	}
}

func TestPrintCSV(t *testing.T) {
	p, out, _ := newTestPrinter(FormatCSV)
	table := &Table{Headers: []string{"id", "note"}}
	table.AddRow(1, `has, comma`)

	if err := p.Print(nil, table); err != nil {
		t.Fatalf("Print: %v", err)
	}
	want := "id,note\n1,\"has, comma\"\n"
	if out.String() != want {
		t.Errorf("csv = %q, want %q", out.String(), want)
	}
}

func TestPrintJSONIgnoresTable(t *testing.T) {
	p, out, _ := newTestPrinter(FormatJSON)
	data := map[string]any{"id": "1", "name": "Ada"}
	table := &Table{Headers: []string{"id"}}
	table.AddRow("1")

	if err := p.Print(data, table); err != nil {
		t.Fatalf("Print: %v", err)
	}

	var back map[string]any
	if err := json.Unmarshal([]byte(out.String()), &back); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	// JSON must carry the whole response, including what the table left out.
	if back["name"] != "Ada" {
		t.Errorf("name = %v, want Ada: JSON should not be limited to table columns", back["name"])
	}
}

func TestPrintFallsBackToJSONWithoutTable(t *testing.T) {
	p, out, _ := newTestPrinter(FormatTable)
	if err := p.Print(map[string]string{"k": "v"}, nil); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !strings.Contains(out.String(), `"k": "v"`) {
		t.Errorf("output = %q, want JSON when no table was supplied", out.String())
	}
}

func TestTableRowsAreKeptOnOneLine(t *testing.T) {
	p, out, _ := newTestPrinter(FormatTable)
	table := &Table{Headers: []string{"note"}}
	table.AddRow("first\nsecond\tthird")

	if err := p.Print(nil, table); err != nil {
		t.Fatalf("Print: %v", err)
	}
	// A newline or tab inside a value would break the column alignment.
	body := strings.TrimRight(strings.TrimPrefix(out.String(), "NOTE\n"), "\n")
	if strings.Contains(body, "\n") || strings.Contains(body, "\t") {
		t.Errorf("row = %q, want newlines and tabs replaced", body)
	}
}

func TestTableEmptyValueBecomesDash(t *testing.T) {
	p, out, _ := newTestPrinter(FormatTable)
	table := &Table{Headers: []string{"a", "b"}}
	table.AddRow("x", nil)

	if err := p.Print(nil, table); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !strings.Contains(out.String(), "-") {
		t.Errorf("output = %q, want an empty value shown as -", out.String())
	}
}

func TestCSVKeepsEmptyValuesEmpty(t *testing.T) {
	// The dash is a reading aid; a CSV consumer wants a genuinely empty cell.
	p, out, _ := newTestPrinter(FormatCSV)
	table := &Table{Headers: []string{"a", "b"}}
	table.AddRow("x", nil)

	if err := p.Print(nil, table); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got, want := out.String(), "a,b\nx,\n"; got != want {
		t.Errorf("csv = %q, want %q", got, want)
	}
}

func TestKeyValueOutput(t *testing.T) {
	p, out, _ := newTestPrinter(FormatTable)
	err := p.keyValue(nil, [][2]string{{"id", "1"}, {"firstName", "Ada"}})
	if err != nil {
		t.Fatalf("keyValue: %v", err)
	}
	if !strings.Contains(out.String(), "firstName") || !strings.Contains(out.String(), "Ada") {
		t.Errorf("output = %q, want the field and its value", out.String())
	}
}

func TestOrderedKeys(t *testing.T) {
	records := []map[string]any{
		{"zebra": 1, "id": 2, "alpha": 3},
		{"id": 4, "beta": 5},
	}
	got := orderedKeys(records, []string{"id", "missing"})

	// Preferred keys come first in the order given; a preferred key absent from
	// every record is skipped rather than shown empty.
	if got[0] != "id" {
		t.Errorf("first key = %q, want id", got[0])
	}
	// The remainder is sorted so repeated runs render the same columns.
	want := []string{"id", "alpha", "beta", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("orderedKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orderedKeys = %v, want %v", got, want)
		}
	}
}

func TestMapTable(t *testing.T) {
	records := []map[string]any{
		{"id": "1", "name": "Ada", "extra": 1234567},
	}
	table := mapTable(records, []string{"id", "name"})

	if len(table.Headers) != 3 || table.Headers[0] != "id" || table.Headers[1] != "name" {
		t.Fatalf("headers = %v, want id and name first", table.Headers)
	}
	if len(table.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(table.Rows))
	}
	// A large number must not pick up an exponent on its way to the table.
	if table.Rows[0][2] != "1234567" {
		t.Errorf("extra = %q, want 1234567", table.Rows[0][2])
	}
}

func TestWithoutEmptyColumns(t *testing.T) {
	tests := []struct {
		name        string
		table       *Table
		wantHeaders []string
		wantRows    [][]string
	}{
		{
			name: "drops a column empty in every row",
			table: &Table{
				Headers: []string{"id", "name", "division"},
				Rows:    [][]string{{"1", "Ada", ""}, {"2", "Grace", ""}},
			},
			wantHeaders: []string{"id", "name"},
			wantRows:    [][]string{{"1", "Ada"}, {"2", "Grace"}},
		},
		{
			name: "keeps a column with any value",
			table: &Table{
				Headers: []string{"id", "note"},
				Rows:    [][]string{{"1", ""}, {"2", "here"}},
			},
			wantHeaders: []string{"id", "note"},
			wantRows:    [][]string{{"1", ""}, {"2", "here"}},
		},
		{
			name: "whitespace counts as empty",
			table: &Table{
				Headers: []string{"id", "blank"},
				Rows:    [][]string{{"1", "  "}},
			},
			wantHeaders: []string{"id"},
			wantRows:    [][]string{{"1"}},
		},
		{
			// Returning nothing would hide the shape of the response.
			name: "an all-empty table is left alone",
			table: &Table{
				Headers: []string{"a", "b"},
				Rows:    [][]string{{"", ""}},
			},
			wantHeaders: []string{"a", "b"},
			wantRows:    [][]string{{"", ""}},
		},
		{
			name: "a short row is padded rather than panicking",
			table: &Table{
				Headers: []string{"a", "b", "c"},
				Rows:    [][]string{{"x", "", "z"}, {"y"}},
			},
			wantHeaders: []string{"a", "c"},
			wantRows:    [][]string{{"x", "z"}, {"y", ""}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withoutEmptyColumns(tt.table)
			if strings.Join(got.Headers, ",") != strings.Join(tt.wantHeaders, ",") {
				t.Fatalf("headers = %v, want %v", got.Headers, tt.wantHeaders)
			}
			if len(got.Rows) != len(tt.wantRows) {
				t.Fatalf("got %d rows, want %d", len(got.Rows), len(tt.wantRows))
			}
			for i := range tt.wantRows {
				if strings.Join(got.Rows[i], ",") != strings.Join(tt.wantRows[i], ",") {
					t.Errorf("row %d = %v, want %v", i, got.Rows[i], tt.wantRows[i])
				}
			}
		})
	}
}

func TestDropEmptyColumnsAppliesToTableNotCSV(t *testing.T) {
	// A column a company does not use is dead width on screen, but a script
	// reading the CSV needs the schema to stay the same between runs.
	build := func() *Table {
		return &Table{
			Headers:          []string{"id", "division"},
			Rows:             [][]string{{"1", ""}},
			DropEmptyColumns: true,
		}
	}

	p, out, _ := newTestPrinter(FormatTable)
	if err := p.Print(nil, build()); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if strings.Contains(out.String(), "DIVISION") {
		t.Errorf("table = %q, want the all-empty column dropped", out.String())
	}

	p, out, _ = newTestPrinter(FormatCSV)
	if err := p.Print(nil, build()); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !strings.Contains(out.String(), "division") {
		t.Errorf("csv = %q, want every column kept for a stable schema", out.String())
	}
}

func TestDropEmptyColumnsIsOptIn(t *testing.T) {
	// keyValue output must keep its value column even when every value is
	// empty, or a record whose fields are all restricted would print as a bare
	// list of field names.
	p, out, _ := newTestPrinter(FormatTable)
	if err := p.keyValue(nil, [][2]string{{"firstName", ""}, {"lastName", ""}}); err != nil {
		t.Fatalf("keyValue: %v", err)
	}
	if !strings.Contains(out.String(), "VALUE") {
		t.Errorf("output = %q, want the value column kept", out.String())
	}
}

func TestOrderedKeysOmit(t *testing.T) {
	records := []map[string]any{{"id": 1, "photoUrl": "https://...", "name": "Ada"}}

	got := orderedKeys(records, []string{"id", "name"}, "photoUrl")
	if strings.Join(got, ",") != "id,name" {
		t.Errorf("orderedKeys = %v, want the omitted key gone", got)
	}

	// An omitted key wins even when it is also named as preferred.
	got = orderedKeys(records, []string{"id", "photoUrl"}, "photoUrl")
	for _, k := range got {
		if k == "photoUrl" {
			t.Errorf("orderedKeys = %v, want omit to beat preferred", got)
		}
	}
}
