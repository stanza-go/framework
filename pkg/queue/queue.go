// Package queue provides a SQLite-backed job queue with in-process workers.
// Jobs are enqueued with a type and JSON payload, then processed by registered
// handler functions. Failed jobs are retried with configurable backoff, and
// permanently failed jobs are moved to a dead-letter state.
//
// Basic usage:
//
//	q := queue.New(db)
//	q.Register("send_email", func(ctx context.Context, payload []byte) error {
//	    // process the job
//	    return nil
//	})
//
// Integration with lifecycle:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: q.Start,
//	    OnStop:  q.Stop,
//	})
//
// Enqueuing jobs:
//
//	id, err := q.Enqueue(ctx, "send_email", []byte(`{"to":"user@example.com"}`))
//	id, err := q.Enqueue(ctx, "report", payload, queue.Delay(time.Hour))
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/stanza-go/framework/pkg/log"
	"github.com/stanza-go/framework/pkg/sqlite"
)

// Status constants for job state.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusDead      = "dead"
	StatusCancelled = "cancelled"
)

// HandlerFunc processes a job. The payload is the raw JSON bytes that were
// enqueued. The context is cancelled when the queue is stopping.
type HandlerFunc func(ctx context.Context, payload []byte) error

// Job represents a job in the queue.
type Job struct {
	ID          int64
	Queue       string
	Type        string
	Payload     []byte
	Status      string
	Attempts    int
	MaxAttempts int
	LastError   string
	RunAt       time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Stats holds aggregate counts of jobs by status.
type Stats struct {
	Pending   int
	Running   int
	Completed int
	Failed    int
	Dead      int
	Cancelled int
}

// Filter controls which jobs are returned by the Jobs method.
type Filter struct {
	Queue  string // filter by queue name (empty = all)
	Type   string // filter by job type (empty = all)
	Status string // filter by status (empty = all)
	Limit  int    // max results (0 = 50)
	Offset int    // pagination offset
}

// Queue is a SQLite-backed job queue with in-process workers.
type Queue struct {
	db           *sqlite.DB
	handlers     map[string]HandlerFunc
	mu           sync.Mutex
	workers      int
	pollInterval time.Duration
	maxAttempts  int
	retryDelay   time.Duration
	logger       *log.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	started      bool
}

// Option configures a Queue.
type Option func(*Queue)

// WithWorkers sets the number of concurrent worker goroutines. Defaults to 1.
func WithWorkers(n int) Option {
	return func(q *Queue) {
		if n > 0 {
			q.workers = n
		}
	}
}

// WithPollInterval sets how frequently workers check for new jobs.
// Defaults to 1 second.
func WithPollInterval(d time.Duration) Option {
	return func(q *Queue) {
		if d > 0 {
			q.pollInterval = d
		}
	}
}

// WithLogger sets the logger for queue events.
func WithLogger(l *log.Logger) Option {
	return func(q *Queue) {
		q.logger = l
	}
}

// WithMaxAttempts sets the default maximum number of attempts for a job.
// Defaults to 3.
func WithMaxAttempts(n int) Option {
	return func(q *Queue) {
		if n > 0 {
			q.maxAttempts = n
		}
	}
}

// WithRetryDelay sets the base delay between retry attempts. The actual delay
// is multiplied by the attempt number for linear backoff. A value of 0 means
// retry immediately. Defaults to 30 seconds.
func WithRetryDelay(d time.Duration) Option {
	return func(q *Queue) {
		if d >= 0 {
			q.retryDelay = d
		}
	}
}

// EnqueueOption configures a single enqueue call.
type EnqueueOption func(*enqueueConfig)

type enqueueConfig struct {
	queue       string
	delay       time.Duration
	maxAttempts int
}

// Delay postpones job execution by the given duration.
func Delay(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		c.delay = d
	}
}

// MaxAttempts overrides the default max attempts for this job.
func MaxAttempts(n int) EnqueueOption {
	return func(c *enqueueConfig) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// OnQueue specifies which queue the job should be placed on.
// Defaults to "default".
func OnQueue(name string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.queue = name
	}
}

// New creates a new Queue backed by the given database. The queue is not
// active until Start is called.
func New(db *sqlite.DB, opts ...Option) *Queue {
	q := &Queue{
		db:           db,
		handlers:     make(map[string]HandlerFunc),
		workers:      1,
		pollInterval: time.Second,
		maxAttempts:  3,
		retryDelay:   30 * time.Second,
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// Register registers a handler for the given job type. Must be called
// before Start.
func (q *Queue) Register(jobType string, handler HandlerFunc) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[jobType] = handler
}

// Enqueue adds a new job to the queue. Returns the job ID on success.
func (q *Queue) Enqueue(_ context.Context, jobType string, payload []byte, opts ...EnqueueOption) (int64, error) {
	cfg := enqueueConfig{
		queue:       "default",
		maxAttempts: q.maxAttempts,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if len(payload) == 0 {
		payload = []byte("{}")
	}

	now := time.Now().UTC()
	runAt := now
	if cfg.delay > 0 {
		runAt = now.Add(cfg.delay)
	}

	nowStr := now.Format(time.RFC3339)
	runAtStr := runAt.Format(time.RFC3339)

	res, err := q.db.Exec(
		`INSERT INTO _queue_jobs (queue, type, payload, status, attempts, max_attempts, run_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		cfg.queue, jobType, string(payload), StatusPending, cfg.maxAttempts, runAtStr, nowStr, nowStr,
	)
	if err != nil {
		return 0, fmt.Errorf("queue: enqueue: %w", err)
	}

	q.logInfo("job enqueued",
		log.Int64("id", res.LastInsertID),
		log.String("type", jobType),
		log.String("queue", cfg.queue),
	)

	return res.LastInsertID, nil
}

// Start creates the jobs table if needed and launches worker goroutines.
// It returns immediately after starting workers. Designed to be used as
// a lifecycle.Hook OnStart function.
func (q *Queue) Start(_ context.Context) error {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return fmt.Errorf("queue: already started")
	}

	if err := q.createTable(); err != nil {
		q.mu.Unlock()
		return err
	}

	// Recover any jobs stuck in running state from a previous crash.
	if err := q.recoverStuck(); err != nil {
		q.mu.Unlock()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.ctx = ctx
	q.cancel = cancel
	q.started = true
	q.mu.Unlock()

	q.logInfo("queue started",
		log.Int("workers", q.workers),
		log.Int("handlers", len(q.handlers)),
	)

	for i := range q.workers {
		q.wg.Add(1)
		go q.worker(ctx, i)
	}

	return nil
}

// Stop signals workers to stop and waits for in-flight jobs to complete.
// Respects the context deadline for shutdown. Designed to be used as
// a lifecycle.Hook OnStop function.
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	if !q.started {
		q.mu.Unlock()
		return nil
	}
	cancel := q.cancel
	q.mu.Unlock()

	cancel()

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		q.logInfo("queue stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("queue: shutdown timed out: %w", ctx.Err())
	}
}

// Stats returns aggregate job counts grouped by status.
func (q *Queue) Stats() (Stats, error) {
	var s Stats
	rows, err := q.db.Query(
		`SELECT status, COUNT(*) FROM _queue_jobs GROUP BY status`,
	)
	if err != nil {
		return s, fmt.Errorf("queue: stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return s, fmt.Errorf("queue: stats scan: %w", err)
		}
		switch status {
		case StatusPending:
			s.Pending = count
		case StatusRunning:
			s.Running = count
		case StatusCompleted:
			s.Completed = count
		case StatusFailed:
			s.Failed = count
		case StatusDead:
			s.Dead = count
		case StatusCancelled:
			s.Cancelled = count
		}
	}
	if err := rows.Err(); err != nil {
		return s, fmt.Errorf("queue: stats rows: %w", err)
	}
	return s, nil
}

// Job returns a single job by ID.
func (q *Queue) Job(id int64) (Job, error) {
	var j Job
	var payload, lastError, runAt, startedAt, completedAt, createdAt, updatedAt string

	err := q.db.QueryRow(
		`SELECT id, queue, type, payload, status, attempts, max_attempts,
		        COALESCE(last_error, ''), run_at, COALESCE(started_at, ''),
		        COALESCE(completed_at, ''), created_at, updated_at
		 FROM _queue_jobs WHERE id = ?`, id,
	).Scan(&j.ID, &j.Queue, &j.Type, &payload, &j.Status, &j.Attempts,
		&j.MaxAttempts, &lastError, &runAt, &startedAt, &completedAt,
		&createdAt, &updatedAt)

	if errors.Is(err, sqlite.ErrNoRows) {
		return j, fmt.Errorf("queue: job %d not found", id)
	}
	if err != nil {
		return j, fmt.Errorf("queue: job: %w", err)
	}

	j.Payload = []byte(payload)
	j.LastError = lastError
	j.RunAt = parseTime(runAt)
	j.StartedAt = parseTime(startedAt)
	j.CompletedAt = parseTime(completedAt)
	j.CreatedAt = parseTime(createdAt)
	j.UpdatedAt = parseTime(updatedAt)

	return j, nil
}

// JobCount returns the total number of jobs matching the given filter.
// The Limit and Offset fields of the filter are ignored.
func (q *Queue) JobCount(f Filter) (int, error) {
	query := `SELECT COUNT(*) FROM _queue_jobs WHERE 1=1`
	var args []any

	if f.Queue != "" {
		query += " AND queue = ?"
		args = append(args, f.Queue)
	}
	if f.Type != "" {
		query += " AND type = ?"
		args = append(args, f.Type)
	}
	if f.Status != "" {
		query += " AND status = ?"
		args = append(args, f.Status)
	}

	var count int
	err := q.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("queue: job count: %w", err)
	}
	return count, nil
}

// Jobs returns jobs matching the given filter.
func (q *Queue) Jobs(f Filter) ([]Job, error) {
	query := `SELECT id, queue, type, payload, status, attempts, max_attempts,
	                 COALESCE(last_error, ''), run_at, COALESCE(started_at, ''),
	                 COALESCE(completed_at, ''), created_at, updated_at
	          FROM _queue_jobs WHERE 1=1`
	var args []any

	if f.Queue != "" {
		query += " AND queue = ?"
		args = append(args, f.Queue)
	}
	if f.Type != "" {
		query += " AND type = ?"
		args = append(args, f.Type)
	}
	if f.Status != "" {
		query += " AND status = ?"
		args = append(args, f.Status)
	}

	query += " ORDER BY id DESC"

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)
	if f.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, f.Offset)
	}

	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("queue: jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		var payload, lastError, runAt, startedAt, completedAt, createdAt, updatedAt string
		if err := rows.Scan(&j.ID, &j.Queue, &j.Type, &payload, &j.Status,
			&j.Attempts, &j.MaxAttempts, &lastError, &runAt, &startedAt,
			&completedAt, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("queue: jobs scan: %w", err)
		}
		j.Payload = []byte(payload)
		j.LastError = lastError
		j.RunAt = parseTime(runAt)
		j.StartedAt = parseTime(startedAt)
		j.CompletedAt = parseTime(completedAt)
		j.CreatedAt = parseTime(createdAt)
		j.UpdatedAt = parseTime(updatedAt)
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queue: jobs rows: %w", err)
	}

	return jobs, nil
}

// Retry requeues a failed or dead job for another attempt. The job's status
// is set to pending, its run_at is set to now, and max_attempts is increased
// by one to allow the retry.
func (q *Queue) Retry(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.db.Exec(
		`UPDATE _queue_jobs
		 SET status = ?, run_at = ?, max_attempts = max_attempts + 1, updated_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		StatusPending, now, now, id, StatusFailed, StatusDead,
	)
	if err != nil {
		return fmt.Errorf("queue: retry: %w", err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("queue: retry: job %d not found or not in failed/dead state", id)
	}

	q.logInfo("job retried", log.Int64("id", id))
	return nil
}

// Cancel cancels a pending job. Only pending jobs can be cancelled.
func (q *Queue) Cancel(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.db.Exec(
		`UPDATE _queue_jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		StatusCancelled, now, id, StatusPending,
	)
	if err != nil {
		return fmt.Errorf("queue: cancel: %w", err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("queue: cancel: job %d not found or not pending", id)
	}

	q.logInfo("job cancelled", log.Int64("id", id))
	return nil
}

// Purge deletes completed and cancelled jobs older than the given duration.
func (q *Queue) Purge(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	res, err := q.db.Exec(
		`DELETE FROM _queue_jobs WHERE status IN (?, ?) AND updated_at < ?`,
		StatusCompleted, StatusCancelled, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("queue: purge: %w", err)
	}
	return res.RowsAffected, nil
}

// worker is the main loop for a single worker goroutine.
func (q *Queue) worker(ctx context.Context, id int) {
	defer q.wg.Done()

	ticker := time.NewTicker(q.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.poll(ctx, id)
		}
	}
}

// poll attempts to claim and process one job.
func (q *Queue) poll(ctx context.Context, workerID int) {
	job, ok := q.claim()
	if !ok {
		return
	}

	q.logInfo("job started",
		log.Int64("id", job.ID),
		log.String("type", job.Type),
		log.Int("worker", workerID),
		log.Int("attempt", job.Attempts),
	)

	q.mu.Lock()
	handler, exists := q.handlers[job.Type]
	q.mu.Unlock()

	start := time.Now()

	if !exists {
		q.fail(job, fmt.Errorf("no handler registered for job type %q", job.Type))
		return
	}

	err := q.safeHandle(ctx, handler, job.Payload)
	elapsed := time.Since(start)

	if err != nil {
		q.logError("job failed",
			log.Int64("id", job.ID),
			log.String("type", job.Type),
			log.Duration("elapsed", elapsed),
			log.String("error", err.Error()),
		)
		q.fail(job, err)
		return
	}

	q.complete(job)
	q.logInfo("job completed",
		log.Int64("id", job.ID),
		log.String("type", job.Type),
		log.Duration("elapsed", elapsed),
	)
}

// safeHandle executes the handler with panic recovery. If the handler panics,
// the panic is caught and returned as an error instead of crashing the worker
// goroutine.
func (q *Queue) safeHandle(ctx context.Context, handler HandlerFunc, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return handler(ctx, payload)
}

// claim atomically picks the next available job and marks it as running.
func (q *Queue) claim() (Job, bool) {
	now := time.Now().UTC().Format(time.RFC3339)

	var j Job
	var payload, runAt, createdAt, updatedAt string

	// Use a transaction to atomically select and update.
	err := q.db.InTx(func(tx *sqlite.Tx) error {
		row := tx.QueryRow(
			`SELECT id, queue, type, payload, attempts, max_attempts, run_at, created_at, updated_at
			 FROM _queue_jobs
			 WHERE status = ? AND run_at <= ?
			 ORDER BY run_at ASC
			 LIMIT 1`,
			StatusPending, now,
		)
		if err := row.Scan(&j.ID, &j.Queue, &j.Type, &payload, &j.Attempts,
			&j.MaxAttempts, &runAt, &createdAt, &updatedAt); err != nil {
			return err
		}

		j.Attempts++
		_, err := tx.Exec(
			`UPDATE _queue_jobs SET status = ?, attempts = ?, started_at = ?, updated_at = ? WHERE id = ?`,
			StatusRunning, j.Attempts, now, now, j.ID,
		)
		return err
	})

	if err != nil {
		// ErrNoRows is expected when the queue is empty.
		return Job{}, false
	}

	j.Status = StatusRunning
	j.Payload = []byte(payload)
	j.RunAt = parseTime(runAt)
	j.StartedAt = parseTime(now)
	j.CreatedAt = parseTime(createdAt)
	j.UpdatedAt = parseTime(now)

	return j, true
}

// complete marks a job as successfully completed.
func (q *Queue) complete(j Job) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = q.db.Exec(
		`UPDATE _queue_jobs SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		StatusCompleted, now, now, j.ID,
	)
}

// fail handles a job failure. If the job has remaining attempts, it is
// rescheduled with a delay. Otherwise, it is moved to the dead state.
func (q *Queue) fail(j Job, jobErr error) {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	errMsg := jobErr.Error()

	if j.Attempts < j.MaxAttempts {
		// Reschedule for retry with exponential-ish backoff.
		retryAt := now.Add(q.retryDelay * time.Duration(j.Attempts))
		retryAtStr := retryAt.Format(time.RFC3339)
		_, _ = q.db.Exec(
			`UPDATE _queue_jobs SET status = ?, last_error = ?, run_at = ?, updated_at = ? WHERE id = ?`,
			StatusPending, errMsg, retryAtStr, nowStr, j.ID,
		)
		q.logInfo("job scheduled for retry",
			log.Int64("id", j.ID),
			log.String("type", j.Type),
			log.Int("attempt", j.Attempts),
			log.Int("max_attempts", j.MaxAttempts),
		)
		return
	}

	// Max attempts exhausted — move to dead.
	_, _ = q.db.Exec(
		`UPDATE _queue_jobs SET status = ?, last_error = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		StatusDead, errMsg, nowStr, nowStr, j.ID,
	)
	q.logError("job moved to dead letter",
		log.Int64("id", j.ID),
		log.String("type", j.Type),
		log.Int("attempts", j.Attempts),
	)
}

// createTable ensures the _queue_jobs table exists.
func (q *Queue) createTable() error {
	_, err := q.db.Exec(`CREATE TABLE IF NOT EXISTS _queue_jobs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		queue       TEXT    NOT NULL DEFAULT 'default',
		type        TEXT    NOT NULL,
		payload     TEXT    NOT NULL DEFAULT '{}',
		status      TEXT    NOT NULL DEFAULT 'pending',
		attempts    INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3,
		last_error  TEXT,
		run_at      TEXT    NOT NULL,
		started_at  TEXT,
		completed_at TEXT,
		created_at  TEXT    NOT NULL,
		updated_at  TEXT    NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("queue: create table: %w", err)
	}

	_, err = q.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_queue_jobs_dequeue
		 ON _queue_jobs(status, run_at) WHERE status = 'pending'`,
	)
	if err != nil {
		return fmt.Errorf("queue: create index: %w", err)
	}

	_, err = q.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_queue_jobs_status ON _queue_jobs(status)`,
	)
	if err != nil {
		return fmt.Errorf("queue: create index: %w", err)
	}

	return nil
}

// recoverStuck resets jobs left in running state from a previous crash
// back to pending so they can be retried.
func (q *Queue) recoverStuck() error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.db.Exec(
		`UPDATE _queue_jobs SET status = ?, updated_at = ? WHERE status = ?`,
		StatusPending, now, StatusRunning,
	)
	if err != nil {
		return fmt.Errorf("queue: recover stuck: %w", err)
	}
	if res.RowsAffected > 0 {
		q.logInfo("recovered stuck jobs", log.Int64("count", res.RowsAffected))
	}
	return nil
}

// parseTime parses an RFC3339 time string. Returns zero time on failure.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (q *Queue) logInfo(msg string, fields ...log.Field) {
	if q.logger != nil {
		q.logger.Info(msg, fields...)
	}
}

func (q *Queue) logError(msg string, fields ...log.Field) {
	if q.logger != nil {
		q.logger.Error(msg, fields...)
	}
}
