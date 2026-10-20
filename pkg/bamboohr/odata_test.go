package bamboohr

import "testing"

func TestFilterBuilding(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *Filter
		want   string
		isNoop bool
	}{
		{
			name:   "empty",
			build:  func() *Filter { return NewFilter() },
			want:   "",
			isNoop: true,
		},
		{
			name:  "string is quoted",
			build: func() *Filter { return NewFilter().Eq("status", "APPROVED") },
			want:  "status eq 'APPROVED'",
		},
		{
			name:  "number is bare",
			build: func() *Filter { return NewFilter().Eq("employeeId", 614) },
			want:  "employeeId eq 614",
		},
		{
			name:  "nil becomes null",
			build: func() *Filter { return NewFilter().Eq("terminationDate", nil) },
			want:  "terminationDate eq null",
		},
		{
			name:  "ne null matches set values",
			build: func() *Filter { return NewFilter().Ne("managerNote", nil) },
			want:  "managerNote ne null",
		},
		{
			name:  "in renders a list",
			build: func() *Filter { return NewFilter().In("employeeId", 614, 615, 616) },
			want:  "employeeId in (614, 615, 616)",
		},
		{
			name:   "in with no values is skipped",
			build:  func() *Filter { return NewFilter().In("employeeId") },
			want:   "",
			isNoop: true,
		},
		{
			name: "clauses join with and",
			build: func() *Filter {
				return NewFilter().
					Eq("status", "APPROVED").
					Ge("startDate", "2026-04-01").
					Le("endDate", "2026-05-01")
			},
			want: "status eq 'APPROVED' and startDate ge '2026-04-01' and endDate le '2026-05-01'",
		},
		{
			name:  "embedded quote is doubled",
			build: func() *Filter { return NewFilter().Eq("name", "O'Brien") },
			want:  "name eq 'O''Brien'",
		},
		{
			name:  "bool is bare",
			build: func() *Filter { return NewFilter().Eq("isManager", true) },
			want:  "isManager eq true",
		},
		{
			name:  "raw clause passes through",
			build: func() *Filter { return NewFilter().Raw("amount gt 8") },
			want:  "amount gt 8",
		},
		{
			name:   "blank raw clause is skipped",
			build:  func() *Filter { return NewFilter().Raw("   ") },
			want:   "",
			isNoop: true,
		},
		{
			name:  "comparisons",
			build: func() *Filter { return NewFilter().Lt("amount", 8).Gt("amount", 1) },
			want:  "amount lt 8 and amount gt 1",
		},
		{
			name:   "blank field name is skipped",
			build:  func() *Filter { return NewFilter().Eq("", "x") },
			want:   "",
			isNoop: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.build()
			if got := f.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := f.Empty(); got != tt.isNoop {
				t.Errorf("Empty() = %v, want %v", got, tt.isNoop)
			}
		})
	}
}

func TestFilterNilIsEmpty(t *testing.T) {
	var f *Filter
	if !f.Empty() {
		t.Error("a nil Filter reports itself non-empty")
	}
	if got := f.String(); got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
}
