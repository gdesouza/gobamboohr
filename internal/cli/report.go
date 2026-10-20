package cli

import (
	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newReportCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "report <command>",
		Aliases:               []string{"reports"},
		Short:                 "List and run saved custom reports",
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newReportListCommand(a),
		newReportGetCommand(a),
	)
	return cmd
}

func newReportListCommand(a *app) *cobra.Command {
	var (
		page     int
		pageSize int
		all      bool
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved custom reports",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if all {
				reports, err := client.ListAllReports(ctx, pageSize)
				if err != nil {
					return err
				}
				t := &Table{Headers: []string{"id", "name"}}
				for _, r := range reports {
					t.AddRow(r.ID, r.Name)
				}
				return a.printer().Print(reports, t)
			}

			result, err := client.ListReports(ctx, page, pageSize)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"id", "name"}}
			for _, r := range result.Reports {
				t.AddRow(r.ID, r.Name)
			}
			if result.Pagination.HasMore() {
				a.logf("page %d of %d (%d reports total)",
					result.Pagination.CurrentPage, result.Pagination.TotalPages, result.Pagination.TotalRecords)
			}
			return a.printer().Print(result, t)
		},
	}

	f := cmd.Flags()
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "reports per page (default 500)")
	f.BoolVar(&all, "all", false, "fetch every page")
	return cmd
}

func newReportGetCommand(a *app) *cobra.Command {
	var (
		page     int
		pageSize int
		all      bool
	)

	cmd := &cobra.Command{
		Use:     "get <report-id>",
		Aliases: []string{"run"},
		Short:   "Run a saved custom report",
		Long: `Run a saved custom report and print its rows.

The report's own columns become the output columns, in the order it shows them.
Run "bamboohr report list" to find a report ID.`,
		Example: `  bamboohr report get 42
  bamboohr report get 42 --all --output csv > report.csv`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "report id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			var result *bamboohr.ReportData
			if all {
				result, err = client.GetAllReportData(ctx, id, pageSize)
			} else {
				result, err = client.GetReport(ctx, id, page, pageSize)
			}
			if err != nil {
				return err
			}

			t := reportTable(result)
			if result.Pagination.HasMore() && !all {
				a.logf("page %d of %d (%d records total); use --all for everything",
					result.Pagination.CurrentPage, result.Pagination.TotalPages, result.Pagination.TotalRecords)
			}
			return a.printer().Print(result, t)
		},
	}

	f := cmd.Flags()
	f.IntVar(&page, "page", 0, "page number, 1-based")
	f.IntVar(&pageSize, "page-size", 0, "records per page (default 500)")
	f.BoolVar(&all, "all", false, "fetch every page")
	return cmd
}

// reportTable renders a report's rows under the report's own column headings,
// in the order the report shows them. A column with no values in any row is
// still shown, because its absence from the output would otherwise look like
// the report had changed.
func reportTable(data *bamboohr.ReportData) *Table {
	if len(data.Columns) == 0 {
		return mapTable(data.Rows, nil)
	}

	t := &Table{}
	for _, col := range data.Columns {
		t.Headers = append(t.Headers, col.Heading())
	}
	for _, row := range data.Rows {
		values := make([]any, 0, len(data.Columns))
		for _, col := range data.Columns {
			values = append(values, row[col.Name])
		}
		t.AddRow(values...)
	}
	return t
}
