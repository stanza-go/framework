package cron

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// Func is the signature for cron job functions. The context is cancelled when
// the scheduler stops. Jobs should respect cancellation for timely shutdown.
type Func func(ctx context.Context) error

// Entry is a read-only snapshot of a job's current state.
type Entry struct {
	Name     string
	Schedule string
	Enabled  bool
	Running  bool
	LastRun  time.Time
	NextRun  time.Time
	LastErr  error
}

// job is the internal representation of a registered cron job.
type job struct {
	name     string
	schedule schedule
	fn       Func
	enabled  bool
	running  bool
	lastRun  time.Time
	nextRun  time.Time
	lastErr  error
}

// CompletedRun contains information about a completed job execution. Passed to
// the OnComplete callback registered via WithOnComplete.
type CompletedRun struct {
	Name     string
	Started  time.Time
	Duration time.Duration
	Err      error
}

// SchedulerStats holds cumulative execution counters for the scheduler. Use
// Scheduler.Stats to obtain a snapshot.
type SchedulerStats struct {
	// Jobs is the total number of registered jobs.
	Jobs int `json:"jobs"`
	// Completed is the total number of successful job executions.
	Completed int64 `json:"completed"`
	// Failed is the total number of job executions that returned an error or panicked.
	Failed int64 `json:"failed"`
	// Skipped is the total number of times a due job was skipped because its
	// previous execution was still running.
	Skipped int64 `json:"skipped"`
}

// Scheduler manages registered cron jobs and executes them on schedule.
type Scheduler struct {
	mu         sync.Mutex
	jobs       []*job
	location   *time.Location
	logger     *log.Logger
	onComplete func(CompletedRun)
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	started    bool

	totalCompleted atomic.Int64
	totalFailed    atomic.Int64
	totalSkipped   atomic.Int64
}

// Option configures a Scheduler.
type Option func(*Scheduler)

// WithLocation sets the timezone for schedule evaluation. Defaults to
// time.UTC.
func WithLocation(loc *time.Location) Option {
	return func(s *Scheduler) {
		s.location = loc
	}
}

// WithLogger sets the logger for the scheduler. When nil, the scheduler
// operates silently.
func WithLogger(l *log.Logger) Option {
	return func(s *Scheduler) {
		s.logger = l
	}
}

// WithOnComplete registers a callback invoked after each job execution. The
// callback receives the job name, start time, duration, and any error. It runs
// in the same goroutine as the job, so it should return quickly. Use this to
// persist execution history to a database.
func WithOnComplete(fn func(CompletedRun)) Option {
	return func(s *Scheduler) {
		s.onComplete = fn
	}
}

// NewScheduler creates a new cron scheduler with the given options.
func NewScheduler(opts ...Option) *Scheduler {
	s := &Scheduler{
		location: time.UTC,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Add registers a named cron job with the given schedule expression. The
// expression uses standard 5-field format: minute hour day-of-month month
// day-of-week.
//
// Add must be called before Start. Adding jobs after Start is not supported
// and returns an error.
func (s *Scheduler) Add(name, expr string, fn Func) error {
	sched, err := parse(expr)
	if err != nil {
		return fmt.Errorf("cron: job %q: %w", name, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("cron: cannot add job %q after scheduler has started", name)
	}

	for _, j := range s.jobs {
		if j.name == name {
			return fmt.Errorf("cron: duplicate job name %q", name)
		}
	}

	s.jobs = append(s.jobs, &job{
		name:     name,
		schedule: sched,
		fn:       fn,
		enabled:  true,
	})

	return nil
}

// Start begins the scheduling loop. It calculates the next run time for each
// job and ticks every second to check for due jobs. Start blocks until the
// context is cancelled or Stop is called.
//
// Designed to be used as a lifecycle.Hook OnStart function.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("cron: scheduler already started")
	}
	s.started = true

	// Calculate initial next-run times.
	now := time.Now().In(s.location)
	for _, j := range s.jobs {
		if j.enabled {
			j.nextRun = j.schedule.next(now)
		}
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.ctx = runCtx
	s.cancel = cancel
	s.mu.Unlock()

	s.logInfo("cron scheduler started", log.Int("jobs", len(s.jobs)))

	// Run the tick loop in a background goroutine.
	s.wg.Add(1)
	go s.run(runCtx)

	return nil
}

// Stop signals the scheduler to stop and waits for all running jobs to
// complete. It respects the context deadline for shutdown.
//
// Designed to be used as a lifecycle.Hook OnStop function.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	cancel := s.cancel
	s.mu.Unlock()

	// Signal the run loop to stop.
	cancel()

	// Wait for the run loop and all running jobs to finish.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logInfo("cron scheduler stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cron: shutdown timed out: %w", ctx.Err())
	}
}

// Entries returns a snapshot of all registered jobs and their current state.
func (s *Scheduler) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]Entry, len(s.jobs))
	for i, j := range s.jobs {
		entries[i] = Entry{
			Name:     j.name,
			Schedule: j.schedule.expr,
			Enabled:  j.enabled,
			Running:  j.running,
			LastRun:  j.lastRun,
			NextRun:  j.nextRun,
			LastErr:  j.lastErr,
		}
	}
	return entries
}

// Stats returns a snapshot of cumulative execution counters. The counters are
// cumulative since the scheduler was created. Stats is safe to call
// concurrently from any goroutine.
func (s *Scheduler) Stats() SchedulerStats {
	s.mu.Lock()
	jobs := len(s.jobs)
	s.mu.Unlock()

	return SchedulerStats{
		Jobs:      jobs,
		Completed: s.totalCompleted.Load(),
		Failed:    s.totalFailed.Load(),
		Skipped:   s.totalSkipped.Load(),
	}
}

// Enable enables a previously disabled job by name. Returns an error if the
// job is not found.
func (s *Scheduler) Enable(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.jobs {
		if j.name == name {
			if !j.enabled {
				j.enabled = true
				j.nextRun = j.schedule.next(time.Now().In(s.location))
			}
			return nil
		}
	}
	return fmt.Errorf("cron: job %q not found", name)
}

// Disable disables a job by name. Disabled jobs are not executed but remain
// registered. Returns an error if the job is not found.
func (s *Scheduler) Disable(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.jobs {
		if j.name == name {
			j.enabled = false
			j.nextRun = time.Time{}
			return nil
		}
	}
	return fmt.Errorf("cron: job %q not found", name)
}

// Trigger runs a job immediately by name, regardless of its schedule. The job
// runs in a new goroutine. Returns an error if the job is not found or is
// already running.
func (s *Scheduler) Trigger(name string) error {
	s.mu.Lock()

	var target *job
	for _, j := range s.jobs {
		if j.name == name {
			target = j
			break
		}
	}
	if target == nil {
		s.mu.Unlock()
		return fmt.Errorf("cron: job %q not found", name)
	}
	if target.running {
		s.mu.Unlock()
		return fmt.Errorf("cron: job %q is already running", name)
	}

	// Use the scheduler's context if started, otherwise background.
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Unlock()

	s.wg.Add(1)
	go s.execute(ctx, target)
	return nil
}

// run is the main scheduling loop. It ticks every second and dispatches due
// jobs.
func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.tick(ctx, t.In(s.location))
		}
	}
}

// tick checks all jobs and dispatches those that are due.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := make([]*job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if !j.enabled {
			continue
		}
		if j.nextRun.IsZero() || now.Before(j.nextRun) {
			continue
		}
		if j.running {
			s.totalSkipped.Add(1)
			continue
		}
		due = append(due, j)
	}
	s.mu.Unlock()

	for _, j := range due {
		s.wg.Add(1)
		go s.execute(ctx, j)
	}
}

// execute runs a single job, updating its state before and after.
func (s *Scheduler) execute(ctx context.Context, j *job) {
	defer s.wg.Done()

	s.mu.Lock()
	j.running = true
	s.mu.Unlock()

	start := time.Now()
	s.logInfo("cron job started", log.String("job", j.name))

	err := s.safeRun(ctx, j)

	elapsed := time.Since(start)

	s.mu.Lock()
	j.running = false
	j.lastRun = start.In(s.location)
	j.lastErr = err
	if j.enabled {
		j.nextRun = j.schedule.next(time.Now().In(s.location))
	}
	onComplete := s.onComplete
	s.mu.Unlock()

	if onComplete != nil {
		onComplete(CompletedRun{
			Name:     j.name,
			Started:  start,
			Duration: elapsed,
			Err:      err,
		})
	}

	if err != nil {
		s.totalFailed.Add(1)
		s.logError("cron job failed",
			log.String("job", j.name),
			log.Duration("elapsed", elapsed),
			log.String("error", err.Error()),
		)
		return
	}

	s.totalCompleted.Add(1)
	s.logInfo("cron job completed",
		log.String("job", j.name),
		log.Duration("elapsed", elapsed),
	)
}

// safeRun executes the job function with panic recovery. If the function
// panics, the panic is caught and returned as an error instead of crashing
// the scheduler goroutine.
func (s *Scheduler) safeRun(ctx context.Context, j *job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return j.fn(ctx)
}

func (s *Scheduler) logInfo(msg string, fields ...log.Field) {
	if s.logger != nil {
		s.logger.Info(msg, fields...)
	}
}

func (s *Scheduler) logError(msg string, fields ...log.Field) {
	if s.logger != nil {
		s.logger.Error(msg, fields...)
	}
}
