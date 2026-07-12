package bot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
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

func TestPoller_Poll_UpcomingNoUpcomingMessages(t *testing.T) {
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

	poller := newTestPoller(t, fixture.service, location, []string{"/upcoming"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Upcoming messages: 0", reply.Text)
	require.Equal(t, testAccount, reply.Recipient)
}

func TestPoller_Poll_UpcomingListsFuturePendingMessages(t *testing.T) {
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

	poller := newTestPoller(t, fixture.service, location, []string{"/upcoming"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 5)

	reply := messages[len(messages)-1]
	require.Equal(t, testAccount, reply.Recipient)
	require.Equal(t, strings.Join([]string{
		"Upcoming messages: 3",
		formatUpcomingLine(location, earlier),
		formatUpcomingLine(location, sameTime),
		formatUpcomingLine(location, later),
	}, "\n"), reply.Text)
}

func TestPoller_Poll_UpcomingHidesOverduePendingMessages(t *testing.T) {
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

	poller := newTestPoller(t, fixture.service, location, []string{"/upcoming"}, nil)

	err = poller.Poll(t.Context())
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

func TestPoller_Poll_CancelCmd(t *testing.T) {
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

	poller := newTestPoller(
		t,
		fixture.service,
		location,
		[]string{"/cancel " + strconv.FormatUint(created.ID, 10)},
		nil,
	)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	stored, err := loadStoredMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, outbox.MessageStatusCancelled, stored.Status)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Cancelled message "+strconv.FormatUint(created.ID, 10)+".", reply.Text)
	require.Equal(t, testAccount, reply.Recipient)
}

func TestPoller_Poll_CancelCmdReturnsNotFound(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	poller := newTestPoller(t, fixture.service, location, []string{"/cancel 42"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	reply := messages[0]
	require.Equal(t, testAccount, reply.Recipient)
	require.Contains(t, reply.Text, "failed cancel outbox message")
	require.Contains(t, reply.Text, "outbox message 42")
}

func TestPoller_Poll_CancelCmdReturnsConflict(t *testing.T) {
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

	poller := newTestPoller(
		t,
		fixture.service,
		location,
		[]string{"/cancel " + strconv.FormatUint(created.ID, 10)},
		nil,
	)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	stored, err := loadStoredMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, outbox.MessageStatusPending, stored.Status)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, testAccount, reply.Recipient)
	require.Contains(t, reply.Text, "failed cancel outbox message")
	require.Contains(t, reply.Text, "already due")
}

func TestPoller_Poll_ScheduleCmd(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	account := testAccount
	scheduledLocal := time.Now().In(location).AddDate(0, 0, 2)
	scheduledLocal = time.Date(
		scheduledLocal.Year(),
		scheduledLocal.Month(),
		scheduledLocal.Day(),
		15,
		30,
		0,
		0,
		location,
	)

	poller := newTestPoller(
		t,
		fixture.service,
		location,
		[]string{fmt.Sprintf(
			`/schedule %s %s "Alice Smith" Hello there`,
			scheduledLocal.Format("2006-01-02"),
			scheduledLocal.Format("15:04"),
		)},
		[]map[string]string{
			{
				"name":   "Alice Smith",
				"number": "+380501112233",
			},
		},
	)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	created := messages[0]
	require.Equal(t, scheduledLocal.UTC(), created.ScheduledAt)
	require.Equal(t, "Alice Smith", created.Recipient)
	require.Equal(t, "+380501112233", created.RecipientIdentifier)
	require.Equal(t, "Hello there", created.Text)

	reply := messages[1]
	require.Equal(t, account, reply.Recipient)
	require.Equal(
		t,
		"Scheduled message "+strconv.FormatUint(
			created.ID,
			10,
		)+" for "+scheduledLocal.Format(
			"2006-01-02 15:04",
		)+" ("+location.String()+") to Alice Smith.",
		reply.Text,
	)
}

type testOutboxFixture struct {
	db      *bolt.DB
	service *outbox.Service
}

const testAccount = "+380999999999"

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

	signalClient := signaladapter.New(testAccount, server.URL, &http.Client{Timeout: time.Second})

	service, err := outbox.New(5, 15*time.Minute, 30*24*time.Hour, db, signalClient)
	require.NoError(t, err)

	return testOutboxFixture{
		db:      db,
		service: service,
	}
}

func newTestPoller(
	t *testing.T,
	outboxSvc *outbox.Service,
	location *time.Location,
	receiveBodies []string,
	contacts []map[string]string,
) *Poller {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/receive/"+testAccount:
			query := r.URL.Query()
			if query.Get("ignore_attachments") != "true" ||
				query.Get("ignore_stories") != "true" ||
				query.Get("ignore_avatars") != "true" ||
				query.Get("ignore_stickers") != "true" ||
				query.Get("send_read_receipts") != "false" {
				t.Errorf("unexpected receive query: %s", r.URL.RawQuery)
				http.Error(w, "unexpected query", http.StatusBadRequest)
				return
			}

			writeReceiveMessagesJSON(t, w, testAccount, receiveBodies)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/contacts/"+testAccount:
			if err := json.NewEncoder(w).Encode(contacts); err != nil {
				t.Errorf("failed to encode contacts response: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	signalClient := signaladapter.New(testAccount, server.URL, &http.Client{Timeout: time.Second})
	return New(testAccount, location, signalClient, outboxSvc)
}

func writeReceiveMessagesJSON(t *testing.T, w http.ResponseWriter, account string, bodies []string) {
	t.Helper()

	payload := make([]map[string]any, 0, len(bodies))
	for i, body := range bodies {
		timestamp := int64(1_780_293_400_000 + i*1_000)
		payload = append(payload, map[string]any{
			"account": account,
			"envelope": map[string]any{
				"sourceUuid":               fmt.Sprintf("self-%d", i+1),
				"serverReceivedTimestamp":  timestamp + 1,
				"serverDeliveredTimestamp": timestamp + 2,
				"syncMessage": map[string]any{
					"sentMessage": map[string]any{
						"destinationNumber": account,
						"message":           body,
						"timestamp":         timestamp,
					},
				},
			},
		})
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("failed to encode receive response: %v", err)
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
