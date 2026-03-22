// Package http provides HTTP routing, request/response helpers, and server
// lifecycle management. It wraps Go's standard net/http package with
// middleware support, route groups, and JSON helpers.
//
// Create a router, add routes, and serve:
//
//	r := http.NewRouter()
//	r.Use(http.Recovery(nil))
//	r.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
//	    http.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
//	})
//
//	srv := http.NewServer(r, http.WithAddr(":8080"))
//	srv.Start(ctx)
//	defer srv.Stop(ctx)
//
// Routes use Go 1.22+ ServeMux pattern syntax with method-based routing
// and path parameters:
//
//	r.HandleFunc("GET /users/{id}", getUser)
//	r.HandleFunc("POST /users", createUser)
//
// Organize routes into groups with shared prefixes and middleware:
//
//	api := r.Group("/api")
//	api.Use(authMiddleware)
//	api.HandleFunc("GET /users", listUsers)
package http
