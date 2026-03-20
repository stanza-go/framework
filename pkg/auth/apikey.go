package auth

import (
	nethttp "net/http"
	"strings"
)

// KeyValidator looks up an API key by its SHA-256 hash and returns
// the associated claims. Implementations should check for revocation
// and expiration. Return a non-nil error if the key is not found,
// expired, or revoked.
type KeyValidator func(keyHash string) (Claims, error)

// RequireAPIKey creates middleware that authenticates requests using
// an API key provided in the Authorization: Bearer header. The key
// is hashed with SHA-256 via HashToken and passed to the validator
// for lookup.
//
// Use this for routes that only accept API key authentication:
//
//	v1 := api.Group("/v1")
//	v1.Use(auth.RequireAPIKey(validator))
func RequireAPIKey(validator KeyValidator) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			key, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w, "authentication required")
				return
			}

			claims, err := validator(HashToken(key))
			if err != nil {
				writeUnauthorized(w, "invalid api key")
				return
			}

			ctx := withClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuthOrAPIKey creates middleware that tries JWT cookie
// authentication first, then falls back to API key authentication
// from the Authorization: Bearer header. If both fail, a 401
// response is returned.
//
// Use this for routes that accept either browser sessions or
// programmatic API key access:
//
//	user := api.Group("/user")
//	user.Use(a.RequireAuthOrAPIKey(validator))
func (a *Auth) RequireAuthOrAPIKey(validator KeyValidator) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			// Try JWT cookie first.
			token, err := ReadAccessToken(r)
			if err == nil {
				claims, err := a.ValidateAccessToken(token)
				if err == nil {
					ctx := withClaims(r.Context(), claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Fall back to API key from Authorization header.
			key, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w, "authentication required")
				return
			}

			claims, err := validator(HashToken(key))
			if err != nil {
				writeUnauthorized(w, "invalid api key")
				return
			}

			ctx := withClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the token from an Authorization: Bearer header.
func bearerToken(r *nethttp.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := header[7:] // len("Bearer ") == 7
	if token == "" {
		return "", false
	}
	return token, true
}
