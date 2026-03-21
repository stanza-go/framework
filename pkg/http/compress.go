package http

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
)

// CompressConfig configures the Compress middleware.
type CompressConfig struct {
	// Level is the gzip compression level (1–9). Higher levels produce
	// smaller output but use more CPU. Zero uses the default level (6).
	Level int

	// MinSize is the minimum response body size in bytes before
	// compression kicks in. Responses smaller than this are sent
	// uncompressed because the gzip overhead negates any savings.
	// Default: 1024 (1 KB).
	MinSize int

	// ContentTypes is the set of MIME types eligible for compression.
	// Only responses whose Content-Type starts with one of these
	// prefixes are compressed. If empty, a sensible default set is used.
	ContentTypes []string
}

// defaultCompressTypes are the content-type prefixes compressed by
// default. Binary formats (images, video, archives) are already
// compressed and gain nothing from gzip.
var defaultCompressTypes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"application/xhtml+xml",
	"image/svg+xml",
}

// Compress returns middleware that gzip-compresses response bodies for
// clients that accept it. It checks the Accept-Encoding request header
// and only compresses responses whose Content-Type matches the
// configured set and whose body exceeds the minimum size threshold.
//
// Compress should be added early in the middleware chain so that all
// response bodies pass through the compressor:
//
//	r.Use(http.RequestID(http.RequestIDConfig{}))
//	r.Use(http.RequestLogger(logger))
//	r.Use(http.Compress(http.CompressConfig{}))
//	r.Use(http.SecureHeaders(http.SecureHeadersConfig{}))
//	r.Use(http.Recovery(onPanic))
func Compress(cfg CompressConfig) Middleware {
	if cfg.Level == 0 {
		cfg.Level = gzip.DefaultCompression
	}
	if cfg.MinSize == 0 {
		cfg.MinSize = 1024
	}
	types := cfg.ContentTypes
	if len(types) == 0 {
		types = defaultCompressTypes
	}

	pool := &sync.Pool{
		New: func() any {
			w, _ := gzip.NewWriterLevel(io.Discard, cfg.Level)
			return w
		},
	}

	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}

			cw := &compressWriter{
				ResponseWriter: w,
				pool:           pool,
				minSize:        cfg.MinSize,
				types:          types,
			}
			defer cw.Close()

			next.ServeHTTP(cw, r)
		})
	}
}

// acceptsGzip returns true if the request advertises gzip support in
// the Accept-Encoding header.
func acceptsGzip(r *Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(part) == "gzip" {
			return true
		}
	}
	return false
}

// compressWriter buffers writes until it can decide whether to compress.
// Small responses and non-text content types are sent uncompressed.
type compressWriter struct {
	ResponseWriter
	pool    *sync.Pool
	types   []string
	minSize int

	// state
	buf         []byte
	gz          *gzip.Writer
	decided     bool
	compressed  bool
	status      int
	wroteHeader bool
	hijacked    bool
}

// WriteHeader captures the status code. The actual header write is
// deferred until we know whether to add Content-Encoding.
func (cw *compressWriter) WriteHeader(code int) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true
	cw.status = code
}

// Write buffers bytes until the minimum size threshold is reached, then
// decides whether to compress based on content type and accumulated size.
func (cw *compressWriter) Write(b []byte) (int, error) {
	if !cw.wroteHeader {
		cw.WriteHeader(StatusOK)
	}

	if cw.decided {
		if cw.compressed {
			return cw.gz.Write(b)
		}
		return cw.ResponseWriter.Write(b)
	}

	cw.buf = append(cw.buf, b...)

	if len(cw.buf) >= cw.minSize {
		cw.decide()
		return len(b), cw.flush()
	}

	return len(b), nil
}

// decide checks content type and buffer size to determine whether to
// compress.
func (cw *compressWriter) decide() {
	cw.decided = true

	ct := cw.Header().Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	if !cw.matchesType(ct) || len(cw.buf) < cw.minSize {
		return
	}

	cw.compressed = true
	cw.Header().Set("Content-Encoding", "gzip")
	cw.Header().Set("Vary", "Accept-Encoding")
	cw.Header().Del("Content-Length")
}

// matchesType returns true if ct starts with any of the configured
// content-type prefixes.
func (cw *compressWriter) matchesType(ct string) bool {
	for _, prefix := range cw.types {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

// flush writes the buffered data, starting the gzip writer if needed.
func (cw *compressWriter) flush() error {
	cw.ResponseWriter.WriteHeader(cw.status)

	if cw.compressed {
		gz := cw.pool.Get().(*gzip.Writer)
		gz.Reset(cw.ResponseWriter)
		cw.gz = gz
		_, err := gz.Write(cw.buf)
		cw.buf = nil
		return err
	}

	_, err := cw.ResponseWriter.Write(cw.buf)
	cw.buf = nil
	return err
}

// Unwrap returns the underlying ResponseWriter. This allows middleware
// further down the chain (such as the WebSocket upgrader) to find the
// original writer and its Hijacker interface.
func (cw *compressWriter) Unwrap() ResponseWriter {
	return cw.ResponseWriter
}

// Hijack implements net/http.Hijacker. It marks the writer as hijacked
// so that Close and flush are no-ops, then delegates to the underlying
// writer's Hijack.
func (cw *compressWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := cw.ResponseWriter.(nethttp.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("http: underlying ResponseWriter does not implement Hijacker")
	}
	cw.hijacked = true
	return hj.Hijack()
}

// Close finalizes the response. If compression was not decided yet
// (response smaller than minSize), it flushes uncompressed. If a gzip
// writer was started, it flushes and returns it to the pool.
func (cw *compressWriter) Close() {
	if cw.hijacked {
		return
	}

	if !cw.decided {
		cw.decide()
		_ = cw.flush()
	}

	if cw.gz != nil {
		_ = cw.gz.Close()
		cw.pool.Put(cw.gz)
	}
}
