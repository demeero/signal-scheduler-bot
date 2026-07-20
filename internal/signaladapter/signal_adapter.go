package signaladapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/nyaruka/phonenumbers/v2"
)

type recipientRef struct {
	Value      string
	IsPhoneNum bool
}

// SignalMessage is a transport-neutral inbound Signal envelope.
type SignalMessage struct {
	// SentAt is the time the message was sent.
	SentAt time.Time
	// ServerReceivedAt is the time when the Signal server accepted the envelope from the sending device.
	ServerReceivedAt time.Time
	// ServerDeliveredAt is the time when the Signal server delivered the envelope to a specific device
	ServerDeliveredAt time.Time
	// FetchedAt is the time the message was fetched from the Signal server.
	FetchedAt time.Time
	// SourceMessageID is the unique identifier of the message.
	SourceMessageID string
	// Body is the message body.
	Body string
}

type SignalAdapter struct {
	httpClient *http.Client
	account    string
	baseURL    string
}

func New(account, baseURL string, httpClient *http.Client) *SignalAdapter {
	return &SignalAdapter{
		account:    account,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (s *SignalAdapter) ResolveRecipient(ctx context.Context, recipient string) (string, error) {
	parsedRecipient, err := parseRecipient(recipient)
	if err != nil {
		return "", fmt.Errorf("failed parse recipient: %w", err)
	}

	contacts, err := s.listContacts(ctx)
	if err != nil {
		return "", err
	}

	var matches []contact

	for _, contact := range contacts {
		switch {
		case parsedRecipient.IsPhoneNum && contact.MatchesPhone(parsedRecipient.Value):
			matches = append(matches, contact)
		case !parsedRecipient.IsPhoneNum && contact.MatchesName(parsedRecipient.Value):
			matches = append(matches, contact)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %s", errbrick.ErrNotFound, recipient)
	case 1:
		recipientIdentifier := matches[0].Identifier()
		if recipientIdentifier == "" {
			return "", fmt.Errorf("unresolvable recipient identifier %q: %w", recipient, err)
		}
		return recipientIdentifier, nil
	default:
		return "", fmt.Errorf("%w: found %d contacts matching %q", errbrick.ErrConflict, len(matches), recipient)
	}
}

func (s *SignalAdapter) ReceiveSelfMessages(ctx context.Context) ([]SignalMessage, error) {
	req, err := receiveReq(ctx, s.account, s.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed create receive http request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed receive http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status code for receive response: %d: %w", resp.StatusCode, decodeRespErr(resp))
	}

	return receiveResp(s.account, resp)
}

func (s *SignalAdapter) SendMessage(ctx context.Context, recipient, body string) error {
	req, err := sendReq(ctx, s.account, s.baseURL, recipient, body)
	if err != nil {
		return fmt.Errorf("failed create send http request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed send http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status code for send response: %d: %w", resp.StatusCode, decodeRespErr(resp))
	}

	return nil
}

func (s *SignalAdapter) SendSelfMessage(ctx context.Context, body string) error {
	return s.SendMessage(ctx, s.account, body)
}

func (s *SignalAdapter) listContacts(ctx context.Context) (contactsResponse, error) {
	req, err := listContactsReq(ctx, s.account, s.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed create list conteacts http request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed list contacts http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status code for list contacts response: %d: %w", resp.StatusCode, decodeRespErr(resp))
	}

	var contacts contactsResponse
	if err := json.NewDecoder(resp.Body).Decode(&contacts); err != nil {
		return nil, fmt.Errorf("failed decode contacts response: %w", err)
	}

	return contacts, nil
}

func listContactsReq(ctx context.Context, account, baseURL string) (*http.Request, error) {
	endpoint, err := url.JoinPath(baseURL, "v1", "contacts", account)
	if err != nil {
		return nil, fmt.Errorf("failed build contacts URL: %w", err)
	}

	return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
}

func receiveReq(ctx context.Context, account, baseURL string) (*http.Request, error) {
	endpoint, err := url.JoinPath(baseURL, "v1", "receive", account)
	if err != nil {
		return nil, fmt.Errorf("failed build receive URL: %w", err)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed parse receive URL: %w", err)
	}

	query := u.Query()
	query.Set("ignore_attachments", "true")
	query.Set("ignore_stories", "true")
	query.Set("ignore_avatars", "true")
	query.Set("ignore_stickers", "true")
	query.Set("send_read_receipts", "false")
	u.RawQuery = query.Encode()

	return http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
}

func sendReq(ctx context.Context, account, baseURL, recipient, body string) (*http.Request, error) {
	endpoint, err := url.JoinPath(baseURL, "v2", "send")
	if err != nil {
		return nil, fmt.Errorf("failed build send URL: %w", err)
	}

	b, err := json.Marshal(sendRequest{
		Message:    body,
		Number:     account,
		Recipients: []string{strings.TrimSpace(recipient)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed encode sendReq payload: %w", err)
	}

	return http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
}

func receiveResp(account string, resp *http.Response) ([]SignalMessage, error) {
	var data receiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode receive response: %w", err)
	}

	var result []SignalMessage
	for _, item := range data {
		if item.Account != account {
			continue
		}
		sync := item.Envelope.SyncMessage
		if sync == nil || sync.SentMessage == nil {
			continue
		}
		msg := sync.SentMessage
		if msg.Message == "" || msg.IsExpirationUpdate {
			continue
		}
		if msg.DestinationNumber != account && msg.Destination != account {
			continue
		}

		result = append(result, SignalMessage{
			SentAt:            time.UnixMilli(msg.Timestamp),
			ServerReceivedAt:  time.UnixMilli(item.Envelope.ServerReceivedTimestamp),
			ServerDeliveredAt: time.UnixMilli(item.Envelope.ServerDeliveredTimestamp),
			SourceMessageID:   fmt.Sprintf("%s:%d", item.Envelope.SourceUUID, msg.Timestamp),
			Body:              msg.Message,
		})
	}

	return result, nil
}

func decodeRespErr(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return errors.New(payload.Error)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		text = http.StatusText(resp.StatusCode)
	}

	return errors.New(text)
}

func parseRecipient(recipient string) (recipientRef, error) {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return recipientRef{}, errors.New("recipient is empty")
	}

	number, err := phonenumbers.Parse(recipient, "UA")
	if err == nil && phonenumbers.IsValidNumber(number) {
		return recipientRef{IsPhoneNum: true, Value: phonenumbers.Format(number, phonenumbers.E164)}, nil
	}

	return recipientRef{Value: recipient}, nil
}
