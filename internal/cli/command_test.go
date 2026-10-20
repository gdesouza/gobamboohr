package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the CLI against srv and returns stdout, stderr and the exit
// code, so a test can assert on what a user would actually see.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, string, int) {
	t.Helper()
	clearCredentialEnv(t)
	t.Setenv(envSubdomain, "acme")
	t.Setenv(envAPIKey, "test-key")
	t.Setenv(envConfig, filepath.Join(t.TempDir(), "absent.json"))
	if srv != nil {
		t.Setenv(envBaseURL, srv.URL)
	}

	var out, errOut strings.Builder
	code := Execute("test", args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// jsonServer answers every request with the given body.
func jsonServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVersionFlag(t *testing.T) {
	out, _, code := run(t, nil, "--version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("output = %q, want the injected build version", out)
	}
}

func TestHelpNeedsNoCredentials(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv(envConfig, filepath.Join(t.TempDir(), "absent.json"))

	var out, errOut strings.Builder
	if code := Execute("test", []string{"--help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "employee") {
		t.Errorf("help did not list the commands:\n%s", out.String())
	}
}

func TestMissingCredentialsFailsClearly(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv(envConfig, filepath.Join(t.TempDir(), "absent.json"))

	var out, errOut strings.Builder
	code := Execute("test", []string{"meta", "company"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit code = 0, want a failure without credentials")
	}
	if !strings.Contains(errOut.String(), "subdomain") {
		t.Errorf("stderr = %q, want it to say what is missing", errOut.String())
	}
}

func TestBadOutputFormatIsRejected(t *testing.T) {
	_, errOut, code := run(t, nil, "--output", "yaml", "meta", "company")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an unknown format")
	}
	if !strings.Contains(errOut, "yaml") {
		t.Errorf("stderr = %q, want it to name the bad format", errOut)
	}
}

func TestMetaCompanyTable(t *testing.T) {
	srv := jsonServer(t, `{"id":7,"name":"Acme","domain":"acme","baseApiUrl":"https://api.example/acme"}`)

	out, errOut, code := run(t, srv, "meta", "company")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "Acme") {
		t.Errorf("output = %q, want the company name", out)
	}
}

func TestEmployeeGetJSON(t *testing.T) {
	srv := jsonServer(t, `{"id":"123","firstName":"Ada","lastName":"Lovelace","customField9":"Robotics"}`)

	out, errOut, code := run(t, srv, "--output", "json", "employee", "get", "123", "--fields", "firstName,lastName")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	// JSON output must keep fields the table would not have a column for.
	if got["customField9"] != "Robotics" {
		t.Errorf("customField9 = %v, want Robotics", got["customField9"])
	}
}

func TestEmployeeGetSendsFields(t *testing.T) {
	var fields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields = r.URL.Query().Get("fields")
		w.Write([]byte(`{"id":"123"}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "employee", "get", "123", "--fields", "firstName", "--fields", "lastName")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// A repeated flag and a comma-separated list must reach the API the same
	// way.
	if fields != "firstName,lastName" {
		t.Errorf("fields = %q, want firstName,lastName", fields)
	}
}

func TestEmployeeListCSV(t *testing.T) {
	srv := jsonServer(t, `{"data":[
		{"employeeId":"1","firstName":"Ada","lastName":"Lovelace","status":"Active"},
		{"employeeId":"2","firstName":"Grace","lastName":"Hopper","status":"Active"}
	],"meta":{"total":2,"page":{"limit":250}}}`)

	out, errOut, code := run(t, srv, "--output", "csv", "employee", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d CSV lines, want a header and two rows:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "employeeId") {
		t.Errorf("header = %q, want it to include employeeId", lines[0])
	}
}

func TestEmployeeListFilterFlag(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`{"data":[],"meta":{"total":0,"page":{}}}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "employee", "list", "--filter", "status=active", "--filter", "departmentName=Robotics")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	for _, want := range []string{"filter%5Bstatus%5D=active", "filter%5BdepartmentName%5D=Robotics"} {
		if !strings.Contains(query, want) {
			t.Errorf("query = %q, want it to contain %q", query, want)
		}
	}
}

func TestEmployeeListRejectsMalformedFilter(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "employee", "list", "--filter", "status")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a filter without =")
	}
	if !strings.Contains(errOut, "--filter") {
		t.Errorf("stderr = %q, want it to name the flag", errOut)
	}
}

func TestTimeOffWhosOutDefaultsDates(t *testing.T) {
	var start, end string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start = r.URL.Query().Get("start")
		end = r.URL.Query().Get("end")
		w.Write([]byte(`{"data":[{"id":"1","employeeId":42,"timeOffTypeId":7,"start":"2026-05-04","end":"2026-05-08"}],
			"persons":{"42":{"id":42,"displayName":"Jane Doe"}},
			"meta":{"page":1,"pageSize":100,"totalPages":1,"totalItems":1}}`))
	}))
	defer srv.Close()

	out, errOut, code := run(t, srv, "timeoff", "whos-out")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// Both bounds are required by the API, so the bare command must supply
	// them.
	if start == "" || end == "" {
		t.Errorf("start/end = %q/%q, want defaults", start, end)
	}
	if !strings.Contains(out, "Jane Doe") {
		t.Errorf("output = %q, want the employee name", out)
	}
	// The window is inclusive: 4 May to 8 May is five days.
	if !strings.Contains(out, "5") {
		t.Errorf("output = %q, want the day count", out)
	}
}

func TestTimeOffWhosOutRejectsBadDate(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "timeoff", "whos-out", "--start", "01/10/2026")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a non-ISO date")
	}
	if !strings.Contains(errOut, "--start") {
		t.Errorf("stderr = %q, want it to name the flag", errOut)
	}
}

func TestTimeOffRequestsStatusFlag(t *testing.T) {
	var filter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter = r.URL.Query().Get("filter")
		w.Write([]byte(`{"data":[],"meta":{"page":1,"totalPages":1}}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "timeoff", "requests", "--status", "approved,denied")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// The API's statuses are upper case; the flag accepts either.
	if want := "status in ('APPROVED', 'DENIED')"; filter != want {
		t.Errorf("filter = %q, want %q", filter, want)
	}
}

func TestTimeOffRequestsRejectsUnknownStatus(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "timeoff", "requests", "--status", "pending")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an unknown status")
	}
	if !strings.Contains(errOut, "pending") {
		t.Errorf("stderr = %q, want it to name the bad status", errOut)
	}
}

func TestTimeOffCreateRejectsAmountAndDaily(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`),
		"timeoff", "create", "--employee", "1", "--category", "2",
		"--start", "2026-01-01", "--end", "2026-01-02",
		"--amount", "16", "--daily", "2026-01-01=8")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when both amount forms are given")
	}
	if !strings.Contains(errOut, "exactly one") {
		t.Errorf("stderr = %q, want it to explain the conflict", errOut)
	}
}

func TestTimeOffCreateSendsDailyAmounts(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":9,"status":"REQUESTED"}`))
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv,
		"timeoff", "create", "--employee", "1", "--category", "22",
		"--start", "2026-01-01", "--end", "2026-01-02",
		"--daily", "2026-01-01=8", "--daily", "2026-01-02=4")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	daily, ok := body["dailyAmounts"].([]any)
	if !ok || len(daily) != 2 {
		t.Fatalf("dailyAmounts = %v, want two entries", body["dailyAmounts"])
	}
	first := daily[0].(map[string]any)
	if first["date"] != "2026-01-01" || first["amount"] != 8.0 {
		t.Errorf("first daily amount = %v, want 2026-01-01 at 8", first)
	}
}

func TestTimeOffCreateRejectsMalformedDaily(t *testing.T) {
	for _, arg := range []string{"2026-01-01", "01/01/2026=8", "2026-01-01=lots"} {
		_, errOut, code := run(t, jsonServer(t, `{}`),
			"timeoff", "create", "--employee", "1", "--category", "2",
			"--start", "2026-01-01", "--end", "2026-01-02", "--daily", arg)
		if code == 0 {
			t.Errorf("--daily %q was accepted, want a failure", arg)
		}
		if !strings.Contains(errOut, "--daily") {
			t.Errorf("--daily %q: stderr = %q, want it to name the flag", arg, errOut)
		}
	}
}

func TestReportGetUsesReportColumns(t *testing.T) {
	srv := jsonServer(t, `{
		"fields":[{"name":"lastName","label":"Surname"},{"name":"department","label":null}],
		"data":[{"lastName":"Lovelace","department":"Robotics"}],
		"pagination":{"total_records":1,"current_page":1,"total_pages":1}
	}`)

	out, errOut, code := run(t, srv, "report", "get", "42")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// The report's own heading is used, including a customer renaming.
	if !strings.Contains(lines[0], "SURNAME") {
		t.Errorf("header = %q, want the report's renamed column", lines[0])
	}
	if !strings.Contains(out, "Lovelace") {
		t.Errorf("output = %q, want the row", out)
	}
}

func TestReportGetRejectsBadID(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "report", "get", "abc")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a non-numeric report id")
	}
	if !strings.Contains(errOut, "report id") {
		t.Errorf("stderr = %q, want it to name the argument", errOut)
	}
}

func TestMetaFieldsHidesDeprecatedByDefault(t *testing.T) {
	body := `[{"id":1,"name":"Current","type":"text","alias":"current"},
		{"id":2,"name":"Old","type":"text","alias":"old","deprecated":true}]`

	out, errOut, code := run(t, jsonServer(t, body), "meta", "fields")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if strings.Contains(out, "Old") {
		t.Errorf("output = %q, want deprecated fields hidden by default", out)
	}

	out, _, code = run(t, jsonServer(t, body), "meta", "fields", "--deprecated")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "Old") {
		t.Errorf("output = %q, want --deprecated to include them", out)
	}
}

func TestAPIErrorExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantCode int
		wantHint string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: 3, wantHint: "API key"},
		{name: "forbidden", status: http.StatusForbidden, wantCode: 4, wantHint: "permission"},
		{name: "not found", status: http.StatusNotFound, wantCode: 5},
		{name: "bad request", status: http.StatusBadRequest, wantCode: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			// Retries are disabled so a 5xx test would not wait.
			_, errOut, code := run(t, srv, "--retries", "0", "meta", "company")
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %s)", code, tt.wantCode, errOut)
			}
			if tt.wantHint != "" && !strings.Contains(errOut, tt.wantHint) {
				t.Errorf("stderr = %q, want a hint mentioning %q", errOut, tt.wantHint)
			}
		})
	}
}

func TestVerboseGoesToStderr(t *testing.T) {
	srv := jsonServer(t, `{"data":[],"meta":{"total":0,"page":{"nextCursor":""}}}`)

	out, errOut, code := run(t, srv, "--verbose", "employee", "list", "--all")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "fetching all employees") {
		t.Errorf("stderr = %q, want the progress note", errOut)
	}
	// Progress must never contaminate the parseable output.
	if strings.Contains(out, "fetching") {
		t.Errorf("stdout = %q, want progress kept on stderr", out)
	}
}

func TestEmployeeFileDownloadWritesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="offer.pdf"`)
		w.Write([]byte("pdf-bytes"))
	}))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "saved.pdf")
	_, errOut, code := run(t, srv, "employee", "file", "12", "5", "--file", target)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "pdf-bytes" {
		t.Errorf("file contents = %q, want pdf-bytes", data)
	}
	if !strings.Contains(errOut, "saved.pdf") {
		t.Errorf("stderr = %q, want a note naming the file written", errOut)
	}
}

func TestFileDownloadToStdout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("raw-bytes"))
	}))
	defer srv.Close()

	out, errOut, code := run(t, srv, "files", "get", "5", "--file", "-")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if out != "raw-bytes" {
		t.Errorf("stdout = %q, want the file streamed verbatim", out)
	}
}

func TestFailedDownloadLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "missing.pdf")
	_, _, code := run(t, srv, "--retries", "0", "files", "get", "5", "--file", target)
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for a 404")
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("a truncated file was left behind after a failed download")
	}

	// The temporary file must be cleaned up too.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("directory holds %d entries, want none: %v", len(entries), entries)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	_, errOut, code := run(t, nil, "employee", "frobnicate")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an unknown subcommand")
	}
	if !strings.Contains(errOut, "frobnicate") {
		t.Errorf("stderr = %q, want it to name the unknown command", errOut)
	}
}

func TestEmployeeListDoesNotPrintPhotoURL(t *testing.T) {
	// The live list endpoint returns photoUrl in the default field set as a
	// signed URL hundreds of characters long. It belongs in --output json, not
	// in a table.
	const photoURL = "https://images3.bamboohr.com/1/photos/1-1-4.jpg?Policy=verylongsignedvalue"
	body := `{"data":[{"employeeId":"1","firstName":"Ada","lastName":"Lovelace","status":"Active",
		"photoUrl":"` + photoURL + `","_restrictedFields":[]}],
		"meta":{"total":1,"page":{"limit":250}}}`

	for _, format := range []string{"table", "csv"} {
		out, errOut, code := run(t, jsonServer(t, body), "--output", format, "employee", "list")
		if code != 0 {
			t.Fatalf("%s: exit code = %d, want 0; stderr: %s", format, code, errOut)
		}
		if strings.Contains(out, photoURL) {
			t.Errorf("%s output contains the photo URL:\n%s", format, out)
		}
		if !strings.Contains(out, "Lovelace") {
			t.Errorf("%s output lost the useful columns:\n%s", format, out)
		}
	}

	// JSON is the full response, so it keeps the URL.
	out, _, code := run(t, jsonServer(t, body), "--output", "json", "employee", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, photoURL) {
		t.Error("json output dropped the photo URL, want the full response kept")
	}
}

func TestEmployeeDirectoryDropsUIColumns(t *testing.T) {
	// The directory returns every field it is configured with, plus the web
	// UI's photo flags, and a company that leaves a field unused should not
	// pay for its column.
	body := `{"fields":[{"id":"displayName","type":"text","name":"Display Name"}],
		"employees":[{"id":"1","displayName":"Ada Lovelace","workEmail":"ada@x.test",
			"division":"","preferredName":"","photoUrl":"https://x.test/a.jpg",
			"photoUploaded":true,"canUploadPhoto":0}]}`

	out, errOut, code := run(t, jsonServer(t, body), "employee", "directory")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	header := strings.SplitN(out, "\n", 2)[0]
	for _, unwanted := range []string{"PHOTOURL", "PHOTOUPLOADED", "CANUPLOADPHOTO", "DIVISION", "PREFERREDNAME"} {
		if strings.Contains(header, unwanted) {
			t.Errorf("header %q still has %s", header, unwanted)
		}
	}
	for _, wanted := range []string{"DISPLAYNAME", "WORKEMAIL"} {
		if !strings.Contains(header, wanted) {
			t.Errorf("header %q lost %s", header, wanted)
		}
	}
}

func TestEmployeeListDefaultsToActive(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("filter[status]")
		w.Write([]byte(`{"data":[],"meta":{"total":0,"page":{}}}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "employee", "list"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	// Without this the listing is mostly rows the key cannot read.
	if got != "active" {
		t.Errorf("filter[status] = %q, want active by default", got)
	}
}

func TestEmployeeListStatusFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "inactive", args: []string{"--status", "inactive"}, want: "inactive"},
		// "all" must send no status filter, or the masked records stay hidden.
		{name: "all", args: []string{"--status", "all"}, want: ""},
		{name: "via raw filter", args: []string{"--filter", "status=inactive"}, want: "inactive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var present bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("filter[status]")
				present = r.URL.Query().Has("filter[status]")
				w.Write([]byte(`{"data":[],"meta":{"total":0,"page":{}}}`))
			}))
			defer srv.Close()

			args := append([]string{"employee", "list"}, tt.args...)
			if _, errOut, code := run(t, srv, args...); code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
			}
			if tt.want == "" {
				if present {
					t.Errorf("filter[status] = %q, want it omitted entirely", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("filter[status] = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmployeeListRejectsConflictingStatus(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`),
		"employee", "list", "--status", "active", "--filter", "status=inactive")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when the status is set twice")
	}
	if !strings.Contains(errOut, "use one or the other") {
		t.Errorf("stderr = %q, want it to explain the conflict", errOut)
	}
}

func TestEmployeeListRejectsUnknownStatus(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`), "employee", "list", "--status", "pending")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure for an unknown status")
	}
	if !strings.Contains(errOut, "pending") {
		t.Errorf("stderr = %q, want it to name the bad status", errOut)
	}
}

func TestEmployeeListNotesBlankRows(t *testing.T) {
	// A record the key cannot read still occupies a row. The note is what
	// stops it looking like a bug in the tool.
	body := `{"data":[
		{"employeeId":"1","firstName":null,"lastName":null,"jobTitleName":null,"status":null,
		 "_restrictedFields":["firstName","lastName","jobTitleName","status"]},
		{"employeeId":"3","firstName":"Ada","lastName":"Lovelace","status":"Active","_restrictedFields":[]}
	],"meta":{"total":2,"page":{}}}`

	out, errOut, code := run(t, jsonServer(t, body), "employee", "list", "--status", "all")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "1 of 2 rows") {
		t.Errorf("stderr = %q, want a note counting the blank rows", errOut)
	}
	// The note belongs on stderr so it cannot corrupt piped output.
	if strings.Contains(out, "note:") {
		t.Errorf("stdout = %q, want the note kept on stderr", out)
	}
}

func TestEmployeeListNoNoteWhenRowsAreReadable(t *testing.T) {
	// A restriction on a field the table does not show is invisible, so it must
	// not trigger the note.
	body := `{"data":[{"employeeId":"42","firstName":"Luke","lastName":"Zhang",
		"jobTitleName":"Developer","status":"Inactive","preferredName":null,
		"_restrictedFields":["preferredName"]}],"meta":{"total":1,"page":{}}}`

	_, errOut, code := run(t, jsonServer(t, body), "employee", "list", "--status", "inactive")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if strings.Contains(errOut, "note:") {
		t.Errorf("stderr = %q, want no note when every row is readable", errOut)
	}
}

func TestEmployeeGetRequestsDefaultFields(t *testing.T) {
	// A request naming no fields comes back with nothing but the ID, which is
	// a dead end for anyone running the command by hand.
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("fields")
		w.Write([]byte(`{"id":"208","firstName":"Alex","lastName":"Morgan"}`))
	}))
	defer srv.Close()

	out, errOut, code := run(t, srv, "employee", "get", "208")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if got == "" {
		t.Fatal("fields was empty, want a default selection")
	}
	for _, want := range []string{"firstName", "lastName", "jobTitle", "workEmail"} {
		if !strings.Contains(got, want) {
			t.Errorf("fields = %q, want it to include %q", got, want)
		}
	}
	if !strings.Contains(out, "Morgan") {
		t.Errorf("output = %q, want the record rendered", out)
	}
}

func TestEmployeeGetFieldsFlagOverridesDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("fields")
		w.Write([]byte(`{"id":"208","hireDate":"2025-05-20"}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "employee", "get", "208", "--fields", "hireDate"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if got != "hireDate" {
		t.Errorf("fields = %q, want only the requested field", got)
	}
}

func TestEmployeeGetAllFields(t *testing.T) {
	// --all-fields reads the account's catalogue and asks for everything that
	// has an API alias; a field addressable only by numeric ID is skipped,
	// since the catalogue holds more of those than a request may carry.
	var fields string
	var sawCatalogue bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/meta/fields":
			sawCatalogue = true
			w.Write([]byte(`[
				{"id":1,"name":"First Name","type":"text","alias":"firstName"},
				{"id":2,"name":"Pay Rate","type":"currency","alias":"payRate"},
				{"id":3,"name":"No Alias","type":"text"},
				{"id":4,"name":"Legacy","type":"text","alias":"legacy","deprecated":true},
				{"id":5,"name":"Dupe","type":"text","alias":"payRate"}
			]`))
		default:
			fields = r.URL.Query().Get("fields")
			w.Write([]byte(`{"id":"208","firstName":"Alex","payRate":"1.00 CAD"}`))
		}
	}))
	defer srv.Close()

	_, errOut, code := run(t, srv, "employee", "get", "208", "--all-fields")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !sawCatalogue {
		t.Error("--all-fields did not read the field catalogue")
	}
	if fields != "firstName,payRate" {
		t.Errorf("fields = %q, want the aliased, deduplicated, non-deprecated names", fields)
	}
}

func TestEmployeeGetRejectsConflictingFieldFlags(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{}`),
		"employee", "get", "208", "--all-fields", "--fields", "firstName")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure when both choose the fields")
	}
	if !strings.Contains(errOut, "use one or the other") {
		t.Errorf("stderr = %q, want it to explain the conflict", errOut)
	}
}

func TestEmployeeGetNotesWhenEveryFieldWasDropped(t *testing.T) {
	// Fields the key may not read are dropped without an error, so a record
	// carrying only its ID is what a permission boundary looks like here.
	out, errOut, code := run(t, jsonServer(t, `{"id":"1"}`), "employee", "get", "1")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "none of the") {
		t.Errorf("stderr = %q, want a note explaining the empty record", errOut)
	}
	if strings.Contains(out, "note:") {
		t.Errorf("stdout = %q, want the note kept on stderr", out)
	}
}

func TestEmployeeGetNoNoteWhenFieldsCameBack(t *testing.T) {
	_, errOut, code := run(t, jsonServer(t, `{"id":"208","firstName":"Alex"}`), "employee", "get", "208")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if strings.Contains(errOut, "note:") {
		t.Errorf("stderr = %q, want no note when a field came back", errOut)
	}
}

func TestEmployeeGetBlanksZeroDate(t *testing.T) {
	out, errOut, code := run(t, jsonServer(t, `{"id":"208","terminationDate":"0000-00-00"}`),
		"employee", "get", "208", "--fields", "terminationDate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if strings.Contains(out, "0000-00-00") {
		t.Errorf("output = %q, want an unset date shown as empty", out)
	}
}

// searchServer answers the directory request a search makes.
func searchServer(t *testing.T) *httptest.Server {
	t.Helper()
	return jsonServer(t, `{"fields":[{"id":"displayName","type":"text","name":"Display Name"}],
		"employees":`+directoryFixture+`}`)
}

func TestEmployeeSearchEndToEnd(t *testing.T) {
	out, errOut, code := run(t, searchServer(t), "employee", "search", "morgan")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header and two matches:\n%s", len(lines), out)
	}
	// Four fixed columns, and no others.
	header := strings.Fields(lines[0])
	want := []string{"ID", "DISPLAYNAME", "LOCATION", "SUPERVISOR"}
	if strings.Join(header, ",") != strings.Join(want, ",") {
		t.Errorf("header = %v, want %v", header, want)
	}
	// The person named Morgan comes before the report whose manager is.
	if !strings.Contains(lines[1], "208") {
		t.Errorf("first row = %q, want the name match ranked first", lines[1])
	}
	if !strings.Contains(lines[1], "Pat Rivera") {
		t.Errorf("first row = %q, want the supervisor column populated", lines[1])
	}
}

func TestEmployeeSearchMatchedFlag(t *testing.T) {
	out, errOut, code := run(t, searchServer(t), "employee", "search", "morgan", "--matched")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "MATCHED") {
		t.Errorf("output = %q, want the matched column with --matched", out)
	}
	if !strings.Contains(out, "supervisor") {
		t.Errorf("output = %q, want the supervisor-only hit explained", out)
	}
}

func TestEmployeeSearchUsesOneRequest(t *testing.T) {
	// Matching happens locally because the list endpoint ANDs its per-field
	// filters and so cannot match across fields.
	var calls int
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		paths = append(paths, r.URL.Path)
		w.Write([]byte(`{"employees":` + directoryFixture + `}`))
	}))
	defer srv.Close()

	if _, errOut, code := run(t, srv, "employee", "search", "morgan"); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if calls != 1 {
		t.Errorf("made %d requests, want 1: %v", calls, paths)
	}
	if paths[0] != "/api/v1/employees/directory" {
		t.Errorf("requested %q, want the directory", paths[0])
	}
}

func TestEmployeeSearchRequiresATerm(t *testing.T) {
	_, errOut, code := run(t, searchServer(t), "employee", "search")
	if code == 0 {
		t.Fatal("exit code = 0, want a failure with no search term")
	}
	if !strings.Contains(errOut, "arg") {
		t.Errorf("stderr = %q, want it to say an argument is required", errOut)
	}
}

func TestEmployeeSearchBlankTermIsRejected(t *testing.T) {
	// An empty term must not fall through to matching everything.
	out, errOut, code := run(t, searchServer(t), "employee", "search", "   ")
	if code == 0 {
		t.Fatalf("exit code = 0, want a failure for a blank term; stdout: %s", out)
	}
	if !strings.Contains(errOut, "search term is required") {
		t.Errorf("stderr = %q, want it to ask for a term", errOut)
	}
}

func TestEmployeeSearchLimit(t *testing.T) {
	out, errOut, code := run(t, searchServer(t), "employee", "search", "springfield", "--limit", "2")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want a header and two rows:\n%s", len(lines), out)
	}
}

func TestEmployeeSearchWarnsOnUnknownField(t *testing.T) {
	// Without the warning an unknown --in name just returns nothing, which
	// looks like the search is broken.
	_, errOut, code := run(t, searchServer(t), "employee", "search", "alex", "--in", "bogusfield")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "bogusfield") {
		t.Errorf("stderr = %q, want a warning naming the unknown field", errOut)
	}
}

func TestEmployeeSearchJSONIsTheAPIResponse(t *testing.T) {
	out, errOut, code := run(t, searchServer(t), "--output", "json", "employee", "search", "morgan")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, errOut)
	}

	var records []map[string]any
	if err := json.Unmarshal([]byte(out), &records); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	// The matched column is a table affordance, not part of the response.
	if _, ok := records[0]["matched"]; ok {
		t.Error("json carries a matched field, want the API response untouched")
	}
	if _, ok := records[0]["photoUrl"]; !ok {
		t.Error("json dropped photoUrl, want the full response kept")
	}
}
