package http

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Router dispatch ---

func BenchmarkRouter_StaticRoute(b *testing.B) {
	r := NewRouter()
	r.HandleFunc("GET /health", func(w ResponseWriter, req *Request) {
		w.WriteHeader(StatusOK)
	})

	req := httptest.NewRequest("GET", "/health", nil)
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_PathParam(b *testing.B) {
	r := NewRouter()
	r.HandleFunc("GET /users/{id}", func(w ResponseWriter, req *Request) {
		_ = PathParam(req, "id")
		w.WriteHeader(StatusOK)
	})

	req := httptest.NewRequest("GET", "/users/42", nil)
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_ManyRoutes(b *testing.B) {
	r := NewRouter()
	paths := []string{
		"GET /api/health",
		"POST /api/auth/login",
		"GET /api/auth",
		"POST /api/auth/logout",
		"GET /api/admin/dashboard",
		"GET /api/admin/users",
		"POST /api/admin/users",
		"GET /api/admin/users/{id}",
		"PUT /api/admin/users/{id}",
		"DELETE /api/admin/users/{id}",
		"GET /api/admin/admins",
		"GET /api/admin/settings",
		"GET /api/admin/audit",
		"GET /api/admin/queue/jobs",
		"GET /api/admin/cron",
		"GET /api/admin/logs",
		"GET /api/admin/webhooks",
		"GET /api/admin/uploads",
		"GET /api/admin/notifications",
		"GET /api/admin/api-keys",
	}
	for _, p := range paths {
		p := p
		r.HandleFunc(p, func(w ResponseWriter, req *Request) {
			w.WriteHeader(StatusOK)
		})
	}

	req := httptest.NewRequest("GET", "/api/admin/users/42", nil)
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

// --- Middleware chain ---

func noopMiddleware(next Handler) Handler {
	return HandlerFunc(func(w ResponseWriter, r *Request) {
		next.ServeHTTP(w, r)
	})
}

func BenchmarkMiddleware_Chain5(b *testing.B) {
	r := NewRouter()
	r.Use(noopMiddleware, noopMiddleware, noopMiddleware, noopMiddleware, noopMiddleware)
	r.HandleFunc("GET /test", func(w ResponseWriter, req *Request) {
		w.WriteHeader(StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

// --- JSON response ---

func BenchmarkWriteJSON_Small(b *testing.B) {
	payload := map[string]any{
		"id":     42,
		"name":   "John Doe",
		"status": "active",
	}

	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		WriteJSON(w, StatusOK, payload)
	}
}

func BenchmarkWriteJSON_Large(b *testing.B) {
	users := make([]map[string]any, 50)
	for i := range users {
		users[i] = map[string]any{
			"id":         i + 1,
			"name":       "User Name Here",
			"email":      "user@example.com",
			"status":     "active",
			"created_at": "2024-01-15T10:30:00Z",
		}
	}
	payload := map[string]any{
		"data":  users,
		"total": 500,
		"page":  1,
	}

	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		WriteJSON(w, StatusOK, payload)
	}
}

// --- Group routing ---

func BenchmarkRouter_GroupNested(b *testing.B) {
	r := NewRouter()
	api := r.Group("/api")
	admin := api.Group("/admin")
	admin.HandleFunc("GET /users/{id}", func(w ResponseWriter, req *Request) {
		_ = PathParam(req, "id")
		w.WriteHeader(StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/admin/users/42", nil)
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

// --- Request body parsing ---

func BenchmarkReadJSON(b *testing.B) {
	r := NewRouter()
	r.HandleFunc("POST /api/login", func(w ResponseWriter, req *Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		ReadJSON(req, &body)
		w.WriteHeader(StatusOK)
	})

	bodyStr := `{"email":"admin@stanza.dev","password":"admin"}`
	b.ResetTimer()
	for range b.N {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(bodyStr))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
	}
}
