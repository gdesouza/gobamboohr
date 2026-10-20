package bamboohr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TimeOffStatus is the state of a time off request.
type TimeOffStatus string

// The statuses a time off request can be in.
const (
	StatusRequested TimeOffStatus = "REQUESTED"
	StatusApproved  TimeOffStatus = "APPROVED"
	StatusDenied    TimeOffStatus = "DENIED"
	StatusCanceled  TimeOffStatus = "CANCELED"
)

// TimeOffUnit is the unit a time off amount is measured in, set by the time off
// category.
type TimeOffUnit string

// The units a time off amount can use.
const (
	UnitHours TimeOffUnit = "HOURS"
	UnitDays  TimeOffUnit = "DAYS"
)

// PageMeta is the page counter the offset-paginated endpoints return.
type PageMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalPages int `json:"totalPages"`
	TotalItems int `json:"totalItems"`
}

// HasMore reports whether a further page exists after this one.
func (m PageMeta) HasMore() bool { return m.Page > 0 && m.Page < m.TotalPages }

// DailyAmount is the amount of time off taken on a single day.
type DailyAmount struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

// TimeOffRequest is an employee's request to take time off.
type TimeOffRequest struct {
	ID         int           `json:"id"`
	EmployeeID int           `json:"employeeId"`
	CategoryID int           `json:"categoryId"`
	StartDate  string        `json:"startDate"`
	EndDate    string        `json:"endDate"`
	Amount     float64       `json:"amount"`
	Unit       TimeOffUnit   `json:"unit"`
	Status     TimeOffStatus `json:"status"`
	// RequestedAt is a UTC timestamp.
	RequestedAt  string        `json:"requestedAt"`
	DailyAmounts []DailyAmount `json:"dailyAmounts,omitempty"`
	// EmployeeNote is the requester's note; nil when never set.
	EmployeeNote *string `json:"employeeNote"`
	// ManagerNote is the note left when the request was approved or denied; nil
	// when never set.
	ManagerNote     *string `json:"managerNote"`
	StatusUpdatedAt *string `json:"statusUpdatedAt"`
	StatusUpdatedBy *int    `json:"statusUpdatedBy"`
	// Links lists the transitions the caller may make on this request. It is
	// only populated when the call set ReturnActions, and its absence is
	// authoritative: attempting a missing transition returns 403 or 409.
	Links map[string]AvailableAction `json:"_links,omitempty"`
}

// AvailableAction is one state transition offered on a time off request.
type AvailableAction struct {
	Href   string `json:"href"`
	Method string `json:"method"`
}

// TimeOffRequestListOptions controls ListTimeOffRequests.
//
// The convenience fields build an OData filter. Filter overrides them entirely
// for expressions they cannot express.
type TimeOffRequestListOptions struct {
	// EmployeeIDs limits the response to these employees.
	EmployeeIDs []int
	// Statuses limits the response to these statuses.
	Statuses []TimeOffStatus
	// CategoryID limits the response to one time off category.
	CategoryID int
	// Start and End bound the request dates: startDate ge Start and
	// endDate le End. Both are YYYY-MM-DD.
	Start string
	End   string
	// Filter is a raw OData expression. When set, the fields above are ignored.
	Filter string
	// OrderBy sorts the results, for example "requestedAt desc". The API
	// defaults to "requestedAt desc".
	OrderBy string
	// Page is 1-based; zero means the first page.
	Page int
	// PageSize is the number of requests per page; zero uses the API default
	// of 100.
	PageSize int
	// ReturnActions asks for the available transitions on each request.
	ReturnActions bool
}

func (o TimeOffRequestListOptions) query() url.Values {
	q := url.Values{}

	filter := o.Filter
	if filter == "" {
		f := NewFilter()
		if len(o.EmployeeIDs) == 1 {
			f.Eq("employeeId", o.EmployeeIDs[0])
		} else if len(o.EmployeeIDs) > 1 {
			f.In("employeeId", toAny(o.EmployeeIDs)...)
		}
		if len(o.Statuses) == 1 {
			f.Eq("status", string(o.Statuses[0]))
		} else if len(o.Statuses) > 1 {
			values := make([]any, 0, len(o.Statuses))
			for _, s := range o.Statuses {
				values = append(values, string(s))
			}
			f.In("status", values...)
		}
		if o.CategoryID > 0 {
			f.Eq("categoryId", o.CategoryID)
		}
		if o.Start != "" {
			f.Ge("startDate", o.Start)
		}
		if o.End != "" {
			f.Le("endDate", o.End)
		}
		filter = f.String()
	}
	if filter != "" {
		q.Set("filter", filter)
	}
	if o.OrderBy != "" {
		q.Set("orderBy", o.OrderBy)
	}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(o.PageSize))
	}
	if o.ReturnActions {
		q.Set("returnActions", "true")
	}
	return q
}

// TimeOffRequestList is one page of time off requests.
type TimeOffRequestList struct {
	Data []TimeOffRequest `json:"data"`
	Meta PageMeta         `json:"meta"`
}

// ListTimeOffRequests returns one page of time off requests.
//
// A request that a later edit replaced is never returned.
func (c *Client) ListTimeOffRequests(ctx context.Context, opts TimeOffRequestListOptions) (*TimeOffRequestList, error) {
	var out TimeOffRequestList
	if err := c.get(ctx, "/api/v1/time-off/requests", opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllTimeOffRequests walks every page of matching requests.
func (c *Client) ListAllTimeOffRequests(ctx context.Context, opts TimeOffRequestListOptions) ([]TimeOffRequest, error) {
	var all []TimeOffRequest
	if opts.Page <= 0 {
		opts.Page = 1
	}
	for {
		page, err := c.ListTimeOffRequests(ctx, opts)
		if err != nil {
			return all, err
		}
		all = append(all, page.Data...)
		if !page.Meta.HasMore() || len(page.Data) == 0 {
			return all, nil
		}
		opts.Page = page.Meta.Page + 1
	}
}

// GetTimeOffRequest returns one time off request. returnActions asks for the
// transitions the caller may make on it.
func (c *Client) GetTimeOffRequest(ctx context.Context, id int, returnActions bool) (*TimeOffRequest, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bamboohr: time off request id is required")
	}
	q := url.Values{}
	if returnActions {
		q.Set("returnActions", "true")
	}
	var req TimeOffRequest
	if err := c.get(ctx, "/api/v1/time-off/requests/"+strconv.Itoa(id), q, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// NewTimeOffRequest describes a time off request to create.
//
// Exactly one of Amount or DailyAmounts must be set: Amount spreads the total
// across the range the way BambooHR would, DailyAmounts states each day.
type NewTimeOffRequest struct {
	EmployeeID int    `json:"employeeId"`
	CategoryID int    `json:"categoryId"`
	StartDate  string `json:"startDate"`
	// EndDate is inclusive.
	EndDate string `json:"endDate"`
	// Amount is the total for the whole range, in the category's unit.
	Amount *float64 `json:"amount,omitempty"`
	// DailyAmounts gives a per-day breakdown; each date must fall in the range.
	DailyAmounts []DailyAmount `json:"dailyAmounts,omitempty"`
	// EmployeeNote is the requester's note, at most 1024 characters.
	EmployeeNote string `json:"employeeNote,omitempty"`
}

func (r NewTimeOffRequest) validate() error {
	switch {
	case r.EmployeeID <= 0:
		return fmt.Errorf("bamboohr: employee id is required")
	case r.CategoryID <= 0:
		return fmt.Errorf("bamboohr: category id is required")
	case r.StartDate == "":
		return fmt.Errorf("bamboohr: start date is required")
	case r.EndDate == "":
		return fmt.Errorf("bamboohr: end date is required")
	}
	// The API rejects both and neither; catching it here gives a clearer
	// message than a 400 would.
	hasAmount := r.Amount != nil
	hasDaily := len(r.DailyAmounts) > 0
	if hasAmount == hasDaily {
		return fmt.Errorf("bamboohr: set exactly one of amount or daily amounts")
	}
	return nil
}

// CreateTimeOffRequest files a time off request on an employee's behalf.
func (c *Client) CreateTimeOffRequest(ctx context.Context, req NewTimeOffRequest) (*TimeOffRequest, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	var out TimeOffRequest
	if err := c.do(ctx, http.MethodPost, "/api/v1/time-off/requests", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TimeOffDecision is the note and bypass flag carried by an approval or denial.
type TimeOffDecision struct {
	// ManagerNote is recorded on the request, at most 1024 characters.
	ManagerNote string `json:"managerNote,omitempty"`
	// Bypass completes every remaining approval step rather than only the
	// caller's, and requires permission to bypass the other approvers. On a
	// denial it changes who may deny, not the outcome.
	Bypass bool `json:"bypass,omitempty"`
}

// ApproveTimeOffRequest approves a request.
func (c *Client) ApproveTimeOffRequest(ctx context.Context, id int, decision TimeOffDecision) (*TimeOffRequest, error) {
	return c.decide(ctx, id, "approvals", decision)
}

// DenyTimeOffRequest denies a request. A denial always discards the remaining
// approval steps.
func (c *Client) DenyTimeOffRequest(ctx context.Context, id int, decision TimeOffDecision) (*TimeOffRequest, error) {
	return c.decide(ctx, id, "denials", decision)
}

// CancelTimeOffRequest cancels a request.
func (c *Client) CancelTimeOffRequest(ctx context.Context, id int) (*TimeOffRequest, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bamboohr: time off request id is required")
	}
	var out TimeOffRequest
	path := "/api/v1/time-off/requests/" + strconv.Itoa(id) + "/cancellations"
	if err := c.do(ctx, http.MethodPost, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) decide(ctx context.Context, id int, action string, decision TimeOffDecision) (*TimeOffRequest, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bamboohr: time off request id is required")
	}
	var out TimeOffRequest
	path := "/api/v1/time-off/requests/" + strconv.Itoa(id) + "/" + action
	if err := c.do(ctx, http.MethodPost, path, nil, decision, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WhosOutEntry is one approved absence returned by WhosOut.
type WhosOutEntry struct {
	// ID is the time off request ID.
	ID         string `json:"id"`
	EmployeeID int    `json:"employeeId"`
	// TimeOffTypeID identifies the kind of time off.
	TimeOffTypeID int `json:"timeOffTypeId"`
	// Start and End are inclusive dates in the company timezone.
	Start string `json:"start"`
	End   string `json:"end"`
}

// WhosOutPerson is the display data WhosOut embeds when asked for it.
type WhosOutPerson struct {
	ID          int     `json:"id"`
	FirstName   string  `json:"firstName"`
	LastName    string  `json:"lastName"`
	DisplayName string  `json:"displayName"`
	JobTitle    *string `json:"jobTitle"`
	PhotoURL    *string `json:"photoUrl"`
}

// WhosOutOptions controls WhosOut. Start and End are required.
type WhosOutOptions struct {
	// Start and End bound the window, as YYYY-MM-DD.
	Start string
	End   string
	// EmployeeIDs, DepartmentIDs and LocationIDs narrow the results. They are
	// combined with AND and ignored when Filter is set.
	EmployeeIDs   []int
	DepartmentIDs []int
	LocationIDs   []int
	// Filter is a raw OData expression over employeeId, department, division
	// and location. When set, the ID slices above are ignored.
	Filter string
	// DirectReportsOnly limits the results to the caller's direct reports.
	DirectReportsOnly bool
	// IncludePersons populates the Persons map with each employee's name, job
	// title and photo, saving a lookup per employee.
	IncludePersons bool
	// Page is 1-based; zero means the first page.
	Page int
	// PageSize is the number of entries per page; zero uses the API default of
	// 100.
	PageSize int
}

func (o WhosOutOptions) query() (url.Values, error) {
	if o.Start == "" || o.End == "" {
		return nil, fmt.Errorf("bamboohr: start and end are required")
	}
	q := url.Values{"start": {o.Start}, "end": {o.End}}

	filter := o.Filter
	if filter == "" {
		f := NewFilter()
		addIDs(f, "employeeId", o.EmployeeIDs)
		addIDs(f, "department", o.DepartmentIDs)
		addIDs(f, "location", o.LocationIDs)
		filter = f.String()
	}
	if filter != "" {
		q.Set("filter", filter)
	}
	if o.DirectReportsOnly {
		q.Set("directReportsOnly", "true")
	}
	if o.IncludePersons {
		q.Set("includePersons", "true")
	}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(o.PageSize))
	}
	return q, nil
}

// WhosOutPage is one page of absences.
type WhosOutPage struct {
	Data []WhosOutEntry `json:"data"`
	// Persons is keyed by employee ID and is only populated when the call set
	// IncludePersons.
	Persons map[string]WhosOutPerson `json:"persons,omitempty"`
	Meta    PageMeta                 `json:"meta"`
}

// WhosOut lists the approved absences overlapping a date window.
//
// Unlike the older whos_out endpoint this one covers time off only; company
// holidays come from the holidays endpoints.
func (c *Client) WhosOut(ctx context.Context, opts WhosOutOptions) (*WhosOutPage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	var out WhosOutPage
	if err := c.get(ctx, "/api/v1/whos-out", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllWhosOut walks every page of absences in the window, merging the
// Persons maps when IncludePersons is set.
func (c *Client) ListAllWhosOut(ctx context.Context, opts WhosOutOptions) (*WhosOutPage, error) {
	merged := &WhosOutPage{}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	for {
		page, err := c.WhosOut(ctx, opts)
		if err != nil {
			return merged, err
		}
		merged.Data = append(merged.Data, page.Data...)
		if len(page.Persons) > 0 {
			if merged.Persons == nil {
				merged.Persons = make(map[string]WhosOutPerson, len(page.Persons))
			}
			for k, v := range page.Persons {
				merged.Persons[k] = v
			}
		}
		merged.Meta = page.Meta
		if !page.Meta.HasMore() || len(page.Data) == 0 {
			return merged, nil
		}
		opts.Page = page.Meta.Page + 1
	}
}

// TimeOffType is one kind of time off a company tracks.
type TimeOffType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Units is "hours" or "days".
	Units string  `json:"units"`
	Color *string `json:"color"`
	Icon  *string `json:"icon"`
	// Source is "internal", "remote" or "external".
	Source string `json:"source"`
}

// DefaultHours is a company's default working hours for one weekday.
type DefaultHours struct {
	Name string `json:"name"`
	// Amount is a decimal rendered as a string by the API.
	Amount string `json:"amount"`
}

// TimeOffTypes is the response from ListTimeOffTypes.
type TimeOffTypes struct {
	TimeOffTypes []TimeOffType  `json:"timeOffTypes"`
	DefaultHours []DefaultHours `json:"defaultHours"`
}

// ListTimeOffTypes returns the company's time off types and default daily
// hours.
//
// Pass requestableOnly to get only the types the API key's user may request
// time off under, which is what a request form needs.
func (c *Client) ListTimeOffTypes(ctx context.Context, requestableOnly bool) (*TimeOffTypes, error) {
	q := url.Values{}
	if requestableOnly {
		q.Set("mode", "request")
	}
	var out TimeOffTypes
	if err := c.get(ctx, "/api/v1/meta/time_off/types", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TimeOffPolicy is one time off policy.
type TimeOffPolicy struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	CategoryID int    `json:"categoryId"`
	// Type is "ACCRUING", "UNLIMITED" or "MANUAL".
	Type                  string  `json:"type"`
	AssignedEmployeeCount int     `json:"assignedEmployeeCount"`
	CreatedAt             string  `json:"createdAt"`
	UpdatedAt             *string `json:"updatedAt"`
}

// TimeOffPolicyList is one page of policies.
type TimeOffPolicyList struct {
	Data []TimeOffPolicy `json:"data"`
	Meta PageMeta        `json:"meta"`
}

// TimeOffPolicyListOptions controls ListTimeOffPolicies.
type TimeOffPolicyListOptions struct {
	// Filter is a raw OData expression over id, name, categoryId, type,
	// createdAt and updatedAt.
	Filter string
	// OrderBy sorts the results, for example "name asc".
	OrderBy string
	// Page is 1-based; zero means the first page.
	Page int
	// PageSize is the policies per page; zero uses the API default of 50.
	PageSize int
}

// ListTimeOffPolicies returns one page of the company's time off policies.
func (c *Client) ListTimeOffPolicies(ctx context.Context, opts TimeOffPolicyListOptions) (*TimeOffPolicyList, error) {
	q := url.Values{}
	if opts.Filter != "" {
		q.Set("filter", opts.Filter)
	}
	if opts.OrderBy != "" {
		q.Set("orderBy", opts.OrderBy)
	}
	if opts.Page > 0 {
		q.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(opts.PageSize))
	}
	var out TimeOffPolicyList
	if err := c.get(ctx, "/api/v1/time-off/policies", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TimeOffBalance is an employee's balance for one time off type.
type TimeOffBalance struct {
	// TimeOffType is the type's ID.
	TimeOffType string `json:"timeOffType"`
	Name        string `json:"name"`
	// Units is "hours" or "days".
	Units string `json:"units"`
	// Balance is a decimal rendered as a string by the API.
	Balance string `json:"balance"`
	// End is the date the balance was calculated as of.
	End string `json:"end"`
	// PolicyType is "accruing", "discretionary" or "manual".
	PolicyType string `json:"policyType"`
	// UsedYearToDate is a decimal rendered as a string by the API.
	UsedYearToDate string `json:"usedYearToDate"`
}

// GetTimeOffBalance returns an employee's time off balances.
//
// asOf projects the balance to a date (YYYY-MM-DD), which is how an accruing
// policy's future balance is checked; an empty asOf uses today. precision is
// the number of decimal places, defaulting to 2 when zero.
func (c *Client) GetTimeOffBalance(ctx context.Context, employeeID, asOf string, precision int) ([]TimeOffBalance, error) {
	if strings.TrimSpace(employeeID) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	q := url.Values{}
	if asOf != "" {
		q.Set("end", asOf)
	}
	if precision > 0 {
		q.Set("precision", strconv.Itoa(precision))
	}
	var out []TimeOffBalance
	path := "/api/v1/employees/" + url.PathEscape(employeeID) + "/time_off/calculator"
	if err := c.get(ctx, path, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func addIDs(f *Filter, field string, ids []int) {
	switch len(ids) {
	case 0:
	case 1:
		f.Eq(field, ids[0])
	default:
		f.In(field, toAny(ids)...)
	}
}

func toAny(ids []int) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}
