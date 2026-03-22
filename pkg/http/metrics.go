package http

import (
	"sync/atomic"
	"time"
)

// Metrics tracks HTTP request statistics using atomic counters. Create
// a Metrics value with NewMetrics, add its middleware to the router,
// and call Stats to read a snapshot:
//
//	m := http.NewMetrics()
//	router.Use(m.Middleware())
//
//	// In a handler:
//	stats := m.Stats()
type Metrics struct {
	totalRequests  atomic.Int64
	activeRequests atomic.Int64
	status2xx      atomic.Int64
	status3xx      atomic.Int64
	status4xx      atomic.Int64
	status5xx      atomic.Int64
	bytesWritten   atomic.Int64
	totalDuration  atomic.Int64 // nanoseconds
}

// MetricsStats is a point-in-time snapshot of HTTP request metrics.
type MetricsStats struct {
	TotalRequests  int64   `json:"total_requests"`
	ActiveRequests int64   `json:"active_requests"`
	Status2xx      int64   `json:"status_2xx"`
	Status3xx      int64   `json:"status_3xx"`
	Status4xx      int64   `json:"status_4xx"`
	Status5xx      int64   `json:"status_5xx"`
	BytesWritten   int64   `json:"bytes_written"`
	AvgDurationMs  float64 `json:"avg_duration_ms"`
}

// NewMetrics creates a new Metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// Middleware returns a Middleware that records request metrics. Add it
// early in the chain so it captures the full request lifecycle:
//
//	router.Use(http.RequestID(cfg))
//	router.Use(metrics.Middleware())
//	router.Use(http.RequestLogger(logger))
func (m *Metrics) Middleware() Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			m.activeRequests.Add(1)
			start := time.Now()

			rec := &responseRecorder{
				ResponseWriter: w,
				status:         StatusOK,
			}

			next.ServeHTTP(rec, r)

			m.activeRequests.Add(-1)
			m.totalRequests.Add(1)
			m.bytesWritten.Add(rec.written)
			m.totalDuration.Add(int64(time.Since(start)))

			switch {
			case rec.status >= 500:
				m.status5xx.Add(1)
			case rec.status >= 400:
				m.status4xx.Add(1)
			case rec.status >= 300:
				m.status3xx.Add(1)
			default:
				m.status2xx.Add(1)
			}
		})
	}
}

// Stats returns a snapshot of current request metrics. All counters
// are read atomically. AvgDurationMs is the mean request duration in
// milliseconds across all completed requests.
func (m *Metrics) Stats() MetricsStats {
	total := m.totalRequests.Load()
	var avgMs float64
	if total > 0 {
		avgMs = float64(m.totalDuration.Load()) / float64(total) / 1e6
	}
	return MetricsStats{
		TotalRequests:  total,
		ActiveRequests: m.activeRequests.Load(),
		Status2xx:      m.status2xx.Load(),
		Status3xx:      m.status3xx.Load(),
		Status4xx:      m.status4xx.Load(),
		Status5xx:      m.status5xx.Load(),
		BytesWritten:   m.bytesWritten.Load(),
		AvgDurationMs:  avgMs,
	}
}
