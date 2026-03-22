package email

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New("re_test_key")
	if c.apiKey != "re_test_key" {
		t.Fatalf("expected apiKey re_test_key, got %s", c.apiKey)
	}
	if c.endpoint != resendEndpoint {
		t.Fatalf("expected default endpoint, got %s", c.endpoint)
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestNewWithOptions(t *testing.T) {
	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint("http://localhost:9999"),
		WithTimeout(5*time.Second),
	)
	if c.from != "noreply@example.com" {
		t.Fatalf("expected from noreply@example.com, got %s", c.from)
	}
	if c.endpoint != "http://localhost:9999" {
		t.Fatalf("expected custom endpoint, got %s", c.endpoint)
	}
	if c.httpClient.Timeout != 5*time.Second {
		t.Fatalf("expected 5s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestConfigured(t *testing.T) {
	c := New("")
	if c.Configured() {
		t.Fatal("expected Configured() false for empty key")
	}
	c = New("re_test")
	if !c.Configured() {
		t.Fatal("expected Configured() true for non-empty key")
	}
}

func TestSendValidation(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		from    string
		msg     Message
		wantErr error
	}{
		{
			name:    "no api key",
			apiKey:  "",
			msg:     Message{To: []string{"a@b.com"}, Subject: "hi", HTML: "<p>hi</p>"},
			wantErr: ErrNoAPIKey,
		},
		{
			name:    "no recipient",
			apiKey:  "re_test",
			msg:     Message{Subject: "hi", HTML: "<p>hi</p>"},
			wantErr: ErrNoRecipient,
		},
		{
			name:    "no subject",
			apiKey:  "re_test",
			msg:     Message{To: []string{"a@b.com"}, HTML: "<p>hi</p>"},
			wantErr: ErrNoSubject,
		},
		{
			name:    "no body",
			apiKey:  "re_test",
			msg:     Message{To: []string{"a@b.com"}, Subject: "hi"},
			wantErr: ErrNoBody,
		},
		{
			name:    "no from",
			apiKey:  "re_test",
			msg:     Message{To: []string{"a@b.com"}, Subject: "hi", HTML: "<p>hi</p>"},
			wantErr: ErrNoFrom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.apiKey, WithFrom(tt.from))
			_, err := c.Send(context.Background(), tt.msg)
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSendSuccess(t *testing.T) {
	var gotPayload resendPayload
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	result, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com", "user2@example.com"},
		Subject: "Test Subject",
		HTML:    "<h1>Hello</h1>",
		Text:    "Hello",
		ReplyTo: []string{"support@example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "msg_123" {
		t.Fatalf("expected ID msg_123, got %s", result.ID)
	}
	if gotAuth != "Bearer re_test_key" {
		t.Fatalf("expected Bearer auth, got %s", gotAuth)
	}
	if gotPayload.From != "noreply@example.com" {
		t.Fatalf("expected from noreply@example.com, got %s", gotPayload.From)
	}
	if len(gotPayload.To) != 2 || gotPayload.To[0] != "user@example.com" {
		t.Fatalf("unexpected To: %v", gotPayload.To)
	}
	if gotPayload.Subject != "Test Subject" {
		t.Fatalf("unexpected Subject: %s", gotPayload.Subject)
	}
	if gotPayload.HTML != "<h1>Hello</h1>" {
		t.Fatalf("unexpected HTML: %s", gotPayload.HTML)
	}
	if gotPayload.Text != "Hello" {
		t.Fatalf("unexpected Text: %s", gotPayload.Text)
	}
	if len(gotPayload.ReplyTo) != 1 || gotPayload.ReplyTo[0] != "support@example.com" {
		t.Fatalf("unexpected ReplyTo: %v", gotPayload.ReplyTo)
	}
}

func TestSendMessageFromOverride(t *testing.T) {
	var gotPayload resendPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_456"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("default@example.com"),
		WithEndpoint(srv.URL),
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Override From",
		HTML:    "<p>hi</p>",
		From:    "override@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPayload.From != "override@example.com" {
		t.Fatalf("expected from override, got %s", gotPayload.From)
	}
}

func TestSendAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid email address"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"bad"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", apiErr.StatusCode)
	}
	if apiErr.Body == "" {
		t.Fatal("expected non-empty error body")
	}
}

func TestSendContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"id":"msg_late"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
		WithTimeout(5*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Send(ctx, Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSendTextOnly(t *testing.T) {
	var gotPayload resendPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_text"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	result, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Plain text",
		Text:    "Hello plain",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "msg_text" {
		t.Fatalf("expected ID msg_text, got %s", result.ID)
	}
	if gotPayload.HTML != "" {
		t.Fatalf("expected empty HTML, got %s", gotPayload.HTML)
	}
	if gotPayload.Text != "Hello plain" {
		t.Fatalf("expected text body, got %s", gotPayload.Text)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	e := &APIError{StatusCode: 400, Body: "bad request"}
	want := "email: resend API error (status 400): bad request"
	if e.Error() != want {
		t.Fatalf("expected %q, got %q", want, e.Error())
	}
}

func TestSendInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return invalid JSON — status 200 but body is not valid JSON.
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	// Should be a decode error, not an API error.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatal("should not be APIError — it was a 200 response")
	}
}

func TestSendServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal server error"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Fatalf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestSendRateLimited(t *testing.T) {
	var callCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 429 {
		t.Fatalf("expected status 429, got %d", apiErr.StatusCode)
	}
}

func TestSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"id":"msg_late"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
		WithTimeout(50*time.Millisecond), // very short timeout
	)

	_, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}

func TestSendEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return empty body — valid JSON unmarshal to zero value.
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	result, err := c.Send(context.Background(), Message{
		To:      []string{"user@example.com"},
		Subject: "Test",
		HTML:    "<p>hi</p>",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "" {
		t.Fatalf("expected empty ID, got %s", result.ID)
	}
}

func TestSendConcurrent(t *testing.T) {
	var count atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_concurrent"}`))
	}))
	defer srv.Close()

	c := New("re_test_key",
		WithFrom("noreply@example.com"),
		WithEndpoint(srv.URL),
	)

	done := make(chan struct{})
	for i := range 10 {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, err := c.Send(context.Background(), Message{
				To:      []string{"user@example.com"},
				Subject: "Concurrent " + string(rune('0'+i)),
				HTML:    "<p>hi</p>",
			})
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
			}
		}(i)
	}

	for range 10 {
		<-done
	}

	if count.Load() != 10 {
		t.Fatalf("expected 10 requests, got %d", count.Load())
	}
}
