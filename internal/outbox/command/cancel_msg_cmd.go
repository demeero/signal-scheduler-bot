package command

import (
	"context"
	"fmt"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
)

type CancelMessage struct {
	dbMsgWriter *dbadapter.DBMessageWriter
}

func NewCancelMesssage(dbMsgWriter *dbadapter.DBMessageWriter) *CancelMessage {
	return &CancelMessage{dbMsgWriter: dbMsgWriter}
}

func (s *CancelMessage) Exec(ctx context.Context, id uint64) (domain.Message, error) {
	var cancelled domain.Message

	err := s.dbMsgWriter.Update(ctx, id, func(_ context.Context, m domain.Message) (domain.Message, error) {
		cancelledMsg, err := m.Cancel()
		if err != nil {
			return domain.Message{}, err
		}

		cancelled = cancelledMsg

		return cancelledMsg, nil
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("failed cancel outbox message: %w", err)
	}

	return cancelled, nil
}
