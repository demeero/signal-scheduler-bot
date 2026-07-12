package logbrick

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigure_SetsDefaultLoggerForSupportedHandlers(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	tests := []struct {
		name   string
		json   bool
		pretty bool
	}{
		{name: "json", json: true},
		{name: "pretty", pretty: true},
		{name: "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Configure("warn", true, tt.json, tt.pretty)
			require.NotNil(t, slog.Default())
		})
	}
}

func TestParseLevel_InvalidUsesFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	level := ParseLevel("definitely-not-a-level", slog.LevelWarn)
	require.Equal(t, slog.LevelWarn, level)
}

func TestContextHelpers(t *testing.T) {
	require.Same(t, slog.Default(), FromCtx(context.Background()))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := ToCtx(context.Background(), logger)

	require.Same(t, logger, FromCtx(ctx))
}
