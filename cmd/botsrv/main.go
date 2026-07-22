package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "time/tzdata"

	"github.com/demeero/signal-scheduler-bot/internal/bot"
	"github.com/demeero/signal-scheduler-bot/internal/config"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	bolt "go.etcd.io/bbolt"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

const (
	versionFlag       = "--version"
	versionFlagCompat = "-version"
)

func main() {
	if shouldPrintVersion(os.Args[1:]) {
		fmt.Println(versionString())
		return
	}

	cfg := config.Load()
	logbrick.Configure(cfg.Log.Level, cfg.Log.AddSource, cfg.Log.JSON, cfg.Log.Pretty)
	slog.Info("starting signal scheduler bot",
		"version", Version,
		"commit", Commit,
		"build_time", BuildTime)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("failed run", "err", err)
		os.Exit(1)
	}
}

func shouldPrintVersion(args []string) bool {
	return len(args) == 1 && (args[0] == versionFlag || args[0] == versionFlagCompat)
}

func versionString() string {
	return fmt.Sprintf("signal-scheduler-bot version=%s commit=%s build_time=%s", Version, Commit, BuildTime)
}

func run(ctx context.Context, cfg config.Config) error {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return fmt.Errorf("failed load timezone: %w", err)
	}

	db, err := openBoltDB(cfg.Bolt)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed close bolt database", "err", err)
		}
	}()

	signalAdapter := signaladapter.New(cfg.Signal.Account, cfg.Signal.APIBaseURL, &http.Client{Timeout: cfg.Signal.RequestTimeout})

	outbox, err := outbox.New(cfg.Outbox.MaxAttempts, cfg.Outbox.MaxAge, cfg.Outbox.VacuumRetention, db, signalAdapter)
	if err != nil {
		return fmt.Errorf("failed init outbox service: %w", err)
	}

	botPoller := bot.New(cfg.Signal.Account, location, signalAdapter, outbox.Queries, outbox.Commands.Create, outbox.Commands.Cancel)

	var wg sync.WaitGroup
	runPeriodicWorker(ctx, &wg, "inbound polling", cfg.Bot.PollInterval, botPoller.Poll)
	runPeriodicWorker(ctx, &wg, "outbox sending", cfg.Outbox.WorkerInterval, outbox.Commands.SendDue.Exec)
	runPeriodicWorker(ctx, &wg, "outbox vacuum", cfg.Outbox.VacuumInterval, outbox.Commands.Vacuum.Exec)

	<-ctx.Done()
	wg.Wait()

	return nil
}

func openBoltDB(cfg config.Bolt) (*bolt.DB, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, errors.New("bolt path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create bolt parent directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: cfg.Timeout})
	if err != nil {
		return nil, fmt.Errorf("open bolt database: %w", err)
	}

	return db, nil
}

func runPeriodicWorker(ctx context.Context, wg *sync.WaitGroup, name string, interval time.Duration, fn func(context.Context) error) {
	wg.Go(func() {
		slog.Info("started worker", "worker", name, "interval", interval)
		for {
			if err := fn(ctx); err != nil {
				if ctx.Err() != nil {
					slog.Info("stopped worker", "worker", name)
					return
				}

				slog.Error("worker iteration failed", "worker", name, "err", err)
			}

			select {
			case <-ctx.Done():
				slog.Info("stopped worker", "worker", name)
				return
			case <-time.After(interval):
			}
		}
	})
}
