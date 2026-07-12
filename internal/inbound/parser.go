package inbound

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
)

const (
	commandHelp           = "/help"
	commandList           = "/list"
	commandCancelPrefix   = "/cancel "
	commandSchedulePrefix = "/schedule "
)

// parser parses raw bot commands into transport-local command values.
type parser struct {
	location *time.Location
}

// newParser builds a command parser for a specific timezone.
func newParser(location *time.Location) *parser {
	return &parser{location: location}
}

// Parse parses a raw incoming command into a typed command.
func (p *parser) Parse(raw string, now time.Time) (parsedCommand, error) {
	text := strings.TrimSpace(raw)
	switch {
	case text == commandHelp:
		return helpCommand{}, nil
	case text == commandList:
		return listCommand{}, nil
	case strings.HasPrefix(text, commandCancelPrefix):
		return parseCancel(text)
	case strings.HasPrefix(text, commandSchedulePrefix):
		return p.parseSchedule(text, now)
	default:
		return nil, fmt.Errorf("%w: unsupported command", errbrick.ErrInvalidData)
	}
}

func parseCancel(text string) (parsedCommand, error) {
	idText := strings.TrimSpace(strings.TrimPrefix(text, commandCancelPrefix))
	if idText == "" {
		return nil, fmt.Errorf("%w: cancel message id is empty", errbrick.ErrInvalidData)
	}

	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: failed parse id: %s", errbrick.ErrInvalidData, idText)
	}

	return cancelCommand{id: id}, nil
}

func (p *parser) parseSchedule(text string, now time.Time) (parsedCommand, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(text, commandSchedulePrefix))
	dateToken, rest, err := cutField(rest)
	if err != nil {
		return nil, err
	}

	timeToken, rest, err := cutField(rest)
	if err != nil {
		return nil, err
	}

	recipient, rest, err := cutRecipient(rest)
	if err != nil {
		return nil, err
	}

	message := strings.TrimSpace(rest)
	if message == "" {
		return nil, fmt.Errorf("%w: schedule message body is empty", errbrick.ErrInvalidData)
	}

	whenUTC, localText, err := p.parseWhen(now, dateToken, timeToken)
	if err != nil {
		return nil, err
	}

	if whenUTC.Before(now.UTC()) {
		return nil, fmt.Errorf("%w: scheduled time is in the past", errbrick.ErrInvalidData)
	}

	return scheduleCommand{
		When:              whenUTC,
		OriginalLocalTime: localText,
		Timezone:          p.location.String(),
		Recipient:         recipient,
		Text:              message,
	}, nil
}

func (p *parser) parseWhen(now time.Time, dateToken, timeToken string) (time.Time, string, error) {
	clock, err := time.ParseInLocation("15:04", timeToken, p.location)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: invalid time format", errbrick.ErrInvalidData)
	}

	baseNow := now.In(p.location)

	var localDate time.Time
	switch strings.ToLower(dateToken) {
	case "today":
		localDate = time.Date(baseNow.Year(), baseNow.Month(), baseNow.Day(), 0, 0, 0, 0, p.location)
	case "tomorrow":
		tomorrow := baseNow.AddDate(0, 0, 1)
		localDate = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, p.location)
	default:
		localDate, err = time.ParseInLocation("2006-01-02", dateToken, p.location)
		if err != nil {
			return time.Time{}, "", fmt.Errorf("%w: invalid date format", errbrick.ErrInvalidData)
		}
	}

	whenLocal := time.Date(
		localDate.Year(),
		localDate.Month(),
		localDate.Day(),
		clock.Hour(),
		clock.Minute(),
		0,
		0,
		p.location,
	)

	return whenLocal.UTC(), whenLocal.Format("2006-01-02 15:04"), nil
}

func cutField(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", fmt.Errorf("%w: schedule command is incomplete", errbrick.ErrInvalidData)
	}

	for i, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			return text[:i], text[i+1:], nil
		}
	}

	return "", "", fmt.Errorf("%w: schedule command is incomplete", errbrick.ErrInvalidData)
}

func cutRecipient(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", fmt.Errorf("%w: recipient is empty", errbrick.ErrInvalidData)
	}

	if strings.HasPrefix(text, "\"") {
		end := strings.Index(text[1:], "\"")
		if end < 0 {
			return "", "", fmt.Errorf("%w: contact name is missing closing quote", errbrick.ErrInvalidData)
		}

		name := text[1 : end+1]

		return name, text[end+2:], nil
	}

	token, rest, err := cutFieldOrRemainder(text)
	if err != nil {
		return "", "", err
	}

	return token, rest, nil
}

func cutFieldOrRemainder(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", fmt.Errorf("%w: recipient is empty", errbrick.ErrInvalidData)
	}

	for i, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			return text[:i], text[i+1:], nil
		}
	}

	return text, "", nil
}
