package command

import (
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/stretchr/testify/require"
)

func TestCreateMessage_Exec_InitializesDeliveryState(t *testing.T) {
	fixture := newCommandFixture(t)

	created := createTestMessage(t, fixture, time.Now().UTC().Add(time.Hour), "Future", "future-id", "future text")

	require.Equal(t, domain.MessageStatusPending, created.Status)
	require.Zero(t, created.Attempt)
	require.EqualValues(t, 5, created.MaxAttempts)
	require.Empty(t, created.LastError)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, created, stored)
}
