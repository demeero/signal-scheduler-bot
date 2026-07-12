package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"go.etcd.io/bbolt"
)

var (
	outboxMessagesBucket = []byte("outbox_messages")
)

type CreateMessageParams struct {
	ScheduledAt         time.Time
	Recipient           string
	RecipientIdentifier string
	Text                string
}

type Service struct {
	db           *bbolt.DB
	signalClient *signaladapter.SignalAdapter
	maxAttempts  uint16
	maxAge       time.Duration
}

func New(maxAttempts uint16, maxAge time.Duration, db *bbolt.DB, signalClient *signaladapter.SignalAdapter) (*Service, error) {
	s := &Service{
		db:           db,
		signalClient: signalClient,
		maxAttempts:  maxAttempts,
		maxAge:       maxAge,
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(outboxMessagesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed initialize outbox service: %w", err)
	}

	return s, nil
}

func (s *Service) CreateMessage(_ context.Context, params CreateMessageParams) (Message, error) {
	var msg Message
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		seq, err := bucket.NextSequence()
		if err != nil {
			return fmt.Errorf("failed generate outbox message ID: %w", err)
		}

		msg, err = newMessage(seq, params.ScheduledAt, params.Recipient, params.RecipientIdentifier, params.Text, s.maxAttempts)
		if err != nil {
			return fmt.Errorf("failed create outbox message: %w", err)
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed encode outbox message: %w", err)
		}

		if err := bucket.Put(msg.key(), data); err != nil {
			return fmt.Errorf("failed store outbox message: %w", err)
		}

		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("failed put outbox message: %w", err)
	}

	return msg, nil
}

func (s *Service) LoadUpcomingMessages(_ context.Context) ([]Message, error) {
	var messages []Message

	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		return bucket.ForEach(func(_, value []byte) error {
			var msg Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return fmt.Errorf("failed decode outbox message: %w", err)
			}

			if msg.IsUpcoming() {
				messages = append(messages, msg)
			}

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed list outbox messages: %w", err)
	}

	sortMessages(messages)
	return messages, nil
}

func (s *Service) CancelMessage(_ context.Context, id uint64) (Message, error) {
	var cancelled Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		raw := bucket.Get(outboxMessageKey(id))
		if raw == nil {
			return fmt.Errorf("%w: outbox message %d", errbrick.ErrNotFound, id)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("failed decode outbox message: %w", err)
		}

		cancelledMsg, err := msg.Cancel()
		if err != nil {
			return err
		}
		cancelled = cancelledMsg

		data, err := json.Marshal(cancelled)
		if err != nil {
			return fmt.Errorf("failed encode outbox message: %w", err)
		}

		if err := bucket.Put(cancelled.key(), data); err != nil {
			return fmt.Errorf("failed store outbox message: %w", err)
		}

		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("failed cancel outbox message: %w", err)
	}

	return cancelled, nil
}

func (s *Service) SendDue(ctx context.Context) error {
	now := time.Now().UTC()
	messages, err := s.loadDueMessages(now)
	if err != nil {
		return fmt.Errorf("failed load due outbox messages: %w", err)
	}

	logger := logbrick.FromCtx(ctx)
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			return err
		}

		if msg.IsExpired(now, s.maxAge) {
			expired, err := s.failExpiredMessage(msg.ID, now)
			if err != nil {
				return fmt.Errorf("failed mark expired outbox message %d: %w", msg.ID, err)
			}

			if err := s.notifyPermanentFailure(ctx, expired); err != nil {
				logger.Error("failed notify outbox permanent failure",
					slog.Uint64("msg_id", expired.ID),
					slog.String("recipient", expired.Recipient),
					slog.String("err", err.Error()))
			}

			logger.Error("outbox message expired before send",
				slog.Uint64("msg_id", expired.ID),
				slog.String("recipient", expired.Recipient),
				slog.Time("scheduled_at", expired.ScheduledAt),
				slog.Duration("max_age", s.maxAge))
			continue
		}

		attempted, err := s.startSendAttempt(msg.ID)
		if err != nil {
			return fmt.Errorf("failed start send attempt for outbox message %d: %w", msg.ID, err)
		}

		sendErr := s.signalClient.SendMessage(ctx, attempted.RecipientIdentifier, attempted.Text)
		if sendErr == nil {
			sent, err := s.finishSendSuccess(attempted.ID)
			if err != nil {
				return fmt.Errorf("failed mark outbox message %d as sent: %w", attempted.ID, err)
			}

			logger.Info("outbox message sent",
				slog.Uint64("msg_id", sent.ID),
				slog.Uint64("attempt", uint64(sent.Attempt)),
				slog.String("recipient", sent.Recipient))
			continue
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			if err := s.rollbackSendAttempt(msg); err != nil {
				return fmt.Errorf("failed rollback send attempt for outbox message %d: %w", msg.ID, err)
			}

			return ctxErr
		}

		finalized, err := s.finishSendFailure(attempted.ID, sendErr.Error())
		if err != nil {
			return fmt.Errorf("failed finalize outbox message %d send error: %w", attempted.ID, err)
		}

		logMsg := "outbox message send retry scheduled"
		if finalized.Status == MessageStatusFailed {
			logMsg = "outbox message send failed permanently"

			if err := s.notifyPermanentFailure(ctx, finalized); err != nil {
				logger.Error("failed notify outbox permanent failure",
					slog.Uint64("msg_id", finalized.ID),
					slog.String("recipient", finalized.Recipient),
					slog.String("err", err.Error()))
			}
		}
		logger.Error(logMsg,
			slog.Uint64("msg_id", finalized.ID),
			slog.Uint64("attempt", uint64(finalized.Attempt)),
			slog.Uint64("max_attempts", uint64(finalized.MaxAttempts)),
			slog.String("recipient", finalized.Recipient),
			slog.String("last_error", finalized.LastError))
	}

	return nil
}

func (s *Service) loadDueMessages(now time.Time) ([]Message, error) {
	var messages []Message

	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		return bucket.ForEach(func(_, value []byte) error {
			var msg Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return fmt.Errorf("failed decode outbox message: %w", err)
			}

			if msg.IsDue(now) {
				messages = append(messages, msg)
			}

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	sortMessages(messages)
	return messages, nil
}

func (s *Service) failExpiredMessage(id uint64, now time.Time) (Message, error) {
	var expired Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		msg, bucket, err := s.loadMessageForUpdate(tx, id)
		if err != nil {
			return err
		}

		expired, err = msg.MarkExpired(now, s.maxAge)
		if err != nil {
			return err
		}

		return storeMessage(bucket, expired)
	})
	if err != nil {
		return Message{}, err
	}

	return expired, nil
}

func (s *Service) startSendAttempt(id uint64) (Message, error) {
	var attempted Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		msg, bucket, err := s.loadMessageForUpdate(tx, id)
		if err != nil {
			return err
		}

		attempted, err = msg.StartSendAttempt()
		if err != nil {
			return err
		}

		return storeMessage(bucket, attempted)
	})
	if err != nil {
		return Message{}, err
	}

	return attempted, nil
}

func (s *Service) finishSendSuccess(id uint64) (Message, error) {
	var sent Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		msg, bucket, err := s.loadMessageForUpdate(tx, id)
		if err != nil {
			return err
		}

		sent, err = msg.MarkSent()
		if err != nil {
			return err
		}

		return storeMessage(bucket, sent)
	})
	if err != nil {
		return Message{}, err
	}

	return sent, nil
}

func (s *Service) finishSendFailure(id uint64, lastErr string) (Message, error) {
	var updated Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		msg, bucket, err := s.loadMessageForUpdate(tx, id)
		if err != nil {
			return err
		}

		if msg.Attempt < msg.MaxAttempts {
			updated, err = msg.MarkRetry(lastErr)
		} else {
			updated, err = msg.MarkFailed(lastErr)
		}
		if err != nil {
			return err
		}

		return storeMessage(bucket, updated)
	})
	if err != nil {
		return Message{}, err
	}

	return updated, nil
}

func (s *Service) notifyPermanentFailure(ctx context.Context, msg Message) error {
	text := fmt.Sprintf(
		"Failed to deliver scheduled message %d to %s. Status: %s. Error: %s. Scheduled at: %s. Text: %s",
		msg.ID,
		msg.Recipient,
		msg.Status,
		msg.LastError,
		msg.ScheduledAt.UTC().Format(time.RFC3339),
		msg.Text,
	)

	if err := s.signalClient.SendSelfMessage(ctx, text); err != nil {
		return fmt.Errorf("send self failure notification: %w", err)
	}

	return nil
}

func (s *Service) rollbackSendAttempt(previous Message) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		current, bucket, err := s.loadMessageForUpdate(tx, previous.ID)
		if err != nil {
			return err
		}

		if current.Status != previous.Status || current.Attempt != previous.Attempt+1 {
			return fmt.Errorf("%w: outbox message %d send attempt state changed", errbrick.ErrConflict, previous.ID)
		}

		return storeMessage(bucket, previous)
	})
}

func (s *Service) loadMessageForUpdate(tx *bbolt.Tx, id uint64) (Message, *bbolt.Bucket, error) {
	bucket := tx.Bucket(outboxMessagesBucket)
	if bucket == nil {
		return Message{}, nil, errors.New("outbox messages bucket does not exist")
	}

	raw := bucket.Get(outboxMessageKey(id))
	if raw == nil {
		return Message{}, nil, fmt.Errorf("%w: outbox message %d", errbrick.ErrNotFound, id)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Message{}, nil, fmt.Errorf("failed decode outbox message: %w", err)
	}

	return msg, bucket, nil
}

func storeMessage(bucket *bbolt.Bucket, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed encode outbox message: %w", err)
	}

	if err := bucket.Put(msg.key(), data); err != nil {
		return fmt.Errorf("failed store outbox message: %w", err)
	}

	return nil
}

func sortMessages(messages []Message) {
	slices.SortFunc(messages, func(a, b Message) int {
		if cmp := a.ScheduledAt.Compare(b.ScheduledAt); cmp != 0 {
			return cmp
		}

		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
}
