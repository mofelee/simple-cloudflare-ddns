package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdaterUpdatesChangedRecord(t *testing.T) {
	var patched bool
	var checkedDomainAsZone bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ip":
			fmt.Fprint(response, "fl=1f2\nh=cloudflare.com\nip=203.0.113.10\nts=1700000000\n")
		case "/client/v4/zones":
			if request.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			}
			switch request.URL.Query().Get("name") {
			case "home.example.com":
				checkedDomainAsZone = true
				writeCloudflareResponse(t, response, []cloudflareZone{})
			case "example.com":
				writeCloudflareResponse(t, response, []cloudflareZone{{ID: "zone-id", Name: "example.com"}})
			default:
				t.Errorf("unexpected zone query name = %q", request.URL.Query().Get("name"))
				writeCloudflareResponse(t, response, []cloudflareZone{})
			}
		case "/client/v4/zones/zone-id/dns_records":
			if request.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", request.Method)
			}
			if request.URL.Query().Get("type") != "A" || request.URL.Query().Get("name") != "home.example.com" {
				t.Errorf("record query = %s", request.URL.RawQuery)
			}
			writeCloudflareResponse(t, response, []dnsRecord{{
				ID: "record-id", Type: "A", Name: "home.example.com", Content: "198.51.100.4",
			}})
		case "/client/v4/zones/zone-id/dns_records/record-id":
			if request.Method != http.MethodPatch {
				t.Errorf("method = %s, want PATCH", request.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			if len(body) != 1 || body["content"] != "203.0.113.10" {
				t.Errorf("patch body = %#v", body)
			}
			patched = true
			writeCloudflareResponse(t, response, map[string]any{})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	config := Config{
		APIToken: "test-token",
		Domain:   "home.example.com",
		IPURL:    server.URL + "/ip",
		TTL:      1,
	}
	client := newCloudflareClient(server.Client(), config)
	client.baseURL = server.URL + "/client/v4"
	ddns := newUpdater(server.Client(), client, config)

	result, err := ddns.sync(context.Background())
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}
	if result.Action != "updated" || result.IP != "203.0.113.10" {
		t.Errorf("sync() = %#v, want updated result", result)
	}
	if !patched {
		t.Error("Cloudflare record was not patched")
	}
	if !checkedDomainAsZone {
		t.Error("automatic discovery did not check the full domain first")
	}
}

func TestUpdaterCreatesMissingRecord(t *testing.T) {
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ip" {
			if request.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			}
		}

		switch request.URL.Path {
		case "/ip":
			fmt.Fprint(response, "192.0.2.20")
		case "/client/v4/zones":
			writeCloudflareResponse(t, response, []cloudflareZone{{ID: "zone-id", Name: "example.com"}})
		case "/client/v4/zones/zone-id/dns_records":
			switch request.Method {
			case http.MethodGet:
				writeCloudflareResponse(t, response, []dnsRecord{})
			case http.MethodPost:
				var body struct {
					Type    string `json:"type"`
					Name    string `json:"name"`
					Content string `json:"content"`
					TTL     int    `json:"ttl"`
					Proxied bool   `json:"proxied"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				if body.Type != "A" || body.Name != "home.example.com" || body.Content != "192.0.2.20" || body.TTL != 300 || !body.Proxied {
					t.Errorf("create body = %#v", body)
				}
				created = true
				writeCloudflareResponse(t, response, map[string]any{})
			default:
				t.Errorf("method = %s, want GET or POST", request.Method)
			}
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	config := Config{
		APIToken: "test-token",
		Zone:     "example.com", Domain: "home.example.com",
		IPURL: server.URL + "/ip", TTL: 300, Proxied: true,
	}
	client := newCloudflareClient(server.Client(), config)
	client.baseURL = server.URL + "/client/v4"
	ddns := newUpdater(server.Client(), client, config)

	result, err := ddns.sync(context.Background())
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}
	if result.Action != "created" || !created {
		t.Errorf("sync() = %#v, created = %t", result, created)
	}
}

func TestUpdaterRejectsInvalidPublicIPBeforeCloudflareCall(t *testing.T) {
	var cloudflareCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ip" {
			fmt.Fprint(response, "ip=2001:db8::1\n")
			return
		}
		cloudflareCalled = true
	}))
	defer server.Close()

	config := Config{APIToken: "token", Zone: "example.com", Domain: "home.example.com", IPURL: server.URL + "/ip"}
	client := newCloudflareClient(server.Client(), config)
	client.baseURL = server.URL + "/client/v4"
	ddns := newUpdater(server.Client(), client, config)

	_, err := ddns.sync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not contain a valid IPv4") {
		t.Fatalf("sync() error = %v, want invalid IPv4 error", err)
	}
	if cloudflareCalled {
		t.Error("Cloudflare API was called for an invalid public IP")
	}
}

func TestCloudflareAPIErrorIncludesDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		fmt.Fprint(response, `{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}],"result":null}`)
	}))
	defer server.Close()

	client := newCloudflareClient(server.Client(), Config{APIToken: "bad-token"})
	client.baseURL = server.URL
	_, err := client.findZone(context.Background(), "example.com")
	if err == nil || !strings.Contains(err.Error(), "9109: Invalid access token") {
		t.Fatalf("findZone() error = %v", err)
	}
}

func writeCloudflareResponse(t *testing.T, response http.ResponseWriter, result any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(map[string]any{
		"success": true,
		"errors":  []any{},
		"result":  result,
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
