// Package email provides a simple email-sending client backed by the Resend
// HTTP API. It is built entirely on Go's standard library with zero external
// dependencies.
//
// Basic usage:
//
//	client := email.New("re_your_api_key",
//		email.WithFrom("noreply@example.com"),
//	)
//	err := client.Send(ctx, email.Message{
//		To:      []string{"user@example.com"},
//		Subject: "Welcome",
//		HTML:    "<h1>Hello!</h1>",
//	})
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// Client sends emails via the Resend API.
type Client struct {
	apiKey     string
	authHeader string
	from       string
	endpoint   string
	httpClient *http.Client
}

// Message represents an email to be sent.
type Message struct {
	// To is the list of recipient email addresses. Required.
	To []string `json:"to"`

	// Subject is the email subject line. Required.
	Subject string `json:"subject"`

	// HTML is the HTML body of the email. At least one of HTML or Text is required.
	HTML string `json:"html,omitempty"`

	// Text is the plain-text body of the email.
	Text string `json:"text,omitempty"`

	// From overrides the client-level default sender for this message.
	From string `json:"from,omitempty"`

	// ReplyTo sets the Reply-To header.
	ReplyTo []string `json:"reply_to,omitempty"`
}

// SendResult contains the response from a successful send.
type SendResult struct {
	// ID is the Resend message ID.
	ID string `json:"id"`
}

type config struct {
	from     string
	endpoint string
	timeout  time.Duration
}

// Option configures the email Client.
type Option func(*config)

// WithFrom sets the default sender address for all messages. Individual
// messages can override this with Message.From.
func WithFrom(from string) Option {
	return func(c *config) {
		c.from = from
	}
}

// WithEndpoint overrides the Resend API endpoint. Useful for testing.
func WithEndpoint(endpoint string) Option {
	return func(c *config) {
		c.endpoint = endpoint
	}
}

// WithTimeout sets the HTTP request timeout. Defaults to 10 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// New creates a new email Client with the given Resend API key.
func New(apiKey string, opts ...Option) *Client {
	cfg := config{
		endpoint: resendEndpoint,
		timeout:  10 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Client{
		apiKey:     apiKey,
		authHeader: "Bearer " + apiKey,
		from:       cfg.from,
		endpoint:   cfg.endpoint,
		httpClient: &http.Client{Timeout: cfg.timeout},
	}
}

// Errors returned by Send.
var (
	ErrNoRecipient = errors.New("email: at least one recipient is required")
	ErrNoSubject   = errors.New("email: subject is required")
	ErrNoBody      = errors.New("email: at least one of HTML or Text body is required")
	ErrNoFrom      = errors.New("email: sender address is required (set via WithFrom or Message.From)")
	ErrNoAPIKey    = errors.New("email: API key is required")
)

// Send sends an email message via the Resend API. It blocks until the API
// responds or the context is cancelled.
func (c *Client) Send(ctx context.Context, msg Message) (SendResult, error) {
	if c.apiKey == "" {
		return SendResult{}, ErrNoAPIKey
	}
	if len(msg.To) == 0 {
		return SendResult{}, ErrNoRecipient
	}
	if msg.Subject == "" {
		return SendResult{}, ErrNoSubject
	}
	if msg.HTML == "" && msg.Text == "" {
		return SendResult{}, ErrNoBody
	}

	from := msg.From
	if from == "" {
		from = c.from
	}
	if from == "" {
		return SendResult{}, ErrNoFrom
	}

	payload := resendPayload{
		From:    from,
		To:      msg.To,
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    msg.Text,
		ReplyTo: msg.ReplyTo,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return SendResult{}, fmt.Errorf("email: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("email: create request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("email: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return SendResult{}, fmt.Errorf("email: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SendResult{}, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var result SendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return SendResult{}, fmt.Errorf("email: decode response: %w", err)
	}

	return result, nil
}

// Configured reports whether the client has been configured with an API key.
// Callers can check this to skip email-sending when no key is set (e.g. local
// development).
func (c *Client) Configured() bool {
	return c.apiKey != ""
}

// resendPayload is the JSON body sent to the Resend API.
type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
	ReplyTo []string `json:"reply_to,omitempty"`
}

// APIError is returned when the Resend API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("email: resend API error (status %d): %s", e.StatusCode, e.Body)
}
