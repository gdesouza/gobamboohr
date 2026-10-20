package bamboohr

import (
	"fmt"
	"strconv"
	"strings"
)

// FlexInt is an integer that decodes from either a JSON number or a quoted
// number. Several BambooHR endpoints differ on which they send for the same
// logical field, most visibly file sizes.
type FlexInt int64

// UnmarshalJSON accepts a number, a quoted number, or null.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Fall back through float so a value like "1024.0" still decodes.
		g, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return fmt.Errorf("bamboohr: cannot decode %q as an integer", s)
		}
		n = int64(g)
	}
	*f = FlexInt(n)
	return nil
}

// MarshalJSON writes the value as a plain JSON number.
func (f FlexInt) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, int64(f), 10), nil
}

// Int64 returns the value as an int64.
func (f FlexInt) Int64() int64 { return int64(f) }

// String renders the value in base 10.
func (f FlexInt) String() string { return strconv.FormatInt(int64(f), 10) }
