package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- Container / DI ---

func TestProvideBasic(t *testing.T) {
	type Config struct{ Port int }

	app := New(
		Provide(func() *Config {
			return &Config{Port: 8080}
		}),
		Invoke(func(cfg *Config) {
			if cfg.Port != 8080 {
				t.Errorf("got port %d, want 8080", cfg.Port)
			}
		}),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideWithDeps(t *testing.T) {
	type Config struct{ Port int }
	type Server struct{ Addr string }

	app := New(
		Provide(
			func() *Config {
				return &Config{Port: 8080}
			},
			func(cfg *Config) *Server {
				return &Server{Addr: fmt.Sprintf(":%d", cfg.Port)}
			},
		),
		Invoke(func(srv *Server) {
			if srv.Addr != ":8080" {
				t.Errorf("got addr %q, want %q", srv.Addr, ":8080")
			}
		}),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideDependencyChain(t *testing.T) {
	type A struct{ Val string }
	type B struct{ Val string }
	type C struct{ Val string }

	app := New(
		Provide(
			func(b *B) *C { return &C{Val: b.Val + "->C"} },
			func() *A { return &A{Val: "A"} },
			func(a *A) *B { return &B{Val: a.Val + "->B"} },
		),
		Invoke(func(c *C) {
			want := "A->B->C"
			if c.Val != want {
				t.Errorf("got %q, want %q", c.Val, want)
			}
		}),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideMultipleReturns(t *testing.T) {
	type Config struct{ Port int }
	type Logger struct{ Level string }

	app := New(
		Provide(func() (*Config, *Logger) {
			return &Config{Port: 8080}, &Logger{Level: "info"}
		}),
		Invoke(func(cfg *Config, log *Logger) {
			if cfg.Port != 8080 {
				t.Errorf("got port %d, want 8080", cfg.Port)
			}
			if log.Level != "info" {
				t.Errorf("got level %q, want %q", log.Level, "info")
			}
		}),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideWithError(t *testing.T) {
	type Config struct{ Port int }

	app := New(
		Provide(func() (*Config, error) {
			return &Config{Port: 8080}, nil
		}),
		Invoke(func(cfg *Config) {
			if cfg.Port != 8080 {
				t.Errorf("got port %d, want 8080", cfg.Port)
			}
		}),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProvideConstructorError(t *testing.T) {
	type Config struct{}
	want := errors.New("config load failed")

	app := New(
		Provide(func() (*Config, error) {
			return nil, want
		}),
	)
	if app.Err() == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(app.Err(), want) {
		t.Errorf("got %v, want wrapping %v", app.Err(), want)
	}
}

func TestProvideNonFunction(t *testing.T) {
	app := New(Provide(42))
	if app.Err() == nil {
		t.Fatal("expected error for non-function provide")
	}
}

func TestProvideNoReturn(t *testing.T) {
	app := New(Provide(func() {}))
	if app.Err() == nil {
		t.Fatal("expected error for no-return constructor")
	}
}

func TestProvideOnlyError(t *testing.T) {
	app := New(Provide(func() error { return nil }))
	if app.Err() == nil {
		t.Fatal("expected error for error-only constructor")
	}
}

func TestProvideDuplicate(t *testing.T) {
	type Config struct{}

	app := New(
		Provide(
			func() *Config { return &Config{} },
			func() *Config { return &Config{} },
		),
	)
	if app.Err() == nil {
		t.Fatal("expected error for duplicate type")
	}
}

func TestProvideMissingDep(t *testing.T) {
	type Config struct{}
	type Server struct{}

	app := New(
		Provide(func(cfg *Config) *Server { return &Server{} }),
	)
	if app.Err() == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestProvideCycle(t *testing.T) {
	type A struct{}
	type B struct{}

	app := New(
		Provide(
			func(b *B) *A { return &A{} },
			func(a *A) *B { return &B{} },
		),
	)
	if app.Err() == nil {
		t.Fatal("expected error for dependency cycle")
	}
}

func TestProvideSuppliedConflict(t *testing.T) {
	// *Lifecycle is auto-supplied, providing it again should fail.
	app := New(
		Provide(func() *Lifecycle { return &Lifecycle{} }),
	)
	if app.Err() == nil {
		t.Fatal("expected error for conflicting with supplied type")
	}
}

// --- Invoke ---

func TestInvokeBasic(t *testing.T) {
	called := false
	app := New(Invoke(func() { called = true }))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("invoke function was not called")
	}
}

func TestInvokeWithDeps(t *testing.T) {
	type Config struct{ Port int }
	var got int

	app := New(
		Provide(func() *Config { return &Config{Port: 9090} }),
		Invoke(func(cfg *Config) { got = cfg.Port }),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if got != 9090 {
		t.Errorf("got %d, want 9090", got)
	}
}

func TestInvokeError(t *testing.T) {
	want := errors.New("invoke failed")
	app := New(Invoke(func() error { return want }))
	if !errors.Is(app.Err(), want) {
		t.Errorf("got %v, want wrapping %v", app.Err(), want)
	}
}

func TestInvokeNonFunction(t *testing.T) {
	app := New(Invoke("not a function"))
	if app.Err() == nil {
		t.Fatal("expected error for non-function invoke")
	}
}

func TestInvokeMissingDep(t *testing.T) {
	type Config struct{}
	app := New(Invoke(func(cfg *Config) {}))
	if app.Err() == nil {
		t.Fatal("expected error for missing invoke dependency")
	}
}

func TestInvokeOrder(t *testing.T) {
	var order []int
	app := New(
		Invoke(
			func() { order = append(order, 1) },
			func() { order = append(order, 2) },
			func() { order = append(order, 3) },
		),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("got order %v, want [1 2 3]", order)
	}
}

func TestInvokeNotCalledAfterProvideError(t *testing.T) {
	called := false
	app := New(
		Provide(42),
		Invoke(func() { called = true }),
	)
	if app.Err() == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Error("invoke should not be called after provide error")
	}
}

// --- Lifecycle / Hooks ---

func TestLifecycleStartOrder(t *testing.T) {
	var order []int
	lc := &Lifecycle{}
	lc.Append(Hook{OnStart: func(context.Context) error { order = append(order, 1); return nil }})
	lc.Append(Hook{OnStart: func(context.Context) error { order = append(order, 2); return nil }})
	lc.Append(Hook{OnStart: func(context.Context) error { order = append(order, 3); return nil }})

	if err := lc.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("got start order %v, want [1 2 3]", order)
	}
}

func TestLifecycleStopReverseOrder(t *testing.T) {
	var order []int
	lc := &Lifecycle{}
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { order = append(order, 1); return nil },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { order = append(order, 2); return nil },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { order = append(order, 3); return nil },
	})

	if err := lc.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lc.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("got stop order %v, want [3 2 1]", order)
	}
}

func TestLifecycleStartError(t *testing.T) {
	var started []int
	lc := &Lifecycle{}
	lc.Append(Hook{OnStart: func(context.Context) error { started = append(started, 1); return nil }})
	lc.Append(Hook{OnStart: func(context.Context) error { return errors.New("fail") }})
	lc.Append(Hook{OnStart: func(context.Context) error { started = append(started, 3); return nil }})

	err := lc.start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if len(started) != 1 || started[0] != 1 {
		t.Errorf("got started %v, want [1]", started)
	}
	if lc.started != 1 {
		t.Errorf("got started count %d, want 1", lc.started)
	}
}

func TestLifecycleStopPartial(t *testing.T) {
	var stopped []int
	lc := &Lifecycle{}
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopped = append(stopped, 1); return nil },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return errors.New("fail") },
		OnStop:  func(context.Context) error { stopped = append(stopped, 2); return nil },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopped = append(stopped, 3); return nil },
	})

	lc.start(context.Background())
	lc.stop(context.Background())

	// Only hook 0 started successfully, so only hook 0 should be stopped.
	if len(stopped) != 1 || stopped[0] != 1 {
		t.Errorf("got stopped %v, want [1]", stopped)
	}
}

func TestLifecycleStopContinuesOnError(t *testing.T) {
	var stopped []int
	lc := &Lifecycle{}
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopped = append(stopped, 1); return nil },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopped = append(stopped, 2); return errors.New("stop fail") },
	})
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopped = append(stopped, 3); return nil },
	})

	lc.start(context.Background())
	err := lc.stop(context.Background())

	// All hooks should be stopped despite the error.
	if len(stopped) != 3 {
		t.Errorf("got %d stopped hooks, want 3", len(stopped))
	}
	if err == nil {
		t.Fatal("expected error from stop")
	}
}

func TestLifecycleStopIdempotent(t *testing.T) {
	stopCount := 0
	lc := &Lifecycle{}
	lc.Append(Hook{
		OnStart: func(context.Context) error { return nil },
		OnStop:  func(context.Context) error { stopCount++; return nil },
	})

	lc.start(context.Background())
	lc.stop(context.Background())
	lc.stop(context.Background())

	if stopCount != 1 {
		t.Errorf("got stop count %d, want 1", stopCount)
	}
}

func TestLifecycleOnStartOnly(t *testing.T) {
	started := false
	lc := &Lifecycle{}
	lc.Append(Hook{OnStart: func(context.Context) error { started = true; return nil }})

	if err := lc.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Error("OnStart was not called")
	}
	if err := lc.stop(context.Background()); err != nil {
		t.Errorf("stop should succeed with nil OnStop: %v", err)
	}
}

func TestLifecycleOnStopOnly(t *testing.T) {
	stopped := false
	lc := &Lifecycle{}
	lc.Append(Hook{OnStop: func(context.Context) error { stopped = true; return nil }})

	if err := lc.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lc.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("OnStop was not called")
	}
}

func TestLifecycleConcurrentAppend(t *testing.T) {
	lc := &Lifecycle{}
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lc.Append(Hook{
				OnStart: func(context.Context) error { return nil },
			})
		}()
	}
	wg.Wait()

	lc.mu.Lock()
	got := len(lc.hooks)
	lc.mu.Unlock()

	if got != n {
		t.Errorf("got %d hooks, want %d", got, n)
	}
}

// --- App ---

func TestAppStartStop(t *testing.T) {
	var order []string
	app := New(
		Provide(func(lc *Lifecycle) *struct{} {
			lc.Append(Hook{
				OnStart: func(context.Context) error { order = append(order, "start"); return nil },
				OnStop:  func(context.Context) error { order = append(order, "stop"); return nil },
			})
			return &struct{}{}
		}),
		Invoke(func(*struct{}) {}),
	)

	if err := app.Err(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if len(order) != 2 || order[0] != "start" || order[1] != "stop" {
		t.Errorf("got order %v, want [start stop]", order)
	}
}

func TestAppStartReturnsInitError(t *testing.T) {
	app := New(Provide(42))

	err := app.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from Start with init error")
	}
}

func TestAppLifecycleInjection(t *testing.T) {
	type DB struct{ Name string }

	var gotDB *DB
	app := New(
		Provide(func(lc *Lifecycle) *DB {
			db := &DB{Name: "test.db"}
			lc.Append(Hook{
				OnStart: func(context.Context) error { return nil },
				OnStop:  func(context.Context) error { db.Name = "closed"; return nil },
			})
			return db
		}),
		Invoke(func(db *DB) { gotDB = db }),
	)

	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if gotDB == nil || gotDB.Name != "test.db" {
		t.Fatal("DB not properly injected")
	}

	app.Start(context.Background())
	app.Stop(context.Background())

	if gotDB.Name != "closed" {
		t.Errorf("got DB name %q, want %q", gotDB.Name, "closed")
	}
}

func TestAppTimeouts(t *testing.T) {
	app := New(
		WithStartTimeout(5 * time.Second),
		WithStopTimeout(10 * time.Second),
	)

	if app.startTimeout != 5*time.Second {
		t.Errorf("got start timeout %v, want 5s", app.startTimeout)
	}
	if app.stopTimeout != 10*time.Second {
		t.Errorf("got stop timeout %v, want 10s", app.stopTimeout)
	}
}

func TestAppDefaultTimeouts(t *testing.T) {
	app := New()
	if app.startTimeout != 15*time.Second {
		t.Errorf("got start timeout %v, want 15s", app.startTimeout)
	}
	if app.stopTimeout != 15*time.Second {
		t.Errorf("got stop timeout %v, want 15s", app.stopTimeout)
	}
}

func TestAppRunWithShutdown(t *testing.T) {
	started := false
	stopped := false

	app := New(
		Provide(func(lc *Lifecycle) *struct{} {
			lc.Append(Hook{
				OnStart: func(context.Context) error { started = true; return nil },
				OnStop:  func(context.Context) error { stopped = true; return nil },
			})
			return &struct{}{}
		}),
		Invoke(func(*struct{}) {}),
	)

	go func() {
		time.Sleep(50 * time.Millisecond)
		app.Shutdown()
	}()

	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Error("hook was not started")
	}
	if !stopped {
		t.Error("hook was not stopped")
	}
}

func TestAppRunStartError(t *testing.T) {
	var stopped []int

	app := New(
		Provide(func(lc *Lifecycle) *struct{} {
			lc.Append(Hook{
				OnStart: func(context.Context) error { return nil },
				OnStop:  func(context.Context) error { stopped = append(stopped, 1); return nil },
			})
			lc.Append(Hook{
				OnStart: func(context.Context) error { return errors.New("start failed") },
				OnStop:  func(context.Context) error { stopped = append(stopped, 2); return nil },
			})
			return &struct{}{}
		}),
		Invoke(func(*struct{}) {}),
	)

	err := app.Run()
	if err == nil {
		t.Fatal("expected error from Run")
	}

	// Hook 0 was started, hook 1 failed. Stop should clean up hook 0.
	if len(stopped) != 1 || stopped[0] != 1 {
		t.Errorf("got stopped %v, want [1]", stopped)
	}
}

func TestAppRunInitError(t *testing.T) {
	app := New(Provide(42))
	err := app.Run()
	if err == nil {
		t.Fatal("expected error from Run with init error")
	}
}

func TestAppShutdownIdempotent(t *testing.T) {
	app := New()

	// Should not panic.
	app.Shutdown()
	app.Shutdown()
	app.Shutdown()
}

// --- Integration ---

func TestFullStack(t *testing.T) {
	type Config struct {
		Addr string
	}
	type DB struct {
		DSN    string
		Closed bool
	}
	type Server struct {
		Addr    string
		Started bool
		Stopped bool
	}

	var order []string

	app := New(
		Provide(
			func() *Config {
				return &Config{Addr: ":8080"}
			},
			func(cfg *Config, lc *Lifecycle) *DB {
				db := &DB{DSN: "test.db"}
				lc.Append(Hook{
					OnStart: func(context.Context) error {
						order = append(order, "db.start")
						return nil
					},
					OnStop: func(context.Context) error {
						order = append(order, "db.stop")
						db.Closed = true
						return nil
					},
				})
				return db
			},
			func(cfg *Config, db *DB, lc *Lifecycle) *Server {
				srv := &Server{Addr: cfg.Addr}
				lc.Append(Hook{
					OnStart: func(context.Context) error {
						order = append(order, "server.start")
						srv.Started = true
						return nil
					},
					OnStop: func(context.Context) error {
						order = append(order, "server.stop")
						srv.Stopped = true
						return nil
					},
				})
				return srv
			},
		),
		Invoke(func(srv *Server, db *DB) {
			order = append(order, "invoke")
		}),
	)

	if err := app.Err(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Start order: db first (dependency of server), then server.
	wantStart := []string{"invoke", "db.start", "server.start"}
	if len(order) != len(wantStart) {
		t.Fatalf("after start: got order %v, want %v", order, wantStart)
	}
	for i, w := range wantStart {
		if order[i] != w {
			t.Errorf("after start: order[%d] = %q, want %q", i, order[i], w)
		}
	}

	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	// Stop order: reverse of start (server stop, then db stop).
	wantFull := []string{"invoke", "db.start", "server.start", "server.stop", "db.stop"}
	if len(order) != len(wantFull) {
		t.Fatalf("after stop: got order %v, want %v", order, wantFull)
	}
	for i, w := range wantFull {
		if order[i] != w {
			t.Errorf("after stop: order[%d] = %q, want %q", i, order[i], w)
		}
	}
}
