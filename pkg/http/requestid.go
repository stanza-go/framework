package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey int

const requestIDKey contextKey = 0

// RequestIDConfig configures the RequestID middleware.
type RequestIDConfig struct {
	// Header is the HTTP header name used to read and write the request
	// ID. If the incoming request already carries this header, its value
	// is reused; otherwise a new ID is generated.
	// Default: "X-Request-ID".
	Header string

	// Generator returns a new unique request ID. Override this to use a
	// custom ID format (e.g., ULID, snowflake).
	// Default: random UUID v4.
	Generator func() string
}

// RequestID returns middleware that assigns a unique identifier to every
// request. If the incoming request already carries the configured header
// (default X-Request-ID), that value is reused; otherwise a new UUID v4
// is generated.
//
// The request ID is:
//   - set as a response header so clients can correlate requests
//   - stored in the request context (retrieve with GetRequestID)
//   - available to downstream middleware and handlers
//
// RequestID should be the first middleware in the chain so that all
// subsequent middleware (including RequestLogger) can access the ID:
//
//	r.Use(http.RequestID(http.RequestIDConfig{}))
//	r.Use(http.RequestLogger(logger))
//	r.Use(http.SecureHeaders(http.SecureHeadersConfig{}))
//	r.Use(http.Recovery(onPanic))
func RequestID(cfg RequestIDConfig) Middleware {
	if cfg.Header == "" {
		cfg.Header = "X-Request-ID"
	}
	if cfg.Generator == nil {
		cfg.Generator = generateUUID
	}

	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			id := r.Header.Get(cfg.Header)
			if id == "" {
				id = cfg.Generator()
			}

			w.Header().Set(cfg.Header, id)

			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID returns the request ID from the request context. Returns
// an empty string if no request ID was set (i.e., the RequestID
// middleware was not applied).
func GetRequestID(r *Request) string {
	id, _ := r.Context().Value(requestIDKey).(string)
	return id
}

// generateUUID returns a random UUID v4 string (e.g.,
// "550e8400-e29b-41d4-a716-446655440000"). It uses crypto/rand for
// secure randomness.
func generateUUID() string {
	var uuid [16]byte
	_, _ = io.ReadFull(rand.Reader, uuid[:])

	// Set version 4 (0100xxxx) and variant 10xxxxxx per RFC 4122.
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	var buf [36]byte
	hex.Encode(buf[0:8], uuid[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], uuid[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], uuid[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], uuid[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], uuid[10:16])

	return string(buf[:])
}
