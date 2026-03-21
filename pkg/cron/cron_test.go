package cron

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// === Parse Tests ===

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"every minute", "* * * * *", false},
		{"specific values", "30 2 15 6 3", false},
		{"ranges", "0-30 9-17 * * 1-5", false},
		{"steps", "*/5 */2 * * *", false},
		{"range with step", "0-30/10 * * * *", false},
		{"lists", "0,15,30,45 * * * *", false},
		{"complex", "0,30 9-17/2 1,15 1-6 1-5", false},

		// Errors.
		{"too few fields", "* * *", true},
		{"too many fields", "* * * * * *", true},
		{"empty string", "", true},
		{"minute out of range", "60 * * * *", true},
		{"hour out of range", "* 24 * * *", true},
		{"dom out of range", "* * 32 * *", true},
		{"dom zero", "* * 0 * *", true},
		{"month out of range", "* * * 13 *", true},
		{"month zero", "* * * 0 *", true},
		{"dow out of range", "* * * * 7", true},
		{"invalid step", "*/0 * * * *", true},
		{"invalid range", "5-3 * * * *", true},
		{"non-numeric", "abc * * * *", true},
	}

	for _, tt := range tests {
		s, err := parse(tt.expr)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error for %q, got nil", tt.name, tt.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error for %q: %v", tt.name, tt.expr, err)
			continue
		}
		if s.expr != tt.expr {
			t.Errorf("%s: expr = %q, want %q", tt.name, s.expr, tt.expr)
		}
	}
}

func TestScheduleMatches(t *testing.T) {
	// 2026-03-21 14:30:00 is a Saturday (weekday=6).
	base := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		expr  string
		time  time.Time
		match bool
	}{
		{"every minute matches", "* * * * *", base, true},
		{"exact match", "30 14 21 3 6", base, true},
		{"minute mismatch", "0 14 21 3 6", base, false},
		{"hour mismatch", "30 15 21 3 6", base, false},
		{"day mismatch", "30 14 20 3 6", base, false},
		{"month mismatch", "30 14 21 4 6", base, false},
		{"dow mismatch", "30 14 21 3 5", base, false},
		{"range match", "25-35 14 * * *", base, true},
		{"step match", "*/10 * * * *", base, true},       // 30 is divisible by 10
		{"step no match", "*/7 * * * *", base, false},     // 30 is not in {0,7,14,21,28,35,...}
		{"list match", "15,30,45 * * * *", base, true},
		{"list no match", "0,15,45 * * * *", base, false},
	}

	for _, tt := range tests {
		s, err := parse(tt.expr)
		if err != nil {
			t.Fatalf("%s: parse error: %v", tt.name, err)
		}
		got := s.matches(tt.time)
		if got != tt.match {
			t.Errorf("%s: matches(%v) = %v, want %v", tt.name, tt.time, got, tt.match)
		}
	}
}

func TestScheduleNext(t *testing.T) {
	// 2026-03-21 14:30:00 Saturday.
	from := time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		expr string
		want time.Time
	}{
		{
			"every minute",
			"* * * * *",
			time.Date(2026, 3, 21, 14, 31, 0, 0, time.UTC),
		},
		{
			"next hour",
			"0 * * * *",
			time.Date(2026, 3, 21, 15, 0, 0, 0, time.UTC),
		},
		{
			"specific time tomorrow",
			"0 9 * * *",
			time.Date(2026, 3, 22, 9, 0, 0, 0, time.UTC),
		},
		{
			"next Monday (weekday 1)",
			"0 0 * * 1",
			time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		},
		{
			"step expression",
			"*/15 * * * *",
			time.Date(2026, 3, 21, 14, 45, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		s, err := parse(tt.expr)
		if err != nil {
			t.Fatalf("%s: parse error: %v", tt.name, err)
		}
		got := s.next(from)
		if !got.Equal(tt.want) {
			t.Errorf("%s: next(%v) = %v, want %v", tt.name, from, got, tt.want)
		}
	}
}

func TestScheduleNextSkipsCurrentMinute(t *testing.T) {
	// next() should not return the current minute even if it matches.
	from := time.Date(2026, 3, 21, 14, 0, 0, 0, time.UTC)
	s, _ := parse("0 14 * * *")
	got := s.next(from)
	// Should be the next day at 14:00, not the current minute.
	want := time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("next(%v) = %v, want %v", from, got, want)
	}
}

// === Scheduler Tests ===

func TestSchedulerAdd(t *testing.T) {
	s := NewScheduler()

	if err := s.Add("job1", "* * * * *", func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Duplicate name.
	err := s.Add("job1", "*/5 * * * *", func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}

	// Bad expression.
	err = s.Add("job2", "bad", func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected error for bad expression")
	}
}

func TestSchedulerAddAfterStart(t *testing.T) {
	s := NewScheduler()
	ctx := context.Background()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(ctx)

	err := s.Add("late", "* * * * *", func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected error adding job after start")
	}
}

func TestSchedulerEntries(t *testing.T) {
	s := NewScheduler()
	s.Add("alpha", "*/5 * * * *", func(ctx context.Context) error { return nil })
	s.Add("beta", "0 * * * *", func(ctx context.Context) error { return nil })

	entries := s.Entries()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Name != "alpha" || entries[1].Name != "beta" {
		t.Errorf("unexpected entry names: %v, %v", entries[0].Name, entries[1].Name)
	}
	if !entries[0].Enabled || !entries[1].Enabled {
		t.Error("entries should be enabled by default")
	}
}

func TestSchedulerEnableDisable(t *testing.T) {
	s := NewScheduler()
	s.Add("job", "* * * * *", func(ctx context.Context) error { return nil })

	if err := s.Disable("job"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	entries := s.Entries()
	if entries[0].Enabled {
		t.Error("expected job to be disabled")
	}

	if err := s.Enable("job"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	entries = s.Entries()
	if !entries[0].Enabled {
		t.Error("expected job to be enabled")
	}

	// Not found.
	if err := s.Enable("nonexistent"); err == nil {
		t.Error("expected error for nonexistent job")
	}
	if err := s.Disable("nonexistent"); err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	s := NewScheduler()
	s.Add("noop", "* * * * *", func(ctx context.Context) error { return nil })

	ctx := context.Background()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Double start should fail.
	if err := s.Start(ctx); err == nil {
		t.Fatal("expected error on double start")
	}

	// NextRun should be set.
	entries := s.Entries()
	if entries[0].NextRun.IsZero() {
		t.Error("expected NextRun to be set after start")
	}

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Stop on not-started scheduler is fine.
	s2 := NewScheduler()
	if err := s2.Stop(ctx); err != nil {
		t.Fatalf("Stop on unstarted: %v", err)
	}
}

func TestSchedulerTrigger(t *testing.T) {
	var called atomic.Int32
	s := NewScheduler()
	s.Add("trigger-test", "0 0 1 1 *", func(ctx context.Context) error {
		called.Add(1)
		return nil
	})

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	if err := s.Trigger("trigger-test"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	// Wait for execution.
	deadline := time.After(2 * time.Second)
	for called.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("trigger-test was not called within 2s")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if got := called.Load(); got != 1 {
		t.Errorf("called = %d, want 1", got)
	}

	// Trigger nonexistent.
	if err := s.Trigger("nonexistent"); err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestSchedulerTriggerWhileRunning(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})

	s := NewScheduler()
	s.Add("slow", "0 0 1 1 *", func(ctx context.Context) error {
		close(started)
		<-block
		return nil
	})

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	s.Trigger("slow")
	<-started

	// Should fail because it's already running.
	err := s.Trigger("slow")
	if err == nil {
		t.Error("expected error when job is already running")
	}

	close(block)
}

func TestSchedulerJobExecution(t *testing.T) {
	// This test verifies that the tick mechanism dispatches jobs.
	// We use a schedule that matches every minute and set nextRun to the
	// past after Start() so the tick fires immediately.
	var called atomic.Int32

	s := NewScheduler()
	s.Add("fast", "* * * * *", func(ctx context.Context) error {
		called.Add(1)
		return nil
	})

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	// Override nextRun to the past so the next tick dispatches immediately.
	s.mu.Lock()
	s.jobs[0].nextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()

	// Wait for at least one execution.
	deadline := time.After(3 * time.Second)
	for called.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("job was not executed within 3s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestSchedulerJobError(t *testing.T) {
	errBoom := errors.New("boom")
	s := NewScheduler()
	s.Add("failing", "0 0 1 1 *", func(ctx context.Context) error {
		return errBoom
	})

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	s.Trigger("failing")

	// Wait for execution.
	deadline := time.After(2 * time.Second)
	for {
		entries := s.Entries()
		if entries[0].LastErr != nil {
			if !errors.Is(entries[0].LastErr, errBoom) {
				t.Errorf("LastErr = %v, want %v", entries[0].LastErr, errBoom)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("job error was not recorded within 2s")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestSchedulerDisabledJobNotExecuted(t *testing.T) {
	var called atomic.Int32
	s := NewScheduler()
	s.Add("disabled", "* * * * *", func(ctx context.Context) error {
		called.Add(1)
		return nil
	})
	s.Disable("disabled")

	ctx := context.Background()
	s.Start(ctx)

	time.Sleep(2 * time.Second)
	s.Stop(ctx)

	if got := called.Load(); got != 0 {
		t.Errorf("disabled job was called %d times", got)
	}
}

func TestSchedulerGracefulShutdown(t *testing.T) {
	started := make(chan struct{})
	var finished atomic.Bool

	s := NewScheduler()
	s.Add("slow-job", "0 0 1 1 *", func(ctx context.Context) error {
		close(started)
		time.Sleep(500 * time.Millisecond)
		finished.Store(true)
		return nil
	})

	ctx := context.Background()
	s.Start(ctx)

	s.Trigger("slow-job")
	<-started

	// Stop should wait for the slow job.
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if !finished.Load() {
		t.Error("Stop returned before slow job completed")
	}
}

func TestSchedulerShutdownTimeout(t *testing.T) {
	block := make(chan struct{})
	s := NewScheduler()
	s.Add("blocker", "0 0 1 1 *", func(ctx context.Context) error {
		<-block
		return nil
	})

	ctx := context.Background()
	s.Start(ctx)
	s.Trigger("blocker")

	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Stop with a very short timeout.
	stopCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err := s.Stop(stopCtx)
	if err == nil {
		t.Error("expected timeout error")
	}

	close(block)
	// Allow goroutines to drain.
	time.Sleep(100 * time.Millisecond)
}

func TestSchedulerContextCancellation(t *testing.T) {
	var gotCancel atomic.Bool
	s := NewScheduler()
	s.Add("cancellable", "0 0 1 1 *", func(ctx context.Context) error {
		<-ctx.Done()
		gotCancel.Store(true)
		return ctx.Err()
	})

	ctx := context.Background()
	s.Start(ctx)
	s.Trigger("cancellable")

	time.Sleep(50 * time.Millisecond)
	s.Stop(ctx)

	// The context should have been cancelled.
	time.Sleep(100 * time.Millisecond)
	if !gotCancel.Load() {
		t.Error("job context was not cancelled on stop")
	}
}

// === Concurrency Tests ===

func TestSchedulerConcurrentEntries(t *testing.T) {
	s := NewScheduler()
	for i := 0; i < 10; i++ {
		name := "job-" + time.Now().Format("150405.000") + "-" + string(rune('a'+i))
		s.Add(name, "* * * * *", func(ctx context.Context) error { return nil })
	}

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Entries()
		}()
	}
	wg.Wait()
}

// === Location Tests ===

func TestSchedulerWithLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("timezone America/New_York not available")
	}

	s := NewScheduler(WithLocation(loc))
	s.Add("tz-test", "* * * * *", func(ctx context.Context) error { return nil })

	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop(ctx)

	entries := s.Entries()
	if entries[0].NextRun.Location() != loc {
		t.Errorf("NextRun location = %v, want %v", entries[0].NextRun.Location(), loc)
	}
}

// === Bitset Field Tests ===

func TestParseFieldStar(t *testing.T) {
	bits, err := parseField("*", 0, 59)
	if err != nil {
		t.Fatalf("parseField: %v", err)
	}
	// All bits 0-59 should be set.
	for i := 0; i <= 59; i++ {
		if bits&(1<<uint(i)) == 0 {
			t.Errorf("bit %d not set", i)
		}
	}
}

func TestParseFieldStep(t *testing.T) {
	bits, err := parseField("*/15", 0, 59)
	if err != nil {
		t.Fatalf("parseField: %v", err)
	}
	expected := []int{0, 15, 30, 45}
	for _, v := range expected {
		if bits&(1<<uint(v)) == 0 {
			t.Errorf("bit %d not set for */15", v)
		}
	}
	// 5 should not be set.
	if bits&(1<<5) != 0 {
		t.Error("bit 5 should not be set for */15")
	}
}

func TestParseFieldRange(t *testing.T) {
	bits, err := parseField("1-5", 0, 6)
	if err != nil {
		t.Fatalf("parseField: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if bits&(1<<uint(i)) == 0 {
			t.Errorf("bit %d not set for 1-5", i)
		}
	}
	if bits&1 != 0 {
		t.Error("bit 0 should not be set for 1-5")
	}
	if bits&(1<<6) != 0 {
		t.Error("bit 6 should not be set for 1-5")
	}
}

func TestParseFieldList(t *testing.T) {
	bits, err := parseField("0,15,30,45", 0, 59)
	if err != nil {
		t.Fatalf("parseField: %v", err)
	}
	for _, v := range []int{0, 15, 30, 45} {
		if bits&(1<<uint(v)) == 0 {
			t.Errorf("bit %d not set", v)
		}
	}
	if bits&(1<<1) != 0 {
		t.Error("bit 1 should not be set")
	}
}

func TestParseFieldRangeWithStep(t *testing.T) {
	bits, err := parseField("0-30/10", 0, 59)
	if err != nil {
		t.Fatalf("parseField: %v", err)
	}
	expected := []int{0, 10, 20, 30}
	for _, v := range expected {
		if bits&(1<<uint(v)) == 0 {
			t.Errorf("bit %d not set for 0-30/10", v)
		}
	}
	if bits&(1<<5) != 0 {
		t.Error("bit 5 should not be set for 0-30/10")
	}
}

// === OnComplete Callback Tests ===

func TestOnComplete_Success(t *testing.T) {
	var mu sync.Mutex
	var runs []CompletedRun

	s := NewScheduler(WithOnComplete(func(r CompletedRun) {
		mu.Lock()
		runs = append(runs, r)
		mu.Unlock()
	}))

	if err := s.Add("cb-job", "* * * * *", func(_ context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	if err := s.Trigger("cb-job"); err != nil {
		t.Fatal(err)
	}

	// Wait for callback.
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(runs)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for onComplete callback")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	r := runs[0]
	mu.Unlock()

	if r.Name != "cb-job" {
		t.Errorf("name = %q, want cb-job", r.Name)
	}
	if r.Err != nil {
		t.Errorf("err = %v, want nil", r.Err)
	}
	if r.Duration <= 0 {
		t.Errorf("duration = %v, want > 0", r.Duration)
	}
	if r.Started.IsZero() {
		t.Error("started is zero")
	}
}

func TestWithLogger(t *testing.T) {
	logger := log.New(log.WithWriter(io.Discard))
	s := NewScheduler(WithLogger(logger))

	if err := s.Add("logged-job", "* * * * *", func(_ context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	// Trigger to exercise logInfo path.
	if err := s.Trigger("logged-job"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries := s.Entries()
		if entries[0].Running {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if !entries[0].LastRun.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for job")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWithLoggerError(t *testing.T) {
	logger := log.New(log.WithWriter(io.Discard))
	s := NewScheduler(WithLogger(logger))

	if err := s.Add("fail-logged", "* * * * *", func(_ context.Context) error {
		return errors.New("intentional failure for logging")
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	// Trigger to exercise logError path.
	if err := s.Trigger("fail-logged"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries := s.Entries()
		if entries[0].Running {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if !entries[0].LastRun.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for job")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOnComplete_Error(t *testing.T) {
	var mu sync.Mutex
	var runs []CompletedRun

	s := NewScheduler(WithOnComplete(func(r CompletedRun) {
		mu.Lock()
		runs = append(runs, r)
		mu.Unlock()
	}))

	jobErr := errors.New("job failed")
	if err := s.Add("fail-job", "* * * * *", func(_ context.Context) error {
		return jobErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())

	if err := s.Trigger("fail-job"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(runs)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for onComplete callback")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	r := runs[0]
	mu.Unlock()

	if r.Name != "fail-job" {
		t.Errorf("name = %q, want fail-job", r.Name)
	}
	if !errors.Is(r.Err, jobErr) {
		t.Errorf("err = %v, want %v", r.Err, jobErr)
	}
}
