package inbound

import (
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/stretchr/testify/require"
)

func TestParserParseUpcoming(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	parser := newParser(location)

	parsed, err := parser.Parse("/upcoming", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	cmd, ok := parsed.(upcomingCommand)
	require.True(t, ok)
	require.Equal(t, "upcoming", cmd.Name())
}

func TestParserParseCancel(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	parser := newParser(location)

	parsed, err := parser.Parse("/cancel 42", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	cmd, ok := parsed.(cancelCommand)
	require.True(t, ok)
	require.Equal(t, uint64(42), cmd.id)
	require.Equal(t, "cancel", cmd.Name())
}

func TestParserParseCancelRejectedWhenIDMissing(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	parser := newParser(location)

	_, err = parser.Parse("/cancel", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "cancel message id is empty")
}

func TestParserParseCancelRejectedWhenIDInvalid(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	parser := newParser(location)

	_, err = parser.Parse("/cancel abc", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "failed parse id")
}

func TestParserParseListRejected(t *testing.T) {
	location, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)

	parser := newParser(location)

	_, err = parser.Parse("/list", time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.ErrorIs(t, err, errbrick.ErrInvalidData)
	require.ErrorContains(t, err, "unsupported command")
}
