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

const testAccount = "+380500000000"

func TestSignalAdapter_ResolveRecipient_ByPhone(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/contacts/"+testAccount, r.URL.Path)
		writeReceiveJSON(t, w, contactsResponse{
			{
				Name:   "Alice",
				Number: "+380501112233",
				UUID:   "alice-uuid",
			},
		})
	}))

	recipient, err := adapter.ResolveRecipient(t.Context(), " +380501112233 ")
	require.NoError(t, err)
	require.Equal(t, "+380501112233", recipient)
}

func TestSignalAdapter_ResolveRecipient_ByNameUsesUUIDFallback(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/contacts/"+testAccount, r.URL.Path)
		writeReceiveJSON(t, w, contactsResponse{
			{
				Name: "Alice Smith",
				UUID: "alice-uuid",
			},
		})
	}))

	recipient, err := adapter.ResolveRecipient(t.Context(), "Alice Smith")
	require.NoError(t, err)
	require.Equal(t, "alice-uuid", recipient)
}

func TestSignalAdapter_ResolveRecipient_ByProfileGivenName(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/contacts/"+testAccount, r.URL.Path)
		writeReceiveJSON(t, w, contactsResponse{
			{
				Number: "+380668360625",
				UUID:   "expert-uuid",
				Profile: contactProfile{
					GivenName: "expert",
				},
			},
		})
	}))

	recipient, err := adapter.ResolveRecipient(t.Context(), "expert")
	require.NoError(t, err)
	require.Equal(t, "+380668360625", recipient)
}

func TestSignalAdapter_ResolveRecipient_ReturnsConflictForAmbiguousMatch(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/contacts/"+testAccount, r.URL.Path)
		writeReceiveJSON(t, w, contactsResponse{
			{Name: "Alice"},
			{Name: "Alice"},
		})
	}))

	_, err := adapter.ResolveRecipient(t.Context(), "Alice")
	require.Error(t, err)
	require.ErrorContains(t, err, "found 2 contacts matching")
}

func TestParseRecipient(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		wantErr   string
		want      recipientRef
	}{
		{
			name:      "phone number",
			recipient: "+380501112233",
			want: recipientRef{
				Value:      "+380501112233",
				IsPhoneNum: true,
			},
		},
		{
			name:      "contact name",
			recipient: " Alice Smith ",
			want: recipientRef{
				Value: "Alice Smith",
			},
		},
		{
			name:      "empty",
			recipient: "  ",
			wantErr:   "recipient is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRecipient(tt.recipient)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSignalAdapter_SendMessage(t *testing.T) {
	const recipient = "+380501112233"
	const body = "Test message"

	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v2/send", r.URL.Path)

		var payload sendRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, sendRequest{
			Message:    body,
			Number:     testAccount,
			Recipients: []string{recipient},
		}, payload)

		w.WriteHeader(http.StatusCreated)
	}))

	err := adapter.SendMessage(t.Context(), "  "+recipient+"  ", body)
	require.NoError(t, err)
}

func TestSignalAdapter_SendMessage_HTTPJSONError(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeReceiveJSON(t, w, map[string]string{"error": "recipient missing"})
	}))

	err := adapter.SendMessage(t.Context(), "+380501112233", "Test message")
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for send response: 400")
	require.ErrorContains(t, err, "recipient missing")
}

func TestSignalAdapter_SendMessage_HTTPTextError(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, err := w.Write([]byte("temporarily unavailable"))
		assert.NoError(t, err)
	}))

	err := adapter.SendMessage(t.Context(), "+380501112233", "Test message")
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for send response: 503")
	require.ErrorContains(t, err, "temporarily unavailable")
}

func TestSignalAdapter_SendSelfMessage(t *testing.T) {
	const body = "Self note"

	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v2/send", r.URL.Path)

		var payload sendRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, sendRequest{
			Message:    body,
			Number:     testAccount,
			Recipients: []string{testAccount},
		}, payload)

		w.WriteHeader(http.StatusAccepted)
	}))

	err := adapter.SendSelfMessage(t.Context(), body)
	require.NoError(t, err)
}

func TestSignalAdapter_ReceiveSelfMessages(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/receive/"+testAccount, r.URL.Path)

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
							DestinationNumber: testAccount,
							Message:           "ignore other account",
							Timestamp:         1_780_293_000_000,
						},
					},
				},
			},
			{
				Account:  testAccount,
				Envelope: envelope{SourceUUID: "ignored-no-sync"},
			},
			{
				Account: testAccount,
				Envelope: envelope{
					SourceUUID:  "ignored-no-sent",
					SyncMessage: &syncMessage{},
				},
			},
			{
				Account: testAccount,
				Envelope: envelope{
					SourceUUID: "ignored-empty-message",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: testAccount,
							Timestamp:         1_780_293_100_000,
						},
					},
				},
			},
			{
				Account: testAccount,
				Envelope: envelope{
					SourceUUID: "ignored-expiration-update",
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber:  testAccount,
							Message:            "ignore expiration",
							Timestamp:          1_780_293_200_000,
							IsExpirationUpdate: true,
						},
					},
				},
			},
			{
				Account: testAccount,
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
				Account: testAccount,
				Envelope: envelope{
					SourceUUID:               "self-number",
					ServerReceivedTimestamp:  1_780_293_401_000,
					ServerDeliveredTimestamp: 1_780_293_402_000,
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							DestinationNumber: testAccount,
							Message:           "/help",
							Timestamp:         1_780_293_400_000,
						},
					},
				},
			},
			{
				Account: testAccount,
				Envelope: envelope{
					SourceUUID:               "self-destination",
					ServerReceivedTimestamp:  1_780_293_501_000,
					ServerDeliveredTimestamp: 1_780_293_502_000,
					SyncMessage: &syncMessage{
						SentMessage: &sentMessage{
							Destination: testAccount,
							Message:     "ping",
							Timestamp:   1_780_293_500_000,
						},
					},
				},
			},
		})
	}))

	messages, err := adapter.ReceiveSelfMessages(t.Context())
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Equal(t, "/help", messages[0].Body)
	require.Equal(t, "self-number:1780293400000", messages[0].SourceMessageID)
	require.Equal(t, time.UnixMilli(1_780_293_400_000), messages[0].SentAt)
	require.Equal(t, time.UnixMilli(1_780_293_401_000), messages[0].ServerReceivedAt)
	require.Equal(t, time.UnixMilli(1_780_293_402_000), messages[0].ServerDeliveredAt)

	require.Equal(t, "ping", messages[1].Body)
	require.Equal(t, "self-destination:1780293500000", messages[1].SourceMessageID)
	require.Equal(t, time.UnixMilli(1_780_293_500_000), messages[1].SentAt)
	require.Equal(t, time.UnixMilli(1_780_293_501_000), messages[1].ServerReceivedAt)
	require.Equal(t, time.UnixMilli(1_780_293_502_000), messages[1].ServerDeliveredAt)
}

func TestSignalAdapter_ReceiveSelfMessages_HTTPJSONError(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeReceiveJSON(t, w, map[string]string{"error": "signal service unavailable"})
	}))

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for receive response: 500")
	require.ErrorContains(t, err, "signal service unavailable")
}

func TestSignalAdapter_ReceiveSelfMessages_HTTPTextError(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, err := w.Write([]byte("upstream reset"))
		assert.NoError(t, err)
	}))

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected status code for receive response: 502")
	require.ErrorContains(t, err, "upstream reset")
}

func TestSignalAdapter_ReceiveSelfMessages_DecodeError(t *testing.T) {
	adapter := newTestAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte("{"))
		assert.NoError(t, err)
	}))

	_, err := adapter.ReceiveSelfMessages(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to decode receive response")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newTestAdapter(t *testing.T, handler http.HandlerFunc) *SignalAdapter {
	t.Helper()

	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler(recorder, r)
			return recorder.Result(), nil
		}),
	}

	return New(testAccount, "http://signal.test", client)
}

func writeReceiveJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(payload))
}
