package timesync

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap/zapcore"
)

type StructValidator func(any) validator.ValidationErrors

func generate_ua() string {
	return fmt.Sprintf(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36",
		// Chrome 96 - 195
		96+rand.IntN(100),
	)
}

const MaxConfigSize = 65536 * 1024 // 6 MiB
const MaxRespSize = 16 * 1024      // 16 KiB
const MaxSuggestedPrecision = time.Minute
const DefaultTimeout = 5 * time.Second
const DefaultRetry = 2

var DefaultUserAgent = generate_ua()

const EndpointTypeSeparator = ":"
const EndpointTypeSeparatorLength = len(EndpointTypeSeparator)

const EndpointJSONPathSeparator = '.'
const EndpointJSONPathEscape = string(EndpointJSONPathSeparator) + string(EndpointJSONPathSeparator)

type EndpointMethod string

var valid_methods = map[EndpointMethod]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodConnect: true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

func is_valid_method(m EndpointMethod) bool {
	return valid_methods[m]
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
		if typed_val, ok := val.(float64); ok {
			total_nano = int64(typed_val * float64(precision))
		} else if typed_val, ok := val.(string); ok {
			if int_val, err := strconv.ParseInt(typed_val, 10, 64); err == nil {
				total_nano = int64(int_val) * int64(precision)
			} else if float_val, err := strconv.ParseFloat(typed_val, 64); err == nil {
				total_nano = int64(float_val * float64(precision))
			} else {
				return nil, fmt.Errorf("invalid value type (invalid `string` of `int64`/`float64 based 10: `%q`)", val)
			}
		} else {
			return nil, fmt.Errorf("invalid value type (expected `float64` or `string`, got `%T`)", val)
		}

		t := time.Unix(0, total_nano)
		return &t, nil
	case EndpointFormatISO:
		typed_val, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value type (expected `string`, got `%T`)", val)
		}
		t, err := time.Parse(time.RFC3339, typed_val)
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
		if len(p) == 0 || p[0] == EndpointJSONPathSeparator {
			return nil, fmt.Errorf("invalid endpoint type `%q`: path must not be empty", EndpointTypeText)
		}

		index := 0
		last_index := 0
		target_index := len(p)
		path := make([]string, 0)

		for index < target_index {
			if p[index] == EndpointJSONPathSeparator {
				if index+1 < target_index && p[index+1] == EndpointJSONPathSeparator {
					index += 2
					continue
				}

				segment := p[last_index:index]
				segment = strings.ReplaceAll(segment, EndpointJSONPathEscape, string(EndpointJSONPathSeparator))
				path = append(path, segment)
				last_index = index + 1
			}
			index++
		}

		if last_index <= len(p) {
			segment := p[last_index:]
			segment = strings.ReplaceAll(segment, EndpointJSONPathEscape, string(EndpointJSONPathSeparator))
			path = append(path, segment)
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
		if len(p) == 0 || p[0] == EndpointJSONPathSeparator {
			return nil, fmt.Errorf("invalid endpoint type `%q`: path must not be empty", EndpointTypeText)
		}

		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}

		return func(this *Endpoint, body []byte) (*time.Time, error) {
			return parse_time_with_format(re.Find(body), this.Precision.Duration, this.Format)
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

	sep_index := strings.LastIndex(this.Type, EndpointTypeSeparator)
	if sep_index == -1 {
		endpoint_type = this.Type
	} else {
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
	// Resolve the default before conditional validation: an omitted format
	// defaults to timestamps, which require precision.
	if err := this.init_format(); err != nil {
		return err
	}

	if err := validator(this); err != nil {
		return err
	}

	if err := this.init(); err != nil {
		return err
	}

	return nil
}

var log_levels = map[string]zapcore.Level{
	"DEBUG":  zapcore.DebugLevel,
	"INFO":   zapcore.InfoLevel,
	"WARN":   zapcore.WarnLevel,
	"ERROR":  zapcore.ErrorLevel,
	"DPANIC": zapcore.DPanicLevel,
	"PANIC":  zapcore.PanicLevel,
	"FATAL":  zapcore.FatalLevel,
}

type Config struct {
	LogLevel    string      `json:"log-level"`
	UserAgent   string      `json:"user-agent"`
	MaxRespSize uint        `json:"max-resp-size"`
	Timeout     Duration    `json:"timeout"`
	Retry       uint        `json:"retry"`
	TLSVerify   bool        `json:"tls-verify"`
	Endpoints   []*Endpoint `json:"endpoints"`

	_cached_log_level zapcore.Level
}

func (this *Config) GetLogLevel() (zapcore.Level, bool) {
	l, ok := log_levels[strings.ToUpper(this.LogLevel)]
	return l, ok
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
		if endpoint == nil {
			return fmt.Errorf("endpoint %d is null", i)
		}
		if err := endpoint.Parse(validator); err != nil {
			return fmt.Errorf("endpoint %d: %w", i, err)
		}
	}

	return nil
}
