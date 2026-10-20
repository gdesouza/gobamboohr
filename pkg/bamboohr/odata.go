package bamboohr

import (
	"fmt"
	"strconv"
	"strings"
)

// Filter builds the OData expressions the newer BambooHR list endpoints accept
// in their filter query parameter.
//
// Only the operators BambooHR implements are offered: eq, ne, lt, le, gt, ge
// and in, combined with and. There is no or, no not and no grouping, so a
// Filter is a flat conjunction of clauses.
//
//	f := bamboohr.NewFilter().
//		Eq("status", "APPROVED").
//		Ge("startDate", "2026-04-01").
//		In("employeeId", 614, 615)
//	// status eq 'APPROVED' and startDate ge '2026-04-01' and employeeId in (614, 615)
type Filter struct {
	clauses []string
}

// NewFilter returns an empty filter.
func NewFilter() *Filter { return &Filter{} }

// Eq adds "field eq value". A nil value produces "field eq null", which matches
// records where the field is unset.
func (f *Filter) Eq(field string, value any) *Filter { return f.cmp(field, "eq", value) }

// Ne adds "field ne value". A nil value produces "field ne null", which matches
// records where the field is set.
func (f *Filter) Ne(field string, value any) *Filter { return f.cmp(field, "ne", value) }

// Lt adds "field lt value".
func (f *Filter) Lt(field string, value any) *Filter { return f.cmp(field, "lt", value) }

// Le adds "field le value".
func (f *Filter) Le(field string, value any) *Filter { return f.cmp(field, "le", value) }

// Gt adds "field gt value".
func (f *Filter) Gt(field string, value any) *Filter { return f.cmp(field, "gt", value) }

// Ge adds "field ge value".
func (f *Filter) Ge(field string, value any) *Filter { return f.cmp(field, "ge", value) }

// In adds "field in (a, b, c)". It is a no-op when no values are given, so that
// an empty selection does not silently become a filter that matches nothing.
func (f *Filter) In(field string, values ...any) *Filter {
	if field == "" || len(values) == 0 {
		return f
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, literal(v))
	}
	f.clauses = append(f.clauses, fmt.Sprintf("%s in (%s)", field, strings.Join(parts, ", ")))
	return f
}

// Raw adds an already-formed clause, for expressions this builder does not
// cover.
func (f *Filter) Raw(clause string) *Filter {
	if clause = strings.TrimSpace(clause); clause != "" {
		f.clauses = append(f.clauses, clause)
	}
	return f
}

func (f *Filter) cmp(field, op string, value any) *Filter {
	if field == "" {
		return f
	}
	f.clauses = append(f.clauses, field+" "+op+" "+literal(value))
	return f
}

// Empty reports whether the filter has no clauses.
func (f *Filter) Empty() bool { return f == nil || len(f.clauses) == 0 }

// String renders the filter. An empty filter renders as "", which callers omit
// from the query rather than sending.
func (f *Filter) String() string {
	if f.Empty() {
		return ""
	}
	return strings.Join(f.clauses, " and ")
}

// literal renders a value in OData syntax: numbers and booleans bare, nil as
// null, everything else as a quoted string with embedded quotes doubled.
func literal(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case string:
		return "'" + strings.ReplaceAll(val, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(val), "'", "''") + "'"
	}
}
