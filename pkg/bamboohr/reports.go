package bamboohr

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Report identifies a saved custom report.
type Report struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Pagination is the page counter the report endpoints return.
type Pagination struct {
	TotalRecords int     `json:"total_records"`
	CurrentPage  int     `json:"current_page"`
	TotalPages   int     `json:"total_pages"`
	NextPage     *string `json:"next_page"`
	PrevPage     *string `json:"prev_page"`
}

// HasMore reports whether a further page exists after this one.
func (p Pagination) HasMore() bool { return p.CurrentPage > 0 && p.CurrentPage < p.TotalPages }

// ReportList is one page of the company's saved reports.
type ReportList struct {
	Reports    []Report   `json:"reports"`
	Pagination Pagination `json:"pagination"`
}

// ListReports returns the saved custom reports the API key's user can see.
//
// page is 1-based and pageSize defaults to 500 at the API; zero leaves each to
// its default.
func (c *Client) ListReports(ctx context.Context, page, pageSize int) (*ReportList, error) {
	var out ReportList
	if err := c.get(ctx, "/api/v1/custom-reports", pageQuery(page, pageSize), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllReports walks every page of saved reports.
func (c *Client) ListAllReports(ctx context.Context, pageSize int) ([]Report, error) {
	var all []Report
	for page := 1; ; page++ {
		got, err := c.ListReports(ctx, page, pageSize)
		if err != nil {
			return all, err
		}
		all = append(all, got.Reports...)
		if !got.Pagination.HasMore() || len(got.Reports) == 0 {
			return all, nil
		}
	}
}

// ReportColumn describes one column of a report's output.
type ReportColumn struct {
	// Name is the key this column uses in every data row.
	Name string `json:"name"`
	// Label is the heading shown in BambooHR, including any renaming the
	// customer did. It is nil when unavailable, in which case display Name.
	Label *string `json:"label"`
}

// Heading returns the column's display heading, falling back to its key.
func (c ReportColumn) Heading() string {
	if c.Label != nil && *c.Label != "" {
		return *c.Label
	}
	return c.Name
}

// ReportAggregation is one aggregate row a report computes.
type ReportAggregation struct {
	Field           string         `json:"field"`
	AggregationType string         `json:"aggregationType"`
	Groups          map[string]any `json:"groups"`
	All             int            `json:"all"`
}

// ReportData is one page of a saved report's output.
type ReportData struct {
	// Columns are the report's columns in the order it shows them.
	Columns []ReportColumn `json:"fields"`
	// Rows are the report's records, keyed by column name.
	Rows         []map[string]any    `json:"data"`
	Aggregations []ReportAggregation `json:"aggregations,omitempty"`
	Pagination   Pagination          `json:"pagination"`
}

// GetReport runs a saved custom report and returns one page of its output.
//
// page is 1-based and pageSize defaults to 500 at the API; zero leaves each to
// its default.
func (c *Client) GetReport(ctx context.Context, reportID, page, pageSize int) (*ReportData, error) {
	if reportID <= 0 {
		return nil, fmt.Errorf("bamboohr: report id is required")
	}
	var out ReportData
	path := "/api/v1/custom-reports/" + strconv.Itoa(reportID)
	if err := c.get(ctx, path, pageQuery(page, pageSize), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAllReportData runs a saved report and walks every page of its output. The
// columns and aggregations come from the first page.
func (c *Client) GetAllReportData(ctx context.Context, reportID, pageSize int) (*ReportData, error) {
	merged := &ReportData{}
	for page := 1; ; page++ {
		got, err := c.GetReport(ctx, reportID, page, pageSize)
		if err != nil {
			return merged, err
		}
		if page == 1 {
			merged.Columns = got.Columns
			merged.Aggregations = got.Aggregations
		}
		merged.Rows = append(merged.Rows, got.Rows...)
		merged.Pagination = got.Pagination
		if !got.Pagination.HasMore() || len(got.Rows) == 0 {
			return merged, nil
		}
	}
}

// pageQuery renders the page/page_size pair the report endpoints use, omitting
// either when it is left at zero.
func pageQuery(page, pageSize int) url.Values {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
	return q
}
