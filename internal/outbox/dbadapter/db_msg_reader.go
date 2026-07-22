package dbadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"go.etcd.io/bbolt"
)

type DBMessageReader struct {
	db     *bbolt.DB
	bucket []byte
}

func NewDBMessageReader(bucket []byte, db *bbolt.DB) *DBMessageReader {
	return &DBMessageReader{db: db, bucket: bucket}
}

func (r *DBMessageReader) Load(_ context.Context, predicate func(domain.Message) bool) ([]domain.Message, error) {
	if predicate == nil {
		predicate = func(domain.Message) bool { return true }
	}

	var messages []domain.Message
	err := r.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(r.bucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		return bucket.ForEach(func(_, value []byte) error {
			var msg domain.Message
			if err := json.Unmarshal(value, &msg); err != nil {
				return fmt.Errorf("failed decode outbox message: %w", err)
			}

			if predicate(msg) {
				messages = append(messages, msg)
			}

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed load outbox messages: %w", err)
	}

	return messages, nil
}
