package bot

import (
	"time"
)

type parsedCommand interface {
	isCommand()
	Name() string
}

type helpCommand struct{}

func (helpCommand) Name() string {
	return "help"
}

func (helpCommand) isCommand() {}

type upcomingCommand struct{}

func (upcomingCommand) Name() string {
	return "upcoming"
}

func (upcomingCommand) isCommand() {}

type cancelCommand struct {
	id uint64
}

func (cancelCommand) Name() string {
	return "cancel"
}
func (cancelCommand) isCommand() {}

type scheduleCommand struct {
	When              time.Time
	OriginalLocalTime string
	Timezone          string
	Recipient         string
	Text              string
}

func (scheduleCommand) Name() string {
	return "schedule"
}

func (scheduleCommand) isCommand() {}
