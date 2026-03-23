package metrics

import (
	"context"
	"runtime"
	"time"
)

// collectSystemMetrics records Go runtime metrics every 10 seconds.
// Collected metrics:
//
//   - go_goroutines          — current goroutine count
//   - go_heap_alloc_bytes    — heap bytes allocated and still in use
//   - go_heap_inuse_bytes    — heap bytes in spans that have at least one object
//   - go_heap_objects        — number of allocated heap objects
//   - go_stack_inuse_bytes   — stack bytes in use by goroutines
//   - go_gc_pause_ns         — duration of the most recent GC pause (nanoseconds)
//   - go_gc_runs             — total completed GC cycles
//   - go_alloc_bytes_total   — cumulative bytes allocated (even if freed)
//   - go_sys_bytes           — total bytes of memory obtained from the OS
//   - go_mallocs_total       — cumulative count of heap allocations
//   - go_frees_total         — cumulative count of heap frees
func (s *Store) collectSystemMetrics(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			s.Record("go_goroutines", float64(runtime.NumGoroutine()))
			s.Record("go_heap_alloc_bytes", float64(m.HeapAlloc))
			s.Record("go_heap_inuse_bytes", float64(m.HeapInuse))
			s.Record("go_heap_objects", float64(m.HeapObjects))
			s.Record("go_stack_inuse_bytes", float64(m.StackInuse))
			s.Record("go_gc_pause_ns", float64(m.PauseNs[(m.NumGC+255)%256]))
			s.Record("go_gc_runs", float64(m.NumGC))
			s.Record("go_alloc_bytes_total", float64(m.TotalAlloc))
			s.Record("go_sys_bytes", float64(m.Sys))
			s.Record("go_mallocs_total", float64(m.Mallocs))
			s.Record("go_frees_total", float64(m.Frees))
		}
	}
}
