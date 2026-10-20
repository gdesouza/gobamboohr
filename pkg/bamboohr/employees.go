package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Employee is a single employee record.
//
// BambooHR returns only the fields a request asked for, so most of these are
// empty on any given response. Fields holds every key exactly as the API
// returned it, including custom fields that have no typed counterpart here.
type Employee struct {
	// ID is the internal employee ID, returned by GetEmployee.
	ID string `json:"id,omitempty"`
	// EmployeeID is the same internal ID under the name ListEmployees uses.
	EmployeeID string `json:"employeeId,omitempty"`
	// EmployeeNumber is the editable, HR-visible "Employee #". It is not the
	// identifier other endpoints accept; use ID for that.
	EmployeeNumber string `json:"employeeNumber,omitempty"`
	FirstName      string `json:"firstName,omitempty"`
	LastName       string `json:"lastName,omitempty"`
	PreferredName  string `json:"preferredName,omitempty"`
	MiddleName     string `json:"middleName,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	// JobTitle is spelled jobTitle by GetEmployee and jobTitleName by
	// ListEmployees; either fills this field.
	JobTitle         string `json:"jobTitle,omitempty"`
	Department       string `json:"department,omitempty"`
	Division         string `json:"division,omitempty"`
	Location         string `json:"location,omitempty"`
	Status           string `json:"status,omitempty"`
	WorkEmail        string `json:"workEmail,omitempty"`
	HomeEmail        string `json:"homeEmail,omitempty"`
	WorkPhone        string `json:"workPhone,omitempty"`
	MobilePhone      string `json:"mobilePhone,omitempty"`
	HireDate         string `json:"hireDate,omitempty"`
	TerminationDate  string `json:"terminationDate,omitempty"`
	EmploymentStatus string `json:"employmentStatus,omitempty"`
	// ReportsToName is spelled supervisor by GetEmployee and reportsToName by
	// the report and dataset endpoints; either fills this field.
	ReportsToName string `json:"reportsToName,omitempty"`
	ReportsToID   string `json:"reportsToId,omitempty"`
	PhotoURL      string `json:"photoUrl,omitempty"`

	// RestrictedFields names the requested fields the API key's user may not
	// read. Their values come back null.
	RestrictedFields []string `json:"-"`

	// Fields holds every key from the response, keyed by field name or alias.
	Fields map[string]any `json:"-"`
}

// Key returns the internal employee ID, whichever name the endpoint used for
// it.
func (e Employee) Key() string {
	if e.ID != "" {
		return e.ID
	}
	return e.EmployeeID
}

// Field returns a field's value as a display string, or "" when the field was
// not returned. It reads from Fields, so it reaches custom fields too.
func (e Employee) Field(name string) string {
	if e.Fields == nil {
		return ""
	}
	return FormatValue(e.Fields[name])
}

// UnmarshalJSON decodes an employee into both its typed fields and Fields.
func (e *Employee) UnmarshalJSON(data []byte) error {
	type alias Employee
	var typed alias
	if err := json.Unmarshal(data, &typed); err != nil {
		return err
	}
	*e = Employee(typed)

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Fields = raw

	// _restrictedFields is carried separately so it does not look like a field
	// value in Fields-driven output.
	if restricted, ok := raw["_restrictedFields"].([]any); ok {
		for _, v := range restricted {
			if s, ok := v.(string); ok {
				e.RestrictedFields = append(e.RestrictedFields, s)
			}
		}
		delete(e.Fields, "_restrictedFields")
	}
	e.resolveAliases()
	return nil
}

// resolveAliases fills typed fields whose key the responding endpoint spells
// differently.
//
// The employee endpoints do not agree on names for the same concept: a job
// title is jobTitle from GetEmployee but jobTitleName from ListEmployees, and a
// manager is supervisor from one and reportsToName from another. A struct tag
// can only name one of them, so the alternatives are resolved here rather than
// leaving a typed field silently empty for callers who picked the wrong
// endpoint.
func (e *Employee) resolveAliases() {
	pick := func(dst *string, keys ...string) {
		if *dst != "" {
			return
		}
		for _, key := range keys {
			if s, ok := e.Fields[key].(string); ok && s != "" {
				*dst = s
				return
			}
		}
	}
	pick(&e.JobTitle, "jobTitle", "jobTitleName")
	pick(&e.ReportsToName, "supervisor", "reportsToName")
	pick(&e.ReportsToID, "supervisorId", "reportsToId")
	pick(&e.EmployeeID, "employeeId")
	pick(&e.Status, "status", "employmentStatus")
}

// MarshalJSON writes the record back out as the API returned it, so custom
// fields survive a round trip.
func (e Employee) MarshalJSON() ([]byte, error) {
	if e.Fields == nil {
		type alias Employee
		return json.Marshal(alias(e))
	}
	return json.Marshal(e.Fields)
}

// DefaultEmployeeFields is a general-purpose field selection for GetEmployee:
// who someone is, what they do, how to reach them and when they joined.
//
// GetEmployee returns only the fields a request names, so a call with none
// comes back with nothing but the ID. This set exists so that the common case
// does not have to be spelled out. Names the account does not have, or that the
// API key may not read, are silently dropped by BambooHR, so a generous default
// costs nothing.
//
// The spellings are the ones the single-employee endpoint uses, which are not
// always the ones ListEmployees returns.
var DefaultEmployeeFields = []string{
	"employeeNumber",
	"firstName",
	"lastName",
	"preferredName",
	"displayName",
	"jobTitle",
	"department",
	"division",
	"location",
	"supervisor",
	"workEmail",
	"workPhone",
	"mobilePhone",
	"status",
	"hireDate",
}

// GetEmployeeOptions selects which fields GetEmployee returns.
type GetEmployeeOptions struct {
	// Fields is the list of field names, numeric IDs or custom-field aliases to
	// return. BambooHR allows at most 400 and silently drops names it does not
	// recognise. A request with no fields comes back with only the ID.
	Fields []string
	// OnlyCurrent limits values from historical tables to the ones in effect
	// today. It defaults to true at the API; set it to false explicitly to
	// include future-dated rows.
	OnlyCurrent *bool
}

// GetEmployee returns one employee by internal employee ID.
func (c *Client) GetEmployee(ctx context.Context, id string, opts GetEmployeeOptions) (*Employee, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	q := url.Values{}
	if len(opts.Fields) > 0 {
		q.Set("fields", strings.Join(opts.Fields, ","))
	}
	if opts.OnlyCurrent != nil {
		q.Set("onlyCurrent", strconv.FormatBool(*opts.OnlyCurrent))
	}

	var emp Employee
	if err := c.get(ctx, "/api/v1/employees/"+url.PathEscape(id), q, &emp); err != nil {
		return nil, err
	}
	return &emp, nil
}

// EmployeeListOptions controls ListEmployees.
type EmployeeListOptions struct {
	// Filters are matched as filter[name]=value and combined with AND. Text
	// fields match on a case-insensitive substring; IDs, enums, booleans and
	// dates match exactly. See the BambooHR docs for which fields are
	// filterable: an unknown or unfilterable name returns 422.
	Filters map[string]string
	// IDs restricts the response to these internal employee IDs.
	IDs []string
	// Fields adds fields beyond the default set (employeeId, firstName,
	// lastName, preferredName, photoUrl, jobTitleName, status).
	Fields []string
	// Sort is a comma-separated list of sortable fields, each optionally
	// prefixed with "-" for descending order. Sortable: employeeId, firstName,
	// lastName, preferredName, jobTitleName, status.
	Sort string
	// Limit is the page size, up to 2500. Zero uses the API default of 250.
	Limit int
	// After and Before page forward and backward using a cursor from a previous
	// response's Meta.Page. Setting both is an error at the API.
	After  string
	Before string
}

func (o EmployeeListOptions) query() url.Values {
	q := url.Values{}
	for k, v := range o.Filters {
		q.Set("filter["+k+"]", v)
	}
	if len(o.IDs) > 0 {
		// The comma-separated form avoids the repeated-key spelling, which not
		// every HTTP stack preserves.
		q.Set("filter[ids]", strings.Join(o.IDs, ","))
	}
	if len(o.Fields) > 0 {
		q.Set("fields", strings.Join(o.Fields, ","))
	}
	if o.Sort != "" {
		q.Set("sort", o.Sort)
	}
	if o.Limit > 0 {
		q.Set("page[limit]", strconv.Itoa(o.Limit))
	}
	if o.After != "" {
		q.Set("page[after]", o.After)
	}
	if o.Before != "" {
		q.Set("page[before]", o.Before)
	}
	return q
}

// EmployeeList is one page of employees.
type EmployeeList struct {
	Data []Employee       `json:"data"`
	Meta EmployeeListMeta `json:"meta"`
}

// EmployeeListMeta carries the total count and the cursors for the next and
// previous pages.
type EmployeeListMeta struct {
	Total int `json:"total"`
	Page  struct {
		Limit      int    `json:"limit"`
		NextCursor string `json:"nextCursor"`
		PrevCursor string `json:"prevCursor"`
	} `json:"page"`
}

// ListEmployees returns one page of employees.
//
// Use Meta.Page.NextCursor as the next call's After to walk the whole list, or
// call ListAllEmployees to do that in one step.
func (c *Client) ListEmployees(ctx context.Context, opts EmployeeListOptions) (*EmployeeList, error) {
	var out EmployeeList
	if err := c.get(ctx, "/api/v1/employees", opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllEmployees follows the cursor until every matching employee has been
// returned. Callers should set a context deadline: a large company is many
// round trips.
func (c *Client) ListAllEmployees(ctx context.Context, opts EmployeeListOptions) ([]Employee, error) {
	var all []Employee
	// Before pages backward, which would fight the forward walk below.
	opts.Before = ""
	for {
		page, err := c.ListEmployees(ctx, opts)
		if err != nil {
			return all, err
		}
		all = append(all, page.Data...)
		next := page.Meta.Page.NextCursor
		if next == "" || len(page.Data) == 0 {
			return all, nil
		}
		opts.After = next
	}
}

// CreateEmployee adds an employee. The fields map is sent as the request body;
// BambooHR requires at least firstName and lastName.
//
// It returns the new employee's internal ID, taken from the Location header.
func (c *Client) CreateEmployee(ctx context.Context, fields map[string]any) (string, error) {
	if len(fields) == 0 {
		return "", fmt.Errorf("bamboohr: at least one field is required to create an employee")
	}
	resp, err := c.send(ctx, http.MethodPost, "/api/v1/employees", nil, fields, acceptJSON)
	if err != nil {
		return "", err
	}
	defer drain(resp)

	// The created employee's ID is the last path segment of Location.
	if loc := resp.Header.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil {
			if id := lastPathSegment(u.Path); id != "" {
				return id, nil
			}
		}
	}
	// Some responses carry the record instead of, or as well as, the header.
	var emp Employee
	if body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)); err == nil && len(body) > 0 {
		if json.Unmarshal(body, &emp) == nil && emp.Key() != "" {
			return emp.Key(), nil
		}
	}
	return "", fmt.Errorf("bamboohr: employee created but the response did not identify it")
}

// UpdateEmployee changes the given fields on an employee, leaving the rest
// alone.
func (c *Client) UpdateEmployee(ctx context.Context, id string, fields map[string]any) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("bamboohr: employee id is required")
	}
	if len(fields) == 0 {
		return fmt.Errorf("bamboohr: at least one field is required to update an employee")
	}
	return c.do(ctx, http.MethodPost, "/api/v1/employees/"+url.PathEscape(id), nil, fields, nil)
}

// Directory is the company's published employee directory.
type Directory struct {
	// Fields describes the columns every employee object carries. The set is
	// fixed by the company's directory configuration.
	Fields []DirectoryField `json:"fields"`
	// Employees holds one record per directory employee.
	Employees []Employee `json:"employees"`
}

// DirectoryField describes one directory column.
type DirectoryField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// GetDirectory returns the published employee directory. onlyCurrent may be nil
// to accept the API default of true.
func (c *Client) GetDirectory(ctx context.Context, onlyCurrent *bool) (*Directory, error) {
	q := url.Values{}
	if onlyCurrent != nil {
		q.Set("onlyCurrent", strconv.FormatBool(*onlyCurrent))
	}
	var dir Directory
	if err := c.get(ctx, "/api/v1/employees/directory", q, &dir); err != nil {
		return nil, err
	}
	return &dir, nil
}

// ChangeType selects which changes GetChangedEmployees reports.
type ChangeType string

// The change types BambooHR accepts.
const (
	ChangeInserted ChangeType = "inserted"
	ChangeUpdated  ChangeType = "updated"
	ChangeDeleted  ChangeType = "deleted"
	ChangeAll      ChangeType = "all"
)

// EmployeeChange records one employee's most recent change.
type EmployeeChange struct {
	ID string `json:"id"`
	// Action is "Inserted", "Updated" or "Deleted".
	Action      string `json:"action"`
	LastChanged string `json:"lastChanged"`
}

// ChangedEmployees is the result of GetChangedEmployees.
type ChangedEmployees struct {
	// Latest is the newest change timestamp in this response. Pass it as the
	// next poll's since value.
	Latest string `json:"latest"`
	// Employees is keyed by internal employee ID.
	Employees map[string]EmployeeChange `json:"employees"`
}

// GetChangedEmployees lists the employees changed since a point in time, which
// is how an integration polls for updates instead of re-reading everything.
func (c *Client) GetChangedEmployees(ctx context.Context, since time.Time, changeType ChangeType) (*ChangedEmployees, error) {
	if since.IsZero() {
		return nil, fmt.Errorf("bamboohr: since is required")
	}
	q := url.Values{"since": {since.UTC().Format(time.RFC3339)}}
	if changeType != "" {
		q.Set("type", string(changeType))
	}
	var out ChangedEmployees
	if err := c.get(ctx, "/api/v1/employees/changed", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TableRow is one row of an employee table. Its columns depend on the table and
// on the API key user's field permissions, so every value is kept as returned.
type TableRow map[string]any

// ID returns the row's identifier.
func (r TableRow) ID() string { return FormatValue(r["id"]) }

// GetEmployeeTable returns the rows of one of an employee's tables, such as
// jobInfo, compensation or employeeEducation. Use ListTables to discover the
// table names a company has, including custom ones.
func (c *Client) GetEmployeeTable(ctx context.Context, id, table string) ([]TableRow, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	if strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("bamboohr: table name is required")
	}
	var rows []TableRow
	path := "/api/v1/employees/" + url.PathEscape(id) + "/tables/" + url.PathEscape(table)
	if err := c.get(ctx, path, nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// PhotoSize names one of the sizes BambooHR renders employee photos at.
type PhotoSize string

// The photo sizes the API serves.
const (
	PhotoOriginal PhotoSize = "original"
	PhotoLarge    PhotoSize = "large"
	PhotoMedium   PhotoSize = "medium"
	PhotoSmall    PhotoSize = "small"
	PhotoXS       PhotoSize = "xs"
	PhotoTiny     PhotoSize = "tiny"
)

// GetEmployeePhoto streams an employee's photo into w at the requested size.
func (c *Client) GetEmployeePhoto(ctx context.Context, id string, size PhotoSize, w io.Writer) (*Download, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	if size == "" {
		size = PhotoMedium
	}
	path := "/api/v1/employees/" + url.PathEscape(id) + "/photo/" + url.PathEscape(string(size))
	return c.download(ctx, path, nil, w)
}

func lastPathSegment(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
