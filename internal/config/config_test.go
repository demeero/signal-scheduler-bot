package config

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoad_UsesEnvAndCachesResult(t *testing.T) {
	resetConfigStateForTest(t)

	t.Setenv("TIMEZONE", "UTC")
	t.Setenv("SIGNAL_API_BASE_URL", "http://signal.test")
	t.Setenv("SIGNAL_ACCOUNT", "+380500000000")
	t.Setenv("LOG_CONFIG", "false")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("OUTBOX_MAX_ATTEMPTS", "7")
	t.Setenv("OUTBOX_MAX_AGE", "20m")
	t.Setenv("BOT_POLL_INTERVAL", "9s")

	loaded := Load()
	require.Equal(t, "UTC", loaded.Timezone)
	require.Equal(t, "http://signal.test", loaded.Signal.APIBaseURL)
	require.Equal(t, "+380500000000", loaded.Signal.Account)
	require.Equal(t, "warn", loaded.Log.Level)
	require.EqualValues(t, 7, loaded.Outbox.MaxAttempts)
	require.Equal(t, 20*time.Minute, loaded.Outbox.MaxAge)
	require.Equal(t, 9*time.Second, loaded.Bot.PollInterval)

	t.Setenv("TIMEZONE", "Europe/Kyiv")

	cached := Load()
	require.Equal(t, loaded, cached)
	require.Equal(t, "UTC", cached.Timezone)
}

func resetConfigStateForTest(t *testing.T) {
	t.Helper()

	cfg = Config{}
	once = sync.Once{}

	t.Cleanup(func() {
		cfg = Config{}
		once = sync.Once{}
	})
}
