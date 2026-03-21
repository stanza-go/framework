package http

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures the RateLimit middleware.
type RateLimitConfig struct {
	// Limit is the maximum number of requests allowed per window.
	// Default: 60.
	Limit int

	// Window is the time window for counting requests.
	// Default: 1 minute.
	Window time.Duration

	// KeyFunc extracts the rate limit key from a request. Requests with
	// the same key share a rate limit counter. Default: client IP via
	// X-Forwarded-For, X-Real-IP, or RemoteAddr.
	KeyFunc func(*Request) string

	// Message is the error message returned when the limit is exceeded.
	// Default: "rate limit exceeded".
	Message string
}

// RateLimit returns middleware that limits the rate of requests per key
// (default: client IP address). When the limit is exceeded, it responds
// with 429 Too Many Requests and a Retry-After header indicating when
// the client can retry.
//
// Each unique key gets a fixed time window starting from its first
// request. The window resets after the configured duration. Expired
// entries are automatically cleaned up to prevent memory growth.
//
// Rate limit headers are included in every response:
//   - X-RateLimit-Limit: the configured limit
//   - X-RateLimit-Remaining: requests remaining in the current window
//   - X-RateLimit-Reset: Unix timestamp when the current window expires
//
// Protect auth endpoints with 10 requests per minute:
//
//	authGroup.Use(http.RateLimit(http.RateLimitConfig{
//	    Limit:  10,
//	    Window: time.Minute,
//	}))
//
// Apply a global rate limit with a custom key:
//
//	r.Use(http.RateLimit(http.RateLimitConfig{
//	    Limit:   100,
//	    Window:  time.Minute,
//	    KeyFunc: func(r *http.Request) string { return r.Header.Get("X-API-Key") },
//	}))
func RateLimit(cfg RateLimitConfig) Middleware {
	if cfg.Limit <= 0 {
		cfg.Limit = 60
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = ClientIP
	}
	if cfg.Message == "" {
		cfg.Message = "rate limit exceeded"
	}

	rl := &rateLimiter{
		limit:     cfg.Limit,
		window:    cfg.Window,
		entries:   make(map[string]*rateLimitEntry),
		lastSweep: time.Now(),
	}

	limitStr := strconv.Itoa(cfg.Limit)

	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			key := cfg.KeyFunc(r)
			remaining, resetAt, allowed := rl.check(key)

			h := w.Header()
			h.Set("X-RateLimit-Limit", limitStr)
			h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			h.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

			if !allowed {
				retryAfter := resetAt - time.Now().Unix()
				if retryAfter < 1 {
					retryAfter = 1
				}
				h.Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				WriteError(w, StatusTooManyRequests, cfg.Message)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extracts the client's IP address from the request. It checks
// X-Forwarded-For and X-Real-IP headers (common behind reverse proxies
// and load balancers like Railway, Cloud Run, Nginx) before falling back
// to RemoteAddr.
func ClientIP(r *Request) string {
	// X-Forwarded-For may contain "client, proxy1, proxy2".
	// The first entry is the original client.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr is "IP:port"; strip the port.
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i != -1 {
		return addr[:i]
	}
	return addr
}

// rateLimiter tracks request counts per key using fixed time windows.
// It is safe for concurrent use.
type rateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	entries   map[string]*rateLimitEntry
	lastSweep time.Time
}

// rateLimitEntry tracks the request count within a fixed time window.
type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

// check records a request for the given key and returns the remaining
// request count, the Unix timestamp when the window resets, and whether
// the request is allowed.
func (rl *rateLimiter) check(key string) (remaining int, resetAt int64, allowed bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Sweep expired entries periodically to prevent memory growth.
	// Runs at most once every two window durations.
	if now.Sub(rl.lastSweep) >= rl.window*2 {
		for k, e := range rl.entries {
			if now.Sub(e.windowStart) >= rl.window {
				delete(rl.entries, k)
			}
		}
		rl.lastSweep = now
	}

	entry, ok := rl.entries[key]
	if !ok || now.Sub(entry.windowStart) >= rl.window {
		// New window — first request.
		rl.entries[key] = &rateLimitEntry{
			count:       1,
			windowStart: now,
		}
		return rl.limit - 1, now.Add(rl.window).Unix(), true
	}

	resetAt = entry.windowStart.Add(rl.window).Unix()

	if entry.count >= rl.limit {
		return 0, resetAt, false
	}

	entry.count++
	return rl.limit - entry.count, resetAt, true
}
