package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newEmployeeCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "employee <command>",
		Aliases:               []string{"employees", "emp"},
		Short:                 "Read and update employee records",
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newEmployeeListCommand(a),
		newEmployeeSearchCommand(a),
		newEmployeeGetCommand(a),
		newEmployeeCreateCommand(a),
		newEmployeeUpdateCommand(a),
		newEmployeeDirectoryCommand(a),
		newEmployeeChangedCommand(a),
		newEmployeeTableCommand(a),
		newEmployeePhotoCommand(a),
		newEmployeeFilesCommand(a),
		newEmployeeFileCommand(a),
	)
	return cmd
}

func newEmployeeListCommand(a *app) *cobra.Command {
	var (
		filters []string
		status  string
		ids     []string
		fields  []string
		sortBy  string
		limit   int
		after   string
		all     bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List employees",
		Long: `List employees, with server-side filtering.

Only active employees are listed by default. Pass --status inactive for former
employees, or --status all for everyone.

The default is not only about who is current. The endpoint returns every record
the API key can see the existence of, including the ones whose fields it may not
read; those come back with every column masked and render as a row of dashes.
Restricting to a status drops them, because a masked status matches no value.

Filters are given as --filter name=value and combined with AND. Text fields
match on a case-insensitive substring; IDs, enums, booleans and dates match
exactly. Run "bamboohr meta fields" to see the field names.`,
		Example: `  bamboohr employee list --limit 50
  bamboohr employee list --status all
  bamboohr employee list --filter departmentName=Robotics --fields workEmail,hireDate
  bamboohr employee list --id 123 --id 456
  bamboohr employee list --all --output csv > employees.csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			parsed, err := parseKeyValues(filters)
			if err != nil {
				return fmt.Errorf("--filter: %w", err)
			}
			parsed, err = applyStatusFilter(parsed, status, cmd.Flags().Changed("status"))
			if err != nil {
				return err
			}
			opts := bamboohr.EmployeeListOptions{
				Filters: parsed,
				IDs:     ids,
				Fields:  splitList(fields),
				Sort:    sortBy,
				Limit:   limit,
				After:   after,
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if all {
				a.logf("fetching all employees...")
				employees, err := client.ListAllEmployees(ctx, opts)
				if err != nil {
					return err
				}
				table := employeeTable(employees, opts.Fields)
				if err := a.printer().Print(employees, table); err != nil {
					return err
				}
				a.noteMasked(table)
				return nil
			}

			page, err := client.ListEmployees(ctx, opts)
			if err != nil {
				return err
			}
			if next := page.Meta.Page.NextCursor; next != "" && a.format == FormatTable {
				a.logf("showing %d of %d; next page: --after %s", len(page.Data), page.Meta.Total, next)
			}
			table := employeeTable(page.Data, opts.Fields)
			if err := a.printer().Print(page, table); err != nil {
				return err
			}
			a.noteMasked(table)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&status, "status", employeeStatusActive, "employee status: active, inactive or all")
	f.StringArrayVar(&filters, "filter", nil, "filter as name=value; repeatable")
	f.StringArrayVar(&ids, "id", nil, "restrict to these employee IDs; repeatable")
	f.StringSliceVar(&fields, "fields", nil, "extra fields to return")
	f.StringVar(&sortBy, "sort", "", `sort fields, "-" for descending (e.g. -lastName,firstName)`)
	f.IntVar(&limit, "limit", 0, "page size, up to 2500 (default 250)")
	f.StringVar(&after, "after", "", "cursor from a previous page")
	f.BoolVar(&all, "all", false, "follow the cursor and return every match")
	return cmd
}

// employeeColumnOrder is the reading order for the columns a listing shows
// first. It covers both spellings the API uses, since the list endpoint calls
// the ID employeeId and the title jobTitleName while the directory calls them
// id and jobTitle. A name absent from the response is skipped, so listing both
// costs nothing.
var employeeColumnOrder = []string{
	"id", "employeeId", "firstName", "lastName", "preferredName", "displayName",
	"jobTitleName", "jobTitle", "department", "division", "location",
	"supervisor", "workEmail", "workPhone", "status",
}

// employeePhotoColumns are returned in the default field set but are not worth
// a column: photoUrl is a signed URL several hundred characters long that
// pushes every useful column off the screen, and the other two are flags for
// the BambooHR web UI's upload button.
var employeePhotoColumns = []string{"photoUrl", "photoUploaded", "canUploadPhoto"}

// employeeTable renders the columns a listing is useful with.
//
// requested names the fields the caller asked for with --fields; naming a photo
// column there brings it back. The full record, photo URL included, is always
// available through --output json.
func employeeTable(employees []bamboohr.Employee, requested []string) *Table {
	// Any extra fields the caller requested are worth showing, so the columns
	// are derived from the records rather than fixed.
	records := make([]map[string]any, 0, len(employees))
	for _, e := range employees {
		records = append(records, e.Fields)
	}

	omit := make([]string, 0, len(employeePhotoColumns))
	for _, column := range employeePhotoColumns {
		if !slices.Contains(requested, column) {
			omit = append(omit, column)
		}
	}
	return mapTableOmitting(records, employeeColumnOrder, omit)
}

func newEmployeeGetCommand(a *app) *cobra.Command {
	var (
		fields      []string
		allFields   bool
		onlyCurrent bool
	)

	cmd := &cobra.Command{
		Use:   "get <employee-id>",
		Short: "Get one employee",
		Long: `Get one employee by internal employee ID.

BambooHR returns only the fields a request names, so this command asks for a
general-purpose set by default: name, job, contact details and hire date.

Pass --fields to choose your own, or --all-fields for every field this account
has an API name for. Run "bamboohr meta fields" to browse the catalogue.

Field names the account does not have, and ones the API key may not read, are
dropped by BambooHR without an error, so a field you asked for can simply be
absent from the output. Note that the names here are not always the ones
"employee list" returns: a job title is jobTitle here and jobTitleName there.

--all-fields holds nothing back: where the key has the permission it returns
compensation, dates of birth, home addresses and national identifiers. Think
about where the output is going before piping it somewhere.`,
		Example: `  bamboohr employee get 123
  bamboohr employee get 123 --fields firstName,lastName,workEmail
  bamboohr employee get 123 --all-fields
  bamboohr employee get 123 --fields hireDate,department --output json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if allFields && len(fields) > 0 {
				return fmt.Errorf("--all-fields and --fields both choose the fields: use one or the other")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			selected := splitList(fields)
			switch {
			case allFields:
				selected, err = a.everyFieldSelector(ctx, client)
				if err != nil {
					return err
				}
			case len(selected) == 0:
				selected = bamboohr.DefaultEmployeeFields
			}

			opts := bamboohr.GetEmployeeOptions{Fields: selected}
			if cmd.Flags().Changed("only-current") {
				opts.OnlyCurrent = &onlyCurrent
			}

			emp, err := client.GetEmployee(ctx, args[0], opts)
			if err != nil {
				return err
			}
			if len(emp.RestrictedFields) > 0 {
				a.logf("restricted fields returned as null: %s", strings.Join(emp.RestrictedFields, ", "))
			}

			if err := a.printer().keyValue(emp, fieldPairs(emp.Fields, []string{"id", "firstName", "lastName"})); err != nil {
				return err
			}
			a.noteOnlyID(emp, len(selected))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&fields, "fields", nil, "fields to return (names, IDs or aliases); default is a general-purpose set")
	f.BoolVar(&allFields, "all-fields", false, "request every field this account has an API name for")
	f.BoolVar(&onlyCurrent, "only-current", true, "limit historical tables to currently effective values")
	return cmd
}

// maxFieldsPerRequest is BambooHR's documented cap on the fields parameter.
const maxFieldsPerRequest = 400

// everyFieldSelector lists every field this account can address by name.
//
// The account's catalogue runs to hundreds of entries, most of them belonging
// to employee tables rather than the employee record, and only the ones with an
// API alias can be named in a fields parameter at all. Asking for those keeps
// the request inside BambooHR's 400-field cap and is what "every field" means
// in practice.
func (a *app) everyFieldSelector(ctx context.Context, client *bamboohr.Client) ([]string, error) {
	catalogue, err := client.ListFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the account's fields for --all-fields: %w", err)
	}

	seen := make(map[string]bool, len(catalogue))
	selectors := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		// A field with no alias can only be named by numeric ID, and the
		// catalogue holds far more of those than a request may carry.
		if field.Deprecated || field.Alias == "" || seen[field.Alias] {
			continue
		}
		seen[field.Alias] = true
		selectors = append(selectors, field.Alias)
	}

	if len(selectors) > maxFieldsPerRequest {
		a.logf("account has %d named fields; requesting the first %d, which is BambooHR's limit",
			len(selectors), maxFieldsPerRequest)
		selectors = selectors[:maxFieldsPerRequest]
	}
	a.logf("requesting %d named fields", len(selectors))
	return selectors, nil
}

// noteOnlyID explains a response that carries nothing but the identifier.
//
// Fields are dropped without an error when the key may not read them, so an
// otherwise empty record is the shape a permission boundary takes here.
func (a *app) noteOnlyID(emp *bamboohr.Employee, requested int) {
	if len(emp.Fields) > 1 || requested == 0 {
		return
	}
	fmt.Fprintf(a.errOut,
		"note: none of the %d requested fields came back; the API key's user most likely "+
			"cannot read this employee's fields (try --all-fields, or \"bamboohr meta fields\")\n",
		requested)
}

// fieldPairs turns a record into ordered field/value pairs for keyValue output.
func fieldPairs(fields map[string]any, preferred []string) [][2]string {
	keys := orderedKeys([]map[string]any{fields}, preferred)
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, bamboohr.FormatValue(fields[k])})
	}
	return pairs
}

func newEmployeeCreateCommand(a *app) *cobra.Command {
	var (
		data string
		sets []string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an employee",
		Long: `Create an employee from field values.

BambooHR requires at least firstName and lastName. Give fields with --set, or
pass a JSON object with --data (@file to read a file, - for stdin).`,
		Example: `  bamboohr employee create --set firstName=Ada --set lastName=Lovelace
  bamboohr employee create --data @new-hire.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			fields, err := mergeFieldInput(data, sets)
			if err != nil {
				return err
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			id, err := client.CreateEmployee(ctx, fields)
			if err != nil {
				return err
			}
			result := map[string]string{"id": id}
			return a.printer().Print(result, &Table{
				Headers: []string{"id"},
				Rows:    [][]string{{id}},
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&data, "data", "", "JSON object of fields; @file or - for stdin")
	f.StringArrayVar(&sets, "set", nil, "field as name=value; repeatable")
	return cmd
}

func newEmployeeUpdateCommand(a *app) *cobra.Command {
	var (
		data string
		sets []string
	)

	cmd := &cobra.Command{
		Use:   "update <employee-id>",
		Short: "Update an employee",
		Long: `Update the given fields on an employee, leaving the rest unchanged.

Give fields with --set, or pass a JSON object with --data (@file to read a
file, - for stdin).`,
		Example: `  bamboohr employee update 123 --set workPhone=555-0100
  bamboohr employee update 123 --data @changes.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			fields, err := mergeFieldInput(data, sets)
			if err != nil {
				return err
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if err := client.UpdateEmployee(ctx, args[0], fields); err != nil {
				return err
			}
			fmt.Fprintf(a.errOut, "updated employee %s (%d field(s))\n", args[0], len(fields))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&data, "data", "", "JSON object of fields; @file or - for stdin")
	f.StringArrayVar(&sets, "set", nil, "field as name=value; repeatable")
	return cmd
}

func newEmployeeDirectoryCommand(a *app) *cobra.Command {
	var onlyCurrent bool

	cmd := &cobra.Command{
		Use:     "directory",
		Aliases: []string{"dir"},
		Short:   "Get the published employee directory",
		Long: `Get the company's published employee directory.

The columns are fixed by the company's directory configuration rather than
chosen per request, which makes this the cheapest way to get a name, title and
contact list for everyone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			var oc *bool
			if cmd.Flags().Changed("only-current") {
				oc = &onlyCurrent
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			dir, err := client.GetDirectory(ctx, oc)
			if err != nil {
				return err
			}
			return a.printer().Print(dir, employeeTable(dir.Employees, nil))
		},
	}
	cmd.Flags().BoolVar(&onlyCurrent, "only-current", true, "limit historical tables to currently effective values")
	return cmd
}

func newEmployeeChangedCommand(a *app) *cobra.Command {
	var (
		since      string
		changeType string
	)

	cmd := &cobra.Command{
		Use:     "changed",
		Aliases: []string{"changes"},
		Short:   "List employees changed since a point in time",
		Long: `List the employees changed since a point in time.

This is how an integration polls for updates instead of re-reading every
record. --since takes an RFC 3339 timestamp or a relative age such as 24h or
7d. The response's "latest" value is the right --since for the next poll.`,
		Example: `  bamboohr employee changed --since 24h
  bamboohr employee changed --since 2026-09-01T00:00:00Z --type updated`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ts, err := parseSince(since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			changed, err := client.GetChangedEmployees(ctx, ts, bamboohr.ChangeType(changeType))
			if err != nil {
				return err
			}

			t := &Table{Headers: []string{"id", "action", "lastChanged"}}
			for _, id := range sortedKeys(changed.Employees) {
				c := changed.Employees[id]
				t.AddRow(id, c.Action, c.LastChanged)
			}
			if changed.Latest != "" {
				a.logf("latest change: %s (use as the next --since)", changed.Latest)
			}
			return a.printer().Print(changed, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&since, "since", "", "RFC 3339 timestamp or relative age such as 24h or 7d (required)")
	f.StringVar(&changeType, "type", "", "inserted, updated, deleted or all")
	_ = cmd.MarkFlagRequired("since")
	return cmd
}

func newEmployeeTableCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "table <employee-id> <table>",
		Short: "Get the rows of an employee table",
		Long: `Get the rows of one of an employee's tables.

Standard tables include jobInfo, compensation, employmentStatus,
emergencyContacts and employeeEducation. Run "bamboohr meta tables" to see
every table this account has, including custom ones.`,
		Example: `  bamboohr employee table 123 jobInfo
  bamboohr employee table 123 compensation --output json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			rows, err := client.GetEmployeeTable(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			records := make([]map[string]any, 0, len(rows))
			for _, r := range rows {
				records = append(records, r)
			}
			return a.printer().Print(rows, mapTable(records, []string{"id", "employeeId", "date", "startDate"}))
		},
	}
	return cmd
}

func newEmployeePhotoCommand(a *app) *cobra.Command {
	var (
		size    string
		outFile string
	)

	cmd := &cobra.Command{
		Use:   "photo <employee-id>",
		Short: "Download an employee photo",
		Example: `  bamboohr employee photo 123 --size large --file ada.jpg
  bamboohr employee photo 123 --file - > ada.jpg`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			return a.saveDownload(
				downloadTarget{Path: outFile, Fallback: "photo-" + args[0]},
				func(w io.Writer) (*bamboohr.Download, error) {
					return client.GetEmployeePhoto(ctx, args[0], bamboohr.PhotoSize(size), w)
				})
		},
	}

	f := cmd.Flags()
	f.StringVar(&size, "size", string(bamboohr.PhotoMedium), "original, large, medium, small, xs or tiny")
	f.StringVarP(&outFile, "file", "f", "", "write to this path, or - for stdout (default: the name BambooHR suggests)")
	return cmd
}

func newEmployeeFilesCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files <employee-id>",
		Short: "List an employee's files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			files, err := client.ListEmployeeFiles(ctx, args[0])
			if err != nil {
				return err
			}
			return a.printer().Print(files, fileTable(files.Categories))
		},
	}
	return cmd
}

func newEmployeeFileCommand(a *app) *cobra.Command {
	var outFile string

	cmd := &cobra.Command{
		Use:   "file <employee-id> <file-id>",
		Short: "Download one of an employee's files",
		Long: `Download one of an employee's files.

Run "bamboohr employee files <employee-id>" to find the file ID.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			fileID, err := parseID(args[1], "file id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			return a.saveDownload(
				downloadTarget{
					Path:     outFile,
					Prefix:   fmt.Sprintf("employee-%s-", args[0]),
					Fallback: fmt.Sprintf("file-%d", fileID),
				},
				func(w io.Writer) (*bamboohr.Download, error) {
					return client.DownloadEmployeeFile(ctx, args[0], fileID, w)
				})
		},
	}
	cmd.Flags().StringVarP(&outFile, "file", "f", "", "write to this path, or - for stdout (default: the name BambooHR suggests)")
	return cmd
}

// The --status values the employee list command accepts.
const (
	employeeStatusActive   = "active"
	employeeStatusInactive = "inactive"
	// employeeStatusAll asks for every employee, whatever their status, and so
	// sends no status filter at all.
	employeeStatusAll = "all"
)

// applyStatusFilter folds the --status flag into the filter map.
//
// --filter status=... reaches the same query parameter, so setting both is
// rejected rather than resolved by a precedence rule the user cannot see.
func applyStatusFilter(filters map[string]string, status string, explicit bool) (map[string]string, error) {
	if _, viaFilter := filters["status"]; viaFilter {
		if explicit {
			return nil, fmt.Errorf("--status and --filter status= both set the employee status: use one or the other")
		}
		// An explicit --filter status=... replaces the default.
		return filters, nil
	}

	switch strings.ToLower(strings.TrimSpace(status)) {
	case employeeStatusAll:
		return filters, nil
	case employeeStatusActive, employeeStatusInactive:
		if filters == nil {
			filters = make(map[string]string, 1)
		}
		filters["status"] = strings.ToLower(strings.TrimSpace(status))
		return filters, nil
	default:
		return nil, fmt.Errorf("unknown --status %q: want active, inactive or all", status)
	}
}

// noteMasked explains any rows that rendered as nothing but an identifier.
//
// A record whose fields the API key may not read still appears in the listing,
// with every value masked. Without a word on stderr those rows look like a bug
// in the tool rather than a permission boundary.
//
// It counts blank rows rather than records carrying _restrictedFields, because
// a restriction on a field the table does not show is invisible and not worth
// a note.
func (a *app) noteMasked(table *Table) {
	blank := 0
	for _, row := range table.Rows {
		if isBlankRow(row) {
			blank++
		}
	}
	if blank == 0 {
		return
	}
	fmt.Fprintf(a.errOut,
		"note: %d of %d rows have no readable values beyond the identifier, "+
			"usually the API key's field permissions\n",
		blank, len(table.Rows))
}

// isBlankRow reports whether every value past the first is empty. The first
// column is the identifier, which BambooHR returns even for a record whose
// fields the key may not read.
func isBlankRow(row []string) bool {
	if len(row) < 2 {
		return false
	}
	for _, v := range row[1:] {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
