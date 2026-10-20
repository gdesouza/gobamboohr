package bamboohr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Labelled is the {id, label} pair the applicant tracking endpoints use for
// every lookup value: a job title, a department, a location, a status.
type Labelled struct {
	// ID is nil when the value is free text rather than a list entry.
	ID    *int   `json:"id"`
	Label string `json:"label"`
}

// String returns the label, so a Labelled prints as the name a user recognises.
func (l Labelled) String() string { return l.Label }

// JobOpening is a job opening and its applicant counts.
type JobOpening struct {
	ID    int      `json:"id"`
	Title Labelled `json:"title"`
	// PostedDate is an ISO 8601 timestamp.
	PostedDate string         `json:"postedDate"`
	Location   *JobLocation   `json:"location"`
	Department *Labelled      `json:"department"`
	Status     Labelled       `json:"status"`
	HiringLead *JobHiringLead `json:"hiringLead"`
	// NewApplicantsCount counts applicants still in the New status.
	NewApplicantsCount int `json:"newApplicantsCount"`
	// ActiveApplicantsCount counts applicants in any active status.
	ActiveApplicantsCount int `json:"activeApplicantsCount"`
	TotalApplicantsCount  int `json:"totalApplicantsCount"`
	// PostingURL is the public posting, nil when the opening is not posted.
	PostingURL *string `json:"postingUrl"`
}

// JobLocation is a job opening's location.
type JobLocation struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
	// Address holds the location's address fields, which vary by country.
	Address map[string]any `json:"address"`
}

// JobHiringLead is the employee leading a job opening's hiring.
type JobHiringLead struct {
	EmployeeID int     `json:"employeeId"`
	FirstName  string  `json:"firstName"`
	LastName   string  `json:"lastName"`
	Avatar     *string `json:"avatar"`
}

// Name returns the hiring lead's full name.
func (l JobHiringLead) Name() string {
	return strings.TrimSpace(l.FirstName + " " + l.LastName)
}

// JobOpeningListOptions controls ListJobOpenings.
type JobOpeningListOptions struct {
	// StatusGroups selects job status groups: ALL, DRAFT_AND_OPEN, Open,
	// Filled, Draft, Deleted, "On Hold" or Canceled. Empty uses the API's
	// default of every non-deleted opening.
	StatusGroups string
	// StatusIDs limits the results to these job status IDs.
	StatusIDs []string
	// SortBy is one of count, title, lead, created or status.
	SortBy string
	// SortOrder is "ASC" or "DESC".
	SortOrder string
}

// ListJobOpenings returns the company's job openings with their applicant
// counts. The endpoint is not paginated.
func (c *Client) ListJobOpenings(ctx context.Context, opts JobOpeningListOptions) ([]JobOpening, error) {
	q := url.Values{}
	if opts.StatusGroups != "" {
		q.Set("statusGroups", opts.StatusGroups)
	}
	if len(opts.StatusIDs) > 0 {
		q.Set("status_ids", strings.Join(opts.StatusIDs, ","))
	}
	if opts.SortBy != "" {
		q.Set("sortBy", opts.SortBy)
	}
	if opts.SortOrder != "" {
		q.Set("sortOrder", strings.ToUpper(opts.SortOrder))
	}

	var out []JobOpening
	if err := c.get(ctx, "/api/v1/applicant_tracking/jobs", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Applicant is the person behind an application.
type Applicant struct {
	ID        int     `json:"id"`
	FirstName string  `json:"firstName"`
	LastName  string  `json:"lastName"`
	Email     string  `json:"email"`
	Source    string  `json:"source"`
	Avatar    *string `json:"avatar"`
}

// Name returns the applicant's full name.
func (a Applicant) Name() string {
	return strings.TrimSpace(a.FirstName + " " + a.LastName)
}

// Application is one application to a job opening.
type Application struct {
	ID int `json:"id"`
	// AppliedDate is an ISO 8601 timestamp.
	AppliedDate string   `json:"appliedDate"`
	Status      Labelled `json:"status"`
	// Rating is the candidate's 1-to-5 rating, nil when unrated.
	Rating    *int      `json:"rating"`
	Applicant Applicant `json:"applicant"`
	Job       struct {
		ID    int      `json:"id"`
		Title Labelled `json:"title"`
	} `json:"job"`
}

// ApplicationList is one page of applications.
//
// This endpoint pages with a "complete" flag and a next-page URL rather than
// the counters the other list endpoints return.
type ApplicationList struct {
	Applications []Application `json:"applications"`
	// PaginationComplete is true once the last page has been returned.
	PaginationComplete bool `json:"paginationComplete"`
	// NextPageURL is the next page, nil on the last page.
	NextPageURL *string `json:"nextPageUrl"`
}

// ApplicationListOptions controls ListApplications.
type ApplicationListOptions struct {
	// Page is 1-based; zero means the first page.
	Page int
	// JobID limits the results to one job opening.
	JobID int
	// StatusID limits the results to one applicant status, by ID.
	StatusID string
	// StatusGroups limits the results to these application status groups:
	// ALL, ALL_ACTIVE, NEW, ACTIVE, INACTIVE or HIRED.
	StatusGroups string
	// JobStatusGroups limits the results by the job's status group: ALL,
	// DRAFT_AND_OPEN, Open, Filled, Draft, Deleted, "On Hold" or Canceled.
	//
	// The API's own default covers open jobs only, so a company whose openings
	// are all filled or on hold gets an empty list from an unfiltered request.
	JobStatusGroups string
	// Search matches the applicant's details server-side.
	Search string
	// NewSince returns only applications created since this ISO 8601 timestamp.
	NewSince string
	// SortBy is one of first_name, job_title, rating, phone, status,
	// last_updated or created_date.
	SortBy string
	// SortOrder is "ASC" or "DESC".
	SortOrder string
}

func (o ApplicationListOptions) query() url.Values {
	q := url.Values{}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.JobID > 0 {
		q.Set("jobId", strconv.Itoa(o.JobID))
	}
	if o.StatusID != "" {
		q.Set("applicationStatusId", o.StatusID)
	}
	if o.StatusGroups != "" {
		q.Set("applicationStatus", o.StatusGroups)
	}
	if o.JobStatusGroups != "" {
		q.Set("jobStatusGroups", o.JobStatusGroups)
	}
	if o.Search != "" {
		q.Set("searchString", o.Search)
	}
	if o.NewSince != "" {
		q.Set("newSince", o.NewSince)
	}
	if o.SortBy != "" {
		q.Set("sortBy", o.SortBy)
	}
	if o.SortOrder != "" {
		q.Set("sortOrder", strings.ToUpper(o.SortOrder))
	}
	return q
}

// ListApplications returns one page of job applications.
func (c *Client) ListApplications(ctx context.Context, opts ApplicationListOptions) (*ApplicationList, error) {
	var out ApplicationList
	if err := c.get(ctx, "/api/v1/applicant_tracking/applications", opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAllApplications walks every page of matching applications.
func (c *Client) ListAllApplications(ctx context.Context, opts ApplicationListOptions) ([]Application, error) {
	var all []Application
	if opts.Page <= 0 {
		opts.Page = 1
	}
	for {
		page, err := c.ListApplications(ctx, opts)
		if err != nil {
			return all, err
		}
		all = append(all, page.Applications...)
		// The flag is authoritative; the page counter is not returned here.
		if page.PaginationComplete || len(page.Applications) == 0 {
			return all, nil
		}
		opts.Page++
	}
}

// ApplicationAttachment is a file attached to an application.
type ApplicationAttachment struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	FileURL string `json:"fileUrl"`
}

// ApplicationDetails is one application with its full history.
type ApplicationDetails struct {
	ID          int    `json:"id"`
	AppliedDate string `json:"appliedDate"`
	Status      struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
		// DateChanged is when the status last moved.
		DateChanged string `json:"dateChanged"`
	} `json:"status"`
	Rating            *int                    `json:"rating"`
	Applicant         Applicant               `json:"applicant"`
	ResumeFileID      *int                    `json:"resumeFileId"`
	CoverLetterFileID *int                    `json:"coverLetterFileId"`
	AttachmentCount   *int                    `json:"attachmentCount"`
	Attachments       []ApplicationAttachment `json:"attachments"`
	// Fields holds the whole response, so details this struct does not name
	// are still reachable.
	Fields map[string]any `json:"-"`
}

// UnmarshalJSON decodes the details into both the typed fields and Fields.
func (d *ApplicationDetails) UnmarshalJSON(data []byte) error {
	type alias ApplicationDetails
	var typed alias
	if err := jsonUnmarshal(data, &typed); err != nil {
		return err
	}
	*d = ApplicationDetails(typed)
	return jsonUnmarshal(data, &d.Fields)
}

// GetApplication returns one application with its attachments and status
// history.
func (c *Client) GetApplication(ctx context.Context, id int) (*ApplicationDetails, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bamboohr: application id is required")
	}
	var out ApplicationDetails
	path := "/api/v1/applicant_tracking/applications/" + strconv.Itoa(id)
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplicantStatus is one stage an application can sit in.
type ApplicantStatus struct {
	ID string `json:"id"`
	// Code is BambooHR's internal code, nil for a status the company added.
	Code *string `json:"code"`
	Name string  `json:"name"`
	// TranslatedName is the name in the account's language.
	TranslatedName string  `json:"translatedName"`
	Description    *string `json:"description"`
	// Enabled reports whether the status can currently be selected.
	Enabled bool `json:"enabled"`
	// Manageable reports whether the company may edit or delete the status.
	Manageable bool `json:"manageable"`
}

// ListApplicantStatuses returns the stages an application can be moved to,
// which is where SetApplicationStatus gets its IDs.
func (c *Client) ListApplicantStatuses(ctx context.Context) ([]ApplicantStatus, error) {
	var out []ApplicantStatus
	if err := c.get(ctx, "/api/v1/applicant_tracking/statuses", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HiringLead is an employee who can lead hiring on a job opening.
type HiringLead struct {
	EmployeeID int `json:"employeeId"`
	// PreferredFullName is the employee's preferred full name.
	PreferredFullName string `json:"preferredFullName"`
}

// ListHiringLeads returns the employees who can be set as a job opening's
// hiring lead.
func (c *Client) ListHiringLeads(ctx context.Context) ([]HiringLead, error) {
	var out []HiringLead
	if err := c.get(ctx, "/api/v1/applicant_tracking/hiring_leads", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RecruitingLocation is a location a job opening can be posted to.
type RecruitingLocation struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Fields holds the whole entry, since the address shape varies by country.
	Fields map[string]any `json:"-"`
}

// UnmarshalJSON decodes a location into both its typed fields and Fields.
func (l *RecruitingLocation) UnmarshalJSON(data []byte) error {
	type alias RecruitingLocation
	var typed alias
	if err := jsonUnmarshal(data, &typed); err != nil {
		return err
	}
	*l = RecruitingLocation(typed)
	return jsonUnmarshal(data, &l.Fields)
}

// ListRecruitingLocations returns the locations a job opening can be posted to.
func (c *Client) ListRecruitingLocations(ctx context.Context) ([]RecruitingLocation, error) {
	var out []RecruitingLocation
	if err := c.get(ctx, "/api/v1/applicant_tracking/locations", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetApplicationStatus moves an application to a different status.
//
// statusID comes from ListApplicantStatuses; the endpoint takes the numeric ID,
// not the status name.
func (c *Client) SetApplicationStatus(ctx context.Context, applicationID, statusID int) error {
	if applicationID <= 0 {
		return fmt.Errorf("bamboohr: application id is required")
	}
	if statusID <= 0 {
		return fmt.Errorf("bamboohr: status id is required")
	}
	body := map[string]int{"status": statusID}
	path := "/api/v1/applicant_tracking/applications/" + strconv.Itoa(applicationID) + "/status"
	return c.do(ctx, http.MethodPost, path, nil, body, nil)
}

// CommentOnApplication adds a comment to an application.
func (c *Client) CommentOnApplication(ctx context.Context, applicationID int, comment string) error {
	if applicationID <= 0 {
		return fmt.Errorf("bamboohr: application id is required")
	}
	if strings.TrimSpace(comment) == "" {
		return fmt.Errorf("bamboohr: comment text is required")
	}
	body := map[string]string{"type": "comment", "comment": comment}
	path := "/api/v1/applicant_tracking/applications/" + strconv.Itoa(applicationID) + "/comments"
	return c.do(ctx, http.MethodPost, path, nil, body, nil)
}

// JobStatusGroups are the values the job and application endpoints accept for a
// job's status group.
var JobStatusGroups = []string{"ALL", "DRAFT_AND_OPEN", "Open", "Filled", "Draft", "Deleted", "On Hold", "Canceled"}

// ApplicationStatusGroups are the values the application endpoint accepts for
// an application's status group.
var ApplicationStatusGroups = []string{"ALL", "ALL_ACTIVE", "NEW", "ACTIVE", "INACTIVE", "HIRED"}

// ApplicationFileKind says what role a file plays on an application.
type ApplicationFileKind string

// The roles a file can play on an application.
const (
	FileResume      ApplicationFileKind = "resume"
	FileCoverLetter ApplicationFileKind = "coverLetter"
	FileAttachment  ApplicationFileKind = "attachment"
)

// ApplicationFile is one file attached to an application.
type ApplicationFile struct {
	ID   int
	Kind ApplicationFileKind
	// Name is the original filename. It is only known for an attachment: a
	// resume and a cover letter are identified by ID alone.
	Name string
	// URL is the download URL the API supplied for an attachment, empty for a
	// resume or cover letter. DownloadApplicationFile does not need it.
	URL string
}

// Files returns every file on the application, resume first.
//
// A resume and a cover letter are reported as bare IDs on the application,
// while extra attachments arrive as a list with names. This flattens the two
// into one set so a caller does not have to handle them separately.
func (d *ApplicationDetails) Files() []ApplicationFile {
	var files []ApplicationFile
	if d.ResumeFileID != nil && *d.ResumeFileID > 0 {
		files = append(files, ApplicationFile{ID: *d.ResumeFileID, Kind: FileResume})
	}
	if d.CoverLetterFileID != nil && *d.CoverLetterFileID > 0 {
		files = append(files, ApplicationFile{ID: *d.CoverLetterFileID, Kind: FileCoverLetter})
	}
	for _, att := range d.Attachments {
		// The resume is sometimes repeated in the attachment list; reporting it
		// twice would download it twice.
		if d.ResumeFileID != nil && att.ID == *d.ResumeFileID {
			continue
		}
		if d.CoverLetterFileID != nil && att.ID == *d.CoverLetterFileID {
			continue
		}
		files = append(files, ApplicationFile{
			ID:   att.ID,
			Kind: FileAttachment,
			Name: att.Name,
			URL:  att.FileURL,
		})
	}
	return files
}

// DownloadApplicationFile streams one of an application's files into w.
//
// This endpoint is not in BambooHR's published OpenAPI specification. The
// documented route is the fileUrl on each entry of an application's attachments
// list, but that list comes back null on applications that do carry a resume,
// so the resume would be undownloadable through the documented path. The path
// used here was verified against live applications, returning the expected PDF
// and PNG content, and is the only way to fetch a file identified only by
// resumeFileId or coverLetterFileId.
//
// Being undocumented, it may change without notice. A caller that needs to
// survive that should fall back to the attachments list's fileUrl.
func (c *Client) DownloadApplicationFile(ctx context.Context, applicationID, fileID int, w io.Writer) (*Download, error) {
	if applicationID <= 0 {
		return nil, fmt.Errorf("bamboohr: application id is required")
	}
	if fileID <= 0 {
		return nil, fmt.Errorf("bamboohr: file id is required")
	}
	path := "/api/v1/applicant_tracking/applications/" + strconv.Itoa(applicationID) +
		"/files/" + strconv.Itoa(fileID)
	return c.download(ctx, path, nil, w)
}
