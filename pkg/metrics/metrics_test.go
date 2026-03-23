package metrics

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "metrics")
}

func TestSeriesRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "series.txt")

	reg, err := loadSeriesRegistry(path)
	if err != nil {
		t.Fatal(err)
	}

	// First resolve creates a new series.
	id1 := reg.resolve("cpu", nil)
	if id1 == 0 {
		t.Fatal("expected non-zero ID")
	}

	// Same name+labels returns the same ID.
	id1b := reg.resolve("cpu", nil)
	if id1b != id1 {
		t.Fatalf("expected same ID %d, got %d", id1, id1b)
	}

	// Different labels create a different series.
	id2 := reg.resolve("cpu", map[string]string{"core": "0"})
	if id2 == id1 {
		t.Fatal("expected different ID for different labels")
	}

	// Different name creates a different series.
	id3 := reg.resolve("memory", nil)
	if id3 == id1 || id3 == id2 {
		t.Fatal("expected different ID for different name")
	}

	// Lookup works.
	name, labels, ok := reg.lookup(id2)
	if !ok || name != "cpu" || labels["core"] != "0" {
		t.Fatalf("lookup failed: name=%s labels=%v ok=%v", name, labels, ok)
	}

	// matchingSeries works.
	ids := reg.matchingSeries("cpu", nil)
	if len(ids) != 2 {
		t.Fatalf("expected 2 cpu series, got %d", len(ids))
	}

	ids = reg.matchingSeries("cpu", map[string]string{"core": "0"})
	if len(ids) != 1 || ids[0] != id2 {
		t.Fatalf("expected 1 filtered cpu series, got %v", ids)
	}

	// names() returns sorted unique names.
	names := reg.names()
	if len(names) != 2 || names[0] != "cpu" || names[1] != "memory" {
		t.Fatalf("expected [cpu memory], got %v", names)
	}

	reg.close()

	// Reload from disk — should recover all series.
	reg2, err := loadSeriesRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.close()

	if reg2.count() != 3 {
		t.Fatalf("expected 3 series after reload, got %d", reg2.count())
	}

	// Same resolve should return existing IDs.
	if reg2.resolve("cpu", nil) != id1 {
		t.Fatal("ID mismatch after reload")
	}
	if reg2.resolve("cpu", map[string]string{"core": "0"}) != id2 {
		t.Fatal("ID mismatch after reload")
	}
}

func TestPartitionWriteRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "2026-03-23")

	p, err := openPartition(dir)
	if err != nil {
		t.Fatal(err)
	}

	samples := []sample{
		{ts: 1000, seriesID: 1, value: 1.5},
		{ts: 2000, seriesID: 2, value: 2.5},
		{ts: 3000, seriesID: 1, value: 3.5},
	}

	if err := p.append(samples); err != nil {
		t.Fatal(err)
	}

	if p.rowCount() != 3 {
		t.Fatalf("expected 3 rows, got %d", p.rowCount())
	}

	ts, sids, vals, err := p.readAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(ts) != 3 || len(sids) != 3 || len(vals) != 3 {
		t.Fatalf("expected 3 rows, got ts=%d sids=%d vals=%d", len(ts), len(sids), len(vals))
	}

	if ts[0] != 1000 || ts[1] != 2000 || ts[2] != 3000 {
		t.Fatalf("timestamps mismatch: %v", ts)
	}
	if sids[0] != 1 || sids[1] != 2 || sids[2] != 1 {
		t.Fatalf("series IDs mismatch: %v", sids)
	}
	if vals[0] != 1.5 || vals[1] != 2.5 || vals[2] != 3.5 {
		t.Fatalf("values mismatch: %v", vals)
	}

	// Append more — column files grow.
	if err := p.append([]sample{{ts: 4000, seriesID: 3, value: 4.5}}); err != nil {
		t.Fatal(err)
	}

	if p.rowCount() != 4 {
		t.Fatalf("expected 4 rows after second append, got %d", p.rowCount())
	}

	if p.diskBytes() != 4*colSize*3 {
		t.Fatalf("expected %d disk bytes, got %d", 4*colSize*3, p.diskBytes())
	}

	p.close()
}

func TestStoreRecordAndQuery(t *testing.T) {
	dir := tempDir(t)
	store := New(dir, WithFlushSize(10), WithFlushInterval(100*time.Millisecond))

	ctx := context.Background()
	if err := store.Start(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	// Record some metrics.
	store.Record("http_requests", 1, "method", "GET")
	store.Record("http_requests", 1, "method", "POST")
	store.Record("http_requests", 1, "method", "GET")
	store.Record("http_requests", 1, "method", "GET")
	store.Record("cpu_usage", 0.75)
	store.Record("cpu_usage", 0.80)

	// Force flush.
	if err := store.flush(); err != nil {
		t.Fatal(err)
	}

	// Query all http_requests.
	result, err := store.Query(Query{
		Name:  "http_requests",
		Start: now.Add(-1 * time.Minute),
		End:   now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 series: GET and POST.
	if len(result.Series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(result.Series))
	}

	// Count total points.
	totalPoints := 0
	for _, s := range result.Series {
		totalPoints += len(s.Points)
	}
	if totalPoints != 4 {
		t.Fatalf("expected 4 total points, got %d", totalPoints)
	}

	// Query with label filter.
	result, err = store.Query(Query{
		Name:   "http_requests",
		Labels: map[string]string{"method": "GET"},
		Start:  now.Add(-1 * time.Minute),
		End:    now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Series) != 1 {
		t.Fatalf("expected 1 series for GET, got %d", len(result.Series))
	}
	if len(result.Series[0].Points) != 3 {
		t.Fatalf("expected 3 GET points, got %d", len(result.Series[0].Points))
	}

	// Query cpu_usage with aggregation.
	result, err = store.Query(Query{
		Name:  "cpu_usage",
		Start: now.Add(-1 * time.Minute),
		End:   now.Add(1 * time.Minute),
		Step:  1 * time.Minute,
		Fn:    Avg,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Series) != 1 {
		t.Fatalf("expected 1 cpu_usage series, got %d", len(result.Series))
	}

	// Average of 0.75 and 0.80 should be 0.775.
	if len(result.Series[0].Points) == 0 {
		t.Fatal("expected at least 1 aggregated point")
	}
	avg := result.Series[0].Points[0].V
	if math.Abs(avg-0.775) > 0.001 {
		t.Fatalf("expected avg ~0.775, got %f", avg)
	}

	// Names returns registered metrics.
	names := store.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 metric names, got %d", len(names))
	}

	// LabelValues for http_requests.
	methods := store.LabelValues("http_requests", "method")
	if len(methods) != 2 {
		t.Fatalf("expected 2 method values, got %d: %v", len(methods), methods)
	}

	// Stats.
	stats := store.Stats()
	if stats.SeriesCount != 3 {
		t.Fatalf("expected 3 series, got %d", stats.SeriesCount)
	}
	if stats.PartitionCount != 1 {
		t.Fatalf("expected 1 partition, got %d", stats.PartitionCount)
	}
	if stats.DiskBytes == 0 {
		t.Fatal("expected non-zero disk bytes")
	}

	if err := store.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStoreQueryUnflushedBuffer(t *testing.T) {
	dir := tempDir(t)
	// Large flush size so nothing auto-flushes.
	store := New(dir, WithFlushSize(10000), WithFlushInterval(1*time.Hour))

	ctx := context.Background()
	if err := store.Start(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	store.Record("buffer_test", 42.0)

	// Query without flushing — should find the sample in the buffer.
	result, err := store.Query(Query{
		Name:  "buffer_test",
		Start: now.Add(-1 * time.Minute),
		End:   now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Series) != 1 || len(result.Series[0].Points) != 1 {
		t.Fatalf("expected 1 series with 1 point from buffer, got %d series", len(result.Series))
	}
	if result.Series[0].Points[0].V != 42.0 {
		t.Fatalf("expected value 42, got %f", result.Series[0].Points[0].V)
	}

	if err := store.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAggregation(t *testing.T) {
	points := []Point{
		{T: 1000, V: 10},
		{T: 1500, V: 20},
		{T: 2000, V: 30},
		{T: 2500, V: 40},
		{T: 3000, V: 50},
	}

	tests := []struct {
		name     string
		fn       AggFn
		step     int64
		wantLen  int
		wantVals []float64
	}{
		{"sum_1s", Sum, 1000, 3, []float64{30, 70, 50}},
		{"avg_1s", Avg, 1000, 3, []float64{15, 35, 50}},
		{"min_1s", Min, 1000, 3, []float64{10, 30, 50}},
		{"max_1s", Max, 1000, 3, []float64{20, 40, 50}},
		{"count_1s", Count, 1000, 3, []float64{2, 2, 1}},
		{"last_1s", Last, 1000, 3, []float64{20, 40, 50}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := aggregate(points, 1000, 3500, tt.step, tt.fn)
			if len(result) != tt.wantLen {
				t.Fatalf("expected %d buckets, got %d: %v", tt.wantLen, len(result), result)
			}
			for i, want := range tt.wantVals {
				if math.Abs(result[i].V-want) > 0.001 {
					t.Errorf("bucket %d: expected %f, got %f", i, want, result[i].V)
				}
			}
		})
	}
}

func TestPrune(t *testing.T) {
	dir := tempDir(t)
	// Test prune logic without Start/Stop to avoid background goroutine races.
	store := New(dir, WithRetention(24*time.Hour))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a fake old partition directory (48h old, beyond 24h retention).
	oldDate := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02")
	oldDir := filepath.Join(dir, oldDate)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "ts.col"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a recent partition that should NOT be pruned.
	recentDate := time.Now().UTC().Format("2006-01-02")
	recentDir := filepath.Join(dir, recentDate)
	if err := os.MkdirAll(recentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recentDir, "ts.col"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run prune directly.
	store.prune()

	// Old partition should be gone.
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old partition should have been pruned")
	}

	// Recent partition should still exist.
	if _, err := os.Stat(recentDir); err != nil {
		t.Fatal("recent partition should NOT have been pruned")
	}
}

func TestLabelEncoding(t *testing.T) {
	labels := map[string]string{"method": "GET", "status": "200", "path": "/api"}
	encoded := encodeLabelsSorted(labels)
	// Keys should be sorted alphabetically.
	expected := "method=GET,path=/api,status=200"
	if encoded != expected {
		t.Fatalf("expected %q, got %q", expected, encoded)
	}

	// Empty labels.
	if encodeLabelsSorted(nil) != "" {
		t.Fatal("expected empty string for nil labels")
	}
}

func TestMatchLabels(t *testing.T) {
	series := map[string]string{"method": "GET", "status": "200"}

	if !matchLabels(series, nil) {
		t.Fatal("nil filter should match everything")
	}
	if !matchLabels(series, map[string]string{"method": "GET"}) {
		t.Fatal("should match subset")
	}
	if !matchLabels(series, map[string]string{"method": "GET", "status": "200"}) {
		t.Fatal("should match exact")
	}
	if matchLabels(series, map[string]string{"method": "POST"}) {
		t.Fatal("should not match wrong value")
	}
	if matchLabels(series, map[string]string{"host": "localhost"}) {
		t.Fatal("should not match missing key")
	}
}

func TestAggFnString(t *testing.T) {
	tests := []struct {
		fn   AggFn
		want string
	}{
		{Sum, "sum"},
		{Avg, "avg"},
		{Min, "min"},
		{Max, "max"},
		{Count, "count"},
		{Last, "last"},
		{AggFn(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.fn.String(); got != tt.want {
			t.Errorf("AggFn(%d).String() = %q, want %q", tt.fn, got, tt.want)
		}
	}
}
