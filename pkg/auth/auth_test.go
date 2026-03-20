package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testKey is a 32-byte key for testing.
var testKey = []byte("test-signing-key-that-is-32bytes!")

// --- JWT Tests ---

func TestCreateJWT(t *testing.T) {
	t.Parallel()

	claims := Claims{
		UID:       "user-123",
		Scopes:    []string{"admin:read", "admin:write"},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := CreateJWT(testKey, claims)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	// Decode header
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header["alg"] != "HS256" {
		t.Errorf("header alg = %q, want %q", header["alg"], "HS256")
	}
	if header["typ"] != "JWT" {
		t.Errorf("header typ = %q, want %q", header["typ"], "JWT")
	}

	// Decode payload
	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var decoded Claims
	if err := json.Unmarshal(payloadJSON, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.UID != "user-123" {
		t.Errorf("uid = %q, want %q", decoded.UID, "user-123")
	}
	if len(decoded.Scopes) != 2 {
		t.Errorf("scopes len = %d, want 2", len(decoded.Scopes))
	}
}

func TestCreateJWT_ShortKey(t *testing.T) {
	t.Parallel()

	_, err := CreateJWT([]byte("short"), Claims{})
	if err == nil {
		t.Fatal("expected error for short key")
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Errorf("error = %q, want mention of 32 bytes", err.Error())
	}
}

func TestValidateJWT_Valid(t *testing.T) {
	t.Parallel()

	original := Claims{
		UID:       "user-456",
		Scopes:    []string{"read"},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := CreateJWT(testKey, original)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	claims, err := ValidateJWT(testKey, token)
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}

	if claims.UID != original.UID {
		t.Errorf("uid = %q, want %q", claims.UID, original.UID)
	}
	if len(claims.Scopes) != 1 || claims.Scopes[0] != "read" {
		t.Errorf("scopes = %v, want [read]", claims.Scopes)
	}
}

func TestValidateJWT_Expired(t *testing.T) {
	t.Parallel()

	claims := Claims{
		UID:       "user-789",
		Scopes:    []string{},
		IssuedAt:  time.Now().Add(-10 * time.Minute).Unix(),
		ExpiresAt: time.Now().Add(-5 * time.Minute).Unix(),
	}

	token, err := CreateJWT(testKey, claims)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	_, err = ValidateJWT(testKey, token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestValidateJWT_WrongKey(t *testing.T) {
	t.Parallel()

	claims := Claims{
		UID:       "user-123",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := CreateJWT(testKey, claims)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	wrongKey := []byte("wrong-signing-key-that-is-32byte!")
	_, err = ValidateJWT(wrongKey, token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestValidateJWT_Tampered(t *testing.T) {
	t.Parallel()

	claims := Claims{
		UID:       "user-123",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	token, err := CreateJWT(testKey, claims)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	// Tamper with payload
	parts := strings.Split(token, ".")
	tampered := parts[0] + "." + base64URLEncode([]byte(`{"uid":"admin","iat":0,"exp":9999999999}`)) + "." + parts[2]

	_, err = ValidateJWT(testKey, tampered)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestValidateJWT_Malformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one part", "abc"},
		{"two parts", "abc.def"},
		{"bad base64 sig", "abc.def.!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateJWT(testKey, tt.token)
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

// --- Claims Tests ---

func TestClaims_Valid(t *testing.T) {
	t.Parallel()

	future := Claims{ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if !future.Valid() {
		t.Error("future claims should be valid")
	}

	past := Claims{ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	if past.Valid() {
		t.Error("past claims should be invalid")
	}
}

func TestClaims_HasScope(t *testing.T) {
	t.Parallel()

	c := Claims{Scopes: []string{"admin:read", "admin:write"}}
	if !c.HasScope("admin:read") {
		t.Error("should have admin:read")
	}
	if c.HasScope("admin:delete") {
		t.Error("should not have admin:delete")
	}

	empty := Claims{}
	if empty.HasScope("anything") {
		t.Error("empty scopes should not match")
	}
}

// --- Token Generation Tests ---

func TestGenerateRefreshToken(t *testing.T) {
	t.Parallel()

	token, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 (32 bytes hex)", len(token))
	}

	// Verify uniqueness
	token2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if token == token2 {
		t.Error("two generated tokens should not be equal")
	}
}

func TestHashToken(t *testing.T) {
	t.Parallel()

	token := "abc123"
	hash := HashToken(token)

	expected := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(expected[:])
	if hash != want {
		t.Errorf("hash = %q, want %q", hash, want)
	}

	// Same input → same hash
	if HashToken(token) != hash {
		t.Error("hash should be deterministic")
	}

	// Different input → different hash
	if HashToken("xyz") == hash {
		t.Error("different tokens should hash differently")
	}
}

// --- Auth Type Tests ---

func TestNew_Defaults(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	if a.accessTokenTTL != 5*time.Minute {
		t.Errorf("accessTokenTTL = %v, want 5m", a.accessTokenTTL)
	}
	if a.refreshTokenTTL != 24*time.Hour {
		t.Errorf("refreshTokenTTL = %v, want 24h", a.refreshTokenTTL)
	}
	if a.cookiePath != "/api/admin" {
		t.Errorf("cookiePath = %q, want /api/admin", a.cookiePath)
	}
	if a.authCookiePath != "/api/admin/auth" {
		t.Errorf("authCookiePath = %q, want /api/admin/auth", a.authCookiePath)
	}
	if !a.secureCookies {
		t.Error("secureCookies should default to true")
	}
}

func TestNew_Options(t *testing.T) {
	t.Parallel()

	a := New(testKey,
		WithAccessTokenTTL(10*time.Minute),
		WithRefreshTokenTTL(48*time.Hour),
		WithCookiePath("/api/v2"),
		WithSecureCookies(false),
	)
	if a.accessTokenTTL != 10*time.Minute {
		t.Errorf("accessTokenTTL = %v, want 10m", a.accessTokenTTL)
	}
	if a.refreshTokenTTL != 48*time.Hour {
		t.Errorf("refreshTokenTTL = %v, want 48h", a.refreshTokenTTL)
	}
	if a.cookiePath != "/api/v2" {
		t.Errorf("cookiePath = %q, want /api/v2", a.cookiePath)
	}
	if a.authCookiePath != "/api/v2/auth" {
		t.Errorf("authCookiePath = %q, want /api/v2/auth", a.authCookiePath)
	}
	if a.secureCookies {
		t.Error("secureCookies should be false")
	}
}

func TestIssueAndValidateAccessToken(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	token, err := a.IssueAccessToken("user-1", []string{"read", "write"})
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := a.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UID != "user-1" {
		t.Errorf("uid = %q, want user-1", claims.UID)
	}
	if len(claims.Scopes) != 2 {
		t.Errorf("scopes = %v, want [read write]", claims.Scopes)
	}
}

// --- Cookie Tests ---

func TestSetAndReadAccessToken(t *testing.T) {
	t.Parallel()

	a := New(testKey, WithSecureCookies(false))
	token := "test-access-token"

	rec := httptest.NewRecorder()
	a.SetAccessTokenCookie(rec, token)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != AccessTokenCookie {
		t.Errorf("name = %q, want %q", c.Name, AccessTokenCookie)
	}
	if c.Value != token {
		t.Errorf("value = %q, want %q", c.Value, token)
	}
	if c.Path != "/api/admin" {
		t.Errorf("path = %q, want /api/admin", c.Path)
	}
	if !c.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}

	// Read it back from a request
	req := httptest.NewRequest("GET", "/api/admin/test", nil)
	req.AddCookie(c)
	val, err := ReadAccessToken(req)
	if err != nil {
		t.Fatalf("ReadAccessToken: %v", err)
	}
	if val != token {
		t.Errorf("read value = %q, want %q", val, token)
	}
}

func TestSetAndReadRefreshToken(t *testing.T) {
	t.Parallel()

	a := New(testKey, WithSecureCookies(false))
	token := "test-refresh-token"

	rec := httptest.NewRecorder()
	a.SetRefreshTokenCookie(rec, token)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	c := cookies[0]
	if c.Name != RefreshTokenCookie {
		t.Errorf("name = %q, want %q", c.Name, RefreshTokenCookie)
	}
	if c.Path != "/api/admin/auth" {
		t.Errorf("path = %q, want /api/admin/auth", c.Path)
	}

	req := httptest.NewRequest("GET", "/api/admin/auth", nil)
	req.AddCookie(c)
	val, err := ReadRefreshToken(req)
	if err != nil {
		t.Fatalf("ReadRefreshToken: %v", err)
	}
	if val != token {
		t.Errorf("read value = %q, want %q", val, token)
	}
}

func TestReadAccessToken_Missing(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/", nil)
	_, err := ReadAccessToken(req)
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestReadRefreshToken_Missing(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/", nil)
	_, err := ReadRefreshToken(req)
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestClearAllCookies(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	rec := httptest.NewRecorder()
	a.ClearAllCookies(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cleared cookies, got %d", len(cookies))
	}

	for _, c := range cookies {
		if c.MaxAge != -1 {
			t.Errorf("cookie %q MaxAge = %d, want -1", c.Name, c.MaxAge)
		}
		if c.Value != "" {
			t.Errorf("cookie %q value = %q, want empty", c.Name, c.Value)
		}
	}
}

// --- Middleware Tests ---

func TestRequireAuth_ValidToken(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	token, err := a.IssueAccessToken("user-1", []string{"admin:read"})
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var gotClaims Claims
	var gotOK bool
	handler := a.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims, gotOK = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !gotOK {
		t.Fatal("claims not found in context")
	}
	if gotClaims.UID != "user-1" {
		t.Errorf("uid = %q, want user-1", gotClaims.UID)
	}
}

func TestRequireAuth_NoToken(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	handler := a.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/admin/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "authentication required" {
		t.Errorf("error = %q, want 'authentication required'", body["error"])
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	claims := Claims{
		UID:       "user-1",
		IssuedAt:  time.Now().Add(-10 * time.Minute).Unix(),
		ExpiresAt: time.Now().Add(-5 * time.Minute).Unix(),
	}
	token, _ := CreateJWT(testKey, claims)

	handler := a.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "token expired" {
		t.Errorf("error = %q, want 'token expired'", body["error"])
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	handler := a.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/admin/test", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: "garbage.token.here"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireScope_HasScope(t *testing.T) {
	t.Parallel()

	called := false
	handler := RequireScope("admin:write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	ctx := withClaims(context.Background(), Claims{
		UID:    "user-1",
		Scopes: []string{"admin:read", "admin:write"},
	})
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequireScope_MissingScope(t *testing.T) {
	t.Parallel()

	handler := RequireScope("admin:delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	ctx := withClaims(context.Background(), Claims{
		UID:    "user-1",
		Scopes: []string{"admin:read"},
	})
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestRequireScope_NoClaims(t *testing.T) {
	t.Parallel()

	handler := RequireScope("anything")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// --- API Key Middleware Tests ---

func validKeyValidator(keyHash string) (Claims, error) {
	// Accept any key whose hash matches our test key hash.
	expected := HashToken("stza_testkey123")
	if keyHash == expected {
		return Claims{
			UID:    "apikey:42",
			Scopes: []string{"read", "write"},
		}, nil
	}
	return Claims{}, errors.New("key not found")
}

func TestRequireAPIKey_ValidKey(t *testing.T) {
	t.Parallel()

	var gotClaims Claims
	var gotOK bool
	handler := RequireAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims, gotOK = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer stza_testkey123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !gotOK {
		t.Fatal("claims not found in context")
	}
	if gotClaims.UID != "apikey:42" {
		t.Errorf("uid = %q, want apikey:42", gotClaims.UID)
	}
	if len(gotClaims.Scopes) != 2 {
		t.Errorf("scopes = %v, want [read write]", gotClaims.Scopes)
	}
}

func TestRequireAPIKey_NoHeader(t *testing.T) {
	t.Parallel()

	handler := RequireAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAPIKey_InvalidKey(t *testing.T) {
	t.Parallel()

	handler := RequireAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer stza_wrongkey")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid api key" {
		t.Errorf("error = %q, want 'invalid api key'", body["error"])
	}
}

func TestRequireAPIKey_BadAuthScheme(t *testing.T) {
	t.Parallel()

	handler := RequireAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAPIKey_EmptyBearer(t *testing.T) {
	t.Parallel()

	handler := RequireAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAuthOrAPIKey_JWTFirst(t *testing.T) {
	t.Parallel()

	a := New(testKey)
	token, _ := a.IssueAccessToken("user-1", []string{"admin"})

	var gotClaims Claims
	handler := a.RequireAuthOrAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotClaims.UID != "user-1" {
		t.Errorf("uid = %q, want user-1 (JWT should take precedence)", gotClaims.UID)
	}
}

func TestRequireAuthOrAPIKey_FallbackToAPIKey(t *testing.T) {
	t.Parallel()

	a := New(testKey)

	var gotClaims Claims
	handler := a.RequireAuthOrAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer stza_testkey123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotClaims.UID != "apikey:42" {
		t.Errorf("uid = %q, want apikey:42 (should fall back to API key)", gotClaims.UID)
	}
}

func TestRequireAuthOrAPIKey_BothFail(t *testing.T) {
	t.Parallel()

	a := New(testKey)

	handler := a.RequireAuthOrAPIKey(validKeyValidator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{"valid", "Bearer mytoken", "mytoken", true},
		{"empty header", "", "", false},
		{"no bearer prefix", "Basic abc", "", false},
		{"bearer only", "Bearer ", "", false},
		{"lowercase bearer", "bearer token", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got, ok := bearerToken(req)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Context Tests ---

func TestClaimsFromContext_Empty(t *testing.T) {
	t.Parallel()

	_, ok := ClaimsFromContext(context.Background())
	if ok {
		t.Error("should return false for empty context")
	}
}

func TestClaimsFromContext_Present(t *testing.T) {
	t.Parallel()

	original := Claims{UID: "user-1", Scopes: []string{"read"}}
	ctx := withClaims(context.Background(), original)

	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		t.Fatal("should return true")
	}
	if claims.UID != "user-1" {
		t.Errorf("uid = %q, want user-1", claims.UID)
	}
}
