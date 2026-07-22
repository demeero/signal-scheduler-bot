package command

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/stretchr/testify/require"
)

func TestSendDueMessages_Exec_MarksMessageSent(t *testing.T) {
	fixture := newCommandFixture(t)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Alice", "+380501112233", "hello")

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusSent, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)
	require.Empty(t, stored.LastError)

	requests := fixture.requests()
	require.Len(t, requests, 1)
	require.Equal(t, "+380501112233", requests[0].Recipients[0])
	require.Equal(t, "hello", requests[0].Message)
}

func TestSendDueMessages_Exec_MarksMessageRetry(t *testing.T) {
	fixture := newCommandFixture(t, testSendResponse{statusCode: http.StatusServiceUnavailable, body: "temporarily unavailable"})
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Alice", "+380501112233", "hello")

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusRetry, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)
	require.Contains(t, stored.LastError, "temporarily unavailable")
}

func TestSendDueMessages_Exec_MarksMessageFailedAtMaxAttempt(t *testing.T) {
	fixture := newCommandFixture(t,
		testSendResponse{statusCode: http.StatusServiceUnavailable, body: "still unavailable"},
		testSendResponse{statusCode: http.StatusCreated},
	)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Alice", "+380501112233", "hello")
	require.NoError(t, updateStoredMessage(t, fixture.db, created.ID, func(msg domain.Message) domain.Message {
		msg.Status = domain.MessageStatusRetry
		msg.Attempt = 4
		msg.LastError = "previous failure"
		return msg
	}))

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusFailed, stored.Status)
	require.EqualValues(t, 5, stored.Attempt)
	require.Contains(t, stored.LastError, "still unavailable")

	requests := fixture.requests()
	require.Len(t, requests, 2)
	require.Equal(t, "+380501112233", requests[0].Recipients[0])
	require.Equal(t, "+380500000000", requests[1].Recipients[0])
	require.Contains(t, requests[1].Message, "Failed to deliver scheduled message")
	require.Contains(t, requests[1].Message, "still unavailable")
}

func TestSendDueMessages_Exec_FailsExpiredMessageWithoutSending(t *testing.T) {
	fixture := newCommandFixtureWithMaxAge(t, 15*time.Minute)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-16*time.Minute), "Alice", "+380501112233", "hello")

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusFailed, stored.Status)
	require.Zero(t, stored.Attempt)
	require.Contains(t, stored.LastError, "message expired before send")

	requests := fixture.requests()
	require.Len(t, requests, 1)
	require.Equal(t, "+380500000000", requests[0].Recipients[0])
	require.Contains(t, requests[0].Message, "Failed to deliver scheduled message")
}

func TestSendDueMessages_Exec_SendsFreshOverdueMessage(t *testing.T) {
	fixture := newCommandFixtureWithMaxAge(t, 15*time.Minute)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-10*time.Minute), "Alice", "+380501112233", "hello")

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusSent, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)
	require.Len(t, fixture.requests(), 1)
}

func TestSendDueMessages_Exec_SkipsNonDueStates(t *testing.T) {
	fixture := newCommandFixture(t)
	future := createTestMessage(t, fixture, time.Now().UTC().Add(time.Hour), "Future", "future-id", "future text")
	cancelled := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Cancelled", "cancelled-id", "cancelled text")
	sent := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Sent", "sent-id", "sent text")
	failed := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Failed", "failed-id", "failed text")
	due := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Due", "due-id", "due text")

	for _, update := range []struct {
		apply func(domain.Message) domain.Message
		id    uint64
	}{
		{id: cancelled.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusCancelled
			return msg
		}},
		{id: sent.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusSent
			msg.Attempt = 1
			return msg
		}},
		{id: failed.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusFailed
			msg.Attempt = msg.MaxAttempts
			msg.LastError = "boom"
			return msg
		}},
	} {
		require.NoError(t, updateStoredMessage(t, fixture.db, update.id, update.apply))
	}

	require.NoError(t, fixture.sendDue.Exec(t.Context()))
	requests := fixture.requests()
	require.Len(t, requests, 1)
	require.Equal(t, "due text", requests[0].Message)

	dueStored, err := loadMessageByID(t, fixture.db, due.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusSent, dueStored.Status)
	futureStored, err := loadMessageByID(t, fixture.db, future.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusPending, futureStored.Status)
	require.Zero(t, futureStored.Attempt)
}

func TestSendDueMessages_Exec_ContinuesBatchAfterSendFailure(t *testing.T) {
	fixture := newCommandFixture(t,
		testSendResponse{statusCode: http.StatusServiceUnavailable, body: "first failed"},
		testSendResponse{statusCode: http.StatusCreated},
	)
	first := createTestMessage(t, fixture, time.Now().UTC().Add(-2*time.Minute), "First", "first-id", "first text")
	second := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Second", "second-id", "second text")

	require.NoError(t, fixture.sendDue.Exec(t.Context()))

	firstStored, err := loadMessageByID(t, fixture.db, first.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusRetry, firstStored.Status)
	secondStored, err := loadMessageByID(t, fixture.db, second.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusSent, secondStored.Status)
	require.Len(t, fixture.requests(), 2)
}

func TestSendDueMessages_Exec_RollsBackAttemptOnContextCancel(t *testing.T) {
	fixture := newCancellableSendFixture(t)
	created, err := fixture.create.Exec(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- fixture.sendDue.Exec(ctx) }()
	<-fixture.requestStarted
	cancel()

	require.ErrorIs(t, <-errCh, context.Canceled)
	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, created, stored)
}
