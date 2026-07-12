package signaladapter

import (
	"strings"

	"github.com/nyaruka/phonenumbers/v2"
)

type receiveResponse []receiveResponseItem

type receiveResponseItem struct {
	Account  string   `json:"account"`
	Envelope envelope `json:"envelope"`
}

type envelope struct {
	SourceNumber             *string        `json:"sourceNumber"`
	SyncMessage              *syncMessage   `json:"syncMessage,omitempty"`
	TypingMessage            *typingMessage `json:"typingMessage,omitempty"`
	Source                   string         `json:"source"`
	SourceUUID               string         `json:"sourceUuid"`
	SourceName               string         `json:"sourceName"`
	SourceDevice             int64          `json:"sourceDevice"`
	Timestamp                int64          `json:"timestamp"`
	ServerReceivedTimestamp  int64          `json:"serverReceivedTimestamp"`
	ServerDeliveredTimestamp int64          `json:"serverDeliveredTimestamp"`
}

type syncMessage struct {
	SentMessage *sentMessage `json:"sentMessage"`
}

type sentMessage struct {
	Destination        string `json:"destination"`
	DestinationNumber  string `json:"destinationNumber"`
	DestinationUUID    string `json:"destinationUuid"`
	Message            string `json:"message"`
	Timestamp          int64  `json:"timestamp"`
	ExpiresInSeconds   int64  `json:"expiresInSeconds"`
	IsExpirationUpdate bool   `json:"isExpirationUpdate"`
	ViewOnce           bool   `json:"viewOnce"`
}

type typingMessage struct {
	Action    string `json:"action"`
	GroupID   string `json:"groupId"`
	Timestamp int64  `json:"timestamp"`
}

type contactsResponse []contact

type contact struct {
	Number      string `json:"number"`
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	ProfileName string `json:"profile_name"`
	Username    string `json:"username"`
	Color       string `json:"color"`
	Blocked     bool   `json:"blocked"`
}

func (c contact) MatchesPhone(expected string) bool {
	if strings.TrimSpace(c.Number) == "" {
		return false
	}

	number, err := phonenumbers.Parse(c.Number, "UA")
	if err != nil || !phonenumbers.IsValidNumber(number) {
		return false
	}

	return phonenumbers.Format(number, phonenumbers.E164) == expected
}

func (c contact) Identifier() string {
	if number := strings.TrimSpace(c.Number); number != "" {
		parsed, err := phonenumbers.Parse(number, "UA")
		if err == nil && phonenumbers.IsValidNumber(parsed) {
			return phonenumbers.Format(parsed, phonenumbers.E164)
		}
	}

	if uuid := strings.TrimSpace(c.UUID); uuid != "" {
		return uuid
	}

	return ""
}
