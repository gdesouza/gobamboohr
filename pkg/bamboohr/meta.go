package bamboohr

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
)

// Field describes one employee field in the company's account.
type Field struct {
	// ID is a numeric field ID, or a dotted string such as "4340.4" for a
	// subfield.
	ID string `json:"-"`
	// Name is the field's display name.
	Name string `json:"name"`
	// Type is the field's data type, such as text, date or list.
	Type string `json:"type"`
	// Alias is the name to use in a fields parameter. It is empty for fields
	// that have no alias, which must be addressed by ID instead.
	Alias string `json:"alias,omitempty"`
	// Deprecated marks fields that should no longer be used.
	Deprecated bool `json:"deprecated,omitempty"`
}

// UnmarshalJSON accepts the field ID as either a JSON string or a number, both
// of which the API returns.
func (f *Field) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID         json.RawMessage `json:"id"`
		Name       string          `json:"name"`
		Type       string          `json:"type"`
		Alias      string          `json:"alias"`
		Deprecated bool            `json:"deprecated"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Name, f.Type, f.Alias, f.Deprecated = raw.Name, raw.Type, raw.Alias, raw.Deprecated
	f.ID = flexString(raw.ID)
	return nil
}

// Selector returns the name to pass in a fields parameter: the alias when the
// field has one, otherwise its ID.
func (f Field) Selector() string {
	if f.Alias != "" {
		return f.Alias
	}
	return f.ID
}

// ListFields returns every employee field in the account, which is how a caller
// discovers the names to pass to GetEmployee.
func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	var out []Field
	if err := c.get(ctx, "/api/v1/meta/fields", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TableField is one column of an employee table.
type TableField struct {
	ID    string `json:"-"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Type  string `json:"type"`
}

// UnmarshalJSON accepts the field ID as either a JSON string or a number.
func (t *TableField) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID    json.RawMessage `json:"id"`
		Name  string          `json:"name"`
		Alias string          `json:"alias"`
		Type  string          `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.Name, t.Alias, t.Type = raw.Name, raw.Alias, raw.Type
	t.ID = flexString(raw.ID)
	return nil
}

// Table is one employee table and its columns.
type Table struct {
	// Alias is the table name to pass to GetEmployeeTable.
	Alias  string       `json:"alias"`
	Fields []TableField `json:"fields"`
}

// ListTables returns every employee table in the account, including custom
// ones, which is how a caller discovers the names GetEmployeeTable accepts.
func (c *Client) ListTables(ctx context.Context) ([]Table, error) {
	var out []Table
	if err := c.get(ctx, "/api/v1/meta/tables", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListOption is one selectable value of a list field.
type ListOption struct {
	ID       string `json:"-"`
	Name     string `json:"name"`
	Archived string `json:"archived,omitempty"`
}

// UnmarshalJSON accepts the option ID as either a JSON string or a number, and
// the option value under either of the keys the API uses for it.
func (o *ListOption) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       json.RawMessage `json:"id"`
		Name     string          `json:"name"`
		Value    string          `json:"value"`
		Archived string          `json:"archived"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	o.ID = flexString(raw.ID)
	o.Name = raw.Name
	if o.Name == "" {
		o.Name = raw.Value
	}
	o.Archived = raw.Archived
	return nil
}

// IsArchived reports whether the option has been archived and so can no longer
// be selected.
func (o ListOption) IsArchived() bool { return yesNo(o.Archived) }

// ListField is one list field and its options.
type ListField struct {
	ID      int    `json:"id"`
	FieldID int    `json:"fieldId"`
	Name    string `json:"name"`
	// Alias is the name to use in a fields parameter; nil when the field has
	// none.
	Alias *string `json:"alias"`
	// Manageable is "yes" when the options can be changed through the API.
	Manageable string `json:"manageable"`
	// Multiple is "yes" when the field accepts more than one value.
	Multiple string       `json:"multiple"`
	Options  []ListOption `json:"options"`
}

// IsManageable reports whether the field's options can be changed through the
// API.
func (f ListField) IsManageable() bool { return yesNo(f.Manageable) }

// AcceptsMultiple reports whether the field accepts more than one value.
func (f ListField) AcceptsMultiple() bool { return yesNo(f.Multiple) }

// ListLists returns every list field in the account with its options, which is
// how a caller finds the accepted values for a list field.
func (c *Client) ListLists(ctx context.Context) ([]ListField, error) {
	var out []ListField
	// The endpoint answers XML unless asked otherwise; both the Accept header
	// and this parameter are honoured, and sending both is harmless.
	if err := c.get(ctx, "/api/v1/meta/lists", url.Values{"format": {"json"}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// User is a BambooHR user account.
type User struct {
	ID int `json:"id"`
	// EmployeeID is the linked employee record, 0 when the user has none.
	EmployeeID int    `json:"employeeId"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	// Email is resolved in priority order: work, home, then account email.
	Email string `json:"email"`
	// Status is "enabled" or "disabled".
	Status string `json:"status"`
	// LastLogin is empty when the user has never logged in.
	LastLogin string `json:"lastLogin,omitempty"`
}

// ListUsers returns the company's users, sorted by ID.
//
// status filters by account status ("enabled" or "disabled"); an empty status
// returns all of them.
func (c *Client) ListUsers(ctx context.Context, status string) ([]User, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	// The API keys this response by user ID rather than returning an array.
	var byID map[string]User
	if err := c.get(ctx, "/api/v1/meta/users", q, &byID); err != nil {
		return nil, err
	}

	users := make([]User, 0, len(byID))
	for _, u := range byID {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users, nil
}

// Company describes the company the API key belongs to.
type Company struct {
	// ID is sent as a quoted number by the live API even though the published
	// spec types it as an integer, so it is decoded leniently.
	ID   FlexInt `json:"id"`
	Name string  `json:"name"`
	// Domain is the company's BambooHR subdomain.
	Domain string `json:"domain"`
	// BaseAPIURL is the gateway URL for this tenant, used by the older
	// gateway-style API paths.
	BaseAPIURL string `json:"baseApiUrl"`
}

// GetCompany returns the company's identity and API gateway URL. It needs no
// arguments, which makes it a cheap way to check that a key works.
func (c *Client) GetCompany(ctx context.Context) (*Company, error) {
	var out Company
	if err := c.get(ctx, "/api/v1/meta/company", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// flexString renders a JSON value that may be quoted or numeric as a string.
func flexString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}
