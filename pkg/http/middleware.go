package http

import (
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// Recovery returns middleware that recovers from panics in handlers.
// If a panic occurs, it writes a 500 JSON error response. If onPanic
// is non-nil, it is called with the recovered value and the stack
// trace before the response is written.
//
//	r.Use(http.Recovery(func(v any, stack []byte) {
//	    logger.Error("panic recovered",
//	        log.Any("error", v),
//	        log.String("stack", string(stack)),
//	    )
//	}))
func Recovery(onPanic func(recovered any, stack []byte)) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			defer func() {
				if v := recover(); v != nil {
					stack := debug.Stack()
					if onPanic != nil {
						onPanic(v, stack)
					}
					WriteError(w, StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger returns middleware that logs each HTTP request with
// method, path, status code, duration, response size, and remote
// address.
//
// Requests that result in 5xx status codes are logged at Error level.
// All other requests are logged at Info level.
//
// RequestLogger should be added before Recovery so that panics
// recovered by Recovery are logged with the correct 500 status:
//
//	r.Use(http.RequestLogger(logger))
//	r.Use(http.Recovery(onPanic))
func RequestLogger(logger *log.Logger) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			start := time.Now()

			rec := &responseRecorder{
				ResponseWriter: w,
				status:         StatusOK,
			}

			next.ServeHTTP(rec, r)

			fields := []log.Field{
				log.String("method", r.Method),
				log.String("path", r.URL.Path),
				log.Int("status", rec.status),
				log.Duration("duration", time.Since(start)),
				log.Int64("bytes", rec.written),
				log.String("remote", r.RemoteAddr),
			}

			if rec.status >= 500 {
				logger.Error("http request", fields...)
			} else {
				logger.Info("http request", fields...)
			}
		})
	}
}

// responseRecorder wraps a ResponseWriter to capture the status code
// and number of bytes written. It delegates all other calls to the
// underlying ResponseWriter.
type responseRecorder struct {
	ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

// WriteHeader captures the status code and delegates to the wrapped
// ResponseWriter. Only the first call takes effect.
func (rec *responseRecorder) WriteHeader(code int) {
	if !rec.wroteHeader {
		rec.status = code
		rec.wroteHeader = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

// Write captures the number of bytes written and delegates to the
// wrapped ResponseWriter. If WriteHeader has not been called, it
// implicitly sets the status to 200.
func (rec *responseRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(StatusOK)
	}
	n, err := rec.ResponseWriter.Write(b)
	rec.written += int64(n)
	return n, err
}

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowOrigins is the list of origins allowed to make cross-origin
	// requests. Use "*" to allow all origins (not compatible with
	// AllowCredentials). If empty, no CORS headers are set.
	AllowOrigins []string

	// AllowMethods is the list of HTTP methods allowed for cross-origin
	// requests. Defaults to GET, POST, PUT, DELETE, PATCH, OPTIONS.
	AllowMethods []string

	// AllowHeaders is the list of HTTP headers the client may send in
	// cross-origin requests. Defaults to Origin, Content-Type, Accept,
	// Authorization.
	AllowHeaders []string

	// AllowCredentials indicates whether the response can include
	// credentials (cookies, HTTP authentication, client certificates).
	// When true, AllowOrigins must not contain "*".
	AllowCredentials bool

	// MaxAge is the duration in seconds that preflight results can be
	// cached by the browser. Defaults to 86400 (24 hours).
	MaxAge int
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
// It responds to preflight OPTIONS requests with the configured CORS
// headers and a 204 status, and adds CORS headers to all other
// cross-origin requests.
//
// For development with Vite (admin on :23705, API on :23710):
//
//	r.Use(http.CORS(http.CORSConfig{
//	    AllowOrigins:     []string{"http://localhost:23705"},
//	    AllowCredentials: true,
//	}))
//
// CORS should be added after RequestLogger (so preflights are logged)
// and before Recovery:
//
//	r.Use(http.RequestLogger(logger))
//	r.Use(http.CORS(corsConfig))
//	r.Use(http.Recovery(onPanic))
func CORS(cfg CORSConfig) Middleware {
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	}
	if len(cfg.AllowHeaders) == 0 {
		cfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400
	}

	methods := strings.Join(cfg.AllowMethods, ", ")
	headers := strings.Join(cfg.AllowHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)

	origins := make(map[string]bool, len(cfg.AllowOrigins))
	allowAll := false
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			allowAll = true
		}
		origins[o] = true
	}

	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !allowAll && !origins[origin] {
				next.ServeHTTP(w, r)
				return
			}

			if allowAll && !cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Preflight request: respond immediately without calling next.
			if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
