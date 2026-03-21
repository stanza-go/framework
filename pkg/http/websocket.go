package http

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MessageType represents the type of a WebSocket message.
type MessageType int

const (
	// TextMessage denotes a text data message. The text must be valid UTF-8.
	TextMessage MessageType = 1

	// BinaryMessage denotes a binary data message.
	BinaryMessage MessageType = 2

	// CloseMessage denotes a close control message.
	CloseMessage MessageType = 8

	// PingMessage denotes a ping control message.
	PingMessage MessageType = 9

	// PongMessage denotes a pong control message.
	PongMessage MessageType = 10
)

// Close status codes defined in RFC 6455, Section 7.4.
const (
	CloseNormalClosure    = 1000
	CloseGoingAway        = 1001
	CloseProtocolError    = 1002
	CloseUnsupportedData  = 1003
	CloseNoStatusReceived = 1005
	CloseAbnormalClosure  = 1006
	CloseInvalidPayload   = 1007
	ClosePolicyViolation  = 1008
	CloseMessageTooBig    = 1009
)

// websocketGUID is the magic GUID specified in RFC 6455, Section 4.2.2.
const websocketGUID = "258EAFA5-E914-47DA-95CA-5AB53F89FEED"

// Default buffer sizes.
const (
	defaultReadBufferSize  = 4096
	defaultWriteBufferSize = 4096
	maxControlPayload      = 125
	maxDefaultMessageSize  = 16 * 1024 * 1024 // 16 MB
)

var (
	// ErrCloseSent is returned when attempting to write after a close frame
	// has been sent.
	ErrCloseSent = errors.New("websocket: close frame already sent")

	// ErrReadLimit is returned when a message exceeds the read size limit.
	ErrReadLimit = errors.New("websocket: message exceeds read limit")
)

// CloseError represents a WebSocket close frame with a status code and text.
type CloseError struct {
	Code int
	Text string
}

// Error returns the close error as a string.
func (e *CloseError) Error() string {
	return fmt.Sprintf("websocket: close %d: %s", e.Code, e.Text)
}

// Upgrader upgrades an HTTP connection to a WebSocket connection.
type Upgrader struct {
	// ReadBufferSize specifies the size of the read buffer in bytes.
	// If zero, a default of 4096 bytes is used.
	ReadBufferSize int

	// WriteBufferSize specifies the size of the write buffer in bytes.
	// If zero, a default of 4096 bytes is used.
	WriteBufferSize int

	// CheckOrigin returns true if the request origin is acceptable.
	// If nil, a safe default is used: the Origin header must match the
	// Host header.
	CheckOrigin func(r *Request) bool
}

// Upgrade upgrades the HTTP connection to the WebSocket protocol.
// The caller is responsible for closing the returned Conn.
func (u Upgrader) Upgrade(w ResponseWriter, r *Request) (*Conn, error) {
	if !headerContains(r.Header, "Connection", "upgrade") {
		return nil, writeUpgradeError(w, StatusBadRequest, "missing Connection: upgrade header")
	}
	if !headerContains(r.Header, "Upgrade", "websocket") {
		return nil, writeUpgradeError(w, StatusBadRequest, "missing Upgrade: websocket header")
	}
	if r.Method != "GET" {
		return nil, writeUpgradeError(w, StatusMethodNotAllowed, "websocket upgrade requires GET")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return nil, writeUpgradeError(w, StatusBadRequest, "unsupported WebSocket version")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, writeUpgradeError(w, StatusBadRequest, "missing Sec-WebSocket-Key header")
	}

	checkOrigin := u.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = defaultCheckOrigin
	}
	if !checkOrigin(r) {
		return nil, writeUpgradeError(w, StatusForbidden, "origin not allowed")
	}

	hj, ok := findHijacker(w)
	if !ok {
		return nil, writeUpgradeError(w, StatusInternalServerError, "server does not support connection hijacking")
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("websocket: hijack failed: %w", err)
	}

	accept := computeAcceptKey(key)

	var buf []byte
	buf = append(buf, "HTTP/1.1 101 Switching Protocols\r\n"...)
	buf = append(buf, "Upgrade: websocket\r\n"...)
	buf = append(buf, "Connection: Upgrade\r\n"...)
	buf = append(buf, "Sec-WebSocket-Accept: "...)
	buf = append(buf, accept...)
	buf = append(buf, "\r\n\r\n"...)
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket: write handshake: %w", err)
	}

	readBufSize := u.ReadBufferSize
	if readBufSize <= 0 {
		readBufSize = defaultReadBufferSize
	}
	writeBufSize := u.WriteBufferSize
	if writeBufSize <= 0 {
		writeBufSize = defaultWriteBufferSize
	}

	// Reuse the hijacked bufio.ReadWriter if its buffer is large enough.
	if brw.Reader.Buffered() > 0 || brw.Reader.Size() >= readBufSize {
		// Keep the existing reader with its buffered data.
	} else {
		brw.Reader = bufio.NewReaderSize(conn, readBufSize)
	}
	if brw.Writer.Buffered() > 0 || brw.Writer.Size() >= writeBufSize {
		// Keep the existing writer.
	} else {
		brw.Writer = bufio.NewWriterSize(conn, writeBufSize)
	}

	c := &Conn{
		conn:           conn,
		br:             brw.Reader,
		bw:             brw.Writer,
		maxMessageSize: maxDefaultMessageSize,
	}
	c.pingHandler = c.defaultPingHandler
	return c, nil
}

// Conn represents a WebSocket connection. All methods are safe for
// concurrent use by a single reader and a single writer. However,
// ReadMessage must not be called concurrently from multiple goroutines,
// and WriteMessage must not be called concurrently from multiple
// goroutines. One goroutine reading and one goroutine writing is safe.
type Conn struct {
	conn           net.Conn
	br             *bufio.Reader
	bw             *bufio.Writer
	writeMu        sync.Mutex
	closeSent      bool
	maxMessageSize int64
	pingHandler    func(data []byte) error
	pongHandler    func(data []byte) error
}

// SetMaxMessageSize sets the maximum size in bytes for a message read
// from the peer. If a message exceeds the limit, the connection sends
// a close frame to the peer and returns ErrReadLimit. The default is
// 16 MB.
func (c *Conn) SetMaxMessageSize(limit int64) {
	c.maxMessageSize = limit
}

// SetPingHandler sets the handler for ping messages received from the
// peer. If h is nil, the default handler sends a pong reply.
func (c *Conn) SetPingHandler(h func(data []byte) error) {
	if h == nil {
		c.pingHandler = c.defaultPingHandler
		return
	}
	c.pingHandler = h
}

// SetPongHandler sets the handler for pong messages received from the
// peer. If h is nil, pong messages are silently discarded.
func (c *Conn) SetPongHandler(h func(data []byte) error) {
	c.pongHandler = h
}

// SetReadDeadline sets the deadline for future read calls. A zero
// value means reads will not time out.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future write calls. A zero
// value means writes will not time out.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// ReadMessage reads the next data message from the connection.
// Control frames (ping, pong, close) are handled internally. Ping
// frames are answered with pong by default. A received close frame
// causes ReadMessage to return a *CloseError.
//
// Fragmented messages are reassembled transparently.
func (c *Conn) ReadMessage() (MessageType, []byte, error) {
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch opcode {
		case 0x9: // Ping
			if c.pingHandler != nil {
				if err := c.pingHandler(payload); err != nil {
					return 0, nil, err
				}
			}
			continue

		case 0xA: // Pong
			if c.pongHandler != nil {
				if err := c.pongHandler(payload); err != nil {
					return 0, nil, err
				}
			}
			continue

		case 0x8: // Close
			code, text := parseClosePayload(payload)
			_ = c.writeClose(code, text)
			return 0, nil, &CloseError{Code: code, Text: text}

		case 0x1, 0x2: // Text, Binary
			msgType := MessageType(opcode)
			if fin {
				if msgType == TextMessage && !utf8.Valid(payload) {
					_ = c.writeClose(CloseInvalidPayload, "invalid UTF-8")
					return 0, nil, &CloseError{Code: CloseInvalidPayload, Text: "invalid UTF-8"}
				}
				return msgType, payload, nil
			}
			// Fragmented message — collect continuation frames.
			data, err := c.readContinuation(payload)
			if err != nil {
				return 0, nil, err
			}
			if msgType == TextMessage && !utf8.Valid(data) {
				_ = c.writeClose(CloseInvalidPayload, "invalid UTF-8")
				return 0, nil, &CloseError{Code: CloseInvalidPayload, Text: "invalid UTF-8"}
			}
			return msgType, data, nil

		default:
			_ = c.writeClose(CloseProtocolError, "unknown opcode")
			return 0, nil, &CloseError{Code: CloseProtocolError, Text: "unknown opcode"}
		}
	}
}

// readContinuation reads continuation frames until a final frame is
// received. initial contains the payload from the first fragment.
func (c *Conn) readContinuation(initial []byte) ([]byte, error) {
	data := make([]byte, 0, len(initial)*2)
	data = append(data, initial...)

	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}

		// Handle interleaved control frames.
		switch opcode {
		case 0x9: // Ping
			if c.pingHandler != nil {
				if err := c.pingHandler(payload); err != nil {
					return nil, err
				}
			}
			continue
		case 0xA: // Pong
			if c.pongHandler != nil {
				if err := c.pongHandler(payload); err != nil {
					return nil, err
				}
			}
			continue
		case 0x8: // Close
			code, text := parseClosePayload(payload)
			_ = c.writeClose(code, text)
			return nil, &CloseError{Code: code, Text: text}
		case 0x0: // Continuation
			// Expected.
		default:
			_ = c.writeClose(CloseProtocolError, "expected continuation frame")
			return nil, &CloseError{Code: CloseProtocolError, Text: "expected continuation frame"}
		}

		if int64(len(data))+int64(len(payload)) > c.maxMessageSize {
			_ = c.writeClose(CloseMessageTooBig, "message too big")
			return nil, ErrReadLimit
		}
		data = append(data, payload...)
		if fin {
			return data, nil
		}
	}
}

// WriteMessage writes a message to the connection. The message type
// must be TextMessage or BinaryMessage. For text messages, data must
// be valid UTF-8.
func (c *Conn) WriteMessage(messageType MessageType, data []byte) error {
	if messageType != TextMessage && messageType != BinaryMessage {
		return fmt.Errorf("websocket: invalid message type %d", messageType)
	}
	return c.writeFrame(true, byte(messageType), data)
}

// WritePing sends a ping control frame. The data must not exceed 125
// bytes.
func (c *Conn) WritePing(data []byte) error {
	if len(data) > maxControlPayload {
		return fmt.Errorf("websocket: ping payload exceeds %d bytes", maxControlPayload)
	}
	return c.writeFrame(true, 0x9, data)
}

// WritePong sends a pong control frame. The data must not exceed 125
// bytes.
func (c *Conn) WritePong(data []byte) error {
	if len(data) > maxControlPayload {
		return fmt.Errorf("websocket: pong payload exceeds %d bytes", maxControlPayload)
	}
	return c.writeFrame(true, 0xA, data)
}

// Close sends a close frame with CloseNormalClosure and closes the
// underlying connection.
func (c *Conn) Close() error {
	_ = c.writeClose(CloseNormalClosure, "")
	return c.conn.Close()
}

// CloseWithMessage sends a close frame with the given status code and
// text, then closes the underlying connection.
func (c *Conn) CloseWithMessage(code int, text string) error {
	_ = c.writeClose(code, text)
	return c.conn.Close()
}

// RemoteAddr returns the remote network address of the peer.
func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// readFrame reads a single WebSocket frame from the connection.
func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	// Read first two bytes: FIN + opcode, MASK + payload length.
	header := make([]byte, 2)
	if _, err = io.ReadFull(c.br, header); err != nil {
		return false, 0, nil, fmt.Errorf("websocket: read frame header: %w", err)
	}

	fin = header[0]&0x80 != 0
	rsv := header[0] & 0x70
	if rsv != 0 {
		return false, 0, nil, &CloseError{Code: CloseProtocolError, Text: "reserved bits set"}
	}
	opcode = header[0] & 0x0F
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)

	// Control frames must not be fragmented.
	if opcode >= 0x8 && !fin {
		return false, 0, nil, &CloseError{Code: CloseProtocolError, Text: "fragmented control frame"}
	}

	// Extended payload length.
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read extended length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read extended length: %w", err)
		}
		length = binary.BigEndian.Uint64(ext)
		if length>>63 != 0 {
			return false, 0, nil, &CloseError{Code: CloseProtocolError, Text: "payload length overflow"}
		}
	}

	// Control frame payload must not exceed 125 bytes.
	if opcode >= 0x8 && length > maxControlPayload {
		return false, 0, nil, &CloseError{Code: CloseProtocolError, Text: "control frame payload too large"}
	}

	// Check message size limit.
	if int64(length) > c.maxMessageSize {
		return false, 0, nil, ErrReadLimit
	}

	// Read masking key if present (client-to-server frames must be masked).
	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, maskKey[:]); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read mask key: %w", err)
		}
	}

	// Read payload.
	payload = make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(c.br, payload); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read payload: %w", err)
		}
	}

	// Unmask payload.
	if masked {
		maskBytes(maskKey, payload)
	}

	return fin, opcode, payload, nil
}

// writeFrame writes a single WebSocket frame. Server-to-client frames
// are never masked per RFC 6455.
func (c *Conn) writeFrame(fin bool, opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closeSent && opcode != 0x8 {
		return ErrCloseSent
	}

	// First byte: FIN + opcode.
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	if err := c.bw.WriteByte(b0); err != nil {
		return fmt.Errorf("websocket: write frame: %w", err)
	}

	// Second byte: payload length (no mask for server-to-client).
	length := len(payload)
	switch {
	case length <= 125:
		if err := c.bw.WriteByte(byte(length)); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
	case length <= 65535:
		if err := c.bw.WriteByte(126); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(length))
		if _, err := c.bw.Write(ext); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
	default:
		if err := c.bw.WriteByte(127); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(length))
		if _, err := c.bw.Write(ext); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
	}

	// Write payload.
	if length > 0 {
		if _, err := c.bw.Write(payload); err != nil {
			return fmt.Errorf("websocket: write frame: %w", err)
		}
	}

	return c.bw.Flush()
}

// writeClose sends a close control frame.
func (c *Conn) writeClose(code int, text string) error {
	payload := buildClosePayload(code, text)
	c.writeMu.Lock()
	alreadySent := c.closeSent
	c.closeSent = true
	c.writeMu.Unlock()
	if alreadySent {
		return nil
	}
	return c.writeFrame(true, 0x8, payload)
}

// defaultPingHandler responds to ping with pong carrying the same payload.
func (c *Conn) defaultPingHandler(data []byte) error {
	return c.writeFrame(true, 0xA, data)
}

// computeAcceptKey computes the Sec-WebSocket-Accept value per
// RFC 6455, Section 4.2.2.
func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// maskBytes applies the XOR mask to data in place.
func maskBytes(key [4]byte, data []byte) {
	for i := range data {
		data[i] ^= key[i%4]
	}
}

// buildClosePayload builds a close frame payload with status code and
// optional text.
func buildClosePayload(code int, text string) []byte {
	if code == CloseNoStatusReceived {
		return nil
	}
	payload := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], text)
	return payload
}

// parseClosePayload extracts the status code and text from a close
// frame payload.
func parseClosePayload(payload []byte) (int, string) {
	if len(payload) < 2 {
		return CloseNoStatusReceived, ""
	}
	code := int(binary.BigEndian.Uint16(payload))
	text := ""
	if len(payload) > 2 {
		text = string(payload[2:])
	}
	return code, text
}

// defaultCheckOrigin checks that the Origin header matches the Host.
func defaultCheckOrigin(r *Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Strip scheme from origin for comparison.
	o := origin
	if i := strings.Index(o, "://"); i != -1 {
		o = o[i+3:]
	}
	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}
	return strings.EqualFold(o, host)
}

// headerContains returns true if the named header contains the target
// value (case-insensitive, comma-separated).
func headerContains(h nethttp.Header, key, target string) bool {
	for _, v := range h[nethttp.CanonicalHeaderKey(key)] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), target) {
				return true
			}
		}
	}
	return false
}

// writeUpgradeError writes an HTTP error response during the upgrade
// handshake and returns an error.
func writeUpgradeError(w ResponseWriter, status int, msg string) error {
	nethttp.Error(w, msg, status)
	return fmt.Errorf("websocket: %s", msg)
}

// findHijacker walks the ResponseWriter wrapper chain to find a
// Hijacker. Middleware that wraps ResponseWriter should implement
// Unwrap() ResponseWriter so that WebSocket upgrades work through the
// middleware chain.
func findHijacker(w ResponseWriter) (nethttp.Hijacker, bool) {
	for {
		if hj, ok := w.(nethttp.Hijacker); ok {
			return hj, true
		}
		unwrapper, ok := w.(interface{ Unwrap() ResponseWriter })
		if !ok {
			return nil, false
		}
		w = unwrapper.Unwrap()
	}
}

// GenerateKey generates a random Sec-WebSocket-Key for client use
// in testing.
func GenerateKey() string {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	return base64.StdEncoding.EncodeToString(key)
}
