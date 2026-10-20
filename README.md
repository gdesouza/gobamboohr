# bamboohr

A Go client for the [BambooHR API](https://documentation.bamboohr.com/) and a
CLI built on top of it.

- `pkg/bamboohr` — an importable client: typed requests and responses,
  API-key auth, retries on throttling, and typed errors.
- `cmd/bamboohr` — the CLI, with `table`, `json` and `csv` output.

## Install

With Go (1.26+), no clone needed:

```sh
go install github.com/gdesouza/gobamboohr/cmd/bamboohr@latest
```

This puts `bamboohr` in `$(go env GOBIN)` (default `$(go env GOPATH)/bin`, usually
`~/go/bin`); make sure that directory is on your `PATH`. Check with
`bamboohr --help`.

Or from a clone:

```sh
make build          # -> bin/bamboohr
make install        # -> $GOBIN/bamboohr
```

Or as a library:

```sh
go get github.com/gdesouza/gobamboohr/pkg/bamboohr
```

## Credentials

Generate an API key from **API Keys** in your BambooHR account settings. The
subdomain is the company label in the host: for `https://acme.bamboohr.com` it
is `acme`.

Resolved most specific first:

1. `--subdomain`, `--api-key` / `--api-key-file`
2. `BAMBOOHR_SUBDOMAIN`, `BAMBOOHR_API_KEY`, `BAMBOOHR_API_KEY_FILE`
3. `~/.config/bamboohr/config.json` (override the path with `BAMBOOHR_CONFIG`):

```json
{
  "subdomain": "acme",
  "apiKey": "your-api-key"
}
```

The sources combine, so a common setup is the subdomain in the config file and
the key in the environment. `apiKeyFile` is also accepted in the config file if
you keep the key in a separate file.

Passing the key as a flag puts it in your shell history and in the process
list — prefer the environment or `--api-key-file`.

Check that it works:

```sh
bamboohr meta company
```

## CLI

Every command takes `--output table|json|csv` (`-o`). `table` is the default and
is meant for reading; `json` returns the API response untouched; `csv` sits
between the two.

For listings whose columns come from the response rather than being chosen
(`employee list`, `employee directory`, `employee table`), the formats differ
deliberately:

| | `table` | `csv` | `json` |
| --- | --- | --- | --- |
| Photo columns (`photoUrl`, `photoUploaded`, `canUploadPhoto`) | omitted | omitted | kept |
| Columns empty in every row | omitted | kept | kept |

`photoUrl` arrives in the default field set as a signed URL several hundred
characters long, and the other two are flags for the BambooHR web UI's upload
button — none of them earn a column. Name one in `--fields` to bring it back.
Columns that are empty for every record are dropped from the table because they
are dead width on screen, but kept in CSV so a script sees the same schema
whatever a given page contains.

Progress and diagnostics go to stderr, so `-o csv > file.csv` and `-o json | jq`
stay clean.

### Employees

```sh
bamboohr employee search morgan
bamboohr employee search alex engineering
bamboohr employee get 123
bamboohr employee get 123 --fields firstName,lastName,workEmail
bamboohr employee get 123 --all-fields
bamboohr employee list --limit 50
bamboohr employee list --status all                     # including former staff
bamboohr employee list --filter departmentName=Robotics --fields workEmail,hireDate
bamboohr employee list --all -o csv > employees.csv
bamboohr employee directory
bamboohr employee table 123 jobInfo
bamboohr employee changed --since 24h
bamboohr employee create --set firstName=Ada --set lastName=Lovelace
bamboohr employee update 123 --set workPhone=555-0100
bamboohr employee photo 123 --size large --file ada.jpg
bamboohr employee files 123
bamboohr employee file 123 456 --file offer.pdf
```

### Searching

`employee search` matches a term against every field in the company directory:

```sh
bamboohr employee search morgan
bamboohr employee search alex engineering      # both terms, not necessarily the same field
bamboohr employee search "morgan"           # quote for a phrase
bamboohr employee search springfield --in location
bamboohr employee search morgan --matched        # why did each row match?
```

```
ID   DISPLAYNAME    LOCATION   SUPERVISOR
208  Alex Morgan   Springfield  Pat Rivera
666  Sam Patel   Springfield  Alex Morgan
```

Every field is searched, but the output is a fixed four columns; unlike the
other listings they are not derived from the response, so they show even when a
company leaves one empty. Records matching on a name rank first, then by
surname.

`--matched` adds a column naming the field each hit came from, which explains a
result the row does not account for — above, the second person appears only
because their manager is named Morgan:

```sh
bamboohr employee search morgan --matched
```

`--output json` returns the whole directory record for each match.

**Why this is client-side.** The list endpoint's filters are per-field and
combined with AND, so there is no way to ask the API for "this term in the name
*or* the title *or* the email". The directory, by contrast, is one request and
comes back fully readable, so `search` fetches it and matches locally.

The trade-off is scope: the directory holds current employees the company
publishes, so former employees are absent, as is any field the directory is not
configured to publish. For those, filter the list instead — one field at a time:

```sh
bamboohr employee list --status all --filter lastName=morgan
```

`employee get` returns **only** the fields a request names, so it asks for a
general-purpose set by default (name, job, contact details, hire date). Use
`--fields` to choose your own, or `--all-fields` for everything this account has
an API name for. `bamboohr meta fields` browses the catalogue.

Two traps worth knowing:

- **Unknown and unauthorised field names are dropped silently** — no error, the
  field is just absent. A record that comes back carrying nothing but its ID
  means the key could not read any of it; the CLI says so on stderr.
- **The names differ between endpoints.** A job title is `jobTitle` on
  `employee get` but `jobTitleName` on `employee list`; a manager is
  `supervisor` on one and `reportsToName` on the report endpoints. The library
  fills its typed fields from either spelling, but a `--fields` value has to
  match the endpoint you are calling.

`--all-fields` holds nothing back: where the key has the permission it returns
compensation, dates of birth, home addresses and national identifiers. Think
about where the output is going.

`employee list` filters server-side. Text fields match on a case-insensitive
substring; IDs, enums, booleans and dates match exactly.

**It lists active employees only by default** (`--status inactive` or
`--status all` for the rest). That default does more than hide former staff.
Field permissions are per record, not per key, and the endpoint returns every
record your key can see the *existence* of — including ones whose fields it may
not read, which arrive with every value masked and render as a row of dashes.
A status filter drops them, because a masked status matches no value.

On one real tenant the difference was stark:

| | Records |
| --- | --- |
| no filter | 1029 |
| `--status active` | 155 |
| `--status inactive` | 1 |

The missing 873 are neither — their status is masked. So `--status all` is the
honest "everything" but is mostly unreadable rows; the active default is what
makes the listing useful. When blank rows do appear, a note on stderr says how
many, so they do not look like a bug.

### Time off

```sh
bamboohr timeoff whos-out                                   # today .. +14 days
bamboohr timeoff whos-out --start 2026-10-01 --end 2026-10-31
bamboohr timeoff whos-out --department 12 -o csv

bamboohr timeoff requests --status requested
bamboohr timeoff requests --employee 123 --start 2026-01-01 --end 2026-12-31
bamboohr timeoff requests --filter "status eq 'APPROVED' and amount gt 8"
bamboohr timeoff request 456 --actions

bamboohr timeoff types                                      # category IDs and units
bamboohr timeoff balance 123 --as-of 2026-12-31
bamboohr timeoff policies

bamboohr timeoff create --employee 123 --category 22 \
    --start 2026-11-02 --end 2026-11-04 --amount 24 --note "Family trip"
bamboohr timeoff approve 456 --note "Enjoy"
bamboohr timeoff deny 456 --note "Need coverage"
bamboohr timeoff cancel 456
```

The convenience flags build the server-side filter for you; `--filter` takes a
raw OData expression for anything they cannot express. BambooHR supports `eq`,
`ne`, `lt`, `le`, `gt`, `ge` and `in`, combined with `and` — there is no `or`,
no `not` and no grouping.

### Holidays

```sh
bamboohr holiday list                          # the current year
bamboohr holiday list --year 2026
bamboohr holiday list --from 2026-01-01 --to 2026-06-30 --public
bamboohr holiday get 42
bamboohr holiday catalog --country CA --year 2026
```

`holiday list` covers what the company observes; `holiday catalog` is BambooHR's
read-only global reference list that a company holiday can be seeded from. The
`SOURCE` column says which of the two a holiday came from, and `DAYS` counts
inclusively — 24 to 31 December is eight days.

With no date range it lists the current year, because the endpoint has no
default of its own.

### Recruiting

```sh
bamboohr recruiting jobs                       # openings + applicant counts
bamboohr recruiting applications --job 937
bamboohr recruiting applications --status-group NEW --all
bamboohr recruiting applications --new-since 7d
bamboohr recruiting application 24732
bamboohr recruiting files 24732                 # resume, cover letter, attachments
bamboohr recruiting download 24732             # fetch them all
bamboohr recruiting download 24732 --resume --dir ./candidates
bamboohr recruiting download 24732 --file 25625 --out cv.pdf
bamboohr recruiting statuses                   # the IDs set-status takes
bamboohr recruiting set-status 4821 --status 2
bamboohr recruiting comment 4821 --message "Strong on the systems round"
bamboohr recruiting hiring-leads
bamboohr recruiting locations
```

`recruiting jobs` is the starting point: the `NEW` count is candidates nobody
has looked at yet.

Three filters look similar and are not:

| Flag | Filters on | Values |
| --- | --- | --- |
| `--status` | one applicant status, by ID | from `recruiting statuses` |
| `--status-group` | the application's stage group | `ALL`, `ALL_ACTIVE`, `NEW`, `ACTIVE`, `INACTIVE`, `HIRED` |
| `--job-status` | the *job's* status group | `ALL`, `DRAFT_AND_OPEN`, `Open`, `Filled`, `Draft`, `Deleted`, `On Hold`, `Canceled` |

**`applications` defaults to `--job-status ALL`.** The API filters to open jobs
unless told otherwise, so a company whose openings are all filled or on hold
gets an empty list from an unfiltered request — which looks like a broken
command rather than a filter. Naming a `--job` is itself a scope, so the default
does not apply then.

**Candidate files.** BambooHR's published spec offers only the `fileUrl` on each
entry of an application's `attachments` list — but that list comes back `null`
on applications that do carry a resume, so the documented route cannot reach
one. `download` therefore uses `/applications/{id}/files/{fileId}`, which is
**not in the spec**; it was verified against live applications returning the
expected PDF and PNG content. Being undocumented it could change without
notice; `recruiting files` prints any `fileUrl` the API does supply as a
fallback.

Most of these responses carry no filename, so names are built as
`application-<id>-resume.pdf`, with the extension inferred from the content
type. When the API does supply a name it is used, still prefixed with the
application ID — two candidates both uploading `Resume.pdf` would otherwise
collide. Downloads are written `0600` and an existing file is never replaced
unless you named it with `--out`.

`set-status` and `comment` are visible to colleagues in BambooHR and may trigger
the account's candidate notifications. Creating candidates and job openings is
not wired up; use the web UI or the API directly.

### Reports

```sh
bamboohr report list
bamboohr report get 42
bamboohr report get 42 --all -o csv > headcount.csv
```

The report's own columns become the output columns, in the order the report
shows them, including any renaming done in BambooHR.

### Metadata

```sh
bamboohr meta company           # also the quickest credentials check
bamboohr meta fields            # --fields selectors
bamboohr meta tables --fields   # employee table names and their columns
bamboohr meta lists --options   # list fields and their allowed values
bamboohr meta users --status enabled
```

### Files

```sh
bamboohr files list
bamboohr files get 789 --file handbook.pdf
bamboohr files get 789 --file -         # stream to stdout
```

With no `--file`, the name from the response's `Content-Disposition` is used.
Downloads are written owner-only (`0600`) — they are usually personal data — and
are staged in a temporary file first, so a failed transfer leaves nothing
behind.

### Exit codes

| Code | Meaning |
| ---- | ------- |
| 0 | success |
| 1 | usage or local error |
| 3 | 401 — the API key was not accepted |
| 4 | 403 — the key's user lacks permission |
| 5 | 404 — no such record |
| 6 | 429 or 503 — throttled |
| 7 | any other API error |
| 130 | interrupted |

## Library

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

func main() {
	client, err := bamboohr.New("acme", os.Getenv("BAMBOOHR_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	emp, err := client.GetEmployee(ctx, "123", bamboohr.GetEmployeeOptions{
		Fields: []string{"firstName", "lastName", "workEmail"},
	})
	if err != nil {
		if bamboohr.IsNotFound(err) {
			log.Fatal("no such employee")
		}
		log.Fatal(err)
	}
	fmt.Println(emp.FirstName, emp.LastName, emp.WorkEmail)

	// Custom fields have no struct field; read them through Fields.
	fmt.Println(emp.Field("customField4340"))

	out, err := client.WhosOut(ctx, bamboohr.WhosOutOptions{
		Start:          "2026-10-01",
		End:            "2026-10-31",
		IncludePersons: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range out.Data {
		person := out.Persons[fmt.Sprint(entry.EmployeeID)]
		fmt.Printf("%s: %s to %s\n", person.DisplayName, entry.Start, entry.End)
	}
}
```

Notes:

- **Client options.** `WithTimeout`, `WithHTTPClient`, `WithUserAgent`,
  `WithMaxRetries` and `WithBaseURL`.
- **Errors.** Failures are `*bamboohr.APIError`, carrying the status code and
  the `X-BambooHR-Error-Message` header BambooHR uses to explain 4xx responses.
  Classify with `IsNotFound`, `IsUnauthorized`, `IsForbidden`, `IsRateLimited`
  or `StatusCode`.
- **Retries.** 429, 503 and 5xx are retried with exponential backoff, honouring
  `Retry-After`. 4xx responses are not retried.
- **Pagination.** `ListEmployees` is cursor-based; the time off and report
  endpoints are page-numbered. Each has a `ListAll…` / `GetAll…` counterpart
  that walks every page — pass a context with a deadline.
- **Filters.** `bamboohr.NewFilter()` builds the OData expressions the newer
  list endpoints take, quoting and escaping values correctly.

## Coverage

The client covers the endpoints a platform or HR integration usually needs:

| Area | Endpoints |
| ---- | --------- |
| Employees | get, list (filtered, cursor-paged), create, update, directory, changed-since, tables |
| Time off | who's out, list/get/create requests, approve, deny, cancel, types, policies, balances |
| Reports | list saved reports, run a saved report |
| Metadata | fields, tables, list fields, users, company |
| Holidays | company holidays, the global catalogue |
| Recruiting | job openings, applications, statuses, status changes, comments, hiring leads, locations |
| Files | list and download employee and company files; employee photos |

The published BambooHR spec has around 390 operations in total. Areas not
covered here include time tracking, compensation planning, goals, benefits,
onboarding, training and webhooks.

## Development

```sh
make help     # list targets
make build    # compile into bin/
make test     # go test -race ./...
make cover    # coverage summary
make lint     # gofmt -s, go vet, golangci-lint
make ci       # lint + test + build
make release  # cross-compile into dist/
```

The version is injected at build time:

```sh
make build VERSION=1.2.3
./bin/bamboohr --version
```

Tests run against `net/http/httptest` servers and make no network calls. The
hidden `--base-url` flag (and `BAMBOOHR_BASE_URL`) is what points the CLI at a
test server or a proxy.
