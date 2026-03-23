package http

import (
	nethttp "net/http"
	"sort"
	"strings"
	"sync"
)

// Handler is an alias for net/http.Handler.
type Handler = nethttp.Handler

// HandlerFunc is an alias for net/http.HandlerFunc.
type HandlerFunc = nethttp.HandlerFunc

// ResponseWriter is an alias for net/http.ResponseWriter.
type ResponseWriter = nethttp.ResponseWriter

// Request is an alias for net/http.Request.
type Request = nethttp.Request

// Middleware wraps an HTTP handler to add behavior. Middleware functions
// receive the next handler in the chain and return a new handler that
// may execute logic before or after calling next.
type Middleware func(Handler) Handler

// Route describes a registered route with its HTTP method and path.
// Routes are recorded as handlers are registered and can be retrieved
// with Router.Routes for introspection, debugging, or documentation.
type Route struct {
	Method string // HTTP method (GET, POST, PUT, PATCH, DELETE) or empty for catch-all
	Path   string // URL path pattern including parameters (e.g. "/users/{id}")
}

// Router routes HTTP requests to handlers based on method and path.
// It supports middleware, route groups with prefixes, and path parameters.
//
// Routes are registered using Go 1.22+ ServeMux pattern syntax:
//
//	r.HandleFunc("GET /users", listUsers)
//	r.HandleFunc("GET /users/{id}", getUser)
//	r.HandleFunc("POST /users", createUser)
//
// Router middleware is configured with Use and applies to all routes:
//
//	r.Use(recovery, logging)
//
// Configure middleware before the router handles its first request.
// Middleware added after the first request has no effect.
type Router struct {
	mux        *nethttp.ServeMux
	middleware []Middleware
	handler    Handler
	buildOnce  sync.Once
	routes     []Route
}

// NewRouter creates a new Router.
func NewRouter() *Router {
	return &Router{
		mux: nethttp.NewServeMux(),
	}
}

// Use appends middleware to the router. Middleware runs in the order
// added and applies to all requests. Configure middleware before the
// router handles its first request.
func (r *Router) Use(mw ...Middleware) {
	r.middleware = append(r.middleware, mw...)
}

// Handle registers a handler for the given pattern. Patterns follow
// Go 1.22+ ServeMux syntax: "METHOD /path", "/path", "GET /path/{id}".
func (r *Router) Handle(pattern string, handler Handler) {
	r.record(pattern)
	r.mux.Handle(pattern, handler)
}

// HandleFunc registers a handler function for the given pattern.
func (r *Router) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	r.record(pattern)
	r.mux.HandleFunc(pattern, handler)
}

// Routes returns all registered routes sorted by path then method.
// The returned slice is a copy — callers may modify it freely.
func (r *Router) Routes() []Route {
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// record adds a route entry from the given pattern.
func (r *Router) record(pattern string) {
	method, path := parsePattern(pattern)
	r.routes = append(r.routes, Route{Method: method, Path: path})
}

// Group creates a route group with the given path prefix. Routes added
// to the group have the prefix prepended to their path.
func (r *Router) Group(prefix string) *Group {
	return &Group{
		router: r,
		prefix: prefix,
	}
}

// ServeHTTP dispatches the request to the matching handler after
// applying the router's middleware chain.
func (r *Router) ServeHTTP(w ResponseWriter, req *Request) {
	r.buildOnce.Do(func() {
		r.handler = chain(r.middleware, r.mux)
	})
	r.handler.ServeHTTP(w, req)
}

// Group is a collection of routes sharing a common path prefix and
// middleware. Groups can be nested to create hierarchical route
// structures.
//
//	api := r.Group("/api")
//	api.Use(authMiddleware)
//	api.HandleFunc("GET /users", listUsers)   // matches GET /api/users
//
//	v1 := api.Group("/v1")
//	v1.HandleFunc("GET /items", listItems)    // matches GET /api/v1/items
type Group struct {
	router     *Router
	prefix     string
	parent     *Group
	middleware []Middleware
}

// Use appends middleware to the group. Group middleware runs after
// router middleware and before the handler.
func (g *Group) Use(mw ...Middleware) {
	g.middleware = append(g.middleware, mw...)
}

// Handle registers a handler in the group. The group's prefix is
// prepended to the pattern's path, and all group middleware is applied.
func (g *Group) Handle(pattern string, handler Handler) {
	full := g.fullPattern(pattern)
	handler = chain(g.allMiddleware(), handler)
	g.router.record(full)
	g.router.mux.Handle(full, handler)
}

// HandleFunc registers a handler function in the group.
func (g *Group) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	g.Handle(pattern, HandlerFunc(handler))
}

// Group creates a sub-group with an additional path prefix. The
// sub-group inherits middleware from all ancestor groups.
func (g *Group) Group(prefix string) *Group {
	return &Group{
		router: g.router,
		prefix: g.prefix + prefix,
		parent: g,
	}
}

// fullPattern prepends the group's full prefix to the path in the pattern.
func (g *Group) fullPattern(pattern string) string {
	method, path := parsePattern(pattern)
	if method != "" {
		return method + " " + g.prefix + path
	}
	return g.prefix + path
}

// allMiddleware returns the full middleware chain from the root group
// to this group. Parent middleware runs before child middleware.
func (g *Group) allMiddleware() []Middleware {
	if g.parent == nil {
		return g.middleware
	}
	parent := g.parent.allMiddleware()
	if len(g.middleware) == 0 {
		return parent
	}
	mw := make([]Middleware, 0, len(parent)+len(g.middleware))
	mw = append(mw, parent...)
	mw = append(mw, g.middleware...)
	return mw
}

// parsePattern splits "METHOD /path" into method and path. If the
// pattern has no method prefix, it returns an empty method and the
// full pattern as the path.
func parsePattern(pattern string) (method, path string) {
	if i := strings.IndexByte(pattern, ' '); i != -1 {
		return pattern[:i], pattern[i+1:]
	}
	return "", pattern
}

// chain applies middleware to a handler. The first middleware in the
// slice is the outermost (runs first).
func chain(mw []Middleware, handler Handler) Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		handler = mw[i](handler)
	}
	return handler
}
