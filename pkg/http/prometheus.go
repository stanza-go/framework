package http

import "fmt"

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
