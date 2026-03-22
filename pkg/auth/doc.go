// Package auth provides JWT access tokens, opaque refresh tokens, password
// hashing, API key generation, and authentication middleware. It implements
// a stateless JWT strategy with short-lived access tokens and longer-lived
// refresh tokens for session management.
//
// The token lifecycle uses two cookies: a short-lived JWT access token
// (default 5 minutes) for zero-database-hit authentication, and an opaque
// refresh token (default 24 hours) for obtaining fresh access tokens. Both
// are stored as HttpOnly, Secure, SameSite=Lax cookies.
//
// Basic usage:
//
//	a := auth.New(signingKey,
//	    auth.WithAccessTokenTTL(5*time.Minute),
//	    auth.WithRefreshTokenTTL(24*time.Hour),
//	    auth.WithSecureCookies(true),
//	)
//
// Creating and validating JWT tokens:
//
//	token, err := auth.CreateJWT(key, auth.Claims{UID: "42", Scopes: "admin"})
//	claims, err := auth.ValidateJWT(key, token)
//
// Authentication middleware extracts the JWT from the cookie and injects
// claims into the request context:
//
//	api.Use(a.RequireAuth)
//	// In handlers:
//	claims, ok := auth.ClaimsFromContext(r.Context())
//
// API key authentication:
//
//	api.Use(auth.RequireAPIKey(lookupKeyFunc))
//
// Password hashing uses PBKDF2-SHA256:
//
//	hash, err := auth.HashPassword("secret")
//	ok := auth.VerifyPassword(hash, "secret")
package auth
