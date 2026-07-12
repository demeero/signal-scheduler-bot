package outbound

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
	outboundMessagesBucket = []byte("outbound_messages")
)

type CreateOutboundMessageParams struct {
	ScheduledAt         time.Time
	Recipient           string
	RecipientIdentifier string
	Text                string
}

type Service struct {
	db           *bbolt.DB
	signalClient *signaladapter.SignalAdapter
	maxAttempts  uint16
}

func New(maxAttempts uint16, db *bbolt.DB, signalClient *signaladapter.SignalAdapter) (*Service, error) {
	s := &Service{
		db:           db,
		signalClient: signalClient,
		maxAttempts:  maxAttempts,
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(outboundMessagesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed initialize outbound adapter: %w", err)
	}

	return s, nil
}

func (s *Service) CreateMessage(_ context.Context, params CreateOutboundMessageParams) (Message, error) {
	var msg Message
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboundMessagesBucket)
		if bucket == nil {
			return errors.New("outbound messages bucket does not exist")
		}

		seq, err := bucket.NextSequence()
		if err != nil {
			return fmt.Errorf("failed generate outbound message ID: %w", err)
		}

		msg, err = newMessage(seq, params.ScheduledAt, params.Recipient, params.RecipientIdentifier, params.Text, s.maxAttempts)
		if err != nil {
			return fmt.Errorf("failed create outbound message: %w", err)
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed encode outbound message: %w", err)
		}

		if err := bucket.Put(msg.key(), data); err != nil {
			return fmt.Errorf("failed store outbound message: %w", err)
		}

		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("failed put outbound message: %w", err)
	}

	return msg, nil
}

func (s *Service) LoadUpcomingMessages(_ context.Context) ([]Message, error) {
	var messages []Message

	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboundMessagesBucket)
		if bucket == nil {
			return errors.New("outbound messages bucket does not exist")
		}

		return bucket.ForEach(func(_, value []byte) error {
			var msg Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return fmt.Errorf("failed decode outbound message: %w", err)
			}

			if msg.IsUpcoming() {
				messages = append(messages, msg)
			}

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed list outbound messages: %w", err)
	}

	sortMessages(messages)
	return messages, nil
}

func (s *Service) CancelMessage(_ context.Context, id uint64) (Message, error) {
	var cancelled Message

	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(outboundMessagesBucket)
		if bucket == nil {
			return errors.New("outbound messages bucket does not exist")
		}

		raw := bucket.Get(outboundMessageKey(id))
		if raw == nil {
			return fmt.Errorf("%w: outbound message %d", errbrick.ErrNotFound, id)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("failed decode outbound message: %w", err)
		}

		canceledMsg, err := msg.Cancel()
		if err != nil {
			return err
		}
		cancelled = canceledMsg

		data, err := json.Marshal(cancelled)
		if err != nil {
			return fmt.Errorf("failed encode outbound message: %w", err)
		}

		if err := bucket.Put(cancelled.key(), data); err != nil {
			return fmt.Errorf("failed store outbound message: %w", err)
		}

		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("failed cancel outbound message: %w", err)
	}

	return cancelled, nil
}

func (s *Service) SendDue(ctx context.Context) error {
	now := time.Now().UTC()
	messages, err := s.loadDueMessages(now)
	if err != nil {
		return fmt.Errorf("failed load due outbound messages: %w", err)
	}

	logger := logbrick.FromCtx(ctx)
	for _, msg := range messages {
		attempted, err := s.startSendAttempt(msg.ID)
		if err != nil {
			return fmt.Errorf("failed start send attempt for outbound message %d: %w", msg.ID, err)
		}

		sendErr := s.signalClient.SendMessage(ctx, attempted.RecipientIdentifier, attempted.Text)
		if sendErr == nil {
			sent, err := s.finishSendSuccess(attempted.ID)
			if err != nil {
				return fmt.Errorf("failed mark outbound message %d as sent: %w", attempted.ID, err)
			}

			logger.Info("outbound message sent",
				slog.Uint64("msg_id", sent.ID),
				slog.Uint64("attempt", uint64(sent.Attempt)),
				slog.String("recipient", sent.Recipient))
			continue
		}

		finalized, err := s.finishSendFailure(attempted.ID, sendErr.Error())
		if err != nil {
			return fmt.Errorf("failed finalize outbound message %d send error: %w", attempted.ID, err)
		}

		logMsg := "outbound message send retry scheduled"
		if finalized.Status == MessageStatusFailed {
			logMsg = "outbound message send failed permanently"
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
		bucket := tx.Bucket(outboundMessagesBucket)
		if bucket == nil {
			return errors.New("outbound messages bucket does not exist")
		}

		return bucket.ForEach(func(_, value []byte) error {
			var msg Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return fmt.Errorf("failed decode outbound message: %w", err)
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

func (s *Service) loadMessageForUpdate(tx *bbolt.Tx, id uint64) (Message, *bbolt.Bucket, error) {
	bucket := tx.Bucket(outboundMessagesBucket)
	if bucket == nil {
		return Message{}, nil, errors.New("outbound messages bucket does not exist")
	}

	raw := bucket.Get(outboundMessageKey(id))
	if raw == nil {
		return Message{}, nil, fmt.Errorf("%w: outbound message %d", errbrick.ErrNotFound, id)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Message{}, nil, fmt.Errorf("failed decode outbound message: %w", err)
	}

	return msg, bucket, nil
}

func storeMessage(bucket *bbolt.Bucket, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed encode outbound message: %w", err)
	}

	if err := bucket.Put(msg.key(), data); err != nil {
		return fmt.Errorf("failed store outbound message: %w", err)
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
