package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func TestHolidayListDefaultsToThisYear(t *testing.T) {
	// A bare command should show something useful, and the API has no default
	// range of its own.
	var filter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = r.URL.Query().Get("filter")
		w.Write([]byte(`{"data":[],"meta":{"page":1,"totalPages":1}}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "holiday", "list"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	year := strconv.Itoa(time.Now().Year())
	if !strings.Contains(filter, year+"-01-01") || !strings.Contains(filter, year+"-12-31") {
		t.Errorf("filter = %q, want it to span %s", filter, year)
	}
}

func TestHolidayListYearShorthand(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{"data":[],"meta":{"page":1,"totalPages":1}}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "holiday", "list", "--year", "2026"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	want := "startDate ge '2026-01-01' and startDate le '2026-12-31'"
	if query.Get("filter") != want {
		t.Errorf("filter = %q, want %q", query.Get("filter"), want)
	}
	// The API's own page size of 20 would split a normal year over pages.
	if query.Get("pageSize") != "100" {
		t.Errorf("pageSize = %q, want 100", query.Get("pageSize"))
	}
}

func TestHolidayListRejectsConflictingRange(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`),
		"holiday", "list", "--year", "2026", "--from", "2026-03-01")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when both set the range")
	}
	if !strings.Contains(errOut, "use one or the other") {
		t.Errorf("stderr = %q, want it to explain the conflict", errOut)
	}
}

func TestHolidayListRejectsBadDate(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "holiday", "list", "--from", "01/03/2026")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a non-ISO date")
	}
	if !strings.Contains(errOut, "--from") {
		t.Errorf("stderr = %q, want it to name the flag", errOut)
	}
}

func TestHolidayListRendersSpanAndSource(t *testing.T) {
	body := `{"data":[
		{"id":42,"name":"Canada Day","startDate":"2026-07-01","endDate":null,"isPublic":true,
		 "countryCodes":["CA"],"audience":{"mode":"ALL_EMPLOYEES"},
		 "globalHolidayUuid":"abc-123"},
		{"id":43,"name":"Shutdown","startDate":"2026-12-24","endDate":"2026-12-31","isPublic":false,
		 "countryCodes":[],"audience":{"mode":"SPECIFIC_EMPLOYEES","specificEmployeeIds":[1,2,3]},
		 "globalHolidayUuid":null}
	],"meta":{"page":1,"pageSize":100,"totalPages":1,"totalItems":2}}`

	out, errOut, code := run(t, jsonServer(t, body), "holiday", "list", "--year", "2026")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// A single-day holiday is one day; 24 to 31 December is eight.
	if !strings.Contains(out, "Canada Day") || !strings.Contains(out, "Shutdown") {
		t.Fatalf("output lost a holiday:\n%s", out)
	}
	if !strings.Contains(out, "catalog") {
		t.Errorf("output = %q, want the catalogue-sourced holiday labelled", out)
	}
	if !strings.Contains(out, "custom") {
		t.Errorf("output = %q, want the company-defined holiday labelled custom", out)
	}
	if !strings.Contains(out, "3 employees") {
		t.Errorf("output = %q, want the specific-employee audience summarised", out)
	}
}

func TestHolidayCatalogRejectsBadCountry(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "holiday", "catalog", "--country", "CAN")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a three-letter code")
	}
	if !strings.Contains(errOut, "two-letter") {
		t.Errorf("stderr = %q, want it to explain the code format", errOut)
	}
}

func TestHolidayGetRejectsBadID(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "holiday", "get", "abc")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a non-numeric id")
	}
	if !strings.Contains(errOut, "holiday id") {
		t.Errorf("stderr = %q, want it to name the argument", errOut)
	}
}

func TestAudienceSummary(t *testing.T) {
	tests := []struct {
		name     string
		audience bamboohr.HolidayAudience
		want     string
	}{
		{name: "empty", audience: bamboohr.HolidayAudience{}, want: ""},
		{name: "everyone", audience: bamboohr.HolidayAudience{Mode: "ALL_EMPLOYEES"}, want: "all"},
		{
			name:     "filtered",
			audience: bamboohr.HolidayAudience{Mode: "FILTERED", FilterListValueIDs: []int{1, 2}},
			want:     "filtered (2)",
		},
		{
			name:     "specific",
			audience: bamboohr.HolidayAudience{Mode: "SPECIFIC_EMPLOYEES", SpecificEmployeeIDs: []int{1, 2, 3}},
			want:     "3 employees",
		},
		{name: "unknown mode", audience: bamboohr.HolidayAudience{Mode: "SOMETHING_NEW"}, want: "something_new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audienceSummary(tt.audience); got != tt.want {
				t.Errorf("audienceSummary = %q, want %q", got, tt.want)
			}
		})
	}
}
