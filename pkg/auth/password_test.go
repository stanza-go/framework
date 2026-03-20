package auth

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	parts := strings.SplitN(hash, "$", 4)
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d: %s", len(parts), hash)
	}
	if parts[0] != "pbkdf2" {
		t.Errorf("expected prefix pbkdf2, got %s", parts[0])
	}
	if parts[1] != "100000" {
		t.Errorf("expected 100000 iterations, got %s", parts[1])
	}
	if len(parts[2]) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("expected 32-char salt hex, got %d chars", len(parts[2]))
	}
	if len(parts[3]) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected 64-char hash hex, got %d chars", len(parts[3]))
	}
}

func TestHashPasswordUniqueSalts(t *testing.T) {
	h1, _ := HashPassword("same-password")
	h2, _ := HashPassword("same-password")
	if h1 == h2 {
		t.Error("two hashes of the same password should differ (unique salts)")
	}
}

func TestVerifyPasswordCorrect(t *testing.T) {
	hash, err := HashPassword("my-secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(hash, "my-secret") {
		t.Error("VerifyPassword should return true for correct password")
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	hash, err := HashPassword("my-secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Error("VerifyPassword should return false for wrong password")
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"no delimiters", "pbkdf2"},
		{"wrong prefix", "bcrypt$100000$aabb$ccdd"},
		{"bad iterations", "pbkdf2$abc$aabb$ccdd"},
		{"zero iterations", "pbkdf2$0$aabb$ccdd"},
		{"bad salt hex", "pbkdf2$100000$zzzz$ccdd"},
		{"bad hash hex", "pbkdf2$100000$aabb$zzzz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if VerifyPassword(tt.hash, "anything") {
				t.Error("VerifyPassword should return false for malformed hash")
			}
		})
	}
}

func TestVerifyPasswordKnownVector(t *testing.T) {
	// Hash a known password, then verify it matches only that password.
	hash, _ := HashPassword("test123")
	if !VerifyPassword(hash, "test123") {
		t.Error("should verify correct password")
	}
	if VerifyPassword(hash, "test124") {
		t.Error("should reject wrong password")
	}
	if VerifyPassword(hash, "Test123") {
		t.Error("should be case-sensitive")
	}
	if VerifyPassword(hash, "") {
		t.Error("should reject empty password")
	}
}
