package outbound

import (
	"fmt"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
)

type MessageStatus string

const (
	MessageStatusPending   MessageStatus = "pending"
	MessageStatusSent      MessageStatus = "sent"
	MessageStatusFailed    MessageStatus = "failed"
	MessageStatusCancelled MessageStatus = "cancelled"
)

type Message struct {
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	ScheduledAt         time.Time     `json:"scheduled_at"`
	RecipientIdentifier string        `json:"recipient_identifier"`
	Recipient           string        `json:"recipient"`
	Text                string        `json:"text"`
	Status              MessageStatus `json:"status"`
	ID                  uint64        `json:"id"`
}

func newMessage(id uint64, scheduledAt time.Time, recipient, recipientIdentifier, text string) (Message, error) {
	if scheduledAt.IsZero() {
		return Message{}, fmt.Errorf("%w: scheduledAt empty", errbrick.ErrInvalidData)
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return Message{}, fmt.Errorf("%w: recipient empty", errbrick.ErrInvalidData)
	}
	recipientIdentifier = strings.TrimSpace(recipientIdentifier)
	if recipientIdentifier == "" {
		return Message{}, fmt.Errorf("%w: recipientIdentifier empty", errbrick.ErrInvalidData)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, fmt.Errorf("%w: text empty", errbrick.ErrInvalidData)
	}

	now := time.Now().UTC()

	return Message{
		ID:                  id,
		CreatedAt:           now,
		UpdatedAt:           now,
		ScheduledAt:         scheduledAt.UTC(),
		Recipient:           recipient,
		RecipientIdentifier: recipientIdentifier,
		Text:                text,
		Status:              MessageStatusPending,
	}, nil
}

func (m Message) Cancel() (Message, error) {
	if m.Status != MessageStatusPending {
		return Message{}, fmt.Errorf("%w: outbound message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if !m.ScheduledAt.UTC().After(time.Now().UTC()) {
		return Message{}, fmt.Errorf("%w: outbound message %d is already due", errbrick.ErrConflict, m.ID)
	}

	m.Status = MessageStatusCancelled
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) IsUpcoming() bool {
	return m.Status == MessageStatusPending && m.ScheduledAt.UTC().After(time.Now().UTC())
}
