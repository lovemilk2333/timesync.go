package timesync

import (
	"log/slog"
	"os"
)

var log_level = &slog.LevelVar{}

var logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: log_level}))

func init() {
	log_level.Set(slog.LevelInfo)
}

func sync_logger() error {
	return os.Stderr.Sync()
}
