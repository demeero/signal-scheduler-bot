package outbound

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

func (s *Service) CreateOutboundMessage(_ context.Context, params CreateOutboundMessageParams) (Message, error) {
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

func outboundMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}
