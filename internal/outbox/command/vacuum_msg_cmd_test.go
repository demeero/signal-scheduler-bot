package command

import (
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/stretchr/testify/require"
)

func TestVacuumMessages_Exec_DeletesOnlyOldTerminalMessages(t *testing.T) {
	fixture := newCommandFixtureWithConfig(t, 15*time.Minute, 30*24*time.Hour)
	now := time.Now().UTC()

	oldSent := createTestMessage(t, fixture, now.Add(-48*time.Hour), "Sent", "sent-id", "sent text")
	oldFailed := createTestMessage(t, fixture, now.Add(-48*time.Hour), "Failed", "failed-id", "failed text")
	oldCancelled := createTestMessage(t, fixture, now.Add(48*time.Hour), "Cancelled", "cancelled-id", "cancelled text")
	recentSent := createTestMessage(t, fixture, now.Add(-time.Hour), "Recent sent", "recent-sent-id", "recent sent text")
	oldPending := createTestMessage(t, fixture, now.Add(-48*time.Hour), "Pending", "pending-id", "pending text")
	oldRetry := createTestMessage(t, fixture, now.Add(-48*time.Hour), "Retry", "retry-id", "retry text")
	cutoff := now.Add(-31 * 24 * time.Hour)

	for _, update := range []struct {
		apply func(domain.Message) domain.Message
		id    uint64
	}{
		{id: oldSent.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusSent
			msg.Attempt = 1
			msg.UpdatedAt = cutoff.Add(-time.Hour)
			return msg
		}},
		{id: oldFailed.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusFailed
			msg.Attempt = msg.MaxAttempts
			msg.LastError = "boom"
			msg.UpdatedAt = cutoff.Add(-2 * time.Hour)
			return msg
		}},
		{id: oldCancelled.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusCancelled
			msg.UpdatedAt = cutoff.Add(-3 * time.Hour)
			return msg
		}},
		{id: recentSent.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusSent
			msg.Attempt = 1
			msg.UpdatedAt = cutoff.Add(7 * 24 * time.Hour)
			return msg
		}},
		{id: oldRetry.ID, apply: func(msg domain.Message) domain.Message {
			msg.Status = domain.MessageStatusRetry
			msg.Attempt = 1
			msg.LastError = "temporary"
			msg.UpdatedAt = cutoff.Add(-4 * time.Hour)
			return msg
		}},
	} {
		require.NoError(t, updateStoredMessage(t, fixture.db, update.id, update.apply))
	}

	require.NoError(t, fixture.vacuum.Exec(t.Context()))

	for _, id := range []uint64{oldSent.ID, oldFailed.ID, oldCancelled.ID} {
		_, err := loadMessageByID(t, fixture.db, id)
		require.ErrorIs(t, err, errbrick.ErrNotFound)
	}
	for _, id := range []uint64{recentSent.ID, oldPending.ID, oldRetry.ID} {
		_, err := loadMessageByID(t, fixture.db, id)
		require.NoError(t, err)
	}
}
