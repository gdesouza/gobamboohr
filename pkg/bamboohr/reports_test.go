package bamboohr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListReports(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/custom-reports"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{"reports":[{"id":4,"name":"Headcount"}],
			"pagination":{"total_records":1,"current_page":1,"total_pages":1}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.ListReports(context.Background(), 2, 100)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if got.Get("page") != "2" {
		t.Errorf("page = %q, want 2", got.Get("page"))
	}
	// This endpoint spells the size parameter with an underscore.
	if got.Get("page_size") != "100" {
		t.Errorf("page_size = %q, want 100", got.Get("page_size"))
	}
	if len(result.Reports) != 1 || result.Reports[0].Name != "Headcount" {
		t.Errorf("reports = %+v, want one Headcount entry", result.Reports)
	}
}

func TestGetReportKeepsColumnOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/custom-reports/42"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{
			"fields": [
				{"name":"lastName","label":"Surname"},
				{"name":"jobInformationLocation","label":null}
			],
			"data": [{"lastName":"Lovelace","jobInformationLocation":"Remote"}],
			"pagination": {"total_records":1,"current_page":1,"total_pages":1}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	report, err := c.GetReport(context.Background(), 42, 0, 0)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if len(report.Columns) != 2 {
		t.Fatalf("got %d columns, want 2", len(report.Columns))
	}
	// A renamed column shows the customer's own wording.
	if got := report.Columns[0].Heading(); got != "Surname" {
		t.Errorf("Heading() = %q, want Surname", got)
	}
	// A null label falls back to the key.
	if got := report.Columns[1].Heading(); got != "jobInformationLocation" {
		t.Errorf("Heading() = %q, want the column key", got)
	}
	if report.Rows[0]["lastName"] != "Lovelace" {
		t.Errorf("row = %v, want Lovelace", report.Rows[0])
	}
}

func TestGetAllReportDataWalksPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"fields":[{"name":"id"}],"data":[{"id":"2"}],
				"pagination":{"total_records":2,"current_page":2,"total_pages":2}}`))
			return
		}
		w.Write([]byte(`{"fields":[{"name":"id"}],"data":[{"id":"1"}],
			"pagination":{"total_records":2,"current_page":1,"total_pages":2}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	report, err := c.GetAllReportData(context.Background(), 42, 1)
	if err != nil {
		t.Fatalf("GetAllReportData: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(report.Rows))
	}
	// The columns come from the first page and must survive the merge.
	if len(report.Columns) != 1 || report.Columns[0].Name != "id" {
		t.Errorf("columns = %+v, want one id column", report.Columns)
	}
}

func TestGetReportRequiresID(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetReport(context.Background(), 0, 0, 0); err == nil {
		t.Fatal("GetReport(0) succeeded, want an error")
	}
}

func TestPaginationHasMore(t *testing.T) {
	tests := []struct {
		pagination Pagination
		want       bool
	}{
		{pagination: Pagination{CurrentPage: 1, TotalPages: 5}, want: true},
		{pagination: Pagination{CurrentPage: 5, TotalPages: 5}, want: false},
		{pagination: Pagination{}, want: false},
	}
	for _, tt := range tests {
		if got := tt.pagination.HasMore(); got != tt.want {
			t.Errorf("Pagination%+v.HasMore() = %v, want %v", tt.pagination, got, tt.want)
		}
	}
}
