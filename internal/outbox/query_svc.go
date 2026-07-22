package outbox

import (
	"context"
	"fmt"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/outbox/domain"
)

type QueryService struct {
	dbMsgReader *dbadapter.DBMessageReader
}

func NewQueryService(dbMsgReader *dbadapter.DBMessageReader) *QueryService {
	return &QueryService{dbMsgReader: dbMsgReader}
}

func (s *QueryService) LoadUpcomingMessages(ctx context.Context) ([]domain.Message, error) {
	var messages []domain.Message

	messages, err := s.dbMsgReader.Load(ctx, func(m domain.Message) bool { return m.IsUpcoming() })
	if err != nil {
		return nil, fmt.Errorf("failed load upcoming messages: %w", err)
	}

	domain.SortMessagesByScheduledAt(messages)
	return messages, nil
}

// LoadHistoryMessages returns retained outbox messages ordered by most recent update.
func (s *QueryService) LoadHistoryMessages(ctx context.Context, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: history limit must be positive", errbrick.ErrInvalidData)
	}

	messages, err := s.dbMsgReader.Load(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed load history messages: %w", err)
	}

	domain.SortMessagesByUpdatedAt(messages)
	if len(messages) > limit {
		messages = messages[:limit]
	}

	return messages, nil
}
