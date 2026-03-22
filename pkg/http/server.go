package http

import (
	"context"
	"fmt"
	"net"
	nethttp "net/http"
	"time"
)

// Server wraps a net/http.Server with lifecycle management. It provides
// Start and Stop methods that integrate with the lifecycle package via
// hooks:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: srv.Start,
//	    OnStop:  srv.Stop,
//	})
type Server struct {
	server   *nethttp.Server
	listener net.Listener
}

// ServerOption configures a Server.
type ServerOption func(*nethttp.Server)

// WithAddr sets the listen address. The default is ":8080".
func WithAddr(addr string) ServerOption {
	return func(s *nethttp.Server) {
		s.Addr = addr
	}
}

// WithReadTimeout sets the maximum duration for reading the entire
// request, including the body. The default is 15 seconds.
func WithReadTimeout(d time.Duration) ServerOption {
	return func(s *nethttp.Server) {
		s.ReadTimeout = d
	}
}

// WithWriteTimeout sets the maximum duration before timing out writes
// of the response. The default is 15 seconds.
func WithWriteTimeout(d time.Duration) ServerOption {
	return func(s *nethttp.Server) {
		s.WriteTimeout = d
	}
}

// WithIdleTimeout sets the maximum amount of time to wait for the
// next request when keep-alives are enabled. The default is 60 seconds.
func WithIdleTimeout(d time.Duration) ServerOption {
	return func(s *nethttp.Server) {
		s.IdleTimeout = d
	}
}

// WithMaxHeaderBytes sets the maximum size of request headers in
// bytes. The default is 1 MB (Go's http.DefaultMaxHeaderBytes).
// This prevents clients from sending excessively large headers.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(s *nethttp.Server) {
		s.MaxHeaderBytes = n
	}
}

// NewServer creates a new Server with the given handler and options.
// Sensible defaults are applied: 15s read/write timeouts, 60s idle
// timeout, and ":8080" listen address.
func NewServer(handler Handler, opts ...ServerOption) *Server {
	srv := &nethttp.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	for _, opt := range opts {
		opt(srv)
	}
	return &Server{server: srv}
}

// Start binds to the configured address and begins serving in a
// background goroutine. The context is used for the listener bind;
// it does not cancel serving. Start returns immediately after the
// port is bound.
func (s *Server) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("http: listen %s: %w", s.server.Addr, err)
	}
	s.listener = ln
	go func() {
		// Serve returns ErrServerClosed on graceful shutdown.
		// Other errors mean the listener stopped, but Stop still completes cleanly.
		_ = s.server.Serve(ln)
	}()
	return nil
}

// Stop gracefully shuts down the server. It stops accepting new
// connections and waits for active requests to complete, respecting
// the context deadline.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Addr returns the address the server is listening on. This is useful
// when the server was configured with port 0 (OS-assigned port).
// Addr returns an empty string if the server has not been started.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}
