package timesync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigUsesWorkingDirectoryAndFallsBackWhenMissing(t *testing.T) {
	original_dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original_dir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	t.Run("uses config in current working directory", func(t *testing.T) {
		dir := t.TempDir()
		config_data, err := json.Marshal(Config{
			LogLevel: "DEBUG",
			Endpoints: []*Endpoint{{
				OriginalURL: "https://override.example/time",
				Type:        "text",
				Format:      EndpointFormatISO,
			}},
		})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, ConfigFile), config_data, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir() error = %v", err)
		}

		config := load_config()
		if config == nil {
			t.Fatal("load_config() returned nil")
		}
		if config.LogLevel != "DEBUG" || len(config.Endpoints) != 1 || config.Endpoints[0].URL.Host != "override.example" {
			t.Errorf("load_config() did not load current directory config: %+v", config)
		}
	})

	t.Run("falls back to embedded defaults when config is absent", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir() error = %v", err)
		}

		config := load_config()
		if config == nil {
			t.Fatal("load_config() returned nil")
		}
		if len(config.Endpoints) == 0 {
			t.Fatal("embedded default config has no endpoints")
		}
	})
}

func TestNewHTTPClientTLSVerifyDefaultsToFalse(t *testing.T) {
	config := &Config{Timeout: Duration{Duration: time.Second}}
	client := new_http_client(config)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatal("new_http_client() did not configure an HTTP transport")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLS verification should be disabled by default")
	}

	config.TLSVerify = true
	client = new_http_client(config)
	transport = client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLS verification should be enabled when tls-verify is true")
	}
}

type round_trip_func func(*http.Request) (*http.Response, error)

func (f round_trip_func) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func new_test_endpoint(t *testing.T, url, endpoint_type string, format EndpointFormat, precision time.Duration) *Endpoint {
	t.Helper()
	ep := &Endpoint{
		OriginalURL: url,
		Type:        endpoint_type,
		Format:      format,
		Precision:   Duration{Duration: precision},
	}
	if err := ep.Parse(v); err != nil {
		t.Fatalf("Endpoint.Parse() error = %v", err)
	}
	return ep
}

func TestFetchEndpointTimeTextTimestamp(t *testing.T) {
	transport := round_trip_func(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("User-Agent"); got != "timesync-test" {
			t.Errorf("User-Agent = %q, want timesync-test", got)
		}
		return response(http.StatusOK, "1740000000000"), nil
	})
	client := &http.Client{Transport: transport, Timeout: time.Second}
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "timesync-test", MaxRespSize: 64, Retry: 0}
	ep := new_test_endpoint(t, "https://time.example/test", "text", EndpointFormatTS, time.Millisecond)

	got, err := fetch_endpoint_time(client, config, ep)
	if err != nil {
		t.Fatalf("fetch_endpoint_time() error = %v", err)
	}
	want := time.Unix(1740000000, 0)
	if delta := got.Sub(want); delta < 0 || delta > 100*time.Millisecond {
		t.Errorf("fetched time = %v, want near %v (delta %v)", got, want, delta)
	}
}

func TestFetchEndpointTimeRejectsOversizedBody(t *testing.T) {
	client := &http.Client{Transport: round_trip_func(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "1740000000000"), nil
	})}
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "test", MaxRespSize: 4, Retry: 0}
	ep := new_test_endpoint(t, "https://time.example/test", "text", EndpointFormatTS, time.Millisecond)
	if _, err := fetch_endpoint_time(client, config, ep); err == nil {
		t.Fatal("fetch_endpoint_time() expected oversized response error")
	}
}

func TestSyncTimeUsesMedianEndpointTime(t *testing.T) {
	server_times := []time.Time{
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 1, 0, 0, 2, 0, time.UTC),
		time.Date(2035, time.January, 1, 0, 0, 0, 0, time.UTC), // outlier
	}
	responses := make(map[string]string, len(server_times))
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "test", MaxRespSize: 128, Retry: 0}
	for i, server_time := range server_times {
		url := fmt.Sprintf("https://time-%d.example/test", i)
		responses[url] = server_time.Format(time.RFC3339Nano)
		config.Endpoints = append(config.Endpoints, new_test_endpoint(t, url, "text", EndpointFormatISO, 0))
	}
	client := &http.Client{Transport: round_trip_func(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusOK, responses[r.URL.String()]), nil
	})}

	var got time.Time
	if err := sync_time_with_client(config, client, func(t *time.Time) error {
		got = *t
		return nil
	}); err != nil {
		t.Fatalf("sync_time_with_client() error = %v", err)
	}
	want := server_times[1]
	if delta := got.Sub(want); delta < 0 || delta > 100*time.Millisecond {
		t.Errorf("selected time = %v, want near median %v (delta %v)", got, want, delta)
	}
}
