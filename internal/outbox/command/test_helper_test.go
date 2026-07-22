package command

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

var testOutboxBucket = []byte("outbox_messages")

type commandFixture struct {
	db        *bolt.DB
	create    *CreateMessage
	cancel    *CancelMessage
	sendDue   *SendDueMessages
	vacuum    *VacuumMessages
	responses []testSendResponse
	requested []testSendRequest
	mu        sync.Mutex
}

type testSendRequest struct {
	Message    string   `json:"message"`
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

func newCommandFixture(t *testing.T, responses ...testSendResponse) *commandFixture {
	t.Helper()

	return newCommandFixtureWithConfig(t, 15*time.Minute, 30*24*time.Hour, responses...)
}

func newCommandFixtureWithMaxAge(t *testing.T, maxAge time.Duration, responses ...testSendResponse) *commandFixture {
	t.Helper()

	return newCommandFixtureWithConfig(t, maxAge, 30*24*time.Hour, responses...)
}

func newCommandFixtureWithConfig(t *testing.T, maxAge, vacuumAge time.Duration, responses ...testSendResponse) *commandFixture {
	t.Helper()

	db := newTestDB(t)
	fixture := &commandFixture{db: db, responses: slices.Clone(responses)}
	client := &http.Client{
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

			var request testSendRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(recorder, "bad request", http.StatusBadRequest)
				return recorder.Result(), nil
			}

			fixture.mu.Lock()
			fixture.requested = append(fixture.requested, request)
			response := testSendResponse{statusCode: http.StatusCreated}
			if len(fixture.responses) > 0 {
				response = fixture.responses[0]
				fixture.responses = fixture.responses[1:]
			}
			fixture.mu.Unlock()

			recorder.WriteHeader(response.statusCode)
			if response.body != "" {
				if _, err := recorder.WriteString(response.body); err != nil {
					t.Errorf("write response: %v", err)
				}
			}

			return recorder.Result(), nil
		}),
	}

	reader := dbadapter.NewDBMessageReader(testOutboxBucket, db)
	writer := dbadapter.NewDBMessageWriter(testOutboxBucket, db)
	signalAdapter := signaladapter.New("+380500000000", "http://signal.test", client)
	fixture.create = NewCreateMessage(5, writer)
	fixture.cancel = NewCancelMesssage(writer)
	fixture.sendDue = NewSendDueMessages(maxAge, writer, reader, signalAdapter)
	fixture.vacuum = NewVacuumMessages(vacuumAge, writer)

	return fixture
}

func newTestDB(t *testing.T) *bolt.DB {
	t.Helper()

	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket(testOutboxBucket)
		return err
	}))

	return db
}

func (f *commandFixture) requests() []testSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.requested)
}

func loadMessageByID(t *testing.T, db *bolt.DB, id uint64) (domain.Message, error) {
	t.Helper()

	var msg domain.Message
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(testOutboxBucket)
		if bucket == nil {
			return errbrick.ErrNotFound
		}

		value := bucket.Get(testMessageKey(id))
		if value == nil {
			return errbrick.ErrNotFound
		}

		return json.Unmarshal(value, &msg)
	})

	return msg, err
}

func updateStoredMessage(t *testing.T, db *bolt.DB, id uint64, update func(domain.Message) domain.Message) error {
	t.Helper()

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(testOutboxBucket)
		if bucket == nil {
			return errbrick.ErrNotFound
		}

		key := testMessageKey(id)
		value := bucket.Get(key)
		if value == nil {
			return errbrick.ErrNotFound
		}

		var msg domain.Message
		if err := json.Unmarshal(value, &msg); err != nil {
			return err
		}

		data, err := json.Marshal(update(msg))
		if err != nil {
			return err
		}

		return bucket.Put(key, data)
	})
}

func testMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}

func createTestMessage(t *testing.T, fixture *commandFixture, scheduledAt time.Time, recipient, identifier, text string) domain.Message {
	t.Helper()

	message, err := fixture.create.Exec(t.Context(), CreateMessageParams{
		ScheduledAt:         scheduledAt,
		Recipient:           recipient,
		RecipientIdentifier: identifier,
		Text:                text,
	})
	require.NoError(t, err)

	return message
}

type cancellableSendFixture struct {
	db             *bolt.DB
	create         *CreateMessage
	sendDue        *SendDueMessages
	requestStarted <-chan struct{}
}

func newCancellableSendFixture(t *testing.T) *cancellableSendFixture {
	t.Helper()

	db := newTestDB(t)
	requestStarted := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	reader := dbadapter.NewDBMessageReader(testOutboxBucket, db)
	writer := dbadapter.NewDBMessageWriter(testOutboxBucket, db)
	adapter := signaladapter.New("+380500000000", "http://signal.test", client)
	return &cancellableSendFixture{
		db:             db,
		create:         NewCreateMessage(5, writer),
		sendDue:        NewSendDueMessages(15*time.Minute, writer, reader, adapter),
		requestStarted: requestStarted,
	}
}
