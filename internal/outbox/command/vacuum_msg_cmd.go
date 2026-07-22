package command

import (
	"context"
	"fmt"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
)

type VacuumMessages struct {
	dbMsgWriter *dbadapter.DBMessageWriter
	vacuumAge   time.Duration
}

func NewVacuumMessages(vacuumAge time.Duration, dbMsgWriter *dbadapter.DBMessageWriter) *VacuumMessages {
	return &VacuumMessages{dbMsgWriter: dbMsgWriter, vacuumAge: vacuumAge}
}

func (c *VacuumMessages) Exec(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-c.vacuumAge)

	n, err := c.dbMsgWriter.DeleteFrom(ctx, func(m domain.Message) bool {
		if !m.IsTerminal() {
			return false
		}

		return !m.UpdatedAt.UTC().After(cutoff.UTC())
	})
	if err != nil {
		return fmt.Errorf("failed vacuum outbox messages: %w", err)
	}
	if n == 0 {
		return nil
	}

	logbrick.FromCtx(ctx).Info("vacuumed outbox messages",
		"deleted", n,
		"cutoff", cutoff,
		"retention", c.vacuumAge)

	return nil
}
