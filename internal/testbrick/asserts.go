package testbrick

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func AssertOptionalTimeEqual(t *testing.T, expected, actual *time.Time, tolerance time.Duration) {
	t.Helper()

	if expected == nil {
		require.Nil(t, actual)
		return
	}

	require.NotNil(t, actual)
	require.WithinDuration(t, expected.UTC(), actual.UTC(), tolerance)
}
