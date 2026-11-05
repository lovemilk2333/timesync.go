package timesync

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log_level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

var logger = func() *zap.Logger {
	config := zap.NewProductionConfig()
	config.Level = log_level
	instance, _ := config.Build()
	return instance
}()
