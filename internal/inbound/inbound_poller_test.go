package inbound

import (
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbound"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestPollerHandleUpcomingCmdNoUpcomingMessages(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	_, err = fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "+380500000000",
		RecipientIdentifier: "+380500000000",
		Text:                "already due",
	})
	require.NoError(t, err)

	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleUpcomingCmd(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Upcoming messages: 0", reply.Text)
	require.Equal(t, "+380999999999", reply.Recipient)
}

func TestPollerHandleUpcomingCmdListsFuturePendingMessages(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	now := time.Now().UTC()

	later, err := fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         now.Add(3 * time.Hour),
		Recipient:           "Later",
		RecipientIdentifier: "later-id",
		Text:                "later text",
	})
	require.NoError(t, err)

	earlier, err := fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         now.Add(90 * time.Minute),
		Recipient:           "Earlier",
		RecipientIdentifier: "earlier-id",
		Text:                "earlier text",
	})
	require.NoError(t, err)

	sameTime, err := fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         earlier.ScheduledAt,
		Recipient:           "SameTime",
		RecipientIdentifier: "same-time-id",
		Text:                "same time text",
	})
	require.NoError(t, err)

	_, err = fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         now.Add(-5 * time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleUpcomingCmd(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 5)

	reply := messages[len(messages)-1]
	require.Equal(t, "+380999999999", reply.Recipient)
	require.Equal(t, strings.Join([]string{
		"Upcoming messages: 3",
		formatUpcomingLine(location, earlier),
		formatUpcomingLine(location, sameTime),
		formatUpcomingLine(location, later),
	}, "\n"), reply.Text)
}

func TestPollerHandleUpcomingCmdHidesOverduePendingMessages(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	_, err = fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-30 * time.Second),
		Recipient:           "Overdue",
		RecipientIdentifier: "overdue-id",
		Text:                "overdue text",
	})
	require.NoError(t, err)

	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleUpcomingCmd(t.Context())
	require.NoError(t, err)

	upcoming, err := fixture.service.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Empty(t, upcoming)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Upcoming messages: 0", reply.Text)
}

func TestPollerHandleCancelCmd(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	created, err := fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleCancelCmd(t.Context(), cancelCommand{id: created.ID})
	require.NoError(t, err)

	stored, err := loadStoredMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, outbound.MessageStatusCancelled, stored.Status)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Cancelled message "+strconv.FormatUint(created.ID, 10)+".", reply.Text)
	require.Equal(t, "+380999999999", reply.Recipient)
}

func TestPollerHandleCancelCmdReturnsNotFound(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleCancelCmd(t.Context(), cancelCommand{id: 42})
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrNotFound)
	require.EqualError(t, err, "message 42 not found")
}

func TestPollerHandleCancelCmdReturnsConflict(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboundFixture(t)
	created, err := fixture.service.CreateMessage(t.Context(), outbound.CreateOutboundMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleCancelCmd(t.Context(), cancelCommand{id: created.ID})
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
	require.EqualError(t, err, "message "+strconv.FormatUint(created.ID, 10)+" cannot be cancelled")
}

type testOutboundFixture struct {
	db      *bolt.DB
	service *outbound.Service
}

func newTestOutboundFixture(t *testing.T) testOutboundFixture {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	service, err := outbound.New(db)
	require.NoError(t, err)

	return testOutboundFixture{
		db:      db,
		service: service,
	}
}

func formatUpcomingLine(location *time.Location, msg outbound.Message) string {
	return strings.Join([]string{
		strconv.FormatUint(msg.ID, 10),
		msg.ScheduledAt.In(location).Format("2006-01-02 15:04") + " (" + location.String() + ")",
		msg.Recipient,
		msg.Text,
	}, " | ")
}

func loadAllMessages(t *testing.T, db *bolt.DB) ([]outbound.Message, error) {
	t.Helper()

	messages := make([]outbound.Message, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbound_messages"))
		require.NotNil(t, bucket)

		return bucket.ForEach(func(_, value []byte) error {
			var msg outbound.Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return err
			}

			messages = append(messages, msg)
			return nil
		})
	})

	return messages, err
}

func loadStoredMessageByID(t *testing.T, db *bolt.DB, id uint64) (outbound.Message, error) {
	t.Helper()

	var msg outbound.Message
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbound_messages"))
		require.NotNil(t, bucket)

		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)

		value := bucket.Get(key)
		if value == nil {
			return errbrick.ErrNotFound
		}

		return json.Unmarshal(value, &msg)
	})

	return msg, err
}
