package http

import (
	"fmt"
	"runtime"
)

// PrometheusMetric represents a single metric for Prometheus text
// exposition format. Counters are monotonically increasing values
// (requests, errors). Gauges are point-in-time values that can go up
// or down (active connections, queue depth).
type PrometheusMetric struct {
	Name  string  // metric name, e.g. "stanza_http_requests_total"
	Help  string  // one-line description
	Type  string  // "counter" or "gauge"
	Value float64 // current value
}

// RuntimeMetrics returns standard Go runtime metrics for Prometheus
// exposition. The returned metrics use the same names as the official Go
// Prometheus client library, making them compatible with standard Go
// Grafana dashboards and alerting rules.
//
// RuntimeMetrics calls runtime.ReadMemStats, which briefly stops the
// world. This is negligible at typical scrape intervals (15–60 seconds).
//
// Append the result to your application metrics:
//
//	out = append(out, http.RuntimeMetrics()...)
func RuntimeMetrics() []PrometheusMetric {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return []PrometheusMetric{
		{Name: "go_goroutines", Help: "Number of goroutines that currently exist", Type: "gauge", Value: float64(runtime.NumGoroutine())},
		{Name: "go_memstats_alloc_bytes", Help: "Number of bytes allocated and still in use", Type: "gauge", Value: float64(mem.Alloc)},
		{Name: "go_memstats_sys_bytes", Help: "Number of bytes obtained from system", Type: "gauge", Value: float64(mem.Sys)},
		{Name: "go_memstats_heap_inuse_bytes", Help: "Number of heap bytes in use", Type: "gauge", Value: float64(mem.HeapInuse)},
		{Name: "go_memstats_stack_inuse_bytes", Help: "Number of stack bytes in use", Type: "gauge", Value: float64(mem.StackInuse)},
		{Name: "go_memstats_heap_objects", Help: "Number of allocated heap objects", Type: "gauge", Value: float64(mem.HeapObjects)},
		{Name: "go_gc_completed_total", Help: "Number of completed GC cycles", Type: "counter", Value: float64(mem.NumGC)},
		{Name: "go_gc_pause_total_seconds", Help: "Total cumulative GC pause duration in seconds", Type: "counter", Value: float64(mem.PauseTotalNs) / 1e9},
	}
}

// PrometheusHandler returns a handler that renders metrics in Prometheus
// text exposition format (text/plain; version=0.0.4). The collect
// function is called on each scrape to gather current metric values.
//
// Example:
//
//	router.HandleFunc("GET /metrics", http.PrometheusHandler(func() []http.PrometheusMetric {
//	    stats := db.Stats()
//	    return []http.PrometheusMetric{
//	        {Name: "myapp_db_reads_total", Help: "Total read queries", Type: "counter", Value: float64(stats.TotalReads)},
//	    }
//	}))
func PrometheusHandler(collect func() []PrometheusMetric) HandlerFunc {
	return func(w ResponseWriter, r *Request) {
		metrics := collect()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		for _, m := range metrics {
			if m.Help != "" {
				fmt.Fprintf(w, "# HELP %s %s\n", m.Name, m.Help)
			}
			if m.Type != "" {
				fmt.Fprintf(w, "# TYPE %s %s\n", m.Name, m.Type)
			}
			fmt.Fprintf(w, "%s %g\n", m.Name, m.Value)
		}
	}
}
