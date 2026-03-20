package auth

import "errors"

// Sentinel errors returned by the auth package.
var (
	// ErrInvalidToken indicates a JWT that is malformed, has an invalid
	// signature, or cannot be decoded.
	ErrInvalidToken = errors.New("auth: invalid token")

	// ErrTokenExpired indicates a JWT whose exp claim is in the past.
	ErrTokenExpired = errors.New("auth: token expired")

	// ErrNoToken indicates that no authentication token was found in the
	// request (missing cookie or header).
	ErrNoToken = errors.New("auth: no token")
)
