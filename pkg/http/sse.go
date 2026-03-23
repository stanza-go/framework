package http

import (
	nethttp "net/http"
	"strconv"
	"strings"
	"time"
)

// SSEWriter writes Server-Sent Events to an HTTP response. Create one
// at the start of a handler and send events in a loop until the client
// disconnects (detected via r.Context().Done()):
//
//	sse := http.NewSSEWriter(w)
//	for {
//	    select {
//	    case <-r.Context().Done():
//	        return
//	    case msg := <-updates:
//	        sse.Event("message", msg)
//	    case <-time.After(30 * time.Second):
//	        sse.Comment("keepalive")
//	    }
//	}
type SSEWriter struct {
	w       ResponseWriter
	flusher nethttp.Flusher
}

// NewSSEWriter creates a writer for Server-Sent Events. It sets
// Content-Type to text/event-stream, disables caching, and flushes
// headers to the client immediately. If the underlying ResponseWriter
// supports flushing (including through middleware wrappers), events
// are flushed after each write.
//
// NewSSEWriter automatically clears the server's write deadline so
// the stream is not killed by the server's WriteTimeout (typically
// 15 seconds). WebSocket connections avoid this because they hijack
// the connection; SSE uses the regular ResponseWriter and needs an
// explicit deadline reset.
func NewSSEWriter(w ResponseWriter) *SSEWriter {
	// Clear the write deadline set by the server's WriteTimeout.
	// SSE streams are long-lived — the server's default timeout
	// (e.g. 15s) would kill the connection mid-stream. Using
	// ResponseController walks the middleware Unwrap chain to reach
	// the underlying net.Conn and reset its deadline.
	rc := nethttp.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(StatusOK)

	s := &SSEWriter{w: w}
	if f, ok := w.(nethttp.Flusher); ok {
		s.flusher = f
		f.Flush()
	}
	return s
}

// Event sends a named Server-Sent Event. Multiline data is split
// across multiple data: fields per the SSE specification.
func (s *SSEWriter) Event(event, data string) error {
	var b strings.Builder
	b.WriteString("event: ")
	b.WriteString(event)
	b.WriteByte('\n')
	writeDataLines(&b, data)
	b.WriteByte('\n')
	_, err := s.w.Write([]byte(b.String()))
	if err == nil {
		s.flush()
	}
	return err
}

// Data sends an unnamed Server-Sent Event. Multiline data is split
// across multiple data: fields per the SSE specification.
func (s *SSEWriter) Data(data string) error {
	var b strings.Builder
	writeDataLines(&b, data)
	b.WriteByte('\n')
	_, err := s.w.Write([]byte(b.String()))
	if err == nil {
		s.flush()
	}
	return err
}

// Comment sends an SSE comment. Comments are invisible to EventSource
// clients and useful as keep-alive heartbeats to prevent proxy
// timeouts.
func (s *SSEWriter) Comment(text string) error {
	_, err := s.w.Write([]byte(": " + text + "\n"))
	if err == nil {
		s.flush()
	}
	return err
}

// Retry tells the client to wait ms milliseconds before reconnecting
// after a connection loss.
func (s *SSEWriter) Retry(ms int) error {
	_, err := s.w.Write([]byte("retry: " + strconv.Itoa(ms) + "\n\n"))
	if err == nil {
		s.flush()
	}
	return err
}

// writeDataLines writes data as one or more data: fields to b.
// Each line in data becomes a separate data: field per the SSE spec.
func writeDataLines(b *strings.Builder, data string) {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// flush sends buffered data to the client immediately.
func (s *SSEWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}
