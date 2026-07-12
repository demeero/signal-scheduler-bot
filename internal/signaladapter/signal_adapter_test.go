package signaladapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalAdapter_SendMessage(t *testing.T) {
	const account = "+380500000000"
	const recipient = "+380501112233"
	const body = "Test message"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v2/send", r.URL.Path)

		var payload sendRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, sendRequest{
			Message:    body,
			Number:     account,
			Recipients: []string{recipient},
		}, payload)

		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	adapter := New(account, server.URL, &http.Client{Timeout: time.Second})

	err := adapter.SendMessage(t.Context(), "  "+recipient+"  ", body)
	require.NoError(t, err)
}

func TestSignalAdapter_SendMessage_HTTPJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeReceiveJSON(t, w, map[string]string{"error": "recipient missing"})
	}))
	t.Cleanup(server.Close)

	adapter := New("+380500000000", server.URL, &http.Client{Timeout: time.Second})

	err := adapter.SendMessage(t.Context(), "+380501112233", "Test message")
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for send response: 400")
	require.ErrorContains(t, err, "recipient missing")
}

func TestSignalAdapter_SendMessage_HTTPTextError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, err := w.Write([]byte("temporarily unavailable"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	adapter := New("+380500000000", server.URL, &http.Client{Timeout: time.Second})

	err := adapter.SendMessage(t.Context(), "+380501112233", "Test message")
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for send response: 503")
	require.ErrorContains(t, err, "temporarily unavailable")
}

func TestSignalAdapter_SendSelfMessage(t *testing.T) {
	const account = "+380500000000"
	const body = "Self note"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v2/send", r.URL.Path)

		var payload sendRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, sendRequest{
			Message:    body,
			Number:     account,
			Recipients: []string{account},
		}, payload)

		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	adapter := New(account, server.URL, &http.Client{Timeout: time.Second})

	err := adapter.SendSelfMessage(t.Context(), body)
	require.NoError(t, err)
}

func TestSignalAdapter_ReceiveSelfMessages(t *testing.T) {
	const account = "+380500000000"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/receive/"+account, r.URL.Path)

		query := r.URL.Query()
		assert.Equal(t, "true", query.Get("ignore_attachments"))
		assert.Equal(t, "true", query.Get("ignore_stories"))
		assert.Equal(t, "true", query.Get("ignore_avatars"))
		assert.Equal(t, "true", query.Get("ignore_stickers"))
		assert.Equal(t, "false", query.Get("send_read_receipts"))

		writeReceiveJSON(t, w, receiveResponse{
			{
				Account: "+380671112233",
				Envelope: envelope{
					SourceUUID: "ignored-account",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: account,
							Message:           "ignore other account",
							Timestamp:         1_780_293_000_000,
						},
					},
				},
			},
			{
				Account:  account,
				Envelope: envelope{SourceUUID: "ignored-no-sync"},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID:  "ignored-no-sent",
					SyncMessage: &syncMessage{},
				},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID: "ignored-empty-message",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: account,
							Timestamp:         1_780_293_100_000,
						},
					},
				},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID: "ignored-expiration-update",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber:  account,
							Message:            "ignore expiration",
							Timestamp:          1_780_293_200_000,
							IsExpirationUpdate: true,
						},
					},
				},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID: "ignored-other-destination",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: "+380501112233",
							Destination:       "uuid-other",
							Message:           "ignore other destination",
							Timestamp:         1_780_293_300_000,
						},
					},
				},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID: "self-number",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: account,
							Message:           "/help",
							Timestamp:         1_780_293_400_000,
						},
					},
				},
			},
			{
				Account: account,
				Envelope: envelope{
					SourceUUID: "self-destination",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							Destination: account,
							Message:     "ping",
							Timestamp:   1_780_293_500_000,
						},
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	adapter := New(account, server.URL, &http.Client{Timeout: time.Second})

	messages, err := adapter.ReceiveSelfMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Equal(t, "/help", messages[0].Body)
	require.Equal(t, "self-number:1780293400000", messages[0].SourceMessageID)
	require.Equal(t, time.UnixMilli(1_780_293_400_000), messages[0].SentAt)

	require.Equal(t, "ping", messages[1].Body)
	require.Equal(t, "self-destination:1780293500000", messages[1].SourceMessageID)
	require.Equal(t, time.UnixMilli(1_780_293_500_000), messages[1].SentAt)
}

func TestSignalAdapter_ReceiveSelfMessages_HTTPJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeReceiveJSON(t, w, map[string]string{"error": "signal service unavailable"})
	}))
	t.Cleanup(server.Close)

	adapter := New("+380500000000", server.URL, &http.Client{Timeout: time.Second})

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for receive response: 500")
	require.ErrorContains(t, err, "signal service unavailable")
}

func TestSignalAdapter_ReceiveSelfMessages_HTTPTextError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, err := w.Write([]byte("upstream reset"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	adapter := New("+380500000000", server.URL, &http.Client{Timeout: time.Second})

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for receive response: 502")
	require.ErrorContains(t, err, "upstream reset")
}

func TestSignalAdapter_ReceiveSelfMessages_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte("{"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	adapter := New("+380500000000", server.URL, &http.Client{Timeout: time.Second})

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to decode receive response")
}

func writeReceiveJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(payload))
}
