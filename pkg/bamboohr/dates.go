package bamboohr

import "time"

// DateLayout is the YYYY-MM-DD form every date parameter and date field in the
// BambooHR API uses.
const DateLayout = "2006-01-02"

// InclusiveDays returns the number of days a start and end date span, counting
// both ends, which is how BambooHR expresses a date range: a holiday or a time
// off request from the 4th to the 8th is five days, not four.
//
// It returns 0 when either date does not parse or the range runs backwards.
func InclusiveDays(start, end string) int {
	from, err := time.Parse(DateLayout, start)
	if err != nil {
		return 0
	}
	to, err := time.Parse(DateLayout, end)
	if err != nil {
		return 0
	}
	if to.Before(from) {
		return 0
	}
	// Both dates are parsed as UTC midnight, so the difference is a whole
	// number of days and no daylight-saving correction is needed.
	return int(to.Sub(from).Hours()/24) + 1
}

// inclusiveDays is the unexported spelling used inside the package.
func inclusiveDays(start, end string) int { return InclusiveDays(start, end) }
