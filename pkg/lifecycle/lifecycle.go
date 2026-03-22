// Package lifecycle provides dependency injection and application lifecycle
// management. It resolves constructor dependencies automatically via
// topological sort and orchestrates startup and shutdown hooks in the
// correct order.
//
// Basic usage:
//
//	app := lifecycle.New(
//	    lifecycle.Provide(
//	        func() *Config { return loadConfig() },
//	        func(cfg *Config, lc *lifecycle.Lifecycle) *Server {
//	            srv := &Server{Addr: cfg.Addr}
//	            lc.Append(lifecycle.Hook{
//	                OnStart: func(ctx context.Context) error {
//	                    go srv.ListenAndServe()
//	                    return nil
//	                },
//	                OnStop: func(ctx context.Context) error {
//	                    return srv.Shutdown(ctx)
//	                },
//	            })
//	            return srv
//	        },
//	    ),
//	    lifecycle.Invoke(func(srv *Server) {
//	        fmt.Println("server configured at", srv.Addr)
//	    }),
//	)
//	if err := app.Run(); err != nil {
//	    log.Fatal(err)
//	}
//
// Constructors registered with Provide are called in dependency order during
// New. A *Lifecycle is automatically available for injection — constructors
// that need startup/shutdown behavior request it as a parameter and call
// Append to register hooks.
//
// Hooks run in registration order on Start and reverse order on Stop. If a
// start hook fails, only hooks that were successfully started are stopped.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Hook is a pair of start and stop callbacks for lifecycle management.
// Both fields are optional. OnStart is called during App.Start and OnStop
// is called during App.Stop in reverse order.
type Hook struct {
	OnStart func(context.Context) error
	OnStop  func(context.Context) error
}

// Lifecycle manages ordered startup and shutdown hooks. Constructors that
// need to perform work at startup or cleanup at shutdown request a
// *Lifecycle parameter and call Append to register hooks.
//
// Lifecycle is safe for concurrent use by multiple goroutines.
type Lifecycle struct {
	mu      sync.Mutex
	hooks   []Hook
	started int
}

// Append adds a hook to the lifecycle. Hooks are started in the order they
// are appended and stopped in reverse order.
func (l *Lifecycle) Append(h Hook) {
	l.mu.Lock()
	l.hooks = append(l.hooks, h)
	l.mu.Unlock()
}

// start runs all OnStart hooks in order. It tracks how many hooks started
// successfully so that stop only cleans up hooks that were started.
func (l *Lifecycle) start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, h := range l.hooks {
		if h.OnStart != nil {
			if err := h.OnStart(ctx); err != nil {
				return fmt.Errorf("lifecycle: start hook %d: %w", i, err)
			}
		}
		l.started = i + 1
	}
	return nil
}

// stop runs OnStop hooks in reverse order, only for hooks that were
// successfully started. It continues on errors and returns all errors
// joined. It is safe to call stop multiple times.
func (l *Lifecycle) stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error
	for i := l.started - 1; i >= 0; i-- {
		if l.hooks[i].OnStop != nil {
			if err := l.hooks[i].OnStop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("lifecycle: stop hook %d: %w", i, err))
			}
		}
	}
	l.started = 0
	return errors.Join(errs...)
}

// App manages dependency injection and application lifecycle. It resolves
// constructor dependencies, calls invoke functions, and orchestrates
// startup and shutdown hooks.
//
// Create an App with New, then call Run to start the application and block
// until a shutdown signal is received.
type App struct {
	lc           *Lifecycle
	err          error
	startTimeout time.Duration
	stopTimeout  time.Duration
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// Option configures an App.
type Option func(*config)

type config struct {
	provides     []any
	invokes      []any
	startTimeout time.Duration
	stopTimeout  time.Duration
}

// Provide registers constructor functions with the dependency injection
// container. Constructors declare their dependencies as parameters and
// return the types they provide. Supported signatures:
//
//	func() T
//	func() (T, error)
//	func(A, B) T
//	func(A, B) (T, error)
//	func(A) (T, U, error)
//
// Constructors are called at most once during initialization, and results
// are cached as singletons. The order of Provide calls does not matter —
// dependencies are resolved automatically via topological sort.
//
// A *Lifecycle is always available for injection without being explicitly
// provided.
func Provide(constructors ...any) Option {
	return func(c *config) {
		c.provides = append(c.provides, constructors...)
	}
}

// Invoke registers functions that execute during app initialization, after
// all constructors have been called. Arguments are resolved from the DI
// container. Invoke functions are called in the order they are registered.
//
// If an invoke function's last return value is an error and it is non-nil,
// initialization fails and App.Err returns the error.
func Invoke(funcs ...any) Option {
	return func(c *config) {
		c.invokes = append(c.invokes, funcs...)
	}
}

// WithStartTimeout sets the maximum duration for running all OnStart hooks.
// The default is 15 seconds.
func WithStartTimeout(d time.Duration) Option {
	return func(c *config) {
		c.startTimeout = d
	}
}

// WithStopTimeout sets the maximum duration for running all OnStop hooks.
// The default is 15 seconds.
func WithStopTimeout(d time.Duration) Option {
	return func(c *config) {
		c.stopTimeout = d
	}
}

// New creates an App, resolves all dependencies, and calls invoke functions.
// If any step fails, the returned App's Err method returns the error.
// The App can still be used — Start and Run will return the error
// immediately.
func New(opts ...Option) *App {
	cfg := &config{
		startTimeout: 15 * time.Second,
		stopTimeout:  15 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	lc := &Lifecycle{}
	app := &App{
		lc:           lc,
		startTimeout: cfg.startTimeout,
		stopTimeout:  cfg.stopTimeout,
		shutdown:     make(chan struct{}),
	}

	ctr := newContainer()
	ctr.supply(lc)

	for _, p := range cfg.provides {
		if err := ctr.provide(p); err != nil {
			app.err = err
			return app
		}
	}

	if err := ctr.resolve(); err != nil {
		app.err = err
		return app
	}

	for _, fn := range cfg.invokes {
		if err := ctr.call(fn); err != nil {
			app.err = err
			return app
		}
	}

	return app
}

// Err returns the initialization error, if any. A non-nil error means
// that dependency resolution or an invoke function failed during New.
func (a *App) Err() error {
	return a.err
}

// Start runs all OnStart lifecycle hooks in order. If the App has an
// initialization error, Start returns it immediately without running any
// hooks. If a hook fails, Start returns the error and subsequent hooks
// are not run. The caller should call Stop to clean up hooks that were
// already started.
func (a *App) Start(ctx context.Context) error {
	if a.err != nil {
		return a.err
	}
	return a.lc.start(ctx)
}

// Stop runs all OnStop lifecycle hooks in reverse order. Only hooks whose
// OnStart was successfully called are stopped. Stop continues on errors
// and returns all errors joined. It is safe to call Stop multiple times.
func (a *App) Stop(ctx context.Context) error {
	return a.lc.stop(ctx)
}

// Shutdown triggers a graceful shutdown of a running App. It causes Run
// to proceed to the stop phase. It is safe to call Shutdown multiple times.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		close(a.shutdown)
	})
}

// Run starts the application, blocks until SIGINT or SIGTERM is received
// (or Shutdown is called), then stops the application.
//
// Run uses the configured start and stop timeouts for the lifecycle hooks.
// If Start fails, Run calls Stop to clean up any hooks that were started
// before the failure.
func (a *App) Run() error {
	if a.err != nil {
		return a.err
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), a.startTimeout)
	defer startCancel()

	if err := a.Start(startCtx); err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), a.stopTimeout)
		defer stopCancel()
		_ = a.Stop(stopCtx)
		return err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-a.shutdown:
	}
	signal.Stop(sig)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), a.stopTimeout)
	defer stopCancel()
	return a.Stop(stopCtx)
}
