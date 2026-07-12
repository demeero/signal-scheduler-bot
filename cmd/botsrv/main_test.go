package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRun_InvalidTimezone(t *testing.T) {
	err := run(t.Context(), config.Config{Timezone: "Mars/Olympus"})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed load timezone")
}

func TestOpenBoltDB_RejectsEmptyPath(t *testing.T) {
	_, err := openBoltDB(config.Bolt{Path: " \t ", Timeout: time.Second})
	require.Error(t, err)
	require.ErrorContains(t, err, "bolt path is empty")
}

func TestOpenBoltDB_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "signal.db")

	db, err := openBoltDB(config.Bolt{Path: path, Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	info, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestRunPeriodicWorker_RunsAndStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var wg sync.WaitGroup
	calls := make(chan struct{}, 1)

	runPeriodicWorker(ctx, &wg, "test-worker", time.Hour, func(context.Context) error {
		select {
		case calls <- struct{}{}:
		default:
		}

		cancel()
		return nil
	})

	wg.Wait()
	require.Len(t, calls, 1)
}
