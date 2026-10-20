package bamboohr

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// zeroDate is the value BambooHR sends for a date field that was never set.
const zeroDate = "0000-00-00"

// FormatValue renders a value decoded from a JSON response as a display
// string.
//
// It exists because BambooHR returns many fields with a type that varies by
// field and by endpoint: an ID may arrive as a string or a number, and a
// missing value may be null or absent. Numbers are printed without an exponent
// so that an employee ID stays readable, and nested objects and arrays are
// printed as compact JSON.
func FormatValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		// BambooHR returns an unset date as a zero date rather than null or an
		// empty string. Displaying it as-is reads like a real value.
		if val == zeroDate {
			return ""
		}
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return formatFloat(val)
	case float32:
		return formatFloat(float64(val))
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case json.Number:
		return val.String()
	case []any:
		return compactJSON(val)
	case map[string]any:
		return compactJSON(val)
	default:
		return fmt.Sprint(val)
	}
}

func formatFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return ""
	}
	// JSON has one number type, so an integer field arrives as a float. Print
	// it without a decimal point or exponent.
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// yesNo reports whether one of the API's "yes"/"no" string flags is set.
func yesNo(s string) bool { return strings.EqualFold(s, "yes") }

// jsonUnmarshal is json.Unmarshal under a package-local name, used by the types
// that decode a response into both typed fields and a raw map so that the two
// calls read as one operation.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
