// Package cli implements the bamboohr command line interface over
// pkg/bamboohr.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// app carries the state every command needs: the resolved flags and a client
// built lazily, so that --help and the version never require credentials.
type app struct {
	subdomain  string
	apiKey     string
	apiKeyFile string
	outputFlag string
	timeout    time.Duration
	maxRetries int
	verbose    bool
	baseURL    string

	format Format
	out    io.Writer
	errOut io.Writer

	client *bamboohr.Client
}

// printer returns a printer for the resolved output format.
func (a *app) printer() *printer {
	return &printer{out: a.out, errOut: a.errOut, format: a.format}
}

// api returns the API client, creating it on first use.
func (a *app) api() (*bamboohr.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	creds, err := resolveCredentials(a.subdomain, a.apiKey, a.apiKeyFile)
	if err != nil {
		return nil, err
	}
	opts := []bamboohr.Option{
		bamboohr.WithTimeout(a.timeout),
		bamboohr.WithMaxRetries(a.maxRetries),
		bamboohr.WithUserAgent(userAgent()),
	}
	// An override is needed to point the CLI at a recording proxy or a test
	// server; it is not part of normal use.
	if base := firstNonEmpty(a.baseURL, os.Getenv(envBaseURL)); base != "" {
		opts = append(opts, bamboohr.WithBaseURL(base))
	}
	client, err := bamboohr.New(creds.subdomain, creds.apiKey, opts...)
	if err != nil {
		return nil, err
	}
	a.client = client
	return client, nil
}

// ctx returns a context bounded by the --timeout flag, so a walk over many
// pages cannot run forever.
func (a *app) ctx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	if a.timeout <= 0 {
		return context.WithCancel(parent)
	}
	// Each request already has the HTTP timeout; give a paginating command room
	// for several of them.
	return context.WithTimeout(parent, a.timeout*10)
}

// logf writes progress to stderr when --verbose is set, keeping stdout clean
// for the result.
func (a *app) logf(format string, args ...any) {
	if a.verbose {
		fmt.Fprintf(a.errOut, format+"\n", args...)
	}
}

// version is set by NewRootCommand from the build-time value in main.
var version = "dev"

func userAgent() string {
	return bamboohr.DefaultUserAgent + "-cli/" + version
}

// NewRootCommand builds the bamboohr command tree. buildVersion is injected at
// build time via -ldflags.
func NewRootCommand(buildVersion string, out, errOut io.Writer) *cobra.Command {
	if buildVersion != "" {
		version = buildVersion
	}
	a := &app{out: out, errOut: errOut}

	root := &cobra.Command{
		Use:   "bamboohr",
		Short: "Query and update BambooHR from the command line",
		Long: `bamboohr is a command line client for the BambooHR API.

Credentials are resolved in this order, most specific first:

  1. --subdomain and --api-key (or --api-key-file)
  2. the BAMBOOHR_SUBDOMAIN, BAMBOOHR_API_KEY and BAMBOOHR_API_KEY_FILE
     environment variables
  3. a JSON config file, by default ~/.config/bamboohr/config.json:

       {"subdomain": "acme", "apiKey": "..."}

The subdomain is the company label in the BambooHR host: for
https://acme.bamboohr.com it is "acme". Generate an API key from
"API Keys" in your BambooHR account settings.

Passing the key as a flag puts it in your shell history and in the process
list; prefer --api-key-file or the environment.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Version:           buildVersion,
		PersistentPreRunE: a.setup,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetVersionTemplate("bamboohr {{.Version}}\n")

	pf := root.PersistentFlags()
	pf.StringVar(&a.subdomain, "subdomain", "", "company subdomain, the label in <subdomain>.bamboohr.com")
	pf.StringVar(&a.apiKey, "api-key", "", "API key (prefer --api-key-file or "+envAPIKey+")")
	pf.StringVar(&a.apiKeyFile, "api-key-file", "", "file containing the API key")
	pf.StringVarP(&a.outputFlag, "output", "o", string(FormatTable), "output format: table, json or csv")
	pf.DurationVar(&a.timeout, "timeout", 60*time.Second, "per-request timeout")
	pf.IntVar(&a.maxRetries, "retries", 3, "retries for throttled and 5xx responses")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "log progress to stderr")
	pf.StringVar(&a.baseURL, "base-url", "", "override the API base URL")
	_ = pf.MarkHidden("base-url")

	root.AddCommand(
		newEmployeeCommand(a),
		newTimeOffCommand(a),
		newHolidayCommand(a),
		newRecruitingCommand(a),
		newReportCommand(a),
		newMetaCommand(a),
		newFilesCommand(a),
	)
	return root
}

// setup validates the global flags before any command runs.
func (a *app) setup(cmd *cobra.Command, _ []string) error {
	format, err := parseFormat(a.outputFlag)
	if err != nil {
		return err
	}
	a.format = format
	return nil
}

// Execute runs the CLI and returns the process exit code.
func Execute(buildVersion string, args []string, out, errOut io.Writer) int {
	root := NewRootCommand(buildVersion, out, errOut)
	root.SetArgs(args)

	ctx, stop := signalContext()
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		return reportError(err, errOut)
	}
	return 0
}

// reportError prints err and maps it to an exit code. API failures get codes of
// their own so a script can tell a permissions problem from a bad request
// without parsing the message.
func reportError(err error, errOut io.Writer) int {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(errOut, "canceled")
		return 130
	}
	fmt.Fprintln(errOut, "error:", err)

	var apiErr *bamboohr.APIError
	if errors.As(err, &apiErr) {
		switch {
		case bamboohr.IsUnauthorized(err):
			fmt.Fprintln(errOut, "hint: check the API key; 401 means BambooHR did not accept it")
			return 3
		case bamboohr.IsForbidden(err):
			fmt.Fprintln(errOut, "hint: the key's user lacks permission for this data")
			return 4
		case bamboohr.IsNotFound(err):
			return 5
		case bamboohr.IsRateLimited(err):
			fmt.Fprintln(errOut, "hint: BambooHR is throttling; retry later or raise --retries")
			return 6
		}
		return 7
	}
	return 1
}

// groupRunE backs a command group, which has subcommands but no action of its
// own. Cobra would print help and exit zero for an unrecognised subcommand,
// which hides a typo in a script, so an unexpected argument is an error here.
func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q for %q\nRun \"%s --help\" for the available commands",
			args[0], cmd.CommandPath(), cmd.CommandPath())
	}
	return cmd.Help()
}
