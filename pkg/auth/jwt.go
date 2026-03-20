package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims holds the JWT payload. UID identifies the authenticated entity,
// Scopes lists the granted permissions, and IssuedAt/ExpiresAt control
// token validity.
type Claims struct {
	UID       string   `json:"uid"`
	Scopes    []string `json:"scopes"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
}

// Valid reports whether the claims have not expired relative to the
// current time.
func (c Claims) Valid() bool {
	return time.Now().Unix() < c.ExpiresAt
}

// HasScope reports whether the claims include the named scope.
func (c Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// jwtHeader is the fixed JOSE header for HMAC-SHA256 JWTs.
var jwtHeader = mustBase64JSON(map[string]string{
	"alg": "HS256",
	"typ": "JWT",
})

// CreateJWT signs claims with key using HMAC-SHA256 and returns the
// encoded JWT string. The key must be at least 32 bytes.
func CreateJWT(key []byte, claims Claims) (string, error) {
	if len(key) < 32 {
		return "", fmt.Errorf("auth: signing key must be at least 32 bytes")
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}

	encodedPayload := base64URLEncode(payload)
	signingInput := jwtHeader + "." + encodedPayload

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	signature := base64URLEncode(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

// ValidateJWT parses and verifies a JWT string signed with key. It
// returns the decoded claims if the signature is valid and the token
// has not expired. Returns ErrInvalidToken for malformed or tampered
// tokens and ErrTokenExpired for expired tokens.
func ValidateJWT(key []byte, token string) (Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]

	sig, err := base64URLDecode(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return Claims{}, ErrInvalidToken
	}

	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}

	if !claims.Valid() {
		return claims, ErrTokenExpired
	}

	return claims, nil
}

// base64URLEncode encodes src using unpadded base64url encoding.
func base64URLEncode(src []byte) string {
	return base64.RawURLEncoding.EncodeToString(src)
}

// base64URLDecode decodes an unpadded base64url string.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// mustBase64JSON marshals v to JSON and returns the base64url encoding.
// It panics on marshal failure, so it must only be used with static
// data at package init time.
func mustBase64JSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic("auth: " + err.Error())
	}
	return base64URLEncode(data)
}
