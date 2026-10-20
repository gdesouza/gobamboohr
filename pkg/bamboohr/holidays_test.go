package bamboohr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const holidayPayload = `{
	"data": [
		{"id":42,"name":"Canada Day","startDate":"2026-07-01","endDate":null,"isPublic":true,
		 "countryCodes":["CA"],"audience":{"mode":"ALL_EMPLOYEES"},
		 "holidayPay":{"mode":"HOURS_BY_STATUS","defaultHours":"8.00"},
		 "globalHolidayUuid":"f5a3b821-0c1e-4d39-9b7e-2c6d8a4f9e10",
		 "createdAt":"2026-04-30T18:00:00Z","updatedAt":"2026-04-30T18:00:00Z"},
		{"id":43,"name":"Company Shutdown","startDate":"2026-12-24","endDate":"2026-12-31",
		 "isPublic":false,"countryCodes":[],
		 "audience":{"mode":"SPECIFIC_EMPLOYEES","specificEmployeeIds":[1,2,3]},
		 "holidayPay":null,"globalHolidayUuid":null,
		 "createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}
	],
	"meta": {"page":1,"pageSize":20,"totalPages":1,"totalItems":2}
}`

func TestListHolidays(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/holidays"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(holidayPayload))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.ListHolidays(context.Background(), HolidayListOptions{
		From:       "2026-01-01",
		To:         "2026-12-31",
		PublicOnly: true,
		Select:     []string{"id", "name", "startDate"},
		PageSize:   100,
	})
	if err != nil {
		t.Fatalf("ListHolidays: %v", err)
	}

	if want := "startDate ge '2026-01-01' and startDate le '2026-12-31' and isPublic eq true"; got.Get("filter") != want {
		t.Errorf("filter = %q, want %q", got.Get("filter"), want)
	}
	if got.Get("select") != "id,name,startDate" {
		t.Errorf("select = %q, want the comma-separated property list", got.Get("select"))
	}
	if got.Get("pageSize") != "100" {
		t.Errorf("pageSize = %q, want 100", got.Get("pageSize"))
	}

	if len(result.Data) != 2 {
		t.Fatalf("got %d holidays, want 2", len(result.Data))
	}

	single := result.Data[0]
	// A single-day holiday has a null end date, not an empty string.
	if single.EndDate != nil {
		t.Errorf("EndDate = %v, want nil for a single-day holiday", *single.EndDate)
	}
	if got := single.Days(); got != 1 {
		t.Errorf("Days() = %d, want 1", got)
	}
	if single.Custom() {
		t.Error("Custom() = true for a holiday seeded from the catalogue")
	}
	if single.Pay == nil || single.Pay.Mode != "HOURS_BY_STATUS" {
		t.Errorf("Pay = %+v, want HOURS_BY_STATUS", single.Pay)
	}

	span := result.Data[1]
	// 24 to 31 December inclusive is eight days.
	if got := span.Days(); got != 8 {
		t.Errorf("Days() = %d, want 8", got)
	}
	if !span.Custom() {
		t.Error("Custom() = false for a holiday with no catalogue UUID")
	}
	if span.Pay != nil {
		t.Error("Pay is set, want nil when the holiday has no pay treatment")
	}
	if span.Audience.Mode != "SPECIFIC_EMPLOYEES" || len(span.Audience.SpecificEmployeeIDs) != 3 {
		t.Errorf("Audience = %+v, want three specific employees", span.Audience)
	}
}

func TestListHolidaysRawFilterWins(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"data":[],"meta":{"page":1,"totalPages":1}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.ListHolidays(context.Background(), HolidayListOptions{
		From:   "2026-01-01",
		Filter: "name eq 'Canada Day'",
	}); err != nil {
		t.Fatalf("ListHolidays: %v", err)
	}
	if got.Get("filter") != "name eq 'Canada Day'" {
		t.Errorf("filter = %q, want the raw expression to win", got.Get("filter"))
	}
}

func TestListAllHolidaysWalksPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"data":[{"id":2,"name":"Two","startDate":"2026-02-01"}],
				"meta":{"page":2,"pageSize":1,"totalPages":2,"totalItems":2}}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":1,"name":"One","startDate":"2026-01-01"}],
			"meta":{"page":1,"pageSize":1,"totalPages":2,"totalItems":2}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	holidays, err := c.ListAllHolidays(context.Background(), HolidayListOptions{})
	if err != nil {
		t.Fatalf("ListAllHolidays: %v", err)
	}
	if len(holidays) != 2 {
		t.Fatalf("got %d holidays, want 2", len(holidays))
	}
}

func TestGetHoliday(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/holidays/42"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{"id":42,"name":"Canada Day","startDate":"2026-07-01","isPublic":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	holiday, err := c.GetHoliday(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetHoliday: %v", err)
	}
	if holiday.Name != "Canada Day" {
		t.Errorf("Name = %q, want Canada Day", holiday.Name)
	}

	if _, err := c.GetHoliday(context.Background(), 0); err == nil {
		t.Error("GetHoliday(0) succeeded, want an error")
	}
}

func TestListCatalogHolidays(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/holidays/catalog"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{"data":[{"uuid":"abc","name":"Canada Day","type":"PUBLIC",
			"startDate":"2026-07-01","endDate":null,"countryCode":"CA","source":"GoogleCalendar"}],
			"meta":{"page":1,"pageSize":20,"totalPages":1,"totalItems":1}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.ListCatalogHolidays(context.Background(), CatalogHolidayOptions{
		CountryCode: "ca",
		Year:        2026,
	})
	if err != nil {
		t.Fatalf("ListCatalogHolidays: %v", err)
	}
	// The API expects an upper-case ISO code.
	if got.Get("countryCode") != "CA" {
		t.Errorf("countryCode = %q, want CA", got.Get("countryCode"))
	}
	if got.Get("year") != "2026" {
		t.Errorf("year = %q, want 2026", got.Get("year"))
	}
	if len(result.Data) != 1 || result.Data[0].Type != "PUBLIC" {
		t.Errorf("data = %+v, want one PUBLIC entry", result.Data)
	}
}

func TestGetCatalogHolidayRequiresUUID(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetCatalogHoliday(context.Background(), "  "); err == nil {
		t.Fatal("GetCatalogHoliday with a blank uuid succeeded, want an error")
	}
}

func TestInclusiveDays(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
		want  int
	}{
		// Both ends count: a range is inclusive throughout this API.
		{name: "same day", start: "2026-07-01", end: "2026-07-01", want: 1},
		{name: "a week", start: "2026-12-24", end: "2026-12-31", want: 8},
		{name: "across a month", start: "2026-01-30", end: "2026-02-02", want: 4},
		// 2026 is not a leap year, so February has 28 days.
		{name: "across February", start: "2026-02-27", end: "2026-03-01", want: 3},
		{name: "backwards", start: "2026-07-05", end: "2026-07-01", want: 0},
		{name: "unparseable start", start: "nope", end: "2026-07-01", want: 0},
		{name: "unparseable end", start: "2026-07-01", end: "nope", want: 0},
		{name: "empty", start: "", end: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InclusiveDays(tt.start, tt.end); got != tt.want {
				t.Errorf("InclusiveDays(%q, %q) = %d, want %d", tt.start, tt.end, got, tt.want)
			}
		})
	}
}
