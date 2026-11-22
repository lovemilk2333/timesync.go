package timesync

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptrace"
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
				Format:      ENDPOINT_FORMAT_ISO,
			}},
		})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, CONFIG_FILE), config_data, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir() error = %v", err)
		}

		config, _ := load_config()
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

		config, _ := load_config()
		if config == nil {
			t.Fatal("load_config() returned nil")
		}
		if len(config.Endpoints) == 0 {
			t.Fatal("embedded default config has no endpoints")
		}
		if !config.TLSVerify || config.TLSTimeVerify {
			t.Errorf("embedded TLS defaults = tls-verify:%v tls-time-verify:%v, want true and false", config.TLSVerify, config.TLSTimeVerify)
		}
	})

	t.Run("retains TLS defaults for omitted fields and accepts explicit false", func(t *testing.T) {
		dir := t.TempDir()
		config_data := `{"endpoints":[{"url":"https://override.example/time","type":"text","precision":"1ms","format":"iso","method":"get"}]}`
		if err := os.WriteFile(filepath.Join(dir, CONFIG_FILE), []byte(config_data), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir() error = %v", err)
		}

		config, loaded := load_config()
		if !loaded {
			t.Fatal("load_config() did not load config file")
		}
		if !config.TLSVerify || config.TLSTimeVerify {
			t.Errorf("TLS defaults = tls-verify:%v tls-time-verify:%v, want true and false", config.TLSVerify, config.TLSTimeVerify)
		}

		if err := os.WriteFile(filepath.Join(dir, CONFIG_FILE), []byte(`{"tls-verify":false,"endpoints":[{"url":"https://override.example/time","type":"text","precision":"1ms","format":"iso","method":"get"}]}`), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		config, loaded = load_config()
		if !loaded {
			t.Fatal("load_config() did not load config file with explicit false")
		}
		if config.TLSVerify || config.TLSTimeVerify {
			t.Errorf("TLS settings = tls-verify:%v tls-time-verify:%v, want false and default false", config.TLSVerify, config.TLSTimeVerify)
		}
	})
}

func TestNewHTTPClientTLSVerificationOptions(t *testing.T) {
	config := &Config{Timeout: Duration{Duration: time.Second}, TLSVerify: true}
	client := new_http_client(config)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatal("new_http_client() did not configure an HTTP transport")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.VerifyConnection == nil {
		t.Error("TLS verification should validate the chain while allowing out-of-time certificates by default")
	}

	config.TLSTimeVerify = true
	client = new_http_client(config)
	transport = client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.VerifyConnection != nil {
		t.Error("tls-time-verify should use Go's normal chain and time verification")
	}

	config.TLSVerify = false
	client = new_http_client(config)
	transport = client.Transport.(*http.Transport)
	if !transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.VerifyConnection != nil {
		t.Error("tls-verify=false should disable certificate verification")
	}
}

func TestVerifyTLSChainWithoutTime(t *testing.T) {
	now := time.Now()
	root_key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root_template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	root_der, err := x509.CreateCertificate(rand.Reader, root_template, root_template, &root_key.PublicKey, root_key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(root_der)
	if err != nil {
		t.Fatal(err)
	}
	leaf_key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf_template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		DNSNames:     []string{"time.example"},
		NotBefore:    now.Add(-2 * time.Hour),
		NotAfter:     now.Add(-time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leaf_der, err := x509.CreateCertificate(rand.Reader, leaf_template, root, &leaf_key.PublicKey, root_key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leaf_der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, ServerName: "time.example"}
	if err := verify_tls_chain_without_time_with_roots(state, roots); err != nil {
		t.Fatalf("valid chain with expired leaf should pass when time checks are disabled: %v", err)
	}
	if err := verify_tls_chain_without_time(state); err == nil {
		t.Fatal("verification should reject a chain signed by an untrusted root")
	}
	leaf.NotBefore = now.Add(time.Hour)
	leaf.NotAfter = now.Add(2 * time.Hour)
	if err := verify_tls_chain_without_time_with_roots(state, roots); err != nil {
		t.Fatalf("valid chain with a not-yet-valid leaf should pass when time checks are disabled: %v", err)
	}
	state.ServerName = "wrong.example"
	if err := verify_tls_chain_without_time_with_roots(state, roots); err == nil {
		t.Fatal("verification should reject a hostname mismatch")
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
		trace := httptrace.ContextClientTrace(r.Context())
		time.Sleep(20 * time.Millisecond) // DNS/TCP setup is before ConnectDone.
		if trace != nil && trace.ConnectDone != nil {
			trace.ConnectDone("tcp", "time.example:443", nil)
		}
		time.Sleep(20 * time.Millisecond)
		if trace != nil && trace.GotFirstResponseByte != nil {
			trace.GotFirstResponseByte()
		}
		return response(http.StatusOK, "1740000000000"), nil
	})
	client := &http.Client{Transport: transport, Timeout: time.Second}
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "timesync-test", MaxRespSize: 64, Retry: 1}
	ep := new_test_endpoint(t, "https://time.example/test", "text", ENDPOINT_FORMAT_TS, time.Millisecond)

	got, _, err := fetch_endpoint_time(client, config, ep)
	if err != nil {
		t.Fatalf("fetch_endpoint_time() error = %v", err)
	}
	want := time.Unix(1740000000, 0)
	if delta := got.Sub(want); delta < 5*time.Millisecond || delta > 100*time.Millisecond {
		t.Errorf("fetched time = %v, want about half the 20ms connection-to-first-byte delay after %v (delta %v)", got, want, delta)
	}
}

func TestFetchEndpointTimeRejectsOversizedBody(t *testing.T) {
	client := &http.Client{Transport: round_trip_func(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "1740000000000"), nil
	})}
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "test", MaxRespSize: 4, Retry: 1}
	ep := new_test_endpoint(t, "https://time.example/test", "text", ENDPOINT_FORMAT_TS, time.Millisecond)
	if _, _, err := fetch_endpoint_time(client, config, ep); err == nil {
		t.Fatal("fetch_endpoint_time() expected oversized response error")
	}
}

func TestSyncTimeStopsAfterFirstSuccessfulEndpoint(t *testing.T) {
	first_successful_time := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	config := &Config{Timeout: Duration{Duration: time.Second}, UserAgent: "test", MaxRespSize: 128, Retry: 1}
	config.Endpoints = []*Endpoint{
		new_test_endpoint(t, "https://unavailable.example/time", "text", ENDPOINT_FORMAT_ISO, 0),
		new_test_endpoint(t, "https://first-success.example/time", "text", ENDPOINT_FORMAT_ISO, 0),
		new_test_endpoint(t, "https://must-not-run.example/time", "text", ENDPOINT_FORMAT_ISO, 0),
	}
	var calls []string
	client := &http.Client{Transport: round_trip_func(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Host)
		switch r.URL.Host {
		case "unavailable.example":
			return response(http.StatusServiceUnavailable, ""), nil
		case "first-success.example":
			return response(http.StatusOK, first_successful_time.Format(time.RFC3339Nano)), nil
		default:
			t.Errorf("unexpected endpoint request: %s", r.URL)
			return nil, fmt.Errorf("unexpected endpoint request: %s", r.URL)
		}
	})}

	var got time.Time
	if err := sync_time_with_client(config, client, func(t *time.Time) error {
		got = *t
		return nil
	}); err != nil {
		t.Fatalf("sync_time_with_client() error = %v", err)
	}
	if delta := got.Sub(first_successful_time); delta < 0 || delta > 100*time.Millisecond {
		t.Errorf("selected time = %v, want near first successful endpoint time %v (delta %v)", got, first_successful_time, delta)
	}
	want_calls := []string{"unavailable.example", "first-success.example"}
	if fmt.Sprint(calls) != fmt.Sprint(want_calls) {
		t.Errorf("endpoint calls = %v, want %v", calls, want_calls)
	}
}
