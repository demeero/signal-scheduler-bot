package dbadapter

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
	"go.etcd.io/bbolt"
)

type DBMessageWriter struct {
	db     *bbolt.DB
	bucket []byte
}

func NewDBMessageWriter(bucket []byte, db *bbolt.DB) *DBMessageWriter {
	return &DBMessageWriter{db: db, bucket: bucket}
}

func (w *DBMessageWriter) Create(_ context.Context, cb func(id uint64) (domain.Message, error)) error {
	err := w.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(w.bucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		seq, err := bucket.NextSequence()
		if err != nil {
			return fmt.Errorf("failed generate outbox message ID: %w", err)
		}

		msg, err := cb(seq)
		if err != nil {
			return fmt.Errorf("failed create outbox message: %w", err)
		}

		return storeMessage(bucket, msg)
	})
	if err != nil {
		return fmt.Errorf("failed put outbox message: %w", err)
	}

	return nil
}

func (w *DBMessageWriter) Update(ctx context.Context, id uint64, updateFn func(context.Context, domain.Message) (domain.Message, error)) error {
	err := w.db.Update(func(tx *bbolt.Tx) error {
		msg, bucket, err := w.loadMessageForUpdate(tx, id)
		if err != nil {
			return err
		}

		updatedMsg, err := updateFn(ctx, msg)
		if err != nil {
			return fmt.Errorf("failed update outbox message: %w", err)
		}

		return storeMessage(bucket, updatedMsg)
	})
	if err != nil {
		return fmt.Errorf("failed update outbox message: %w", err)
	}

	return nil
}

func (w *DBMessageWriter) DeleteFrom(ctx context.Context, predicate func(domain.Message) bool) (uint, error) {
	var deleted uint

	err := w.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(w.bucket)
		if bucket == nil {
			return errors.New("outbox messages bucket does not exist")
		}

		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			msg, err := decodeMessage(value)
			if err != nil {
				return err
			}
			if !predicate(msg) {
				continue
			}

			if err := cursor.Delete(); err != nil {
				return fmt.Errorf("failed delete outbox message %d: %w", msg.ID, err)
			}
			deleted++
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return deleted, nil
}

func storeMessage(bucket *bbolt.Bucket, msg domain.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed encode outbox message: %w", err)
	}

	if err := bucket.Put(outboxMessageKey(msg.ID), data); err != nil {
		return fmt.Errorf("failed store outbox message: %w", err)
	}

	return nil
}

func (w *DBMessageWriter) loadMessageForUpdate(tx *bbolt.Tx, id uint64) (domain.Message, *bbolt.Bucket, error) {
	bucket := tx.Bucket(w.bucket)
	if bucket == nil {
		return domain.Message{}, nil, errors.New("outbox messages bucket does not exist")
	}

	raw := bucket.Get(outboxMessageKey(id))
	if raw == nil {
		return domain.Message{}, nil, fmt.Errorf("%w: outbox message %d", errbrick.ErrNotFound, id)
	}

	msg, err := decodeMessage(raw)
	if err != nil {
		return domain.Message{}, nil, err
	}

	return msg, bucket, nil
}

func outboxMessageKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)

	return key
}

func decodeMessage(raw []byte) (domain.Message, error) {
	var msg domain.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return domain.Message{}, fmt.Errorf("failed decode outbox message: %w", err)
	}

	return msg, nil
}
