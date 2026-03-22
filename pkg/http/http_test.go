package http

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// === Router Tests ===

func TestRouterBasicRoute(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /hello", func(w ResponseWriter, req *Request) {
		w.Write([]byte("world"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/hello", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "world" {
		t.Errorf("body = %q, want %q", got, "world")
	}
}

func TestRouterMethodRouting(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /item", func(w ResponseWriter, req *Request) {
		w.Write([]byte("get"))
	})
	r.HandleFunc("POST /item", func(w ResponseWriter, req *Request) {
		w.Write([]byte("post"))
	})

	tests := []struct {
		method string
		want   string
	}{
		{"GET", "get"},
		{"POST", "post"},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tt.method, "/item", nil)
		r.ServeHTTP(w, req)
		if got := w.Body.String(); got != tt.want {
			t.Errorf("%s /item body = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestRouterPathParam(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /users/{id}", func(w ResponseWriter, req *Request) {
		w.Write([]byte(PathParam(req, "id")))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/42", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "42" {
		t.Errorf("body = %q, want %q", got, "42")
	}
}

func TestRouterMultiplePathParams(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /orgs/{org}/repos/{repo}", func(w ResponseWriter, req *Request) {
		w.Write([]byte(PathParam(req, "org") + "/" + PathParam(req, "repo")))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/orgs/stanza/repos/framework", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "stanza/framework" {
		t.Errorf("body = %q, want %q", got, "stanza/framework")
	}
}

func TestRouterNotFound(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /exists", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nope", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, StatusNotFound)
	}
}

func TestRouterMethodNotAllowed(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /only-get", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/only-get", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, StatusMethodNotAllowed)
	}
}

// === Middleware Tests ===

func TestRouterMiddleware(t *testing.T) {
	r := NewRouter()
	r.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Set("X-Custom", "applied")
			next.ServeHTTP(w, req)
		})
	})
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Custom"); got != "applied" {
		t.Errorf("X-Custom = %q, want %q", got, "applied")
	}
}

func TestRouterMiddlewareOrder(t *testing.T) {
	r := NewRouter()
	r.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-Order", "first")
			next.ServeHTTP(w, req)
		})
	})
	r.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-Order", "second")
			next.ServeHTTP(w, req)
		})
	})
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Header().Add("X-Order", "handler")
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Values("X-Order")
	want := []string{"first", "second", "handler"}
	if len(got) != len(want) {
		t.Fatalf("X-Order values = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("X-Order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// === Group Tests ===

func TestGroupBasicRoute(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.HandleFunc("GET /users", func(w ResponseWriter, req *Request) {
		w.Write([]byte("users"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/users", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "users" {
		t.Errorf("body = %q, want %q", got, "users")
	}
}

func TestGroupMethodRouting(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.HandleFunc("GET /items", func(w ResponseWriter, req *Request) {
		w.Write([]byte("list"))
	})
	api.HandleFunc("POST /items", func(w ResponseWriter, req *Request) {
		w.Write([]byte("create"))
	})

	tests := []struct {
		method string
		want   string
	}{
		{"GET", "list"},
		{"POST", "create"},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tt.method, "/api/items", nil)
		r.ServeHTTP(w, req)
		if got := w.Body.String(); got != tt.want {
			t.Errorf("%s /api/items body = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestGroupNested(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	v1 := api.Group("/v1")
	v1.HandleFunc("GET /items", func(w ResponseWriter, req *Request) {
		w.Write([]byte("v1-items"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/items", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "v1-items" {
		t.Errorf("body = %q, want %q", got, "v1-items")
	}
}

func TestGroupMiddleware(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /public", func(w ResponseWriter, req *Request) {
		w.Write([]byte("public"))
	})

	api := r.Group("/api")
	api.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Set("X-Group", "api")
			next.ServeHTTP(w, req)
		})
	})
	api.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("data"))
	})

	// Group middleware applies to group routes.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/data", nil)
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Group"); got != "api" {
		t.Errorf("X-Group = %q, want %q", got, "api")
	}

	// Group middleware does NOT apply to non-group routes.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/public", nil)
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Group"); got != "" {
		t.Errorf("X-Group on /public = %q, want empty", got)
	}
}

func TestGroupMiddlewareOrder(t *testing.T) {
	r := NewRouter()
	r.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-Order", "router")
			next.ServeHTTP(w, req)
		})
	})

	api := r.Group("/api")
	api.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-Order", "group")
			next.ServeHTTP(w, req)
		})
	})
	api.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Header().Add("X-Order", "handler")
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/test", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Values("X-Order")
	want := []string{"router", "group", "handler"}
	if len(got) != len(want) {
		t.Fatalf("X-Order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("X-Order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNestedGroupMiddleware(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-MW", "api")
			next.ServeHTTP(w, req)
		})
	})

	v1 := api.Group("/v1")
	v1.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Add("X-MW", "v1")
			next.ServeHTTP(w, req)
		})
	})
	v1.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Header().Add("X-MW", "handler")
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Values("X-MW")
	want := []string{"api", "v1", "handler"}
	if len(got) != len(want) {
		t.Fatalf("X-MW = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("X-MW[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGroupNoMiddleware(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.HandleFunc("GET /ping", func(w ResponseWriter, req *Request) {
		w.Write([]byte("pong"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ping", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "pong" {
		t.Errorf("body = %q, want %q", got, "pong")
	}
}

func TestGroupPathParam(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.HandleFunc("GET /users/{id}", func(w ResponseWriter, req *Request) {
		w.Write([]byte(PathParam(req, "id")))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/users/99", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "99" {
		t.Errorf("body = %q, want %q", got, "99")
	}
}

// === Request Helper Tests ===

func TestQueryParam(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /search", func(w ResponseWriter, req *Request) {
		w.Write([]byte(QueryParam(req, "q")))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=hello", nil)
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "hello" {
		t.Errorf("body = %q, want %q", got, "hello")
	}
}

func TestQueryParamMissing(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	if got := QueryParam(req, "missing"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestQueryParamOr(t *testing.T) {
	tests := []struct {
		url      string
		name     string
		fallback string
		want     string
	}{
		{"/test?sort=name", "sort", "id", "name"},
		{"/test", "sort", "id", "id"},
		{"/test?sort=", "sort", "id", "id"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.url, nil)
		got := QueryParamOr(req, tt.name, tt.fallback)
		if got != tt.want {
			t.Errorf("QueryParamOr(%q, %q, %q) = %q, want %q", tt.url, tt.name, tt.fallback, got, tt.want)
		}
	}
}

func TestQueryParamInt(t *testing.T) {
	tests := []struct {
		url      string
		name     string
		fallback int
		want     int
	}{
		{"/test?page=3", "page", 1, 3},
		{"/test?page=0", "page", 1, 0},
		{"/test", "page", 1, 1},
		{"/test?page=abc", "page", 1, 1},
		{"/test?page=", "page", 1, 1},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.url, nil)
		got := QueryParamInt(req, tt.name, tt.fallback)
		if got != tt.want {
			t.Errorf("QueryParamInt(%q, %q, %d) = %d, want %d", tt.url, tt.name, tt.fallback, got, tt.want)
		}
	}
}

func TestReadJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	body := strings.NewReader(`{"name":"John","age":30}`)
	req := httptest.NewRequest("POST", "/users", body)

	var p payload
	if err := ReadJSON(req, &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "John" {
		t.Errorf("name = %q, want %q", p.Name, "John")
	}
	if p.Age != 30 {
		t.Errorf("age = %d, want %d", p.Age, 30)
	}
}

func TestReadJSONInvalid(t *testing.T) {
	body := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest("POST", "/test", body)

	var v map[string]any
	if err := ReadJSON(req, &v); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadJSONEmpty(t *testing.T) {
	body := strings.NewReader("")
	req := httptest.NewRequest("POST", "/test", body)

	var v map[string]any
	if err := ReadJSON(req, &v); err == nil {
		t.Error("expected error for empty body")
	}
}

func TestReadJSONLimit(t *testing.T) {
	// Create a body larger than the limit.
	large := strings.Repeat("x", 100)
	body := strings.NewReader(`{"data":"` + large + `"}`)
	req := httptest.NewRequest("POST", "/test", body)

	var v map[string]any
	// Limit to 50 bytes — body is larger, so decoding should fail
	// because the JSON object is incomplete within the limit.
	err := ReadJSONLimit(req, &v, 50)
	if err == nil {
		t.Error("expected error when body exceeds limit")
	}
}

// === Response Helper Tests ===

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	WriteJSON(w, StatusOK, data)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["key"] != "value" {
		t.Errorf("key = %q, want %q", got["key"], "value")
	}
}

func TestWriteJSONNull(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, StatusOK, nil)

	got := strings.TrimSpace(w.Body.String())
	if got != "null" {
		t.Errorf("body = %q, want %q", got, "null")
	}
}

func TestWriteJSONSlice(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, StatusOK, []string{"a", "b"})

	var got []string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestWriteJSONCreated(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, StatusCreated, map[string]int{"id": 1})

	if w.Code != StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, StatusCreated)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, StatusBadRequest, "invalid input")

	if w.Code != StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, StatusBadRequest)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["error"] != "invalid input" {
		t.Errorf("error = %q, want %q", got["error"], "invalid input")
	}
}

// === Server Tests ===

func TestServerStartStop(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /ping", func(w ResponseWriter, req *Request) {
		w.Write([]byte("pong"))
	})

	srv := NewServer(r, WithAddr("127.0.0.1:0"))
	ctx := context.Background()

	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(ctx)

	resp, err := nethttp.Get("http://" + srv.Addr() + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("body = %q, want %q", body, "pong")
	}
}

func TestServerAddr(t *testing.T) {
	srv := NewServer(NewRouter(), WithAddr("127.0.0.1:0"))

	if addr := srv.Addr(); addr != "" {
		t.Errorf("addr before start = %q, want empty", addr)
	}

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(ctx)

	addr := srv.Addr()
	if addr == "" {
		t.Error("addr after start is empty")
	}
	if !strings.Contains(addr, "127.0.0.1:") {
		t.Errorf("addr = %q, want 127.0.0.1:*", addr)
	}
}

func TestServerOptions(t *testing.T) {
	srv := NewServer(NewRouter(),
		WithAddr(":9999"),
		WithReadTimeout(5000000000),  // 5s
		WithWriteTimeout(5000000000), // 5s
		WithIdleTimeout(30000000000), // 30s
	)
	// Verify the server was created without error.
	if srv == nil {
		t.Fatal("server is nil")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /ok", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	srv := NewServer(r, WithAddr("127.0.0.1:0"))
	ctx := context.Background()

	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify it's serving.
	resp, err := nethttp.Get("http://" + srv.Addr() + "/ok")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Stop should return nil.
	if err := srv.Stop(ctx); err != nil {
		t.Errorf("stop error = %v", err)
	}

	// After stop, new connections should fail.
	resp, err = nethttp.Get("http://" + srv.Addr() + "/ok")
	if err == nil {
		resp.Body.Close()
		t.Error("expected error after stop")
	}
}

// === Recovery Middleware Tests ===

func TestRecovery(t *testing.T) {
	r := NewRouter()
	r.Use(Recovery(nil))
	r.HandleFunc("GET /panic", func(w ResponseWriter, req *Request) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/panic", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, StatusInternalServerError)
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["error"] != "internal server error" {
		t.Errorf("error = %q, want %q", got["error"], "internal server error")
	}
}

func TestRecoveryCallback(t *testing.T) {
	var recovered any
	var gotStack []byte

	r := NewRouter()
	r.Use(Recovery(func(v any, stack []byte) {
		recovered = v
		gotStack = stack
	}))
	r.HandleFunc("GET /panic", func(w ResponseWriter, req *Request) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/panic", nil)
	r.ServeHTTP(w, req)

	if recovered != "test panic" {
		t.Errorf("recovered = %v, want %q", recovered, "test panic")
	}
	if len(gotStack) == 0 {
		t.Error("stack trace is empty")
	}
}

func TestRecoveryNoPanic(t *testing.T) {
	r := NewRouter()
	r.Use(Recovery(nil))
	r.HandleFunc("GET /ok", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ok", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

// === Integration Tests ===

func TestFullStack(t *testing.T) {
	r := NewRouter()

	// Global middleware: add request ID header.
	r.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			w.Header().Set("X-Request-Id", "req-123")
			next.ServeHTTP(w, req)
		})
	})

	// Public route.
	r.HandleFunc("GET /health", func(w ResponseWriter, req *Request) {
		WriteJSON(w, StatusOK, map[string]string{"status": "ok"})
	})

	// API group with auth middleware.
	api := r.Group("/api")
	api.Use(func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, req *Request) {
			if req.Header.Get("Authorization") == "" {
				WriteError(w, StatusUnauthorized, "missing token")
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	api.HandleFunc("GET /users/{id}", func(w ResponseWriter, req *Request) {
		id := PathParam(req, "id")
		WriteJSON(w, StatusOK, map[string]string{"id": id})
	})
	api.HandleFunc("POST /users", func(w ResponseWriter, req *Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := ReadJSON(req, &body); err != nil {
			WriteError(w, StatusBadRequest, "invalid json")
			return
		}
		WriteJSON(w, StatusCreated, map[string]string{"name": body.Name})
	})

	// Test health endpoint.
	t.Run("health", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/health", nil)
		r.ServeHTTP(w, req)

		if w.Code != StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		if w.Header().Get("X-Request-Id") != "req-123" {
			t.Error("missing request ID header")
		}
	})

	// Test API without auth.
	t.Run("api_no_auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/users/1", nil)
		r.ServeHTTP(w, req)

		if w.Code != StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, StatusUnauthorized)
		}
	})

	// Test API with auth — GET.
	t.Run("api_get_user", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/users/42", nil)
		req.Header.Set("Authorization", "Bearer token")
		r.ServeHTTP(w, req)

		if w.Code != StatusOK {
			t.Errorf("status = %d", w.Code)
		}
		var got map[string]string
		json.Unmarshal(w.Body.Bytes(), &got)
		if got["id"] != "42" {
			t.Errorf("id = %q, want %q", got["id"], "42")
		}
	})

	// Test API with auth — POST.
	t.Run("api_create_user", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"name":"Alice"}`))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/users", body)
		req.Header.Set("Authorization", "Bearer token")
		r.ServeHTTP(w, req)

		if w.Code != StatusCreated {
			t.Errorf("status = %d, want %d", w.Code, StatusCreated)
		}
		var got map[string]string
		json.Unmarshal(w.Body.Bytes(), &got)
		if got["name"] != "Alice" {
			t.Errorf("name = %q, want %q", got["name"], "Alice")
		}
	})

	// Test API with auth — invalid JSON.
	t.Run("api_bad_json", func(t *testing.T) {
		body := bytes.NewReader([]byte(`not json`))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/users", body)
		req.Header.Set("Authorization", "Bearer token")
		r.ServeHTTP(w, req)

		if w.Code != StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, StatusBadRequest)
		}
	})
}

// === RequestLogger Middleware Tests ===

func TestRequestLoggerInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	r := NewRouter()
	r.Use(RequestLogger(logger))
	r.HandleFunc("GET /ok", func(w ResponseWriter, req *Request) {
		WriteJSON(w, StatusOK, map[string]string{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ok", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	r.ServeHTTP(w, req)

	entry := parseLogEntry(t, buf.Bytes())
	if entry["level"] != "info" {
		t.Errorf("level = %q, want %q", entry["level"], "info")
	}
	if entry["msg"] != "http request" {
		t.Errorf("msg = %q, want %q", entry["msg"], "http request")
	}
	if entry["method"] != "GET" {
		t.Errorf("method = %q, want %q", entry["method"], "GET")
	}
	if entry["path"] != "/ok" {
		t.Errorf("path = %q, want %q", entry["path"], "/ok")
	}
	if status, ok := entry["status"].(float64); !ok || int(status) != 200 {
		t.Errorf("status = %v, want 200", entry["status"])
	}
	if entry["remote"] != "192.168.1.1:12345" {
		t.Errorf("remote = %q, want %q", entry["remote"], "192.168.1.1:12345")
	}
	if _, ok := entry["duration"]; !ok {
		t.Error("missing duration field")
	}
	if b, ok := entry["bytes"].(float64); !ok || b == 0 {
		t.Errorf("bytes = %v, want > 0", entry["bytes"])
	}
}

func TestRequestLoggerError(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	r := NewRouter()
	r.Use(RequestLogger(logger))
	r.HandleFunc("POST /fail", func(w ResponseWriter, req *Request) {
		WriteError(w, StatusInternalServerError, "something broke")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/fail", nil)
	r.ServeHTTP(w, req)

	entry := parseLogEntry(t, buf.Bytes())
	if entry["level"] != "error" {
		t.Errorf("level = %q, want %q", entry["level"], "error")
	}
	if entry["method"] != "POST" {
		t.Errorf("method = %q, want %q", entry["method"], "POST")
	}
	if status, ok := entry["status"].(float64); !ok || int(status) != 500 {
		t.Errorf("status = %v, want 500", entry["status"])
	}
}

func TestRequestLoggerWithRecovery(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	r := NewRouter()
	r.Use(RequestLogger(logger))
	r.Use(Recovery(nil))
	r.HandleFunc("GET /panic", func(w ResponseWriter, req *Request) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/panic", nil)
	r.ServeHTTP(w, req)

	entry := parseLogEntry(t, buf.Bytes())
	if entry["level"] != "error" {
		t.Errorf("level = %q, want %q", entry["level"], "error")
	}
	if status, ok := entry["status"].(float64); !ok || int(status) != 500 {
		t.Errorf("status = %v, want 500", entry["status"])
	}
}

func TestRequestLogger404(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	r := NewRouter()
	r.Use(RequestLogger(logger))
	r.HandleFunc("GET /exists", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nope", nil)
	r.ServeHTTP(w, req)

	entry := parseLogEntry(t, buf.Bytes())
	if entry["level"] != "info" {
		t.Errorf("level = %q, want %q", entry["level"], "info")
	}
	if status, ok := entry["status"].(float64); !ok || int(status) != 404 {
		t.Errorf("status = %v, want 404", entry["status"])
	}
}

// === responseRecorder Tests ===

func TestResponseRecorderDefaults(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: w, status: StatusOK}

	rec.Write([]byte("hello"))

	if rec.status != StatusOK {
		t.Errorf("status = %d, want %d", rec.status, StatusOK)
	}
	if rec.written != 5 {
		t.Errorf("written = %d, want 5", rec.written)
	}
}

func TestResponseRecorderExplicitStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: w, status: StatusOK}

	rec.WriteHeader(StatusNotFound)
	rec.Write([]byte("not found"))

	if rec.status != StatusNotFound {
		t.Errorf("status = %d, want %d", rec.status, StatusNotFound)
	}
	if rec.written != 9 {
		t.Errorf("written = %d, want 9", rec.written)
	}
}

func TestResponseRecorderDoubleWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: w, status: StatusOK}

	rec.WriteHeader(StatusCreated)
	rec.WriteHeader(StatusBadRequest) // second call — status should not change

	if rec.status != StatusCreated {
		t.Errorf("status = %d, want %d (first call wins)", rec.status, StatusCreated)
	}
}

// newTestLogger creates a logger that writes to the given buffer at debug level.
func newTestLogger(buf *bytes.Buffer) *log.Logger {
	return log.New(
		log.WithLevel(log.LevelDebug),
		log.WithWriter(buf),
	)
}

// parseLogEntry parses a single JSON log line into a map.
func parseLogEntry(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v\nraw: %s", err, data)
	}
	return entry
}

// === CORS Middleware Tests ===

func TestCORSPreflight(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("POST /api/login", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/login", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if w.Code != StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:23705" {
		t.Errorf("Allow-Origin = %q, want %q", got, "http://localhost:23705")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("missing Allow-Methods header")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("missing Allow-Headers header")
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age = %q, want %q", got, "86400")
	}
	if got := w.Body.Len(); got != 0 {
		t.Errorf("body length = %d, want 0", got)
	}
}

func TestCORSActualRequest(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("GET /api/health", func(w ResponseWriter, req *Request) {
		WriteJSON(w, StatusOK, map[string]string{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:23705" {
		t.Errorf("Allow-Origin = %q, want %q", got, "http://localhost:23705")
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want %q", got, "Origin")
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("GET /api/health", func(w ResponseWriter, req *Request) {
		WriteJSON(w, StatusOK, map[string]string{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "http://evil.com")
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty (origin not allowed)", got)
	}
}

func TestCORSNoOriginHeader(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("GET /api/health", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	// No Origin header — same-origin request
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty (no Origin header)", got)
	}
}

func TestCORSWildcard(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"*"},
	}))
	r.HandleFunc("GET /api/public", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/public", nil)
	req.Header.Set("Origin", "http://any-site.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want %q", got, "*")
	}
	if got := w.Header().Get("Vary"); got != "" {
		t.Errorf("Vary = %q, want empty (wildcard origin)", got)
	}
}

func TestCORSCredentials(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins:     []string{"http://localhost:23705"},
		AllowCredentials: true,
	}))
	r.HandleFunc("GET /api/me", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want %q", got, "true")
	}
	// With credentials, origin must be echoed, not "*"
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:23705" {
		t.Errorf("Allow-Origin = %q, want %q", got, "http://localhost:23705")
	}
}

func TestCORSCredentialsPreflight(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins:     []string{"http://localhost:23705"},
		AllowCredentials: true,
	}))
	r.HandleFunc("POST /api/login", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/login", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if w.Code != StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want %q", got, "true")
	}
}

func TestCORSCustomMethods(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
		AllowMethods: []string{"GET", "POST"},
	}))
	r.HandleFunc("POST /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("Allow-Methods = %q, want %q", got, "GET, POST")
	}
}

func TestCORSCustomHeaders(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
		AllowHeaders: []string{"X-Custom", "Authorization"},
	}))
	r.HandleFunc("POST /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "X-Custom, Authorization" {
		t.Errorf("Allow-Headers = %q, want %q", got, "X-Custom, Authorization")
	}
}

func TestCORSCustomMaxAge(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
		MaxAge:       3600,
	}))
	r.HandleFunc("POST /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("Max-Age = %q, want %q", got, "3600")
	}
}

func TestCORSMultipleOrigins(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705", "http://localhost:23700"},
	}))
	r.HandleFunc("GET /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	tests := []struct {
		origin string
		want   string
	}{
		{"http://localhost:23705", "http://localhost:23705"},
		{"http://localhost:23700", "http://localhost:23700"},
		{"http://evil.com", ""},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("Origin", tt.origin)
		r.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.want {
			t.Errorf("origin=%q: Allow-Origin = %q, want %q", tt.origin, got, tt.want)
		}
	}
}

func TestCORSPreflightDoesNotCallHandler(t *testing.T) {
	called := false
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("POST /api/data", func(w ResponseWriter, req *Request) {
		called = true
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if called {
		t.Error("handler was called on preflight — should be short-circuited by CORS middleware")
	}
}

func TestCORSOptionsWithoutPreflight(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{
		AllowOrigins: []string{"http://localhost:23705"},
	}))
	r.HandleFunc("OPTIONS /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("custom-options"))
	})

	// OPTIONS with Origin but without Access-Control-Request-Method
	// is NOT a preflight — should pass through to handler.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "custom-options" {
		t.Errorf("body = %q, want %q", got, "custom-options")
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:23705" {
		t.Errorf("Allow-Origin = %q, want %q", got, "http://localhost:23705")
	}
}

func TestCORSEmptyConfig(t *testing.T) {
	r := NewRouter()
	r.Use(CORS(CORSConfig{}))
	r.HandleFunc("GET /api/data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Origin", "http://localhost:23705")
	r.ServeHTTP(w, req)

	// No origins configured — no CORS headers.
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty (no origins configured)", got)
	}
	if got := w.Body.String(); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

// === Static Handler Tests ===

func TestStaticServesFile(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":       {Data: []byte("<html>app</html>")},
		"assets/style.css": {Data: []byte("body{}")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /{path...}", Static(fsys))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/assets/style.css", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "body{}" {
		t.Errorf("body = %q, want %q", got, "body{}")
	}
}

func TestStaticRootServesIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html>root</html>")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /{path...}", Static(fsys))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<html>root</html>") {
		t.Errorf("body = %q, want to contain index.html content", w.Body.String())
	}
}

func TestStaticSPAFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html>spa</html>")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /{path...}", Static(fsys))

	// Request a path without a file extension — should serve index.html.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<html>spa</html>") {
		t.Errorf("body = %q, want SPA fallback with index.html", w.Body.String())
	}
}

func TestStaticSPAFallbackNestedPath(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html>spa</html>")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /{path...}", Static(fsys))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/123/edit", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<html>spa</html>") {
		t.Errorf("body = %q, want SPA fallback", w.Body.String())
	}
}

func TestStaticMissingAsset404(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("<html>app</html>")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /{path...}", Static(fsys))

	// Request a non-existent file WITH extension — should return 404.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing.js", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, StatusNotFound)
	}
}

func TestStaticWithPrefix(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":       {Data: []byte("<html>admin</html>")},
		"assets/app.js":    {Data: []byte("console.log('hi')")},
	}

	mux := nethttp.NewServeMux()
	mux.Handle("GET /admin/{path...}", Static(fsys))

	// Serve index via prefix root.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<html>admin</html>") {
		t.Errorf("body = %q, want admin index", w.Body.String())
	}

	// Serve asset under prefix.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/admin/assets/app.js", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "console.log('hi')" {
		t.Errorf("body = %q, want js content", got)
	}

	// SPA fallback under prefix.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/admin/settings", nil)
	mux.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Errorf("status = %d, want %d", w.Code, StatusOK)
	}
	if !strings.Contains(w.Body.String(), "<html>admin</html>") {
		t.Errorf("body = %q, want SPA fallback", w.Body.String())
	}
}

// === parsePattern Tests ===

func TestParsePattern(t *testing.T) {
	tests := []struct {
		pattern    string
		wantMethod string
		wantPath   string
	}{
		{"GET /users", "GET", "/users"},
		{"POST /users", "POST", "/users"},
		{"DELETE /users/{id}", "DELETE", "/users/{id}"},
		{"/static/", "", "/static/"},
		{"GET /", "GET", "/"},
	}
	for _, tt := range tests {
		method, path := parsePattern(tt.pattern)
		if method != tt.wantMethod || path != tt.wantPath {
			t.Errorf("parsePattern(%q) = (%q, %q), want (%q, %q)",
				tt.pattern, method, path, tt.wantMethod, tt.wantPath)
		}
	}
}

func TestSecureHeadersDefaults(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		WriteJSON(w, StatusOK, map[string]string{"ok": "true"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, StatusOK)
	}

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"Referrer-Policy":       "strict-origin-when-cross-origin",
		"X-XSS-Protection":     "0",
		"Permissions-Policy":    "camera=(), microphone=(), geolocation=()",
	}
	for header, want := range checks {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	// HSTS and CSP should be absent by default.
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want empty", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("Content-Security-Policy = %q, want empty", got)
	}
}

func TestSecureHeadersCustomFrameOptions(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		FrameOptions: "SAMEORIGIN",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "SAMEORIGIN")
	}
}

func TestSecureHeadersHSTS(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		HSTSMaxAge: 63072000,
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	want := "max-age=63072000; includeSubDomains"
	if got := w.Header().Get("Strict-Transport-Security"); got != want {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, want)
	}
}

func TestSecureHeadersCSP(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		ContentSecurityPolicy: "default-src 'self'",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("Content-Security-Policy = %q, want %q", got, "default-src 'self'")
	}
}

func TestSecureHeadersCustomReferrerPolicy(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		ReferrerPolicy: "no-referrer",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want %q", got, "no-referrer")
	}
}

func TestSecureHeadersCustomPermissionsPolicy(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		PermissionsPolicy: "camera=(), microphone=()",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Permissions-Policy"); got != "camera=(), microphone=()" {
		t.Errorf("Permissions-Policy = %q, want %q", got, "camera=(), microphone=()")
	}
}

func TestSecureHeadersFullConfig(t *testing.T) {
	r := NewRouter()
	r.Use(SecureHeaders(SecureHeadersConfig{
		FrameOptions:          "SAMEORIGIN",
		ReferrerPolicy:        "no-referrer",
		PermissionsPolicy:     "camera=(self)",
		HSTSMaxAge:            31536000,
		ContentSecurityPolicy: "default-src 'self'; script-src 'self'",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	checks := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":          "SAMEORIGIN",
		"Referrer-Policy":          "no-referrer",
		"X-XSS-Protection":        "0",
		"Permissions-Policy":       "camera=(self)",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Content-Security-Policy":  "default-src 'self'; script-src 'self'",
	}
	for header, want := range checks {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// === RateLimit Tests ===

func TestRateLimitAllowsUnderLimit(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 5, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	for i := range 5 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		r.ServeHTTP(w, req)

		if w.Code != StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, w.Code, StatusOK)
		}
		if got := w.Body.String(); got != "ok" {
			t.Fatalf("request %d: body = %q, want %q", i+1, got, "ok")
		}
	}
}

func TestRateLimitBlocksOverLimit(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 3, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// First 3 requests should pass.
	for i := range 3 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		r.ServeHTTP(w, req)

		if w.Code != StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, w.Code, StatusOK)
		}
	}

	// 4th request should be blocked.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	if w.Code != StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, StatusTooManyRequests)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("error = %q, want %q", body["error"], "rate limit exceeded")
	}
}

func TestRateLimitHeaders(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 5, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// First request: remaining should be limit-1.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-RateLimit-Limit"); got != "5" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", got, "5")
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "4" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "4")
	}
	if got := w.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Error("X-RateLimit-Reset header is empty")
	}
}

func TestRateLimitRetryAfterHeader(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 1, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// Exhaust the limit.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	// Next request should have Retry-After.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	if w.Code != StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, StatusTooManyRequests)
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("Retry-After header is empty")
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After %q is not an integer: %v", retryAfter, err)
	}
	if seconds < 1 || seconds > 60 {
		t.Errorf("Retry-After = %d, want 1..60", seconds)
	}
}

func TestRateLimitPerIPIsolation(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 2, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// Exhaust limit for IP A.
	for range 2 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		r.ServeHTTP(w, req)
	}

	// IP A is blocked.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusTooManyRequests {
		t.Fatalf("IP A: status = %d, want %d", w.Code, StatusTooManyRequests)
	}

	// IP B should still be allowed.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("IP B: status = %d, want %d", w.Code, StatusOK)
	}
}

func TestRateLimitWindowReset(t *testing.T) {
	window := 50 * time.Millisecond
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 1, Window: window}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// First request passes.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("first: status = %d, want %d", w.Code, StatusOK)
	}

	// Second request blocked.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusTooManyRequests {
		t.Fatalf("second: status = %d, want %d", w.Code, StatusTooManyRequests)
	}

	// Wait for window to expire.
	time.Sleep(window + 10*time.Millisecond)

	// Request should pass again.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("after reset: status = %d, want %d", w.Code, StatusOK)
	}
}

func TestRateLimitCustomMessage(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{
		Limit:   1,
		Window:  time.Minute,
		Message: "slow down",
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// Exhaust limit.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	// Check custom message.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "slow down" {
		t.Errorf("error = %q, want %q", body["error"], "slow down")
	}
}

func TestRateLimitCustomKeyFunc(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{
		Limit:  2,
		Window: time.Minute,
		KeyFunc: func(r *Request) string {
			return r.Header.Get("X-API-Key")
		},
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// Exhaust limit for key "abc".
	for range 2 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "abc")
		r.ServeHTTP(w, req)
	}

	// Key "abc" blocked.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "abc")
	r.ServeHTTP(w, req)
	if w.Code != StatusTooManyRequests {
		t.Fatalf("key abc: status = %d, want %d", w.Code, StatusTooManyRequests)
	}

	// Key "xyz" still allowed.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "xyz")
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("key xyz: status = %d, want %d", w.Code, StatusOK)
	}
}

func TestRateLimitDefaults(t *testing.T) {
	// Zero-value config should use defaults (60 req/min).
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	// 60 requests should all pass.
	for i := range 60 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		r.ServeHTTP(w, req)

		if w.Code != StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, w.Code, StatusOK)
		}
	}

	// 61st should be blocked.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusTooManyRequests {
		t.Fatalf("request 61: status = %d, want %d", w.Code, StatusTooManyRequests)
	}
}

func TestRateLimitRemainingDecrement(t *testing.T) {
	r := NewRouter()
	r.Use(RateLimit(RateLimitConfig{Limit: 3, Window: time.Minute}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	expected := []string{"2", "1", "0"}
	for i, want := range expected {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		r.ServeHTTP(w, req)

		got := w.Header().Get("X-RateLimit-Remaining")
		if got != want {
			t.Errorf("request %d: X-RateLimit-Remaining = %q, want %q", i+1, got, want)
		}
	}

	// Blocked request should also show remaining=0.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("blocked request: X-RateLimit-Remaining = %q, want %q", got, "0")
	}
}

func TestClientIPXForwardedFor(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		xri    string
		remote string
		want   string
	}{
		{"single XFF", "1.2.3.4", "", "9.9.9.9:1234", "1.2.3.4"},
		{"multi XFF", "1.2.3.4, 5.6.7.8, 9.10.11.12", "", "9.9.9.9:1234", "1.2.3.4"},
		{"XRI fallback", "", "5.6.7.8", "9.9.9.9:1234", "5.6.7.8"},
		{"RemoteAddr fallback", "", "", "9.9.9.9:1234", "9.9.9.9"},
		{"RemoteAddr no port", "", "", "9.9.9.9", "9.9.9.9"},
		{"XFF with spaces", " 1.2.3.4 , 5.6.7.8 ", "", "9.9.9.9:1234", "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			got := ClientIP(req)
			if got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimitOnGroup(t *testing.T) {
	r := NewRouter()
	limited := r.Group("/limited")
	limited.Use(RateLimit(RateLimitConfig{Limit: 1, Window: time.Minute}))
	limited.HandleFunc("GET /endpoint", func(w ResponseWriter, req *Request) {
		w.Write([]byte("limited"))
	})

	// Unlimited route on the same router.
	r.HandleFunc("GET /free", func(w ResponseWriter, req *Request) {
		w.Write([]byte("free"))
	})

	// First limited request passes.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/limited/endpoint", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("limited first: status = %d, want %d", w.Code, StatusOK)
	}

	// Second limited request blocked.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/limited/endpoint", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusTooManyRequests {
		t.Fatalf("limited second: status = %d, want %d", w.Code, StatusTooManyRequests)
	}

	// Unlimited route still works for the same IP.
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/free", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	if w.Code != StatusOK {
		t.Fatalf("free: status = %d, want %d", w.Code, StatusOK)
	}
}

// === RequestID Tests ===

func TestRequestIDGeneratesUUID(t *testing.T) {
	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, StatusOK)
	}

	id := w.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	// UUID v4 format: 8-4-4-4-12 hex chars = 36 total.
	if len(id) != 36 {
		t.Errorf("X-Request-ID length = %d, want 36", len(id))
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("X-Request-ID = %q, not valid UUID format", id)
	}
}

func TestRequestIDReusesIncoming(t *testing.T) {
	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-trace-id-123")
	r.ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got != "my-trace-id-123" {
		t.Errorf("X-Request-ID = %q, want %q", got, "my-trace-id-123")
	}
}

func TestRequestIDUnique(t *testing.T) {
	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	ids := make(map[string]bool)
	for range 100 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		id := w.Header().Get("X-Request-ID")
		if ids[id] {
			t.Fatalf("duplicate request ID: %s", id)
		}
		ids[id] = true
	}
}

func TestRequestIDContext(t *testing.T) {
	var captured string

	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		captured = GetRequestID(req)
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "ctx-test-456")
	r.ServeHTTP(w, req)

	if captured != "ctx-test-456" {
		t.Errorf("GetRequestID() = %q, want %q", captured, "ctx-test-456")
	}
}

func TestGetRequestIDWithoutMiddleware(t *testing.T) {
	var captured string

	r := NewRouter()
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		captured = GetRequestID(req)
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if captured != "" {
		t.Errorf("GetRequestID() = %q, want empty", captured)
	}
}

func TestRequestIDCustomHeader(t *testing.T) {
	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{Header: "X-Trace-ID"}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Trace-ID", "custom-trace")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Trace-ID"); got != "custom-trace" {
		t.Errorf("X-Trace-ID = %q, want %q", got, "custom-trace")
	}
	// Default header should not be set.
	if got := w.Header().Get("X-Request-ID"); got != "" {
		t.Errorf("X-Request-ID = %q, want empty", got)
	}
}

func TestRequestIDCustomGenerator(t *testing.T) {
	counter := 0
	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{
		Generator: func() string {
			counter++
			return "req-" + strconv.Itoa(counter)
		},
	}))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	for i := 1; i <= 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		want := "req-" + strconv.Itoa(i)
		if got := w.Header().Get("X-Request-ID"); got != want {
			t.Errorf("request %d: X-Request-ID = %q, want %q", i, got, want)
		}
	}
}

func TestRequestIDInRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	r := NewRouter()
	r.Use(RequestID(RequestIDConfig{}))
	r.Use(RequestLogger(logger))
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "log-trace-789")
	r.ServeHTTP(w, req)

	entry := parseLogEntry(t, buf.Bytes())
	if entry["request_id"] != "log-trace-789" {
		t.Errorf("log request_id = %v, want %q", entry["request_id"], "log-trace-789")
	}
}

func TestGenerateUUIDFormat(t *testing.T) {
	for range 50 {
		id := generateUUID()
		if len(id) != 36 {
			t.Fatalf("length = %d, want 36", len(id))
		}
		// Check dashes.
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("invalid format: %s", id)
		}
		// Check version: character at index 14 should be '4'.
		if id[14] != '4' {
			t.Errorf("version char = %c, want '4'", id[14])
		}
		// Check variant: character at index 19 should be 8, 9, a, or b.
		v := id[19]
		if v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Errorf("variant char = %c, want 8/9/a/b", v)
		}
	}
}

// === Compress Middleware Tests ===

// gzipDecode decompresses gzipped bytes for test assertions.
func gzipDecode(t *testing.T, data []byte) string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(out)
}

func TestCompressJSONAboveMinSize(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		body := strings.Repeat(`{"key":"value"},`, 100) // ~1600 bytes
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "gzip")
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want %q", got, "Accept-Encoding")
	}

	decoded := gzipDecode(t, w.Body.Bytes())
	want := strings.Repeat(`{"key":"value"},`, 100)
	if decoded != want {
		t.Errorf("decoded body length = %d, want %d", len(decoded), len(want))
	}

	// Compressed should be smaller than original.
	if w.Body.Len() >= len(want) {
		t.Errorf("compressed size %d >= original %d", w.Body.Len(), len(want))
	}
}

func TestCompressSkipsBelowMinSize(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /small", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty (below min size)", got)
	}
	if got := w.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q, want raw JSON", got)
	}
}

func TestCompressSkipsNoAcceptEncoding(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		body := strings.Repeat("x", 2000)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	// No Accept-Encoding header.
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty (no Accept-Encoding)", got)
	}
	if w.Body.Len() != 2000 {
		t.Errorf("body length = %d, want 2000", w.Body.Len())
	}
}

func TestCompressSkipsBinaryContentType(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /image", func(w ResponseWriter, req *Request) {
		body := strings.Repeat("\x00", 2000)
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/image", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty (binary type)", got)
	}
}

func TestCompressTextHTML(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /page", func(w ResponseWriter, req *Request) {
		body := "<html>" + strings.Repeat("<p>paragraph</p>", 200) + "</html>"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/page", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "gzip")
	}

	decoded := gzipDecode(t, w.Body.Bytes())
	want := "<html>" + strings.Repeat("<p>paragraph</p>", 200) + "</html>"
	if decoded != want {
		t.Errorf("decoded length = %d, want %d", len(decoded), len(want))
	}
}

func TestCompressCustomMinSize(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{MinSize: 50}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", 100)))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q (above custom min)", got, "gzip")
	}
}

func TestCompressCustomContentTypes(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{
		MinSize:      50,
		ContentTypes: []string{"application/vnd.custom"},
	}))
	r.HandleFunc("GET /custom", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/vnd.custom+json")
		w.Write([]byte(strings.Repeat("x", 200)))
	})
	r.HandleFunc("GET /json", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(strings.Repeat("x", 200)))
	})

	// Custom type should be compressed.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/custom", nil)
	req1.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w1, req1)
	if got := w1.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("custom type: Content-Encoding = %q, want gzip", got)
	}

	// JSON should NOT be compressed (not in custom list).
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/json", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("json type: Content-Encoding = %q, want empty (not in custom list)", got)
	}
}

func TestCompressRemovesContentLength(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		body := strings.Repeat("z", 2000)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "2000")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want empty (removed by compress)", got)
	}
}

func TestCompressMultipleWrites(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{MinSize: 50}))
	r.HandleFunc("GET /multi", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Write in multiple small chunks that together exceed minSize.
		for i := 0; i < 20; i++ {
			w.Write([]byte(strings.Repeat("x", 10)))
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/multi", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	decoded := gzipDecode(t, w.Body.Bytes())
	if len(decoded) != 200 {
		t.Errorf("decoded length = %d, want 200", len(decoded))
	}
}

func TestCompressPreservesStatusCode(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{MinSize: 10}))
	r.HandleFunc("GET /created", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(StatusCreated)
		w.Write([]byte(strings.Repeat(`{"id":1}`, 20)))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/created", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, StatusCreated)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
}

func TestCompressCustomLevel(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{Level: gzip.BestSpeed}))
	body := strings.Repeat("hello world ", 200)
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	decoded := gzipDecode(t, w.Body.Bytes())
	if decoded != body {
		t.Errorf("decoded body mismatch")
	}
}

func TestCompressSVG(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	svg := "<svg>" + strings.Repeat(`<rect width="10" height="10"/>`, 100) + "</svg>"
	r.HandleFunc("GET /icon.svg", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(svg))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/icon.svg", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip (SVG is text)", got)
	}
}

func TestCompressAcceptEncodingVariants(t *testing.T) {
	handler := func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", 2000)))
	}

	tests := []struct {
		name     string
		encoding string
		want     bool
	}{
		{"gzip only", "gzip", true},
		{"gzip with others", "deflate, gzip, br", true},
		{"gzip with quality", "gzip, deflate", true},
		{"no gzip", "deflate, br", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter()
			r.Use(Compress(CompressConfig{}))
			r.HandleFunc("GET /data", handler)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/data", nil)
			if tt.encoding != "" {
				req.Header.Set("Accept-Encoding", tt.encoding)
			}
			r.ServeHTTP(w, req)

			got := w.Header().Get("Content-Encoding") == "gzip"
			if got != tt.want {
				t.Errorf("compressed = %v, want %v for Accept-Encoding %q", got, tt.want, tt.encoding)
			}
		})
	}
}

// === ETag Middleware Tests ===

func TestETagSetsHeader(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hello":"world"}`))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, StatusOK)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header, got empty")
	}
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag %q should be quoted", etag)
	}
	if got := w.Body.String(); got != `{"hello":"world"}` {
		t.Errorf("body = %q, want %q", got, `{"hello":"world"}`)
	}
}

func TestETag304WhenMatches(t *testing.T) {
	handler := HandlerFunc(func(w ResponseWriter, req *Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hello":"world"}`))
	})

	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.Handle("GET /data", handler)

	// First request to get the ETag.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on first request")
	}

	// Second request with If-None-Match.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/data", nil)
	req2.Header.Set("If-None-Match", etag)
	r.ServeHTTP(w2, req2)

	if w2.Code != StatusNotModified {
		t.Fatalf("status = %d, want %d", w2.Code, StatusNotModified)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("304 response should have empty body, got %d bytes", w2.Body.Len())
	}
	// ETag header should still be set on 304.
	if got := w2.Header().Get("ETag"); got != etag {
		t.Errorf("ETag on 304 = %q, want %q", got, etag)
	}
}

func TestETag200WhenMismatch(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("hello"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("If-None-Match", `"deadbeef"`)
	r.ServeHTTP(w, req)

	if w.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, StatusOK)
	}
	if got := w.Body.String(); got != "hello" {
		t.Errorf("body = %q, want %q", got, "hello")
	}
}

func TestETagSkipsPOST(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("POST /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("created"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/data", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("ETag"); got != "" {
		t.Errorf("POST should not get ETag, got %q", got)
	}
}

func TestETagSkipsNon2xx(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /err", func(w ResponseWriter, req *Request) {
		w.WriteHeader(StatusNotFound)
		w.Write([]byte("not found"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/err", nil)
	r.ServeHTTP(w, req)

	if w.Code != StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, StatusNotFound)
	}
	if got := w.Header().Get("ETag"); got != "" {
		t.Errorf("non-2xx should not get ETag, got %q", got)
	}
	if got := w.Body.String(); got != "not found" {
		t.Errorf("body = %q, want %q", got, "not found")
	}
}

func TestETagSkipsEmptyBody(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /empty", func(w ResponseWriter, req *Request) {
		w.WriteHeader(StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/empty", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("ETag"); got != "" {
		t.Errorf("empty body should not get ETag, got %q", got)
	}
}

func TestETagWeakETag(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{Weak: true}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("content"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w, req)

	etag := w.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Errorf("weak ETag %q should start with W/", etag)
	}
}

func TestETagWeakComparison(t *testing.T) {
	// Per RFC 7232, If-None-Match uses weak comparison:
	// W/"x" matches "x" and vice versa.
	r := NewRouter()
	r.Use(ETag(ETagConfig{})) // Strong ETags.
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("content"))
	})

	// Get the strong ETag.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag") // e.g., "abc123"

	// Send If-None-Match with W/ prefix — should still match.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/data", nil)
	req2.Header.Set("If-None-Match", "W/"+etag)
	r.ServeHTTP(w2, req2)

	if w2.Code != StatusNotModified {
		t.Fatalf("weak comparison: status = %d, want %d", w2.Code, StatusNotModified)
	}
}

func TestETagWildcard(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("content"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("If-None-Match", "*")
	r.ServeHTTP(w, req)

	if w.Code != StatusNotModified {
		t.Fatalf("wildcard: status = %d, want %d", w.Code, StatusNotModified)
	}
}

func TestETagMultipleValues(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("content"))
	})

	// Get the actual ETag.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")

	// Send If-None-Match with multiple values, one matching.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/data", nil)
	req2.Header.Set("If-None-Match", `"deadbeef", `+etag+`, "cafebabe"`)
	r.ServeHTTP(w2, req2)

	if w2.Code != StatusNotModified {
		t.Fatalf("multi-value match: status = %d, want %d", w2.Code, StatusNotModified)
	}
}

func TestETagConsistentHash(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("deterministic content"))
	})

	// Same content produces same ETag.
	var etags [3]string
	for i := range etags {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/data", nil)
		r.ServeHTTP(w, req)
		etags[i] = w.Header().Get("ETag")
	}

	if etags[0] != etags[1] || etags[1] != etags[2] {
		t.Errorf("ETags should be consistent: %v", etags)
	}
}

func TestETagDifferentContent(t *testing.T) {
	call := 0
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		call++
		fmt.Fprintf(w, "content-%d", call)
	})

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest("GET", "/data", nil))
	etag1 := w1.Header().Get("ETag")

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest("GET", "/data", nil))
	etag2 := w2.Header().Get("ETag")

	if etag1 == etag2 {
		t.Errorf("different content should produce different ETags: %q", etag1)
	}
}

func TestETagPreservesExistingETag(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Header().Set("ETag", `"custom-etag"`)
		w.Write([]byte("content"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/data", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("ETag"); got != `"custom-etag"` {
		t.Errorf("ETag = %q, want %q", got, `"custom-etag"`)
	}
}

func TestETagHEADRequest(t *testing.T) {
	r := NewRouter()
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		w.Write([]byte("content"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("HEAD", "/data", nil)
	r.ServeHTTP(w, req)

	// HEAD should still get an ETag header.
	if got := w.Header().Get("ETag"); got == "" {
		t.Error("HEAD request should get ETag header")
	}
}

func TestETagWithCompress(t *testing.T) {
	r := NewRouter()
	r.Use(Compress(CompressConfig{}))
	r.Use(ETag(ETagConfig{}))
	r.HandleFunc("GET /data", func(w ResponseWriter, req *Request) {
		body := strings.Repeat(`{"key":"value"},`, 100) // >1KB
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})

	// First request: get ETag and compressed body.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/data", nil)
	req1.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w1, req1)

	if w1.Code != StatusOK {
		t.Fatalf("status = %d, want %d", w1.Code, StatusOK)
	}
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}
	if got := w1.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	// Second request with If-None-Match: should get 304.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/data", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	req2.Header.Set("If-None-Match", etag)
	r.ServeHTTP(w2, req2)

	if w2.Code != StatusNotModified {
		t.Fatalf("conditional with compress: status = %d, want %d", w2.Code, StatusNotModified)
	}
}
