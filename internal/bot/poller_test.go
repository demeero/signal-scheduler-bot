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
	"github.com/demeero/signal-scheduler-bot/internal/outbox/command"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestPoller_Poll_UpcomingNoUpcomingMessages(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	_, err = fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "+380500000000",
		RecipientIdentifier: "+380500000000",
		Text:                "already due",
	})
	require.NoError(t, err)

	poller := newTestPoller(t, fixture.outbox, location, []string{"/upcoming"}, nil)

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

	later, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         now.Add(3 * time.Hour),
		Recipient:           "Later",
		RecipientIdentifier: "later-id",
		Text:                "later text",
	})
	require.NoError(t, err)

	earlier, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         now.Add(90 * time.Minute),
		Recipient:           "Earlier",
		RecipientIdentifier: "earlier-id",
		Text:                "earlier text",
	})
	require.NoError(t, err)

	sameTime, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         earlier.ScheduledAt,
		Recipient:           "SameTime",
		RecipientIdentifier: "same-time-id",
		Text:                "same time text",
	})
	require.NoError(t, err)

	_, err = fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         now.Add(-5 * time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	poller := newTestPoller(t, fixture.outbox, location, []string{"/upcoming"}, nil)

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
	_, err = fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-30 * time.Second),
		Recipient:           "Overdue",
		RecipientIdentifier: "overdue-id",
		Text:                "overdue text",
	})
	require.NoError(t, err)

	poller := newTestPoller(t, fixture.outbox, location, []string{"/upcoming"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	upcoming, err := fixture.outbox.Queries.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Empty(t, upcoming)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	reply := messages[len(messages)-1]
	require.Equal(t, "Upcoming messages: 0", reply.Text)
}

func TestPoller_Poll_HistoryListsDeliveryDetails(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	updatedAt := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)

	failed, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         updatedAt.Add(-time.Hour),
		Recipient:           "Failed recipient",
		RecipientIdentifier: "failed-id",
		Text:                "failed\ntext",
	})
	require.NoError(t, err)
	err = updateStoredMessage(t, fixture.db, failed.ID, func(msg domain.Message) domain.Message {
		msg.Status = domain.MessageStatusFailed
		msg.Attempt = msg.MaxAttempts
		msg.LastError = "delivery \"failed\"\npermanently"
		msg.UpdatedAt = updatedAt
		return msg
	})
	require.NoError(t, err)

	retry, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         updatedAt.Add(-2 * time.Hour),
		Recipient:           "Retry recipient",
		RecipientIdentifier: "retry-id",
		Text:                "retry text",
	})
	require.NoError(t, err)
	err = updateStoredMessage(t, fixture.db, retry.ID, func(msg domain.Message) domain.Message {
		msg.Status = domain.MessageStatusRetry
		msg.Attempt = 2
		msg.LastError = "temporary failure"
		msg.UpdatedAt = updatedAt.Add(-time.Minute)
		return msg
	})
	require.NoError(t, err)

	poller := newTestPoller(t, fixture.outbox, location, []string{"/history 2"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	reply := messages[len(messages)-1]
	require.Equal(t, testAccount, reply.Recipient)
	require.Equal(t, strings.Join([]string{
		"History: 2",
		strconv.FormatUint(failed.ID, 10) +
			" | status: failed | scheduled: " + failed.ScheduledAt.In(location).Format("2006-01-02 15:04:05") + " (" + location.String() + ")" +
			" | updated: " + updatedAt.In(location).Format("2006-01-02 15:04:05") + " (" + location.String() + ")" +
			" | recipient: \"Failed recipient\" | attempts: 5/5 | last error: \"delivery \\\"failed\\\"\\npermanently\" | text: \"failed\\ntext\"",
		strconv.FormatUint(retry.ID, 10) +
			" | status: retry | scheduled: " + retry.ScheduledAt.In(location).Format("2006-01-02 15:04:05") + " (" + location.String() + ")" +
			" | updated: " + updatedAt.Add(-time.Minute).In(location).Format("2006-01-02 15:04:05") + " (" + location.String() + ")" +
			" | recipient: \"Retry recipient\" | attempts: 2/5 | last error: \"temporary failure\" | text: \"retry text\"",
	}, "\n"), reply.Text)
}

func TestPoller_Poll_CancelCmd(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	created, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	poller := newTestPoller(
		t,
		fixture.outbox,
		location,
		[]string{"/cancel " + strconv.FormatUint(created.ID, 10)},
		nil,
	)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	stored, err := loadStoredMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusCancelled, stored.Status)

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
	poller := newTestPoller(t, fixture.outbox, location, []string{"/cancel 42"}, nil)

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
	created, err := fixture.outbox.Commands.Create.Exec(t.Context(), command.CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	poller := newTestPoller(
		t,
		fixture.outbox,
		location,
		[]string{"/cancel " + strconv.FormatUint(created.ID, 10)},
		nil,
	)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	stored, err := loadStoredMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MessageStatusPending, stored.Status)

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
		fixture.outbox,
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

func TestPoller_Poll_HelpCmd(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	fixture := newTestOutboxFixture(t)
	poller := newTestPoller(t, fixture.outbox, location, []string{"/help"}, nil)

	err = poller.Poll(t.Context())
	require.NoError(t, err)

	messages, err := loadAllMessages(t, fixture.db)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, testAccount, messages[0].Recipient)
	require.Contains(t, messages[0].Text, "Available commands:")
	require.Contains(t, messages[0].Text, "/cancel MESSAGE_ID")
}

func TestCommandName(t *testing.T) {
	require.Equal(t, "help", commandName(helpCommand{}))
	require.Equal(t, "upcoming", commandName(upcomingCommand{}))
	require.Equal(t, "history", commandName(historyCommand{}))
	require.Equal(t, "cancel", commandName(cancelCommand{}))
	require.Equal(t, "schedule", commandName(scheduleCommand{}))
	require.Equal(t, "unknown", commandName(testUnknownCommand{}))
}

type testOutboxFixture struct {
	db     *bolt.DB
	outbox *outbox.Outbox
}

const testAccount = "+380999999999"

type testUnknownCommand struct{}

func (testUnknownCommand) isCommand() {}

func newTestOutboxFixture(t *testing.T) testOutboxFixture {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	signalClient := signaladapter.New(testAccount, "http://signal.test", &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: got %s want %s", r.Method, http.MethodPost)
				http.Error(recorder, "unexpected method", http.StatusMethodNotAllowed)
				return recorder.Result(), nil
			}
			if r.URL.Path != "/v2/send" {
				t.Errorf("unexpected path: got %s want %s", r.URL.Path, "/v2/send")
				http.Error(recorder, "unexpected path", http.StatusNotFound)
				return recorder.Result(), nil
			}
			recorder.WriteHeader(http.StatusCreated)
			return recorder.Result(), nil
		}),
	})

	box, err := outbox.New(5, 15*time.Minute, 30*24*time.Hour, db, signalClient)
	require.NoError(t, err)

	return testOutboxFixture{
		db:     db,
		outbox: box,
	}
}

func newTestPoller(
	t *testing.T,
	box *outbox.Outbox,
	location *time.Location,
	receiveBodies []string,
	contacts []map[string]string,
) *Poller {
	t.Helper()

	signalClient := signaladapter.New(testAccount, "http://signal.test", &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/v1/receive/"+testAccount:
				query := r.URL.Query()
				if query.Get("ignore_attachments") != "true" ||
					query.Get("ignore_stories") != "true" ||
					query.Get("ignore_avatars") != "true" ||
					query.Get("ignore_stickers") != "true" ||
					query.Get("send_read_receipts") != "false" {
					t.Errorf("unexpected receive query: %s", r.URL.RawQuery)
					http.Error(recorder, "unexpected query", http.StatusBadRequest)
					return recorder.Result(), nil
				}

				writeReceiveMessagesJSON(t, recorder, testAccount, receiveBodies)
			case r.Method == http.MethodGet && r.URL.Path == "/v1/contacts/"+testAccount:
				if err := json.NewEncoder(recorder).Encode(contacts); err != nil {
					t.Errorf("failed to encode contacts response: %v", err)
				}
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
				http.Error(recorder, "unexpected request", http.StatusNotFound)
			}

			return recorder.Result(), nil
		}),
	})
	return New(testAccount, location, signalClient, box.Queries, box.Commands.Create, box.Commands.Cancel)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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

func formatUpcomingLine(location *time.Location, msg domain.Message) string {
	return strings.Join([]string{
		strconv.FormatUint(msg.ID, 10),
		msg.ScheduledAt.In(location).Format("2006-01-02 15:04") + " (" + location.String() + ")",
		msg.Recipient,
		msg.Text,
	}, " | ")
}

func loadAllMessages(t *testing.T, db *bolt.DB) ([]domain.Message, error) {
	t.Helper()

	messages := make([]domain.Message, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbox_messages"))
		require.NotNil(t, bucket)

		return bucket.ForEach(func(_, value []byte) error {
			var msg domain.Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return err
			}

			messages = append(messages, msg)
			return nil
		})
	})

	return messages, err
}

func loadStoredMessageByID(t *testing.T, db *bolt.DB, id uint64) (domain.Message, error) {
	t.Helper()

	var msg domain.Message
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

func updateStoredMessage(t *testing.T, db *bolt.DB, id uint64, fn func(domain.Message) domain.Message) error {
	t.Helper()

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("outbox_messages"))
		require.NotNil(t, bucket)

		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)

		value := bucket.Get(key)
		if value == nil {
			return errbrick.ErrNotFound
		}

		var msg domain.Message
		if err := json.Unmarshal(value, &msg); err != nil {
			return err
		}

		data, err := json.Marshal(fn(msg))
		if err != nil {
			return err
		}

		return bucket.Put(key, data)
	})
}
