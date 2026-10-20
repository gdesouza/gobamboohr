package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig points BAMBOOHR_CONFIG at a temporary file holding body.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(envConfig, path)
	return path
}

// clearCredentialEnv removes any real credentials from the test's environment.
func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envSubdomain, envAPIKey, envAPIKeyFile, envConfig, envBaseURL} {
		t.Setenv(key, "")
	}
}

func TestResolveCredentialsPrecedence(t *testing.T) {
	t.Run("flags win over the environment", func(t *testing.T) {
		clearCredentialEnv(t)
		t.Setenv(envSubdomain, "from-env")
		t.Setenv(envAPIKey, "env-key")

		creds, err := resolveCredentials("from-flag", "flag-key", "")
		if err != nil {
			t.Fatalf("resolveCredentials: %v", err)
		}
		if creds.subdomain != "from-flag" || creds.apiKey != "flag-key" {
			t.Errorf("creds = %+v, want the flag values", creds)
		}
	})

	t.Run("environment wins over the config file", func(t *testing.T) {
		clearCredentialEnv(t)
		writeConfig(t, `{"subdomain":"from-file","apiKey":"file-key"}`)
		t.Setenv(envSubdomain, "from-env")
		t.Setenv(envAPIKey, "env-key")

		creds, err := resolveCredentials("", "", "")
		if err != nil {
			t.Fatalf("resolveCredentials: %v", err)
		}
		if creds.subdomain != "from-env" || creds.apiKey != "env-key" {
			t.Errorf("creds = %+v, want the environment values", creds)
		}
	})

	t.Run("config file is the fallback", func(t *testing.T) {
		clearCredentialEnv(t)
		writeConfig(t, `{"subdomain":"from-file","apiKey":"file-key"}`)

		creds, err := resolveCredentials("", "", "")
		if err != nil {
			t.Fatalf("resolveCredentials: %v", err)
		}
		if creds.subdomain != "from-file" || creds.apiKey != "file-key" {
			t.Errorf("creds = %+v, want the config file values", creds)
		}
	})

	t.Run("sources combine", func(t *testing.T) {
		// A common setup: the company in the config file, the key in the
		// environment.
		clearCredentialEnv(t)
		writeConfig(t, `{"subdomain":"acme"}`)
		t.Setenv(envAPIKey, "env-key")

		creds, err := resolveCredentials("", "", "")
		if err != nil {
			t.Fatalf("resolveCredentials: %v", err)
		}
		if creds.subdomain != "acme" || creds.apiKey != "env-key" {
			t.Errorf("creds = %+v, want acme with the environment key", creds)
		}
	})
}

func TestResolveCredentialsFromKeyFile(t *testing.T) {
	clearCredentialEnv(t)
	keyPath := filepath.Join(t.TempDir(), "key")
	// A key written by an editor or `echo` has a trailing newline.
	if err := os.WriteFile(keyPath, []byte("file-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	creds, err := resolveCredentials("acme", "", keyPath)
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.apiKey != "file-key" {
		t.Errorf("apiKey = %q, want the trimmed file contents", creds.apiKey)
	}
}

func TestResolveCredentialsKeyFileErrors(t *testing.T) {
	clearCredentialEnv(t)

	t.Run("missing file", func(t *testing.T) {
		_, err := resolveCredentials("acme", "", filepath.Join(t.TempDir(), "absent"))
		if err == nil {
			t.Fatal("resolveCredentials succeeded with a missing key file")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := resolveCredentials("acme", "", path)
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("error = %v, want it to mention the empty file", err)
		}
	})
}

func TestResolveCredentialsMissing(t *testing.T) {
	clearCredentialEnv(t)
	// Point the config at a path that does not exist, so a real config file on
	// the machine running the tests cannot satisfy the lookup.
	t.Setenv(envConfig, filepath.Join(t.TempDir(), "absent.json"))

	t.Run("no subdomain", func(t *testing.T) {
		_, err := resolveCredentials("", "key", "")
		if err == nil || !strings.Contains(err.Error(), "subdomain") {
			t.Fatalf("error = %v, want it to ask for a subdomain", err)
		}
	})

	t.Run("no api key", func(t *testing.T) {
		_, err := resolveCredentials("acme", "", "")
		if err == nil || !strings.Contains(err.Error(), "API key") {
			t.Fatalf("error = %v, want it to ask for an API key", err)
		}
	})
}

func TestLoadConfigMissingFileIsNotAnError(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv(envConfig, filepath.Join(t.TempDir(), "absent.json"))

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Subdomain != "" || cfg.APIKey != "" {
		t.Errorf("cfg = %+v, want the zero value", cfg)
	}
}

func TestLoadConfigMalformedIsAnError(t *testing.T) {
	clearCredentialEnv(t)
	writeConfig(t, `{not json`)

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig succeeded on malformed JSON, want an error")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		values []string
		want   string
	}{
		{values: []string{"", "b", "c"}, want: "b"},
		{values: []string{"  ", "b"}, want: "b"},
		{values: []string{"a", "b"}, want: "a"},
		{values: []string{"", ""}, want: ""},
		{values: nil, want: ""},
	}
	for _, tt := range tests {
		if got := firstNonEmpty(tt.values...); got != tt.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
		}
	}
}
