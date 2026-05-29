package config

import (
	"log/slog"
	"os"
	"strings"
)

var Logger = newLogger()

func newLogger() *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "DEBUG":
		level.Set(slog.LevelDebug)
	case "WARN":
		level.Set(slog.LevelWarn)
	case "ERROR":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func RefreshLogger() {
	Logger = newLogger()
}
