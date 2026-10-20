package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newMetaCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "meta",
		Aliases: []string{"metadata"},
		Short:   "Discover the fields, tables, lists and users in this account",
		Long: `Discover what this BambooHR account contains.

These commands answer the questions the other commands raise: which field names
--fields accepts, which table names "employee table" accepts, and which values
a list field allows.`,
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newMetaFieldsCommand(a),
		newMetaTablesCommand(a),
		newMetaListsCommand(a),
		newMetaUsersCommand(a),
		newMetaCompanyCommand(a),
	)
	return cmd
}

func newMetaFieldsCommand(a *app) *cobra.Command {
	var includeDeprecated bool

	cmd := &cobra.Command{
		Use:   "fields",
		Short: "List every employee field",
		Long: `List every employee field in the account.

The "selector" column is what to pass to --fields: the alias when a field has
one, otherwise its numeric ID.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			fields, err := client.ListFields(ctx)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"selector", "id", "name", "type", "deprecated"}}
			for _, f := range fields {
				if f.Deprecated && !includeDeprecated {
					continue
				}
				t.AddRow(f.Selector(), f.ID, f.Name, f.Type, f.Deprecated)
			}
			return a.printer().Print(fields, t)
		},
	}
	cmd.Flags().BoolVar(&includeDeprecated, "deprecated", false, "include deprecated fields")
	return cmd
}

func newMetaTablesCommand(a *app) *cobra.Command {
	var showFields bool

	cmd := &cobra.Command{
		Use:   "tables",
		Short: "List every employee table",
		Long: `List every employee table in the account, including custom ones.

The "table" column is what to pass to "bamboohr employee table". Add --fields
to list each table's columns as well.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			tables, err := client.ListTables(ctx)
			if err != nil {
				return err
			}

			var t *Table
			if showFields {
				t = &Table{Headers: []string{"table", "field", "fieldId", "type"}}
				for _, tbl := range tables {
					for _, f := range tbl.Fields {
						name := f.Alias
						if name == "" {
							name = f.Name
						}
						t.AddRow(tbl.Alias, name, f.ID, f.Type)
					}
				}
			} else {
				t = &Table{Headers: []string{"table", "fields"}}
				for _, tbl := range tables {
					t.AddRow(tbl.Alias, len(tbl.Fields))
				}
			}
			return a.printer().Print(tables, t)
		},
	}
	cmd.Flags().BoolVar(&showFields, "fields", false, "list each table's columns")
	return cmd
}

func newMetaListsCommand(a *app) *cobra.Command {
	var (
		showOptions bool
		includeArch bool
	)

	cmd := &cobra.Command{
		Use:     "lists",
		Aliases: []string{"list-fields"},
		Short:   "List every list field and its options",
		Long: `List every list field in the account.

Add --options to see the values each field accepts, which is what an update to
a list field has to choose from.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			lists, err := client.ListLists(ctx)
			if err != nil {
				return err
			}

			var t *Table
			if showOptions {
				t = &Table{Headers: []string{"field", "fieldId", "optionId", "option", "archived"}}
				for _, l := range lists {
					for _, opt := range l.Options {
						if opt.IsArchived() && !includeArch {
							continue
						}
						t.AddRow(listName(l.Name, l.Alias), l.FieldID, opt.ID, opt.Name, opt.IsArchived())
					}
				}
			} else {
				t = &Table{Headers: []string{"field", "alias", "fieldId", "options", "multiple", "manageable"}}
				for _, l := range lists {
					t.AddRow(l.Name, deref(l.Alias), l.FieldID, len(l.Options), l.AcceptsMultiple(), l.IsManageable())
				}
			}
			return a.printer().Print(lists, t)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&showOptions, "options", false, "list each field's options")
	f.BoolVar(&includeArch, "archived", false, "include archived options")
	return cmd
}

// listName prefers the alias, which is the name a fields parameter needs.
func listName(name string, alias *string) string {
	if alias != nil && *alias != "" {
		return *alias
	}
	return name
}

func newMetaUsersCommand(a *app) *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "users",
		Short: "List the company's users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			users, err := client.ListUsers(ctx, strings.ToLower(status))
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"id", "employeeId", "firstName", "lastName", "email", "status", "lastLogin"}}
			for _, u := range users {
				employeeID := ""
				// The API sends 0 for a user with no employee record.
				if u.EmployeeID != 0 {
					employeeID = bamboohrItoa(u.EmployeeID)
				}
				t.AddRow(u.ID, employeeID, u.FirstName, u.LastName, u.Email, u.Status, u.LastLogin)
			}
			return a.printer().Print(users, t)
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "enabled or disabled")
	return cmd
}

func newMetaCompanyCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "company",
		Short: "Show the company the API key belongs to",
		Long: `Show the company the API key belongs to.

It takes no arguments and needs no permissions beyond a working key, which
makes it the quickest way to check that credentials are set up correctly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			company, err := client.GetCompany(ctx)
			if err != nil {
				return err
			}
			return a.printer().keyValue(company, [][2]string{
				{"id", company.ID.String()},
				{"name", company.Name},
				{"domain", company.Domain},
				{"baseApiUrl", company.BaseAPIURL},
			})
		},
	}
}
