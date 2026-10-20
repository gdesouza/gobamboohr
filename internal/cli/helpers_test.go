package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseKeyValues(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr string
	}{
		{name: "empty", input: nil, want: nil},
		{
			name:  "simple pairs",
			input: []string{"status=active", "departmentName=Robotics"},
			want:  map[string]string{"status": "active", "departmentName": "Robotics"},
		},
		{
			// Only the first "=" separates, so a value may contain one.
			name:  "value contains equals",
			input: []string{"filter=a=b"},
			want:  map[string]string{"filter": "a=b"},
		},
		{
			name:  "empty value is allowed",
			input: []string{"note="},
			want:  map[string]string{"note": ""},
		},
		{name: "no separator", input: []string{"status"}, wantErr: "not name=value"},
		{name: "no name", input: []string{"=active"}, wantErr: "not name=value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKeyValues(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseKeyValues(%v) succeeded, want an error", tt.input)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseKeyValues: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("[%s] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil", input: nil, want: nil},
		{name: "comma separated", input: []string{"a,b,c"}, want: []string{"a", "b", "c"}},
		{name: "repeated flag", input: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "mixed with spaces", input: []string{" a , b ", "c"}, want: []string{"a", "b", "c"}},
		{name: "blanks dropped", input: []string{"a,,b", ""}, want: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitList(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitList(%v) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMergeFieldInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	if err := os.WriteFile(path, []byte(`{"firstName":"Ada","lastName":"Byron"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("set only", func(t *testing.T) {
		got, err := mergeFieldInput("", []string{"firstName=Ada"})
		if err != nil {
			t.Fatalf("mergeFieldInput: %v", err)
		}
		if got["firstName"] != "Ada" {
			t.Errorf("firstName = %v, want Ada", got["firstName"])
		}
	})

	t.Run("file only", func(t *testing.T) {
		got, err := mergeFieldInput("@"+path, nil)
		if err != nil {
			t.Fatalf("mergeFieldInput: %v", err)
		}
		if got["lastName"] != "Byron" {
			t.Errorf("lastName = %v, want Byron", got["lastName"])
		}
	})

	t.Run("set overrides the file", func(t *testing.T) {
		got, err := mergeFieldInput("@"+path, []string{"lastName=Lovelace"})
		if err != nil {
			t.Fatalf("mergeFieldInput: %v", err)
		}
		if got["lastName"] != "Lovelace" {
			t.Errorf("lastName = %v, want the --set value to win", got["lastName"])
		}
		if got["firstName"] != "Ada" {
			t.Errorf("firstName = %v, want the file value kept", got["firstName"])
		}
	})

	t.Run("inline json", func(t *testing.T) {
		got, err := mergeFieldInput(`{"workPhone":"555-0100"}`, nil)
		if err != nil {
			t.Fatalf("mergeFieldInput: %v", err)
		}
		if got["workPhone"] != "555-0100" {
			t.Errorf("workPhone = %v, want 555-0100", got["workPhone"])
		}
	})

	t.Run("no input is an error", func(t *testing.T) {
		if _, err := mergeFieldInput("", nil); err == nil {
			t.Fatal("mergeFieldInput with no input succeeded, want an error")
		}
	})

	t.Run("bad json is an error", func(t *testing.T) {
		if _, err := mergeFieldInput(`["not","an","object"]`, nil); err == nil {
			t.Fatal("mergeFieldInput with a JSON array succeeded, want an error")
		}
	})
}

func TestParseSince(t *testing.T) {
	t.Run("absolute forms", func(t *testing.T) {
		tests := []struct {
			input string
			want  time.Time
		}{
			{input: "2026-09-01T10:30:00Z", want: time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)},
			{input: "2026-09-01", want: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		}
		for _, tt := range tests {
			got, err := parseSince(tt.input)
			if err != nil {
				t.Fatalf("parseSince(%q): %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseSince(%q) = %v, want %v", tt.input, got, tt.want)
			}
		}
	})

	t.Run("relative forms", func(t *testing.T) {
		tests := []struct {
			input string
			want  time.Duration
		}{
			{input: "24h", want: 24 * time.Hour},
			{input: "30m", want: 30 * time.Minute},
			{input: "7d", want: 7 * 24 * time.Hour},
			{input: "2w", want: 14 * 24 * time.Hour},
		}
		for _, tt := range tests {
			got, err := parseSince(tt.input)
			if err != nil {
				t.Fatalf("parseSince(%q): %v", tt.input, err)
			}
			age := time.Since(got)
			// Allow for the clock moving between the call and the assertion.
			if age < tt.want-time.Minute || age > tt.want+time.Minute {
				t.Errorf("parseSince(%q) is %v ago, want about %v", tt.input, age, tt.want)
			}
		}
	})

	t.Run("rejected", func(t *testing.T) {
		for _, input := range []string{"", "  ", "yesterday", "7 days"} {
			if _, err := parseSince(input); err == nil {
				t.Errorf("parseSince(%q) succeeded, want an error", input)
			}
		}
	})
}

func TestParseID(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{input: "42", want: 42},
		{input: " 42 ", want: 42},
		{input: "0", wantErr: true},
		{input: "-1", wantErr: true},
		{input: "abc", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseID(tt.input, "request id")
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseID(%q) = %d, want an error", tt.input, got)
			} else if !strings.Contains(err.Error(), "request id") {
				t.Errorf("parseID(%q) error = %q, want it to name the argument", tt.input, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseID(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseID(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 512, want: "512 B"},
		{bytes: 2048, want: "2.0 KiB"},
		{bytes: 51200, want: "50.0 KiB"},
		{bytes: 5 * 1024 * 1024, want: "5.0 MiB"},
		{bytes: 3 * 1024 * 1024 * 1024, want: "3.0 GiB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.bytes); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestExtensionFor(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{contentType: "image/jpeg", want: ".jpg"},
		{contentType: "image/png", want: ".png"},
		{contentType: "application/pdf", want: ".pdf"},
		{contentType: "image/JPEG; charset=binary", want: ".jpg"},
		{contentType: "application/octet-stream", want: ""},
		{contentType: "", want: ""},
	}
	for _, tt := range tests {
		if got := extensionFor(tt.contentType); got != tt.want {
			t.Errorf("extensionFor(%q) = %q, want %q", tt.contentType, got, tt.want)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]int{"c": 3, "a": 1, "b": 2})
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys = %v, want %v", got, want)
		}
	}
}
