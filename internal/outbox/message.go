package outbox

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
)

type MessageStatus string

const (
	MessageStatusPending   MessageStatus = "pending"
	MessageStatusRetry     MessageStatus = "retry"
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
	LastError           string        `json:"last_error"`
	Status              MessageStatus `json:"status"`
	Attempt             uint16        `json:"attempt"`
	MaxAttempts         uint16        `json:"max_attempts"`
	ID                  uint64        `json:"id"`
}

func newMessage(id uint64, scheduledAt time.Time, recipient, recipientIdentifier, text string, maxAttempts uint16) (Message, error) {
	if scheduledAt.IsZero() {
		return Message{}, fmt.Errorf("%w: scheduledAt empty", errbrick.ErrInvalidData)
	}
	if maxAttempts == 0 {
		return Message{}, fmt.Errorf("%w: maxAttempts empty", errbrick.ErrInvalidData)
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
		MaxAttempts:         maxAttempts,
	}, nil
}

func (m Message) Cancel() (Message, error) {
	if m.Status != MessageStatusPending {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if !m.ScheduledAt.UTC().After(time.Now().UTC()) {
		return Message{}, fmt.Errorf("%w: outbox message %d is already due", errbrick.ErrConflict, m.ID)
	}

	m.Status = MessageStatusCancelled
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) StartSendAttempt() (Message, error) {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if m.Attempt >= m.MaxAttempts {
		return Message{}, fmt.Errorf("%w: outbox message %d reached max attempts", errbrick.ErrConflict, m.ID)
	}

	m.Attempt++
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) MarkSent() (Message, error) {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if m.Attempt == 0 {
		return Message{}, fmt.Errorf("%w: outbox message %d has no send attempts", errbrick.ErrConflict, m.ID)
	}
	if m.Attempt > m.MaxAttempts {
		return Message{}, fmt.Errorf("%w: outbox message %d exceeded max attempts", errbrick.ErrConflict, m.ID)
	}

	m.Status = MessageStatusSent
	m.LastError = ""
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) MarkRetry(lastErr string) (Message, error) {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if m.Attempt == 0 {
		return Message{}, fmt.Errorf("%w: outbox message %d has no send attempts", errbrick.ErrConflict, m.ID)
	}
	if m.Attempt >= m.MaxAttempts {
		return Message{}, fmt.Errorf("%w: outbox message %d reached max attempts", errbrick.ErrConflict, m.ID)
	}

	lastErr = strings.TrimSpace(lastErr)
	if lastErr == "" {
		return Message{}, fmt.Errorf("%w: outbox message %d last error empty", errbrick.ErrInvalidData, m.ID)
	}

	m.Status = MessageStatusRetry
	m.LastError = lastErr
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) MarkFailed(lastErr string) (Message, error) {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if m.Attempt == 0 {
		return Message{}, fmt.Errorf("%w: outbox message %d has no send attempts", errbrick.ErrConflict, m.ID)
	}
	if m.Attempt < m.MaxAttempts {
		return Message{}, fmt.Errorf("%w: outbox message %d can still retry", errbrick.ErrConflict, m.ID)
	}

	lastErr = strings.TrimSpace(lastErr)
	if lastErr == "" {
		return Message{}, fmt.Errorf("%w: outbox message %d last error empty", errbrick.ErrInvalidData, m.ID)
	}

	m.Status = MessageStatusFailed
	m.LastError = lastErr
	m.UpdatedAt = time.Now().UTC()
	return m, nil
}

func (m Message) IsUpcoming() bool {
	return m.Status == MessageStatusPending && m.ScheduledAt.UTC().After(time.Now().UTC())
}

func (m Message) IsDue(now time.Time) bool {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return false
	}

	return !m.ScheduledAt.UTC().After(now.UTC())
}

func (m Message) IsExpired(now time.Time, maxAge time.Duration) bool {
	if !m.IsDue(now) {
		return false
	}

	return now.UTC().Sub(m.ScheduledAt.UTC()) > maxAge
}

func (m Message) IsTerminal() bool {
	switch m.Status {
	case MessageStatusSent, MessageStatusFailed, MessageStatusCancelled:
		return true
	case MessageStatusPending, MessageStatusRetry:
		return false
	}

	return false
}

func (m Message) MarkExpired(now time.Time, maxAge time.Duration) (Message, error) {
	if m.Status != MessageStatusPending && m.Status != MessageStatusRetry {
		return Message{}, fmt.Errorf("%w: outbox message %d status is %s", errbrick.ErrConflict, m.ID, m.Status)
	}
	if maxAge <= 0 {
		return Message{}, fmt.Errorf("%w: maxAge must be positive", errbrick.ErrInvalidData)
	}
	if !m.IsExpired(now, maxAge) {
		return Message{}, fmt.Errorf("%w: outbox message %d is not expired", errbrick.ErrConflict, m.ID)
	}

	m.Status = MessageStatusFailed
	m.LastError = fmt.Sprintf("message expired before send: scheduled at %s exceeded max age %s", m.ScheduledAt.UTC().Format(time.RFC3339), maxAge)
	m.UpdatedAt = now.UTC()
	return m, nil
}

func (m Message) key() []byte {
	return outboxMessageKey(m.ID)
}

func outboxMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}
