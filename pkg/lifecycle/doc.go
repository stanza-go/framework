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
