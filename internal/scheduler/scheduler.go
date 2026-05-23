package scheduler

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler manages background cron jobs.
type Scheduler struct {
	cron *cron.Cron
}

type cronLogger struct{}

func (cronLogger) Info(msg string, keysAndValues ...interface{}) {
	// slog wants key-value pairs; keysAndValues is already key-value pairs
	slog.Info(msg, keysAndValues...)
}

func (cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	slog.Error(msg, append(keysAndValues, "error", err)...)
}

// NewScheduler initializes a Scheduler with the configured timezone.
func NewScheduler(timezone string) (*Scheduler, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	c := cron.New(cron.WithLocation(loc), cron.WithLogger(cronLogger{}))
	return &Scheduler{cron: c}, nil
}

// Start starts the cron scheduler asynchronously.
func (s *Scheduler) Start() {
	slog.Info("Starting cron scheduler...")
	s.cron.Start()
}

// Stop stops the cron scheduler.
func (s *Scheduler) Stop() {
	slog.Info("Stopping cron scheduler...")
	ctx := s.cron.Stop()
	// Wait for any running jobs to complete (up to 10 seconds)
	select {
	case <-ctx.Done():
		slog.Info("Cron scheduler stopped gracefully.")
	case <-time.After(10 * time.Second):
		slog.Warn("Cron scheduler stop timed out, force stopping.")
	}
}

// ScheduleDailyJob schedules a job to run daily at the configured HH:MM.
func (s *Scheduler) ScheduleDailyJob(scheduleTime string, job func()) error {
	parts := strings.Split(scheduleTime, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid schedule_time format: %q (expected HH:MM)", scheduleTime)
	}
	hour := parts[0]
	minute := parts[1]

	// Cron spec format: minute hour day-of-month month day-of-week
	cronExpr := fmt.Sprintf("%s %s * * *", minute, hour)
	slog.Info("Scheduling daily check job", "cronExpr", cronExpr, "time", scheduleTime)

	_, err := s.cron.AddFunc(cronExpr, func() {
		slog.Info("Triggering daily check job...")
		job()
	})
	if err != nil {
		return fmt.Errorf("failed to schedule daily job: %w", err)
	}

	return nil
}

// SchedulePurgeJob schedules the inactive user purge job daily at 04:00.
func (s *Scheduler) SchedulePurgeJob(job func()) error {
	cronExpr := "0 4 * * *"
	slog.Info("Scheduling inactive user purge job", "cronExpr", cronExpr)

	_, err := s.cron.AddFunc(cronExpr, func() {
		slog.Info("Triggering inactive user purge job...")
		job()
	})
	if err != nil {
		return fmt.Errorf("failed to schedule purge job: %w", err)
	}

	return nil
}
