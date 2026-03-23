package task

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/stanza-go/framework/pkg/log"
)

// Pool manages a bounded set of worker goroutines for running fire-and-forget
// background tasks. Tasks are not persisted — if the process crashes, pending
// tasks are lost. For durable jobs, use the queue package instead.
type Pool struct {
	tasks   chan func()
	wg      sync.WaitGroup
	logger  *log.Logger
	workers int
	buffer  int
	stopped atomic.Bool

	// Stats counters — accessed atomically.
	submitted atomic.Int64
	completed atomic.Int64
	panics    atomic.Int64
	dropped   atomic.Int64
}

// Stats holds aggregate pool statistics.
type Stats struct {
	Workers   int   `json:"workers"`
	Buffer    int   `json:"buffer"`
	Pending   int   `json:"pending"`
	Submitted int64 `json:"submitted"`
	Completed int64 `json:"completed"`
	Panics    int64 `json:"panics"`
	Dropped   int64 `json:"dropped"`
}

// Option configures a Pool.
type Option func(*Pool)

// WithWorkers sets the number of concurrent worker goroutines. Defaults to 4.
func WithWorkers(n int) Option {
	return func(p *Pool) {
		if n > 0 {
			p.workers = n
		}
	}
}

// WithBuffer sets the task buffer capacity. When the buffer is full, Submit
// returns false. Defaults to 100.
func WithBuffer(n int) Option {
	return func(p *Pool) {
		if n > 0 {
			p.buffer = n
		}
	}
}

// WithLogger sets the logger for panic recovery and pool events.
func WithLogger(l *log.Logger) Option {
	return func(p *Pool) {
		p.logger = l
	}
}

// New creates a Pool with the given options. Call Start to launch workers.
func New(opts ...Option) *Pool {
	p := &Pool{
		workers: 4,
		buffer:  100,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Start launches worker goroutines. It is safe to call from a lifecycle hook.
func (p *Pool) Start(_ context.Context) error {
	p.tasks = make(chan func(), p.buffer)
	p.stopped.Store(false)
	for range p.workers {
		p.wg.Add(1)
		go p.worker()
	}
	return nil
}

// Stop signals the pool to stop accepting new tasks and waits for all
// in-flight tasks to complete. Pending tasks in the buffer are drained
// before returning.
func (p *Pool) Stop(_ context.Context) error {
	p.stopped.Store(true)
	close(p.tasks)
	p.wg.Wait()
	return nil
}

// Submit enqueues a function for background execution. Returns true if the
// task was accepted, false if the pool is full or stopped. A nil function
// is silently ignored.
func (p *Pool) Submit(fn func()) bool {
	if fn == nil {
		return true
	}
	if p.stopped.Load() {
		p.dropped.Add(1)
		return false
	}
	select {
	case p.tasks <- fn:
		p.submitted.Add(1)
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

// Stats returns a snapshot of pool statistics.
func (p *Pool) Stats() Stats {
	pending := len(p.tasks)
	return Stats{
		Workers:   p.workers,
		Buffer:    p.buffer,
		Pending:   pending,
		Submitted: p.submitted.Load(),
		Completed: p.completed.Load(),
		Panics:    p.panics.Load(),
		Dropped:   p.dropped.Load(),
	}
}

// worker processes tasks from the channel until it is closed.
func (p *Pool) worker() {
	defer p.wg.Done()
	for fn := range p.tasks {
		p.run(fn)
	}
}

// run executes a single task with panic recovery.
func (p *Pool) run(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			p.panics.Add(1)
			if p.logger != nil {
				p.logger.Error("task panic recovered",
					log.String("panic", stringifyPanic(r)),
				)
			}
		}
	}()
	fn()
	p.completed.Add(1)
}

// stringifyPanic converts a recovered panic value to a string.
func stringifyPanic(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	if s, ok := r.(string); ok {
		return s
	}
	return "unknown panic"
}
