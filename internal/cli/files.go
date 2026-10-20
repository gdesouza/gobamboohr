package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newFilesCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "files",
		Aliases: []string{"file"},
		Short:   "List and download company files",
		Long: `List and download company files.

For files attached to an employee, use "bamboohr employee files" and
"bamboohr employee file".`,
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newCompanyFilesListCommand(a),
		newCompanyFileGetCommand(a),
	)
	return cmd
}

func newCompanyFilesListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List company files",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			files, err := client.ListCompanyFiles(ctx)
			if err != nil {
				return err
			}
			return a.printer().Print(files, fileTable(files.Categories))
		},
	}
}

func newCompanyFileGetCommand(a *app) *cobra.Command {
	var outFile string

	cmd := &cobra.Command{
		Use:     "get <file-id>",
		Aliases: []string{"download"},
		Short:   "Download a company file",
		Long: `Download a company file.

Run "bamboohr files list" to find the file ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			fileID, err := parseID(args[0], "file id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			return a.saveDownload(
				downloadTarget{Path: outFile, Fallback: fmt.Sprintf("company-file-%d", fileID)},
				func(w io.Writer) (*bamboohr.Download, error) {
					return client.DownloadCompanyFile(ctx, fileID, w)
				})
		},
	}
	cmd.Flags().StringVarP(&outFile, "file", "f", "", "write to this path, or - for stdout (default: the name BambooHR suggests)")
	return cmd
}

// bamboohrItoa keeps integer rendering in one place alongside the client's
// FormatValue, so numbers in tables never pick up an exponent.
func bamboohrItoa(n int) string { return strconv.Itoa(n) }
