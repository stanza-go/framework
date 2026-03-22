package http

import (
	"bufio"
	"fmt"
	"net"
	nethttp "net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// MaxBody returns middleware that limits the size of request bodies.
// When a handler reads beyond the limit, the read returns an error
// and the server closes the connection. Multipart requests (file
// uploads) are exempt — they should enforce their own limits via
// net/http.MaxBytesReader in the handler.
//
// A typical global limit of 2 MB protects JSON endpoints from abuse
// while leaving upload endpoints free to set their own limits:
//
//	r.Use(http.MaxBody(2 << 20))
func MaxBody(limit int64) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "multipart/") {
				r.Body = nethttp.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

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

			// Pre-allocate with capacity 7 to avoid growth when request_id
			// is present (the common case with RequestID middleware).
			fields := make([]log.Field, 6, 7)
			fields[0] = log.String("method", r.Method)
			fields[1] = log.String("path", r.URL.Path)
			fields[2] = log.Int("status", rec.status)
			fields[3] = log.Duration("duration", time.Since(start))
			fields[4] = log.Int64("bytes", rec.written)
			fields[5] = log.String("remote", r.RemoteAddr)

			// Include request ID if the RequestID middleware ran earlier
			// in the chain.
			if id := GetRequestID(r); id != "" {
				fields = append(fields, log.String("request_id", id))
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
	hijacked    bool
}

// Unwrap returns the underlying ResponseWriter. This allows middleware
// further down the chain (such as the WebSocket upgrader) to find the
// original writer and its Hijacker interface.
func (rec *responseRecorder) Unwrap() ResponseWriter {
	return rec.ResponseWriter
}

// WriteHeader captures the status code and delegates to the wrapped
// ResponseWriter. Only the first call is forwarded; subsequent calls
// are silently ignored to prevent "superfluous WriteHeader" warnings
// from net/http.
func (rec *responseRecorder) WriteHeader(code int) {
	if rec.wroteHeader || rec.hijacked {
		return
	}
	rec.status = code
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(code)
}

// Hijack implements net/http.Hijacker. It marks the recorder as hijacked
// so that subsequent WriteHeader and Write calls are no-ops, then
// delegates to the underlying writer's Hijack.
func (rec *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := rec.ResponseWriter.(nethttp.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("http: underlying ResponseWriter does not implement Hijacker")
	}
	rec.hijacked = true
	return hj.Hijack()
}

// Write captures the number of bytes written and delegates to the
// wrapped ResponseWriter. If WriteHeader has not been called, it
// implicitly sets the status to 200.
func (rec *responseRecorder) Write(b []byte) (int, error) {
	if rec.hijacked {
		return 0, fmt.Errorf("http: write on hijacked connection")
	}
	if !rec.wroteHeader {
		rec.WriteHeader(StatusOK)
	}
	n, err := rec.ResponseWriter.Write(b)
	rec.written += int64(n)
	return n, err
}

// SecureHeadersConfig configures the SecureHeaders middleware.
type SecureHeadersConfig struct {
	// FrameOptions controls the X-Frame-Options header. Common values
	// are "DENY" (default) and "SAMEORIGIN". Set to empty string to
	// use the default.
	FrameOptions string

	// ReferrerPolicy controls the Referrer-Policy header.
	// Defaults to "strict-origin-when-cross-origin".
	ReferrerPolicy string

	// PermissionsPolicy controls the Permissions-Policy header.
	// Defaults to "camera=(), microphone=(), geolocation=()".
	PermissionsPolicy string

	// HSTSMaxAge sets the Strict-Transport-Security max-age in seconds.
	// Only set this when the app is served exclusively over HTTPS.
	// Zero (default) omits the header entirely.
	HSTSMaxAge int

	// ContentSecurityPolicy sets the Content-Security-Policy header.
	// Empty (default) omits the header — CSP is highly app-specific.
	ContentSecurityPolicy string
}

// SecureHeaders returns middleware that sets common security headers on
// all responses. With zero-value config, it applies safe defaults:
//
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - X-XSS-Protection: 0
//   - Permissions-Policy: camera=(), microphone=(), geolocation=()
//
// SecureHeaders should be added early in the middleware chain, before
// CORS and Recovery:
//
//	r.Use(http.RequestLogger(logger))
//	r.Use(http.SecureHeaders(http.SecureHeadersConfig{}))
//	r.Use(http.CORS(corsConfig))
//	r.Use(http.Recovery(onPanic))
func SecureHeaders(cfg SecureHeadersConfig) Middleware {
	if cfg.FrameOptions == "" {
		cfg.FrameOptions = "DENY"
	}
	if cfg.ReferrerPolicy == "" {
		cfg.ReferrerPolicy = "strict-origin-when-cross-origin"
	}
	if cfg.PermissionsPolicy == "" {
		cfg.PermissionsPolicy = "camera=(), microphone=(), geolocation=()"
	}

	// Pre-compute HSTS value once.
	var hstsValue string
	if cfg.HSTSMaxAge > 0 {
		hstsValue = "max-age=" + strconv.Itoa(cfg.HSTSMaxAge) + "; includeSubDomains"
	}

	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", cfg.FrameOptions)
			h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			h.Set("X-XSS-Protection", "0")
			h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			if hstsValue != "" {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			if cfg.ContentSecurityPolicy != "" {
				h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
			next.ServeHTTP(w, r)
		})
	}
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
// For development with Vite (admin on :23706, API on :23710):
//
//	r.Use(http.CORS(http.CORSConfig{
//	    AllowOrigins:     []string{"http://localhost:23706"},
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

			// When using specific origins (not wildcard "*"), always set
			// Vary: Origin so shared caches vary responses by origin.
			// This must be set even for disallowed origins — otherwise a
			// cache could store a non-CORS response and serve it later
			// when an allowed origin makes the same request.
			if !allowAll && len(origins) > 0 {
				w.Header().Add("Vary", "Origin")
			}

			if !allowAll && !origins[origin] {
				next.ServeHTTP(w, r)
				return
			}

			if allowAll && !cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
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
