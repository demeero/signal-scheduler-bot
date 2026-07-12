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
	"syscall"
	"time"

	_ "time/tzdata"

	"github.com/demeero/signal-scheduler-bot/internal/config"
	"github.com/demeero/signal-scheduler-bot/internal/inbound"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbound"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	bolt "go.etcd.io/bbolt"
)

func main() {
	cfg := config.Load()
	logbrick.Configure(cfg.Log.Level, cfg.Log.AddSource, cfg.Log.JSON, cfg.Log.Pretty)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("failed run", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	location, err := time.LoadLocation(cfg.Scheduler.Timezone)
	if err != nil {
		return fmt.Errorf("load scheduler timezone: %w", err)
	}

	db, err := openBoltDB(cfg.Bolt)
	if err != nil {
		return err
	}
	defer closeBoltDB(db)

	outboundSvc, err := outbound.New(db)
	if err != nil {
		return fmt.Errorf("init outbound adapter: %w", err)
	}

	signalAdapter := newSignalAdapter(cfg)

	inboundPoller := inbound.New(cfg.Signal.Account, location, signalAdapter, outboundSvc)

	go func() {
		for {
			if err := inboundPoller.Poll(ctx); err != nil {
				slog.Error("failed poll", "err", err)
			}

			select {
			case <-ctx.Done():
				slog.Info("ctx done received - finish inbound polling")
				return
			case <-time.After(cfg.Scheduler.PollInterval):
			}
		}
	}()

	<-ctx.Done()
	return nil
}

func newSignalAdapter(cfg config.Config) *signaladapter.SignalAdapter {
	timeout := cfg.Signal.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	return signaladapter.New(cfg.Signal.Account, cfg.Signal.APIBaseURL, &http.Client{Timeout: timeout})
}

func openBoltDB(cfg config.Bolt) (*bolt.DB, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, errors.New("bolt path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create bolt parent directory: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("open bolt database: %w", err)
	}

	return db, nil
}

func closeBoltDB(db *bolt.DB) {
	if err := db.Close(); err != nil {
		slog.Error("failed close bolt database", "err", err)
	}
}
