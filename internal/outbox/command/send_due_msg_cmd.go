package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
)

type SendDueMessages struct {
	dbMsgWriter   *dbadapter.DBMessageWriter
	dbMsgReader   *dbadapter.DBMessageReader
	signalAdapter *signaladapter.SignalAdapter
	maxAge        time.Duration
}

func NewSendDueMessages(
	maxAge time.Duration,
	dbMsgWriter *dbadapter.DBMessageWriter,
	dbMsgReader *dbadapter.DBMessageReader,
	signalAdapter *signaladapter.SignalAdapter,
) *SendDueMessages {
	return &SendDueMessages{
		dbMsgWriter:   dbMsgWriter,
		dbMsgReader:   dbMsgReader,
		signalAdapter: signalAdapter,
		maxAge:        maxAge,
	}
}

func (c *SendDueMessages) Exec(ctx context.Context) error {
	now := time.Now().UTC()

	messages, err := c.dbMsgReader.Load(ctx, func(m domain.Message) bool {
		return m.IsDue(now)
	})
	if err != nil {
		return fmt.Errorf("failed load due messages: %w", err)
	}

	domain.SortMessagesByScheduledAt(messages)

	logger := logbrick.FromCtx(ctx)
	for _, msg := range messages {
		if err := c.sendDueMessage(ctx, logger, now, msg); err != nil {
			return err
		}
	}

	return nil
}

func (c *SendDueMessages) sendDueMessage(ctx context.Context, logger *slog.Logger, now time.Time, msg domain.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if msg.IsExpired(now, c.maxAge) {
		return c.handleExpiredDueMessage(ctx, logger, now, msg)
	}

	return c.sendActiveDueMessage(ctx, logger, msg)
}

func (c *SendDueMessages) handleExpiredDueMessage(ctx context.Context, logger *slog.Logger, now time.Time, msg domain.Message) error {
	expired, err := c.failExpiredMessage(ctx, msg.ID, now)
	if err != nil {
		return fmt.Errorf("failed mark expired outbox message %d: %w", msg.ID, err)
	}

	if err := c.notifyPermanentFailure(ctx, expired); err != nil {
		c.logPermanentFailureNotifyError(logger, expired, err)
	}

	logger.Error("outbox message expired before send",
		slog.Uint64("msg_id", expired.ID),
		slog.String("recipient", expired.Recipient),
		slog.Time("scheduled_at", expired.ScheduledAt),
		slog.Duration("max_age", c.maxAge))

	return nil
}

func (c *SendDueMessages) sendActiveDueMessage(ctx context.Context, logger *slog.Logger, msg domain.Message) error {
	attempted, err := c.startSendAttempt(ctx, msg.ID)
	if err != nil {
		return fmt.Errorf("failed start send attempt for outbox message %d: %w", msg.ID, err)
	}

	sendErr := c.signalAdapter.SendMessage(ctx, attempted.RecipientIdentifier, attempted.Text)
	if sendErr == nil {
		return c.handleSentDueMessage(ctx, logger, attempted)
	}

	return c.handleFailedDueMessage(ctx, logger, msg, attempted, sendErr)
}

func (c *SendDueMessages) handleSentDueMessage(ctx context.Context, logger *slog.Logger, attempted domain.Message) error {
	sent, err := c.finishSendSuccess(ctx, attempted.ID)
	if err != nil {
		return fmt.Errorf("failed mark outbox message %d as sent: %w", attempted.ID, err)
	}

	logger.Info("outbox message sent",
		slog.Uint64("msg_id", sent.ID),
		slog.Uint64("attempt", uint64(sent.Attempt)),
		slog.String("recipient", sent.Recipient))

	return nil
}

func (c *SendDueMessages) handleFailedDueMessage(ctx context.Context, logger *slog.Logger, previous, attempted domain.Message, sendErr error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if err := c.rollbackSendAttempt(ctx, previous); err != nil {
			return fmt.Errorf("failed rollback send attempt for outbox message %d: %w", previous.ID, err)
		}

		return ctxErr
	}

	finalized, err := c.finishSendFailure(ctx, attempted.ID, sendErr.Error())
	if err != nil {
		return fmt.Errorf("failed finalize outbox message %d send error: %w", attempted.ID, err)
	}

	if finalized.Status == domain.MessageStatusFailed {
		if err := c.notifyPermanentFailure(ctx, finalized); err != nil {
			c.logPermanentFailureNotifyError(logger, finalized, err)
		}
	}

	c.logSendFailureResult(logger, finalized)

	return nil
}

func (c *SendDueMessages) failExpiredMessage(ctx context.Context, id uint64, now time.Time) (domain.Message, error) {
	var expired domain.Message

	err := c.dbMsgWriter.Update(ctx, id, func(_ context.Context, m domain.Message) (domain.Message, error) {
		updatedMsg, err := m.MarkExpired(now, c.maxAge)
		if err != nil {
			return domain.Message{}, err
		}

		expired = updatedMsg

		return expired, nil
	})
	if err != nil {
		return domain.Message{}, err
	}

	return expired, nil
}

func (c *SendDueMessages) startSendAttempt(ctx context.Context, id uint64) (domain.Message, error) {
	var attempted domain.Message

	err := c.dbMsgWriter.Update(ctx, id, func(_ context.Context, m domain.Message) (domain.Message, error) {
		updatedMsg, err := m.StartSendAttempt()
		if err != nil {
			return domain.Message{}, err
		}

		attempted = updatedMsg

		return updatedMsg, nil
	})
	if err != nil {
		return domain.Message{}, err
	}

	return attempted, nil
}

func (c *SendDueMessages) finishSendSuccess(ctx context.Context, id uint64) (domain.Message, error) {
	var sent domain.Message

	err := c.dbMsgWriter.Update(ctx, id, func(_ context.Context, m domain.Message) (domain.Message, error) {
		updatedMsg, err := m.MarkSent()
		if err != nil {
			return domain.Message{}, err
		}

		sent = updatedMsg

		return sent, nil
	})
	if err != nil {
		return domain.Message{}, err
	}

	return sent, nil
}

func (c *SendDueMessages) finishSendFailure(ctx context.Context, id uint64, lastErr string) (domain.Message, error) {
	var updated domain.Message

	err := c.dbMsgWriter.Update(ctx, id, func(_ context.Context, m domain.Message) (domain.Message, error) {
		var err error
		if m.Attempt < m.MaxAttempts {
			updated, err = m.MarkRetry(lastErr)
		} else {
			updated, err = m.MarkFailed(lastErr)
		}
		if err != nil {
			return domain.Message{}, err
		}

		return updated, nil
	})
	if err != nil {
		return domain.Message{}, err
	}

	return updated, nil
}

func (c *SendDueMessages) rollbackSendAttempt(ctx context.Context, previous domain.Message) error {
	return c.dbMsgWriter.Update(ctx, previous.ID, func(_ context.Context, m domain.Message) (domain.Message, error) {
		if m.Status != previous.Status || m.Attempt != previous.Attempt+1 {
			return domain.Message{}, fmt.Errorf("%w: outbox message %d send attempt state changed", errbrick.ErrConflict, previous.ID)
		}

		return previous, nil
	})
}

func (c *SendDueMessages) notifyPermanentFailure(ctx context.Context, msg domain.Message) error {
	text := fmt.Sprintf(
		"Failed to deliver scheduled message %d to %s. Status: %s. Error: %s. Scheduled at: %s. Text: %s",
		msg.ID,
		msg.Recipient,
		msg.Status,
		msg.LastError,
		msg.ScheduledAt.UTC().Format(time.RFC3339),
		msg.Text,
	)

	if err := c.signalAdapter.SendSelfMessage(ctx, text); err != nil {
		return fmt.Errorf("send self failure notification: %w", err)
	}

	return nil
}

func (c *SendDueMessages) logPermanentFailureNotifyError(logger *slog.Logger, msg domain.Message, err error) {
	logger.Error("failed notify outbox permanent failure",
		slog.Uint64("msg_id", msg.ID),
		slog.String("recipient", msg.Recipient),
		slog.String("err", err.Error()))
}

func (c *SendDueMessages) logSendFailureResult(logger *slog.Logger, msg domain.Message) {
	logMsg := "outbox message send retry scheduled"
	if msg.Status == domain.MessageStatusFailed {
		logMsg = "outbox message send failed permanently"
	}

	logger.Error(logMsg,
		slog.Uint64("msg_id", msg.ID),
		slog.Uint64("attempt", uint64(msg.Attempt)),
		slog.Uint64("max_attempts", uint64(msg.MaxAttempts)),
		slog.String("recipient", msg.Recipient),
		slog.String("last_error", msg.LastError))
}
