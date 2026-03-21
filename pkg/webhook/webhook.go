// Package webhook provides an HTTP client for delivering outgoing webhook
// events with HMAC-SHA256 signatures and configurable retry logic.
//
// The signature scheme follows industry conventions (Stripe, Svix): the
// recipient can verify authenticity by recomputing HMAC-SHA256 over the
// concatenation of the delivery ID, timestamp, and raw body.
//
// Basic usage:
//
//	client := webhook.NewClient()
//	result, err := client.Send(ctx, &webhook.Delivery{
//		URL:     "https://example.com/webhook",
//		Secret:  "whsec_abc123",
//		Event:   "user.created",
//		Payload: jsonBytes,
//	})
//
// With retry:
//
//	result, err := client.SendWithRetry(ctx, &webhook.Delivery{
//		URL:     "https://example.com/webhook",
//		Secret:  "whsec_abc123",
//		Event:   "user.created",
//		Payload: jsonBytes,
//	})
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Header names used for webhook signatures.
const (
	HeaderID        = "X-Webhook-ID"
	HeaderTimestamp = "X-Webhook-Timestamp"
	HeaderSignature = "X-Webhook-Signature"
	HeaderEvent     = "X-Webhook-Event"
)

// Client delivers webhook events to endpoints.
type Client struct {
	timeout        time.Duration
	maxRetries     int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
	now            func() time.Time
}

// Delivery represents a single webhook delivery request.
type Delivery struct {
	// URL is the endpoint to deliver to. Required.
	URL string

	// Secret is the HMAC-SHA256 signing key. If empty, no signature headers
	// are added.
	Secret string

	// Event is the event type (e.g. "user.created"). Sent in the
	// X-Webhook-Event header.
	Event string

	// Payload is the raw JSON body to deliver.
	Payload []byte

	// Headers are additional headers to include in the request. These are
	// added after the standard webhook headers, so they can override them
	// if needed.
	Headers map[string]string
}

// Result contains the response from a delivery attempt.
type Result struct {
	// StatusCode is the HTTP status code returned by the endpoint.
	StatusCode int

	// Body is the response body (truncated to 64KB).
	Body string

	// Attempts is the total number of delivery attempts made.
	Attempts int

	// DeliveryID is the unique ID assigned to this delivery.
	DeliveryID string
}

type config struct {
	timeout        time.Duration
	maxRetries     int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
}

// Option configures the webhook Client.
type Option func(*config)

// WithTimeout sets the per-request HTTP timeout. Defaults to 10 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithMaxRetries sets the maximum number of retry attempts for SendWithRetry.
// Defaults to 3 (meaning up to 4 total attempts including the initial send).
func WithMaxRetries(n int) Option {
	return func(c *config) {
		c.maxRetries = n
	}
}

// WithRetryBaseDelay sets the base delay for exponential backoff. Each
// subsequent retry doubles the delay. Defaults to 1 second.
func WithRetryBaseDelay(d time.Duration) Option {
	return func(c *config) {
		c.retryBaseDelay = d
	}
}

// WithRetryMaxDelay sets the maximum delay between retries. Defaults to
// 30 seconds.
func WithRetryMaxDelay(d time.Duration) Option {
	return func(c *config) {
		c.retryMaxDelay = d
	}
}

// NewClient creates a new webhook Client with the given options.
func NewClient(opts ...Option) *Client {
	cfg := config{
		timeout:        10 * time.Second,
		maxRetries:     3,
		retryBaseDelay: 1 * time.Second,
		retryMaxDelay:  30 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Client{
		timeout:        cfg.timeout,
		maxRetries:     cfg.maxRetries,
		retryBaseDelay: cfg.retryBaseDelay,
		retryMaxDelay:  cfg.retryMaxDelay,
		now:            time.Now,
	}
}

// ErrNoURL is returned when a delivery has no URL.
var ErrNoURL = fmt.Errorf("webhook: URL is required")

// Send delivers a webhook event to the endpoint. It makes a single attempt
// and returns the result. A non-2xx response is not treated as an error — the
// caller should inspect Result.StatusCode.
func (c *Client) Send(ctx context.Context, d *Delivery) (*Result, error) {
	if d.URL == "" {
		return nil, ErrNoURL
	}

	deliveryID := generateID()
	timestamp := strconv.FormatInt(c.now().Unix(), 10)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Payload))
	if err != nil {
		return nil, fmt.Errorf("webhook: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderID, deliveryID)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderEvent, d.Event)

	if d.Secret != "" {
		sig := Sign(d.Secret, deliveryID, timestamp, d.Payload)
		req.Header.Set(HeaderSignature, sig)
	}

	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: c.timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("webhook: read response: %w", err)
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		Attempts:   1,
		DeliveryID: deliveryID,
	}, nil
}

// SendWithRetry delivers a webhook event with exponential backoff retry.
// It retries on network errors and 5xx responses. 2xx responses succeed
// immediately. 4xx responses (client errors) are not retried.
func (c *Client) SendWithRetry(ctx context.Context, d *Delivery) (*Result, error) {
	if d.URL == "" {
		return nil, ErrNoURL
	}

	deliveryID := generateID()
	timestamp := strconv.FormatInt(c.now().Unix(), 10)

	var lastResult *Result
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryBaseDelay * (1 << (attempt - 1))
			if delay > c.retryMaxDelay {
				delay = c.retryMaxDelay
			}
			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Payload))
		if err != nil {
			return nil, fmt.Errorf("webhook: create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderID, deliveryID)
		req.Header.Set(HeaderTimestamp, timestamp)
		req.Header.Set(HeaderEvent, d.Event)

		if d.Secret != "" {
			sig := Sign(d.Secret, deliveryID, timestamp, d.Payload)
			req.Header.Set(HeaderSignature, sig)
		}

		for k, v := range d.Headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: c.timeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("webhook: send request (attempt %d): %w", attempt+1, err)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("webhook: read response (attempt %d): %w", attempt+1, readErr)
			continue
		}

		lastResult = &Result{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Attempts:   attempt + 1,
			DeliveryID: deliveryID,
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return lastResult, nil
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return lastResult, nil
		}

		lastErr = fmt.Errorf("webhook: server error (attempt %d): status %d", attempt+1, resp.StatusCode)
	}

	if lastResult != nil {
		return lastResult, lastErr
	}
	return nil, lastErr
}

// Sign computes the HMAC-SHA256 signature for a webhook delivery. The signed
// content is "{id}.{timestamp}.{body}". The returned value is the hex-encoded
// HMAC digest.
func Sign(secret, id, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte("."))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks whether a webhook delivery signature is valid. It recomputes
// the HMAC-SHA256 and compares using constant-time comparison.
func Verify(secret, id, timestamp, signature string, body []byte) bool {
	expected := Sign(secret, id, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// generateID creates a random delivery ID in the format "whd_<hex>".
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "whd_" + hex.EncodeToString(b)
}
