package auth

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey int

const claimsKey contextKey = 0

// ClaimsFromContext extracts the JWT claims from the request context.
// Returns the claims and true if present, or zero Claims and false if
// the request was not authenticated.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// withClaims returns a new context carrying the JWT claims.
func withClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// WithClaimsForTest is an exported version of withClaims for use in
// integration tests that need to set up authenticated request contexts
// without going through the middleware stack.
func WithClaimsForTest(ctx context.Context, claims Claims) context.Context {
	return withClaims(ctx, claims)
}

// RequireAuth returns middleware that validates the JWT access token
// from the request cookie. If the token is valid, the claims are
// stored in the request context and the next handler is called. If
// the token is missing, invalid, or expired, a 401 JSON response is
// returned.
//
// Use ClaimsFromContext in handlers to access the authenticated claims:
//
//	claims, ok := auth.ClaimsFromContext(r.Context())
func (a *Auth) RequireAuth() func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			token, err := ReadAccessToken(r)
			if err != nil {
				writeUnauthorized(w, "authentication required")
				return
			}

			claims, err := a.ValidateAccessToken(token)
			if errors.Is(err, ErrTokenExpired) {
				writeUnauthorized(w, "token expired")
				return
			}
			if err != nil {
				writeUnauthorized(w, "invalid token")
				return
			}

			ctx := withClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns middleware that checks whether the authenticated
// claims include the specified scope. It must be applied after
// RequireAuth. If the scope is missing, a 403 JSON response is returned.
func RequireScope(scope string) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				writeUnauthorized(w, "authentication required")
				return
			}
			if !claims.HasScope(scope) {
				writeForbidden(w, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeUnauthorized writes a 401 JSON error response.
func writeUnauthorized(w nethttp.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeForbidden writes a 403 JSON error response.
func writeForbidden(w nethttp.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
