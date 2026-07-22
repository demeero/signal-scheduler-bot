package command

import (
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/stretchr/testify/require"
)

func TestCancelMessage_Exec_CancelsPendingFutureMessage(t *testing.T) {
	fixture := newCommandFixture(t)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(time.Hour), "Future", "future-id", "future text")

	time.Sleep(time.Millisecond)
	cancelled, err := fixture.cancel.Exec(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, cancelled.ID)
	require.Equal(t, domain.MessageStatusCancelled, cancelled.Status)
	require.True(t, cancelled.UpdatedAt.After(created.UpdatedAt))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, stored)
}

func TestCancelMessage_Exec_ReturnsNotFound(t *testing.T) {
	fixture := newCommandFixture(t)

	_, err := fixture.cancel.Exec(t.Context(), 42)
	require.ErrorIs(t, err, errbrick.ErrNotFound)
}

func TestCancelMessage_Exec_ReturnsConflictForAlreadyCancelledMessage(t *testing.T) {
	fixture := newCommandFixture(t)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(time.Hour), "Future", "future-id", "future text")

	_, err := fixture.cancel.Exec(t.Context(), created.ID)
	require.NoError(t, err)
	_, err = fixture.cancel.Exec(t.Context(), created.ID)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestCancelMessage_Exec_ReturnsConflictForDueMessage(t *testing.T) {
	fixture := newCommandFixture(t)
	created := createTestMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "Past", "past-id", "past text")

	_, err := fixture.cancel.Exec(t.Context(), created.ID)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}
