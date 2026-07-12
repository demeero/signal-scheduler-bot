package inbound

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/dbadapter"
	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
)

type Poller struct {
	signalClient *signaladapter.SignalAdapter
	parser       *parser
	dbAdapter    *dbadapter.OutboundAdapter
	account      string
}

func New(account string, location *time.Location, signalClient *signaladapter.SignalAdapter, dbAdapter *dbadapter.OutboundAdapter) *Poller {
	return &Poller{
		account:      account,
		signalClient: signalClient,
		parser:       newParser(location),
		dbAdapter:    dbAdapter,
	}
}

func (p *Poller) Poll(ctx context.Context) error {
	messages, err := p.signalClient.ReceiveSelfMessages(ctx)
	if err != nil {
		return fmt.Errorf("failed receive self messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	logger := logbrick.FromCtx(ctx)

	for _, msg := range messages {
		body := strings.TrimSpace(msg.Body)

		logger = logger.With("src_msg_id", msg.SourceMessageID)

		if !strings.HasPrefix(body, "/") {
			logger.Debug("ignore not command msg")
			continue
		}

		cmd, err := p.parser.Parse(body, time.Now().UTC())
		if err != nil {
			logger.Error("failed parse command", "err", err)
			p.storeSelfOutboundErr(ctx, err)
			continue
		}

		if err := p.handleCmd(ctx, cmd); err != nil {
			logger.Error("failed handle command", "err", err)
			p.storeSelfOutboundErr(ctx, err)
			continue
		}
	}

	return nil
}

func (p *Poller) handleCmd(ctx context.Context, cmd parsedCommand) error {
	switch c := cmd.(type) {
	case helpCommand:
		return p.handleHelpCmd(ctx)
	case listCommand:
		return p.handleListCmd(ctx)
	case cancelCommand:
		return p.handleCancelCmd(ctx, c)
	case scheduleCommand:
		return p.handleScheduleCmd(ctx, c)
	default:
		return fmt.Errorf("%w: unsupported command type", errbrick.ErrInvalidData)
	}
}

func (p *Poller) handleListCmd(ctx context.Context) error {
	panic("not implemented")
}

func (p *Poller) handleCancelCmd(ctx context.Context, cmd cancelCommand) error {
	panic("not implemented")
}

func (p *Poller) handleScheduleCmd(ctx context.Context, cmd scheduleCommand) error {
	resolvedRecipient, err := p.signalClient.ResolveRecipient(ctx, cmd.recipient)
	if err != nil {
		return fmt.Errorf("failed resolve recipient: %w", err)
	}

	if _, err := p.dbAdapter.InsertOutboundMessage(ctx, cmd.whenUTC, cmd.recipient, resolvedRecipient, cmd.message); err != nil {
		return fmt.Errorf("failed insert outbound message: %w", err)
	}

	return nil
}

func (p *Poller) handleHelpCmd(ctx context.Context) error {
	return p.signalClient.SendMessage(ctx, p.account, helpText())
}

func (p *Poller) storeSelfOutboundErr(ctx context.Context, err error) {
	if _, err := p.dbAdapter.InsertOutboundMessage(ctx, time.Now().UTC(), p.account, p.account, err.Error()); err != nil {
		logbrick.FromCtx(ctx).Error("failed to insert outbound message", "err", err)
	}
}

func helpText() string {
	return strings.Join([]string{
		"Available commands:",
		"",
		"/schedule YYYY-MM-DD HH:mm +380XXXXXXXXX Message text",
		"/schedule tomorrow HH:mm +380XXXXXXXXX Message text",
		"/schedule today HH:mm +380XXXXXXXXX Message text",
		"/schedule YYYY-MM-DD HH:mm \"Contact Name\" Message text",
		"/schedule tomorrow HH:mm \"Contact Name\" Message text",
		"/schedule today HH:mm \"Contact Name\" Message text",
		"",
		"/list",
		"",
		"/cancel MESSAGE_ID",
		"",
		"/help",
	}, "\n")
}
