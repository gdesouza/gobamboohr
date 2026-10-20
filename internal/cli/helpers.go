package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdesouza/gobamboohr/pkg/bamboohr"
)

// parseKeyValues parses repeated name=value flags into a map. The value may
// contain "=", so only the first one separates.
func parseKeyValues(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		name, value, found := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, fmt.Errorf("%q is not name=value", pair)
		}
		out[name] = value
	}
	return out, nil
}

// splitList flattens comma-separated values, so --fields a,b and
// --fields a --fields b behave the same.
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// mergeFieldInput combines a JSON --data document with repeated --set flags.
// The --set values win, which lets a caller override one field of a template.
func mergeFieldInput(data string, sets []string) (map[string]any, error) {
	fields := map[string]any{}

	if data != "" {
		raw, err := readInput(data)
		if err != nil {
			return nil, fmt.Errorf("--data: %w", err)
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("--data: not a JSON object: %w", err)
		}
	}

	pairs, err := parseKeyValues(sets)
	if err != nil {
		return nil, fmt.Errorf("--set: %w", err)
	}
	for k, v := range pairs {
		fields[k] = v
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields given: use --set name=value or --data")
	}
	return fields, nil
}

// readInput reads a flag value that may be inline text, @file, or - for stdin.
func readInput(value string) ([]byte, error) {
	switch {
	case value == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(value, "@"):
		return os.ReadFile(strings.TrimPrefix(value, "@"))
	default:
		return []byte(value), nil
	}
}

// parseSince accepts an RFC 3339 timestamp, a plain date, or a relative age
// such as 24h or 7d, and returns the absolute time it names.
func parseSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("a timestamp or relative age is required")
	}

	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts, nil
		}
	}

	// A relative age reads better than a timestamp for a routine poll.
	if d, err := parseDuration(value); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("%q is not an RFC 3339 timestamp, a date, or an age such as 24h or 7d", value)
}

// parseDuration extends time.ParseDuration with days and weeks, which is how
// people describe a polling window.
func parseDuration(value string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(value, "d"); ok {
		days, err := strconv.ParseFloat(n, 64)
		if err == nil {
			return time.Duration(days * 24 * float64(time.Hour)), nil
		}
	}
	if n, ok := strings.CutSuffix(value, "w"); ok {
		weeks, err := strconv.ParseFloat(n, 64)
		if err == nil {
			return time.Duration(weeks * 7 * 24 * float64(time.Hour)), nil
		}
	}
	return time.ParseDuration(value)
}

// parseID parses a positive integer identifier, naming the argument in the
// error so the message points at the right one.
func parseID(value, name string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, value)
	}
	return id, nil
}

// sortedKeys returns a map's keys in order, so repeated runs print the same
// rows in the same places.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fileTable renders file categories as one row per file.
func fileTable(categories []bamboohr.FileCategory) *Table {
	t := &Table{Headers: []string{"id", "name", "category", "size", "created", "createdBy", "shared"}}
	for _, cat := range categories {
		for _, f := range cat.Files {
			name := f.Name
			if name == "" {
				name = f.OriginalFileName
			}
			t.AddRow(f.ID, name, cat.Name, humanSize(f.Size.Int64()), f.DateCreated, f.CreatedBy, f.Shared())
		}
	}
	return t
}

// humanSize renders a byte count in the largest unit that keeps it short.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value)
}

// downloadFileMode is the mode a downloaded file is created with. Employee
// files hold personal data, so they are owner-only rather than the usual 0644.
const downloadFileMode = 0o600

// downloadFunc streams a binary response into w.
type downloadFunc func(w io.Writer) (*bamboohr.Download, error)

// downloadTarget says where a download should be written.
type downloadTarget struct {
	// Path, when set, is the exact file to write, or "-" for stdout. It is the
	// user naming the file, so it is used as given and may overwrite.
	Path string
	// Dir is the directory a generated name is placed in.
	Dir string
	// Prefix is prepended to a generated name, including one the server
	// suggested. It keeps files belonging to different records apart: two
	// candidates both uploading "Resume.pdf" would otherwise collide.
	Prefix string
	// Fallback is the base name to use when the response suggests none. An
	// extension is added from the content type if it has none.
	Fallback string
}

// saveDownload runs a download and writes it where the target says.
//
// It writes to a temporary file first so that a failed transfer does not leave
// a truncated file behind, and refuses to replace an existing file unless the
// caller named that exact path.
func (a *app) saveDownload(target downloadTarget, fn downloadFunc) error {
	if target.Path == "-" {
		_, err := fn(a.out)
		return err
	}

	// The temporary file has to sit on the same filesystem as the target for
	// the rename to be atomic, so it goes next to wherever the target lands.
	tmpDir := target.Dir
	if target.Path != "" {
		tmpDir = filepath.Dir(target.Path)
	}
	if tmpDir == "" {
		tmpDir = "."
	}

	tmp, err := os.CreateTemp(tmpDir, ".bamboohr-download-*")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	meta, err := fn(tmp)
	if err != nil {
		return err
	}
	// os.CreateTemp makes the file owner-only. Set it explicitly rather than
	// relying on that: employee and candidate documents are the kind of thing
	// that should not land in a shared directory world-readable.
	if err := tmp.Chmod(downloadFileMode); err != nil {
		return fmt.Errorf("setting permissions on the download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing temporary file: %w", err)
	}

	path := target.Path
	if path == "" {
		path = filepath.Join(target.Dir, target.Prefix+downloadName(meta, target.Fallback))
		// A generated name is a guess, so replacing an existing file would be
		// destroying something the user did not point at.
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --out to choose a name or move the existing file", path)
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("saving to %s: %w", path, err)
	}

	fmt.Fprintf(a.errOut, "wrote %s (%s)\n", path, humanSize(meta.Size))
	return nil
}

// downloadName picks a filename for a response that was not given an explicit
// one: the name the server suggested, else the caller's fallback with an
// extension worked out from the content type.
func downloadName(meta *bamboohr.Download, fallbackName string) string {
	if meta.Filename != "" {
		return meta.Filename
	}
	// An attachment's own name already carries its extension; adding a second
	// one would produce resume.pdf.pdf.
	if filepath.Ext(fallbackName) != "" {
		return fallbackName
	}
	return fallbackName + extensionFor(meta.ContentType)
}

// extensionFor guesses a file extension from a media type, for responses that
// do not suggest a filename.
//
// The applicant tracking downloads send no Content-Disposition at all, so this
// is the only thing naming a candidate's resume correctly.
func extensionFor(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	switch strings.TrimSpace(strings.ToLower(mediaType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/rtf", "text/rtf":
		return ".rtf"
	case "text/plain":
		return ".txt"
	case "application/zip":
		return ".zip"
	default:
		return ""
	}
}
