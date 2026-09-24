// Package logger 提供结构化日志。用标准库 slog，不引第三方。
package logger

import (
	"log/slog"
	"os"
)

// New 返回结构化 logger。开发环境用 text 便于阅读，生产用 JSON 便于采集。
func New(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == "development" {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	if env == "development" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(h).With("service", "tidings")
}
