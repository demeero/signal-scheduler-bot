package dbadapter

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	bolt "go.etcd.io/bbolt"
)

var (
	outboundMessagesBucket = []byte("outbound_messages")
)

type OutboundMessageStatus string

const (
	OutboundMessageStatusPending   OutboundMessageStatus = "pending"
	OutboundMessageStatusSent      OutboundMessageStatus = "sent"
	OutboundMessageStatusFailed    OutboundMessageStatus = "failed"
	OutboundMessageStatusCancelled OutboundMessageStatus = "cancelled"
)

type OutboundMessage struct {
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	ScheduledAt         time.Time             `json:"scheduled_at"`
	RecipientIdentifier string                `json:"recipient_identifier"`
	Recipient           string                `json:"recipient"`
	Text                string                `json:"text"`
	Status              OutboundMessageStatus `json:"status"`
	ID                  uint64                `json:"id"`
}

func newOutboundMessage(id uint64, scheduledAt time.Time, recipient, recipientIdentifier, text string) (OutboundMessage, error) {
	if scheduledAt.IsZero() {
		return OutboundMessage{}, fmt.Errorf("%w: scheduledAt empty", errbrick.ErrInvalidData)
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return OutboundMessage{}, fmt.Errorf("%w: recipient empty", errbrick.ErrInvalidData)
	}
	recipientIdentifier = strings.TrimSpace(recipientIdentifier)
	if recipientIdentifier == "" {
		return OutboundMessage{}, fmt.Errorf("%w: recipientIdentifier empty", errbrick.ErrInvalidData)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return OutboundMessage{}, fmt.Errorf("%w: text empty", errbrick.ErrInvalidData)
	}

	now := time.Now().UTC()

	return OutboundMessage{
		ID:                  id,
		CreatedAt:           now,
		UpdatedAt:           now,
		ScheduledAt:         scheduledAt.UTC(),
		Recipient:           recipient,
		RecipientIdentifier: recipientIdentifier,
		Text:                text,
		Status:              OutboundMessageStatusPending,
	}, nil
}

type OutboundAdapter struct {
	db *bolt.DB
}

func NewOutboundAdapter(db *bolt.DB) (*OutboundAdapter, error) {
	adapter := &OutboundAdapter{
		db: db,
	}

	err := adapter.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(outboundMessagesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed initialize outbound adapter: %w", err)
	}

	return adapter, nil
}

func (a *OutboundAdapter) InsertOutboundMessage(
	ctx context.Context,
	scheduledAt time.Time,
	recipient, recipientIdentifier, text string,
) (OutboundMessage, error) {
	var msg OutboundMessage
	err := a.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(outboundMessagesBucket)
		if bucket == nil {
			return errors.New("outbound messages bucket does not exist")
		}

		sequence, err := bucket.NextSequence()
		if err != nil {
			return fmt.Errorf("failed generate outbound message ID: %w", err)
		}

		msg, err = newOutboundMessage(sequence, scheduledAt, recipient, recipientIdentifier, text)
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
		return OutboundMessage{}, fmt.Errorf("failed put outbound message: %w", err)
	}

	return msg, nil
}

func outboundMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}
