package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const cloudflareAPIURL = "https://api.cloudflare.com/client/v4"

type cloudflareClient struct {
	httpClient *http.Client
	baseURL    string
	apiToken   string
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareEnvelope struct {
	Success bool              `json:"success"`
	Errors  []cloudflareError `json:"errors"`
	Result  json.RawMessage   `json:"result"`
}

type cloudflareZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func newCloudflareClient(httpClient *http.Client, config Config) *cloudflareClient {
	return &cloudflareClient{
		httpClient: httpClient,
		baseURL:    cloudflareAPIURL,
		apiToken:   config.APIToken,
	}
}

func (c *cloudflareClient) findZone(ctx context.Context, name string) (string, error) {
	zoneID, found, err := c.lookupZone(ctx, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("find zone: active zone %q was not found", name)
	}
	return zoneID, nil
}

func (c *cloudflareClient) discoverZone(ctx context.Context, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	for index := 0; index < len(labels); index++ {
		name := strings.Join(labels[index:], ".")
		zoneID, found, err := c.lookupZone(ctx, name)
		if err != nil {
			return "", err
		}
		if found {
			return zoneID, nil
		}
	}
	return "", fmt.Errorf("find zone: no active zone contains domain %q", domain)
}

func (c *cloudflareClient) lookupZone(ctx context.Context, name string) (string, bool, error) {
	query := url.Values{
		"name":   {name},
		"status": {"active"},
		"match":  {"all"},
	}
	var zones []cloudflareZone
	if err := c.do(ctx, http.MethodGet, "/zones", query, nil, &zones); err != nil {
		return "", false, fmt.Errorf("find zone %q: %w", name, err)
	}
	if len(zones) == 0 {
		return "", false, nil
	}
	if len(zones) > 1 {
		return "", false, fmt.Errorf("find zone: multiple active zones matched %q", name)
	}
	return zones[0].ID, true, nil
}

func (c *cloudflareClient) findARecord(ctx context.Context, zoneID, name string) (*dnsRecord, error) {
	query := url.Values{
		"type":  {"A"},
		"name":  {name},
		"match": {"all"},
	}
	var records []dnsRecord
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	if err := c.do(ctx, http.MethodGet, path, query, nil, &records); err != nil {
		return nil, fmt.Errorf("find DNS record: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	if len(records) > 1 {
		return nil, fmt.Errorf("find DNS record: multiple A records matched %q", name)
	}
	return &records[0], nil
}

func (c *cloudflareClient) createARecord(ctx context.Context, zoneID, name, ip string, ttl int, proxied bool) error {
	body := struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Proxied bool   `json:"proxied"`
	}{
		Type: "A", Content: ip, Name: name, TTL: ttl, Proxied: proxied,
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	if err := c.do(ctx, http.MethodPost, path, nil, body, nil); err != nil {
		return fmt.Errorf("create DNS record: %w", err)
	}
	return nil
}

func (c *cloudflareClient) updateARecord(ctx context.Context, zoneID, recordID, ip string) error {
	body := struct {
		Content string `json:"content"`
	}{Content: ip}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records/" + url.PathEscape(recordID)
	if err := c.do(ctx, http.MethodPatch, path, nil, body, nil); err != nil {
		return fmt.Errorf("update DNS record: %w", err)
	}
	return nil
}

func (c *cloudflareClient) do(ctx context.Context, method, path string, query url.Values, body, result any) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-cloudflare-ddns/1.0")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var envelope cloudflareEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("Cloudflare returned HTTP %d with an invalid response", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		return fmt.Errorf("Cloudflare returned HTTP %d: %s", response.StatusCode, formatCloudflareErrors(envelope.Errors))
	}
	if result != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode response result: %w", err)
		}
	}
	return nil
}

func formatCloudflareErrors(errors []cloudflareError) string {
	if len(errors) == 0 {
		return "unknown API error"
	}
	parts := make([]string, 0, len(errors))
	for _, apiError := range errors {
		if apiError.Code != 0 {
			parts = append(parts, fmt.Sprintf("%d: %s", apiError.Code, apiError.Message))
		} else {
			parts = append(parts, apiError.Message)
		}
	}
	return strings.Join(parts, "; ")
}
