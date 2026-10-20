// Package bamboohr is a client for the BambooHR REST API (v1).
//
// A client authenticates with an API key using HTTP Basic auth, which is the
// scheme BambooHR documents for server-side integrations: the key is the
// username and the password is ignored.
//
//	c, err := bamboohr.New("acme", os.Getenv("BAMBOOHR_API_KEY"))
//	if err != nil {
//		return err
//	}
//	emp, err := c.GetEmployee(ctx, "123", bamboohr.GetEmployeeOptions{
//		Fields: []string{"firstName", "lastName", "workEmail"},
//	})
//
// All requests are made against https://{subdomain}.bamboohr.com and ask for
// JSON. Failed requests are reported as *APIError, which carries the status
// code and the X-BambooHR-Error-Message header BambooHR uses to explain 4xx
// responses.
package bamboohr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent identifies this client to BambooHR.
const DefaultUserAgent = "go-bamboohr"

// The Accept headers the client sends.
const (
	acceptJSON = "application/json"
	// acceptAny is sent for binary downloads. Some endpoints honour Accept and
	// return the file wrapped in a JSON envelope when the request says it
	// accepts JSON, which would save an employee photo as JSON rather than an
	// image.
	acceptAny = "*/*"
)

// defaults for a client created without options.
const (
	defaultTimeout    = 60 * time.Second
	defaultMaxRetries = 3
	defaultRetryBase  = 500 * time.Millisecond
	// maxRetryAfter caps how long a Retry-After header can park a request.
	maxRetryAfter = 30 * time.Second
)

// Client talks to the BambooHR API for a single company subdomain.
//
// A Client is safe for concurrent use.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
	userAgent  string
	maxRetries int
	retryBase  time.Duration
	// sleep is swappable so tests do not pay for backoff.
	sleep func(time.Duration)
}

// Option customises a Client. Options are applied in order by New.
type Option func(*Client)

// WithHTTPClient sets the underlying HTTP client. Use it to control transport
// settings, proxies or timeouts. It replaces the timeout set by WithTimeout, so
// pass the two in a deliberate order if you use both.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithTimeout sets the per-request timeout on the client's HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

// WithUserAgent overrides the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithBaseURL points the client at an alternative host. It is intended for
// tests and for proxies that stand in front of the BambooHR API; it overrides
// the URL derived from the subdomain.
func WithBaseURL(raw string) Option {
	return func(c *Client) {
		if u, err := url.Parse(raw); err == nil {
			c.baseURL = u
		}
	}
}

// WithMaxRetries sets how many times a retryable response (429, 503 and other
// 5xx) is retried before the error is returned. Zero disables retrying.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// New returns a Client for the given company subdomain and API key.
//
// The subdomain is the first label of the company's BambooHR host: for
// https://acme.bamboohr.com it is "acme". Both arguments are required.
func New(subdomain, apiKey string, opts ...Option) (*Client, error) {
	subdomain = strings.TrimSpace(subdomain)
	apiKey = strings.TrimSpace(apiKey)
	if subdomain == "" {
		return nil, errors.New("bamboohr: subdomain is required")
	}
	if apiKey == "" {
		return nil, errors.New("bamboohr: api key is required")
	}
	// Accept a full host or URL as well as a bare subdomain; people paste what
	// they see in the browser.
	if strings.Contains(subdomain, ".") || strings.Contains(subdomain, "/") {
		return nil, fmt.Errorf("bamboohr: subdomain %q must be the company label only, not a host or URL", subdomain)
	}

	base, err := url.Parse("https://" + subdomain + ".bamboohr.com")
	if err != nil {
		return nil, fmt.Errorf("bamboohr: invalid subdomain %q: %w", subdomain, err)
	}

	c := &Client{
		baseURL:    base,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  DefaultUserAgent,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
		sleep:      time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the root URL the client sends requests to.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// newRequest builds an authenticated request. A non-nil body is encoded as
// JSON unless it is already a []byte.
func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body any, accept string) (*http.Request, []byte, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, nil, fmt.Errorf("bamboohr: invalid path %q: %w", path, err)
	}
	u := c.baseURL.ResolveReference(rel)
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var raw []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		raw = b
	default:
		raw, err = json.Marshal(b)
		if err != nil {
			return nil, nil, fmt.Errorf("bamboohr: encoding request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	// BambooHR authenticates the API key as the Basic username; the password is
	// unused but must be present.
	req.SetBasicAuth(c.apiKey, "x")
	if accept == "" {
		accept = acceptJSON
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent)
	if raw != nil {
		// The API only recognises an exact "application/json" content type;
		// adding a charset makes it parse the body as XML instead.
		req.Header.Set("Content-Type", "application/json")
	}
	return req, raw, nil
}

// send performs a request, retrying retryable responses, and returns the
// response with its body still open. The caller must close it.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any, accept string) (*http.Response, error) {
	req, raw, err := c.newRequest(ctx, method, path, query, body, accept)
	if err != nil {
		return nil, err
	}

	for attempt := 0; ; attempt++ {
		if raw != nil {
			req.Body = io.NopCloser(bytes.NewReader(raw))
			req.ContentLength = int64(len(raw))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Context errors are the caller's decision, not something to retry.
			if ctx.Err() != nil {
				return nil, err
			}
			if attempt < c.maxRetries {
				if werr := c.wait(ctx, c.backoff(attempt)); werr != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("bamboohr: %s %s: %w", method, path, err)
		}

		if attempt < c.maxRetries && retryable(resp.StatusCode) {
			delay := retryAfter(resp.Header.Get("Retry-After"))
			if delay <= 0 {
				delay = c.backoff(attempt)
			}
			drain(resp)
			if werr := c.wait(ctx, delay); werr != nil {
				return nil, werr
			}
			continue
		}

		if resp.StatusCode >= 400 {
			defer drain(resp)
			return nil, newAPIError(method, path, resp)
		}
		return resp, nil
	}
}

// do performs a request and decodes a JSON response into out. A nil out
// discards the body, which is what the no-content endpoints need.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	resp, err := c.send(ctx, method, path, query, body, acceptJSON)
	if err != nil {
		return err
	}
	defer drain(resp)

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("bamboohr: reading %s %s response: %w", method, path, err)
	}
	// Some endpoints answer a successful mutation with an empty body.
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("bamboohr: decoding %s %s response: %w", method, path, err)
	}
	return nil
}

// get is the common case: a GET whose JSON body is decoded into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// download streams a binary response into w and returns its metadata.
//
// It asks for any content type rather than JSON, and unwraps the base64
// envelope some endpoints return anyway, so that a caller always receives the
// file's own bytes.
func (c *Client) download(ctx context.Context, path string, query url.Values, w io.Writer) (*Download, error) {
	resp, err := c.send(ctx, http.MethodGet, path, query, nil, acceptAny)
	if err != nil {
		return nil, err
	}
	defer drain(resp)

	contentType := resp.Header.Get("Content-Type")
	if isJSONContentType(contentType) {
		return unwrapBase64File(resp, w, path)
	}

	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bamboohr: downloading %s: %w", path, err)
	}
	return &Download{
		Filename:    filenameFromDisposition(resp.Header.Get("Content-Disposition")),
		ContentType: contentType,
		Size:        n,
	}, nil
}

// base64FileEnvelope is the shape an endpoint uses when it returns a file as
// JSON instead of raw bytes.
type base64FileEnvelope struct {
	MimeType   string `json:"mimeType"`
	FileBase64 string `json:"fileBase64"`
}

// unwrapBase64File decodes a JSON-wrapped file into w.
func unwrapBase64File(resp *http.Response, w io.Writer, path string) (*Download, error) {
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bamboohr: downloading %s: %w", path, err)
	}

	var envelope base64FileEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.FileBase64 == "" {
		return nil, fmt.Errorf("bamboohr: %s returned JSON rather than a file: %s",
			path, truncate(oneLine(string(payload)), 120))
	}

	decoded, err := base64.StdEncoding.DecodeString(envelope.FileBase64)
	if err != nil {
		return nil, fmt.Errorf("bamboohr: decoding the file %s returned: %w", path, err)
	}
	n, err := w.Write(decoded)
	if err != nil {
		return nil, fmt.Errorf("bamboohr: writing %s: %w", path, err)
	}
	return &Download{
		Filename:    filenameFromDisposition(resp.Header.Get("Content-Disposition")),
		ContentType: envelope.MimeType,
		Size:        int64(n),
	}, nil
}

// isJSONContentType reports whether a media type is JSON, ignoring any
// parameters such as a charset.
func isJSONContentType(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), acceptJSON)
}

// Download describes a binary response saved by the client.
type Download struct {
	// Filename is the name from the Content-Disposition header, empty when the
	// response did not suggest one.
	Filename string
	// ContentType is the response media type.
	ContentType string
	// Size is the number of bytes written.
	Size int64
}

func (c *Client) backoff(attempt int) time.Duration {
	d := c.retryBase << attempt
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d
}

// wait sleeps for d unless the context finishes first.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if c.sleep != nil {
		// A swapped-in sleep is a test hook; honour cancellation but do not
		// spin up a timer.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.sleep(d)
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryable reports whether a status code is worth trying again. BambooHR
// documents 503 (often rate limiting, with Retry-After) and 5xx as transient.
func retryable(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		http.StatusInternalServerError:
		return true
	}
	return false
}

// retryAfter parses a Retry-After header in either of its documented forms.
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			if d > maxRetryAfter {
				return maxRetryAfter
			}
			return d
		}
	}
	return 0
}

// drain closes a response body, reading what is left so the connection can be
// reused.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// filenameFromDisposition pulls the filename out of a Content-Disposition
// header. mime.ParseMediaType decodes the RFC 2231 filename* form into the
// plain "filename" key, so both spellings are covered.
func filenameFromDisposition(v string) string {
	if v == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if name == "" {
		return ""
	}
	// Some BambooHR responses single-quote the filename, which RFC 2045 does
	// not define as quoting, so mime keeps the quotes as part of the value.
	// Left alone they end up in the name of the file written to disk.
	name = trimMatchingQuotes(name)
	if name == "" {
		return ""
	}
	// Strip any directory component: the name is used to create a local file.
	return filepath.Base(name)
}

// trimMatchingQuotes removes a surrounding pair of single or double quotes.
func trimMatchingQuotes(s string) string {
	if len(s) >= 2 {
		if first, last := s[0], s[len(s)-1]; first == last && (first == '\'' || first == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
