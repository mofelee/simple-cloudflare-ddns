package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaultsAndNormalization(t *testing.T) {
	clearConfigEnvironment(t)

	path := writeTestConfig(t, `{
		"api_token": " token ",
		"domain": "Home.Example.COM."
	}`)

	config, interval, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.APIToken != "token" {
		t.Errorf("APIToken = %q, want token", config.APIToken)
	}
	if config.Zone != "" {
		t.Errorf("Zone = %q, want empty for automatic discovery", config.Zone)
	}
	if config.Domain != "home.example.com" {
		t.Errorf("Domain = %q, want home.example.com", config.Domain)
	}
	if interval != 5*time.Minute {
		t.Errorf("interval = %v, want 5m", interval)
	}
	if config.IPURL != defaultIPURL {
		t.Errorf("IPURL = %q, want %q", config.IPURL, defaultIPURL)
	}
	if config.TTL != 1 {
		t.Errorf("TTL = %d, want 1", config.TTL)
	}
}

func TestLoadConfigRejectsInvalidConfigurations(t *testing.T) {
	clearConfigEnvironment(t)

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing authentication",
			content: `{"zone":"example.com","domain":"home.example.com"}`,
			want:    "api_token",
		},
		{
			name:    "legacy API key field",
			content: `{"api_token":"token","api_key":"key","domain":"home.example.com"}`,
			want:    "unknown field",
		},
		{
			name:    "domain outside zone",
			content: `{"api_token":"token","zone":"example.com","domain":"other.test"}`,
			want:    "is not within zone",
		},
		{
			name:    "invalid interval",
			content: `{"api_token":"token","zone":"example.com","domain":"home.example.com","interval":"now"}`,
			want:    "positive duration",
		},
		{
			name:    "unknown field",
			content: `{"api_token":"token","zone":"example.com","domain":"home.example.com","typo":true}`,
			want:    "unknown field",
		},
		{
			name:    "trailing JSON",
			content: `{"api_token":"token","zone":"example.com","domain":"home.example.com"} {}`,
			want:    "unexpected data",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := loadConfig(writeTestConfig(t, test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadConfig() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-token")
	t.Setenv("DDNS_ZONE", "Example.COM")
	t.Setenv("DDNS_DOMAIN", "Home.Example.COM")
	t.Setenv("DDNS_INTERVAL", "60s")
	t.Setenv("DDNS_IP_URL", "https://ip.example.com")
	t.Setenv("DDNS_TTL", "300")
	t.Setenv("DDNS_PROXIED", "true")

	missingPath := filepath.Join(t.TempDir(), "missing.json")
	config, interval, err := loadConfig(missingPath)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.APIToken != "env-token" || config.Zone != "example.com" || config.Domain != "home.example.com" {
		t.Errorf("environment strings were not loaded: %#v", config)
	}
	if interval != time.Minute {
		t.Errorf("interval = %v, want 1m", interval)
	}
	if config.IPURL != "https://ip.example.com" || config.TTL != 300 || !config.Proxied {
		t.Errorf("environment options were not loaded: %#v", config)
	}
}

func TestEnvironmentOverridesJSONConfig(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-token")
	t.Setenv("DDNS_DOMAIN", "env.example.com")
	t.Setenv("DDNS_INTERVAL", "60s")

	path := writeTestConfig(t, `{
		"api_token": "file-token",
		"domain": "file.example.com",
		"interval": "5m"
	}`)
	config, interval, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.APIToken != "env-token" || config.Domain != "env.example.com" || interval != time.Minute {
		t.Errorf("environment did not override JSON: config = %#v, interval = %v", config, interval)
	}
}

func TestLoadConfigRejectsInvalidEnvironmentValue(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	t.Setenv("DDNS_DOMAIN", "home.example.com")
	t.Setenv("DDNS_PROXIED", "sometimes")

	_, _, err := loadConfig("")
	if err == nil || !strings.Contains(err.Error(), "DDNS_PROXIED") {
		t.Fatalf("loadConfig() error = %v, want DDNS_PROXIED error", err)
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CLOUDFLARE_API_TOKEN",
		"DDNS_ZONE",
		"DDNS_DOMAIN",
		"DDNS_INTERVAL",
		"DDNS_IP_URL",
		"DDNS_TTL",
		"DDNS_PROXIED",
	} {
		value, found := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
		t.Cleanup(func() {
			var err error
			if found {
				err = os.Setenv(name, value)
			} else {
				err = os.Unsetenv(name)
			}
			if err != nil {
				t.Errorf("restore %s: %v", name, err)
			}
		})
	}
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
