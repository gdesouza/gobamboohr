package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// employeesFrom decodes API-shaped records the way a real response would be,
// so the table sees exactly what the client would hand it.
func employeesFrom(t *testing.T, payload string) []bamboohr.Employee {
	t.Helper()
	var employees []bamboohr.Employee
	if err := json.Unmarshal([]byte(payload), &employees); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return employees
}

func TestEmployeeTableOmitsPhotoColumns(t *testing.T) {
	// photoUrl is a signed URL hundreds of characters long, and the other two
	// are flags for the web UI's upload button. All three arrive in the default
	// field set without being asked for.
	employees := employeesFrom(t, `[{
		"employeeId": "123",
		"firstName": "Ada",
		"lastName": "Lovelace",
		"status": "Active",
		"photoUrl": "https://images3.bamboohr.com/1/photos/1.jpg?Policy=verylong",
		"photoUploaded": true,
		"canUploadPhoto": 0
	}]`)

	table := employeeTable(employees, nil)
	headers := strings.Join(table.Headers, ",")
	for _, unwanted := range []string{"photoUrl", "photoUploaded", "canUploadPhoto"} {
		if strings.Contains(headers, unwanted) {
			t.Errorf("headers = %v, want %s dropped", table.Headers, unwanted)
		}
	}
	// The useful columns must survive.
	for _, wanted := range []string{"employeeId", "firstName", "lastName", "status"} {
		if !strings.Contains(headers, wanted) {
			t.Errorf("headers = %v, want %s kept", table.Headers, wanted)
		}
	}
}

func TestEmployeeTableKeepsPhotoURLWhenRequested(t *testing.T) {
	employees := employeesFrom(t, `[{"employeeId":"123","photoUrl":"https://example.test/a.jpg"}]`)

	table := employeeTable(employees, []string{"photoUrl"})
	if !strings.Contains(strings.Join(table.Headers, ","), "photoUrl") {
		t.Errorf("headers = %v, want photoUrl kept when named in --fields", table.Headers)
	}
}

func TestEmployeeTableColumnOrder(t *testing.T) {
	// The reading order matters more than the API's own key order, and it has
	// to cover both spellings: the list endpoint sends employeeId and
	// jobTitleName, the directory sends id and jobTitle.
	tests := []struct {
		name    string
		payload string
		want    []string
	}{
		{
			name: "list shape",
			payload: `[{"employeeId":"1","firstName":"Ada","lastName":"L",
				"jobTitleName":"Engineer","status":"Active","workEmail":"a@x.test"}]`,
			want: []string{"employeeId", "firstName", "lastName", "jobTitleName", "workEmail", "status"},
		},
		{
			name: "directory shape",
			payload: `[{"id":"1","firstName":"Ada","lastName":"L","displayName":"Ada L",
				"jobTitle":"Engineer","department":"Robotics","workEmail":"a@x.test"}]`,
			want: []string{"id", "firstName", "lastName", "displayName", "jobTitle", "department", "workEmail"},
		},
		{
			name:    "unknown fields follow the known ones, sorted",
			payload: `[{"employeeId":"1","zeta":"z","customField9":"c"}]`,
			want:    []string{"employeeId", "customField9", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := employeeTable(employeesFrom(t, tt.payload), nil)
			if got := strings.Join(table.Headers, ","); got != strings.Join(tt.want, ",") {
				t.Errorf("headers = %v, want %v", table.Headers, tt.want)
			}
		})
	}
}

func TestEmployeeTableDropsEmptyColumns(t *testing.T) {
	table := employeeTable(employeesFrom(t, `[{"employeeId":"1","firstName":"Ada"}]`), nil)
	if !table.DropEmptyColumns {
		t.Error("DropEmptyColumns = false, want a response-derived listing to drop unused columns")
	}
}

func TestFieldPairs(t *testing.T) {
	fields := map[string]any{"id": "1", "lastName": "Lovelace", "firstName": "Ada"}
	pairs := fieldPairs(fields, []string{"id", "firstName"})

	if len(pairs) != 3 {
		t.Fatalf("got %d pairs, want 3", len(pairs))
	}
	if pairs[0][0] != "id" || pairs[1][0] != "firstName" {
		t.Errorf("pairs = %v, want the preferred fields first", pairs)
	}
	if pairs[0][1] != "1" {
		t.Errorf("id value = %q, want 1", pairs[0][1])
	}
}

func TestApplyStatusFilter(t *testing.T) {
	tests := []struct {
		name     string
		filters  map[string]string
		status   string
		explicit bool
		want     string // the resulting filter[status], "" for absent
		wantErr  string
	}{
		{
			name:   "defaults to active",
			status: employeeStatusActive,
			want:   "active",
		},
		{
			name:   "inactive",
			status: employeeStatusInactive,
			want:   "inactive",
		},
		{
			// "all" means no status filter, which is what brings the
			// permission-masked records back.
			name:   "all sends no filter",
			status: employeeStatusAll,
			want:   "",
		},
		{
			name:   "case and spacing are tolerated",
			status: " Active ",
			want:   "active",
		},
		{
			name:    "an explicit --filter replaces the default",
			filters: map[string]string{"status": "inactive"},
			status:  employeeStatusActive,
			want:    "inactive",
		},
		{
			name:    "other filters are preserved",
			filters: map[string]string{"departmentName": "Robotics"},
			status:  employeeStatusActive,
			want:    "active",
		},
		{
			name:     "setting the status twice is rejected",
			filters:  map[string]string{"status": "active"},
			status:   employeeStatusInactive,
			explicit: true,
			wantErr:  "use one or the other",
		},
		{
			name:    "unknown status",
			status:  "pending",
			wantErr: "want active, inactive or all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyStatusFilter(tt.filters, tt.status, tt.explicit)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("applyStatusFilter succeeded, want an error mentioning %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyStatusFilter: %v", err)
			}
			if got["status"] != tt.want {
				t.Errorf("filter[status] = %q, want %q", got["status"], tt.want)
			}
			for k, v := range tt.filters {
				if k != "status" && got[k] != v {
					t.Errorf("filter[%s] = %q, want %q preserved", k, got[k], v)
				}
			}
		})
	}
}

func TestIsBlankRow(t *testing.T) {
	tests := []struct {
		name string
		row  []string
		want bool
	}{
		// The identifier is always readable, so it does not count.
		{name: "only an id", row: []string{"1", "", "", ""}, want: true},
		{name: "one readable value", row: []string{"1", "", "Ada"}, want: false},
		{name: "whitespace only", row: []string{"1", "  "}, want: true},
		{name: "fully populated", row: []string{"1", "Ada", "Lovelace"}, want: false},
		{name: "single column", row: []string{"1"}, want: false},
		{name: "empty", row: nil, want: false},
	}
	for _, tt := range tests {
		if got := isBlankRow(tt.row); got != tt.want {
			t.Errorf("%s: isBlankRow(%v) = %v, want %v", tt.name, tt.row, got, tt.want)
		}
	}
}
