package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type syncResult struct {
	Action string
	IP     string
}

type updater struct {
	httpClient *http.Client
	cloudflare *cloudflareClient
	config     Config
	zoneID     string
}

func newUpdater(httpClient *http.Client, cloudflare *cloudflareClient, config Config) *updater {
	return &updater{
		httpClient: httpClient,
		cloudflare: cloudflare,
		config:     config,
	}
}

func newIPv4HTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func (u *updater) sync(ctx context.Context) (syncResult, error) {
	ip, err := u.publicIPv4(ctx)
	if err != nil {
		return syncResult{}, err
	}

	if u.zoneID == "" {
		if u.config.Zone != "" {
			u.zoneID, err = u.cloudflare.findZone(ctx, u.config.Zone)
		} else {
			u.zoneID, err = u.cloudflare.discoverZone(ctx, u.config.Domain)
		}
		if err != nil {
			return syncResult{}, err
		}
	}

	record, err := u.cloudflare.findARecord(ctx, u.zoneID, u.config.Domain)
	if err != nil {
		return syncResult{}, err
	}
	if record == nil {
		if err := u.cloudflare.createARecord(ctx, u.zoneID, u.config.Domain, ip, u.config.TTL, u.config.Proxied); err != nil {
			return syncResult{}, err
		}
		return syncResult{Action: "created", IP: ip}, nil
	}
	if record.Content == ip {
		return syncResult{Action: "unchanged", IP: ip}, nil
	}
	if err := u.cloudflare.updateARecord(ctx, u.zoneID, record.ID, ip); err != nil {
		return syncResult{}, err
	}
	return syncResult{Action: "updated", IP: ip}, nil
}

func (u *updater) publicIPv4(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.config.IPURL, nil)
	if err != nil {
		return "", fmt.Errorf("get public IP: create request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "simple-cloudflare-ddns/1.0")

	response, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get public IP: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("get public IP: server returned HTTP %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("get public IP: read response: %w", err)
	}
	value := traceIP(string(data))
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("get public IP: response does not contain a valid IPv4 address")
	}
	return ip.To4().String(), nil
}

func traceIP(response string) string {
	response = strings.TrimSpace(response)
	for _, line := range strings.Split(response, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && key == "ip" {
			return strings.TrimSpace(value)
		}
	}
	// Keep custom endpoints that return only an address usable via ip_url.
	return response
}
