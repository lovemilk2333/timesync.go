package timesync

import (
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

type StructValidator func(any) validator.ValidationErrors

func generate_ua() string {
	return fmt.Sprintf(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36",
		// Chrome 96 - 195
		96+rand.IntN(100),
	)
}

const MaxConfigSize = 6 * 1024 // 6 MiB
const MaxRespSize = 16 * 1024  // 16 KiB
const MaxSuggestedPrecision = time.Minute
const DefaultTimeout = 5 * time.Second
const DefaultRetry = 2

var DefaultUserAgent = generate_ua()

const EndpointTypeSeparator = ":"
const EndpointTypeSeparatorLength = len(EndpointTypeSeparator)

const EndpointJSONPathSeparator = '.'
const EndpointJSONPathEscape = string(EndpointJSONPathSeparator) + string(EndpointJSONPathSeparator)

type EndpointMethod string

func is_valid_method(m EndpointMethod) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
		http.MethodTrace:
		return true
	default:
		return false
	}
}

type EndpointType string
type EndpointPathGetter func(this *Endpoint, body []byte) (*time.Time, error)

const (
	EndpointTypeText  EndpointType = "text"
	EndpointTypeJSON  EndpointType = "json"
	EndpointTypeRegex EndpointType = "regex"
)

type EndpointFormat string

const (
	EndpointFormatTS  EndpointFormat = "ts"
	EndpointFormatISO EndpointFormat = "iso"
)

func parse_time_with_format(val any, precision time.Duration, format EndpointFormat) (*time.Time, error) {
	switch format {
	case EndpointFormatTS:
		var total_nano int64
		switch typed_val := val.(type) {
		case float64:
			total_nano = int64(typed_val * float64(precision))
		case string:
			typed_val = strings.TrimSpace(typed_val)
			if int_val, err := strconv.ParseInt(typed_val, 10, 64); err == nil {
				total_nano = int_val * int64(precision)
			} else if float_val, err := strconv.ParseFloat(typed_val, 64); err == nil {
				total_nano = int64(float_val * float64(precision))
			} else {
				return nil, fmt.Errorf("invalid value type (invalid `string` of `int64`/`float64 based 10: `%q`)", val)
			}
		default:
			return nil, fmt.Errorf("invalid value type (expected `float64` or `string`, got `%T`)", val)
		}

		t := time.Unix(0, total_nano)
		return &t, nil
	case EndpointFormatISO:
		typed_val, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value type (expected `string`, got `%T`)", val)
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(typed_val))
		if err != nil {
			return nil, err
		}

		return &t, nil
	default:
		panic(fmt.Sprintf("unreachable: invalid time format %q", format))
	}
}

func get_by_path(m map[string]any, path []string) (any, bool) {
	var current any = m
	for _, key := range path {
		curr, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}

		var found bool
		current, found = curr[key]
		if !found {
			return nil, false
		}
	}
	return current, true
}

// parse_json_path splits a dotted JSON path into segments. A doubled dot (..)
// encodes a literal dot inside a segment, so `a..b.c` resolves to `a.b` -> `c`.
// Empty segments are rejected instead of silently producing unreachable keys.
func parse_json_path(p string) ([]string, error) {
	path := make([]string, 0)
	segment := make([]byte, 0, len(p))

	for i := 0; i < len(p); i++ {
		if p[i] != EndpointJSONPathSeparator {
			segment = append(segment, p[i])
			continue
		}
		if i+1 < len(p) && p[i+1] == EndpointJSONPathSeparator {
			segment = append(segment, EndpointJSONPathSeparator)
			i++
			continue
		}
		if len(segment) == 0 {
			return nil, fmt.Errorf("invalid endpoint type `%q`: path must not be empty or contain an empty segment", EndpointTypeJSON)
		}
		path = append(path, string(segment))
		segment = segment[:0]
	}

	if len(segment) == 0 {
		return nil, fmt.Errorf("invalid endpoint type `%q`: path must not be empty or contain an empty segment", EndpointTypeJSON)
	}
	path = append(path, string(segment))

	return path, nil
}

func parse_endpoint_type(t string, p string) (EndpointPathGetter, error) {
	switch t {
	case string(EndpointTypeText):
		if len(p) > 0 {
			return nil, fmt.Errorf("invalid endpoint type `%q`: path must be empty", EndpointTypeText)
		}
		return func(this *Endpoint, body []byte) (*time.Time, error) {
			return parse_time_with_format(string(body), this.Precision.Duration, this.Format)
		}, nil
	case string(EndpointTypeJSON):
		path, err := parse_json_path(p)
		if err != nil {
			return nil, err
		}

		return func(this *Endpoint, body []byte) (*time.Time, error) {
			var data map[string]any
			err := json.Unmarshal(body, &data)
			if err != nil {
				return nil, err
			}

			if val, ok := get_by_path(data, path); !ok {
				return nil, fmt.Errorf("no such key or path: `%v`", path)
			} else {
				return parse_time_with_format(val, this.Precision.Duration, this.Format)
			}
		}, nil
	case string(EndpointTypeRegex):
		if len(p) == 0 {
			return nil, fmt.Errorf("invalid endpoint type `%q`: pattern must not be empty", EndpointTypeRegex)
		}

		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}

		return func(this *Endpoint, body []byte) (*time.Time, error) {
			match := re.Find(body)
			if match == nil {
				return nil, fmt.Errorf("regex pattern `%q` did not match the response body", p)
			}
			return parse_time_with_format(string(match), this.Precision.Duration, this.Format)
		}, nil
	default:
		return nil, fmt.Errorf("invalid endpoint type `%q`", t)
	}
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	duration, err := time.ParseDuration(s)
	if err != nil {
		return err
	}

	d.Duration = duration
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

type Endpoint struct {
	OriginalURL string         `json:"url" validate:"required,http_url"`
	Method      EndpointMethod `json:"method"`
	URL         *url.URL       `json:"-"`
	Type        string         `json:"type" validate:"required"`
	Precision   Duration       `json:"precision" validate:"required_if=Format ts"`
	Format      EndpointFormat `json:"format"`

	_type_cached bool
	_cache_type  EndpointType
	_cache_path  EndpointPathGetter
}

func (this *Endpoint) GetTypePath() (EndpointType, EndpointPathGetter, error) {
	if this._type_cached {
		return this._cache_type, this._cache_path, nil
	}

	var endpoint_type string
	var endpoint_path string = ""

	sep_index := strings.Index(this.Type, EndpointTypeSeparator)
	if sep_index == -1 {
		endpoint_type = this.Type
	} else {
		// Split at the first separator so paths/patterns may contain colons.
		endpoint_type, endpoint_path = strings.ReplaceAll(this.Type[:sep_index], " ", ""), this.Type[sep_index+EndpointTypeSeparatorLength:]
	}

	pared_path, err := parse_endpoint_type(endpoint_type, endpoint_path)
	if err != nil {
		return "", nil, err
	}

	this._cache_type = EndpointType(endpoint_type)
	this._cache_path = pared_path
	this._type_cached = true
	return this._cache_type, this._cache_path, nil
}

func (this *Endpoint) init_method() error {
	if len(this.Method) == 0 {
		this.Method = http.MethodGet // default method
	}

	m := EndpointMethod(strings.ToUpper(strings.TrimSpace(string(this.Method))))

	if !is_valid_method(m) {
		return fmt.Errorf("invalid endpoint method `%q` (%q)", this.Method, m)
	}

	this.Method = m
	return nil
}

func (this *Endpoint) init_format() error {
	if len(this.Format) == 0 {
		this.Format = EndpointFormatTS // default format
	}

	switch this.Format {
	case EndpointFormatTS:
		if this.Precision.Duration == 0 {
			return fmt.Errorf("invalid endpoint format `%q`: precision cannot be `0`", this.Format)
		}
	case EndpointFormatISO:
	default:
		return fmt.Errorf("invalid endpoint format `%q`", this.Format)
	}

	return nil
}

func (this *Endpoint) init() error {
	if this == nil {
		return fmt.Errorf("nil endpoint found")
	}

	if parsed_url, err := url.Parse(this.OriginalURL); err != nil {
		return err
	} else {
		this.URL = parsed_url
	}

	if err := this.init_format(); err != nil {
		return err
	}

	if err := this.init_method(); err != nil {
		return err
	}

	if _, _, err := this.GetTypePath(); err != nil {
		return err
	}

	return nil
}

func (this *Endpoint) Parse(validator StructValidator) error {
	if err := this.init(); err != nil {
		return err
	}

	if err := validator(this); err != nil {
		return err
	}

	return nil
}

type Config struct {
	LogLevel    string      `json:"log-level"`
	UserAgent   string      `json:"user-agent"`
	MaxRespSize uint        `json:"max-resp-size"`
	Timeout     Duration    `json:"timeout"`
	Retry       uint        `json:"retry"`
	TLSVerify   bool        `json:"tls-verify"`
	Endpoints   []*Endpoint `json:"endpoints"`
}

func (this *Config) GetLogLevel() (slog.Level, bool) {
	switch strings.ToUpper(this.LogLevel) {
	case "DEBUG":
		return slog.LevelDebug, true
	case "INFO":
		return slog.LevelInfo, true
	case "WARN":
		return slog.LevelWarn, true
	case "ERROR":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

func (this *Config) init() error {
	if len(this.LogLevel) == 0 {
		this.LogLevel = "INFO"
	}
	if _, ok := this.GetLogLevel(); !ok {
		return fmt.Errorf("no such log level `%s`", this.LogLevel)
	}

	if len(this.UserAgent) == 0 {
		this.UserAgent = DefaultUserAgent
	}

	if this.Timeout.Duration == 0 {
		this.Timeout.Duration = DefaultTimeout
	}
	if this.MaxRespSize == 0 {
		this.MaxRespSize = MaxRespSize
	}

	if this.Retry == 0 {
		this.Retry = DefaultRetry
	}

	if len(this.Endpoints) == 0 {
		return fmt.Errorf("must have at least 1 endpoint")
	}

	if this.MaxRespSize > MaxRespSize {
		this.MaxRespSize = MaxRespSize
	}

	return nil
}

func (this *Config) Parse(validator StructValidator) error {
	if err := validator(this); err != nil {
		return err
	}

	if err := this.init(); err != nil {
		return err
	}
	for i, endpoint := range this.Endpoints {
		if err := endpoint.Parse(validator); err != nil {
			return fmt.Errorf("endpoint %d: %w", i, err)
		}
	}

	return nil
}
