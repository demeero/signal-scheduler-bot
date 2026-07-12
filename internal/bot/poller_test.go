package bot

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestPollerHandleUpcomingCmdNoUpcomingMessages(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	_, err = fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
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

	fixture := newTestOutboxFixture(t)
	now := time.Now().UTC()

	later, err := fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
		ScheduledAt:         now.Add(3 * time.Hour),
		Recipient:           "Later",
		RecipientIdentifier: "later-id",
		Text:                "later text",
	})
	require.NoError(t, err)

	earlier, err := fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
		ScheduledAt:         now.Add(90 * time.Minute),
		Recipient:           "Earlier",
		RecipientIdentifier: "earlier-id",
		Text:                "earlier text",
	})
	require.NoError(t, err)

	sameTime, err := fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
		ScheduledAt:         earlier.ScheduledAt,
		Recipient:           "SameTime",
		RecipientIdentifier: "same-time-id",
		Text:                "same time text",
	})
	require.NoError(t, err)

	_, err = fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
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

	fixture := newTestOutboxFixture(t)
	_, err = fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
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

	fixture := newTestOutboxFixture(t)
	created, err := fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
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
	require.Equal(t, outbox.MessageStatusCancelled, stored.Status)

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

	fixture := newTestOutboxFixture(t)
	poller := New("+380999999999", location, nil, fixture.service)

	err = poller.handleCancelCmd(t.Context(), cancelCommand{id: 42})
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrNotFound)
	require.ErrorContains(t, err, "failed cancel outbox message")
}

func TestPollerHandleCancelCmdReturnsConflict(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	created, err := fixture.service.CreateMessage(t.Context(), outbox.CreateMessageParams{
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
	require.ErrorContains(t, err, "failed cancel outbox message")
}

type testOutboxFixture struct {
	db      *bolt.DB
	service *outbox.Service
}

func newTestOutboxFixture(t *testing.T) testOutboxFixture {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: got %s want %s", r.Method, http.MethodPost)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v2/send" {
			t.Errorf("unexpected path: got %s want %s", r.URL.Path, "/v2/send")
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	signalClient := signaladapter.New("+380999999999", server.URL, &http.Client{Timeout: time.Second})

	service, err := outbox.New(5, db, signalClient)
	require.NoError(t, err)

	return testOutboxFixture{
		db:      db,
		service: service,
	}
}

func formatUpcomingLine(location *time.Location, msg outbox.Message) string {
	return strings.Join([]string{
		strconv.FormatUint(msg.ID, 10),
		msg.ScheduledAt.In(location).Format("2006-01-02 15:04") + " (" + location.String() + ")",
		msg.Recipient,
		msg.Text,
	}, " | ")
}

func loadAllMessages(t *testing.T, db *bolt.DB) ([]outbox.Message, error) {
	t.Helper()

	messages := make([]outbox.Message, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbox_messages"))
		require.NotNil(t, bucket)

		return bucket.ForEach(func(_, value []byte) error {
			var msg outbox.Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return err
			}

			messages = append(messages, msg)
			return nil
		})
	})

	return messages, err
}

func loadStoredMessageByID(t *testing.T, db *bolt.DB, id uint64) (outbox.Message, error) {
	t.Helper()

	var msg outbox.Message
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbox_messages"))
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
