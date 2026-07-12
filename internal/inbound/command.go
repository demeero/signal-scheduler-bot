package inbound

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

type listCommand struct{}

func (listCommand) Name() string {
	return "list"
}
func (listCommand) isCommand() {}

type cancelCommand struct {
	id int64
}

func (cancelCommand) Name() string {
	return "cancel"
}
func (cancelCommand) isCommand() {}

type scheduleCommand struct {
	whenUTC           time.Time
	originalLocalTime string
	timezone          string
	recipient         string
	message           string
}

func (scheduleCommand) Name() string {
	return "schedule"
}

func (scheduleCommand) isCommand() {}
