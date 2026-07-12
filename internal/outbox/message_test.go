package outbox

import (
	"testing"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/stretchr/testify/require"
)

func TestMessage_StartSendAttempt_RejectsInvalidState(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusSent, MaxAttempts: 5}

	_, err := msg.StartSendAttempt()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessage_StartSendAttempt_RejectsExhaustedAttempts(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 5, MaxAttempts: 5}

	_, err := msg.StartSendAttempt()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessage_MarkSent_RejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusPending, Attempt: 0, MaxAttempts: 5}

	_, err := msg.MarkSent()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessage_MarkRetry_RejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 5, MaxAttempts: 5}

	_, err := msg.MarkRetry("boom")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessage_MarkRetry_RejectsEmptyError(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 1, MaxAttempts: 5}

	_, err := msg.MarkRetry("")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
}

func TestMessage_MarkFailed_RejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 1, MaxAttempts: 5}

	_, err := msg.MarkFailed("boom")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}
