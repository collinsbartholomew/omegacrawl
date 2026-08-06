package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMatchCronStandardSemantics(t *testing.T) {
	// "0 0 1 * 1": minute 0, hour 0, dom 1, any month, dow Monday(1).
	fields := []string{"0", "0", "1", "*", "1"}

	// 2026-01-01 is a Thursday (dow 4) but dom=1 -> matches via dom.
	if !matchCron(fields, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("dom=1 should match even though dow is not Monday")
	}
	// 2026-01-05 is a Monday (dow 1) but dom=5 -> matches via dow.
	if !matchCron(fields, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Error("dow=Monday should match even though dom is not 1")
	}
	// 2026-01-06 is a Tuesday, dom=6 -> matches neither.
	if matchCron(fields, time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)) {
		t.Error("dom=6 Tuesday should not match")
	}
	// Wrong hour should not match.
	if matchCron(fields, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)) {
		t.Error("hour 1 should not match hour 0 cron")
	}
}

func TestMatchCronSingleRestrictedDay(t *testing.T) {
	// "0 0 * * 1": only dow restricted -> Monday only.
	fields := []string{"0", "0", "*", "*", "1"}
	// Tuesday, dom 1 -> must NOT match (dow restriction enforced alone).
	if matchCron(fields, time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)) {
		t.Error("Tuesday should not match Monday-only cron")
	}
	if !matchCron(fields, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Error("Monday should match Monday-only cron")
	}
}

func TestSchedulerRunsJobOnSchedule(t *testing.T) {
	var runs int32
	s := New()
	err := s.Add(&Job{
		ID:       "test",
		Name:     "Test Job",
		CronExpr: "@every 20ms",
		RunFunc: func(ctx context.Context) error {
			atomic.AddInt32(&runs, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&runs) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&runs) < 2 {
		t.Fatalf("expected at least 2 runs, got %d", runs)
	}

	s.Stop()
}

func TestSchedulerRejectsInvalidCron(t *testing.T) {
	s := New()
	err := s.Add(&Job{ID: "bad", CronExpr: "not a cron", RunFunc: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}
