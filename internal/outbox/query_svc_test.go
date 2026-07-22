package outbox

import (
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/command"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

type queryFixture struct {
	db      *bolt.DB
	create  *command.CreateMessage
	cancel  *command.CancelMessage
	queries *QueryService
}

func TestQueryService_LoadUpcomingMessages(t *testing.T) {
	fixture := newQueryFixture(t)
	firstScheduledAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	secondScheduledAt := firstScheduledAt.Add(time.Hour)

	first := createQueryMessage(t, fixture, secondScheduledAt, "second-by-time")
	second := createQueryMessage(t, fixture, firstScheduledAt, "first-by-time")
	createQueryMessage(t, fixture, time.Now().UTC().Add(-time.Minute), "past")

	messages, err := fixture.queries.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Equal(t, []uint64{second.ID, first.ID}, queryMessageIDs(messages))
	require.True(t, firstScheduledAt.Equal(messages[0].ScheduledAt))
	require.True(t, secondScheduledAt.Equal(messages[1].ScheduledAt))
}

func TestQueryService_LoadUpcomingMessages_ExcludesCancelledMessages(t *testing.T) {
	fixture := newQueryFixture(t)
	cancelled := createQueryMessage(t, fixture, time.Now().UTC().Add(2*time.Hour), "Cancelled")
	active := createQueryMessage(t, fixture, time.Now().UTC().Add(3*time.Hour), "Active")
	_, err := fixture.cancel.Exec(t.Context(), cancelled.ID)
	require.NoError(t, err)

	messages, err := fixture.queries.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Equal(t, []uint64{active.ID}, queryMessageIDs(messages))
}

func TestQueryService_LoadHistoryMessages(t *testing.T) {
	fixture := newQueryFixture(t)
	updatedAt := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	type testMessage struct {
		updatedAt time.Time
		status    domain.MessageStatus
		lastError string
		attempt   uint16
	}
	testMessages := []testMessage{
		{status: domain.MessageStatusPending, updatedAt: updatedAt.Add(-2 * time.Minute)},
		{status: domain.MessageStatusRetry, updatedAt: updatedAt.Add(-time.Minute), attempt: 1, lastError: "temporary"},
		{status: domain.MessageStatusSent, updatedAt: updatedAt.Add(-2 * time.Minute), attempt: 1},
		{status: domain.MessageStatusFailed, updatedAt: updatedAt, attempt: 5, lastError: "permanent"},
		{status: domain.MessageStatusCancelled, updatedAt: updatedAt.Add(-3 * time.Minute)},
	}

	created := make([]domain.Message, 0, len(testMessages))
	for _, testMessage := range testMessages {
		msg := createQueryMessage(t, fixture, updatedAt.Add(time.Hour), string(testMessage.status))
		require.NoError(t, updateQueryStoredMessage(t, fixture.db, msg.ID, func(stored domain.Message) domain.Message {
			stored.Status = testMessage.status
			stored.UpdatedAt = testMessage.updatedAt
			stored.Attempt = testMessage.attempt
			stored.LastError = testMessage.lastError
			return stored
		}))
		created = append(created, msg)
	}

	messages, err := fixture.queries.LoadHistoryMessages(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, []uint64{created[3].ID, created[1].ID, created[2].ID, created[0].ID, created[4].ID}, queryMessageIDs(messages))

	limited, err := fixture.queries.LoadHistoryMessages(t.Context(), 3)
	require.NoError(t, err)
	require.Equal(t, []uint64{created[3].ID, created[1].ID, created[2].ID}, queryMessageIDs(limited))

	_, err = fixture.queries.LoadHistoryMessages(t.Context(), 0)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
}

func newQueryFixture(t *testing.T) *queryFixture {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket(outboxMessagesBucket)
		return err
	}))

	reader := dbadapter.NewDBMessageReader(outboxMessagesBucket, db)
	writer := dbadapter.NewDBMessageWriter(outboxMessagesBucket, db)
	return &queryFixture{
		db:      db,
		create:  command.NewCreateMessage(5, writer),
		cancel:  command.NewCancelMesssage(writer),
		queries: NewQueryService(reader),
	}
}

func createQueryMessage(t *testing.T, fixture *queryFixture, scheduledAt time.Time, recipient string) domain.Message {
	t.Helper()

	message, err := fixture.create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         scheduledAt,
		Recipient:           recipient,
		RecipientIdentifier: recipient + "-id",
		Text:                recipient + " text",
	})
	require.NoError(t, err)

	return message
}

func updateQueryStoredMessage(t *testing.T, db *bolt.DB, id uint64, update func(domain.Message) domain.Message) error {
	t.Helper()

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		if bucket == nil {
			return errbrick.ErrNotFound
		}
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		value := bucket.Get(key)
		if value == nil {
			return errbrick.ErrNotFound
		}

		var message domain.Message
		if err := json.Unmarshal(value, &message); err != nil {
			return err
		}
		data, err := json.Marshal(update(message))
		if err != nil {
			return err
		}

		return bucket.Put(key, data)
	})
}

func queryMessageIDs(messages []domain.Message) []uint64 {
	ids := make([]uint64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}

	return ids
}
