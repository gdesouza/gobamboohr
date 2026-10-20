package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGetEmployeeQueryAndDecoding(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/employees/123"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		// A real response includes custom fields with no typed counterpart.
		w.Write([]byte(`{
			"id": "123",
			"firstName": "Ada",
			"lastName": "Lovelace",
			"workEmail": "ada@example.com",
			"customField4340": "Robotics",
			"_restrictedFields": ["payRate"]
		}`))
	}))
	defer srv.Close()

	onlyCurrent := false
	c := newTestClient(t, srv)
	emp, err := c.GetEmployee(context.Background(), "123", GetEmployeeOptions{
		Fields:      []string{"firstName", "lastName", "workEmail", "customField4340"},
		OnlyCurrent: &onlyCurrent,
	})
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}

	if want := "firstName,lastName,workEmail,customField4340"; got.Get("fields") != want {
		t.Errorf("fields = %q, want %q", got.Get("fields"), want)
	}
	if got.Get("onlyCurrent") != "false" {
		t.Errorf("onlyCurrent = %q, want false", got.Get("onlyCurrent"))
	}

	if emp.Key() != "123" {
		t.Errorf("Key() = %q, want 123", emp.Key())
	}
	if emp.FirstName != "Ada" || emp.LastName != "Lovelace" {
		t.Errorf("name = %q %q, want Ada Lovelace", emp.FirstName, emp.LastName)
	}
	// A custom field has no struct field, so it must be reachable through
	// Fields.
	if got := emp.Field("customField4340"); got != "Robotics" {
		t.Errorf("Field(customField4340) = %q, want Robotics", got)
	}
	if got := emp.Field("nope"); got != "" {
		t.Errorf("Field(nope) = %q, want empty", got)
	}
	if len(emp.RestrictedFields) != 1 || emp.RestrictedFields[0] != "payRate" {
		t.Errorf("RestrictedFields = %v, want [payRate]", emp.RestrictedFields)
	}
	// _restrictedFields is metadata, not a field value.
	if _, ok := emp.Fields["_restrictedFields"]; ok {
		t.Error("_restrictedFields leaked into Fields")
	}
}

func TestEmployeeRoundTripsUnknownFields(t *testing.T) {
	payload := `{"id":"5","firstName":"Ada","customField9":"kept"}`
	var emp Employee
	if err := json.Unmarshal([]byte(payload), &emp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	out, err := json.Marshal(emp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("Unmarshal of re-encoded employee: %v", err)
	}
	if back["customField9"] != "kept" {
		t.Errorf("customField9 = %v, want kept: custom fields must survive a round trip", back["customField9"])
	}
}

func TestGetEmployeeRequiresID(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetEmployee(context.Background(), "  ", GetEmployeeOptions{}); err == nil {
		t.Fatal("GetEmployee with a blank id succeeded, want an error")
	}
}

func TestListEmployeesQueryEncoding(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"data":[],"meta":{"total":0,"page":{"limit":250}}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListEmployees(context.Background(), EmployeeListOptions{
		Filters: map[string]string{"status": "active", "departmentName": "Robotics"},
		IDs:     []string{"1", "2"},
		Fields:  []string{"workEmail", "hireDate"},
		Sort:    "-lastName",
		Limit:   500,
		After:   "cursor-1",
	})
	if err != nil {
		t.Fatalf("ListEmployees: %v", err)
	}

	want := map[string]string{
		"filter[status]":         "active",
		"filter[departmentName]": "Robotics",
		// The comma form is used because repeated keys are unreliable.
		"filter[ids]": "1,2",
		"fields":      "workEmail,hireDate",
		"sort":        "-lastName",
		"page[limit]": "500",
		"page[after]": "cursor-1",
	}
	for key, expected := range want {
		if got.Get(key) != expected {
			t.Errorf("%s = %q, want %q", key, got.Get(key), expected)
		}
	}
}

func TestListAllEmployeesFollowsCursor(t *testing.T) {
	var cursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("page[after]")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			w.Write([]byte(`{"data":[{"employeeId":"1"}],"meta":{"total":3,"page":{"nextCursor":"c2"}}}`))
		case "c2":
			w.Write([]byte(`{"data":[{"employeeId":"2"}],"meta":{"total":3,"page":{"nextCursor":"c3"}}}`))
		default:
			// A page with no next cursor ends the walk.
			w.Write([]byte(`{"data":[{"employeeId":"3"}],"meta":{"total":3,"page":{"nextCursor":""}}}`))
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	employees, err := c.ListAllEmployees(context.Background(), EmployeeListOptions{})
	if err != nil {
		t.Fatalf("ListAllEmployees: %v", err)
	}
	if len(employees) != 3 {
		t.Fatalf("got %d employees, want 3", len(employees))
	}
	for i, want := range []string{"1", "2", "3"} {
		if employees[i].Key() != want {
			t.Errorf("employee %d id = %q, want %q", i, employees[i].Key(), want)
		}
	}
	if len(cursors) != 3 {
		t.Errorf("made %d requests, want 3", len(cursors))
	}
}

func TestListAllEmployeesStopsOnEmptyPage(t *testing.T) {
	// A page that reports a next cursor but returns nothing would otherwise
	// loop forever.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(`{"data":[],"meta":{"total":0,"page":{"nextCursor":"always"}}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	employees, err := c.ListAllEmployees(context.Background(), EmployeeListOptions{})
	if err != nil {
		t.Fatalf("ListAllEmployees: %v", err)
	}
	if len(employees) != 0 {
		t.Errorf("got %d employees, want 0", len(employees))
	}
	if calls != 1 {
		t.Errorf("made %d requests, want 1", calls)
	}
}

func TestCreateEmployeeReadsLocationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		if body["firstName"] != "Ada" {
			t.Errorf("firstName = %v, want Ada", body["firstName"])
		}
		w.Header().Set("Location", "https://acme.bamboohr.com/employees/4242/")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.CreateEmployee(context.Background(), map[string]any{"firstName": "Ada", "lastName": "Lovelace"})
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if id != "4242" {
		t.Errorf("id = %q, want 4242", id)
	}
}

func TestCreateEmployeeFallsBackToBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Location header; the ID is only in the body.
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"77"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.CreateEmployee(context.Background(), map[string]any{"firstName": "Ada"})
	if err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if id != "77" {
		t.Errorf("id = %q, want 77", id)
	}
}

func TestCreateAndUpdateEmployeeRejectEmptyInput(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.CreateEmployee(context.Background(), nil); err == nil {
		t.Error("CreateEmployee with no fields succeeded, want an error")
	}
	if err := c.UpdateEmployee(context.Background(), "1", nil); err == nil {
		t.Error("UpdateEmployee with no fields succeeded, want an error")
	}
	if err := c.UpdateEmployee(context.Background(), "", map[string]any{"a": "b"}); err == nil {
		t.Error("UpdateEmployee with no id succeeded, want an error")
	}
}

func TestGetChangedEmployees(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{
			"latest": "2026-09-30T10:00:00+00:00",
			"employees": {"12": {"id":"12","action":"Updated","lastChanged":"2026-09-30T09:00:00+00:00"}}
		}`))
	}))
	defer srv.Close()

	since := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	c := newTestClient(t, srv)
	changed, err := c.GetChangedEmployees(context.Background(), since, ChangeUpdated)
	if err != nil {
		t.Fatalf("GetChangedEmployees: %v", err)
	}

	if want := "2026-09-29T08:00:00Z"; got.Get("since") != want {
		t.Errorf("since = %q, want %q", got.Get("since"), want)
	}
	if got.Get("type") != "updated" {
		t.Errorf("type = %q, want updated", got.Get("type"))
	}
	if changed.Employees["12"].Action != "Updated" {
		t.Errorf("action = %q, want Updated", changed.Employees["12"].Action)
	}
	if changed.Latest == "" {
		t.Error("Latest is empty; it is needed for the next poll")
	}
}

func TestGetChangedEmployeesRequiresSince(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetChangedEmployees(context.Background(), time.Time{}, ChangeAll); err == nil {
		t.Fatal("GetChangedEmployees with a zero time succeeded, want an error")
	}
}

func TestGetEmployeeTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/employees/12/tables/jobInfo"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`[{"id":"1","employeeId":"12","jobTitle":"Engineer","date":"2024-01-01"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	rows, err := c.GetEmployeeTable(context.Background(), "12", "jobInfo")
	if err != nil {
		t.Fatalf("GetEmployeeTable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].ID() != "1" {
		t.Errorf("row ID = %q, want 1", rows[0].ID())
	}
	if rows[0]["jobTitle"] != "Engineer" {
		t.Errorf("jobTitle = %v, want Engineer", rows[0]["jobTitle"])
	}
}

func TestGetEmployeeTableValidatesArguments(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetEmployeeTable(context.Background(), "", "jobInfo"); err == nil {
		t.Error("blank employee id succeeded, want an error")
	}
	if _, err := c.GetEmployeeTable(context.Background(), "1", ""); err == nil {
		t.Error("blank table name succeeded, want an error")
	}
}

func TestEmployeeIDsAreEscapedInPaths(t *testing.T) {
	// An ID is user input; it must not be able to change the request path.
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetEmployee(context.Background(), "../meta/users", GetEmployeeOptions{}); err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	if strings.Contains(path, "/meta/users") {
		t.Errorf("path = %q, want the id escaped rather than traversing", path)
	}
}

func TestGetEmployeePhotoUsesDefaultSize(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprint(w, "jpegbytes")
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	meta, err := c.GetEmployeePhoto(context.Background(), "12", "", &buf)
	if err != nil {
		t.Fatalf("GetEmployeePhoto: %v", err)
	}
	if want := "/api/v1/employees/12/photo/medium"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if meta.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", meta.ContentType)
	}
	if buf.String() != "jpegbytes" {
		t.Errorf("wrote %q, want jpegbytes", buf.String())
	}
}

func TestEmployeeResolvesFieldNameAliases(t *testing.T) {
	// The employee endpoints disagree on names for the same concept, so a
	// typed field must be filled by whichever spelling arrived.
	tests := []struct {
		name          string
		payload       string
		wantJobTitle  string
		wantReportsTo string
		wantStatus    string
	}{
		{
			name: "single-employee spellings",
			payload: `{"id":"208","jobTitle":"Software Manager","supervisor":"Rivera, Pat",
				"supervisorId":"42","employmentStatus":"Full-Time"}`,
			wantJobTitle:  "Software Manager",
			wantReportsTo: "Rivera, Pat",
			wantStatus:    "Full-Time",
		},
		{
			name:          "list spellings",
			payload:       `{"employeeId":"208","jobTitleName":"Software Manager","status":"Active"}`,
			wantJobTitle:  "Software Manager",
			wantReportsTo: "",
			wantStatus:    "Active",
		},
		{
			name:          "report spellings",
			payload:       `{"id":"208","reportsToName":"Pat Rivera","reportsToId":"42"}`,
			wantReportsTo: "Pat Rivera",
		},
		{
			// The tagged spelling must win over an alias when both arrive.
			name:         "tagged name wins",
			payload:      `{"id":"1","jobTitle":"Tagged","jobTitleName":"Alias"}`,
			wantJobTitle: "Tagged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var emp Employee
			if err := json.Unmarshal([]byte(tt.payload), &emp); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if emp.JobTitle != tt.wantJobTitle {
				t.Errorf("JobTitle = %q, want %q", emp.JobTitle, tt.wantJobTitle)
			}
			if emp.ReportsToName != tt.wantReportsTo {
				t.Errorf("ReportsToName = %q, want %q", emp.ReportsToName, tt.wantReportsTo)
			}
			if tt.wantStatus != "" && emp.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", emp.Status, tt.wantStatus)
			}
			if emp.Key() != "208" && emp.Key() != "1" {
				t.Errorf("Key() = %q, want the id whichever key carried it", emp.Key())
			}
		})
	}
}

func TestDefaultEmployeeFieldsUseSingleEmployeeSpellings(t *testing.T) {
	// GetEmployee drops a name it does not recognise without an error, so a
	// list-endpoint spelling here would silently return nothing.
	for _, wrong := range []string{"jobTitleName", "reportsToName", "employeeId"} {
		for _, got := range DefaultEmployeeFields {
			if got == wrong {
				t.Errorf("DefaultEmployeeFields contains %q, which GetEmployee does not recognise", wrong)
			}
		}
	}
	for _, want := range []string{"firstName", "lastName", "jobTitle", "supervisor", "workEmail"} {
		if !slices.Contains(DefaultEmployeeFields, want) {
			t.Errorf("DefaultEmployeeFields is missing %q", want)
		}
	}
}
