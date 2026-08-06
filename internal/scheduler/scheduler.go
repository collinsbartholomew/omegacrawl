package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Job describes a scheduled task with a cron expression and run function.
type Job struct {
	ID       string
	Name     string
	CronExpr string
	RunFunc  func(context.Context) error
}

// Scheduler manages a set of cron jobs and runs them on schedule.
type Scheduler struct {
	mu     sync.Mutex
	jobs   []*Job
	timers map[*time.Timer]struct{}
	cancel context.CancelFunc
}

// New returns an empty Scheduler.
func New() *Scheduler {
	return &Scheduler{timers: make(map[*time.Timer]struct{})}
}

// Add validates the job's cron expression and registers it with the scheduler.
func (s *Scheduler) Add(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := parseCron(job.CronExpr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", job.CronExpr, err)
	}
	s.jobs = append(s.jobs, job)
	return nil
}

// Start schedules all registered jobs to run according to their cron expressions.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	for _, job := range s.jobs {
		job := job
		go s.runJob(ctx, job)
	}
}

// Stop cancels all running jobs and stops pending timers.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	for t := range s.timers {
		t.Stop()
	}
	s.timers = make(map[*time.Timer]struct{})
	s.mu.Unlock()
}

func (s *Scheduler) runJob(ctx context.Context, job *Job) {
	next, err := parseCron(job.CronExpr)
	if err != nil {
		return
	}
	running := false
	for {
		now := time.Now()
		nextRun := next(now)
		if nextRun.Before(now) {
			nextRun = next(now.Add(time.Minute))
		}
		dur := time.Until(nextRun)
		if dur < 0 {
			dur = 0
		}

		timer := time.NewTimer(dur)
		s.mu.Lock()
		s.timers[timer] = struct{}{}
		s.mu.Unlock()

		select {
		case <-timer.C:
			s.mu.Lock()
			delete(s.timers, timer)
			s.mu.Unlock()

			if running {
				continue
			}
			running = true
			func() {
				defer func() { running = false }()
				job.RunFunc(ctx)
			}()
		case <-ctx.Done():
			timer.Stop()
			s.mu.Lock()
			delete(s.timers, timer)
			s.mu.Unlock()
			return
		}
	}
}

// parseCron returns a function that computes the next run time from a given time.
// Supports simplified cron expressions: "every <duration>" or "@every <duration>"
// or standard 5-field cron: "min hour dom mon dow"
func parseCron(expr string) (func(time.Time) time.Time, error) {
	expr = strings.TrimSpace(expr)

	if strings.HasPrefix(expr, "@every ") || strings.HasPrefix(expr, "every ") {
		durStr := strings.TrimPrefix(expr, "@every ")
		durStr = strings.TrimPrefix(durStr, "every ")
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, err
		}
		return func(t time.Time) time.Time {
			return t.Add(dur)
		}, nil
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields or '@every <duration>'")
	}

	return func(t time.Time) time.Time {
		next := t.Truncate(time.Minute)
		for i := 0; i < 525600; i++ {
			next = next.Add(time.Minute)
			if matchCron(fields, next) {
				return next
			}
		}
		return t.Add(24 * time.Hour)
	}, nil
}

func matchCron(fields []string, t time.Time) bool {
	dom := matchField(fields[2], t.Day(), 1, 31)
	month := matchField(fields[3], int(t.Month()), 1, 12)
	dow := matchField(fields[4], int(t.Weekday()), 0, 6)

	// Standard cron semantics: when both the day-of-month and day-of-week
	// fields are restricted (not "*"), the job fires when EITHER matches;
	// otherwise both must match.
	dayMatch := dom || dow
	if fields[2] == "*" || fields[4] == "*" {
		dayMatch = dom && dow
	}
	return matchField(fields[0], t.Minute(), 0, 59) &&
		matchField(fields[1], t.Hour(), 0, 23) &&
		month && dayMatch
}

func matchField(pattern string, value, min, max int) bool {
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == fmt.Sprintf("%d", value) {
			return true
		}
		if strings.Contains(p, "-") {
			var lo, hi int
			fmt.Sscanf(p, "%d-%d", &lo, &hi)
			if value >= lo && value <= hi {
				return true
			}
		}
		if strings.Contains(p, "/") {
			var step int
			fmt.Sscanf(p, "*/%d", &step)
			if step > 0 && (value-min)%step == 0 {
				return true
			}
		}
	}
	return false
}
