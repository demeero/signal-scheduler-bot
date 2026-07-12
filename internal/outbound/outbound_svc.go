package outbound

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
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
	db *bbolt.DB
}

func New(db *bbolt.DB) (*Service, error) {
	s := &Service{
		db: db,
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

		msg, err = newMessage(seq, params.ScheduledAt, params.Recipient, params.RecipientIdentifier, params.Text)
		if err != nil {
			return err
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed encode outbound message: %w", err)
		}

		if err := bucket.Put(outboundMessageKey(msg.ID), data); err != nil {
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

		if err := bucket.Put(outboundMessageKey(id), data); err != nil {
			return fmt.Errorf("failed store outbound message: %w", err)
		}

		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("failed cancel outbound message: %w", err)
	}

	return cancelled, nil
}

func outboundMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}
