package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newTimeOffCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "timeoff <command>",
		Aliases:               []string{"time-off", "pto"},
		Short:                 "Read and act on time off",
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newWhosOutCommand(a),
		newTimeOffRequestsCommand(a),
		newTimeOffRequestCommand(a),
		newTimeOffCreateCommand(a),
		newTimeOffDecisionCommand(a, "approve"),
		newTimeOffDecisionCommand(a, "deny"),
		newTimeOffCancelCommand(a),
		newTimeOffTypesCommand(a),
		newTimeOffPoliciesCommand(a),
		newTimeOffBalanceCommand(a),
	)
	return cmd
}

func newWhosOutCommand(a *app) *cobra.Command {
	var (
		start         string
		end           string
		employeeIDs   []int
		departmentIDs []int
		locationIDs   []int
		filter        string
		directReports bool
		withNames     bool
		all           bool
		page          int
		pageSize      int
	)

	cmd := &cobra.Command{
		Use:     "whos-out",
		Aliases: []string{"whosout", "out"},
		Short:   "List who is out over a date range",
		Long: `List the approved absences overlapping a date range.

--start defaults to today and --end to two weeks out, matching what the
BambooHR calendar shows. Names are fetched alongside the absences by default;
pass --names=false to return employee IDs only and save the lookup.

This covers time off only. Company holidays are a separate resource.`,
		Example: `  bamboohr timeoff whos-out
  bamboohr timeoff whos-out --start 2026-10-01 --end 2026-10-31
  bamboohr timeoff whos-out --department 12 --output csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			// The endpoint requires both bounds; defaulting them keeps the bare
			// command useful.
			if start == "" {
				start = time.Now().Format(bamboohr.DateLayout)
			}
			if end == "" {
				end = time.Now().AddDate(0, 0, 14).Format(bamboohr.DateLayout)
			}
			if err := validateDates(map[string]string{"--start": start, "--end": end}); err != nil {
				return err
			}

			opts := bamboohr.WhosOutOptions{
				Start:             start,
				End:               end,
				EmployeeIDs:       employeeIDs,
				DepartmentIDs:     departmentIDs,
				LocationIDs:       locationIDs,
				Filter:            filter,
				DirectReportsOnly: directReports,
				IncludePersons:    withNames,
				Page:              page,
				PageSize:          pageSize,
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			var result *bamboohr.WhosOutPage
			if all {
				result, err = client.ListAllWhosOut(ctx, opts)
			} else {
				result, err = client.WhosOut(ctx, opts)
			}
			if err != nil {
				return err
			}
			return a.printer().Print(result, whosOutTable(result))
		},
	}

	f := cmd.Flags()
	f.StringVar(&start, "start", "", "first day of the range, YYYY-MM-DD (default today)")
	f.StringVar(&end, "end", "", "last day of the range, YYYY-MM-DD (default two weeks out)")
	f.IntSliceVar(&employeeIDs, "employee", nil, "restrict to these employee IDs")
	f.IntSliceVar(&departmentIDs, "department", nil, "restrict to these department IDs")
	f.IntSliceVar(&locationIDs, "location", nil, "restrict to these location IDs")
	f.StringVar(&filter, "filter", "", "raw OData filter; overrides the flags above")
	f.BoolVar(&directReports, "direct-reports", false, "restrict to the key user's direct reports")
	f.BoolVar(&withNames, "names", true, "include employee names and job titles")
	f.BoolVar(&all, "all", false, "fetch every page")
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "entries per page (default 100)")
	return cmd
}

func whosOutTable(page *bamboohr.WhosOutPage) *Table {
	t := &Table{Headers: []string{"requestId", "employeeId", "employee", "typeId", "start", "end", "days"}}
	for _, e := range page.Data {
		name := ""
		if p, ok := page.Persons[strconv.Itoa(e.EmployeeID)]; ok {
			name = p.DisplayName
		}
		t.AddRow(e.ID, e.EmployeeID, name, e.TimeOffTypeID, e.Start, e.End, spanDays(e.Start, e.End))
	}
	return t
}

// spanDays renders the inclusive length of a date range, or "" when the dates
// do not describe one.
func spanDays(start, end string) string {
	days := bamboohr.InclusiveDays(start, end)
	if days == 0 {
		return ""
	}
	return strconv.Itoa(days)
}

func newTimeOffRequestsCommand(a *app) *cobra.Command {
	var (
		employeeIDs []int
		statuses    []string
		categoryID  int
		start       string
		end         string
		filter      string
		orderBy     string
		actions     bool
		all         bool
		page        int
		pageSize    int
	)

	cmd := &cobra.Command{
		Use:     "requests",
		Aliases: []string{"list"},
		Short:   "List time off requests",
		Long: `List time off requests.

The flags below build a server-side filter; --filter takes a raw OData
expression for anything they cannot express. Filterable fields are id,
employeeId, categoryId, startDate, endDate, amount, unit, status and
requestedAt, with eq, ne, lt, le, gt, ge and in, combined with and.

A request that a later edit replaced is never returned.`,
		Example: `  bamboohr timeoff requests --status requested
  bamboohr timeoff requests --employee 123 --start 2026-01-01 --end 2026-12-31
  bamboohr timeoff requests --filter "status eq 'APPROVED' and amount gt 8"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			parsedStatuses, err := parseStatuses(statuses)
			if err != nil {
				return err
			}
			if err := validateDates(map[string]string{"--start": start, "--end": end}); err != nil {
				return err
			}

			opts := bamboohr.TimeOffRequestListOptions{
				EmployeeIDs:   employeeIDs,
				Statuses:      parsedStatuses,
				CategoryID:    categoryID,
				Start:         start,
				End:           end,
				Filter:        filter,
				OrderBy:       orderBy,
				Page:          page,
				PageSize:      pageSize,
				ReturnActions: actions,
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if all {
				requests, err := client.ListAllTimeOffRequests(ctx, opts)
				if err != nil {
					return err
				}
				return a.printer().Print(requests, requestTable(requests))
			}
			result, err := client.ListTimeOffRequests(ctx, opts)
			if err != nil {
				return err
			}
			if result.Meta.HasMore() {
				a.logf("page %d of %d (%d requests total)", result.Meta.Page, result.Meta.TotalPages, result.Meta.TotalItems)
			}
			return a.printer().Print(result, requestTable(result.Data))
		},
	}

	f := cmd.Flags()
	f.IntSliceVar(&employeeIDs, "employee", nil, "restrict to these employee IDs")
	f.StringSliceVar(&statuses, "status", nil, "requested, approved, denied or canceled")
	f.IntVar(&categoryID, "category", 0, "restrict to one time off category ID")
	f.StringVar(&start, "start", "", "earliest start date, YYYY-MM-DD")
	f.StringVar(&end, "end", "", "latest end date, YYYY-MM-DD")
	f.StringVar(&filter, "filter", "", "raw OData filter; overrides the flags above")
	f.StringVar(&orderBy, "order-by", "", `sort expression, e.g. "requestedAt desc"`)
	f.BoolVar(&actions, "actions", false, "include the transitions available on each request")
	f.BoolVar(&all, "all", false, "fetch every page")
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "requests per page (default 100)")
	return cmd
}

// parseStatuses accepts statuses in any case and rejects unknown ones up front,
// which is a clearer failure than the API's 400.
func parseStatuses(values []string) ([]bamboohr.TimeOffStatus, error) {
	var out []bamboohr.TimeOffStatus
	for _, v := range splitList(values) {
		status := bamboohr.TimeOffStatus(strings.ToUpper(v))
		switch status {
		case bamboohr.StatusRequested, bamboohr.StatusApproved, bamboohr.StatusDenied, bamboohr.StatusCanceled:
			out = append(out, status)
		default:
			return nil, fmt.Errorf("unknown status %q: want requested, approved, denied or canceled", v)
		}
	}
	return out, nil
}

func requestTable(requests []bamboohr.TimeOffRequest) *Table {
	t := &Table{Headers: []string{"id", "employeeId", "categoryId", "start", "end", "amount", "unit", "status", "requestedAt"}}
	for _, r := range requests {
		t.AddRow(r.ID, r.EmployeeID, r.CategoryID, r.StartDate, r.EndDate, r.Amount,
			strings.ToLower(string(r.Unit)), strings.ToLower(string(r.Status)), r.RequestedAt)
	}
	return t
}

func newTimeOffRequestCommand(a *app) *cobra.Command {
	var actions bool

	cmd := &cobra.Command{
		Use:     "request <request-id>",
		Aliases: []string{"get"},
		Short:   "Get one time off request",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "request id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			req, err := client.GetTimeOffRequest(ctx, id, actions)
			if err != nil {
				return err
			}
			return a.printer().keyValue(req, requestPairs(req))
		},
	}
	cmd.Flags().BoolVar(&actions, "actions", false, "include the transitions available on the request")
	return cmd
}

func requestPairs(r *bamboohr.TimeOffRequest) [][2]string {
	pairs := [][2]string{
		{"id", strconv.Itoa(r.ID)},
		{"employeeId", strconv.Itoa(r.EmployeeID)},
		{"categoryId", strconv.Itoa(r.CategoryID)},
		{"startDate", r.StartDate},
		{"endDate", r.EndDate},
		{"amount", bamboohr.FormatValue(r.Amount)},
		{"unit", strings.ToLower(string(r.Unit))},
		{"status", strings.ToLower(string(r.Status))},
		{"requestedAt", r.RequestedAt},
		{"employeeNote", deref(r.EmployeeNote)},
		{"managerNote", deref(r.ManagerNote)},
		{"statusUpdatedAt", deref(r.StatusUpdatedAt)},
	}
	for _, d := range r.DailyAmounts {
		pairs = append(pairs, [2]string{"daily." + d.Date, bamboohr.FormatValue(d.Amount)})
	}
	for _, name := range sortedKeys(r.Links) {
		action := r.Links[name]
		pairs = append(pairs, [2]string{"action." + name, strings.TrimSpace(action.Method + " " + action.Href)})
	}
	return pairs
}

func newTimeOffCreateCommand(a *app) *cobra.Command {
	var (
		employeeID int
		categoryID int
		start      string
		end        string
		amount     float64
		daily      []string
		note       string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "File a time off request",
		Long: `File a time off request on an employee's behalf.

Give either --amount for the whole range or --daily date=hours for a per-day
breakdown, not both. The amount is in the category's unit: run
"bamboohr timeoff types" to see each category's ID and unit.`,
		Example: `  bamboohr timeoff create --employee 123 --category 22 \
    --start 2026-11-02 --end 2026-11-04 --amount 24 --note "Family trip"
  bamboohr timeoff create --employee 123 --category 22 \
    --start 2026-11-02 --end 2026-11-03 --daily 2026-11-02=8 --daily 2026-11-03=4`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if err := validateDates(map[string]string{"--start": start, "--end": end}); err != nil {
				return err
			}

			req := bamboohr.NewTimeOffRequest{
				EmployeeID:   employeeID,
				CategoryID:   categoryID,
				StartDate:    start,
				EndDate:      end,
				EmployeeNote: note,
			}
			if cmd.Flags().Changed("amount") {
				req.Amount = &amount
			}
			if len(daily) > 0 {
				req.DailyAmounts, err = parseDailyAmounts(daily)
				if err != nil {
					return err
				}
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			created, err := client.CreateTimeOffRequest(ctx, req)
			if err != nil {
				return err
			}
			return a.printer().keyValue(created, requestPairs(created))
		},
	}

	f := cmd.Flags()
	f.IntVar(&employeeID, "employee", 0, "employee ID (required)")
	f.IntVar(&categoryID, "category", 0, "time off category ID (required)")
	f.StringVar(&start, "start", "", "first day off, YYYY-MM-DD (required)")
	f.StringVar(&end, "end", "", "last day off, inclusive, YYYY-MM-DD (required)")
	f.Float64Var(&amount, "amount", 0, "total amount for the range, in the category's unit")
	f.StringArrayVar(&daily, "daily", nil, "per-day amount as YYYY-MM-DD=amount; repeatable")
	f.StringVar(&note, "note", "", "note from the requester, up to 1024 characters")
	for _, name := range []string{"employee", "category", "start", "end"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// parseDailyAmounts parses repeated --daily YYYY-MM-DD=amount flags.
func parseDailyAmounts(values []string) ([]bamboohr.DailyAmount, error) {
	out := make([]bamboohr.DailyAmount, 0, len(values))
	for _, v := range values {
		date, amountStr, found := strings.Cut(v, "=")
		if !found {
			return nil, fmt.Errorf("--daily %q is not date=amount", v)
		}
		date = strings.TrimSpace(date)
		if _, err := time.Parse(bamboohr.DateLayout, date); err != nil {
			return nil, fmt.Errorf("--daily %q: %q is not a YYYY-MM-DD date", v, date)
		}
		amount, err := strconv.ParseFloat(strings.TrimSpace(amountStr), 64)
		if err != nil {
			return nil, fmt.Errorf("--daily %q: %q is not a number", v, amountStr)
		}
		out = append(out, bamboohr.DailyAmount{Date: date, Amount: amount})
	}
	return out, nil
}

// newTimeOffDecisionCommand builds the approve and deny commands, which differ
// only in which endpoint they call.
func newTimeOffDecisionCommand(a *app, action string) *cobra.Command {
	var (
		note   string
		bypass bool
	)

	long := map[string]string{
		"approve": `Approve a time off request.

--bypass completes every remaining approval step rather than only yours, and
needs permission to bypass the other approvers.`,
		"deny": `Deny a time off request.

A denial always discards the remaining approval steps. --bypass changes who may
deny, not the outcome: without it you must be a current approver on the chain.`,
	}

	cmd := &cobra.Command{
		Use:   action + " <request-id>",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a time off request",
		Long:  long[action],
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "request id")
			if err != nil {
				return err
			}
			decision := bamboohr.TimeOffDecision{ManagerNote: note, Bypass: bypass}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			var req *bamboohr.TimeOffRequest
			if action == "approve" {
				req, err = client.ApproveTimeOffRequest(ctx, id, decision)
			} else {
				req, err = client.DenyTimeOffRequest(ctx, id, decision)
			}
			if err != nil {
				return err
			}
			return a.printer().keyValue(req, requestPairs(req))
		},
	}

	f := cmd.Flags()
	f.StringVar(&note, "note", "", "note recorded on the request, up to 1024 characters")
	f.BoolVar(&bypass, "bypass", false, "act on the remaining approval steps as well as your own")
	return cmd
}

func newTimeOffCancelCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <request-id>",
		Short: "Cancel a time off request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "request id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			req, err := client.CancelTimeOffRequest(ctx, id)
			if err != nil {
				return err
			}
			return a.printer().keyValue(req, requestPairs(req))
		},
	}
}

func newTimeOffTypesCommand(a *app) *cobra.Command {
	var requestable bool

	cmd := &cobra.Command{
		Use:     "types",
		Aliases: []string{"categories"},
		Short:   "List time off types",
		Long: `List the company's time off types and default daily hours.

The type ID is the --category value for "bamboohr timeoff create".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			types, err := client.ListTimeOffTypes(ctx, requestable)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"id", "name", "units", "source"}}
			for _, ty := range types.TimeOffTypes {
				t.AddRow(ty.ID, ty.Name, ty.Units, ty.Source)
			}
			return a.printer().Print(types, t)
		},
	}
	cmd.Flags().BoolVar(&requestable, "requestable", false, "only the types the key's user may request")
	return cmd
}

func newTimeOffPoliciesCommand(a *app) *cobra.Command {
	var (
		filter   string
		orderBy  string
		page     int
		pageSize int
	)

	cmd := &cobra.Command{
		Use:   "policies",
		Short: "List time off policies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			result, err := client.ListTimeOffPolicies(ctx, bamboohr.TimeOffPolicyListOptions{
				Filter:   filter,
				OrderBy:  orderBy,
				Page:     page,
				PageSize: pageSize,
			})
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"id", "name", "categoryId", "type", "assignedEmployees"}}
			for _, p := range result.Data {
				t.AddRow(p.ID, p.Name, p.CategoryID, p.Type, p.AssignedEmployeeCount)
			}
			return a.printer().Print(result, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "raw OData filter over id, name, categoryId, type, createdAt, updatedAt")
	f.StringVar(&orderBy, "order-by", "", `sort expression, e.g. "name asc"`)
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "policies per page (default 50)")
	return cmd
}

func newTimeOffBalanceCommand(a *app) *cobra.Command {
	var (
		asOf      string
		precision int
	)

	cmd := &cobra.Command{
		Use:   "balance <employee-id>",
		Short: "Get an employee's time off balances",
		Long: `Get an employee's time off balances.

--as-of projects the balance forward, which is how an accruing policy's balance
at a future date is checked. It defaults to today.`,
		Example: `  bamboohr timeoff balance 123
  bamboohr timeoff balance 123 --as-of 2026-12-31`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if err := validateDates(map[string]string{"--as-of": asOf}); err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			balances, err := client.GetTimeOffBalance(ctx, args[0], asOf, precision)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"typeId", "name", "balance", "units", "usedYTD", "policy", "asOf"}}
			for _, b := range balances {
				t.AddRow(b.TimeOffType, b.Name, b.Balance, b.Units, b.UsedYearToDate, b.PolicyType, b.End)
			}
			return a.printer().Print(balances, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&asOf, "as-of", "", "calculate the balance as of this date, YYYY-MM-DD (default today)")
	f.IntVar(&precision, "precision", 0, "decimal places (default 2)")
	return cmd
}

// validateDates rejects malformed dates before the request, so the error names
// the flag rather than arriving as a 400.
func validateDates(byFlag map[string]string) error {
	for _, flag := range sortedKeys(byFlag) {
		value := byFlag[flag]
		if value == "" {
			continue
		}
		if _, err := time.Parse(bamboohr.DateLayout, value); err != nil {
			return fmt.Errorf("%s: %q is not a YYYY-MM-DD date", flag, value)
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
