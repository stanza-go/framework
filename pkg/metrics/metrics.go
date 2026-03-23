package metrics

import (
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// AggFn specifies an aggregation function for queries.
type AggFn int

const (
	// Sum aggregates by summing values in each step.
	Sum AggFn = iota
	// Avg aggregates by averaging values in each step.
	Avg
	// Min aggregates by taking the minimum value in each step.
	Min
	// Max aggregates by taking the maximum value in each step.
	Max
	// Count aggregates by counting samples in each step.
	Count
	// Last aggregates by taking the last value in each step.
	Last
)

// String returns the string representation of an AggFn.
func (f AggFn) String() string {
	switch f {
	case Sum:
		return "sum"
	case Avg:
		return "avg"
	case Min:
		return "min"
	case Max:
		return "max"
	case Count:
		return "count"
	case Last:
		return "last"
	default:
		return "unknown"
	}
}

// Point is a single data point with a timestamp and value.
type Point struct {
	T int64   // Unix milliseconds.
	V float64 // Metric value.
}

// SeriesData holds the result data for a single time series.
type SeriesData struct {
	Name   string
	Labels map[string]string
	Points []Point
}

// Result holds the result of a metrics query.
type Result struct {
	Series []SeriesData
}

// Query specifies parameters for a metrics query.
type Query struct {
	Name   string            // Metric name (required).
	Labels map[string]string // Label filters, exact match (optional).
	Start  time.Time         // Range start (required).
	End    time.Time         // Range end (required).
	Step   time.Duration     // Aggregation interval. 0 returns raw samples.
	Fn     AggFn             // Aggregation function. Default: Avg.
}

// StoreStats holds storage statistics.
type StoreStats struct {
	SeriesCount    int    // Number of unique time series.
	PartitionCount int    // Number of daily partitions on disk.
	DiskBytes      int64  // Total bytes used by metrics data.
	OldestDate     string // Oldest partition date (YYYY-MM-DD).
	NewestDate     string // Newest partition date (YYYY-MM-DD).
}

// sample is an internal representation of a single metric sample.
type sample struct {
	ts       int64  // Unix milliseconds.
	seriesID uint64 // Series identifier.
	value    float64
}

// Option configures a Store.
type Option func(*storeConfig)

type storeConfig struct {
	retention     time.Duration
	flushSize     int
	flushInterval time.Duration
	systemMetrics bool
	logger        *log.Logger
}

// WithRetention sets the data retention period. Partitions older than this
// are automatically pruned. Default: 30 days.
func WithRetention(d time.Duration) Option {
	return func(c *storeConfig) { c.retention = d }
}

// WithFlushInterval sets how often the in-memory buffer is flushed to disk.
// Default: 5 seconds.
func WithFlushInterval(d time.Duration) Option {
	return func(c *storeConfig) { c.flushInterval = d }
}

// WithFlushSize sets the buffer size that triggers an immediate flush.
// Default: 1024 samples.
func WithFlushSize(n int) Option {
	return func(c *storeConfig) { c.flushSize = n }
}

// WithSystemMetrics enables automatic collection of Go runtime metrics
// (goroutines, heap, GC) every 10 seconds.
func WithSystemMetrics() Option {
	return func(c *storeConfig) { c.systemMetrics = true }
}

// WithLogger sets the logger for the metrics store.
func WithLogger(l *log.Logger) Option {
	return func(c *storeConfig) { c.logger = l }
}
