package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/demeero/signal-scheduler-bot/internal/errbrick"
	"github.com/demeero/signal-scheduler-bot/internal/logbrick"
	"github.com/demeero/signal-scheduler-bot/internal/outbox"
	"github.com/demeero/signal-scheduler-bot/internal/signaladapter"
)

type Poller struct {
	signalClient *signaladapter.SignalAdapter
	parser       *parser
	outboxSvc    *outbox.Service
	location     *time.Location
	account      string
}

func New(account string, location *time.Location, signalClient *signaladapter.SignalAdapter, outboxSvc *outbox.Service) *Poller {
	return &Poller{
		account:      account,
		signalClient: signalClient,
		parser:       newParser(location),
		outboxSvc:    outboxSvc,
		location:     location,
	}
}

func (p *Poller) Poll(ctx context.Context) error {
	logger := logbrick.FromCtx(ctx)
	logger.Debug("polling inbound messages")

	messages, err := p.signalClient.ReceiveSelfMessages(ctx)
	if err != nil {
		return fmt.Errorf("failed receive self messages: %w", err)
	}
	if len(messages) == 0 {
		logger.Debug("no new inbound messages")
		return nil
	}

	logger.Debug(fmt.Sprintf("received %d inbound messages", len(messages)))

	for _, msg := range messages {
		body := strings.TrimSpace(msg.Body)
		msgLogger := logger.With("src_msg_id", msg.SourceMessageID)

		if !strings.HasPrefix(body, "/") {
			msgLogger.Debug("ignore not command msg")
			continue
		}

		cmd, err := p.parser.Parse(body, time.Now().UTC())
		if err != nil {
			msgLogger.Error("failed parse command", "err", err)
			p.queueSelfOutboxErr(ctx, err)
			continue
		}

		msgLogger.Debug("received command", "name", cmd.Name())

		if err := p.handleCmd(ctx, cmd); err != nil {
			msgLogger.Error("failed handle command", "err", err)
			p.queueSelfOutboxErr(ctx, err)
			continue
		}
	}

	return nil
}

func (p *Poller) handleCmd(ctx context.Context, cmd parsedCommand) error {
	switch c := cmd.(type) {
	case helpCommand:
		return p.handleHelpCmd(ctx)
	case upcomingCommand:
		return p.handleUpcomingCmd(ctx)
	case cancelCommand:
		return p.handleCancelCmd(ctx, c)
	case scheduleCommand:
		return p.handleScheduleCmd(ctx, c)
	default:
		return fmt.Errorf("%w: unsupported command type", errbrick.ErrInvalidData)
	}
}

func (p *Poller) handleUpcomingCmd(ctx context.Context) error {
	messages, err := p.outboxSvc.LoadUpcomingMessages(ctx)
	if err != nil {
		return fmt.Errorf("failed list outbox messages: %w", err)
	}

	lines := make([]string, 0, len(messages)+1)
	lines = append(lines, fmt.Sprintf("Upcoming messages: %d", len(messages)))
	for _, msg := range messages {
		lines = append(lines, fmt.Sprintf(
			"%d | %s (%s) | %s | %s",
			msg.ID,
			msg.ScheduledAt.In(p.location).Format("2006-01-02 15:04"),
			p.location.String(),
			msg.Recipient,
			msg.Text,
		))
	}

	return p.queueSelfOutboxMessage(ctx, strings.Join(lines, "\n"))
}

func (p *Poller) handleCancelCmd(ctx context.Context, cmd cancelCommand) error {
	_, err := p.outboxSvc.CancelMessage(ctx, cmd.id)
	if err != nil {
		return fmt.Errorf("failed cancel outbox message: %w", err)
	}

	return p.queueSelfOutboxMessage(ctx, fmt.Sprintf("Cancelled message %d.", cmd.id))
}

func (p *Poller) handleScheduleCmd(ctx context.Context, cmd scheduleCommand) error {
	recipientIdentifier, err := p.signalClient.ResolveRecipient(ctx, cmd.Recipient)
	if err != nil {
		return fmt.Errorf("failed resolve recipient: %w", err)
	}

	params := outbox.CreateMessageParams{
		ScheduledAt:         cmd.When,
		Recipient:           cmd.Recipient,
		RecipientIdentifier: recipientIdentifier,
		Text:                cmd.Text,
	}
	outboxMessage, err := p.outboxSvc.CreateMessage(ctx, params)
	if err != nil {
		return fmt.Errorf("failed create outbox message: %w", err)
	}

	return p.queueSelfOutboxMessage(
		ctx,
		fmt.Sprintf("Scheduled message %d for %s (%s) to %s.", outboxMessage.ID, cmd.OriginalLocalTime, cmd.Timezone, cmd.Recipient),
	)
}

func (p *Poller) handleHelpCmd(ctx context.Context) error {
	helpText := strings.Join([]string{
		"Available commands:",
		"",
		"/schedule YYYY-MM-DD HH:mm +380XXXXXXXXX Message text",
		"/schedule tomorrow HH:mm +380XXXXXXXXX Message text",
		"/schedule today HH:mm +380XXXXXXXXX Message text",
		"/schedule YYYY-MM-DD HH:mm \"Contact Name\" Message text",
		"/schedule tomorrow HH:mm \"Contact Name\" Message text",
		"/schedule today HH:mm \"Contact Name\" Message text",
		"",
		"/upcoming",
		"",
		"/cancel MESSAGE_ID",
		"",
		"/help",
	}, "\n")
	return p.queueSelfOutboxMessage(ctx, helpText)
}

func (p *Poller) queueSelfOutboxErr(ctx context.Context, err error) {
	if err := p.queueSelfOutboxMessage(ctx, err.Error()); err != nil {
		logbrick.FromCtx(ctx).Error("failed to queue self outbox error message", "err", err)
	}
}

func (p *Poller) queueSelfOutboxMessage(ctx context.Context, text string) error {
	params := outbox.CreateMessageParams{
		ScheduledAt:         time.Now().UTC(),
		Recipient:           p.account,
		RecipientIdentifier: p.account,
		Text:                text,
	}
	if _, err := p.outboxSvc.CreateMessage(ctx, params); err != nil {
		return fmt.Errorf("failed queue self message: %w", err)
	}

	logbrick.FromCtx(ctx).Debug("self outbox message queued", "msg", text)

	return nil
}
