package config

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Signal    Signal    `json:"signal"`
	Log       Log       `json:"log"`
	Bolt      Bolt      `json:"bolt"`
	Scheduler Scheduler `json:"scheduler"`
	Retry     Retry     `json:"retry"`
	LogConfig bool      `default:"true"   json:"log_config" split_words:"true"`
}

type Log struct {
	// Level is the log level.
	Level string `default:"debug" json:"level"`
	// AddSource adds source file and line number to log.
	AddSource bool `default:"true" json:"add_source" split_words:"true"`
	// JSON enables JSON output.
	JSON bool `json:"json"`
	// Pretty enables pretty console output.
	Pretty bool `default:"true" json:"pretty"`
}

type Bolt struct {
	// Path is the BoltDB database path.
	Path string `default:"./var/signal-scheduler-bot" json:"path"`
	// Timeout configures how long Bolt waits for a writable file lock.
	Timeout time.Duration `default:"5s" json:"timeout"`
}

type Signal struct {
	// APIBaseURL is the base URL of signal-cli-rest-api.
	APIBaseURL string `default:"http://localhost:18080" json:"api_base_url" required:"true" split_words:"true"`
	// Account is the Signal account used by the scheduler.
	Account string `json:"-" required:"true"`
	// RequestTimeout limits a single outbound Signal API request.
	RequestTimeout time.Duration `default:"30s" json:"request_timeout" split_words:"true"`
}

type Scheduler struct {
	// Timezone is the default timezone for parsing scheduled commands.
	Timezone string `default:"Europe/Kyiv" json:"timezone"`
	// PollInterval controls how often the incoming poller runs.
	PollInterval time.Duration `default:"5s" json:"poll_interval" split_words:"true"`
	// WorkerInterval controls how often due scheduled messages are scanned.
	WorkerInterval time.Duration `default:"1s" json:"worker_interval" split_words:"true"`
}

type Retry struct {
	// MaxAttempts is the maximum number of send attempts.
	MaxAttempts uint16 `default:"5" json:"max_attempts" split_words:"true"`
}

var (
	cfg  Config
	once sync.Once
)

func Load() Config {
	once.Do(func() {
		envconfig.MustProcess("", &cfg)
		if !cfg.LogConfig {
			return
		}

		b, err := json.Marshal(cfg)
		if err != nil {
			slog.Error("failed marshal config", "err", err)
			return
		}

		slog.Info("parsed config", slog.String("config", string(b)))
	})

	return cfg
}
