package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	_, err = nethttp.Get("http://" + srv.Addr() + "/ok")
	if err == nil {
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
