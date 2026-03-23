// Package metrics provides a column-oriented time-series storage engine for
// application metrics. It replaces external monitoring stacks (Prometheus,
// Grafana, Google Analytics, PostHog) with a built-in solution that runs
// inside the application process.
//
// # Storage Design
//
// Data is organized into daily partition directories, each containing three
// column files:
//
//	metrics/
//	├── series.txt          ← series registry (metric name + labels → ID)
//	├── 2026-03-23/         ← daily partition
//	│   ├── ts.col          ← timestamps  ([]int64,   8 bytes each)
//	│   ├── sid.col         ← series IDs  ([]uint64,  8 bytes each)
//	│   └── val.col         ← values      ([]float64, 8 bytes each)
//	├── 2026-03-22/
//	│   └── ...
//
// Column-oriented storage enables efficient scans: reading all timestamps
// to filter a time range touches only the ts.col file, without loading
// series IDs or values. Aggregation over float64 arrays is cache-friendly
// and fast.
//
// # Write Path
//
// Incoming samples are buffered in memory and periodically flushed to disk
// (every 5 seconds or when the buffer reaches 1024 samples). The buffer is
// sorted by timestamp before writing to keep partition files ordered.
//
// # Query Path
//
// Queries scan only the partition directories that overlap the requested
// time range. For each partition, columns are read independently — first
// timestamps for range filtering, then series IDs for metric filtering,
// then values for the matching rows. Results can be aggregated into time
// buckets using Sum, Avg, Min, Max, Count, or Last.
//
// # Auto-Pruning
//
// A background goroutine runs hourly and deletes partition directories
// older than the retention period (default: 30 days). Pruning is a simple
// directory delete — no compaction or garbage collection needed.
//
// # Basic Usage
//
//	store := metrics.New(filepath.Join(dataDir, "metrics"),
//	    metrics.WithSystemMetrics(),
//	    metrics.WithLogger(logger),
//	)
//
//	// Lifecycle integration.
//	lc.Append(lifecycle.Hook{
//	    OnStart: store.Start,
//	    OnStop:  store.Stop,
//	})
//
//	// Record metrics from handlers.
//	store.Record("http_requests", 1, "method", "GET", "status", "200")
//	store.Record("http_request_duration", 0.045, "method", "GET")
//
//	// Query metrics.
//	result, err := store.Query(metrics.Query{
//	    Name:  "http_requests",
//	    Labels: map[string]string{"method": "GET"},
//	    Start: time.Now().Add(-1 * time.Hour),
//	    End:   time.Now(),
//	    Step:  1 * time.Minute,
//	    Fn:    metrics.Sum,
//	})
//
// # System Metrics
//
// When WithSystemMetrics() is enabled, the store automatically records Go
// runtime metrics every 10 seconds: goroutine count, heap usage, GC pauses,
// allocation rates, and more. These provide a built-in application health
// dashboard without any external monitoring agent.
//
// # Thread Safety
//
// The Store is safe for concurrent use. Multiple goroutines can call Record
// simultaneously. Queries can run concurrently with writes (queries also
// check the in-memory buffer for unflushed samples).
package metrics
