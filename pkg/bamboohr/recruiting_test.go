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

// jobsPayload mirrors the live shape, including the nulls a job opening can
// carry for its title ID, department and hiring lead.
const jobsPayload = `[{
	"id": 937,
	"title": {"id": null, "label": "Software Developer"},
	"postedDate": "2026-08-25T14:00:00+00:00",
	"location": {"id": 5, "label": "Springfield, Ontario",
	             "address": {"city": "Springfield", "state": "ON", "country": "Canada"}},
	"department": {"id": 9, "label": "Engineering"},
	"status": {"id": 3, "label": "On Hold"},
	"hiringLead": {"employeeId": 208, "firstName": "Alex", "lastName": "Morgan", "avatar": null},
	"newApplicantsCount": 403,
	"activeApplicantsCount": 416,
	"totalApplicantsCount": 416,
	"postingUrl": "https://acme.bamboohr.com/careers/937"
}, {
	"id": 400,
	"title": {"id": 7, "label": "Contract Role"},
	"postedDate": "2026-01-02T00:00:00+00:00",
	"location": null, "department": null,
	"status": {"id": 1, "label": "Draft"},
	"hiringLead": null,
	"newApplicantsCount": 0, "activeApplicantsCount": 0, "totalApplicantsCount": 0,
	"postingUrl": null
}]`

func TestListJobOpenings(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if want := "/api/v1/applicant_tracking/jobs"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(jobsPayload))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	jobs, err := c.ListJobOpenings(context.Background(), JobOpeningListOptions{
		StatusGroups: "ALL",
		StatusIDs:    []string{"1", "3"},
		SortBy:       "count",
		SortOrder:    "desc",
	})
	if err != nil {
		t.Fatalf("ListJobOpenings: %v", err)
	}

	if got.Get("statusGroups") != "ALL" {
		t.Errorf("statusGroups = %q, want ALL", got.Get("statusGroups"))
	}
	if got.Get("status_ids") != "1,3" {
		t.Errorf("status_ids = %q, want 1,3", got.Get("status_ids"))
	}
	// The API only accepts the order in upper case.
	if got.Get("sortOrder") != "DESC" {
		t.Errorf("sortOrder = %q, want DESC", got.Get("sortOrder"))
	}

	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	first := jobs[0]
	if first.Title.Label != "Software Developer" {
		t.Errorf("title = %q, want Software Developer", first.Title.Label)
	}
	// A free-text title has no list ID.
	if first.Title.ID != nil {
		t.Errorf("title ID = %v, want nil", *first.Title.ID)
	}
	if first.HiringLead == nil || first.HiringLead.Name() != "Alex Morgan" {
		t.Errorf("hiring lead = %+v, want Alex Morgan", first.HiringLead)
	}
	if first.NewApplicantsCount != 403 {
		t.Errorf("newApplicantsCount = %d, want 403", first.NewApplicantsCount)
	}

	// The optional relationships must stay nil rather than panicking.
	second := jobs[1]
	if second.Location != nil || second.Department != nil || second.HiringLead != nil {
		t.Errorf("job 400 = %+v, want nil location, department and hiring lead", second)
	}
	if second.PostingURL != nil {
		t.Error("PostingURL is set, want nil for an unposted opening")
	}
}

func TestListApplicationsQuery(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"applications":[],"paginationComplete":true,"nextPageUrl":null}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListApplications(context.Background(), ApplicationListOptions{
		Page:            2,
		JobID:           937,
		StatusID:        "1",
		StatusGroups:    "NEW",
		JobStatusGroups: "ALL",
		Search:          "smith",
		NewSince:        "2026-09-01T00:00:00Z",
		SortBy:          "created_date",
		SortOrder:       "desc",
	})
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}

	want := map[string]string{
		"page":  "2",
		"jobId": "937",
		// The two status filters reach different parameters.
		"applicationStatusId": "1",
		"applicationStatus":   "NEW",
		"jobStatusGroups":     "ALL",
		"searchString":        "smith",
		"newSince":            "2026-09-01T00:00:00Z",
		"sortBy":              "created_date",
		"sortOrder":           "DESC",
	}
	for key, expected := range want {
		if got.Get(key) != expected {
			t.Errorf("%s = %q, want %q", key, got.Get(key), expected)
		}
	}
}

func TestListApplicationsDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"applications":[{
			"id": 114742,
			"appliedDate": "2026-09-01T10:00:00+00:00",
			"status": {"id": 1, "label": "New"},
			"rating": null,
			"applicant": {"id": 1, "firstName": "Ada", "lastName": "Lovelace",
			              "email": "ada@example.test", "source": "LinkedIn", "avatar": null},
			"job": {"id": 937, "title": {"id": null, "label": "Software Developer"}}
		}], "paginationComplete": false, "nextPageUrl": "https://acme.bamboohr.com/next"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	result, err := c.ListApplications(context.Background(), ApplicationListOptions{})
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}
	if len(result.Applications) != 1 {
		t.Fatalf("got %d applications, want 1", len(result.Applications))
	}

	app := result.Applications[0]
	if app.Applicant.Name() != "Ada Lovelace" {
		t.Errorf("applicant = %q, want Ada Lovelace", app.Applicant.Name())
	}
	// An unrated candidate must stay distinguishable from one rated zero.
	if app.Rating != nil {
		t.Errorf("rating = %d, want nil for an unrated application", *app.Rating)
	}
	if result.PaginationComplete {
		t.Error("PaginationComplete = true, want false when a next page exists")
	}
}

func TestListAllApplicationsUsesTheCompletionFlag(t *testing.T) {
	// This endpoint reports paging with a flag rather than page counters, so
	// the walk has to trust the flag.
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "2" {
			w.Write([]byte(`{"applications":[{"id":2}],"paginationComplete":true,"nextPageUrl":null}`))
			return
		}
		w.Write([]byte(`{"applications":[{"id":1}],"paginationComplete":false,
			"nextPageUrl":"https://acme.bamboohr.com/next"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	applications, err := c.ListAllApplications(context.Background(), ApplicationListOptions{})
	if err != nil {
		t.Fatalf("ListAllApplications: %v", err)
	}
	if len(applications) != 2 {
		t.Fatalf("got %d applications, want 2", len(applications))
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Errorf("requested pages %v, want [1 2]", pages)
	}
}

func TestListAllApplicationsStopsOnEmptyPage(t *testing.T) {
	// A page that claims more results but returns none would loop forever.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(`{"applications":[],"paginationComplete":false,"nextPageUrl":"x"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.ListAllApplications(context.Background(), ApplicationListOptions{}); err != nil {
		t.Fatalf("ListAllApplications: %v", err)
	}
	if calls != 1 {
		t.Errorf("made %d requests, want 1", calls)
	}
}

func TestGetApplicationKeepsUnknownFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/applicant_tracking/applications/24732"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{
			"id": 24732,
			"appliedDate": "2022-01-16T21:32:50+00:00",
			"status": {"id": 3, "label": "Schedule Phone Screen", "dateChanged": "2022-01-19T23:46:21+00:00"},
			"rating": 4, "resumeFileId": 25625, "coverLetterFileId": null,
			"applicant": {"id": 9, "firstName": "Ada", "lastName": "Lovelace", "email": "ada@example.test"},
			"attachments": [{"id": 1, "name": "resume.pdf", "fileUrl": "https://example.test/r.pdf"}],
			"somethingNew": "kept"
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	app, err := c.GetApplication(context.Background(), 24732)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if app.Status.Label != "Schedule Phone Screen" || app.Status.DateChanged == "" {
		t.Errorf("status = %+v, want the label and when it changed", app.Status)
	}
	if app.Rating == nil || *app.Rating != 4 {
		t.Errorf("rating = %v, want 4", app.Rating)
	}
	if app.CoverLetterFileID != nil {
		t.Error("CoverLetterFileID is set, want nil")
	}
	if len(app.Attachments) != 1 || app.Attachments[0].Name != "resume.pdf" {
		t.Errorf("attachments = %+v, want one resume.pdf", app.Attachments)
	}
	// A field this struct does not name must still be reachable.
	if app.Fields["somethingNew"] != "kept" {
		t.Errorf("Fields[somethingNew] = %v, want kept", app.Fields["somethingNew"])
	}
}

func TestListApplicantStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"id":"1","code":"NEW","name":"New","translatedName":"New","enabled":true,"manageable":false},
			{"id":"20","code":null,"name":"Candidate did not respond","translatedName":"Candidate did not respond",
			 "enabled":true,"manageable":true}
		]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	statuses, err := c.ListApplicantStatuses(context.Background())
	if err != nil {
		t.Fatalf("ListApplicantStatuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(statuses))
	}
	// The ID is a string here, unlike the integer SetApplicationStatus takes.
	if statuses[0].ID != "1" {
		t.Errorf("id = %q, want \"1\"", statuses[0].ID)
	}
	// A status the company added has no internal code.
	if statuses[1].Code != nil {
		t.Errorf("code = %v, want nil for a custom status", *statuses[1].Code)
	}
}

func TestSetApplicationStatus(t *testing.T) {
	var body map[string]int
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"type":"positionApplicantStatus","id":5}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetApplicationStatus(context.Background(), 4821, 2); err != nil {
		t.Fatalf("SetApplicationStatus: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if want := "/api/v1/applicant_tracking/applications/4821/status"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if body["status"] != 2 {
		t.Errorf("body = %v, want status 2", body)
	}

	if err := c.SetApplicationStatus(context.Background(), 0, 2); err == nil {
		t.Error("a zero application id succeeded, want an error")
	}
	if err := c.SetApplicationStatus(context.Background(), 1, 0); err == nil {
		t.Error("a zero status id succeeded, want an error")
	}
}

func TestCommentOnApplication(t *testing.T) {
	var body map[string]string
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"type":"comment","id":11}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.CommentOnApplication(context.Background(), 4821, "Strong round"); err != nil {
		t.Fatalf("CommentOnApplication: %v", err)
	}
	if want := "/api/v1/applicant_tracking/applications/4821/comments"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if body["comment"] != "Strong round" {
		t.Errorf("comment = %q, want the text", body["comment"])
	}
	if body["type"] != "comment" {
		t.Errorf("type = %q, want comment", body["type"])
	}

	if err := c.CommentOnApplication(context.Background(), 1, "   "); err == nil {
		t.Error("a blank comment succeeded, want an error")
	}
	if err := c.CommentOnApplication(context.Background(), 0, "x"); err == nil {
		t.Error("a zero application id succeeded, want an error")
	}
}

func TestListHiringLeads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/applicant_tracking/hiring_leads"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`[{"employeeId":208,"preferredFullName":"Alex Morgan"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	leads, err := c.ListHiringLeads(context.Background())
	if err != nil {
		t.Fatalf("ListHiringLeads: %v", err)
	}
	if len(leads) != 1 || leads[0].EmployeeID != 208 {
		t.Errorf("leads = %+v, want employee 208", leads)
	}
}

func TestListRecruitingLocationsKeepsVaryingAddressShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"id":5,"name":"Springfield","city":"Springfield","state":"ON","country":"Canada"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	locations, err := c.ListRecruitingLocations(context.Background())
	if err != nil {
		t.Fatalf("ListRecruitingLocations: %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("got %d locations, want 1", len(locations))
	}
	if locations[0].Name != "Springfield" {
		t.Errorf("name = %q, want Springfield", locations[0].Name)
	}
	// The address fields vary by country, so they are kept as returned.
	if locations[0].Fields["state"] != "ON" {
		t.Errorf("Fields[state] = %v, want ON", locations[0].Fields["state"])
	}
}

func TestLabelledPrintsItsLabel(t *testing.T) {
	id := 7
	if got := (Labelled{ID: &id, Label: "Engineering"}).String(); got != "Engineering" {
		t.Errorf("String() = %q, want Engineering", got)
	}
}

func TestApplicationFilesFlattensResumeAndAttachments(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    []ApplicationFile
	}{
		{
			// The live shape: a resume ID with a null attachments list, which
			// is why the documented fileUrl route cannot reach it.
			name:    "resume only",
			payload: `{"id":1,"resumeFileId":25625,"coverLetterFileId":null,"attachments":null}`,
			want:    []ApplicationFile{{ID: 25625, Kind: FileResume}},
		},
		{
			name:    "resume and cover letter",
			payload: `{"id":1,"resumeFileId":10,"coverLetterFileId":11,"attachments":null}`,
			want: []ApplicationFile{
				{ID: 10, Kind: FileResume},
				{ID: 11, Kind: FileCoverLetter},
			},
		},
		{
			name: "attachments carry their own names",
			payload: `{"id":1,"resumeFileId":10,"coverLetterFileId":null,
				"attachments":[{"id":20,"name":"portfolio.pdf","fileUrl":"https://x.test/p.pdf"}]}`,
			want: []ApplicationFile{
				{ID: 10, Kind: FileResume},
				{ID: 20, Kind: FileAttachment, Name: "portfolio.pdf", URL: "https://x.test/p.pdf"},
			},
		},
		{
			// The resume is sometimes repeated in the attachment list;
			// reporting it twice would download it twice.
			name: "resume repeated as an attachment is not duplicated",
			payload: `{"id":1,"resumeFileId":10,"coverLetterFileId":11,
				"attachments":[{"id":10,"name":"cv.pdf"},{"id":11,"name":"letter.pdf"},{"id":20,"name":"extra.pdf"}]}`,
			want: []ApplicationFile{
				{ID: 10, Kind: FileResume},
				{ID: 11, Kind: FileCoverLetter},
				{ID: 20, Kind: FileAttachment, Name: "extra.pdf"},
			},
		},
		{
			name:    "nothing attached",
			payload: `{"id":1,"resumeFileId":null,"coverLetterFileId":null,"attachments":null}`,
			want:    nil,
		},
		{
			// A zero ID is not a file.
			name:    "zero ids ignored",
			payload: `{"id":1,"resumeFileId":0,"coverLetterFileId":0,"attachments":null}`,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var details ApplicationDetails
			if err := json.Unmarshal([]byte(tt.payload), &details); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got := details.Files()
			if len(got) != len(tt.want) {
				t.Fatalf("Files() = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i].ID != tt.want[i].ID || got[i].Kind != tt.want[i].Kind ||
					got[i].Name != tt.want[i].Name || got[i].URL != tt.want[i].URL {
					t.Errorf("Files()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDownloadApplicationFile(t *testing.T) {
	var path, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, accept = r.URL.Path, r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/pdf; charset=UTF-8")
		w.Write([]byte("%PDF-1.7 stub"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	meta, err := c.DownloadApplicationFile(context.Background(), 24732, 25625, &buf)
	if err != nil {
		t.Fatalf("DownloadApplicationFile: %v", err)
	}
	// This path is not in the published spec; it was verified against the live
	// API and is the only route to a file known only by resumeFileId.
	if want := "/api/v1/applicant_tracking/applications/24732/files/25625"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if accept != "*/*" {
		t.Errorf("Accept = %q, want */*", accept)
	}
	if buf.String() != "%PDF-1.7 stub" {
		t.Errorf("wrote %q, want the file bytes", buf.String())
	}
	if meta.ContentType != "application/pdf; charset=UTF-8" {
		t.Errorf("ContentType = %q, want the PDF type", meta.ContentType)
	}
}

func TestDownloadApplicationFileValidatesArguments(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var sink strings.Builder
	if _, err := c.DownloadApplicationFile(context.Background(), 0, 1, &sink); err == nil {
		t.Error("a zero application id succeeded, want an error")
	}
	if _, err := c.DownloadApplicationFile(context.Background(), 1, 0, &sink); err == nil {
		t.Error("a zero file id succeeded, want an error")
	}
}
