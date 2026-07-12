package bot

import (
	"time"
)

type parsedCommand interface {
	isCommand()
}

type helpCommand struct{}

func (helpCommand) isCommand() {}

type upcomingCommand struct{}

func (upcomingCommand) isCommand() {}

type cancelCommand struct {
	id uint64
}

func (cancelCommand) isCommand() {}

type scheduleCommand struct {
	When      time.Time
	Recipient string
	Text      string
}

func (scheduleCommand) isCommand() {}

func commandName(cmd parsedCommand) string {
	switch cmd.(type) {
	case helpCommand:
		return "help"
	case upcomingCommand:
		return "upcoming"
	case cancelCommand:
		return "cancel"
	case scheduleCommand:
		return "schedule"
	default:
		return "unknown"
	}
}
