package http

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// === Helper: dial a test WebSocket server ===

// dialTestServer creates a test HTTP server with WebSocket support,
// dials it, and performs the WebSocket handshake. It returns the
// buffered reader (preserving any already-buffered data from the
// handshake), the raw connection, and the server for cleanup.
func dialTestServer(t *testing.T, handler func(*Conn)) (*bufio.Reader, net.Conn, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		conn, err := upgrader.Upgrade(w, r)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	// Perform WebSocket handshake.
	key := GenerateKey()
	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), key,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		srv.Close()
		t.Fatalf("write handshake: %v", err)
	}

	// Read 101 response. Keep the bufio.Reader so callers reuse it
	// (it may have buffered data beyond the HTTP response).
	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		srv.Close()
		t.Fatalf("read handshake response: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 101 {
		conn.Close()
		srv.Close()
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}

	accept := resp.Header.Get("Sec-WebSocket-Accept")
	expectedAccept := computeAcceptKey(key)
	if accept != expectedAccept {
		conn.Close()
		srv.Close()
		t.Fatalf("Sec-WebSocket-Accept = %q, want %q", accept, expectedAccept)
	}

	return br, conn, srv
}

// writeClientFrame writes a masked WebSocket frame (client-to-server).
func writeClientFrame(w io.Writer, fin bool, opcode byte, payload []byte) error {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}

	var header []byte
	length := len(payload)
	switch {
	case length <= 125:
		header = []byte{b0, byte(length) | 0x80} // Set mask bit.
	case length <= 65535:
		header = make([]byte, 4)
		header[0] = b0
		header[1] = 126 | 0x80
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	default:
		header = make([]byte, 10)
		header[0] = b0
		header[1] = 127 | 0x80
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	if _, err := w.Write(header); err != nil {
		return err
	}

	// Generate random mask key.
	var maskKey [4]byte
	_, _ = rand.Read(maskKey[:])
	if _, err := w.Write(maskKey[:]); err != nil {
		return err
	}

	// Mask and write payload.
	masked := make([]byte, len(payload))
	copy(masked, payload)
	maskBytes(maskKey, masked)
	_, err := w.Write(masked)
	return err
}

// readServerFrame reads a WebSocket frame from the server (not masked).
func readServerFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err = io.ReadFull(r, header); err != nil {
		return false, 0, nil, err
	}

	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0F
	length := uint64(header[1] & 0x7F)

	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(r, ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(r, ext); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	payload = make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(r, payload); err != nil {
			return false, 0, nil, err
		}
	}

	return fin, opcode, payload, nil
}

// === Upgrade Handshake Tests ===

func TestUpgradeSuccess(t *testing.T) {
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.WriteMessage(mt, data)
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x1, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x1 || string(payload) != "hello" {
		t.Errorf("got fin=%v opcode=%d payload=%q, want fin=true opcode=1 payload=hello",
			fin, opcode, payload)
	}
}

func TestUpgradeMissingConnectionHeader(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	resp, err := nethttp.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusBadRequest)
	}
}

func TestUpgradeMissingUpgradeHeader(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), GenerateKey(),
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusBadRequest)
	}
}

func TestUpgradeWrongMethod(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf(
		"POST / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), GenerateKey(),
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusMethodNotAllowed)
	}
}

func TestUpgradeWrongVersion(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 8\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), GenerateKey(),
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusBadRequest)
	}
	if got := resp.Header.Get("Sec-WebSocket-Version"); got != "13" {
		t.Errorf("Sec-WebSocket-Version = %q, want %q", got, "13")
	}
}

func TestUpgradeMissingKey(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(),
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusBadRequest)
	}
}

func TestUpgradeOriginRejected(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{
			CheckOrigin: func(r *Request) bool { return false },
		}
		_, err := upgrader.Upgrade(w, r)
		if err == nil {
			t.Error("expected upgrade error")
		}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), GenerateKey(),
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, StatusForbidden)
	}
}

func TestUpgradeCustomOriginCheck(t *testing.T) {
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		// Just close.
	})
	defer srv.Close()
	conn.Close()
}

// === Message Read/Write Tests ===

func TestTextMessageEcho(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != TextMessage {
			t.Errorf("server msg type = %d, want %d", mt, TextMessage)
		}
		if string(data) != "hello world" {
			t.Errorf("server data = %q, want %q", data, "hello world")
		}
		if err := ws.WriteMessage(TextMessage, []byte("echo: hello world")); err != nil {
			t.Errorf("server write: %v", err)
		}
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x1, []byte("hello world")); err != nil {
		t.Fatalf("write: %v", err)
	}

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x1 || string(payload) != "echo: hello world" {
		t.Errorf("got fin=%v opcode=%d payload=%q", fin, opcode, payload)
	}
	<-done
}

func TestBinaryMessageEcho(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	done := make(chan struct{})

	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, received, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != BinaryMessage {
			t.Errorf("server msg type = %d, want %d", mt, BinaryMessage)
		}
		if err := ws.WriteMessage(BinaryMessage, received); err != nil {
			t.Errorf("server write: %v", err)
		}
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x2, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x2 {
		t.Errorf("got fin=%v opcode=%d", fin, opcode)
	}
	if len(payload) != len(data) {
		t.Fatalf("payload length = %d, want %d", len(payload), len(data))
	}
	for i := range data {
		if payload[i] != data[i] {
			t.Errorf("payload[%d] = 0x%02x, want 0x%02x", i, payload[i], data[i])
		}
	}
	<-done
}

func TestEmptyMessage(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != TextMessage || len(data) != 0 {
			t.Errorf("got type=%d len=%d, want type=1 len=0", mt, len(data))
		}
		_ = ws.WriteMessage(TextMessage, nil)
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x1, nil); err != nil {
		t.Fatalf("write: %v", err)
	}

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x1 || len(payload) != 0 {
		t.Errorf("got fin=%v opcode=%d len=%d", fin, opcode, len(payload))
	}
	<-done
}

// === Ping/Pong Tests ===

func TestPingPongDefault(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = ws.ReadMessage()
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x9, []byte("ping-data")); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if !fin || opcode != 0xA || string(payload) != "ping-data" {
		t.Errorf("got fin=%v opcode=%d payload=%q, want fin=true opcode=10 payload=ping-data",
			fin, opcode, payload)
	}

	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, ""))
	<-done
}

func TestCustomPingHandler(t *testing.T) {
	var pingReceived []byte
	done := make(chan struct{})

	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetPingHandler(func(data []byte) error {
			pingReceived = make([]byte, len(data))
			copy(pingReceived, data)
			return nil
		})
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = ws.ReadMessage()
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0x9, []byte("custom")); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, ""))
	<-done

	if string(pingReceived) != "custom" {
		t.Errorf("ping data = %q, want %q", pingReceived, "custom")
	}
}

func TestCustomPongHandler(t *testing.T) {
	var pongReceived []byte
	done := make(chan struct{})

	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetPongHandler(func(data []byte) error {
			pongReceived = make([]byte, len(data))
			copy(pongReceived, data)
			return nil
		})
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = ws.ReadMessage()
	})
	defer srv.Close()
	defer conn.Close()

	if err := writeClientFrame(conn, true, 0xA, []byte("pong-data")); err != nil {
		t.Fatalf("write pong: %v", err)
	}

	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, ""))
	<-done

	if string(pongReceived) != "pong-data" {
		t.Errorf("pong data = %q, want %q", pongReceived, "pong-data")
	}
}

func TestWritePing(t *testing.T) {
	clientReady := make(chan struct{})
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		if err := ws.WritePing([]byte("server-ping")); err != nil {
			t.Errorf("write ping: %v", err)
		}
		// Wait for client to read the ping before returning
		// (returning triggers defer conn.Close which sends close frame).
		<-clientReady
	})
	defer srv.Close()
	defer conn.Close()

	fin, opcode, payload, err := readServerFrame(br)
	close(clientReady)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x9 || string(payload) != "server-ping" {
		t.Errorf("got fin=%v opcode=%d payload=%q", fin, opcode, payload)
	}
	<-done
}

func TestWritePingTooLarge(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		err := ws.WritePing(make([]byte, 126))
		if err == nil {
			t.Error("expected error for large ping")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

func TestWritePongTooLarge(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		err := ws.WritePong(make([]byte, 126))
		if err == nil {
			t.Error("expected error for large pong")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

// === Close Frame Tests ===

func TestCloseHandshake(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected close error")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T", err)
			return
		}
		if ce.Code != CloseNormalClosure {
			t.Errorf("close code = %d, want %d", ce.Code, CloseNormalClosure)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, "bye"))

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read close reply: %v", err)
	}
	if !fin || opcode != 0x8 {
		t.Errorf("got fin=%v opcode=%d", fin, opcode)
	}
	code, _ := parseClosePayload(payload)
	if code != CloseNormalClosure {
		t.Errorf("reply close code = %d, want %d", code, CloseNormalClosure)
	}
	<-done
}

func TestCloseWithMessage(t *testing.T) {
	clientReady := make(chan struct{})
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_ = ws.writeClose(CloseGoingAway, "server shutting down")
		// Wait for client to read the close frame before the handler
		// returns (which triggers defer conn.Close()).
		<-clientReady
	})
	defer srv.Close()
	defer conn.Close()

	_, opcode, payload, err := readServerFrame(br)
	close(clientReady)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != 0x8 {
		t.Errorf("opcode = %d, want 8", opcode)
	}
	code, text := parseClosePayload(payload)
	if code != CloseGoingAway {
		t.Errorf("code = %d, want %d", code, CloseGoingAway)
	}
	if text != "server shutting down" {
		t.Errorf("text = %q, want %q", text, "server shutting down")
	}
	<-done
}

func TestCloseErrorString(t *testing.T) {
	ce := &CloseError{Code: 1000, Text: "normal"}
	got := ce.Error()
	want := "websocket: close 1000: normal"
	if got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// === Fragmentation Tests ===

func TestFragmentedMessage(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != TextMessage {
			t.Errorf("type = %d, want %d", mt, TextMessage)
		}
		if string(data) != "hello world" {
			t.Errorf("data = %q, want %q", data, "hello world")
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, false, 0x1, []byte("hello"))
	writeClientFrame(conn, true, 0x0, []byte(" world"))

	<-done
}

func TestFragmentedMessageThreeParts(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != BinaryMessage {
			t.Errorf("type = %d, want %d", mt, BinaryMessage)
		}
		if string(data) != "abcdef" {
			t.Errorf("data = %q, want %q", data, "abcdef")
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, false, 0x2, []byte("ab"))
	writeClientFrame(conn, false, 0x0, []byte("cd"))
	writeClientFrame(conn, true, 0x0, []byte("ef"))

	<-done
}

func TestPingDuringFragmentation(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != TextMessage || string(data) != "hello world" {
			t.Errorf("got type=%d data=%q", mt, data)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, false, 0x1, []byte("hello"))
	writeClientFrame(conn, true, 0x9, []byte("ping"))

	_, opcode, _, _ := readServerFrame(br)
	if opcode != 0xA {
		t.Errorf("expected pong, got opcode %d", opcode)
	}

	writeClientFrame(conn, true, 0x0, []byte(" world"))

	<-done
}

// === Edge Cases ===

func TestMaxMessageSizeExceeded(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetMaxMessageSize(10)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for oversized message")
			return
		}
		if err != ErrReadLimit {
			t.Errorf("error = %v, want ErrReadLimit", err)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x1, []byte("this is more than 10 bytes"))
	<-done
}

func TestInvalidUTF8TextMessage(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for invalid UTF-8")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T", err)
			return
		}
		if ce.Code != CloseInvalidPayload {
			t.Errorf("code = %d, want %d", ce.Code, CloseInvalidPayload)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x1, []byte{0xFF, 0xFE})

	_, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != 0x8 {
		t.Errorf("opcode = %d, want 8", opcode)
	}
	code, _ := parseClosePayload(payload)
	if code != CloseInvalidPayload {
		t.Errorf("close code = %d, want %d", code, CloseInvalidPayload)
	}
	<-done
}

func TestWriteAfterClose(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_ = ws.writeClose(CloseNormalClosure, "")
		err := ws.WriteMessage(TextMessage, []byte("after close"))
		if err != ErrCloseSent {
			t.Errorf("error = %v, want ErrCloseSent", err)
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

func TestInvalidMessageType(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		err := ws.WriteMessage(MessageType(99), []byte("bad"))
		if err == nil {
			t.Error("expected error for invalid message type")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

func TestReservedBitsSet(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for reserved bits")
			return
		}
	})
	defer srv.Close()
	defer conn.Close()

	// Write a frame with RSV1 bit set (invalid without extension).
	header := []byte{0xC1, 0x85} // FIN + RSV1 + text opcode, MASK + len 5
	conn.Write(header)
	var maskKey [4]byte
	rand.Read(maskKey[:])
	conn.Write(maskKey[:])
	payload := []byte("hello")
	masked := make([]byte, len(payload))
	copy(masked, payload)
	maskBytes(maskKey, masked)
	conn.Write(masked)

	<-done
}

func TestUnknownOpcode(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for unknown opcode")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T", err)
			return
		}
		if ce.Code != CloseProtocolError {
			t.Errorf("code = %d, want %d", ce.Code, CloseProtocolError)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x3, []byte("bad"))

	_, opcode, _, _ := readServerFrame(br)
	if opcode != 0x8 {
		t.Errorf("expected close frame, got opcode %d", opcode)
	}
	<-done
}

func TestRemoteAddr(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		addr := ws.RemoteAddr()
		if addr == nil {
			t.Error("RemoteAddr returned nil")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

// === Extended Payload Length Tests ===

func TestMediumPayload(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i % 256)
	}

	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, received, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != BinaryMessage || len(received) != 256 {
			t.Errorf("type=%d len=%d", mt, len(received))
			return
		}
		_ = ws.WriteMessage(BinaryMessage, received)
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x2, data)

	_, _, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(payload) != 256 {
		t.Errorf("payload len = %d, want 256", len(payload))
	}
	<-done
}

func TestLargePayload(t *testing.T) {
	data := make([]byte, 70000)
	for i := range data {
		data[i] = byte(i % 251)
	}

	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		mt, received, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if mt != BinaryMessage || len(received) != 70000 {
			t.Errorf("type=%d len=%d", mt, len(received))
			return
		}
		_ = ws.WriteMessage(BinaryMessage, received)
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x2, data)

	_, _, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(payload) != 70000 {
		t.Errorf("payload len = %d, want 70000", len(payload))
	}
	for i := range data {
		if payload[i] != data[i] {
			t.Errorf("mismatch at byte %d: got 0x%02x want 0x%02x", i, payload[i], data[i])
			break
		}
	}
	<-done
}

// === Helper Function Tests ===

func TestComputeAcceptKey(t *testing.T) {
	// Verify deterministic output for a known key.
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	got := computeAcceptKey(key)
	// Recompute to verify consistency.
	got2 := computeAcceptKey(key)
	if got != got2 {
		t.Errorf("non-deterministic: %q != %q", got, got2)
	}
	if got == "" {
		t.Error("computeAcceptKey returned empty string")
	}
}

func TestGenerateKey(t *testing.T) {
	key := GenerateKey()
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 16 {
		t.Errorf("key decoded length = %d, want 16", len(decoded))
	}

	key2 := GenerateKey()
	if key == key2 {
		t.Error("two GenerateKey calls returned the same key")
	}
}

func TestMaskBytes(t *testing.T) {
	data := []byte("hello")
	key := [4]byte{0x37, 0xFA, 0x21, 0x3D}

	maskBytes(key, data)
	if string(data) == "hello" {
		t.Error("masking did not change data")
	}

	maskBytes(key, data)
	if string(data) != "hello" {
		t.Errorf("unmasked = %q, want %q", data, "hello")
	}
}

func TestBuildAndParseClosePayload(t *testing.T) {
	tests := []struct {
		code int
		text string
	}{
		{CloseNormalClosure, "normal"},
		{CloseGoingAway, "going away"},
		{CloseProtocolError, ""},
		{CloseNoStatusReceived, ""},
	}

	for _, tt := range tests {
		payload := buildClosePayload(tt.code, tt.text)
		if tt.code == CloseNoStatusReceived {
			if len(payload) != 0 {
				t.Errorf("CloseNoStatusReceived payload len = %d, want 0", len(payload))
			}
			code, text := parseClosePayload(payload)
			if code != CloseNoStatusReceived || text != "" {
				t.Errorf("parse nil payload: code=%d text=%q", code, text)
			}
			continue
		}
		code, text := parseClosePayload(payload)
		if code != tt.code || text != tt.text {
			t.Errorf("roundtrip: code=%d text=%q, want code=%d text=%q",
				code, text, tt.code, tt.text)
		}
	}
}

func TestHeaderContains(t *testing.T) {
	tests := []struct {
		header string
		values []string
		target string
		want   bool
	}{
		{"Connection", []string{"Upgrade"}, "upgrade", true},
		{"Connection", []string{"keep-alive, Upgrade"}, "upgrade", true},
		{"Connection", []string{"keep-alive"}, "upgrade", false},
		{"Upgrade", []string{"websocket"}, "websocket", true},
		{"Upgrade", []string{"WebSocket"}, "websocket", true},
	}

	for _, tt := range tests {
		h := nethttp.Header{}
		for _, v := range tt.values {
			h.Add(tt.header, v)
		}
		got := headerContains(h, tt.header, tt.target)
		if got != tt.want {
			t.Errorf("headerContains(%q, %v, %q) = %v, want %v",
				tt.header, tt.values, tt.target, got, tt.want)
		}
	}
}

func TestDefaultCheckOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"no origin", "localhost:8080", "", true},
		{"matching origin", "localhost:8080", "http://localhost:8080", true},
		{"matching https", "localhost:8080", "https://localhost:8080", true},
		{"mismatched origin", "localhost:8080", "http://evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &nethttp.Request{
				Host:   tt.host,
				Header: nethttp.Header{},
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			got := defaultCheckOrigin(r)
			if got != tt.want {
				t.Errorf("defaultCheckOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

// === Deadline Tests ===

func TestReadDeadline(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected timeout error")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

func TestWriteDeadline(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetWriteDeadline(time.Now().Add(-1 * time.Second))
		err := ws.WriteMessage(TextMessage, []byte("hello"))
		if err == nil {
			t.Error("expected deadline error")
		}
	})
	defer srv.Close()
	defer conn.Close()
	<-done
}

// === Integration with Router ===

func TestWebSocketWithRouter(t *testing.T) {
	r := NewRouter()

	r.HandleFunc("GET /ws", func(w ResponseWriter, req *Request) {
		upgrader := Upgrader{
			CheckOrigin: func(r *Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, req)
		if err != nil {
			return
		}
		defer conn.Close()

		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.WriteMessage(mt, append([]byte("echo: "), data...))
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := GenerateKey()
	req := fmt.Sprintf(
		"GET /ws HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), key,
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	writeClientFrame(conn, true, 0x1, []byte("router test"))

	fin, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fin || opcode != 0x1 || string(payload) != "echo: router test" {
		t.Errorf("got fin=%v opcode=%d payload=%q", fin, opcode, payload)
	}
}

// === Multiple Messages ===

func TestMultipleMessages(t *testing.T) {
	messages := []string{"msg1", "msg2", "msg3", "msg4", "msg5"}
	done := make(chan struct{})

	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		for i := 0; i < len(messages); i++ {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				t.Errorf("read message %d: %v", i, err)
				return
			}
			if mt != TextMessage || string(data) != messages[i] {
				t.Errorf("message %d: type=%d data=%q, want type=1 data=%q",
					i, mt, data, messages[i])
			}
			_ = ws.WriteMessage(TextMessage, data)
		}
	})
	defer srv.Close()
	defer conn.Close()

	for i, msg := range messages {
		if err := writeClientFrame(conn, true, 0x1, []byte(msg)); err != nil {
			t.Fatalf("write message %d: %v", i, err)
		}
		_, _, payload, err := readServerFrame(br)
		if err != nil {
			t.Fatalf("read reply %d: %v", i, err)
		}
		if string(payload) != msg {
			t.Errorf("reply %d = %q, want %q", i, payload, msg)
		}
	}
	<-done
}

// === SetPingHandler nil resets to default ===

func TestSetPingHandlerNil(t *testing.T) {
	done := make(chan struct{})
	br, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		ws.SetPingHandler(func(data []byte) error { return nil })
		ws.SetPingHandler(nil)
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = ws.ReadMessage()
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x9, []byte("test"))

	_, opcode, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != 0xA || string(payload) != "test" {
		t.Errorf("opcode=%d payload=%q, want opcode=10 payload=test", opcode, payload)
	}

	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, ""))
	<-done
}

// === Fragmented control frame (protocol error) ===

func TestFragmentedControlFrame(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for fragmented control frame")
		}
	})
	defer srv.Close()
	defer conn.Close()

	// Write a ping frame with FIN=0 (fragmented, which is invalid for control frames).
	header := []byte{0x09, 0x84} // No FIN + ping opcode, MASK + len 4
	conn.Write(header)
	var maskKey [4]byte
	rand.Read(maskKey[:])
	conn.Write(maskKey[:])
	payload := []byte("test")
	masked := make([]byte, 4)
	copy(masked, payload)
	maskBytes(maskKey, masked)
	conn.Write(masked)

	<-done
}

// === Control frame payload too large ===

func TestControlFramePayloadTooLarge(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error for oversized control payload")
		}
	})
	defer srv.Close()
	defer conn.Close()

	header := []byte{0x88, 0xFE} // FIN + close opcode, MASK + 126 indicator
	conn.Write(header)
	ext := make([]byte, 2)
	binary.BigEndian.PutUint16(ext, 126)
	conn.Write(ext)
	var maskKey [4]byte
	rand.Read(maskKey[:])
	conn.Write(maskKey[:])
	payload := make([]byte, 126)
	maskBytes(maskKey, payload)
	conn.Write(payload)

	<-done
}

// === Close during fragmentation ===

func TestCloseDuringFragmentation(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected close error during fragmentation")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T: %v", err, err)
			return
		}
		if ce.Code != CloseNormalClosure {
			t.Errorf("code = %d, want %d", ce.Code, CloseNormalClosure)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, false, 0x1, []byte("start"))
	writeClientFrame(conn, true, 0x8, buildClosePayload(CloseNormalClosure, ""))

	<-done
}

// === Non-continuation frame during fragmentation ===

func TestNonContinuationDuringFragmentation(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected error")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T", err)
			return
		}
		if ce.Code != CloseProtocolError {
			t.Errorf("code = %d, want %d", ce.Code, CloseProtocolError)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, false, 0x1, []byte("start"))
	writeClientFrame(conn, true, 0x1, []byte("bad"))

	<-done
}

// === Close with empty payload ===

func TestCloseEmptyPayload(t *testing.T) {
	done := make(chan struct{})
	_, conn, srv := dialTestServer(t, func(ws *Conn) {
		defer close(done)
		_, _, err := ws.ReadMessage()
		if err == nil {
			t.Error("expected close error")
			return
		}
		ce, ok := err.(*CloseError)
		if !ok {
			t.Errorf("expected *CloseError, got %T", err)
			return
		}
		if ce.Code != CloseNoStatusReceived {
			t.Errorf("code = %d, want %d", ce.Code, CloseNoStatusReceived)
		}
	})
	defer srv.Close()
	defer conn.Close()

	writeClientFrame(conn, true, 0x8, nil)
	<-done
}

// === Upgrader with custom buffer sizes ===

func TestUpgraderCustomBufferSizes(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upgrader := Upgrader{
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
			CheckOrigin:     func(r *nethttp.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.WriteMessage(mt, data)
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := GenerateKey()
	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), key,
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	writeClientFrame(conn, true, 0x1, []byte("custom-buf"))
	_, _, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "custom-buf" {
		t.Errorf("payload = %q, want %q", payload, "custom-buf")
	}
}

// TestUpgradeThroughMiddlewareWrappers verifies that WebSocket upgrade
// works through responseRecorder and etagWriter without producing
// "response.WriteHeader on hijacked connection" log spam. The wrappers
// implement Hijacker, so the hijack cascades and deferred cleanup
// (etagWriter.finish, compressWriter.Close) skips WriteHeader calls.
func TestUpgradeThroughMiddlewareWrappers(t *testing.T) {
	// Capture stderr to detect the "response.WriteHeader on hijacked
	// connection" message that net/http logs.
	handler := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		// Simulate the middleware chain: responseRecorder → etagWriter.
		rec := &responseRecorder{ResponseWriter: w, status: StatusOK}
		ew := &etagWriter{
			ResponseWriter: rec,
			buf:            &bytes.Buffer{},
		}

		// WebSocket handler.
		upgrader := Upgrader{}
		conn, err := upgrader.Upgrade(ew, r)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(mt, data)

		// Simulate deferred cleanup that ETag middleware does.
		ew.finish()
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	key := GenerateKey()
	req := fmt.Sprintf(
		"GET / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		srv.Listener.Addr().String(), key,
	)
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := nethttp.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 101 {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	// Verify the hijack flags were set on both wrappers.
	if err := writeClientFrame(conn, true, 0x1, []byte("through-middleware")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, payload, err := readServerFrame(br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "through-middleware" {
		t.Errorf("payload = %q, want %q", payload, "through-middleware")
	}
}

// TestHijackCascadesThroughWrappers verifies that calling Hijack on the
// outermost wrapper sets the hijacked flag on every wrapper in the chain.
func TestHijackCascadesThroughWrappers(t *testing.T) {
	done := make(chan struct{})

	handler := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		defer close(done)

		rec := &responseRecorder{ResponseWriter: w, status: StatusOK}
		cw := &compressWriter{ResponseWriter: rec, minSize: 1024}
		ew := &etagWriter{ResponseWriter: cw, buf: &bytes.Buffer{}}

		// Hijack through the outermost wrapper.
		conn, _, err := ew.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()

		// All wrappers should have hijacked=true.
		if !ew.hijacked {
			t.Error("etagWriter.hijacked should be true")
		}
		if !cw.hijacked {
			t.Error("compressWriter.hijacked should be true")
		}
		if !rec.hijacked {
			t.Error("responseRecorder.hijacked should be true")
		}

		// Deferred cleanup should be no-ops.
		ew.finish()
		cw.Close()
		rec.WriteHeader(StatusOK)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := nethttp.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
	}

	<-done
}

// Ensure unused imports are consumed.
var _ = strings.HasPrefix
