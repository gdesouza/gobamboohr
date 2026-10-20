package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// employeeSearchNameFields rank a match higher than one found anywhere else,
// so that searching a person's name does not bury them under everyone whose
// job title happens to contain the same word.
var employeeSearchNameFields = []string{"displayName", "firstName", "lastName", "preferredName"}

// employeeSearchSkipFields are never matched against.
//
// A signed photo URL contains a long base64 policy that produces meaningless
// hits, and the internal ID is short enough that any numeric query would match
// most of the company; "employee get <id>" is the way to look one up by ID.
var employeeSearchSkipFields = []string{"photoUrl", "photoUploaded", "canUploadPhoto", "id"}

func newEmployeeSearchCommand(a *app) *cobra.Command {
	var (
		in          []string
		limit       int
		showMatched bool
	)

	cmd := &cobra.Command{
		Use:     "search <term>...",
		Aliases: []string{"find"},
		Short:   "Find employees by name, title, department, email or phone",
		Long: `Find employees by matching a term against every field in the company
directory.

Matching is a case-insensitive substring, so partial words work. Several terms
must all match, though not in the same field, which is what makes
"search alex engineering" useful. Quote a term to match it as a phrase.

Every field is searched but only ID, display name, location and supervisor are
shown. Pass --matched to see which field produced each hit, or --output json for
the whole record.

This searches the published employee directory, which is one request and comes
back fully readable. It therefore covers current employees who appear in the
directory: former employees are not included, and neither is any field the
directory is not configured to publish. For those, filter the full employee
list instead:

  bamboohr employee list --status all --filter lastName=morgan

That is a server-side filter, so it is per-field and cannot match across
several fields at once.`,
		Example: `  bamboohr employee search morgan
  bamboohr employee search alex engineering
  bamboohr employee search "morgan"
  bamboohr employee search springfield --in location
  bamboohr employee search morgan --matched
  bamboohr employee search manager --output csv`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			terms := searchTerms(args)
			if len(terms) == 0 {
				return fmt.Errorf("a search term is required")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			a.logf("searching the directory for %s", strings.Join(quoteAll(terms), " and "))
			dir, err := client.GetDirectory(ctx, nil)
			if err != nil {
				return err
			}

			only := splitList(in)
			a.warnUnknownSearchFields(dir.Employees, only)

			matches := searchEmployees(dir.Employees, terms, only)
			a.logf("matched %d of %d directory records", len(matches), len(dir.Employees))
			if limit > 0 && len(matches) > limit {
				a.logf("showing the first %d", limit)
				matches = matches[:limit]
			}
			return a.printer().Print(searchedEmployees(matches), searchTable(matches, showMatched))
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&in, "in", nil, "only match these fields (default: every field in the directory)")
	f.IntVar(&limit, "limit", 0, "show at most this many matches")
	f.BoolVar(&showMatched, "matched", false, "add a column naming the fields each row matched on")
	return cmd
}

// searchTerms normalises the query arguments, dropping blanks so that a
// stray quoted empty string does not match everything.
func searchTerms(args []string) []string {
	terms := make([]string, 0, len(args))
	for _, arg := range args {
		if term := strings.ToLower(strings.TrimSpace(arg)); term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

// employeeMatch is one search hit and the fields that produced it.
type employeeMatch struct {
	Employee bamboohr.Employee
	// Fields names the fields a term was found in, sorted. Searching every
	// field means a record can match for a reason that is not visible in the
	// row, so the reason is carried alongside it.
	Fields []string
	// nameMatch is true when a term matched one of the name fields.
	nameMatch bool
	sortKey   string
}

// searchEmployees returns the records matching every term, best matches first.
//
// A term may match any searched field, but all terms must match somewhere in
// the record. Results are ordered by whether a term matched the person's name
// and then by name, so that looking someone up puts them at the top and
// repeated runs list results the same way.
func searchEmployees(employees []bamboohr.Employee, terms, only []string) []employeeMatch {
	// No query matches nothing rather than everything: a caller that reached
	// here with an empty term list asked for no filter, not for the whole
	// directory.
	if len(terms) == 0 {
		return nil
	}
	// Lowercase here as well as in searchTerms, so the comparison against the
	// lowercased haystack does not depend on the caller having done it.
	needles := make([]string, 0, len(terms))
	for _, term := range terms {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" {
			needles = append(needles, term)
		}
	}
	if len(needles) == 0 {
		return nil
	}

	var results []employeeMatch
	for _, employee := range employees {
		haystack := searchableFields(employee, only)
		if len(haystack) == 0 {
			continue
		}

		matchedAll := true
		matched := make(map[string]bool)
		nameMatch := false
		for _, term := range needles {
			found := false
			for field, value := range haystack {
				if !strings.Contains(value, term) {
					continue
				}
				found = true
				matched[field] = true
				if containsFold(employeeSearchNameFields, field) {
					nameMatch = true
				}
			}
			if !found {
				matchedAll = false
				break
			}
		}
		if !matchedAll {
			continue
		}
		results = append(results, employeeMatch{
			Employee:  employee,
			Fields:    sortedKeys(matched),
			nameMatch: nameMatch,
			sortKey:   strings.ToLower(employeeSortName(employee)),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].nameMatch != results[j].nameMatch {
			return results[i].nameMatch
		}
		return results[i].sortKey < results[j].sortKey
	})
	return results
}

// matchedColumn is the optional column naming the fields a row matched on.
const matchedColumn = "matched"

// searchColumns are the columns a search result shows, in order.
//
// Unlike the other listings, whose columns come from whatever the response
// carried, these are fixed: a search is for finding a person, and the four
// facts that identify one are who they are, where they are and who they report
// to. The whole record is still available through --output json.
var searchColumns = []string{"id", "displayName", "location", "supervisor"}

// searchTable renders search hits.
//
// showMatched appends the column naming why each row matched, which is worth
// asking for when a query turns up someone for a reason the row does not show.
func searchTable(matches []employeeMatch, showMatched bool) *Table {
	headers := append([]string{}, searchColumns...)
	if showMatched {
		headers = append(headers, matchedColumn)
	}

	// The columns are named rather than derived, so they are all shown even
	// when a company leaves one of them empty.
	t := &Table{Headers: headers}
	for _, match := range matches {
		values := make([]any, 0, len(headers))
		for _, column := range searchColumns {
			values = append(values, match.Employee.Fields[column])
		}
		if showMatched {
			values = append(values, strings.Join(match.Fields, ","))
		}
		t.AddRow(values...)
	}
	return t
}

// searchedEmployees pulls the plain records out of the hits, for the JSON
// output that reports the API response rather than the table's columns.
func searchedEmployees(matches []employeeMatch) []bamboohr.Employee {
	employees := make([]bamboohr.Employee, 0, len(matches))
	for _, match := range matches {
		employees = append(employees, match.Employee)
	}
	return employees
}

// searchableFields returns the record's values to match against, lowercased and
// keyed by field name.
//
// only restricts the search to the named fields; an empty only searches
// everything the directory returned, so a company's custom directory fields are
// covered without naming them here.
func searchableFields(employee bamboohr.Employee, only []string) map[string]string {
	haystack := make(map[string]string, len(employee.Fields))
	for field, value := range employee.Fields {
		if len(only) > 0 {
			if !containsFold(only, field) {
				continue
			}
		} else if containsFold(employeeSearchSkipFields, field) {
			continue
		}
		if text := bamboohr.FormatValue(value); text != "" {
			haystack[field] = strings.ToLower(text)
		}
	}
	return haystack
}

// employeeSortName picks the best available name to order results by.
func employeeSortName(employee bamboohr.Employee) string {
	for _, candidate := range []string{
		employee.Field("lastName"),
		employee.Field("displayName"),
		employee.Field("firstName"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return employee.Key()
}

// warnUnknownSearchFields reports --in names that no record has, which would
// otherwise just return nothing and look like the search was broken.
func (a *app) warnUnknownSearchFields(employees []bamboohr.Employee, only []string) {
	if len(only) == 0 || len(employees) == 0 {
		return
	}
	present := make(map[string]bool)
	for _, employee := range employees {
		for field := range employee.Fields {
			present[strings.ToLower(field)] = true
		}
	}

	var unknown []string
	for _, field := range only {
		if !present[strings.ToLower(field)] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		fmt.Fprintf(a.errOut,
			"warning: the directory has no field named %s; run \"bamboohr employee directory --output json\" to see which it publishes\n",
			strings.Join(quoteAll(unknown), ", "))
	}
}

// containsFold reports whether values holds s, ignoring case.
func containsFold(values []string, s string) bool {
	for _, v := range values {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func quoteAll(values []string) []string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	return quoted
}
