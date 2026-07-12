package outbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestServiceCreateMessageInitializesDeliveryState(t *testing.T) {
	fixture := newServiceFixture(t)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	require.Equal(t, MessageStatusPending, created.Status)
	require.Zero(t, created.Attempt)
	require.EqualValues(t, 5, created.MaxAttempts)
	require.Empty(t, created.LastError)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, created, stored)
}

func TestServiceLoadUpcomingMessages(t *testing.T) {
	fixture := newServiceFixture(t)

	firstScheduledAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	secondScheduledAt := firstScheduledAt.Add(time.Hour)

	first, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         secondScheduledAt,
		Recipient:           "second-by-time",
		RecipientIdentifier: "second-by-time-id",
		Text:                "second by time",
	})
	require.NoError(t, err)

	second, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         firstScheduledAt,
		Recipient:           "first-by-time",
		RecipientIdentifier: "first-by-time-id",
		Text:                "first by time",
	})
	require.NoError(t, err)

	_, err = fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	messages, err := fixture.service.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Equal(t, second.ID, messages[0].ID)
	require.True(t, firstScheduledAt.Equal(messages[0].ScheduledAt))
	require.EqualValues(t, 5, messages[0].MaxAttempts)

	require.Equal(t, first.ID, messages[1].ID)
	require.True(t, secondScheduledAt.Equal(messages[1].ScheduledAt))
	require.EqualValues(t, 5, messages[1].MaxAttempts)
}

func TestServiceCancelMessage(t *testing.T) {
	fixture := newServiceFixture(t)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	time.Sleep(time.Millisecond)

	cancelled, err := fixture.service.CancelMessage(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, cancelled.ID)
	require.Equal(t, MessageStatusCancelled, cancelled.Status)
	require.True(t, cancelled.UpdatedAt.After(created.UpdatedAt))

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusCancelled, stored.Status)
	require.True(t, stored.UpdatedAt.Equal(cancelled.UpdatedAt))
}

func TestServiceCancelMessageReturnsNotFound(t *testing.T) {
	fixture := newServiceFixture(t)

	_, err := fixture.service.CancelMessage(t.Context(), 42)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrNotFound)
}

func TestServiceCancelMessageReturnsConflictForAlreadyCancelledMessage(t *testing.T) {
	fixture := newServiceFixture(t)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	_, err = fixture.service.CancelMessage(t.Context(), created.ID)
	require.NoError(t, err)

	_, err = fixture.service.CancelMessage(t.Context(), created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestServiceCancelMessageReturnsConflictForDueMessage(t *testing.T) {
	fixture := newServiceFixture(t)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Past",
		RecipientIdentifier: "past-id",
		Text:                "past text",
	})
	require.NoError(t, err)

	_, err = fixture.service.CancelMessage(t.Context(), created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestServiceLoadUpcomingMessagesExcludesCancelledMessages(t *testing.T) {
	fixture := newServiceFixture(t)

	cancelled, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(2 * time.Hour),
		Recipient:           "Cancelled",
		RecipientIdentifier: "cancelled-id",
		Text:                "cancelled text",
	})
	require.NoError(t, err)

	active, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(3 * time.Hour),
		Recipient:           "Active",
		RecipientIdentifier: "active-id",
		Text:                "active text",
	})
	require.NoError(t, err)

	_, err = fixture.service.CancelMessage(t.Context(), cancelled.ID)
	require.NoError(t, err)

	messages, err := fixture.service.LoadUpcomingMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, active.ID, messages[0].ID)
}

func TestServiceSendDueMarksMessageSent(t *testing.T) {
	fixture := newServiceFixture(t)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusSent, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)
	require.Empty(t, stored.LastError)

	requests := fixture.requests()
	require.Len(t, requests, 1)
	require.Equal(t, "+380501112233", requests[0].Recipients[0])
	require.Equal(t, "hello", requests[0].Message)
}

func TestServiceSendDueMarksMessageRetry(t *testing.T) {
	fixture := newServiceFixture(t, testSendResponse{
		statusCode: http.StatusServiceUnavailable,
		body:       "temporarily unavailable",
	})

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusRetry, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)
	require.Contains(t, stored.LastError, "temporarily unavailable")
}

func TestServiceSendDueMarksMessageFailedAtMaxAttempt(t *testing.T) {
	fixture := newServiceFixture(t, testSendResponse{
		statusCode: http.StatusServiceUnavailable,
		body:       "still unavailable",
	})

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	err = updateStoredMessage(t, fixture.db, created.ID, func(msg Message) Message {
		msg.Status = MessageStatusRetry
		msg.Attempt = 4
		msg.LastError = "previous failure"
		return msg
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusFailed, stored.Status)
	require.EqualValues(t, 5, stored.Attempt)
	require.Contains(t, stored.LastError, "still unavailable")
}

func TestServiceSendDueFailsExpiredMessageWithoutSending(t *testing.T) {
	fixture := newServiceFixtureWithMaxAge(t, 15*time.Minute)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-16 * time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusFailed, stored.Status)
	require.Zero(t, stored.Attempt)
	require.Contains(t, stored.LastError, "message expired before send")

	require.Empty(t, fixture.requests())
}

func TestServiceSendDueSendsFreshOverdueMessage(t *testing.T) {
	fixture := newServiceFixtureWithMaxAge(t, 15*time.Minute)

	created, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-10 * time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	stored, err := loadMessageByID(t, fixture.db, created.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusSent, stored.Status)
	require.EqualValues(t, 1, stored.Attempt)

	require.Len(t, fixture.requests(), 1)
}

func TestServiceSendDueSkipsNonDueStates(t *testing.T) {
	fixture := newServiceFixture(t)

	futurePending, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(time.Hour),
		Recipient:           "Future",
		RecipientIdentifier: "future-id",
		Text:                "future text",
	})
	require.NoError(t, err)

	cancelled, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Cancelled",
		RecipientIdentifier: "cancelled-id",
		Text:                "cancelled text",
	})
	require.NoError(t, err)

	sent, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Sent",
		RecipientIdentifier: "sent-id",
		Text:                "sent text",
	})
	require.NoError(t, err)

	failed, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Failed",
		RecipientIdentifier: "failed-id",
		Text:                "failed text",
	})
	require.NoError(t, err)

	due, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Due",
		RecipientIdentifier: "due-id",
		Text:                "due text",
	})
	require.NoError(t, err)

	err = updateStoredMessage(t, fixture.db, cancelled.ID, func(msg Message) Message {
		msg.Status = MessageStatusCancelled
		return msg
	})
	require.NoError(t, err)
	err = updateStoredMessage(t, fixture.db, sent.ID, func(msg Message) Message {
		msg.Status = MessageStatusSent
		msg.Attempt = 1
		return msg
	})
	require.NoError(t, err)
	err = updateStoredMessage(t, fixture.db, failed.ID, func(msg Message) Message {
		msg.Status = MessageStatusFailed
		msg.Attempt = msg.MaxAttempts
		msg.LastError = "boom"
		return msg
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	requests := fixture.requests()
	require.Len(t, requests, 1)
	require.Equal(t, "due text", requests[0].Message)

	dueStored, err := loadMessageByID(t, fixture.db, due.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusSent, dueStored.Status)

	futureStored, err := loadMessageByID(t, fixture.db, futurePending.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusPending, futureStored.Status)
	require.Zero(t, futureStored.Attempt)
}

func TestServiceSendDueContinuesBatchAfterSendFailure(t *testing.T) {
	fixture := newServiceFixture(t,
		testSendResponse{statusCode: http.StatusServiceUnavailable, body: "first failed"},
		testSendResponse{statusCode: http.StatusCreated},
	)

	first, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-2 * time.Minute),
		Recipient:           "First",
		RecipientIdentifier: "first-id",
		Text:                "first text",
	})
	require.NoError(t, err)

	second, err := fixture.service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Second",
		RecipientIdentifier: "second-id",
		Text:                "second text",
	})
	require.NoError(t, err)

	err = fixture.service.SendDue(t.Context())
	require.NoError(t, err)

	firstStored, err := loadMessageByID(t, fixture.db, first.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusRetry, firstStored.Status)
	require.EqualValues(t, 1, firstStored.Attempt)

	secondStored, err := loadMessageByID(t, fixture.db, second.ID)
	require.NoError(t, err)
	require.Equal(t, MessageStatusSent, secondStored.Status)
	require.EqualValues(t, 1, secondStored.Attempt)

	require.Len(t, fixture.requests(), 2)
}

func TestServiceSendDueRollsBackAttemptOnContextCancel(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	requestStarted := make(chan struct{})
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			close(requestStarted)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}),
	}
	client := signaladapter.New("+380500000000", "http://signal.test", httpClient)
	service, err := New(5, 15*time.Minute, db, client)
	require.NoError(t, err)

	created, err := service.CreateMessage(t.Context(), CreateMessageParams{
		ScheduledAt:         time.Now().UTC().Add(-time.Minute),
		Recipient:           "Alice",
		RecipientIdentifier: "+380501112233",
		Text:                "hello",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.SendDue(ctx)
	}()

	<-requestStarted
	cancel()

	err = <-errCh
	require.ErrorIs(t, err, context.Canceled)

	stored, err := loadMessageByID(t, db, created.ID)
	require.NoError(t, err)
	require.Equal(t, created, stored)
}

func TestMessageStartSendAttemptRejectsInvalidState(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusSent, MaxAttempts: 5}

	_, err := msg.StartSendAttempt()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessageStartSendAttemptRejectsExhaustedAttempts(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 5, MaxAttempts: 5}

	_, err := msg.StartSendAttempt()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessageMarkSentRejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusPending, Attempt: 0, MaxAttempts: 5}

	_, err := msg.MarkSent()
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessageMarkRetryRejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 5, MaxAttempts: 5}

	_, err := msg.MarkRetry("boom")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

func TestMessageMarkRetryRejectsEmptyError(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 1, MaxAttempts: 5}

	_, err := msg.MarkRetry("")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
}

func TestMessageMarkFailedRejectsInvalidInvariant(t *testing.T) {
	msg := Message{ID: 1, Status: MessageStatusRetry, Attempt: 1, MaxAttempts: 5}

	_, err := msg.MarkFailed("boom")
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrConflict)
}

type serviceFixture struct {
	db        *bolt.DB
	service   *Service
	server    *httptest.Server
	responses []testSendResponse
	requested []testSendRequest
	mu        sync.Mutex
}

type testSendRequest struct {
	Message    string   `json:"message"`
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
}

type testSendResponse struct {
	body       string
	statusCode int
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newServiceFixture(t *testing.T, responses ...testSendResponse) *serviceFixture {
	t.Helper()

	return newServiceFixtureWithMaxAge(t, 15*time.Minute, responses...)
}

func newServiceFixtureWithMaxAge(t *testing.T, maxAge time.Duration, responses ...testSendResponse) *serviceFixture {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	fixture := &serviceFixture{
		db:        db,
		responses: append([]testSendResponse(nil), responses...),
	}

	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		var req testSendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		fixture.mu.Lock()
		fixture.requested = append(fixture.requested, req)

		resp := testSendResponse{statusCode: http.StatusCreated}
		if len(fixture.responses) > 0 {
			resp = fixture.responses[0]
			fixture.responses = fixture.responses[1:]
		}
		fixture.mu.Unlock()

		if resp.statusCode == 0 {
			resp.statusCode = http.StatusCreated
		}
		w.WriteHeader(resp.statusCode)
		if resp.body != "" {
			_, writeErr := w.Write([]byte(resp.body))
			if writeErr != nil {
				t.Errorf("write response: %v", writeErr)
			}
		}
	}))
	t.Cleanup(fixture.server.Close)

	client := signaladapter.New("+380500000000", fixture.server.URL, &http.Client{Timeout: time.Second})
	fixture.service, err = New(5, maxAge, db, client)
	require.NoError(t, err)

	return fixture
}

func (f *serviceFixture) requests() []testSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]testSendRequest(nil), f.requested...)
}

func loadMessageByID(t *testing.T, db *bolt.DB, id uint64) (Message, error) {
	t.Helper()

	var msg Message
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		require.NotNil(t, bucket)

		value := bucket.Get(outboxMessageKey(id))
		if value == nil {
			return errbrick.ErrNotFound
		}

		return json.Unmarshal(value, &msg)
	})

	return msg, err
}

func updateStoredMessage(t *testing.T, db *bolt.DB, id uint64, fn func(Message) Message) error {
	t.Helper()

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(outboxMessagesBucket)
		require.NotNil(t, bucket)

		value := bucket.Get(outboxMessageKey(id))
		if value == nil {
			return errbrick.ErrNotFound
		}

		var msg Message
		if err := json.Unmarshal(value, &msg); err != nil {
			return err
		}

		msg = fn(msg)

		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		return bucket.Put(outboxMessageKey(id), data)
	})
}
