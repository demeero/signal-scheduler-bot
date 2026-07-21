package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
)

const (
	commandCancel       = "/cancel"
	commandHelp         = "/help"
	commandHistory      = "/history"
	commandUpcoming     = "/upcoming"
	commandSchedule     = "/schedule"
	defaultHistoryLimit = 20
	maximumHistoryLimit = 100
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
	command, args, err := cutFieldOrRemainder(text)
	if err != nil {
		return nil, fmt.Errorf("%w: unsupported command", errbrick.ErrInvalidData)
	}

	switch command {
	case commandHelp:
		if strings.TrimSpace(args) != "" {
			return nil, fmt.Errorf("%w: unsupported command", errbrick.ErrInvalidData)
		}
		return helpCommand{}, nil
	case commandUpcoming:
		if strings.TrimSpace(args) != "" {
			return nil, fmt.Errorf("%w: unsupported command", errbrick.ErrInvalidData)
		}
		return upcomingCommand{}, nil
	case commandHistory:
		return parseHistory(args)
	case commandCancel:
		return parseCancel(args)
	case commandSchedule:
		return p.parseSchedule(args, now)
	default:
		return nil, fmt.Errorf("%w: unsupported command", errbrick.ErrInvalidData)
	}
}

func parseHistory(args string) (parsedCommand, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return historyCommand{limit: defaultHistoryLimit}, nil
	}

	limitText, remainder, err := cutFieldOrRemainder(args)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid history limit", errbrick.ErrInvalidData)
	}
	if strings.TrimSpace(remainder) != "" {
		return nil, fmt.Errorf("%w: history accepts at most one limit", errbrick.ErrInvalidData)
	}

	limit, err := strconv.ParseUint(limitText, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid history limit %q: %w", errbrick.ErrInvalidData, limitText, err)
	}
	if limit == 0 || limit > maximumHistoryLimit {
		return nil, fmt.Errorf("%w: history limit must be between 1 and %d", errbrick.ErrInvalidData, maximumHistoryLimit)
	}

	return historyCommand{limit: int(limit)}, nil
}

func parseCancel(args string) (parsedCommand, error) {
	idText := strings.TrimSpace(args)
	if idText == "" {
		return nil, fmt.Errorf("%w: cancel message id is empty", errbrick.ErrInvalidData)
	}

	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cancel message id %q: %w", errbrick.ErrInvalidData, idText, err)
	}

	return cancelCommand{id: id}, nil
}

func (p *parser) parseSchedule(args string, now time.Time) (parsedCommand, error) {
	rest := strings.TrimSpace(args)
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

	when, err := p.parseWhen(now, dateToken, timeToken)
	if err != nil {
		return nil, err
	}

	if when.Before(now.UTC()) {
		return nil, fmt.Errorf("%w: scheduled time is in the past", errbrick.ErrInvalidData)
	}

	return scheduleCommand{
		When:      when,
		Recipient: recipient,
		Text:      message,
	}, nil
}

func (p *parser) parseWhen(now time.Time, dateToken, timeToken string) (time.Time, error) {
	clock, err := time.ParseInLocation("15:04", timeToken, p.location)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid time format %q: %w", errbrick.ErrInvalidData, timeToken, err)
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
			return time.Time{}, fmt.Errorf("%w: invalid date format %q: %w", errbrick.ErrInvalidData, dateToken, err)
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

	return whenLocal.UTC(), nil
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

	if open, closeQuote, ok := quotePair(text); ok {
		return cutQuotedRecipient(text, open, closeQuote)
	}

	token, rest, err := cutFieldOrRemainder(text)
	if err != nil {
		return "", "", err
	}

	return token, rest, nil
}

func cutQuotedRecipient(text, openQuote, closeQuote string) (string, string, error) {
	trimmed := strings.TrimSpace(text)
	quoted := []rune(trimmed)
	if len(quoted) == 0 {
		return "", "", fmt.Errorf("%w: recipient is empty", errbrick.ErrInvalidData)
	}

	openRune, _ := utf8.DecodeRuneInString(openQuote)
	closeRune, _ := utf8.DecodeRuneInString(closeQuote)
	closingQuotes := supportedClosingQuotes()

	for idx := 1; idx < len(quoted); idx++ {
		switch quoted[idx] {
		case closeRune:
			name := strings.TrimSpace(string(quoted[1:idx]))
			if name == "" {
				return "", "", fmt.Errorf("%w: recipient is empty", errbrick.ErrInvalidData)
			}

			rest := quoted[idx+1:]
			if len(rest) == 0 {
				return name, "", nil
			}

			if !unicode.IsSpace(rest[0]) {
				return "", "", fmt.Errorf("%w: expected whitespace after closing quote", errbrick.ErrInvalidData)
			}

			return name, string(rest[1:]), nil
		case openRune:
			continue
		default:
			if _, ok := closingQuotes[quoted[idx]]; ok {
				return "", "", fmt.Errorf("%w: contact name uses mismatched quotes", errbrick.ErrInvalidData)
			}
		}
	}

	return "", "", fmt.Errorf("%w: contact name is missing closing quote", errbrick.ErrInvalidData)
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

func quotePair(text string) (string, string, bool) {
	for _, pair := range [][2]string{
		{`"`, `"`},
		{"“", "”"},
		{"„", "“"},
		{"«", "»"},
	} {
		if strings.HasPrefix(text, pair[0]) {
			return pair[0], pair[1], true
		}
	}

	return "", "", false
}

func supportedClosingQuotes() map[rune]struct{} {
	return map[rune]struct{}{
		'"': {},
		'”': {},
		'“': {},
		'»': {},
	}
}
