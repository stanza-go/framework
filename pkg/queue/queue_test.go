package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stanza-go/framework/pkg/sqlite"
)

func testDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db := sqlite.New(":memory:")
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Stop(context.Background()) })
	return db
}

func TestNew(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if q.workers != 1 {
		t.Errorf("default workers = %d, want 1", q.workers)
	}
	if q.maxAttempts != 3 {
		t.Errorf("default maxAttempts = %d, want 3", q.maxAttempts)
	}
	if q.pollInterval != time.Second {
		t.Errorf("default pollInterval = %v, want 1s", q.pollInterval)
	}
	if q.retryDelay != 30*time.Second {
		t.Errorf("default retryDelay = %v, want 30s", q.retryDelay)
	}
}

func TestNewWithOptions(t *testing.T) {
	db := testDB(t)
	q := New(db,
		WithWorkers(4),
		WithPollInterval(500*time.Millisecond),
		WithMaxAttempts(5),
		WithRetryDelay(10*time.Second),
	)
	if q.workers != 4 {
		t.Errorf("workers = %d, want 4", q.workers)
	}
	if q.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want 5", q.maxAttempts)
	}
	if q.pollInterval != 500*time.Millisecond {
		t.Errorf("pollInterval = %v, want 500ms", q.pollInterval)
	}
	if q.retryDelay != 10*time.Second {
		t.Errorf("retryDelay = %v, want 10s", q.retryDelay)
	}
}

func TestEnqueue(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	id, err := q.Enqueue(ctx, "test_job", []byte(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}

	job, err := q.Job(id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Type != "test_job" {
		t.Errorf("type = %q, want %q", job.Type, "test_job")
	}
	if job.Status != StatusPending {
		t.Errorf("status = %q, want %q", job.Status, StatusPending)
	}
	if job.Queue != "default" {
		t.Errorf("queue = %q, want %q", job.Queue, "default")
	}
	if string(job.Payload) != `{"key":"value"}` {
		t.Errorf("payload = %q, want %q", string(job.Payload), `{"key":"value"}`)
	}
	if job.MaxAttempts != 3 {
		t.Errorf("max_attempts = %d, want 3", job.MaxAttempts)
	}
}

func TestEnqueueWithOptions(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	id, err := q.Enqueue(ctx, "delayed_job", nil,
		Delay(time.Hour),
		MaxAttempts(5),
		OnQueue("emails"),
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := q.Job(id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Queue != "emails" {
		t.Errorf("queue = %q, want %q", job.Queue, "emails")
	}
	if job.MaxAttempts != 5 {
		t.Errorf("max_attempts = %d, want 5", job.MaxAttempts)
	}
	// Delayed job should have run_at in the future.
	if !job.RunAt.After(time.Now().UTC().Add(50 * time.Minute)) {
		t.Errorf("run_at = %v, expected ~1 hour from now", job.RunAt)
	}
}

func TestEnqueueNilPayload(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "empty_job", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := q.Job(id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if string(job.Payload) != "{}" {
		t.Errorf("payload = %q, want %q", string(job.Payload), "{}")
	}
}

func TestStartStop(t *testing.T) {
	db := testDB(t)
	q := New(db)

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Double start should error.
	if err := q.Start(context.Background()); err == nil {
		t.Error("expected error on double start")
	}

	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Stop on unstarted should be ok.
	q2 := New(db)
	if err := q2.Stop(context.Background()); err != nil {
		t.Fatalf("stop unstarted: %v", err)
	}
}

func TestJobExecution(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond))

	var executed atomic.Bool
	q.Register("greet", func(ctx context.Context, payload []byte) error {
		executed.Store(true)
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "greet", []byte(`{"name":"world"}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Wait for job to be processed.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for job execution")
		default:
		}
		if executed.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify job is completed.
	job, err := q.Job(id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", job.Status, StatusCompleted)
	}
	if job.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", job.Attempts)
	}
	if job.CompletedAt.IsZero() {
		t.Error("completed_at should be set")
	}
}

func TestJobFailureAndRetry(t *testing.T) {
	db := testDB(t)
	q := New(db,
		WithPollInterval(50*time.Millisecond),
		WithMaxAttempts(2),
		WithRetryDelay(0), // no delay for testing
	)

	var attempts atomic.Int32
	q.Register("flaky", func(ctx context.Context, payload []byte) error {
		n := attempts.Add(1)
		if n < 2 {
			return fmt.Errorf("attempt %d failed", n)
		}
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "flaky", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Wait for job to complete (after retry).
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for retry")
		default:
		}
		j, _ := q.Job(id)
		if j.Status == StatusCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, _ := q.Job(id)
	if job.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", job.Attempts)
	}
}

func TestJobDeadLetter(t *testing.T) {
	db := testDB(t)
	q := New(db,
		WithPollInterval(50*time.Millisecond),
		WithMaxAttempts(2),
		WithRetryDelay(0),
	)

	q.Register("always_fail", func(ctx context.Context, payload []byte) error {
		return fmt.Errorf("permanent failure")
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "always_fail", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Wait for job to reach dead state.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for dead letter")
		default:
		}
		j, _ := q.Job(id)
		if j.Status == StatusDead {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, _ := q.Job(id)
	if job.Status != StatusDead {
		t.Errorf("status = %q, want %q", job.Status, StatusDead)
	}
	if job.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", job.Attempts)
	}
	if job.LastError != "permanent failure" {
		t.Errorf("last_error = %q, want %q", job.LastError, "permanent failure")
	}
	if job.CompletedAt.IsZero() {
		t.Error("completed_at should be set for dead jobs")
	}
}

func TestNoHandler(t *testing.T) {
	db := testDB(t)
	q := New(db,
		WithPollInterval(50*time.Millisecond),
		WithMaxAttempts(1),
	)
	// No handler registered for "unknown" type.

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "unknown", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}
		j, _ := q.Job(id)
		if j.Status == StatusDead {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, _ := q.Job(id)
	if job.Status != StatusDead {
		t.Errorf("status = %q, want %q", job.Status, StatusDead)
	}
	if job.LastError == "" {
		t.Error("expected error about missing handler")
	}
}

func TestStats(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour)) // don't process jobs
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	q.Enqueue(ctx, "a", nil)
	q.Enqueue(ctx, "b", nil)
	q.Enqueue(ctx, "c", nil)

	stats, err := q.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Pending != 3 {
		t.Errorf("pending = %d, want 3", stats.Pending)
	}
	if stats.Running != 0 {
		t.Errorf("running = %d, want 0", stats.Running)
	}
}

func TestJobsFilter(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "sms", nil, OnQueue("sms"))

	// Filter by queue.
	jobs, err := q.Jobs(Filter{Queue: "emails"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("filtered jobs = %d, want 2", len(jobs))
	}

	// Filter by type.
	jobs, err = q.Jobs(Filter{Type: "sms"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("filtered jobs = %d, want 1", len(jobs))
	}

	// All jobs.
	jobs, err = q.Jobs(Filter{})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("all jobs = %d, want 3", len(jobs))
	}

	// Limit.
	jobs, err = q.Jobs(Filter{Limit: 2})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("limited jobs = %d, want 2", len(jobs))
	}
}

func TestJobCount(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "sms", nil, OnQueue("sms"))

	// Count all.
	n, err := q.JobCount(Filter{})
	if err != nil {
		t.Fatalf("job count all: %v", err)
	}
	if n != 3 {
		t.Errorf("count all = %d, want 3", n)
	}

	// Count by queue.
	n, err = q.JobCount(Filter{Queue: "emails"})
	if err != nil {
		t.Fatalf("job count emails: %v", err)
	}
	if n != 2 {
		t.Errorf("count emails = %d, want 2", n)
	}

	// Count by type.
	n, err = q.JobCount(Filter{Type: "sms"})
	if err != nil {
		t.Fatalf("job count sms: %v", err)
	}
	if n != 1 {
		t.Errorf("count sms = %d, want 1", n)
	}

	// Count by status.
	n, err = q.JobCount(Filter{Status: StatusPending})
	if err != nil {
		t.Fatalf("job count pending: %v", err)
	}
	if n != 3 {
		t.Errorf("count pending = %d, want 3", n)
	}

	// Count with no matches.
	n, err = q.JobCount(Filter{Status: StatusCompleted})
	if err != nil {
		t.Fatalf("job count completed: %v", err)
	}
	if n != 0 {
		t.Errorf("count completed = %d, want 0", n)
	}
}

func TestCancel(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := q.Cancel(id); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	job, _ := q.Job(id)
	if job.Status != StatusCancelled {
		t.Errorf("status = %q, want %q", job.Status, StatusCancelled)
	}

	// Cancel again should fail.
	if err := q.Cancel(id); err == nil {
		t.Error("expected error on double cancel")
	}
}

func TestRetryDeadJob(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond), WithMaxAttempts(1), WithRetryDelay(0))

	var attempts atomic.Int32
	q.Register("revive", func(ctx context.Context, payload []byte) error {
		n := attempts.Add(1)
		if n <= 1 {
			return fmt.Errorf("fail first time")
		}
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, err := q.Enqueue(context.Background(), "revive", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Wait for dead state.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for dead")
		default:
		}
		j, _ := q.Job(id)
		if j.Status == StatusDead {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Retry the dead job.
	if err := q.Retry(id); err != nil {
		t.Fatalf("retry: %v", err)
	}

	// Wait for completion.
	deadline = time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for completion after retry")
		default:
		}
		j, _ := q.Job(id)
		if j.Status == StatusCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, _ := q.Job(id)
	if job.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", job.Status, StatusCompleted)
	}
}

func TestRecoverStuck(t *testing.T) {
	db := testDB(t)

	// Start a queue to create the table, then stop it.
	q1 := New(db)
	if err := q1.Start(context.Background()); err != nil {
		t.Fatalf("start q1: %v", err)
	}
	q1.Stop(context.Background())

	// Simulate a crashed job by manually setting status to running.
	id, _ := q1.Enqueue(context.Background(), "stuck", nil)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET status = ?, started_at = ? WHERE id = ?`,
		StatusRunning, now, id)

	// New queue should recover the stuck job on start.
	q2 := New(db, WithPollInterval(50*time.Millisecond))

	var executed atomic.Bool
	q2.Register("stuck", func(ctx context.Context, payload []byte) error {
		executed.Store(true)
		return nil
	})

	if err := q2.Start(context.Background()); err != nil {
		t.Fatalf("start q2: %v", err)
	}
	defer q2.Stop(context.Background())

	// The stuck job should be recovered and processed.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for stuck job recovery")
		default:
		}
		if executed.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, _ := q2.Job(id)
	if job.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", job.Status, StatusCompleted)
	}
}

func TestPurge(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	id, _ := q.Enqueue(ctx, "test", nil)

	// Manually mark as completed with old timestamp.
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET status = ?, updated_at = ? WHERE id = ?`,
		StatusCompleted, oldTime, id)

	deleted, err := q.Purge(24 * time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Should be gone.
	_, err = q.Job(id)
	if err == nil {
		t.Error("expected error for purged job")
	}
}

func TestJobNotFound(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	_, err := q.Job(999)
	if err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestDelayedJob(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond))

	var executed atomic.Bool
	q.Register("delayed", func(ctx context.Context, payload []byte) error {
		executed.Store(true)
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	// Enqueue with 1-hour delay.
	_, err := q.Enqueue(context.Background(), "delayed", nil, Delay(time.Hour))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Should NOT be executed within 200ms.
	time.Sleep(200 * time.Millisecond)
	if executed.Load() {
		t.Error("delayed job should not have executed yet")
	}
}

func TestMultipleWorkers(t *testing.T) {
	db := testDB(t)
	q := New(db, WithWorkers(4), WithPollInterval(50*time.Millisecond))

	var processed atomic.Int32
	q.Register("count", func(ctx context.Context, payload []byte) error {
		processed.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate work
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	// Enqueue several jobs.
	for range 10 {
		q.Enqueue(context.Background(), "count", nil)
	}

	// Wait for all to complete.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout: processed %d/10", processed.Load())
		default:
		}
		if processed.Load() == 10 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestGracefulShutdown(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond))

	started := make(chan struct{})
	finished := make(chan struct{})
	q.Register("slow", func(ctx context.Context, payload []byte) error {
		close(started)
		time.Sleep(200 * time.Millisecond)
		close(finished)
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	q.Enqueue(context.Background(), "slow", nil)

	// Wait for job to start.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Stop should wait for the job to finish.
	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	select {
	case <-finished:
		// good — job completed before stop returned
	default:
		t.Error("stop returned before job finished")
	}
}

func TestShutdownTimeout(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond))

	started := make(chan struct{})
	q.Register("very_slow", func(ctx context.Context, payload []byte) error {
		close(started)
		time.Sleep(5 * time.Second) // longer than shutdown timeout
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	q.Enqueue(context.Background(), "very_slow", nil)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job to start")
	}

	// Stop with a short timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := q.Stop(ctx)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestRetryNonFailedJob(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, _ := q.Enqueue(context.Background(), "test", nil)

	// Pending job can't be retried.
	if err := q.Retry(id); err == nil {
		t.Error("expected error retrying pending job")
	}
}

func TestCancelNonPendingJob(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, _ := q.Enqueue(context.Background(), "test", nil)
	// Mark as completed manually.
	now := time.Now().UTC().Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET status = ? WHERE id = ?`, StatusCompleted, id)
	_ = now

	// Can't cancel completed job.
	if err := q.Cancel(id); err == nil {
		t.Error("expected error cancelling completed job")
	}
}

func TestOptionsValidation(t *testing.T) {
	db := testDB(t)

	// Zero/negative workers should keep default.
	q := New(db, WithWorkers(0))
	if q.workers != 1 {
		t.Errorf("WithWorkers(0): workers = %d, want 1", q.workers)
	}
	q = New(db, WithWorkers(-1))
	if q.workers != 1 {
		t.Errorf("WithWorkers(-1): workers = %d, want 1", q.workers)
	}

	// Zero/negative maxAttempts should keep default.
	q = New(db, WithMaxAttempts(0))
	if q.maxAttempts != 3 {
		t.Errorf("WithMaxAttempts(0): maxAttempts = %d, want 3", q.maxAttempts)
	}
	q = New(db, WithMaxAttempts(-1))
	if q.maxAttempts != 3 {
		t.Errorf("WithMaxAttempts(-1): maxAttempts = %d, want 3", q.maxAttempts)
	}

	// Negative pollInterval should keep default.
	q = New(db, WithPollInterval(-1))
	if q.pollInterval != time.Second {
		t.Errorf("WithPollInterval(-1): pollInterval = %v, want 1s", q.pollInterval)
	}

	// Zero retryDelay is valid (retry immediately).
	q = New(db, WithRetryDelay(0))
	if q.retryDelay != 0 {
		t.Errorf("WithRetryDelay(0): retryDelay = %v, want 0", q.retryDelay)
	}

	// Negative retryDelay should keep default.
	q = New(db, WithRetryDelay(-1))
	if q.retryDelay != 30*time.Second {
		t.Errorf("WithRetryDelay(-1): retryDelay = %v, want 30s", q.retryDelay)
	}
}

func TestEnqueueMaxAttemptsOptionValidation(t *testing.T) {
	db := testDB(t)
	q := New(db)
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	// MaxAttempts(0) should keep queue default.
	id, err := q.Enqueue(context.Background(), "test", nil, MaxAttempts(0))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, _ := q.Job(id)
	if job.MaxAttempts != 3 {
		t.Errorf("MaxAttempts(0): max_attempts = %d, want 3", job.MaxAttempts)
	}

	// MaxAttempts(-1) should keep queue default.
	id, err = q.Enqueue(context.Background(), "test", nil, MaxAttempts(-1))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, _ = q.Job(id)
	if job.MaxAttempts != 3 {
		t.Errorf("MaxAttempts(-1): max_attempts = %d, want 3", job.MaxAttempts)
	}
}

func TestJobsFilterCombined(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "sms", nil, OnQueue("emails"))
	q.Enqueue(ctx, "email", nil, OnQueue("sms"))
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))

	// Filter by both queue AND type.
	jobs, err := q.Jobs(Filter{Queue: "emails", Type: "email"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("filtered by queue+type = %d, want 2", len(jobs))
	}

	// Filter by queue+type+status.
	jobs, err = q.Jobs(Filter{Queue: "emails", Type: "email", Status: StatusPending})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("filtered by queue+type+status = %d, want 2", len(jobs))
	}

	// Non-matching combined filter.
	jobs, err = q.Jobs(Filter{Queue: "emails", Type: "email", Status: StatusCompleted})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("filtered with no matches = %d, want 0", len(jobs))
	}
}

func TestJobsFilterOffset(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	for range 5 {
		q.Enqueue(ctx, "test", nil)
	}

	// Limit 2, offset 3 → should return 2 jobs.
	jobs, err := q.Jobs(Filter{Limit: 2, Offset: 3})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("jobs with limit=2 offset=3 = %d, want 2", len(jobs))
	}

	// Offset past end → should return 0.
	jobs, err = q.Jobs(Filter{Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("jobs past end = %d, want 0", len(jobs))
	}
}

func TestJobsDefaultLimit(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	// Default limit is 50 when Limit=0.
	ctx := context.Background()
	for range 3 {
		q.Enqueue(ctx, "test", nil)
	}

	jobs, err := q.Jobs(Filter{Limit: 0})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("jobs = %d, want 3 (all fit within default limit)", len(jobs))
	}
}

func TestRegisterOverwrite(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(50*time.Millisecond))

	var executed atomic.Int32

	q.Register("job", func(ctx context.Context, payload []byte) error {
		executed.Store(1)
		return nil
	})
	// Overwrite with new handler.
	q.Register("job", func(ctx context.Context, payload []byte) error {
		executed.Store(2)
		return nil
	})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	q.Enqueue(context.Background(), "job", nil)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
		}
		if executed.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if executed.Load() != 2 {
		t.Errorf("handler = %d, want 2 (second registration should overwrite)", executed.Load())
	}
}

func TestRetryFailedJob(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour), WithMaxAttempts(2), WithRetryDelay(0))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, _ := q.Enqueue(context.Background(), "test", nil)

	// Manually set to failed status.
	now := time.Now().UTC().Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET status = ?, last_error = 'oops', updated_at = ? WHERE id = ?`,
		StatusFailed, now, id)

	// Retry should work for failed jobs.
	if err := q.Retry(id); err != nil {
		t.Fatalf("retry failed job: %v", err)
	}

	job, _ := q.Job(id)
	if job.Status != StatusPending {
		t.Errorf("status = %q, want %q", job.Status, StatusPending)
	}
}

func TestJobCountCombinedFilter(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	ctx := context.Background()
	q.Enqueue(ctx, "email", nil, OnQueue("emails"))
	q.Enqueue(ctx, "sms", nil, OnQueue("emails"))
	q.Enqueue(ctx, "email", nil, OnQueue("sms"))

	n, err := q.JobCount(Filter{Queue: "emails", Type: "email"})
	if err != nil {
		t.Fatalf("job count: %v", err)
	}
	if n != 1 {
		t.Errorf("count queue=emails type=email = %d, want 1", n)
	}

	n, err = q.JobCount(Filter{Queue: "emails", Type: "email", Status: StatusPending})
	if err != nil {
		t.Fatalf("job count: %v", err)
	}
	if n != 1 {
		t.Errorf("count with status = %d, want 1", n)
	}
}

func TestWithLogger(t *testing.T) {
	db := testDB(t)
	// Just verify the option is accepted without panic.
	q := New(db, WithLogger(nil))
	if q.logger != nil {
		t.Error("logger should be nil")
	}
}

func TestPurgeCancelled(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, _ := q.Enqueue(context.Background(), "test", nil)
	q.Cancel(id)

	// Set old timestamp.
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET updated_at = ? WHERE id = ?`, oldTime, id)

	deleted, err := q.Purge(24 * time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

func TestPurgeDoesNotDeleteRecent(t *testing.T) {
	db := testDB(t)
	q := New(db, WithPollInterval(time.Hour))
	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer q.Stop(context.Background())

	id, _ := q.Enqueue(context.Background(), "test", nil)
	now := time.Now().UTC().Format(time.RFC3339)
	db.Exec(`UPDATE _queue_jobs SET status = ?, updated_at = ? WHERE id = ?`,
		StatusCompleted, now, id)

	deleted, err := q.Purge(24 * time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (recent job should not be purged)", deleted)
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		input string
		zero  bool
	}{
		{"", true},
		{"2026-03-21T10:00:00Z", false},
		{"invalid", true},
	}

	for _, tt := range tests {
		result := parseTime(tt.input)
		if tt.zero && !result.IsZero() {
			t.Errorf("parseTime(%q) = %v, want zero", tt.input, result)
		}
		if !tt.zero && result.IsZero() {
			t.Errorf("parseTime(%q) = zero, want non-zero", tt.input)
		}
	}
}
