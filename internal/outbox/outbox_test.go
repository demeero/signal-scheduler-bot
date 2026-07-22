package outbox

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func TestNew_InitializesOutboxDependenciesAndBucket(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, &bolt.Options{Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	adapter := signaladapter.New("+380500000000", "http://signal.test", &http.Client{Timeout: time.Second})
	box, err := New(5, 15*time.Minute, 30*24*time.Hour, db, adapter)
	require.NoError(t, err)
	require.NotNil(t, box.Queries)
	require.NotNil(t, box.Commands)
	require.NotNil(t, box.Commands.Create)
	require.NotNil(t, box.Commands.Cancel)
	require.NotNil(t, box.Commands.SendDue)
	require.NotNil(t, box.Commands.Vacuum)

	require.NoError(t, db.View(func(tx *bolt.Tx) error {
		require.NotNil(t, tx.Bucket(outboxMessagesBucket))
		return nil
	}))
}
