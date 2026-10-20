package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func newRecruitingCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "recruiting",
		Aliases: []string{"ats", "hiring"},
		Short:   "Work with job openings and applications",
		Long: `Work with the applicant tracking side of BambooHR: job openings, the
applications against them, and moving a candidate through the pipeline.

Start with "recruiting jobs" for the openings and their applicant counts, then
"recruiting applications --job <id>" for the candidates on one of them.`,
		RunE:                  groupRunE,
		DisableFlagsInUseLine: true,
	}
	cmd.AddCommand(
		newRecruitingJobsCommand(a),
		newRecruitingApplicationsCommand(a),
		newRecruitingApplicationCommand(a),
		newRecruitingFilesCommand(a),
		newRecruitingDownloadCommand(a),
		newRecruitingStatusesCommand(a),
		newRecruitingSetStatusCommand(a),
		newRecruitingCommentCommand(a),
		newRecruitingHiringLeadsCommand(a),
		newRecruitingLocationsCommand(a),
	)
	return cmd
}

func newRecruitingJobsCommand(a *app) *cobra.Command {
	var (
		statusGroups string
		statusIDs    []string
		sortBy       string
		sortOrder    string
	)

	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"openings"},
		Short:   "List job openings and their applicant counts",
		Long: `List the company's job openings with how many candidates sit against each.

The NEW and ACTIVE counts are the ones worth watching: NEW is candidates nobody
has looked at yet.`,
		Example: `  bamboohr recruiting jobs
  bamboohr recruiting jobs --sort-by count --sort-order DESC
  bamboohr recruiting jobs --job-status Open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if err := validateEnum("--sort-by", sortBy, "count", "title", "lead", "created", "status"); err != nil {
				return err
			}
			if err := validateEnum("--sort-order", sortOrder, "ASC", "DESC"); err != nil {
				return err
			}
			if err := validateEnum("--job-status", statusGroups, bamboohr.JobStatusGroups...); err != nil {
				return err
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			jobs, err := client.ListJobOpenings(ctx, bamboohr.JobOpeningListOptions{
				StatusGroups: statusGroups,
				StatusIDs:    statusIDs,
				SortBy:       sortBy,
				SortOrder:    sortOrder,
			})
			if err != nil {
				return err
			}

			t := &Table{Headers: []string{"id", "title", "status", "department", "location", "hiringLead", "new", "active", "total", "posted"}}
			for _, j := range jobs {
				t.AddRow(j.ID, j.Title.Label, j.Status.Label,
					labelOf(j.Department), locationOf(j.Location), leadOf(j.HiringLead),
					j.NewApplicantsCount, j.ActiveApplicantsCount, j.TotalApplicantsCount,
					dateOnly(j.PostedDate))
			}
			return a.printer().Print(jobs, t)
		},
	}

	f := cmd.Flags()
	f.StringVar(&statusGroups, "job-status", "",
		"job status groups, comma-separated: "+strings.Join(bamboohr.JobStatusGroups, ", "))
	f.StringSliceVar(&statusIDs, "status-id", nil, "restrict to these job status IDs")
	f.StringVar(&sortBy, "sort-by", "", "count, title, lead, created or status")
	f.StringVar(&sortOrder, "sort-order", "", "ASC or DESC")
	return cmd
}

func newRecruitingApplicationsCommand(a *app) *cobra.Command {
	var (
		jobID        int
		statusID     string
		statusGroups string
		jobStatus    string
		search       string
		newSince     string
		sortBy       string
		sortOrder    string
		all          bool
		page         int
	)

	cmd := &cobra.Command{
		Use:     "applications",
		Aliases: []string{"candidates", "apps"},
		Short:   "List job applications",
		Long: `List the applications against the company's job openings.

--search is matched server-side by BambooHR, unlike "employee search" which
filters locally. --new-since takes an RFC 3339 timestamp or a relative age such
as 7d, which is what a recruiting digest wants.

Note that the API filters to open jobs unless told otherwise, which returns
nothing for a company whose openings are all filled or on hold. This command
therefore asks for every job status by default; narrow it with --job-status.`,
		Example: `  bamboohr recruiting applications --job 12
  bamboohr recruiting applications --new-since 7d
  bamboohr recruiting applications --search "smith" --all
  bamboohr recruiting applications --status-group NEW
  bamboohr recruiting applications --job-status Open --sort-by created_date --sort-order DESC`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			if err := validateEnum("--sort-by", sortBy,
				"first_name", "job_title", "rating", "phone", "status", "last_updated", "created_date"); err != nil {
				return err
			}
			if err := validateEnum("--sort-order", sortOrder, "ASC", "DESC"); err != nil {
				return err
			}
			if err := validateEnum("--status-group", statusGroups, bamboohr.ApplicationStatusGroups...); err != nil {
				return err
			}
			if err := validateEnum("--job-status", jobStatus, bamboohr.JobStatusGroups...); err != nil {
				return err
			}
			// The API filters to open jobs by default, so a company whose
			// openings are all filled or on hold gets nothing back from an
			// unfiltered request. Naming a job is itself a scope, so the
			// default only applies without one.
			if jobStatus == "" && jobID == 0 {
				jobStatus = "ALL"
				a.logf("no --job or --job-status given; including every job status")
			}

			opts := bamboohr.ApplicationListOptions{
				Page:            page,
				JobID:           jobID,
				StatusID:        statusID,
				StatusGroups:    statusGroups,
				JobStatusGroups: jobStatus,
				Search:          search,
				SortBy:          sortBy,
				SortOrder:       sortOrder,
			}
			if newSince != "" {
				since, err := parseSince(newSince)
				if err != nil {
					return fmt.Errorf("--new-since: %w", err)
				}
				opts.NewSince = since.UTC().Format("2006-01-02T15:04:05Z")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if all {
				applications, err := client.ListAllApplications(ctx, opts)
				if err != nil {
					return err
				}
				return a.printer().Print(applications, applicationTable(applications))
			}

			result, err := client.ListApplications(ctx, opts)
			if err != nil {
				return err
			}
			if !result.PaginationComplete {
				a.logf("more pages available; use --all for every application")
			}
			return a.printer().Print(result, applicationTable(result.Applications))
		},
	}

	f := cmd.Flags()
	f.IntVar(&jobID, "job", 0, "restrict to one job opening ID")
	f.StringVar(&statusID, "status", "", "restrict to one applicant status ID")
	f.StringVar(&statusGroups, "status-group", "",
		"application status groups, comma-separated: "+strings.Join(bamboohr.ApplicationStatusGroups, ", "))
	f.StringVar(&jobStatus, "job-status", "",
		"job status groups, comma-separated: "+strings.Join(bamboohr.JobStatusGroups, ", "))
	f.StringVar(&search, "search", "", "server-side search over the applicant's details")
	f.StringVar(&newSince, "new-since", "", "only applications since this time, e.g. 7d or an RFC 3339 timestamp")
	f.StringVar(&sortBy, "sort-by", "", "first_name, job_title, rating, phone, status, last_updated or created_date")
	f.StringVar(&sortOrder, "sort-order", "", "ASC or DESC")
	f.BoolVar(&all, "all", false, "fetch every page")
	f.IntVar(&page, "page", 0, "page number, 1-based")
	return cmd
}

func applicationTable(applications []bamboohr.Application) *Table {
	t := &Table{Headers: []string{"id", "applicant", "email", "job", "status", "rating", "source", "applied"}}
	for _, app := range applications {
		rating := ""
		if app.Rating != nil {
			rating = strconv.Itoa(*app.Rating)
		}
		t.AddRow(app.ID, app.Applicant.Name(), app.Applicant.Email,
			app.Job.Title.Label, app.Status.Label, rating,
			app.Applicant.Source, dateOnly(app.AppliedDate))
	}
	return t
}

func newRecruitingApplicationCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "application <application-id>",
		Aliases: []string{"candidate"},
		Short:   "Get one job application",
		Long: `Get one application, with its attachments and when its status last moved.

Run "bamboohr recruiting applications" to find an application ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "application id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			app, err := client.GetApplication(ctx, id)
			if err != nil {
				return err
			}

			pairs := [][2]string{
				{"id", strconv.Itoa(app.ID)},
				{"applicant", app.Applicant.Name()},
				{"email", app.Applicant.Email},
				{"source", app.Applicant.Source},
				{"status", app.Status.Label},
				{"statusChanged", app.Status.DateChanged},
				{"applied", app.AppliedDate},
			}
			if app.Rating != nil {
				pairs = append(pairs, [2]string{"rating", strconv.Itoa(*app.Rating)})
			}
			if app.ResumeFileID != nil {
				pairs = append(pairs, [2]string{"resumeFileId", strconv.Itoa(*app.ResumeFileID)})
			}
			if app.CoverLetterFileID != nil {
				pairs = append(pairs, [2]string{"coverLetterFileId", strconv.Itoa(*app.CoverLetterFileID)})
			}
			for i, att := range app.Attachments {
				pairs = append(pairs, [2]string{
					fmt.Sprintf("attachment.%d", i+1),
					fmt.Sprintf("%s (%d)", att.Name, att.ID),
				})
			}
			return a.printer().keyValue(app, pairs)
		},
	}
}

func newRecruitingStatusesCommand(a *app) *cobra.Command {
	var includeDisabled bool

	cmd := &cobra.Command{
		Use:     "statuses",
		Aliases: []string{"stages"},
		Short:   "List the applicant statuses",
		Long: `List the stages an application can sit in.

The ID column is what "recruiting set-status" takes. Statuses a company has
turned off are hidden unless you pass --disabled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			statuses, err := client.ListApplicantStatuses(ctx)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"id", "name", "code", "enabled", "custom"}}
			hidden := 0
			for _, s := range statuses {
				if !s.Enabled && !includeDisabled {
					hidden++
					continue
				}
				// A status with no internal code is one the company added.
				t.AddRow(s.ID, s.Name, deref(s.Code), s.Enabled, s.Code == nil)
			}
			if hidden > 0 {
				// Not gated on --verbose: someone looking for a status by name
				// would otherwise conclude it does not exist.
				fmt.Fprintf(a.errOut,
					"note: %d disabled status(es) hidden; use --disabled to include them\n", hidden)
			}
			return a.printer().Print(statuses, t)
		},
	}
	cmd.Flags().BoolVar(&includeDisabled, "disabled", false, "include statuses the company has turned off")
	return cmd
}

func newRecruitingSetStatusCommand(a *app) *cobra.Command {
	var statusID int

	cmd := &cobra.Command{
		Use:   "set-status <application-id>",
		Short: "Move an application to a different status",
		Long: `Move an application to a different status.

--status takes a status ID, not a name: run "bamboohr recruiting statuses" to
find it. This is visible to your colleagues in BambooHR and may trigger the
account's candidate notifications, so check the ID before running it.`,
		Example: `  bamboohr recruiting set-status 4821 --status 2`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "application id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if err := client.SetApplicationStatus(ctx, id, statusID); err != nil {
				return err
			}
			fmt.Fprintf(a.errOut, "application %d moved to status %d\n", id, statusID)
			return nil
		},
	}
	cmd.Flags().IntVar(&statusID, "status", 0, "the status ID to move the application to (required)")
	_ = cmd.MarkFlagRequired("status")
	return cmd
}

func newRecruitingCommentCommand(a *app) *cobra.Command {
	var comment string

	cmd := &cobra.Command{
		Use:   "comment <application-id>",
		Short: "Add a comment to an application",
		Long: `Add a comment to an application.

The comment is visible to everyone with access to the candidate in BambooHR.
Pass --message -, or @file, to read it from stdin or a file.`,
		Example: `  bamboohr recruiting comment 4821 --message "Strong on the systems round"
  bamboohr recruiting comment 4821 --message @notes.txt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "application id")
			if err != nil {
				return err
			}
			text, err := readInput(comment)
			if err != nil {
				return fmt.Errorf("--message: %w", err)
			}
			if strings.TrimSpace(string(text)) == "" {
				return fmt.Errorf("--message is empty")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			if err := client.CommentOnApplication(ctx, id, string(text)); err != nil {
				return err
			}
			fmt.Fprintf(a.errOut, "commented on application %d\n", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&comment, "message", "m", "", "the comment text; @file or - for stdin (required)")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func newRecruitingHiringLeadsCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "hiring-leads",
		Aliases: []string{"leads"},
		Short:   "List the employees who can lead hiring",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			leads, err := client.ListHiringLeads(ctx)
			if err != nil {
				return err
			}
			t := &Table{Headers: []string{"employeeId", "name"}}
			for _, l := range leads {
				t.AddRow(l.EmployeeID, l.PreferredFullName)
			}
			return a.printer().Print(leads, t)
		},
	}
}

func newRecruitingLocationsCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "locations",
		Short: "List the locations a job opening can be posted to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			locations, err := client.ListRecruitingLocations(ctx)
			if err != nil {
				return err
			}
			records := make([]map[string]any, 0, len(locations))
			for _, l := range locations {
				records = append(records, l.Fields)
			}
			return a.printer().Print(locations, mapTable(records, []string{"id", "name", "city", "state", "country"}))
		},
	}
}

// validateEnum rejects a flag value outside the set the API accepts, so the
// error names the flag rather than arriving as a 400.
func validateEnum(flag, value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	if containsFold(allowed, value) {
		return nil
	}
	return fmt.Errorf("unknown %s %q: want one of %s", flag, value, strings.Join(allowed, ", "))
}

// labelOf renders an optional lookup value.
func labelOf(l *bamboohr.Labelled) string {
	if l == nil {
		return ""
	}
	return l.Label
}

func locationOf(l *bamboohr.JobLocation) string {
	if l == nil {
		return ""
	}
	return l.Label
}

func leadOf(l *bamboohr.JobHiringLead) string {
	if l == nil {
		return ""
	}
	return l.Name()
}

// dateOnly trims the time from an ISO 8601 timestamp, which is all a listing
// needs and keeps the column narrow.
func dateOnly(timestamp string) string {
	if date, _, found := strings.Cut(timestamp, "T"); found {
		return date
	}
	return timestamp
}

func newRecruitingFilesCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "files <application-id>",
		Aliases: []string{"attachments"},
		Short:   "List the files on an application",
		Long: `List a candidate's resume, cover letter and any other attachments.

The ID column is what "recruiting download --file" takes. Download them all
with "recruiting download <application-id>".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "application id")
			if err != nil {
				return err
			}
			ctx, cancel := a.ctx(cmd)
			defer cancel()

			app, err := client.GetApplication(ctx, id)
			if err != nil {
				return err
			}
			files := app.Files()

			t := &Table{Headers: []string{"id", "kind", "name"}}
			for _, f := range files {
				t.AddRow(f.ID, string(f.Kind), f.Name)
			}
			if err := a.printer().Print(files, t); err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Fprintf(a.errOut, "note: application %d has no resume, cover letter or attachments\n", id)
			}
			return nil
		},
	}
}

func newRecruitingDownloadCommand(a *app) *cobra.Command {
	var (
		fileID     int
		resumeOnly bool
		dir        string
		outFile    string
	)

	cmd := &cobra.Command{
		Use:   "download <application-id>",
		Short: "Download a candidate's resume and attachments",
		Long: `Download the files on an application.

With no flags it fetches everything the application carries: the resume, the
cover letter and any other attachments. --resume takes just the resume, and
--file takes one file by ID, as listed by "recruiting files".

BambooHR sends no filename with these downloads, so names are built from the
application ID and the file's role, with the extension taken from the content
type: application-4821-resume.pdf. --out names a single file yourself, and only
works when exactly one file is being fetched.

These are candidates' personal documents. Mind where you put them.`,
		Example: `  bamboohr recruiting download 24732
  bamboohr recruiting download 24732 --resume
  bamboohr recruiting download 24732 --dir ./candidates
  bamboohr recruiting download 24732 --file 25625 --out resume.pdf
  bamboohr recruiting download 24732 --resume --out - | pdftotext - -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.api()
			if err != nil {
				return err
			}
			id, err := parseID(args[0], "application id")
			if err != nil {
				return err
			}
			if fileID > 0 && resumeOnly {
				return fmt.Errorf("--file and --resume both choose what to download: use one or the other")
			}
			if outFile != "" && dir != "" {
				return fmt.Errorf("--out and --dir both choose where to write: use one or the other")
			}

			ctx, cancel := a.ctx(cmd)
			defer cancel()

			// A file named by ID needs no lookup, which also lets a caller
			// fetch one the listing did not report.
			var files []bamboohr.ApplicationFile
			if fileID > 0 {
				files = []bamboohr.ApplicationFile{{ID: fileID, Kind: bamboohr.FileAttachment}}
			} else {
				app, err := client.GetApplication(ctx, id)
				if err != nil {
					return err
				}
				files = app.Files()
				if resumeOnly {
					files = keepKind(files, bamboohr.FileResume)
					if len(files) == 0 {
						return fmt.Errorf("application %d has no resume", id)
					}
				}
				if len(files) == 0 {
					return fmt.Errorf("application %d has no resume, cover letter or attachments", id)
				}
			}

			if outFile != "" && len(files) > 1 {
				return fmt.Errorf("--out names one file but %d are being downloaded: use --dir, or --file to pick one",
					len(files))
			}

			// A directory the user named is one they mean to use.
			if dir != "" {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return fmt.Errorf("creating %s: %w", dir, err)
				}
			}

			for _, f := range files {
				a.logf("downloading file %d (%s)", f.ID, f.Kind)
				err := a.saveDownload(
					downloadTarget{
						Path:   outFile,
						Dir:    dir,
						Prefix: fmt.Sprintf("application-%d-", id),
						// A resume carries no filename of its own, so its role
						// becomes its name.
						Fallback: applicationFileBase(f),
					},
					func(w io.Writer) (*bamboohr.Download, error) {
						return client.DownloadApplicationFile(ctx, id, f.ID, w)
					})
				if err != nil {
					return err
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.IntVar(&fileID, "file", 0, "download one file by ID, as listed by \"recruiting files\"")
	f.BoolVar(&resumeOnly, "resume", false, "download only the resume")
	f.StringVar(&dir, "dir", "", "directory to write into (default: the working directory)")
	f.StringVar(&outFile, "out", "", "write a single file to this path, or - for stdout")
	return cmd
}

// keepKind filters the files down to one role.
func keepKind(files []bamboohr.ApplicationFile, kind bamboohr.ApplicationFileKind) []bamboohr.ApplicationFile {
	var kept []bamboohr.ApplicationFile
	for _, f := range files {
		if f.Kind == kind {
			kept = append(kept, f)
		}
	}
	return kept
}

// applicationFileBase names a downloaded file, without the application prefix
// or an extension.
//
// These downloads mostly carry no Content-Disposition, so nothing in the
// response suggests a name. An attachment has one of its own; a resume or cover
// letter is identified only by ID, so its role becomes its name.
func applicationFileBase(f bamboohr.ApplicationFile) string {
	if name := sanitiseFileName(f.Name); name != "" {
		return name
	}
	switch f.Kind {
	case bamboohr.FileResume:
		return "resume"
	case bamboohr.FileCoverLetter:
		return "cover-letter"
	default:
		return fmt.Sprintf("file-%d", f.ID)
	}
}

// sanitiseFileName reduces a server-supplied name to something safe to create
// in a local directory.
func sanitiseFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	// Base leaves these when the input was only separators or dots.
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}
	return name
}
