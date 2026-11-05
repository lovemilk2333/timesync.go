package timesync

import (
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"go.uber.org/zap"
)

//go:embed embedded.json
var default_config_data []byte

var v = NewValidator()

const ConfigFile = "config.json"

func load_config() *Config {
	var default_config Config
	if err := json.Unmarshal(default_config_data, &default_config); err != nil {
		logger.DPanic("failed to unmarshal default config json", zap.Error(err))
		return nil
	}
	if err := default_config.Parse(v); err != nil {
		logger.DPanic("failed to check default config json", zap.Error(err))
		return nil
	}

	file, err := os.Open(ConfigFile)
	if err != nil {
		logger.Info("using embedded default config", zap.String("file", ConfigFile))
		return &default_config
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxConfigSize+1))
	if err != nil {
		logger.Error("failed to read config file", zap.Error(err))
		return &default_config
	}
	if int64(len(data)) > MaxConfigSize {
		logger.Error("config file size exceeds limit", zap.Int64("max_size", MaxConfigSize))
		return &default_config
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		logger.Error("failed to unmarshal config json", zap.Error(err))
		return &default_config
	}
	if err := config.Parse(v); err != nil {
		logger.Error("failed to check config json", zap.Error(err))
		return &default_config
	}
	return &config
}

// fetch_endpoint_time queries one endpoint and compensates for request latency
// by using the request midpoint as the local observation time.
type http_doer interface {
	Do(*http.Request) (*http.Response, error)
}

func fetch_endpoint_time(client http_doer, config *Config, endpoint *Endpoint) (time.Time, error) {
	_, get_time, err := endpoint.GetTypePath()
	if err != nil {
		return time.Time{}, err
	}

	var last_err error
	for attempt := uint(0); attempt <= config.Retry; attempt++ {
		request, err := http.NewRequest(string(endpoint.Method), endpoint.URL.String(), nil)
		if err != nil {
			return time.Time{}, err
		}
		request.Header.Set("User-Agent", config.UserAgent)

		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			last_err = err
			continue
		}
		body, read_err := io.ReadAll(io.LimitReader(response.Body, int64(config.MaxRespSize)+1))
		close_err := response.Body.Close()
		finished := time.Now()

		if read_err != nil {
			last_err = read_err
			continue
		}
		if close_err != nil {
			last_err = close_err
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			last_err = fmt.Errorf("unexpected HTTP status %s", response.Status)
			continue
		}
		if uint(len(body)) > config.MaxRespSize {
			last_err = fmt.Errorf("response body exceeds %d bytes", config.MaxRespSize)
			continue
		}

		server_time, err := get_time(endpoint, body)
		if err != nil {
			last_err = err
			continue
		}
		return server_time.Add(finished.Sub(started) / 2), nil
	}
	return time.Time{}, last_err
}

func median_time(times []time.Time) time.Time {
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	middle := len(times) / 2
	if len(times)%2 == 1 {
		return times[middle]
	}
	return times[middle-1].Add(times[middle].Sub(times[middle-1]) / 2)
}

func sync_time(config *Config, set_time func(*time.Time) error) error {
	client := new_http_client(config)
	return sync_time_with_client(config, client, set_time)
}

func new_http_client(config *Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.InsecureSkipVerify = !config.TLSVerify
	return &http.Client{Timeout: config.Timeout.Duration, Transport: transport}
}

func sync_time_with_client(config *Config, client http_doer, set_time func(*time.Time) error) error {
	results := make([]time.Time, 0, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		server_time, err := fetch_endpoint_time(client, config, endpoint)
		if err != nil {
			logger.Warn("time endpoint failed", zap.String("url", endpoint.OriginalURL), zap.Error(err))
			continue
		}
		logger.Debug("time endpoint returned a time", zap.String("url", endpoint.OriginalURL), zap.Time("time", server_time))
		results = append(results, server_time)
	}
	if len(results) == 0 {
		return fmt.Errorf("all %d time endpoints failed", len(config.Endpoints))
	}
	chosen := median_time(results)
	if err := set_time(&chosen); err != nil {
		return fmt.Errorf("set system time: %w", err)
	}
	logger.Info("system time synchronized", zap.Time("time", chosen), zap.Int("successful_endpoints", len(results)))
	return nil
}

func Main() {
	if current_dir, err := os.Getwd(); err == nil {
		logger.Debug("loading config from current working directory", zap.String("dir", current_dir))
	}

	config := load_config()
	if config == nil {
		return
	}
	if level, ok := config.GetLogLevel(); ok {
		log_level.SetLevel(level)
	}
	defer logger.Sync()
	if err := sync_time(config, set_system_time); err != nil {
		logger.Error("time synchronization failed", zap.Error(err))
	}
}
