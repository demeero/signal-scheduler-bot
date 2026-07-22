package bot

import (
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/stretchr/testify/require"
)

func TestParser_Parse_Upcoming(t *testing.T) {
	parser := newTestParser(t)

	parsed, err := parser.Parse("/upcoming", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	cmd, ok := parsed.(upcomingCommand)
	require.True(t, ok)
	require.Equal(t, upcomingCommand{}, cmd)
}

func TestParser_Parse_History(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name  string
		input string
		limit int
	}{
		{
			name:  "default limit",
			input: "/history",
			limit: defaultHistoryLimit,
		},
		{
			name:  "explicit limit",
			input: "/history 100",
			limit: maximumHistoryLimit,
		},
		{
			name:  "whitespace after verb",
			input: "/history\t42",
			limit: 42,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parser.Parse(tc.input, now)
			require.NoError(t, err)

			cmd, ok := parsed.(historyCommand)
			require.True(t, ok)
			require.Equal(t, tc.limit, cmd.limit)
		})
	}
}

func TestParser_Parse_HistoryRejectsInvalidLimit(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "zero",
			input:       "/history 0",
			errContains: "history limit must be between 1 and 100",
		},
		{
			name:        "too large",
			input:       "/history 101",
			errContains: "history limit must be between 1 and 100",
		},
		{
			name:        "not a number",
			input:       "/history abc",
			errContains: `invalid history limit "abc"`,
		},
		{
			name:        "more than one argument",
			input:       "/history 1 2",
			errContains: "history accepts at most one limit",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parser.Parse(tc.input, now)
			require.ErrorIs(t, err, errbrick.ErrInvalidData)
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestParser_Parse_Cancel(t *testing.T) {
	parser := newTestParser(t)

	parsed, err := parser.Parse("/cancel 42", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	cmd, ok := parsed.(cancelCommand)
	require.True(t, ok)
	require.Equal(t, uint64(42), cmd.id)
}

func TestParser_Parse_CancelAcceptsWhitespaceAfterVerb(t *testing.T) {
	parser := newTestParser(t)

	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "tab",
			input: "/cancel\t42",
		},
		{
			name:  "newline",
			input: "/cancel\n42",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parser.Parse(tc.input, time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
			require.NoError(t, err)

			cmd, ok := parsed.(cancelCommand)
			require.True(t, ok)
			require.Equal(t, uint64(42), cmd.id)
		})
	}
}

func TestParser_Parse_CancelRejectedWhenIDMissing(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/cancel", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "cancel message id is empty")
}

func TestParser_Parse_CancelRejectedWhenIDInvalid(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/cancel abc", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, `invalid cancel message id "abc"`)
	require.ErrorContains(t, err, "strconv.ParseUint")
	require.ErrorContains(t, err, "invalid syntax")
}

func TestParser_Parse_ScheduleDateWithPhoneRecipient(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	parsed, err := parser.Parse("/schedule 2026-07-13 15:30 +380501112233 Hello there", now)
	require.NoError(t, err)

	cmd, ok := parsed.(scheduleCommand)
	require.True(t, ok)
	require.Equal(t, "+380501112233", cmd.Recipient)
	require.Equal(t, "Hello there", cmd.Text)
	require.Equal(t, time.Date(2026, time.July, 13, 12, 30, 0, 0, time.UTC), cmd.When)
}

func TestParser_Parse_ScheduleToday(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	parsed, err := parser.Parse("/schedule today 15:45 +380501112233 Hi", now)
	require.NoError(t, err)

	cmd, ok := parsed.(scheduleCommand)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, time.July, 12, 12, 45, 0, 0, time.UTC), cmd.When)
}

func TestParser_Parse_ScheduleTomorrowUsesLocalDate(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 22, 30, 0, 0, time.UTC)

	parsed, err := parser.Parse("/schedule tomorrow 09:15 +380501112233 Good morning", now)
	require.NoError(t, err)

	cmd, ok := parsed.(scheduleCommand)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, time.July, 14, 6, 15, 0, 0, time.UTC), cmd.When)
}

func TestParser_Parse_ScheduleAcceptsQuotedRecipientVariants(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name      string
		input     string
		recipient string
	}{
		{
			name:      "ascii",
			input:     `/schedule today 15:30 "Alice Smith" Hello`,
			recipient: "Alice Smith",
		},
		{
			name:      "curly",
			input:     "/schedule today 15:30 “Alice Smith” Hello",
			recipient: "Alice Smith",
		},
		{
			name:      "low-high",
			input:     "/schedule today 15:30 „Alice Smith“ Hello",
			recipient: "Alice Smith",
		},
		{
			name:      "guillemets",
			input:     "/schedule today 15:30 «Alice Smith» Hello",
			recipient: "Alice Smith",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parser.Parse(tc.input, now)
			require.NoError(t, err)

			cmd, ok := parsed.(scheduleCommand)
			require.True(t, ok)
			require.Equal(t, tc.recipient, cmd.Recipient)
			require.Equal(t, "Hello", cmd.Text)
		})
	}
}

func TestParser_Parse_ScheduleAcceptsWhitespaceBetweenTokens(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	parsed, err := parser.Parse("/schedule\t2026-07-13\n15:30\t+380501112233\nHello there", now)
	require.NoError(t, err)

	cmd, ok := parsed.(scheduleCommand)
	require.True(t, ok)
	require.Equal(t, "+380501112233", cmd.Recipient)
	require.Equal(t, "Hello there", cmd.Text)
	require.Equal(t, time.Date(2026, time.July, 13, 12, 30, 0, 0, time.UTC), cmd.When)
}

func TestParser_Parse_ScheduleRejectedWhenScheduledInPast(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/schedule today 11:59 +380501112233 Hello", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "scheduled time is in the past")
}

func TestParser_Parse_ScheduleRejectedWhenDateInvalid(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/schedule 2026-99-13 15:30 +380501112233 Hello", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, `invalid date format "2026-99-13"`)
	require.ErrorContains(t, err, "parsing time")
	require.ErrorContains(t, err, "month out of range")
}

func TestParser_Parse_ScheduleRejectedWhenTimeInvalid(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/schedule 2026-07-13 25:30 +380501112233 Hello", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, `invalid time format "25:30"`)
	require.ErrorContains(t, err, "parsing time")
	require.ErrorContains(t, err, "hour out of range")
}

func TestParser_Parse_ScheduleRejectedWhenCommandIncomplete(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "missing all args",
			input:       "/schedule",
			errContains: "schedule command is incomplete",
		},
		{
			name:        "missing time recipient and message",
			input:       "/schedule today",
			errContains: "schedule command is incomplete",
		},
		{
			name:        "missing recipient and message",
			input:       "/schedule today 15:30",
			errContains: "schedule command is incomplete",
		},
		{
			name:        "missing message for phone recipient",
			input:       "/schedule today 15:30 +380501112233",
			errContains: "schedule message body is empty",
		},
		{
			name:        "missing message for quoted recipient",
			input:       `/schedule today 15:30 "Alice"`,
			errContains: "schedule message body is empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parser.Parse(tc.input, now)
			require.ErrorIs(t, err, errbrick.ErrInvalidData)
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestParser_Parse_ScheduleRejectedWhenQuotedRecipientInvalid(t *testing.T) {
	parser := newTestParser(t)
	now := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "missing closing quote",
			input:       `/schedule today 15:30 "Alice Hello`,
			errContains: "contact name is missing closing quote",
		},
		{
			name:        "mismatched quotes",
			input:       "/schedule today 15:30 “Alice\" Hello",
			errContains: "contact name uses mismatched quotes",
		},
		{
			name:        "empty quoted recipient",
			input:       `/schedule today 15:30 "" Hello`,
			errContains: "recipient is empty",
		},
		{
			name:        "missing delimiter after closing quote",
			input:       `/schedule today 15:30 "Alice"Hello`,
			errContains: "expected whitespace after closing quote",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parser.Parse(tc.input, now)
			require.ErrorIs(t, err, errbrick.ErrInvalidData)
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestParser_Parse_ListRejected(t *testing.T) {
	parser := newTestParser(t)

	_, err := parser.Parse("/list", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "unsupported command")
}

func newTestParser(t *testing.T) *parser {
	t.Helper()

	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	return newParser(location)
}
