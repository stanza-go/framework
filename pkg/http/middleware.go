package http

import (
	"runtime/debug"
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
