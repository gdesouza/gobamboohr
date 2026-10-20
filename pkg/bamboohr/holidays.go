package bamboohr

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Holiday is a holiday the company observes.
type Holiday struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	StartDate string `json:"startDate"`
	// EndDate is nil for a single-day holiday.
	EndDate *string `json:"endDate"`
	// IsPublic reports whether the holiday shows on every employee's calendar,
	// regardless of who it applies to.
	IsPublic bool `json:"isPublic"`
	// CountryCodes restricts the holiday to these ISO 3166-1 alpha-2 codes. An
	// empty slice means no country restriction.
	CountryCodes []string `json:"countryCodes"`
	// Audience says which employees the holiday applies to.
	Audience HolidayAudience `json:"audience"`
	// Pay is the holiday's pay treatment, nil when it has none.
	Pay *HolidayPay `json:"holidayPay"`
	// GlobalHolidayUUID identifies the catalog entry this holiday came from,
	// nil for a holiday the company defined itself.
	GlobalHolidayUUID *string `json:"globalHolidayUuid"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
}

// Days returns the number of days the holiday covers, counting both ends. A
// holiday with no end date is one day.
func (h Holiday) Days() int {
	if h.EndDate == nil || *h.EndDate == "" {
		return 1
	}
	return inclusiveDays(h.StartDate, *h.EndDate)
}

// Custom reports whether the company defined the holiday itself rather than
// seeding it from the global catalog.
func (h Holiday) Custom() bool {
	return h.GlobalHolidayUUID == nil || *h.GlobalHolidayUUID == ""
}

// HolidayAudience says which employees a holiday applies to.
type HolidayAudience struct {
	// Mode is "ALL_EMPLOYEES", "FILTERED" or "SPECIFIC_EMPLOYEES".
	Mode string `json:"mode"`
	// FilterListValueIDs narrow the audience in FILTERED mode.
	FilterListValueIDs []int `json:"filterListValueIds"`
	// SpecificEmployeeIDs are the audience in SPECIFIC_EMPLOYEES mode.
	SpecificEmployeeIDs []int `json:"specificEmployeeIds"`
	// AdditionalEmployeeIDs are included on top of the mode's audience.
	AdditionalEmployeeIDs []int `json:"additionalEmployeeIds"`
	// ExemptEmployeeIDs are excluded from the mode's audience.
	ExemptEmployeeIDs []int `json:"exemptEmployeeIds"`
}

// HolidayPay is a holiday's pay treatment.
type HolidayPay struct {
	// Mode is "HOURS_BY_STATUS" or "MULTIPLIER".
	Mode string `json:"mode"`
	// DefaultHours applies to employment statuses with no override, in
	// HOURS_BY_STATUS mode. It is a decimal rendered as a string.
	DefaultHours *string `json:"defaultHours"`
	// Multiplier scales normal pay in MULTIPLIER mode.
	Multiplier *string `json:"multiplier"`
}

// HolidayList is one page of company holidays.
type HolidayList struct {
	Data []Holiday `json:"data"`
	Meta PageMeta  `json:"meta"`
}

// HolidayListOptions controls ListHolidays.
//
// The convenience fields build an OData filter. Filter overrides them for
// expressions they cannot express.
type HolidayListOptions struct {
	// From and To bound the holiday start date, as YYYY-MM-DD.
	From string
	To   string
	// PublicOnly limits the results to holidays shown on every calendar.
	PublicOnly bool
	// Filter is a raw OData expression over name, startDate, endDate, isPublic
	// and globalHolidayUuid. When set, the fields above are ignored.
	Filter string
	// OrderBy sorts the results. The API defaults to "startDate asc".
	OrderBy string
	// Select limits which properties each holiday carries, which is how a
	// caller keeps the payload small.
	Select []string
	// Page is 1-based; zero means the first page.
	Page int
	// PageSize is the holidays per page; zero uses the API default of 20.
	PageSize int
}

func (o HolidayListOptions) query() url.Values {
	q := url.Values{}

	filter := o.Filter
	if filter == "" {
		f := NewFilter()
		if o.From != "" {
			f.Ge("startDate", o.From)
		}
		if o.To != "" {
			f.Le("startDate", o.To)
		}
		if o.PublicOnly {
			f.Eq("isPublic", true)
		}
		filter = f.String()
	}
	if filter != "" {
		q.Set("filter", filter)
	}
	if o.OrderBy != "" {
		q.Set("orderBy", o.OrderBy)
	}
	if len(o.Select) > 0 {
		q.Set("select", strings.Join(o.Select, ","))
	}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(o.PageSize))
	}
	return q
}

// ListHolidays returns one page of the company's holidays.
func (c *Client) ListHolidays(ctx context.Context, opts HolidayListOptions) (*HolidayList, error) {
	var out HolidayList
	if err := c.get(ctx, "/api/v1/holidays", opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllHolidays walks every page of the company's holidays. The API's default
// page size is small, so a whole year usually spans several pages.
func (c *Client) ListAllHolidays(ctx context.Context, opts HolidayListOptions) ([]Holiday, error) {
	var all []Holiday
	if opts.Page <= 0 {
		opts.Page = 1
	}
	for {
		page, err := c.ListHolidays(ctx, opts)
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

// GetHoliday returns one company holiday.
func (c *Client) GetHoliday(ctx context.Context, id int) (*Holiday, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bamboohr: holiday id is required")
	}
	var out Holiday
	if err := c.get(ctx, "/api/v1/holidays/"+strconv.Itoa(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CatalogHoliday is a read-only entry in BambooHR's global holiday catalog,
// which is what a company holiday can be seeded from.
type CatalogHoliday struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	// Type is "PUBLIC", "CULTURAL", "RELIGIOUS" or "REGIONAL".
	Type      string `json:"type"`
	StartDate string `json:"startDate"`
	// EndDate is nil for a single-day entry.
	EndDate *string `json:"endDate"`
	// CountryCode is an ISO 3166-1 alpha-2 code.
	CountryCode string `json:"countryCode"`
	// Source is the upstream catalog the entry came from.
	Source string `json:"source"`
}

// CatalogHolidayList is one page of the global catalog.
type CatalogHolidayList struct {
	Data []CatalogHoliday `json:"data"`
	Meta PageMeta         `json:"meta"`
}

// CatalogHolidayOptions controls ListCatalogHolidays.
type CatalogHolidayOptions struct {
	// CountryCode limits the catalog to one country, as an ISO 3166-1 alpha-2
	// code. The catalog spans every country without it.
	CountryCode string
	// Year limits the catalog to one calendar year.
	Year int
	// Filter is a raw OData expression.
	Filter string
	// OrderBy sorts the results. The API defaults to "startDate asc".
	OrderBy string
	// Page is 1-based; zero means the first page.
	Page int
	// PageSize is the entries per page; zero uses the API default of 20.
	PageSize int
}

// ListCatalogHolidays returns one page of BambooHR's global holiday catalog.
func (c *Client) ListCatalogHolidays(ctx context.Context, opts CatalogHolidayOptions) (*CatalogHolidayList, error) {
	q := url.Values{}
	if opts.CountryCode != "" {
		q.Set("countryCode", strings.ToUpper(opts.CountryCode))
	}
	if opts.Year > 0 {
		q.Set("year", strconv.Itoa(opts.Year))
	}
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

	var out CatalogHolidayList
	if err := c.get(ctx, "/api/v1/holidays/catalog", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCatalogHoliday returns one entry from the global holiday catalog.
func (c *Client) GetCatalogHoliday(ctx context.Context, uuid string) (*CatalogHoliday, error) {
	if strings.TrimSpace(uuid) == "" {
		return nil, fmt.Errorf("bamboohr: catalog holiday uuid is required")
	}
	var out CatalogHoliday
	if err := c.get(ctx, "/api/v1/holidays/catalog/"+url.PathEscape(uuid), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
