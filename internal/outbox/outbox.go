package outbox

import (
	"fmt"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/command"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
	"go.etcd.io/bbolt"
)

var outboxMessagesBucket = []byte("outbox_messages")

type Commands struct {
	Cancel  *command.CancelMessage
	Create  *command.CreateMessage
	SendDue *command.SendDueMessages
	Vacuum  *command.VacuumMessages
}

type Outbox struct {
	Queries  *QueryService
	Commands *Commands
}

func New(maxAttempts uint16, maxAge, vacuumAge time.Duration, db *bbolt.DB, signalAdapter *signaladapter.SignalAdapter) (*Outbox, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(outboxMessagesBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed initialize outbox service: %w", err)
	}

	dbReader := dbadapter.NewDBMessageReader(outboxMessagesBucket, db)
	dbWriter := dbadapter.NewDBMessageWriter(outboxMessagesBucket, db)

	queries := NewQueryService(dbReader)
	commands := &Commands{
		Cancel:  command.NewCancelMesssage(dbWriter),
		Create:  command.NewCreateMessage(maxAttempts, dbWriter),
		SendDue: command.NewSendDueMessages(maxAge, dbWriter, dbReader, signalAdapter),
		Vacuum:  command.NewVacuumMessages(vacuumAge, dbWriter),
	}

	return &Outbox{
		Queries:  queries,
		Commands: commands,
	}, nil
}
