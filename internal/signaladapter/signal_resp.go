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
	SourceNumber             *string      `json:"sourceNumber"`
	SyncMessage              *syncMessage `json:"syncMessage,omitempty"`
	Source                   string       `json:"source"`
	SourceUUID               string       `json:"sourceUuid"`
	SourceName               string       `json:"sourceName"`
	SourceDevice             int64        `json:"sourceDevice"`
	Timestamp                int64        `json:"timestamp"`
	ServerReceivedTimestamp  int64        `json:"serverReceivedTimestamp"`
	ServerDeliveredTimestamp int64        `json:"serverDeliveredTimestamp"`
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
	IsExpirationUpdate bool   `json:"isExpirationUpdate"`
}

type contactsResponse []contact

type contact struct {
	Nickname    contactNickname `json:"nickname"`
	Profile     contactProfile  `json:"profile"`
	Number      string          `json:"number"`
	UUID        string          `json:"uuid"`
	Name        string          `json:"name"`
	ProfileName string          `json:"profile_name"`
	Username    string          `json:"username"`
	GivenName   string          `json:"given_name"`
	Blocked     bool            `json:"blocked"`
}

type contactProfile struct {
	GivenName string `json:"given_name"`
	LastName  string `json:"lastname"`
}

type contactNickname struct {
	Name       string `json:"name"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
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

func (c contact) MatchesName(expected string) bool {
	for _, candidate := range []string{
		c.Name,
		c.ProfileName,
		c.Username,
		c.Profile.GivenName,
		c.GivenName,
		c.Nickname.Name,
		c.Nickname.GivenName,
	} {
		if strings.TrimSpace(candidate) == expected {
			return true
		}
	}

	return false
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
