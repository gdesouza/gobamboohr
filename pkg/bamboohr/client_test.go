package bamboohr

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient returns a client pointed at srv with backoff disabled.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	all := append([]Option{WithBaseURL(srv.URL), WithHTTPClient(srv.Client())}, opts...)
	c, err := New("acme", "test-key", all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Tests must not pay for real backoff.
	c.sleep = func(time.Duration) {}
	return c
}

func TestNewValidatesArguments(t *testing.T) {
	tests := []struct {
		name      string
		subdomain string
		apiKey    string
		wantErr   string
	}{
		{name: "ok", subdomain: "acme", apiKey: "key"},
		{name: "missing subdomain", apiKey: "key", wantErr: "subdomain is required"},
		{name: "missing key", subdomain: "acme", wantErr: "api key is required"},
		{name: "host given as subdomain", subdomain: "acme.bamboohr.com", apiKey: "key", wantErr: "company label only"},
		{name: "url given as subdomain", subdomain: "https://acme.bamboohr.com/", apiKey: "key", wantErr: "company label only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.subdomain, tt.apiKey)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("New(%q, %q) succeeded, want error", tt.subdomain, tt.apiKey)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("New error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got, want := c.BaseURL(), "https://acme.bamboohr.com"; got != want {
				t.Errorf("BaseURL = %q, want %q", got, want)
			}
		})
	}
}

func TestRequestHeadersAndAuth(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithUserAgent("test-agent/1"))
	if _, err := c.GetEmployee(context.Background(), "1", GetEmployeeOptions{}); err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}

	user, pass, ok := got.BasicAuth()
	if !ok {
		t.Fatal("request carried no basic auth")
	}
	// BambooHR takes the API key as the username and ignores the password.
	if user != "test-key" {
		t.Errorf("basic auth user = %q, want the API key", user)
	}
	if pass == "" {
		t.Error("basic auth password is empty; BambooHR requires a placeholder")
	}
	if accept := got.Header.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}
	if ua := got.Header.Get("User-Agent"); ua != "test-agent/1" {
		t.Errorf("User-Agent = %q, want test-agent/1", ua)
	}
}

func TestJSONBodyContentTypeIsExact(t *testing.T) {
	// BambooHR only treats a body as JSON when the content type is exactly
	// "application/json"; a charset parameter makes it parse the body as XML.
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.Header().Set("Location", "https://acme.bamboohr.com/employees/42")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.CreateEmployee(context.Background(), map[string]any{"firstName": "Ada"}); err != nil {
		t.Fatalf("CreateEmployee: %v", err)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want exactly application/json", contentType)
	}
}

func TestAPIErrorUsesErrorHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(errorMessageHeader, "Invalid field: nope")
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetEmployee(context.Background(), "1", GetEmployeeOptions{})
	if err == nil {
		t.Fatal("GetEmployee succeeded, want an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotAcceptable {
		t.Errorf("StatusCode = %d, want 406", apiErr.StatusCode)
	}
	if apiErr.Message != "Invalid field: nope" {
		t.Errorf("Message = %q, want the X-BambooHR-Error-Message value", apiErr.Message)
	}
	if !strings.Contains(err.Error(), "Invalid field: nope") {
		t.Errorf("Error() = %q, want it to include the message", err)
	}
}

func TestAPIErrorParsesBodyShapes(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "string code envelope",
			body:        `{"error":{"code":"BadRequest","message":"Invalid request parameters."}}`,
			wantCode:    "BadRequest",
			wantMessage: "Invalid request parameters.",
		},
		{
			name:        "integer code envelope",
			body:        `{"error":{"code":500,"message":"Internal server error"}}`,
			wantCode:    "500",
			wantMessage: "Internal server error",
		},
		{
			name:        "problem shape",
			body:        `{"type":"internal_error","title":"Something went wrong.","details":"try again"}`,
			wantCode:    "internal_error",
			wantMessage: "Something went wrong.: try again",
		},
		{
			name: "not json",
			body: `<html>nope</html>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, message := parseErrorBody([]byte(tt.body))
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if message != tt.wantMessage {
				t.Errorf("message = %q, want %q", message, tt.wantMessage)
			}
		})
	}
}

func TestErrorClassifiers(t *testing.T) {
	tests := []struct {
		status          int
		notFound        bool
		unauthorized    bool
		forbidden       bool
		rateLimited     bool
		wantStatusCodeF int
	}{
		{status: http.StatusNotFound, notFound: true, wantStatusCodeF: 404},
		{status: http.StatusUnauthorized, unauthorized: true, wantStatusCodeF: 401},
		{status: http.StatusForbidden, forbidden: true, wantStatusCodeF: 403},
		{status: http.StatusTooManyRequests, rateLimited: true, wantStatusCodeF: 429},
		{status: http.StatusServiceUnavailable, rateLimited: true, wantStatusCodeF: 503},
	}

	for _, tt := range tests {
		err := &APIError{StatusCode: tt.status}
		if got := IsNotFound(err); got != tt.notFound {
			t.Errorf("IsNotFound(%d) = %v, want %v", tt.status, got, tt.notFound)
		}
		if got := IsUnauthorized(err); got != tt.unauthorized {
			t.Errorf("IsUnauthorized(%d) = %v, want %v", tt.status, got, tt.unauthorized)
		}
		if got := IsForbidden(err); got != tt.forbidden {
			t.Errorf("IsForbidden(%d) = %v, want %v", tt.status, got, tt.forbidden)
		}
		if got := IsRateLimited(err); got != tt.rateLimited {
			t.Errorf("IsRateLimited(%d) = %v, want %v", tt.status, got, tt.rateLimited)
		}
		if got := StatusCode(err); got != tt.wantStatusCodeF {
			t.Errorf("StatusCode(%d) = %d, want %d", tt.status, got, tt.wantStatusCodeF)
		}
	}

	if got := StatusCode(errors.New("plain")); got != 0 {
		t.Errorf("StatusCode(non-API error) = %d, want 0", got)
	}
}

func TestRetriesThrottledResponse(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			// This is the response BambooHR documents for rate limiting.
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"id":"7"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	emp, err := c.GetEmployee(context.Background(), "7", GetEmployeeOptions{})
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	if calls != 3 {
		t.Errorf("server saw %d calls, want 3", calls)
	}
	if emp.Key() != "7" {
		t.Errorf("employee id = %q, want 7", emp.Key())
	}
}

func TestRetriesGiveUpAndReportRetryAfter(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(2))
	_, err := c.GetEmployee(context.Background(), "1", GetEmployeeOptions{})
	if err == nil {
		t.Fatal("GetEmployee succeeded, want an error")
	}
	// Two retries means three attempts in total.
	if calls != 3 {
		t.Errorf("server saw %d calls, want 3", calls)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", apiErr.RetryAfter)
	}
	if !IsRateLimited(err) {
		t.Error("IsRateLimited = false, want true for 503")
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetEmployee(context.Background(), "1", GetEmployeeOptions{}); err == nil {
		t.Fatal("GetEmployee succeeded, want an error")
	}
	if calls != 1 {
		t.Errorf("server saw %d calls, want 1: a 403 will not fix itself", calls)
	}
}

func TestContextCancellationStopsRetrying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(5))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.GetEmployee(ctx, "1", GetEmployeeOptions{}); err == nil {
		t.Fatal("GetEmployee succeeded, want an error from the canceled context")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "empty", value: "", want: 0},
		{name: "seconds", value: "12", want: 12 * time.Second},
		{name: "seconds with spaces", value: " 3 ", want: 3 * time.Second},
		{name: "capped", value: "6000", want: maxRetryAfter},
		{name: "past http date", value: "Mon, 02 Jan 2006 15:04:05 GMT", want: 0},
		{name: "nonsense", value: "soon", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryAfter(tt.value); got != tt.want {
				t.Errorf("retryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestDownloadCapturesMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="offer letter.pdf"`)
		w.Write([]byte("%PDF-1.4 stub"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	meta, err := c.DownloadCompanyFile(context.Background(), 9, &buf)
	if err != nil {
		t.Fatalf("DownloadCompanyFile: %v", err)
	}
	if meta.Filename != "offer letter.pdf" {
		t.Errorf("Filename = %q, want %q", meta.Filename, "offer letter.pdf")
	}
	if meta.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want application/pdf", meta.ContentType)
	}
	if meta.Size != int64(buf.Len()) {
		t.Errorf("Size = %d, want %d", meta.Size, buf.Len())
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "empty", header: "", want: ""},
		{name: "quoted", header: `attachment; filename="a.pdf"`, want: "a.pdf"},
		{name: "bare", header: `attachment; filename=a.pdf`, want: "a.pdf"},
		{name: "rfc 5987", header: `attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf`, want: "résumé.pdf"},
		{name: "no filename", header: `inline`, want: ""},
		// A path in the header must not escape the working directory.
		{name: "path stripped", header: `attachment; filename="../../etc/passwd"`, want: "passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filenameFromDisposition(tt.header); got != tt.want {
				t.Errorf("filenameFromDisposition(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestFilenameFromDispositionStripsStrayQuotes(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			// BambooHR's applicant tracking downloads use single quotes, which
			// RFC 2045 does not define as quoting, so mime keeps them.
			name:   "single quoted",
			header: `inline; filename='Cyril_Resume_preview.png'`,
			want:   "Cyril_Resume_preview.png",
		},
		{name: "double quoted", header: `attachment; filename="resume.pdf"`, want: "resume.pdf"},
		{name: "unquoted", header: `attachment; filename=resume.pdf`, want: "resume.pdf"},
		// An apostrophe inside the name must survive.
		{name: "apostrophe kept", header: `attachment; filename="o'brien cv.pdf"`, want: "o'brien cv.pdf"},
		{name: "only quotes", header: `attachment; filename="''"`, want: ""},
		{name: "mismatched", header: `attachment; filename="'resume.pdf"`, want: "'resume.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filenameFromDisposition(tt.header); got != tt.want {
				t.Errorf("filenameFromDisposition(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestTrimMatchingQuotes(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: `'a'`, want: "a"},
		{in: `"a"`, want: "a"},
		{in: `a`, want: "a"},
		{in: `'a`, want: "'a"},
		{in: `a'`, want: "a'"},
		{in: `''`, want: ""},
		{in: ``, want: ""},
		// Only the outermost pair comes off.
		{in: `''a''`, want: "'a'"},
	}
	for _, tt := range tests {
		if got := trimMatchingQuotes(tt.in); got != tt.want {
			t.Errorf("trimMatchingQuotes(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDownloadAsksForAnyContentType(t *testing.T) {
	// The photo endpoint honours Accept and wraps the image in JSON when the
	// request says it accepts JSON, which would save an image as JSON.
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8\xff\xe0jpeg"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	if _, err := c.GetEmployeePhoto(context.Background(), "208", PhotoSmall, &buf); err != nil {
		t.Fatalf("GetEmployeePhoto: %v", err)
	}
	if accept == "application/json" {
		t.Errorf("Accept = %q, want a binary download not to ask for JSON", accept)
	}
	if accept != "*/*" {
		t.Errorf("Accept = %q, want */*", accept)
	}
}

func TestJSONRequestsStillAskForJSON(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.GetEmployee(context.Background(), "1", GetEmployeeOptions{}); err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}
}

func TestDownloadUnwrapsBase64Envelope(t *testing.T) {
	// Even with */* an endpoint may answer with the envelope, so the caller
	// must still receive the file's own bytes.
	const jpeg = "\xff\xd8\xff\xe0the-image-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"mimeType":"image/jpeg","fileBase64":"` +
			base64.StdEncoding.EncodeToString([]byte(jpeg)) + `"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	meta, err := c.GetEmployeePhoto(context.Background(), "208", PhotoSmall, &buf)
	if err != nil {
		t.Fatalf("GetEmployeePhoto: %v", err)
	}
	if buf.String() != jpeg {
		t.Errorf("wrote %q, want the decoded image bytes", buf.String())
	}
	// The envelope's own mime type is what describes the file.
	if meta.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", meta.ContentType)
	}
	if meta.Size != int64(len(jpeg)) {
		t.Errorf("Size = %d, want %d", meta.Size, len(jpeg))
	}
}

func TestDownloadRejectsUnexpectedJSON(t *testing.T) {
	// JSON that is not an envelope must be an error, not a file full of JSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"something":"else"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	_, err := c.GetEmployeePhoto(context.Background(), "208", PhotoSmall, &buf)
	if err == nil {
		t.Fatal("GetEmployeePhoto succeeded, want an error for a non-envelope JSON response")
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %q, want nothing written", buf.String())
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "application/json", want: true},
		{in: "application/json; charset=UTF-8", want: true},
		{in: "Application/JSON", want: true},
		{in: " application/json ", want: true},
		{in: "application/pdf; charset=UTF-8", want: false},
		{in: "image/jpeg", want: false},
		{in: "", want: false},
	}
	for _, tt := range tests {
		if got := isJSONContentType(tt.in); got != tt.want {
			t.Errorf("isJSONContentType(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
