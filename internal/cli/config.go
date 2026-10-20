package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Environment variables the CLI reads credentials from.
const (
	envSubdomain  = "BAMBOOHR_SUBDOMAIN"
	envAPIKey     = "BAMBOOHR_API_KEY"
	envAPIKeyFile = "BAMBOOHR_API_KEY_FILE"
	envConfig     = "BAMBOOHR_CONFIG"
	envBaseURL    = "BAMBOOHR_BASE_URL"
)

// config is the on-disk credential file, read from
// $XDG_CONFIG_HOME/bamboohr/config.json (or ~/.config/bamboohr/config.json).
type config struct {
	Subdomain string `json:"subdomain"`
	APIKey    string `json:"apiKey"`
	// APIKeyFile points at a separate file holding just the key, for setups
	// that keep secrets out of a JSON blob.
	APIKeyFile string `json:"apiKeyFile"`
}

// configPath returns the config file location, honouring BAMBOOHR_CONFIG and
// XDG_CONFIG_HOME. The second result is false when there is no discoverable
// config directory, which is not a failure: flags and the environment can
// supply everything the CLI needs.
func configPath() (string, bool) {
	if p := os.Getenv(envConfig); p != "" {
		return p, true
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "bamboohr", "config.json"), true
}

// loadConfig reads the config file. A missing file is not an error: the CLI is
// usable from flags and the environment alone.
func loadConfig() (config, error) {
	var cfg config
	path, ok := configPath()
	if !ok {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

// credentials is the resolved subdomain and API key.
type credentials struct {
	subdomain string
	apiKey    string
}

// resolveCredentials merges the three sources, most specific first: command
// line flags, then environment variables, then the config file.
func resolveCredentials(flagSubdomain, flagAPIKey, flagAPIKeyFile string) (credentials, error) {
	cfg, err := loadConfig()
	if err != nil {
		return credentials{}, err
	}

	creds := credentials{
		subdomain: firstNonEmpty(flagSubdomain, os.Getenv(envSubdomain), cfg.Subdomain),
		apiKey:    firstNonEmpty(flagAPIKey, os.Getenv(envAPIKey)),
	}

	// A key held in a file beats one in the config JSON, but never an explicit
	// flag or environment variable.
	if creds.apiKey == "" {
		keyFile := firstNonEmpty(flagAPIKeyFile, os.Getenv(envAPIKeyFile), cfg.APIKeyFile)
		if keyFile != "" {
			key, err := readKeyFile(keyFile)
			if err != nil {
				return credentials{}, err
			}
			creds.apiKey = key
		}
	}
	if creds.apiKey == "" {
		creds.apiKey = cfg.APIKey
	}

	if creds.subdomain == "" {
		return credentials{}, fmt.Errorf(
			"no company subdomain: pass --subdomain, set %s, or add \"subdomain\" to the config file", envSubdomain)
	}
	if creds.apiKey == "" {
		return credentials{}, fmt.Errorf(
			"no API key: pass --api-key or --api-key-file, set %s, or add \"apiKey\" to the config file", envAPIKey)
	}
	return creds, nil
}

// readKeyFile reads an API key from a file, trimming the trailing newline an
// editor or `echo` leaves behind.
func readKeyFile(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading API key file: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("API key file %s is empty", path)
	}
	return key, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
