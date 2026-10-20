package bamboohr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTimeOffRequestListOptionsBuildFilter(t *testing.T) {
	tests := []struct {
		name string
		opts TimeOffRequestListOptions
		want string
	}{
		{
			name: "empty sends no filter",
			opts: TimeOffRequestListOptions{},
			want: "",
		},
		{
			name: "single employee uses eq",
			opts: TimeOffRequestListOptions{EmployeeIDs: []int{614}},
			want: "employeeId eq 614",
		},
		{
			name: "several employees use in",
			opts: TimeOffRequestListOptions{EmployeeIDs: []int{614, 615}},
			want: "employeeId in (614, 615)",
		},
		{
			name: "single status uses eq",
			opts: TimeOffRequestListOptions{Statuses: []TimeOffStatus{StatusApproved}},
			want: "status eq 'APPROVED'",
		},
		{
			name: "several statuses use in",
			opts: TimeOffRequestListOptions{Statuses: []TimeOffStatus{StatusApproved, StatusDenied}},
			want: "status in ('APPROVED', 'DENIED')",
		},
		{
			name: "dates bound the range",
			opts: TimeOffRequestListOptions{Start: "2026-04-01", End: "2026-05-01"},
			want: "startDate ge '2026-04-01' and endDate le '2026-05-01'",
		},
		{
			name: "category is matched exactly",
			opts: TimeOffRequestListOptions{CategoryID: 22},
			want: "categoryId eq 22",
		},
		{
			name: "combined clauses",
			opts: TimeOffRequestListOptions{
				EmployeeIDs: []int{614},
				Statuses:    []TimeOffStatus{StatusApproved},
				Start:       "2026-04-01",
			},
			want: "employeeId eq 614 and status eq 'APPROVED' and startDate ge '2026-04-01'",
		},
		{
			name: "raw filter wins over the convenience fields",
			opts: TimeOffRequestListOptions{
				EmployeeIDs: []int{614},
				Filter:      "amount gt 8",
			},
			want: "amount gt 8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.query().Get("filter"); got != tt.want {
				t.Errorf("filter = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTimeOffRequestListOptionsPaging(t *testing.T) {
	q := TimeOffRequestListOptions{Page: 2, PageSize: 50, OrderBy: "requestedAt asc", ReturnActions: true}.query()
	for key, want := range map[string]string{
		"page":          "2",
		"pageSize":      "50",
		"orderBy":       "requestedAt asc",
		"returnActions": "true",
	} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, q.Get(key), want)
		}
	}

	// Zero values are left to the API's own defaults rather than sent.
	q = TimeOffRequestListOptions{}.query()
	for _, key := range []string{"page", "pageSize", "orderBy", "returnActions", "filter"} {
		if q.Has(key) {
			t.Errorf("%s was sent as %q, want it omitted", key, q.Get(key))
		}
	}
}

func TestListTimeOffRequestsDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/time-off/requests"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{
			"data": [{
				"id": 1, "employeeId": 43046, "categoryId": 22,
				"startDate": "2026-01-01", "endDate": "2026-01-03",
				"amount": 16, "unit": "HOURS", "status": "APPROVED",
				"requestedAt": "2026-01-01T06:00:00Z",
				"employeeNote": "Family trip.", "managerNote": null,
				"dailyAmounts": [{"date":"2026-01-01","amount":8}]
			}],
			"meta": {"page":1,"pageSize":100,"totalPages":1,"totalItems":1}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.ListTimeOffRequests(context.Background(), TimeOffRequestListOptions{})
	if err != nil {
		t.Fatalf("ListTimeOffRequests: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("got %d requests, want 1", len(result.Data))
	}

	req := result.Data[0]
	if req.Status != StatusApproved {
		t.Errorf("status = %q, want APPROVED", req.Status)
	}
	if req.Unit != UnitHours {
		t.Errorf("unit = %q, want HOURS", req.Unit)
	}
	if req.EmployeeNote == nil || *req.EmployeeNote != "Family trip." {
		t.Errorf("employeeNote = %v, want the note", req.EmployeeNote)
	}
	// A null note must stay distinguishable from an empty one.
	if req.ManagerNote != nil {
		t.Errorf("managerNote = %v, want nil for JSON null", *req.ManagerNote)
	}
	if len(req.DailyAmounts) != 1 || req.DailyAmounts[0].Amount != 8 {
		t.Errorf("dailyAmounts = %v, want one entry of 8", req.DailyAmounts)
	}
	if result.Meta.HasMore() {
		t.Error("HasMore = true on the only page")
	}
}

func TestListAllTimeOffRequestsWalksPages(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "1" {
			w.Write([]byte(`{"data":[{"id":1}],"meta":{"page":1,"pageSize":1,"totalPages":2,"totalItems":2}}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":2}],"meta":{"page":2,"pageSize":1,"totalPages":2,"totalItems":2}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	requests, err := c.ListAllTimeOffRequests(context.Background(), TimeOffRequestListOptions{})
	if err != nil {
		t.Fatalf("ListAllTimeOffRequests: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(requests))
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Errorf("requested pages %v, want [1 2]", pages)
	}
}

func TestWhosOutRequiresBothDates(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, opts := range []WhosOutOptions{
		{},
		{Start: "2026-01-01"},
		{End: "2026-01-31"},
	} {
		if _, err := c.WhosOut(context.Background(), opts); err == nil {
			t.Errorf("WhosOut(%+v) succeeded, want an error", opts)
		}
	}
}

func TestWhosOutQueryAndDecoding(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/whos-out"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{
			"data": [{"id":"12345","employeeId":42,"timeOffTypeId":7,"start":"2026-05-04","end":"2026-05-08"}],
			"persons": {"42": {"id":42,"firstName":"Jane","lastName":"Doe","displayName":"Jane Doe","jobTitle":null,"photoUrl":null}},
			"meta": {"page":1,"pageSize":100,"totalPages":1,"totalItems":1}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	page, err := c.WhosOut(context.Background(), WhosOutOptions{
		Start:          "2026-05-01",
		End:            "2026-05-31",
		DepartmentIDs:  []int{10, 20},
		LocationIDs:    []int{5},
		IncludePersons: true,
	})
	if err != nil {
		t.Fatalf("WhosOut: %v", err)
	}

	if got.Get("start") != "2026-05-01" || got.Get("end") != "2026-05-31" {
		t.Errorf("start/end = %q/%q, want the requested window", got.Get("start"), got.Get("end"))
	}
	if want := "department in (10, 20) and location eq 5"; got.Get("filter") != want {
		t.Errorf("filter = %q, want %q", got.Get("filter"), want)
	}
	if got.Get("includePersons") != "true" {
		t.Errorf("includePersons = %q, want true", got.Get("includePersons"))
	}

	if len(page.Data) != 1 || page.Data[0].EmployeeID != 42 {
		t.Fatalf("data = %+v, want one entry for employee 42", page.Data)
	}
	person, ok := page.Persons["42"]
	if !ok {
		t.Fatal("persons map has no entry for 42")
	}
	if person.DisplayName != "Jane Doe" {
		t.Errorf("displayName = %q, want Jane Doe", person.DisplayName)
	}
	if person.JobTitle != nil {
		t.Error("jobTitle should stay nil for JSON null")
	}
}

func TestListAllWhosOutMergesPersons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"data":[{"id":"2","employeeId":2}],"persons":{"2":{"id":2,"displayName":"Two"}},
				"meta":{"page":2,"pageSize":1,"totalPages":2,"totalItems":2}}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"1","employeeId":1}],"persons":{"1":{"id":1,"displayName":"One"}},
			"meta":{"page":1,"pageSize":1,"totalPages":2,"totalItems":2}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	page, err := c.ListAllWhosOut(context.Background(), WhosOutOptions{
		Start: "2026-01-01", End: "2026-01-31", IncludePersons: true,
	})
	if err != nil {
		t.Fatalf("ListAllWhosOut: %v", err)
	}
	if len(page.Data) != 2 {
		t.Errorf("got %d entries, want 2", len(page.Data))
	}
	// Each page carries only its own employees, so the maps must be merged.
	if len(page.Persons) != 2 {
		t.Errorf("got %d persons, want 2: %v", len(page.Persons), page.Persons)
	}
}

func TestNewTimeOffRequestValidation(t *testing.T) {
	amount := 16.0
	valid := NewTimeOffRequest{
		EmployeeID: 1, CategoryID: 2,
		StartDate: "2026-01-01", EndDate: "2026-01-02",
		Amount: &amount,
	}

	tests := []struct {
		name    string
		mutate  func(*NewTimeOffRequest)
		wantErr string
	}{
		{name: "valid with amount", mutate: func(*NewTimeOffRequest) {}},
		{
			name: "valid with daily amounts",
			mutate: func(r *NewTimeOffRequest) {
				r.Amount = nil
				r.DailyAmounts = []DailyAmount{{Date: "2026-01-01", Amount: 8}}
			},
		},
		{name: "no employee", mutate: func(r *NewTimeOffRequest) { r.EmployeeID = 0 }, wantErr: "employee id"},
		{name: "no category", mutate: func(r *NewTimeOffRequest) { r.CategoryID = 0 }, wantErr: "category id"},
		{name: "no start", mutate: func(r *NewTimeOffRequest) { r.StartDate = "" }, wantErr: "start date"},
		{name: "no end", mutate: func(r *NewTimeOffRequest) { r.EndDate = "" }, wantErr: "end date"},
		{
			name:    "neither amount nor daily",
			mutate:  func(r *NewTimeOffRequest) { r.Amount = nil },
			wantErr: "exactly one",
		},
		{
			name: "both amount and daily",
			mutate: func(r *NewTimeOffRequest) {
				r.DailyAmounts = []DailyAmount{{Date: "2026-01-01", Amount: 8}}
			},
			wantErr: "exactly one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid
			tt.mutate(&req)
			err := req.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate succeeded, want an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validate error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestCreateTimeOffRequestOmitsUnsetAmount(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		w.Write([]byte(`{"id":9}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateTimeOffRequest(context.Background(), NewTimeOffRequest{
		EmployeeID: 1, CategoryID: 2,
		StartDate: "2026-01-01", EndDate: "2026-01-01",
		DailyAmounts: []DailyAmount{{Date: "2026-01-01", Amount: 8}},
	})
	if err != nil {
		t.Fatalf("CreateTimeOffRequest: %v", err)
	}
	// The API rejects a body carrying both, so an unset amount must not appear
	// even as a zero.
	if _, ok := body["amount"]; ok {
		t.Errorf("body carried amount = %v, want it omitted", body["amount"])
	}
	if _, ok := body["dailyAmounts"]; !ok {
		t.Error("body did not carry dailyAmounts")
	}
}

func TestTimeOffDecisionEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*Client) (*TimeOffRequest, error)
		wantPath string
		wantNote bool
	}{
		{
			name: "approve",
			call: func(c *Client) (*TimeOffRequest, error) {
				return c.ApproveTimeOffRequest(context.Background(), 5, TimeOffDecision{ManagerNote: "ok"})
			},
			wantPath: "/api/v1/time-off/requests/5/approvals",
			wantNote: true,
		},
		{
			name: "deny",
			call: func(c *Client) (*TimeOffRequest, error) {
				return c.DenyTimeOffRequest(context.Background(), 5, TimeOffDecision{ManagerNote: "no"})
			},
			wantPath: "/api/v1/time-off/requests/5/denials",
			wantNote: true,
		},
		{
			name: "cancel",
			call: func(c *Client) (*TimeOffRequest, error) {
				return c.CancelTimeOffRequest(context.Background(), 5)
			},
			wantPath: "/api/v1/time-off/requests/5/cancellations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotMethod string
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Write([]byte(`{"id":5,"status":"APPROVED"}`))
			}))
			defer srv.Close()

			if _, err := tt.call(newTestClient(t, srv)); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotMethod != http.MethodPost {
				t.Errorf("method = %q, want POST", gotMethod)
			}
			if tt.wantNote && body["managerNote"] == nil {
				t.Error("managerNote was not sent")
			}
		})
	}
}

func TestTimeOffDecisionRejectsBadID(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.ApproveTimeOffRequest(context.Background(), 0, TimeOffDecision{}); err == nil {
		t.Error("ApproveTimeOffRequest(0) succeeded, want an error")
	}
	if _, err := c.CancelTimeOffRequest(context.Background(), -1); err == nil {
		t.Error("CancelTimeOffRequest(-1) succeeded, want an error")
	}
	if _, err := c.GetTimeOffRequest(context.Background(), 0, false); err == nil {
		t.Error("GetTimeOffRequest(0) succeeded, want an error")
	}
}

func TestGetTimeOffBalance(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/employees/12/time_off/calculator"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`[{"timeOffType":"64","name":"Vacation","units":"hours",
			"balance":"24.50","end":"2026-12-31","policyType":"accruing","usedYearToDate":"12.25"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	balances, err := c.GetTimeOffBalance(context.Background(), "12", "2026-12-31", 3)
	if err != nil {
		t.Fatalf("GetTimeOffBalance: %v", err)
	}
	if got.Get("end") != "2026-12-31" {
		t.Errorf("end = %q, want 2026-12-31", got.Get("end"))
	}
	if got.Get("precision") != "3" {
		t.Errorf("precision = %q, want 3", got.Get("precision"))
	}
	if len(balances) != 1 || balances[0].Balance != "24.50" {
		t.Fatalf("balances = %+v, want one entry of 24.50", balances)
	}
}

func TestGetTimeOffBalanceRequiresEmployee(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetTimeOffBalance(context.Background(), "", "", 0); err == nil {
		t.Fatal("GetTimeOffBalance with no employee succeeded, want an error")
	}
}

func TestListTimeOffTypesRequestMode(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"timeOffTypes":[{"id":"64","name":"Vacation","units":"hours","source":"internal"}],
			"defaultHours":[{"name":"Monday","amount":"8"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	types, err := c.ListTimeOffTypes(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTimeOffTypes: %v", err)
	}
	if got.Get("mode") != "request" {
		t.Errorf("mode = %q, want request", got.Get("mode"))
	}
	if len(types.TimeOffTypes) != 1 || types.TimeOffTypes[0].Name != "Vacation" {
		t.Errorf("types = %+v, want one Vacation entry", types.TimeOffTypes)
	}
	if len(types.DefaultHours) != 1 {
		t.Errorf("defaultHours = %+v, want one entry", types.DefaultHours)
	}
}

func TestPageMetaHasMore(t *testing.T) {
	tests := []struct {
		meta PageMeta
		want bool
	}{
		{meta: PageMeta{Page: 1, TotalPages: 3}, want: true},
		{meta: PageMeta{Page: 3, TotalPages: 3}, want: false},
		{meta: PageMeta{}, want: false},
	}
	for _, tt := range tests {
		if got := tt.meta.HasMore(); got != tt.want {
			t.Errorf("PageMeta%+v.HasMore() = %v, want %v", tt.meta, got, tt.want)
		}
	}
}
