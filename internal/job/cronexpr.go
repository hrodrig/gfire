package job

import (
	"time"

	"github.com/robfig/cron/v3"
)

// RecurringCronParser matches engine scheduling: 6-field expressions with seconds
// (robfig/cron WithSeconds) plus descriptors such as @every / @hourly.
func RecurringCronParser() cron.Parser {
	return cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
}

// ParseRecurringCron validates and parses a recurring cron expression.
func ParseRecurringCron(expr string) (cron.Schedule, error) {
	return RecurringCronParser().Parse(expr)
}

// NextRecurringRun returns the next fire time after t for expr.
func NextRecurringRun(expr string, t time.Time) (time.Time, error) {
	sched, err := ParseRecurringCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(t), nil
}
