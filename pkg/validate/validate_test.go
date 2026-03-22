package validate

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRequired(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"non-empty", "hello", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"tabs", "\t\n", true},
		{"with leading space", "  hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := Required("field", tt.value)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestMinLen(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		min     int
		wantErr bool
	}{
		{"empty skipped", "", 8, false},
		{"exactly min", "12345678", 8, false},
		{"above min", "123456789", 8, false},
		{"below min", "1234567", 8, true},
		{"single char min", "a", 2, true},
		{"min 1 passes", "a", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := MinLen("password", tt.value, tt.min)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestMaxLen(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		max     int
		wantErr bool
	}{
		{"empty", "", 10, false},
		{"exactly max", "1234567890", 10, false},
		{"below max", "12345", 10, false},
		{"above max", "12345678901", 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := MaxLen("name", tt.value, tt.max)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestEmail(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty skipped", "", false},
		{"valid", "user@example.com", false},
		{"valid subdomain", "user@sub.example.com", false},
		{"no @", "userexample.com", true},
		{"@ at start", "@example.com", true},
		{"no domain", "user@", true},
		{"no dot in domain", "user@localhost", true},
		{"dot at end", "user@example.", true},
		{"plus addressing", "user+tag@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := Email("email", tt.value)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestOneOf(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		allowed []string
		wantErr bool
	}{
		{"empty skipped", "", []string{"a", "b"}, false},
		{"valid", "admin", []string{"admin", "viewer", "superadmin"}, false},
		{"invalid", "editor", []string{"admin", "viewer", "superadmin"}, true},
		{"case sensitive", "Admin", []string{"admin", "viewer"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := OneOf("role", tt.value, tt.allowed...)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestPositive(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"positive", 1, false},
		{"large", 1000000, false},
		{"zero", 0, true},
		{"negative", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := Positive("limit", tt.value)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestInRange(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		min     int
		max     int
		wantErr bool
	}{
		{"in range", 5, 1, 10, false},
		{"at min", 1, 1, 10, false},
		{"at max", 10, 1, 10, false},
		{"below min", 0, 1, 10, true},
		{"above max", 11, 1, 10, true},
		{"negative range", -5, -10, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := InRange("page", tt.value, tt.min, tt.max)
			if tt.wantErr && fe == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && fe != nil {
				t.Errorf("expected nil, got %q", fe.Message)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	fe := Check("expires_at", true, "must be in the future")
	if fe != nil {
		t.Errorf("expected nil, got %q", fe.Message)
	}

	fe = Check("expires_at", false, "must be in the future")
	if fe == nil {
		t.Fatal("expected error, got nil")
	}
	if fe.Message != "must be in the future" {
		t.Errorf("got message %q, want %q", fe.Message, "must be in the future")
	}
}

func TestFields_NoErrors(t *testing.T) {
	v := Fields(
		Required("email", "user@example.com"),
		Required("password", "secret123"),
		MinLen("password", "secret123", 8),
	)
	if v.HasErrors() {
		t.Errorf("expected no errors, got %v", v.Errors())
	}
}

func TestFields_MultipleErrors(t *testing.T) {
	v := Fields(
		Required("email", ""),
		Required("password", ""),
		MinLen("password", "", 8),
	)
	if !v.HasErrors() {
		t.Error("expected errors")
	}
	errs := v.Errors()
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if errs["email"] != "is required" {
		t.Errorf("email error = %q, want %q", errs["email"], "is required")
	}
	if errs["password"] != "is required" {
		t.Errorf("password error = %q, want %q", errs["password"], "is required")
	}
}

func TestFields_FirstErrorPerField(t *testing.T) {
	v := Fields(
		Required("password", ""),
		MinLen("password", "", 8),
	)
	errs := v.Errors()
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errs), errs)
	}
	// Required fires first; MinLen skips because field already has error.
	if errs["password"] != "is required" {
		t.Errorf("password error = %q, want %q", errs["password"], "is required")
	}
}

func TestFields_MinLenAfterRequired(t *testing.T) {
	// When value is present but too short, MinLen fires.
	v := Fields(
		Required("password", "short"),
		MinLen("password", "short", 8),
	)
	errs := v.Errors()
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs["password"] != "must be at least 8 characters" {
		t.Errorf("password error = %q", errs["password"])
	}
}

func TestFields_AllNil(t *testing.T) {
	v := Fields(nil, nil, nil)
	if v.HasErrors() {
		t.Error("expected no errors from all-nil checks")
	}
}

func TestWriteError(t *testing.T) {
	v := Fields(
		Required("email", ""),
		OneOf("role", "editor", "admin", "viewer"),
	)

	rec := httptest.NewRecorder()
	v.WriteError(rec)

	if rec.Code != 422 {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != "validation failed" {
		t.Errorf("error = %q, want %q", resp.Error, "validation failed")
	}
	if resp.Fields["email"] != "is required" {
		t.Errorf("email field = %q", resp.Fields["email"])
	}
	if resp.Fields["role"] != "must be one of: admin, viewer" {
		t.Errorf("role field = %q", resp.Fields["role"])
	}
}

func TestWriteError_NoErrors(t *testing.T) {
	v := Fields(Required("name", "Alice"))
	rec := httptest.NewRecorder()
	v.WriteError(rec)

	// Even if there are no errors, WriteError writes the response.
	// Callers should check HasErrors() first. This tests that it
	// doesn't panic.
	if rec.Code != 422 {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-7, "-7"},
		{1000000, "1000000"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
