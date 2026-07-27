package logger

import (
	"log/slog"
	"os"
	"time"
)

var levelMap = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func Init(level string) {
	lvl, ok := levelMap[level]
	if !ok {
		lvl = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.String("time", time.Now().Format("2006-01-02T15:04:05"))
			}
			return a
		},
	})

	slog.SetDefault(slog.New(handler))
}

func Debug(msg string, args ...interface{}) {
	slog.Debug(msg, args...)
}

func Info(msg string, args ...interface{}) {
	slog.Info(msg, args...)
}

func Warn(msg string, args ...interface{}) {
	slog.Warn(msg, args...)
}

func Error(msg string, args ...interface{}) {
	slog.Error(msg, args...)
}

func Fatal(msg string, args ...interface{}) {
	slog.Error(msg, args...)
	os.Exit(1)
}
