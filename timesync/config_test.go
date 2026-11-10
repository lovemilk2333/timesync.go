package timesync

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"
)

// valid_precision satisfies the `validate:"required"` constraint on Endpoint.Precision.
var valid_precision = Duration{Duration: time.Second}

// build_json_body constructs a nested JSON object following path and assigning
// value at the leaf. It is used to exercise the JSON path getter.
func build_json_body(path []string, value any) []byte {
	var v any = value
	for i := len(path) - 1; i >= 0; i-- {
		v = map[string]any{path[i]: v}
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestEndpointParse(t *testing.T) {
	sv := NewValidator()

	const iso_value = "2023-11-14T22:13:20Z"
	iso_time, err := time.Parse(time.RFC3339, iso_value)
	if err != nil {
		t.Fatalf("failed to parse fixture ISO time: %v", err)
	}

	tests := []struct {
		name              string
		endpoint          *Endpoint
		want_err          bool
		want_err_contains string
		want_type         EndpointType
		// want_path_segs is the expected JSON path segments. When non-empty, the
		// test builds a JSON body and verifies the path getter resolves it.
		want_path_segs []string
		// want_body / want_time are used for non-JSON getters (e.g. text).
		want_body []byte
		want_time *time.Time
	}{
		{
			name: "text type with valid URL and precision",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text",
				Precision:   valid_precision,
			},
			want_type: EndpointTypeText,
		},
		{
			name: "text type with ISO format and value",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text",
				Precision:   valid_precision,
				Format:      EndpointFormatISO,
			},
			want_type: EndpointTypeText,
			want_body: []byte(iso_value),
			want_time: &iso_time,
		},
		{
			name: "json type with single-segment path",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:timestamp",
				Precision:   valid_precision,
			},
			want_type:      EndpointTypeJSON,
			want_path_segs: []string{"timestamp"},
		},
		{
			name: "json type with multi-segment path",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:data.timestamp",
				Precision:   valid_precision,
			},
			want_type:      EndpointTypeJSON,
			want_path_segs: []string{"data", "timestamp"},
		},
		{
			name: "json type with escaped dots in path",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:data..with..dots.timestamp",
				Precision:   valid_precision,
			},
			want_type:      EndpointTypeJSON,
			want_path_segs: []string{"data.with.dots", "timestamp"},
		},
		{
			name: "spaces around type name are trimmed",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json :data.timestamp",
				Precision:   valid_precision,
			},
			want_type:      EndpointTypeJSON,
			want_path_segs: []string{"data", "timestamp"},
		},
		{
			name: "json type with ISO format",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:time",
				Precision:   valid_precision,
				Format:      EndpointFormatISO,
			},
			want_type:      EndpointTypeJSON,
			want_path_segs: []string{"time"},
		},
		{
			name: "regex type with valid pattern",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        `regex:[0-9]+`,
				Precision:   valid_precision,
			},
			want_type: EndpointTypeRegex,
		},
		{
			name: "text type must not carry a path",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text:foo",
				Precision:   valid_precision,
			},
			want_err:          true,
			want_err_contains: "path must be empty",
		},
		{
			name: "json type path must not be empty",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:",
				Precision:   valid_precision,
			},
			want_err:          true,
			want_err_contains: "path must not be empty",
		},
		{
			name: "json type path must not start with a dot",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "json:.data.timestamp",
				Precision:   valid_precision,
			},
			want_err:          true,
			want_err_contains: "path must not be empty",
		},
		{
			name: "unknown type should fail",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/data",
				Type:        "xml:foo",
				Precision:   valid_precision,
			},
			want_err:          true,
			want_err_contains: "invalid endpoint type",
		},
		{
			name: "invalid format should fail",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text",
				Precision:   valid_precision,
				Format:      "rfc3339",
			},
			want_err:          true,
			want_err_contains: "invalid endpoint format",
		},
		{
			name: "invalid regex should fail",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        `regex:[`,
				Precision:   valid_precision,
			},
			want_err:          true,
			want_err_contains: "error parsing regexp",
		},
		{
			name: "missing URL should fail",
			endpoint: &Endpoint{
				Type:      "text",
				Precision: valid_precision,
			},
			want_err: true,
		},
		{
			name: "malformed URL should fail",
			endpoint: &Endpoint{
				OriginalURL: "not-a-url",
				Type:        "text",
				Precision:   valid_precision,
			},
			want_err: true,
		},
		{
			name: "non-HTTP(S) URL scheme should fail",
			endpoint: &Endpoint{
				OriginalURL: "ftp://example.com/time",
				Type:        "text",
				Precision:   valid_precision,
			},
			want_err: true,
		},
		{
			name: "missing Type should fail",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Precision:   valid_precision,
			},
			want_err: true,
		},
		{
			name: "missing Precision should fail",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text",
			},
			want_err:          true,
			want_err_contains: "precision",
		},
		{
			name: "ISO format does not require Precision",
			endpoint: &Endpoint{
				OriginalURL: "https://example.com/time",
				Type:        "text",
				Format:      EndpointFormatISO,
			},
			want_type: EndpointTypeText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.endpoint.Parse(sv)

			if tt.want_err {
				if err == nil {
					t.Fatalf("Parse() expected an error, got nil")
				}
				if tt.want_err_contains != "" && !strings.Contains(err.Error(), tt.want_err_contains) {
					t.Fatalf("Parse() error = %q, want it to contain %q", err.Error(), tt.want_err_contains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}

			if tt.endpoint.URL == nil {
				t.Errorf("URL should not be nil after a successful Parse()")
			}

			got_type, got_path, err := tt.endpoint.GetTypePath()
			if err != nil {
				t.Fatalf("GetTypePath() unexpected error: %v", err)
			}
			if got_type != tt.want_type {
				t.Errorf("type = %v, want %v", got_type, tt.want_type)
			}

			var body []byte
			var want_time *time.Time
			if len(tt.want_path_segs) > 0 {
				if tt.endpoint.Format == EndpointFormatISO {
					body = build_json_body(tt.want_path_segs, iso_value)
					wt := iso_time
					want_time = &wt
				} else {
					const seconds = int64(1000)
					body = build_json_body(tt.want_path_segs, float64(seconds))
					wt := time.Unix(0, seconds*int64(time.Second))
					want_time = &wt
				}
			} else if tt.want_body != nil {
				body = tt.want_body
				want_time = tt.want_time
			}

			if body != nil && want_time != nil {
				got_time, err := got_path(tt.endpoint, body)
				if err != nil {
					t.Fatalf("path getter error: %v", err)
				}
				if !got_time.Equal(*want_time) {
					t.Errorf("time = %v, want %v", got_time, *want_time)
				}
			}
		})
	}
}

func TestParseEndpointTypeTimestampValueTypes(t *testing.T) {
	tests := []struct {
		name      string
		type_str  string
		body      []byte
		precision time.Duration
		format    EndpointFormat
		want      *time.Time
		want_err  string
	}{
		{
			name:      "text int64 string with millisecond precision and timestamp format",
			type_str:  "text",
			body:      []byte("1740000000000"),
			precision: time.Millisecond,
			format:    EndpointFormatTS,
			want:      time_ptr(time.Unix(1740000000, 0)),
		},
		{
			name:      "text float64 string with millisecond precision and timestamp format",
			type_str:  "text",
			body:      []byte("1740000000000.25"),
			precision: time.Millisecond,
			format:    EndpointFormatTS,
			want:      time_ptr(time.Unix(0, int64(1740000000000.25*float64(time.Millisecond)))),
		},
		{
			name:      "JSON integer with millisecond precision and timestamp format",
			type_str:  "json",
			body:      []byte(`{"timestamp":1740000000000}`),
			precision: time.Millisecond,
			format:    EndpointFormatTS,
			want:      time_ptr(time.Unix(1740000000, 0)),
		},
		{
			name:      "JSON float with millisecond precision and timestamp format",
			type_str:  "json",
			body:      []byte(`{"timestamp":1740000000000.25}`),
			precision: time.Millisecond,
			format:    EndpointFormatTS,
			want:      time_ptr(time.Unix(0, int64(1740000000000.25*float64(time.Millisecond)))),
		},
		{
			name:      "JSON int64 string with millisecond precision and timestamp format",
			type_str:  "json",
			body:      []byte(`{"timestamp":"1740000000000"}`),
			precision: time.Millisecond,
			format:    EndpointFormatTS,
			want:      time_ptr(time.Unix(1740000000, 0)),
		},
		{
			name:      "text with millisecond precision and ISO format",
			type_str:  "text",
			body:      []byte("2025-02-19T21:20:00Z"),
			precision: time.Millisecond,
			format:    EndpointFormatISO,
			want:      time_ptr(time.Date(2025, time.February, 19, 21, 20, 0, 0, time.UTC)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := &Endpoint{Precision: Duration{Duration: tt.precision}, Format: tt.format}
			if tt.precision == 0 {
				t.Fatal("test precision must be set")
			}
			path := ""
			if tt.type_str == "json" {
				path = "timestamp"
			}
			getter, err := parse_endpoint_type(tt.type_str, path)
			if err != nil {
				t.Fatalf("parse_endpoint_type() error = %v", err)
			}

			got, err := getter(endpoint, tt.body)
			if tt.want_err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want_err) {
					t.Fatalf("getter() error = %v, want error containing %q", err, tt.want_err)
				}
				return
			}
			if err != nil {
				t.Fatalf("getter() unexpected error: %v", err)
			}
			if !got.Equal(*tt.want) {
				t.Errorf("getter() time = %v, want %v", got, *tt.want)
			}
		})
	}
}

func time_ptr(t time.Time) *time.Time { return &t }

func TestParseEndpointTypeRegex(t *testing.T) {
	const body = "server time: 2025-02-19T21:20:00Z\n"
	tests := []struct {
		name      string
		pattern   string
		format    EndpointFormat
		precision time.Duration
		want      time.Time
		want_err  bool
	}{
		{
			name:    "extracts ISO timestamp",
			pattern: `[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z`,
			format:  EndpointFormatISO,
			want:    time.Date(2025, time.February, 19, 21, 20, 0, 0, time.UTC),
		},
		{
			name:      "extracts timestamp",
			pattern:   `[0-9]+`,
			format:    EndpointFormatTS,
			precision: time.Millisecond,
			want:      time.Unix(0, 2025*int64(time.Millisecond)),
		},
		{
			name:     "pattern starting with a dot is valid",
			pattern:  `.+`,
			format:   EndpointFormatISO,
			want_err: true, // matches, but the captured text is not an ISO timestamp
		},
		{
			name:     "empty pattern fails",
			pattern:  "",
			format:   EndpointFormatISO,
			want_err: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := &Endpoint{Precision: Duration{Duration: tt.precision}, Format: tt.format}
			getter, err := parse_endpoint_type(string(EndpointTypeRegex), tt.pattern)
			if tt.want_err && tt.pattern == "" {
				if err == nil {
					t.Fatal("parse_endpoint_type() expected an error for an empty pattern")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse_endpoint_type() error = %v", err)
			}

			got, err := getter(endpoint, []byte(body))
			if tt.want_err {
				if err == nil {
					t.Fatal("getter() expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("getter() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("getter() time = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTimeWithFormatTrimsWhitespace(t *testing.T) {
	iso, err := parse_time_with_format(" 2025-02-19T21:20:00Z\n", 0, EndpointFormatISO)
	if err != nil {
		t.Fatalf("parse_time_with_format() ISO error = %v", err)
	}
	if want := time.Date(2025, time.February, 19, 21, 20, 0, 0, time.UTC); !iso.Equal(want) {
		t.Errorf("ISO time = %v, want %v", iso, want)
	}

	ts, err := parse_time_with_format(" 2025 \n", time.Millisecond, EndpointFormatTS)
	if err != nil {
		t.Fatalf("parse_time_with_format() timestamp error = %v", err)
	}
	if want := time.Unix(0, 2025*int64(time.Millisecond)); !ts.Equal(want) {
		t.Errorf("timestamp = %v, want %v", ts, want)
	}
}

func TestParseTypeWithColonInPath(t *testing.T) {
	getter, err := parse_endpoint_type(string(EndpointTypeJSON), "time:utc")
	if err != nil {
		t.Fatalf("parse_endpoint_type() error = %v", err)
	}
	endpoint := &Endpoint{Precision: valid_precision, Format: EndpointFormatTS}
	body := build_json_body([]string{"time:utc"}, float64(1000))
	got, err := getter(endpoint, body)
	if err != nil {
		t.Fatalf("getter() error = %v", err)
	}
	if want := time.Unix(0, 1000*int64(time.Second)); !got.Equal(want) {
		t.Errorf("time = %v, want %v", got, want)
	}

	ep := &Endpoint{
		OriginalURL: "https://example.com/data",
		Type:        "json:time:utc",
		Precision:   valid_precision,
	}
	if err := ep.Parse(NewValidator()); err != nil {
		t.Fatalf("Endpoint.Parse() error = %v", err)
	}
	if ended, _, err := ep.GetTypePath(); err != nil || ended != EndpointTypeJSON {
		t.Errorf("GetTypePath() = %v, %v; want json, nil", ended, err)
	}
}

func TestParseJSONPathRejectsEmptySegments(t *testing.T) {
	for _, p := range []string{"", ".a", "a.", "a..b."} {
		if _, err := parse_json_path(p); err == nil {
			t.Errorf("parse_json_path(%q) expected an error", p)
		}
	}
}

func TestConfigParseDefaultsTimeoutAndUA(t *testing.T) {
	tests := []struct {
		name         string
		timeout      Duration
		ua           string
		want_timeout time.Duration
		want_ua      string
	}{
		{
			name:         "unspecified timeout",
			want_timeout: DefaultTimeout,
			want_ua:      DefaultUserAgent,
		},
		{
			name:         "explicit timeout",
			timeout:      Duration{Duration: 3 * time.Second},
			ua:           "Ciallo/1.1",
			want_timeout: 3 * time.Second,
			want_ua:      "Ciallo/1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				UserAgent: tt.ua,
				Timeout:   tt.timeout,
				Endpoints: []*Endpoint{{
					OriginalURL: "https://example.com/time",
					Type:        "text",
					Precision:   valid_precision,
				}},
			}

			if err := config.Parse(NewValidator()); err != nil {
				t.Fatalf("Config.Parse() error = %v", err)
			}
			if got := config.Timeout.Duration; got != tt.want_timeout {
				t.Errorf("Timeout.Duration = %s, want %s", got, tt.want_timeout)
			}
			if got := config.UserAgent; got != tt.want_ua {
				t.Errorf("UserAgent = %s, want %s", got, tt.want_ua)
			}
		})
	}
}

// TestEndpointGetTypePathCached verifies that repeated calls to
// GetTypePath return consistent results (i.e. the cache works).
func TestEndpointGetTypePathCached(t *testing.T) {
	sv := NewValidator()
	ep := &Endpoint{
		OriginalURL: "https://example.com/data",
		Type:        "json:a.b",
		Precision:   valid_precision,
	}

	if err := ep.Parse(sv); err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	typ1, path1, err := ep.GetTypePath()
	if err != nil {
		t.Fatalf("GetTypePath() first call error: %v", err)
	}

	typ2, path2, err := ep.GetTypePath()
	if err != nil {
		t.Fatalf("GetTypePath() second call error: %v", err)
	}

	if typ1 != typ2 {
		t.Errorf("type mismatch after caching: %v vs %v", typ1, typ2)
	}

	// Function values are not comparable; verify behavior instead.
	body := build_json_body([]string{"a", "b"}, float64(1000))
	got1, err := path1(ep, body)
	if err != nil {
		t.Fatalf("path1 getter error: %v", err)
	}
	got2, err := path2(ep, body)
	if err != nil {
		t.Fatalf("path2 getter error: %v", err)
	}
	if !got1.Equal(*got2) {
		t.Errorf("path getters returned different times: %v vs %v", got1, got2)
	}

	if !ep._type_cached {
		t.Errorf("_type_cached should be true after Parse()")
	}
}
