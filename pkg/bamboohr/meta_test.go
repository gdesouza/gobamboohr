package bamboohr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListFieldsHandlesMixedIDTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The API sends a top-level field ID as a number and a subfield ID as a
		// dotted string.
		w.Write([]byte(`[
			{"id": 4340, "name": "Department", "type": "list", "alias": "department"},
			{"id": "4340.4", "name": "Sub", "type": "text"},
			{"id": 9, "name": "Old", "type": "text", "deprecated": true}
		]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	fields, err := c.ListFields(context.Background())
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(fields))
	}

	if fields[0].ID != "4340" {
		t.Errorf("numeric id = %q, want 4340", fields[0].ID)
	}
	// An alias is what a fields parameter accepts, so it wins over the ID.
	if got := fields[0].Selector(); got != "department" {
		t.Errorf("Selector() = %q, want department", got)
	}
	if fields[1].ID != "4340.4" {
		t.Errorf("dotted id = %q, want 4340.4", fields[1].ID)
	}
	// Without an alias the ID is the only way to name the field.
	if got := fields[1].Selector(); got != "4340.4" {
		t.Errorf("Selector() = %q, want 4340.4", got)
	}
	if !fields[2].Deprecated {
		t.Error("deprecated field was not marked")
	}
}

func TestListTables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"alias":"jobInfo","fields":[{"id":4,"name":"Job Title","alias":"jobTitle","type":"list"}]}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tables, err := c.ListTables(context.Background())
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 1 || tables[0].Alias != "jobInfo" {
		t.Fatalf("tables = %+v, want one jobInfo entry", tables)
	}
	if tables[0].Fields[0].ID != "4" {
		t.Errorf("field id = %q, want 4", tables[0].Fields[0].ID)
	}
}

func TestListListsAsksForJSON(t *testing.T) {
	var format string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		format = r.URL.Query().Get("format")
		w.Write([]byte(`[{
			"id": 1, "fieldId": 4340, "name": "Department", "alias": "department",
			"manageable": "yes", "multiple": "no",
			"options": [
				{"id": 10, "name": "Robotics"},
				{"id": "11", "name": "Retired", "archived": "yes"}
			]
		}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	lists, err := c.ListLists(context.Background())
	if err != nil {
		t.Fatalf("ListLists: %v", err)
	}
	// This endpoint answers XML unless JSON is requested.
	if format != "json" {
		t.Errorf("format = %q, want json", format)
	}
	if len(lists) != 1 {
		t.Fatalf("got %d lists, want 1", len(lists))
	}

	list := lists[0]
	if !list.IsManageable() {
		t.Error("IsManageable = false, want true for manageable=yes")
	}
	if list.AcceptsMultiple() {
		t.Error("AcceptsMultiple = true, want false for multiple=no")
	}
	if list.Options[0].ID != "10" || list.Options[1].ID != "11" {
		t.Errorf("option ids = %q, %q, want 10 and 11", list.Options[0].ID, list.Options[1].ID)
	}
	if list.Options[0].IsArchived() {
		t.Error("first option reports itself archived")
	}
	if !list.Options[1].IsArchived() {
		t.Error("archived option was not marked")
	}
}

func TestListOptionAcceptsValueKey(t *testing.T) {
	// Some responses name the option text "value" rather than "name".
	var opt ListOption
	if err := json.Unmarshal([]byte(`{"id":3,"value":"Engineering"}`), &opt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if opt.Name != "Engineering" {
		t.Errorf("Name = %q, want Engineering", opt.Name)
	}
}

func TestListUsersFlattensAndSorts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("status"); got != "enabled" {
			t.Errorf("status = %q, want enabled", got)
		}
		// The API keys this response by user ID rather than returning an array.
		w.Write([]byte(`{
			"20": {"id":20,"employeeId":2,"firstName":"Bea","lastName":"B","email":"b@x.com","status":"enabled"},
			"10": {"id":10,"employeeId":0,"firstName":"Al","lastName":"A","email":"a@x.com","status":"enabled"}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	users, err := c.ListUsers(context.Background(), "enabled")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	// Map iteration is random, so the client sorts to keep output stable.
	if users[0].ID != 10 || users[1].ID != 20 {
		t.Errorf("user ids = %d, %d, want 10 then 20", users[0].ID, users[1].ID)
	}
	if users[0].EmployeeID != 0 {
		t.Errorf("employeeId = %d, want 0 for a user with no employee record", users[0].EmployeeID)
	}
}

func TestGetCompany(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/meta/company"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{"id":7,"name":"Acme","domain":"acme","baseApiUrl":"https://api.bamboohr.com/api/gateway.php/acme"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	company, err := c.GetCompany(context.Background())
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if company.Name != "Acme" || company.Domain != "acme" || company.ID != 7 {
		t.Errorf("company = %+v, want Acme/acme/7", company)
	}
}

func TestFlexString(t *testing.T) {
	tests := []struct {
		payload string
		want    string
	}{
		{payload: `"abc"`, want: "abc"},
		{payload: `42`, want: "42"},
		{payload: `"4340.4"`, want: "4340.4"},
		{payload: `null`, want: ""},
		{payload: ``, want: ""},
		{payload: `{}`, want: ""},
	}
	for _, tt := range tests {
		if got := flexString(json.RawMessage(tt.payload)); got != tt.want {
			t.Errorf("flexString(%s) = %q, want %q", tt.payload, got, tt.want)
		}
	}
}

func TestGetCompanyAcceptsStringID(t *testing.T) {
	// The published spec types meta/company's id as an integer, but the live
	// API sends it as a quoted number.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":"12345","name":"Acme","domain":"acme","baseApiUrl":"https://api.example/acme"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	company, err := c.GetCompany(context.Background())
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if company.ID.Int64() != 12345 {
		t.Errorf("id = %d, want 12345", company.ID.Int64())
	}
	if company.ID.String() != "12345" {
		t.Errorf("id = %q, want 12345", company.ID.String())
	}
}
