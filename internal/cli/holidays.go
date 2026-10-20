package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newHolidayCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "holiday",
		Aliases:               []string{"holidays"},
		Short:                 "Read the company's holidays",
		Long:                  `Read the holidays the company observes, and BambooHR's global holiday catalogue.`,
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newHolidayListCommand(a),
		newHolidayGetCommand(a),
		newHolidayCatalogCommand(a),
	)
	return cmd
}

func newHolidayListCommand(a *app) *cobra.Command {
	var (
		year       int
		from       string
		to         string
		publicOnly bool
		filter     string
		orderBy    string
		all        bool
		page       int
		pageSize   int
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the company's holidays",
		Long: `List the holidays the company observes.

With no date range it lists the current year. --year is a shorthand for a whole
calendar year; --from and --to take an arbitrary range.`,
		Example: `  bamboohr holiday list --year 2026
  bamboohr holiday list --from 2026-01-01 --to 2026-06-30
  bamboohr holiday list --year 2026 --public --output csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if year > 0 && (from != "" || to != "") {
				return fmt.Errorf("--year and --from/--to both set the date range: use one or the other")
			}
			// A bare command should show something useful, and the current
			// year is what "the company holidays" almost always means.
			if year == 0 && from == "" && to == "" && filter == "" {
				year = thisYear()
				a.logf("no date range given; showing %d", year)
			}
			if year > 0 {
				from = fmt.Sprintf("%04d-01-01", year)
				to = fmt.Sprintf("%04d-12-31", year)
			}
			if err := validateDates(map[string]string{"--from": from, "--to": to}); err != nil {
				return err
			}
			// The API defaults to 20 per page, which splits a normal year over
			// several requests for no reason.
			if pageSize == 0 {
				pageSize = defaultHolidayPageSize
			}

			opts := bamboohr.HolidayListOptions{
				From:       from,
				To:         to,
				PublicOnly: publicOnly,
				Filter:     filter,
				OrderBy:    orderBy,
				Page:       page,
				PageSize:   pageSize,
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if all {
				holidays, err := client.ListAllHolidays(ctx, opts)
				if err != nil {
					return err
				}
				return a.printer().Print(holidays, holidayTable(holidays))
			}

			result, err := client.ListHolidays(ctx, opts)
			if err != nil {
				return err
			}
			if result.Meta.HasMore() {
				a.logf("page %d of %d (%d holidays total); use --all for every page",
					result.Meta.Page, result.Meta.TotalPages, result.Meta.TotalItems)
			}
			return a.printer().Print(result, holidayTable(result.Data))
		},
	}

	f := cmd.Flags()
	f.IntVar(&year, "year", 0, "list a whole calendar year (default: the current year)")
	f.StringVar(&from, "from", "", "earliest start date, YYYY-MM-DD")
	f.StringVar(&to, "to", "", "latest start date, YYYY-MM-DD")
	f.BoolVar(&publicOnly, "public", false, "only holidays shown on every employee's calendar")
	f.StringVar(&filter, "filter", "", "raw OData filter over name, startDate, endDate, isPublic, globalHolidayUuid")
	f.StringVar(&orderBy, "order-by", "", `sort expression, e.g. "startDate desc"`)
	f.BoolVar(&all, "all", false, "fetch every page")
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0,
		fmt.Sprintf("holidays per page (default %d)", defaultHolidayPageSize))
	return cmd
}

// defaultHolidayPageSize is large enough that a calendar year arrives in one
// request, unlike the API's own default of 20.
const defaultHolidayPageSize = 100

func holidayTable(holidays []bamboohr.Holiday) *Table {
	t := &Table{Headers: []string{"id", "name", "start", "end", "days", "public", "countries", "audience", "source"}}
	for _, h := range holidays {
		source := "catalog"
		if h.Custom() {
			source = "custom"
		}
		t.AddRow(h.ID, h.Name, h.StartDate, deref(h.EndDate), h.Days(), h.IsPublic,
			strings.Join(h.CountryCodes, ","), audienceSummary(h.Audience), source)
	}
	return t
}

// audienceSummary renders a holiday's audience in one cell, since the full
// object is several lists of IDs.
func audienceSummary(audience bamboohr.HolidayAudience) string {
	switch audience.Mode {
	case "":
		return ""
	case "ALL_EMPLOYEES":
		return "all"
	case "FILTERED":
		return fmt.Sprintf("filtered (%d)", len(audience.FilterListValueIDs))
	case "SPECIFIC_EMPLOYEES":
		return fmt.Sprintf("%d employees", len(audience.SpecificEmployeeIDs))
	default:
		return strings.ToLower(audience.Mode)
	}
}

func newHolidayGetCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "get <holiday-id>",
		Short: "Get one company holiday",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "holiday id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			holiday, err := client.GetHoliday(ctx, id)
			if err != nil {
				return err
			}
			return a.printer().keyValue(holiday, holidayPairs(holiday))
		},
	}
}

func holidayPairs(h *bamboohr.Holiday) [][2]string {
	pairs := [][2]string{
		{"id", strconv.Itoa(h.ID)},
		{"name", h.Name},
		{"startDate", h.StartDate},
		{"endDate", deref(h.EndDate)},
		{"days", strconv.Itoa(h.Days())},
		{"isPublic", strconv.FormatBool(h.IsPublic)},
		{"countryCodes", strings.Join(h.CountryCodes, ",")},
		{"audience", audienceSummary(h.Audience)},
	}
	if h.Custom() {
		pairs = append(pairs, [2]string{"source", "custom"})
	} else {
		pairs = append(pairs, [2]string{"source", "catalog"},
			[2]string{"globalHolidayUuid", deref(h.GlobalHolidayUUID)})
	}
	if h.Pay != nil {
		pairs = append(pairs, [2]string{"payMode", h.Pay.Mode})
		if h.Pay.DefaultHours != nil {
			pairs = append(pairs, [2]string{"payDefaultHours", *h.Pay.DefaultHours})
		}
		if h.Pay.Multiplier != nil {
			pairs = append(pairs, [2]string{"payMultiplier", *h.Pay.Multiplier})
		}
	}
	return append(pairs,
		[2]string{"createdAt", h.CreatedAt},
		[2]string{"updatedAt", h.UpdatedAt})
}

func newHolidayCatalogCommand(a *app) *cobra.Command {
	var (
		country  string
		year     int
		filter   string
		orderBy  string
		page     int
		pageSize int
	)

	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse BambooHR's global holiday catalogue",
		Long: `Browse BambooHR's global holiday catalogue.

This is the read-only reference list a company holiday can be seeded from, not
the holidays your company observes. Use "bamboohr holiday list" for those.

The catalogue spans every country, so narrow it with --country.`,
		Example: `  bamboohr holiday catalog --country CA --year 2026
  bamboohr holiday catalog --country US --year 2026 --page-size 50`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if country != "" && len(country) != 2 {
				return fmt.Errorf("--country must be a two-letter ISO 3166-1 code, got %q", country)
			}
			if year == 0 && country == "" {
				a.logf("no --country or --year given; the catalogue covers every country")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			result, err := client.ListCatalogHolidays(ctx, bamboohr.CatalogHolidayOptions{
				CountryCode: country,
				Year:        year,
				Filter:      filter,
				OrderBy:     orderBy,
				Page:        page,
				PageSize:    pageSize,
			})
			if err != nil {
				return err
			}

			t := &Table{Headers: []string{"uuid", "name", "type", "start", "end", "country", "source"}}
			for _, h := range result.Data {
				t.AddRow(h.UUID, h.Name, h.Type, h.StartDate, deref(h.EndDate), h.CountryCode, h.Source)
			}
			if result.Meta.HasMore() {
				a.logf("page %d of %d (%d entries total)",
					result.Meta.Page, result.Meta.TotalPages, result.Meta.TotalItems)
			}
			return a.printer().Print(result, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&country, "country", "", "ISO 3166-1 alpha-2 country code, e.g. CA")
	f.IntVar(&year, "year", 0, "calendar year")
	f.StringVar(&filter, "filter", "", "raw OData filter")
	f.StringVar(&orderBy, "order-by", "", `sort expression, e.g. "startDate asc"`)
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "entries per page (default 20)")
	return cmd
}

// thisYear is the default a year-scoped command falls back to.
func thisYear() int { return time.Now().Year() }
