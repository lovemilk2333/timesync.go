package timesync

import (
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"sync"
	"time"

	"log/slog"
)

//go:embed embedded.json
var default_config_data []byte

var v = NewValidator()

const ConfigFile = "config.json"

func load_config() *Config {
	var default_config Config
	if err := json.Unmarshal(default_config_data, &default_config); err != nil {
		logger.Error("failed to unmarshal default config json", slog.Any("error", err))
		return nil
	}
	if err := default_config.Parse(v); err != nil {
		logger.Error("failed to check default config json", slog.Any("error", err))
		return nil
	}

	file, err := os.Open(ConfigFile)
	if err != nil {
		logger.Info("using embedded default config", slog.String("file", ConfigFile))
		return &default_config
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxConfigSize+1))
	if err != nil {
		logger.Error("failed to read config file", slog.Any("error", err))
		return &default_config
	}
	if int64(len(data)) > MaxConfigSize {
		logger.Error("config file size exceeds limit", slog.Int64("max_size", MaxConfigSize))
		return &default_config
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		logger.Error("failed to unmarshal config json", slog.Any("error", err))
		return &default_config
	}
	if err := config.Parse(v); err != nil {
		logger.Error("failed to check config json", slog.Any("error", err))
		return &default_config
	}
	return &config
}

// fetch_endpoint_time queries one endpoint and compensates for request latency
// by adding half the time from obtaining a TCP connection to the first response
// byte. For a reused connection, obtaining the connection is the start point.
type HttpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func fetch_endpoint_time(client HttpDoer, config *Config, endpoint *Endpoint) (time.Time, time.Duration, error) {
	_, get_time, err := endpoint.GetTypePath()
	if err != nil {
		return time.Time{}, 0, err
	}

	var last_err error
	for attempt := uint(0); attempt <= config.Retry; attempt++ {
		request, err := http.NewRequest(string(endpoint.Method), endpoint.URL.String(), nil)
		if err != nil {
			return time.Time{}, 0, err
		}
		request.Header.Set("User-Agent", config.UserAgent)

		var trace_mu sync.Mutex
		var tcp_connected_at, connection_at, first_byte_at time.Time
		trace := &httptrace.ClientTrace{
			ConnectDone: func(_, _ string, err error) {
				if err == nil {
					trace_mu.Lock()
					tcp_connected_at = time.Now()
					trace_mu.Unlock()
				}
			},
			GotConn: func(httptrace.GotConnInfo) {
				trace_mu.Lock()
				connection_at = time.Now()
				trace_mu.Unlock()
			},
			GotFirstResponseByte: func() {
				trace_mu.Lock()
				first_byte_at = time.Now()
				trace_mu.Unlock()
			},
		}
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			last_err = err
			continue
		}
		response_received_at := time.Now()
		body, read_err := io.ReadAll(io.LimitReader(response.Body, int64(config.MaxRespSize)+1))
		close_err := response.Body.Close()

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
		trace_mu.Lock()
		latency_start, latency_end := tcp_connected_at, first_byte_at
		if latency_start.IsZero() {
			// A reused connection has no ConnectDone event.
			latency_start = connection_at
		}
		trace_mu.Unlock()
		// Real http.Transport calls both trace hooks. A custom RoundTripper may
		// not; retain a useful fallback based on time until response headers arrive.
		if latency_start.IsZero() || latency_end.IsZero() || latency_end.Before(latency_start) {
			latency_start, latency_end = started, response_received_at
		}

		rtt := latency_end.Sub(latency_start)
		return server_time.Add(rtt / 2), rtt, nil
	}
	return time.Time{}, 0, last_err
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

func sync_time_with_client(config *Config, client HttpDoer, set_time func(*time.Time) error) error {
	if len(config.Endpoints) == 0 {
		return fmt.Errorf("no time endpoints configured")
	}
	for _, endpoint := range config.Endpoints {
		logger.Info("time endpoint start to sync", slog.String("url", endpoint.OriginalURL))
		server_time, rtt, err := fetch_endpoint_time(client, config, endpoint)
		if err != nil {
			logger.Warn("time endpoint failed", slog.String("url", endpoint.OriginalURL), slog.Any("error", err))
			continue
		}
		logger.Debug("time endpoint returned a time", slog.String("url", endpoint.OriginalURL), slog.Time("time", server_time))
		if err := set_time(&server_time); err != nil {
			return fmt.Errorf("set system time: %w", err)
		}
		logger.Info("system time synchronized", slog.String("url", endpoint.OriginalURL), slog.Time("time", server_time), slog.String("delay_error", "±"+(rtt/2).String()))
		return nil
	}
	return fmt.Errorf("all %d time endpoints failed", len(config.Endpoints))
}

func wait_enter_exit(timeout time.Duration) {
	remaining := int(timeout.Seconds())
	// more then 1 second
	if timeout > time.Second {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			fmt.Printf("press enter to exit. will auto exit in %02d s...\r", remaining)
			for range ticker.C {
				remaining--
				if remaining <= 0 {
					break
				}
				fmt.Printf("press enter to exit. will auto exit in %02d s...\r", remaining)
			}

			os.Exit(0)
		}()
		fmt.Scanln()
	} else {
		time.Sleep(timeout)
	}

	os.Exit(0)
}

func Main() {
	if current_dir, err := os.Getwd(); err == nil {
		logger.Debug("loading config from current working directory", slog.String("dir", current_dir))
	}

	config := load_config()
	if config == nil {
		return
	}
	if level, ok := config.GetLogLevel(); ok {
		log_level.Set(level)
	}
	defer sync_logger()
	if err := sync_time(config, set_system_time); err != nil {
		logger.Error("time synchronization failed", slog.Any("error", err))
	}

	wait_enter_exit(time.Second * 7)
}
