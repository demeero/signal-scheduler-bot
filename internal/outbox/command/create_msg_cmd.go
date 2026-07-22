package command

import (
	"context"
	"fmt"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
)

type CreateMessageParams struct {
	ScheduledAt         time.Time
	Recipient           string
	RecipientIdentifier string
	Text                string
}

type CreateMessage struct {
	dbMsgWriter *dbadapter.DBMessageWriter
	maxAttempts uint16
}

func NewCreateMessage(maxAttempts uint16, dbMsgWriter *dbadapter.DBMessageWriter) *CreateMessage {
	return &CreateMessage{maxAttempts: maxAttempts, dbMsgWriter: dbMsgWriter}
}

func (c *CreateMessage) Exec(ctx context.Context, params CreateMessageParams) (domain.Message, error) {
	var msg domain.Message

	err := c.dbMsgWriter.Create(ctx, func(id uint64) (domain.Message, error) {
		newMsg, err := domain.NewMessage(id, params.ScheduledAt, params.Recipient, params.RecipientIdentifier, params.Text, c.maxAttempts)
		if err != nil {
			return domain.Message{}, err
		}

		msg = newMsg

		return msg, err
	})
	if err != nil {
		return domain.Message{}, fmt.Errorf("failed create outbox message: %w", err)
	}

	return msg, nil
}
