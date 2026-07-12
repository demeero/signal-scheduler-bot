package logbrick

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

type logCtxKey struct{}

var logKey = logCtxKey{}

func Configure(level string, addSource, json, pretty bool) {
	lvl := ParseLevel(level, slog.LevelInfo)
	handlerOpts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: addSource,
	}

	var h slog.Handler
	switch {
	case json:
		h = slog.NewJSONHandler(os.Stdout, handlerOpts)
	case pretty:
		h = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      lvl,
			AddSource:  addSource,
			TimeFormat: time.Kitchen,
		})
	default:
		h = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	logger := slog.New(h)

	slog.SetDefault(logger)
	slog.Info("log configured")
}

func ParseLevel(level string, fallback slog.Level) slog.Level {
	logLvl := &slog.LevelVar{}
	if err := logLvl.UnmarshalText([]byte(level)); err != nil {
		slog.Error("failed parse log level - use fallback",
			slog.Any("err", err), slog.String("level", level), slog.String("fallback", fallback.String()))
		logLvl.Set(fallback)
	}

	return logLvl.Level()
}

// FromCtx returns slog logger from context.
func FromCtx(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(logKey).(*slog.Logger)
	if !ok {
		// no slog instance in context - using default
		return slog.Default()
	}

	return logger
}

// ToCtx adds slog logger to context.
func ToCtx(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, logKey, logger)
}
