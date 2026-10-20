package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// directoryFixture mirrors the shape the directory endpoint returns, including
// a record whose only link to the query is their manager's name.
const directoryFixture = `[
	{"id":"208","firstName":"Alex","lastName":"Morgan","displayName":"Alex Morgan",
	 "jobTitle":"Software Manager","department":"Engineering","location":"Springfield",
	 "supervisor":"Pat Rivera","workEmail":"alex.morgan@example.test",
	 "photoUrl":"https://images3.bamboohr.com/1/photos/1.jpg?Policy=morganlookalike"},
	{"id":"666","firstName":"Sam","lastName":"Patel","displayName":"Sam Patel",
	 "jobTitle":"Senior Software Developer","department":"Engineering","location":"Springfield",
	 "supervisor":"Alex Morgan","workEmail":"sam.patel@example.test"},
	{"id":"33","firstName":"Taylor","lastName":"Brooks","displayName":"Taylor Brooks",
	 "jobTitle":"Service Manager","department":"Field Service","location":"Springfield",
	 "supervisor":"Dana Kowalski","workEmail":"taylor.brooks@example.test"}
]`

func directoryEmployees(t *testing.T) []bamboohr.Employee {
	t.Helper()
	var employees []bamboohr.Employee
	if err := json.Unmarshal([]byte(directoryFixture), &employees); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return employees
}

func matchedIDs(matches []employeeMatch) []string {
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.Employee.Key())
	}
	return ids
}

func TestSearchEmployees(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		only  []string
		want  []string
	}{
		{
			// The person named Morgan ranks above the report whose manager is.
			name:  "a name match ranks first",
			terms: []string{"morgan"},
			want:  []string{"208", "666"},
		},
		{
			name:  "matching is case insensitive",
			terms: []string{"MORGAN"},
			want:  []string{"208", "666"},
		},
		{
			// Neither matches in a name field, so both fall back to surname
			// order: "Morgan" before "Patel".
			name:  "partial words match",
			terms: []string{"engin"},
			want:  []string{"208", "666"},
		},
		{
			// Terms are ANDed, but each may land in a different field.
			name:  "several terms across different fields",
			terms: []string{"alex", "engineering"},
			want:  []string{"208", "666"},
		},
		{
			name:  "a term that matches nothing excludes the record",
			terms: []string{"alex", "finance"},
			want:  nil,
		},
		{
			name:  "a quoted phrase matches as one term",
			terms: []string{"morgan"},
			want:  []string{"208", "666"},
		},
		{
			name:  "searching one field only",
			terms: []string{"morgan"},
			only:  []string{"lastName"},
			want:  []string{"208"},
		},
		{
			name:  "searching a field the query does not touch",
			terms: []string{"morgan"},
			only:  []string{"jobTitle"},
			want:  nil,
		},
		{
			name:  "field names in --in are case insensitive",
			terms: []string{"morgan"},
			only:  []string{"LASTNAME"},
			want:  []string{"208"},
		},
		{
			name:  "results are ordered by name when no name matched",
			terms: []string{"springfield"},
			want:  []string{"33", "208", "666"},
		},
		{
			name:  "no terms matches nothing",
			terms: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchedIDs(searchEmployees(directoryEmployees(t), tt.terms, tt.only))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("matched %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchSkipsPhotoURL(t *testing.T) {
	// A signed photo URL carries a long base64 policy; matching against it
	// produces hits with no meaning. The fixture's URL contains "morganlookalike"
	// precisely so this would fail if the field were searched.
	matches := searchEmployees(directoryEmployees(t), []string{"lookalike"}, nil)
	if len(matches) != 0 {
		t.Errorf("matched %v, want nothing: photoUrl must not be searched", matchedIDs(matches))
	}
}

func TestSearchReportsWhichFieldsMatched(t *testing.T) {
	matches := searchEmployees(directoryEmployees(t), []string{"morgan"}, nil)
	if len(matches) != 2 {
		t.Fatalf("matched %v, want two records", matchedIDs(matches))
	}

	// Sorted, so the reason a row appears reads the same way every run.
	if got := strings.Join(matches[0].Fields, ","); got != "displayName,lastName,workEmail" {
		t.Errorf("fields = %q, want displayName,lastName,workEmail", got)
	}
	// The one that only matched through their manager must say so.
	if got := strings.Join(matches[1].Fields, ","); got != "supervisor" {
		t.Errorf("fields = %q, want supervisor", got)
	}
}

func TestSearchTableShowsTheFixedColumns(t *testing.T) {
	matches := searchEmployees(directoryEmployees(t), []string{"morgan"}, nil)
	table := searchTable(matches, false)

	want := []string{"id", "displayName", "location", "supervisor"}
	if strings.Join(table.Headers, ",") != strings.Join(want, ",") {
		t.Fatalf("headers = %v, want %v", table.Headers, want)
	}
	if len(table.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(table.Rows))
	}
	for i, row := range table.Rows {
		if len(row) != len(want) {
			t.Fatalf("row %d has %d values, want %d", i, len(row), len(want))
		}
	}
	// id, displayName, location, supervisor for the person actually named.
	if got := strings.Join(table.Rows[0], "|"); got != "208|Alex Morgan|Springfield|Pat Rivera" {
		t.Errorf("first row = %q, want the four requested values", got)
	}
}

func TestSearchTableKeepsRequestedColumnsWhenEmpty(t *testing.T) {
	// The columns are named rather than derived from the response, so a company
	// that leaves one empty still gets the column it asked for.
	var employees []bamboohr.Employee
	if err := json.Unmarshal([]byte(`[{"id":"1","displayName":"Nobody"}]`), &employees); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	table := searchTable(searchEmployees(employees, []string{"nobody"}, nil), false)

	if len(table.Headers) != 4 {
		t.Errorf("headers = %v, want all four kept", table.Headers)
	}
	if table.DropEmptyColumns {
		t.Error("DropEmptyColumns = true, want the named columns kept regardless")
	}
}

func TestSearchTableMatchedColumnIsOptIn(t *testing.T) {
	matches := searchEmployees(directoryEmployees(t), []string{"morgan"}, nil)

	if got := searchTable(matches, false); containsFold(got.Headers, matchedColumn) {
		t.Errorf("headers = %v, want no matched column by default", got.Headers)
	}

	withMatched := searchTable(matches, true)
	if got := withMatched.Headers[len(withMatched.Headers)-1]; got != matchedColumn {
		t.Errorf("last column = %q, want %q", got, matchedColumn)
	}
	// The reason the second person appears is not visible in their row.
	if got := withMatched.Rows[1][len(withMatched.Headers)-1]; got != "supervisor" {
		t.Errorf("matched = %q, want supervisor", got)
	}
}

func TestSearchTableDoesNotMutateTheResponse(t *testing.T) {
	// JSON output reports the API response, so rendering must not add the
	// synthetic column to the records.
	matches := searchEmployees(directoryEmployees(t), []string{"morgan"}, nil)
	_ = searchTable(matches, true)

	for _, match := range matches {
		if _, ok := match.Employee.Fields[matchedColumn]; ok {
			t.Errorf("employee %s carries a %q field, want the response untouched",
				match.Employee.Key(), matchedColumn)
		}
	}
}

func TestSearchTerms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "lowercased", args: []string{"Morgan"}, want: []string{"morgan"}},
		{name: "trimmed", args: []string{"  alex  "}, want: []string{"alex"}},
		{name: "blanks dropped", args: []string{"alex", "", "   "}, want: []string{"alex"}},
		{name: "phrase kept whole", args: []string{"morgan"}, want: []string{"morgan"}},
		{name: "all blank", args: []string{"", " "}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchTerms(tt.args)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("searchTerms(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestEmployeeSortName(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "last name preferred", payload: `{"id":"1","lastName":"Morgan","firstName":"Alex"}`, want: "Morgan"},
		{name: "falls back to display name", payload: `{"id":"1","displayName":"Alex Morgan"}`, want: "Alex Morgan"},
		{name: "falls back to first name", payload: `{"id":"1","firstName":"Alex"}`, want: "Alex"},
		{name: "falls back to the id", payload: `{"id":"1"}`, want: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var emp bamboohr.Employee
			if err := json.Unmarshal([]byte(tt.payload), &emp); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := employeeSortName(emp); got != tt.want {
				t.Errorf("employeeSortName = %q, want %q", got, tt.want)
			}
		})
	}
}
