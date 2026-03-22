package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// Auth manages JWT access tokens and opaque refresh tokens. It holds
// the signing key, token lifetimes, and cookie configuration.
//
// Create with New and functional options:
//
//	a := auth.New(signingKey,
//	    auth.WithAccessTokenTTL(5*time.Minute),
//	    auth.WithRefreshTokenTTL(24*time.Hour),
//	    auth.WithCookiePath("/api/admin"),
//	    auth.WithSecureCookies(true),
//	)
type Auth struct {
	signingKey      []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	cookiePath      string
	authCookiePath  string
	secureCookies   bool

	// Atomic counters for observability.
	totalIssued   atomic.Int64
	totalAccepted atomic.Int64
	totalRejected atomic.Int64
}

// Option configures an Auth instance.
type Option func(*Auth)

// WithAccessTokenTTL sets the lifetime for JWT access tokens.
// Default: 5 minutes.
func WithAccessTokenTTL(d time.Duration) Option {
	return func(a *Auth) {
		a.accessTokenTTL = d
	}
}

// WithRefreshTokenTTL sets the lifetime for opaque refresh tokens.
// Default: 24 hours.
func WithRefreshTokenTTL(d time.Duration) Option {
	return func(a *Auth) {
		a.refreshTokenTTL = d
	}
}

// WithCookiePath sets the base cookie path for the access token.
// The refresh token cookie path is derived by appending "/auth".
// Default: "/api/admin".
func WithCookiePath(path string) Option {
	return func(a *Auth) {
		a.cookiePath = path
		a.authCookiePath = path + "/auth"
	}
}

// WithSecureCookies controls whether cookies are set with the Secure
// flag. Set to false for local development over HTTP. Default: true.
func WithSecureCookies(secure bool) Option {
	return func(a *Auth) {
		a.secureCookies = secure
	}
}

// New creates an Auth with the given HMAC signing key and options.
// The signing key must be at least 32 bytes.
func New(signingKey []byte, opts ...Option) *Auth {
	a := &Auth{
		signingKey:      signingKey,
		accessTokenTTL:  5 * time.Minute,
		refreshTokenTTL: 24 * time.Hour,
		cookiePath:      "/api/admin",
		authCookiePath:  "/api/admin/auth",
		secureCookies:   true,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// IssueAccessToken creates a signed JWT access token with the given
// user ID and scopes. The token expires after the configured access
// token TTL.
func (a *Auth) IssueAccessToken(uid string, scopes []string) (string, error) {
	now := time.Now()
	claims := Claims{
		UID:       uid,
		Scopes:    scopes,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(a.accessTokenTTL).Unix(),
	}
	token, err := CreateJWT(a.signingKey, claims)
	if err == nil {
		a.totalIssued.Add(1)
	}
	return token, err
}

// ValidateAccessToken verifies a JWT access token string and returns
// its claims. Returns ErrInvalidToken or ErrTokenExpired on failure.
func (a *Auth) ValidateAccessToken(token string) (Claims, error) {
	claims, err := ValidateJWT(a.signingKey, token)
	if err != nil {
		a.totalRejected.Add(1)
	} else {
		a.totalAccepted.Add(1)
	}
	return claims, err
}

// GenerateRefreshToken creates a cryptographically random opaque token
// (32 bytes, hex-encoded to 64 characters).
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of an opaque token. Use
// this to hash refresh tokens before storing them in the database.
// Never store raw tokens.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// GenerateAPIKey creates a cryptographically random API key with the
// given prefix (e.g., "stza_"). It returns the full key, a short
// display prefix for identification, and the SHA-256 hash for storage.
//
// The display prefix is the first len(prefix)+8 characters of the full
// key — enough for identification but not reconstruction:
//
//	fullKey, prefix, hash, err := auth.GenerateAPIKey("stza_")
//	// fullKey:  "stza_a1b2c3d4e5f6..." (prefix + 64 hex chars)
//	// prefix:   "stza_a1b2c3d4"        (first 13 chars)
//	// hash:     "9f86d081884c..."       (SHA-256 for DB storage)
func GenerateAPIKey(prefix string) (fullKey, displayPrefix, keyHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("auth: generate api key: %w", err)
	}
	fullKey = prefix + hex.EncodeToString(b)
	displayPrefix = fullKey[:len(prefix)+8]
	keyHash = HashToken(fullKey)
	return fullKey, displayPrefix, keyHash, nil
}

// RefreshTokenTTL returns the configured refresh token lifetime. Use
// this when setting the expires_at column in the database.
func (a *Auth) RefreshTokenTTL() time.Duration {
	return a.refreshTokenTTL
}

// AccessTokenTTL returns the configured access token lifetime.
func (a *Auth) AccessTokenTTL() time.Duration {
	return a.accessTokenTTL
}

// AuthStats holds a snapshot of cumulative auth operation counters.
type AuthStats struct {
	// Issued is the total number of access tokens successfully created.
	Issued int64 `json:"issued"`

	// Accepted is the total number of tokens that passed validation.
	Accepted int64 `json:"accepted"`

	// Rejected is the total number of tokens that failed validation
	// (expired, malformed, or invalid signature).
	Rejected int64 `json:"rejected"`
}

// Stats returns a snapshot of cumulative auth counters. All counters
// are read atomically and are safe to call from any goroutine.
func (a *Auth) Stats() AuthStats {
	return AuthStats{
		Issued:   a.totalIssued.Load(),
		Accepted: a.totalAccepted.Load(),
		Rejected: a.totalRejected.Load(),
	}
}
