package http

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"net"
	nethttp "net/http"
	"strings"
)

// ETagConfig configures the ETag middleware.
type ETagConfig struct {
	// Weak produces weak ETags (W/"...") instead of strong ETags.
	// Weak ETags indicate semantic equivalence, not byte-for-byte
	// identity. Use weak ETags when responses may vary slightly
	// (e.g., different whitespace) but are logically the same.
	Weak bool
}

// ETag returns middleware that computes ETags for responses and handles
// conditional requests. When a client sends an If-None-Match header
// matching the response's ETag, the middleware returns 304 Not Modified
// with no body, saving bandwidth.
//
// The ETag is a CRC32 hash of the response body, computed by buffering
// the full response. This is efficient for typical API responses and
// embedded static assets. Only GET and HEAD requests are eligible.
//
// Responses that already carry an ETag header (e.g., from net/http's
// ServeFileFS) are passed through unchanged.
//
// ETag should be placed after Compress in the middleware chain so the
// hash is computed on uncompressed content:
//
//	r.Use(http.Compress(http.CompressConfig{}))
//	r.Use(http.ETag(http.ETagConfig{}))
//	r.Use(http.SecureHeaders(http.SecureHeadersConfig{}))
func ETag(cfg ETagConfig) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			// Only compute ETags for GET and HEAD requests.
			if r.Method != "GET" && r.Method != "HEAD" {
				next.ServeHTTP(w, r)
				return
			}

			ew := &etagWriter{
				ResponseWriter: w,
				ifNoneMatch:    r.Header.Get("If-None-Match"),
				weak:           cfg.Weak,
				buf:            &bytes.Buffer{},
			}

			next.ServeHTTP(ew, r)
			ew.finish()
		})
	}
}

// etagWriter buffers the response body to compute a CRC32 ETag after
// the handler finishes. It defers WriteHeader until finish() so it can
// decide between 200 and 304.
type etagWriter struct {
	ResponseWriter
	ifNoneMatch string
	weak        bool
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
	hijacked    bool
}

// Unwrap returns the underlying ResponseWriter. This allows middleware
// further down the chain (such as the WebSocket upgrader) to find the
// original writer and its Hijacker interface.
func (ew *etagWriter) Unwrap() ResponseWriter {
	return ew.ResponseWriter
}

// Hijack implements net/http.Hijacker. It marks the writer as hijacked
// so that finish is a no-op, then delegates to the underlying writer's
// Hijack.
func (ew *etagWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := ew.ResponseWriter.(nethttp.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("http: underlying ResponseWriter does not implement Hijacker")
	}
	ew.hijacked = true
	return hj.Hijack()
}

// WriteHeader captures the status code without forwarding it.
func (ew *etagWriter) WriteHeader(code int) {
	if ew.wroteHeader {
		return
	}
	ew.wroteHeader = true
	ew.status = code
}

// Write buffers bytes for ETag computation.
func (ew *etagWriter) Write(b []byte) (int, error) {
	if !ew.wroteHeader {
		ew.WriteHeader(StatusOK)
	}
	return ew.buf.Write(b)
}

// finish computes the ETag, checks If-None-Match, and writes the
// response (either 304 or the full body with ETag header).
func (ew *etagWriter) finish() {
	if ew.hijacked {
		return
	}

	if !ew.wroteHeader {
		ew.WriteHeader(StatusOK)
	}

	body := ew.buf.Bytes()

	// Only set ETag for 2xx responses with a body.
	if ew.status < 200 || ew.status >= 300 || len(body) == 0 {
		ew.ResponseWriter.WriteHeader(ew.status)
		if len(body) > 0 {
			_, _ = ew.ResponseWriter.Write(body)
		}
		return
	}

	// Skip if the handler already set an ETag (e.g., ServeFileFS).
	if ew.ResponseWriter.Header().Get("ETag") != "" {
		ew.ResponseWriter.WriteHeader(ew.status)
		_, _ = ew.ResponseWriter.Write(body)
		return
	}

	// Compute CRC32 hash.
	hash := crc32.ChecksumIEEE(body)
	etag := fmt.Sprintf(`"%x"`, hash)
	if ew.weak {
		etag = "W/" + etag
	}

	ew.ResponseWriter.Header().Set("ETag", etag)

	// Check If-None-Match.
	if ew.ifNoneMatch != "" && etagMatches(ew.ifNoneMatch, etag) {
		ew.ResponseWriter.WriteHeader(StatusNotModified)
		return
	}

	ew.ResponseWriter.WriteHeader(ew.status)
	_, _ = ew.ResponseWriter.Write(body)
}

// etagMatches checks whether the given If-None-Match header value
// matches the computed ETag. It handles the wildcard "*", comma-
// separated lists, and weak/strong comparison (per RFC 7232 §3.2,
// If-None-Match uses weak comparison: W/"x" matches "x").
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "*" {
		return true
	}

	// Strip W/ prefix for weak comparison.
	bareEtag := stripWeakPrefix(etag)

	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if stripWeakPrefix(candidate) == bareEtag {
			return true
		}
	}
	return false
}

// stripWeakPrefix removes the "W/" prefix from a weak ETag value.
func stripWeakPrefix(s string) string {
	if strings.HasPrefix(s, "W/") {
		return s[2:]
	}
	return s
}
