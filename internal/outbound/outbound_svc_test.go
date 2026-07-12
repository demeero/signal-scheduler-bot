package outbound

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestServiceLoadUpcomingMessages(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	firstScheduledAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	secondScheduledAt := firstScheduledAt.Add(time.Hour)

	first, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         secondScheduledAt,
		Recipient:           "second-by-time",
		RecipientIdentifier: "second-by-time-id",
		Text:                "second by time",
	})
	require.NoError(t, err)

	second, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         firstScheduledAt,
		Recipient:           "first-by-time",
		RecipientIdentifier: "first-by-time-id",
		Text:                "first by time",
	})
	require.NoError(t, err)

	_, err = service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	messages, err := service.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Equal(t, second.ID, messages[0].ID)
	require.True(t, firstScheduledAt.Equal(messages[0].ScheduledAt))
	require.Equal(t, second.Recipient, messages[0].Recipient)
	require.Equal(t, second.RecipientIdentifier, messages[0].RecipientIdentifier)
	require.Equal(t, second.Text, messages[0].Text)
	require.Equal(t, second.Status, messages[0].Status)

	require.Equal(t, first.ID, messages[1].ID)
	require.True(t, secondScheduledAt.Equal(messages[1].ScheduledAt))
	require.Equal(t, first.Recipient, messages[1].Recipient)
	require.Equal(t, first.RecipientIdentifier, messages[1].RecipientIdentifier)
	require.Equal(t, first.Text, messages[1].Text)
	require.Equal(t, first.Status, messages[1].Status)
}

func TestServiceCancelMessage(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	created, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	time.Sleep(time.Millisecond)

	cancelled, err := service.CancelMessage(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, cancelled.ID)
	require.Equal(t, MessageStatusCancelled, cancelled.Status)
	require.True(t, cancelled.UpdatedAt.After(created.UpdatedAt))

	stored, err := loadMessageByID(t, db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusCancelled, stored.Status)
	require.True(t, stored.UpdatedAt.Equal(cancelled.UpdatedAt))
}

func TestServiceCancelMessageReturnsNotFound(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	_, err = service.CancelMessage(t.Context(), 42)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrNotFound)
}

func TestServiceCancelMessageReturnsConflictForAlreadyCancelledMessage(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	created, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	_, err = service.CancelMessage(t.Context(), created.ID)
	require.NoError(t, err)

	_, err = service.CancelMessage(t.Context(), created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestServiceCancelMessageReturnsConflictForDueMessage(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	created, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	_, err = service.CancelMessage(t.Context(), created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestServiceLoadUpcomingMessagesExcludesCancelledMessages(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := New(db)
	require.NoError(t, err)

	cancelled, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(2 * time.Hour),
		Recipient:           "Cancelled",
		RecipientIdentifier: "cancelled-id",
		Text:                "cancelled text",
	})
	require.NoError(t, err)

	active, err := service.CreateMessage(t.Context(), CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(3 * time.Hour),
		Recipient:           "Active",
		RecipientIdentifier: "active-id",
		Text:                "active text",
	})
	require.NoError(t, err)

	_, err = service.CancelMessage(t.Context(), cancelled.ID)
	require.NoError(t, err)

	messages, err := service.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, active.ID, messages[0].ID)
}

func loadMessageByID(t *testing.T, db *bolt.DB, id uint64) (Message, error) {
	t.Helper()

	var msg Message
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbound_messages"))
		require.NotNil(t, bucket)

		value := bucket.Get(outboundMessageKey(id))
		if value == nil {
			return errbrick.ErrNotFound
		}

		return json.Unmarshal(value, &msg)
	})

	return msg, err
}
