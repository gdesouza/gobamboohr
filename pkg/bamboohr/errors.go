package bamboohr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// errorMessageHeader is the header BambooHR uses to explain most 4xx and some
// 5xx responses. The documentation warns the text may change at any time, so it
// is surfaced for humans and logs rather than matched on.
const errorMessageHeader = "X-BambooHR-Error-Message"

// maxErrorBody bounds how much of a failed response is kept for the message.
const maxErrorBody = 8 << 10

// APIError is returned for any BambooHR response with a 4xx or 5xx status.
type APIError struct {
	// Method and Path identify the request that failed.
	Method string
	Path   string
	// StatusCode is the HTTP status code.
	StatusCode int
	// Message is the best available explanation: the X-BambooHR-Error-Message
	// header when present, otherwise a message parsed out of the body.
	Message string
	// Body is the response body, truncated to 8 KiB.
	Body string
	// Code is the machine-readable error code some endpoints return in the body
	// (for example "BadRequest" or "BadPage"). Empty when the response did not
	// carry one.
	Code string
	// RetryAfter is the Retry-After delay BambooHR asked for, zero when absent.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "bamboohr: %s %s: %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	switch {
	case e.Message != "":
		fmt.Fprintf(&b, ": %s", e.Message)
	case e.Body != "":
		fmt.Fprintf(&b, ": %s", truncate(oneLine(e.Body), 200))
	default:
		// The status code is already the whole story; add the hint BambooHR
		// documents for the codes people hit most.
		if hint := statusHint(e.StatusCode); hint != "" {
			fmt.Fprintf(&b, ": %s", hint)
		}
	}
	return b.String()
}

// newAPIError builds an APIError from a failed response. It does not close the
// body; the caller owns it.
func newAPIError(method, path string, resp *http.Response) *APIError {
	e := &APIError{
		Method:     method,
		Path:       path,
		StatusCode: resp.StatusCode,
		Message:    strings.TrimSpace(resp.Header.Get(errorMessageHeader)),
		RetryAfter: retryAfter(resp.Header.Get("Retry-After")),
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err == nil && len(payload) > 0 {
		e.Body = string(payload)
		code, msg := parseErrorBody(payload)
		e.Code = code
		if e.Message == "" {
			e.Message = msg
		}
	}
	return e
}

// parseErrorBody pulls a code and message out of the error shapes the API
// uses: {"error":{"code":..,"message":..}} and {"type":..,"title":..}.
func parseErrorBody(payload []byte) (code, message string) {
	var envelope struct {
		Error struct {
			// The code is a string on the newer endpoints and an integer on the
			// older ones.
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
		} `json:"error"`
		Type    string `json:"type"`
		Title   string `json:"title"`
		Details string `json:"details"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", ""
	}

	if len(envelope.Error.Code) > 0 {
		var s string
		if json.Unmarshal(envelope.Error.Code, &s) == nil {
			code = s
		} else {
			code = strings.Trim(string(envelope.Error.Code), `"`)
		}
	}
	switch {
	case envelope.Error.Message != "":
		message = envelope.Error.Message
	case envelope.Title != "":
		message = envelope.Title
		if envelope.Details != "" {
			message += ": " + envelope.Details
		}
		if code == "" {
			code = envelope.Type
		}
	case envelope.Message != "":
		message = envelope.Message
	}
	// A numeric code duplicating the HTTP status adds nothing.
	if code == "" || code == "0" {
		code = ""
	}
	return code, strings.TrimSpace(message)
}

// statusHint explains the status codes whose BambooHR meaning is not obvious
// from the standard HTTP text.
func statusHint(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "the API key is missing or malformed"
	case http.StatusForbidden:
		return "the API key's user lacks permission for this request"
	case http.StatusNotAcceptable:
		return "the request references fields that do not exist"
	case http.StatusConflict:
		return "the request would create a duplicate, or a field was given an invalid value"
	case http.StatusServiceUnavailable:
		return "the endpoint is unavailable, commonly because of rate limiting"
	}
	return ""
}

// StatusCode returns the HTTP status code carried by err, or 0 if err is not an
// *APIError.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool { return StatusCode(err) == http.StatusNotFound }

// IsUnauthorized reports whether err is a 401, meaning the API key was missing
// or unusable.
func IsUnauthorized(err error) bool { return StatusCode(err) == http.StatusUnauthorized }

// IsForbidden reports whether err is a 403, meaning the key's user lacks the
// required permission.
func IsForbidden(err error) bool { return StatusCode(err) == http.StatusForbidden }

// IsRateLimited reports whether err is a 429 or 503, the statuses BambooHR uses
// when it throttles a caller.
func IsRateLimited(err error) bool {
	switch StatusCode(err) {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	}
	return false
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
