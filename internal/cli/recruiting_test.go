package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func TestRecruitingApplicationsDefaultsToEveryJobStatus(t *testing.T) {
	// The API filters to open jobs by default, which returns nothing for a
	// company whose openings are all filled or on hold.
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{"applications":[],"paginationComplete":true}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "recruiting", "applications"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if query.Get("jobStatusGroups") != "ALL" {
		t.Errorf("jobStatusGroups = %q, want ALL by default", query.Get("jobStatusGroups"))
	}
}

func TestRecruitingApplicationsJobScopesWithoutTheDefault(t *testing.T) {
	// Naming a job is already a scope, so the widening default would be
	// redundant and could change what the job filter returns.
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{"applications":[],"paginationComplete":true}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "recruiting", "applications", "--job", "937"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if query.Has("jobStatusGroups") {
		t.Errorf("jobStatusGroups = %q, want it omitted when a job is named", query.Get("jobStatusGroups"))
	}
	if query.Get("jobId") != "937" {
		t.Errorf("jobId = %q, want 937", query.Get("jobId"))
	}
}

func TestRecruitingApplicationsStatusGroupFlag(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{"applications":[],"paginationComplete":true}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "recruiting", "applications", "--status-group", "NEW"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// The two status filters must not collide on one parameter.
	if query.Get("applicationStatus") != "NEW" {
		t.Errorf("applicationStatus = %q, want NEW", query.Get("applicationStatus"))
	}
	if query.Has("applicationStatusId") {
		t.Errorf("applicationStatusId = %q, want it unset", query.Get("applicationStatusId"))
	}
}

func TestRecruitingRejectsUnknownEnums(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "job status", args: []string{"recruiting", "applications", "--job-status", "Opne"}, want: "Opne"},
		{name: "status group", args: []string{"recruiting", "applications", "--status-group", "BRAND_NEW"}, want: "BRAND_NEW"},
		{name: "sort by", args: []string{"recruiting", "applications", "--sort-by", "nope"}, want: "nope"},
		{name: "sort order", args: []string{"recruiting", "applications", "--sort-order", "sideways"}, want: "sideways"},
		{name: "jobs sort by", args: []string{"recruiting", "jobs", "--sort-by", "nope"}, want: "nope"},
		{name: "jobs job status", args: []string{"recruiting", "jobs", "--job-status", "Opne"}, want: "Opne"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errOut, code := run(t, jsonServer(t, `[]`), tt.args...)
			if code == 0 {
				t.Fatalf("exit code = 0, want a failure for %v", tt.args)
			}
			if !strings.Contains(errOut, tt.want) {
				t.Errorf("stderr = %q, want it to name %q", errOut, tt.want)
			}
		})
	}
}

func TestRecruitingApplicationsNewSince(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{"applications":[],"paginationComplete":true}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "recruiting", "applications", "--new-since", "7d"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// A relative age reads better than a timestamp for a recurring digest.
	since := query.Get("newSince")
	if !strings.HasSuffix(since, "Z") || len(since) != len("2026-09-01T00:00:00Z") {
		t.Errorf("newSince = %q, want an RFC 3339 UTC timestamp", since)
	}
}

func TestRecruitingApplicationsRejectsBadNewSince(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "recruiting", "applications", "--new-since", "last tuesday")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an unparseable time")
	}
	if !strings.Contains(errOut, "--new-since") {
		t.Errorf("stderr = %q, want it to name the flag", errOut)
	}
}

func TestRecruitingJobsRendersCountsAndNulls(t *testing.T) {
	out, errOut, code := run(t, jsonServer(t, jobsFixture), "recruiting", "jobs")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "Software Developer") {
		t.Fatalf("output lost the job title:\n%s", out)
	}
	if !strings.Contains(out, "Alex Morgan") {
		t.Errorf("output = %q, want the hiring lead's name", out)
	}
	// The posted date is trimmed to a date so the column stays narrow.
	if !strings.Contains(out, "2026-08-25") || strings.Contains(out, "2026-08-25T") {
		t.Errorf("output = %q, want the posted date without a time", out)
	}
	// A job with no department, location or hiring lead must still render.
	if !strings.Contains(out, "Contract Role") {
		t.Errorf("output = %q, want the opening with null relationships", out)
	}
}

// jobsFixture mirrors the live shape, including the nulls an opening can carry.
const jobsFixture = `[{
	"id": 937, "title": {"id": null, "label": "Software Developer"},
	"postedDate": "2026-08-25T14:00:00+00:00",
	"location": {"id": 5, "label": "Springfield", "address": {}},
	"department": {"id": 9, "label": "Engineering"},
	"status": {"id": 3, "label": "On Hold"},
	"hiringLead": {"employeeId": 208, "firstName": "Alex", "lastName": "Morgan"},
	"newApplicantsCount": 403, "activeApplicantsCount": 416, "totalApplicantsCount": 416,
	"postingUrl": "https://acme.bamboohr.com/careers/937"
}, {
	"id": 400, "title": {"id": 7, "label": "Contract Role"},
	"postedDate": "2026-01-02T00:00:00+00:00",
	"location": null, "department": null, "hiringLead": null,
	"status": {"id": 1, "label": "Draft"},
	"newApplicantsCount": 0, "activeApplicantsCount": 0, "totalApplicantsCount": 0,
	"postingUrl": null
}]`

func TestRecruitingStatusesHidesDisabled(t *testing.T) {
	body := `[
		{"id":"1","code":"NEW","name":"New","enabled":true,"manageable":false},
		{"id":"6","code":"INTERVIEW","name":"Interviewed","enabled":false,"manageable":false}
	]`

	out, errOut, code := run(t, jsonServer(t, body), "recruiting", "statuses")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if strings.Contains(out, "Interviewed") {
		t.Errorf("output = %q, want disabled statuses hidden by default", out)
	}
	if !strings.Contains(errOut, "1 disabled status") {
		t.Errorf("stderr = %q, want a count of what was hidden", errOut)
	}

	out, _, code = run(t, jsonServer(t, body), "recruiting", "statuses", "--disabled")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "Interviewed") {
		t.Errorf("output = %q, want --disabled to include them", out)
	}
}

func TestRecruitingSetStatus(t *testing.T) {
	var body map[string]int
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"type":"positionApplicantStatus","id":9}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "recruiting", "set-status", "4821", "--status", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if want := "/api/v1/applicant_tracking/applications/4821/status"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if body["status"] != 2 {
		t.Errorf("body = %v, want status 2", body)
	}
	// Writes report to stderr so stdout stays clean for piping.
	if !strings.Contains(errOut, "moved to status 2") {
		t.Errorf("stderr = %q, want confirmation of the change", errOut)
	}
}

func TestRecruitingSetStatusRequiresStatus(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "recruiting", "set-status", "4821")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure without --status")
	}
	if !strings.Contains(errOut, "status") {
		t.Errorf("stderr = %q, want it to ask for the status", errOut)
	}
}

func TestRecruitingComment(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"type":"comment","id":11}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "recruiting", "comment", "4821", "--message", "Strong round")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if body["comment"] != "Strong round" {
		t.Errorf("comment = %q, want the message", body["comment"])
	}
}

func TestRecruitingCommentRejectsBlank(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "recruiting", "comment", "4821", "--message", "   ")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an empty message")
	}
	if !strings.Contains(errOut, "empty") {
		t.Errorf("stderr = %q, want it to say the message is empty", errOut)
	}
}

func TestValidateEnum(t *testing.T) {
	// An empty value means "not set", which must not be rejected.
	if err := validateEnum("--sort-by", "", "count", "title"); err != nil {
		t.Errorf("an unset flag was rejected: %v", err)
	}
	if err := validateEnum("--sort-by", "count", "count", "title"); err != nil {
		t.Errorf("a valid value was rejected: %v", err)
	}
	// Case is tolerated, since the API accepts ASC and asc alike.
	if err := validateEnum("--sort-order", "desc", "ASC", "DESC"); err != nil {
		t.Errorf("a differently cased value was rejected: %v", err)
	}
	err := validateEnum("--sort-by", "nope", "count", "title")
	if err == nil {
		t.Fatal("an unknown value was accepted")
	}
	if !strings.Contains(err.Error(), "--sort-by") || !strings.Contains(err.Error(), "count, title") {
		t.Errorf("error = %q, want the flag and the allowed values", err)
	}
}

func TestDateOnly(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "2026-08-25T14:00:00+00:00", want: "2026-08-25"},
		{in: "2026-08-25", want: "2026-08-25"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := dateOnly(tt.in); got != tt.want {
			t.Errorf("dateOnly(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// applicationWithResume is the live shape: a resume ID and a null attachment
// list.
const applicationWithResume = `{"id":24732,"appliedDate":"2022-01-16T21:32:50+00:00",
	"status":{"id":3,"label":"Schedule Phone Screen"},
	"applicant":{"id":9,"firstName":"Ada","lastName":"Lovelace","email":"ada@example.test"},
	"resumeFileId":25625,"coverLetterFileId":null,"attachments":null}`

// atsServer answers the application lookup and then the file download.
func atsServer(t *testing.T, details, contentType, body string, disposition string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/files/") {
			w.Header().Set("Content-Type", contentType)
			if disposition != "" {
				w.Header().Set("Content-Disposition", disposition)
			}
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(details))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRecruitingFilesListsResume(t *testing.T) {
	out, errOut, code := run(t, jsonServer(t, applicationWithResume), "recruiting", "files", "24732")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "25625") || !strings.Contains(out, "resume") {
		t.Errorf("output = %q, want the resume listed with its ID", out)
	}
}

func TestRecruitingFilesNotesWhenEmpty(t *testing.T) {
	body := `{"id":1,"resumeFileId":null,"coverLetterFileId":null,"attachments":null,
		"applicant":{"id":1},"status":{"id":1,"label":"New"}}`
	_, errOut, code := run(t, jsonServer(t, body), "recruiting", "files", "1")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "no resume") {
		t.Errorf("stderr = %q, want a note that nothing is attached", errOut)
	}
}

func TestRecruitingDownloadNamesFileFromRoleAndContentType(t *testing.T) {
	// Most of these downloads send no filename, so the name has to come from
	// the application, the file's role and the content type.
	srv := atsServer(t, applicationWithResume, "application/pdf; charset=UTF-8", "%PDF-1.7 stub", "")
	dir := t.TempDir()

	_, errOut, code := run(t, srv, "recruiting", "download", "24732", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	want := filepath.Join(dir, "application-24732-resume.pdf")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", want, err)
	}
	if string(data) != "%PDF-1.7 stub" {
		t.Errorf("contents = %q, want the file bytes", data)
	}
	// Candidate documents should not be group- or world-readable.
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}
}

func TestRecruitingDownloadPrefixesServerSuppliedName(t *testing.T) {
	// Two candidates both uploading "Resume.pdf" would collide without the
	// application prefix.
	srv := atsServer(t, applicationWithResume, "image/png", "\x89PNG stub",
		`inline; filename='Ada_Resume_preview.png'`)
	dir := t.TempDir()

	_, errOut, code := run(t, srv, "recruiting", "download", "24732", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	want := filepath.Join(dir, "application-24732-Ada_Resume_preview.png")
	if _, err := os.Stat(want); err != nil {
		entries, _ := os.ReadDir(dir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want %s, found %v", filepath.Base(want), names)
	}
}

func TestRecruitingDownloadRefusesToClobber(t *testing.T) {
	srv := atsServer(t, applicationWithResume, "application/pdf", "%PDF stub", "")
	dir := t.TempDir()

	if _, errOut, code := run(t, srv, "recruiting", "download", "24732", "--dir", dir); code != 0 {
		t.Fatalf("first download failed: %d; stderr: %s", code, errOut)
	}
	// A generated name is a guess, so replacing a file is not the CLI's call.
	_, errOut, code := run(t, srv, "recruiting", "download", "24732", "--dir", dir)
	if code == 0 {
		t.Fatal("exit code = 0, want a refusal to overwrite")
	}
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("stderr = %q, want it to say the file exists", errOut)
	}
}

func TestRecruitingDownloadOutToStdout(t *testing.T) {
	srv := atsServer(t, applicationWithResume, "application/pdf", "%PDF-1.7 piped", "")

	out, errOut, code := run(t, srv, "recruiting", "download", "24732", "--resume", "--out", "-")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if out != "%PDF-1.7 piped" {
		t.Errorf("stdout = %q, want the file streamed verbatim", out)
	}
}

func TestRecruitingDownloadRejectsConflictingFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "file and resume",
			args: []string{"recruiting", "download", "24732", "--file", "1", "--resume"},
			want: "use one or the other",
		},
		{
			name: "out and dir",
			args: []string{"recruiting", "download", "24732", "--out", "x.pdf", "--dir", "y"},
			want: "use one or the other",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errOut, code := run(t, jsonServer(t, applicationWithResume), tt.args...)
			if code == 0 {
				t.Fatalf("exit code = 0, want a failure for %v", tt.args)
			}
			if !strings.Contains(errOut, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tt.want)
			}
		})
	}
}

func TestRecruitingDownloadOutWithSeveralFilesIsRejected(t *testing.T) {
	// --out names one file, so it cannot stand in for a whole set.
	body := `{"id":1,"resumeFileId":10,"coverLetterFileId":11,"attachments":null,
		"applicant":{"id":1},"status":{"id":1,"label":"New"}}`
	_, errOut, code := run(t, jsonServer(t, body),
		"recruiting", "download", "1", "--out", "one.pdf")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when --out would receive two files")
	}
	if !strings.Contains(errOut, "2 are being downloaded") {
		t.Errorf("stderr = %q, want it to say how many files there are", errOut)
	}
}

func TestRecruitingDownloadNoFiles(t *testing.T) {
	body := `{"id":1,"resumeFileId":null,"coverLetterFileId":null,"attachments":null,
		"applicant":{"id":1},"status":{"id":1,"label":"New"}}`
	_, errOut, code := run(t, jsonServer(t, body), "recruiting", "download", "1")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when there is nothing to download")
	}
	if !strings.Contains(errOut, "no resume") {
		t.Errorf("stderr = %q, want it to say nothing is attached", errOut)
	}
}

func TestRecruitingDownloadResumeMissing(t *testing.T) {
	body := `{"id":1,"resumeFileId":null,"coverLetterFileId":11,"attachments":null,
		"applicant":{"id":1},"status":{"id":1,"label":"New"}}`
	_, errOut, code := run(t, jsonServer(t, body), "recruiting", "download", "1", "--resume")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when the application has no resume")
	}
	if !strings.Contains(errOut, "has no resume") {
		t.Errorf("stderr = %q, want it to say the resume is missing", errOut)
	}
}

func TestRecruitingDownloadByFileIDSkipsTheLookup(t *testing.T) {
	// Naming a file by ID should not need the application fetched, which also
	// lets a caller fetch one the listing did not report.
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF stub"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, errOut, code := run(t, srv, "recruiting", "download", "24732", "--file", "999", "--dir", dir); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if len(paths) != 1 {
		t.Fatalf("made %d requests, want 1: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "/files/999") {
		t.Errorf("requested %q, want the file endpoint", paths[0])
	}
}

func TestRecruitingDownloadCreatesTheDirectory(t *testing.T) {
	srv := atsServer(t, applicationWithResume, "application/pdf", "%PDF stub", "")
	dir := filepath.Join(t.TempDir(), "candidates", "2026")

	if _, errOut, code := run(t, srv, "recruiting", "download", "24732", "--dir", dir); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "application-24732-resume.pdf")); err != nil {
		t.Errorf("the named directory was not created: %v", err)
	}
}

func TestApplicationFileBase(t *testing.T) {
	tests := []struct {
		name string
		file bamboohr.ApplicationFile
		want string
	}{
		{name: "resume", file: bamboohr.ApplicationFile{ID: 1, Kind: bamboohr.FileResume}, want: "resume"},
		{
			name: "cover letter",
			file: bamboohr.ApplicationFile{ID: 2, Kind: bamboohr.FileCoverLetter},
			want: "cover-letter",
		},
		{
			name: "named attachment",
			file: bamboohr.ApplicationFile{ID: 3, Kind: bamboohr.FileAttachment, Name: "portfolio.pdf"},
			want: "portfolio.pdf",
		},
		{
			name: "unnamed attachment",
			file: bamboohr.ApplicationFile{ID: 4, Kind: bamboohr.FileAttachment},
			want: "file-4",
		},
		{
			// A server-supplied name must not be able to escape the directory.
			name: "traversal stripped",
			file: bamboohr.ApplicationFile{ID: 5, Kind: bamboohr.FileAttachment, Name: "../../etc/passwd"},
			want: "passwd",
		},
		{
			name: "dots only",
			file: bamboohr.ApplicationFile{ID: 6, Kind: bamboohr.FileAttachment, Name: ".."},
			want: "file-6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applicationFileBase(tt.file); got != tt.want {
				t.Errorf("applicationFileBase = %q, want %q", got, tt.want)
			}
		})
	}
}
