package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInterval = "5m"
	defaultIPURL    = "https://cloudflare.com/cdn-cgi/trace"
)

type Config struct {
	APIToken string `json:"api_token"`
	Zone     string `json:"zone"`
	Domain   string `json:"domain"`
	Interval string `json:"interval"`
	IPURL    string `json:"ip_url"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
}

func loadConfig(path string) (Config, time.Duration, error) {
	config := Config{
		Interval: defaultInterval,
		IPURL:    defaultIPURL,
		TTL:      1,
	}

	file, err := os.Open(path)
	if err == nil {
		defer file.Close()

		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return Config{}, 0, fmt.Errorf("decode config: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return Config{}, 0, fmt.Errorf("decode config: unexpected data after JSON object")
		}
	} else if !os.IsNotExist(err) {
		return Config{}, 0, fmt.Errorf("open config: %w", err)
	}

	if err := applyEnvironment(&config); err != nil {
		return Config{}, 0, err
	}

	interval, err := config.validate()
	if err != nil {
		return Config{}, 0, err
	}
	return config, interval, nil
}

func applyEnvironment(config *Config) error {
	stringValues := []struct {
		name   string
		target *string
	}{
		{name: "CLOUDFLARE_API_TOKEN", target: &config.APIToken},
		{name: "DDNS_ZONE", target: &config.Zone},
		{name: "DDNS_DOMAIN", target: &config.Domain},
		{name: "DDNS_INTERVAL", target: &config.Interval},
		{name: "DDNS_IP_URL", target: &config.IPURL},
	}
	for _, item := range stringValues {
		if value, found := os.LookupEnv(item.name); found {
			*item.target = value
		}
	}

	if value, found := os.LookupEnv("DDNS_TTL"); found {
		ttl, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("config: DDNS_TTL must be an integer: %w", err)
		}
		config.TTL = ttl
	}
	if value, found := os.LookupEnv("DDNS_PROXIED"); found {
		proxied, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("config: DDNS_PROXIED must be true or false: %w", err)
		}
		config.Proxied = proxied
	}
	return nil
}

func (c *Config) validate() (time.Duration, error) {
	c.APIToken = strings.TrimSpace(c.APIToken)
	c.Zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(c.Zone), "."))
	c.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(c.Domain), "."))
	c.IPURL = strings.TrimSpace(c.IPURL)

	switch {
	case c.APIToken == "":
		return 0, fmt.Errorf("config: api_token is required")
	case c.Domain == "":
		return 0, fmt.Errorf("config: domain is required")
	case c.Zone != "" && c.Domain != c.Zone && !strings.HasSuffix(c.Domain, "."+c.Zone):
		return 0, fmt.Errorf("config: domain %q is not within zone %q", c.Domain, c.Zone)
	case c.TTL != 1 && (c.TTL < 60 || c.TTL > 86400):
		return 0, fmt.Errorf("config: ttl must be 1 (automatic) or between 60 and 86400")
	}

	interval, err := time.ParseDuration(c.Interval)
	if err != nil || interval <= 0 {
		return 0, fmt.Errorf("config: interval must be a positive duration such as 5m")
	}

	ipURL, err := url.Parse(c.IPURL)
	if err != nil || (ipURL.Scheme != "http" && ipURL.Scheme != "https") || ipURL.Host == "" {
		return 0, fmt.Errorf("config: ip_url must be a valid HTTP or HTTPS URL")
	}

	return interval, nil
}
