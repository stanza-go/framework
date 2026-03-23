package metrics

import (
	"math"
	"os"
	"sort"
)

// Query executes a metrics query and returns the matching data, optionally
// aggregated into time buckets.
//
// If Step is zero, raw samples are returned. Otherwise, samples are grouped
// into buckets of the given duration and aggregated with Fn.
//
//	result, err := store.Query(metrics.Query{
//	    Name:  "http_request_duration",
//	    Labels: map[string]string{"method": "GET"},
//	    Start: time.Now().Add(-1 * time.Hour),
//	    End:   time.Now(),
//	    Step:  1 * time.Minute,
//	    Fn:    metrics.Avg,
//	})
func (s *Store) Query(q Query) (*Result, error) {
	matchIDs := s.series.matchingSeries(q.Name, q.Labels)
	if len(matchIDs) == 0 {
		return &Result{}, nil
	}

	matchSet := make(map[uint64]bool, len(matchIDs))
	for _, id := range matchIDs {
		matchSet[id] = true
	}

	startMs := q.Start.UnixMilli()
	endMs := q.End.UnixMilli()
	startDate := q.Start.UTC().Format("2006-01-02")
	endDate := q.End.UTC().Format("2006-01-02")

	// Collect raw points per series.
	seriesPoints := make(map[uint64][]Point)

	// Scan matching partitions.
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() || !isDateDir(entry.Name()) {
			continue
		}
		date := entry.Name()
		if date < startDate || date > endDate {
			continue
		}

		p, err := s.getOrCreatePartition(date)
		if err != nil {
			continue
		}

		ts, sids, vals, err := p.readAll()
		if err != nil {
			continue
		}

		for i := range ts {
			if ts[i] < startMs || ts[i] > endMs {
				continue
			}
			if !matchSet[sids[i]] {
				continue
			}
			seriesPoints[sids[i]] = append(seriesPoints[sids[i]], Point{
				T: ts[i],
				V: vals[i],
			})
		}
	}

	// Also check the in-memory buffer for unflushed samples.
	s.mu.Lock()
	bufCopy := make([]sample, len(s.buffer))
	copy(bufCopy, s.buffer)
	s.mu.Unlock()

	for _, sm := range bufCopy {
		if sm.ts < startMs || sm.ts > endMs {
			continue
		}
		if !matchSet[sm.seriesID] {
			continue
		}
		seriesPoints[sm.seriesID] = append(seriesPoints[sm.seriesID], Point{
			T: sm.ts,
			V: sm.value,
		})
	}

	// Build result.
	result := &Result{
		Series: make([]SeriesData, 0, len(seriesPoints)),
	}

	for seriesID, points := range seriesPoints {
		name, labels, ok := s.series.lookup(seriesID)
		if !ok {
			continue
		}

		// Sort points by timestamp.
		sort.Slice(points, func(i, j int) bool {
			return points[i].T < points[j].T
		})

		if q.Step > 0 {
			points = aggregate(points, startMs, endMs, q.Step.Milliseconds(), q.Fn)
		}

		result.Series = append(result.Series, SeriesData{
			Name:   name,
			Labels: labels,
			Points: points,
		})
	}

	// Sort result series by label string for deterministic output.
	sort.Slice(result.Series, func(i, j int) bool {
		return encodeLabelsSorted(result.Series[i].Labels) < encodeLabelsSorted(result.Series[j].Labels)
	})

	return result, nil
}

// aggregate groups points into time buckets and applies the aggregation function.
func aggregate(points []Point, startMs, endMs, stepMs int64, fn AggFn) []Point {
	if stepMs <= 0 || len(points) == 0 {
		return points
	}

	// Align bucket start to step boundary.
	bucketStart := startMs - (startMs % stepMs)
	bucketCount := int((endMs-bucketStart)/stepMs) + 1

	type bucket struct {
		sum   float64
		count int
		min   float64
		max   float64
		last  float64
		lastT int64
	}

	buckets := make([]bucket, bucketCount)
	for i := range buckets {
		buckets[i].min = math.MaxFloat64
		buckets[i].max = -math.MaxFloat64
	}

	for _, p := range points {
		idx := int((p.T - bucketStart) / stepMs)
		if idx < 0 || idx >= len(buckets) {
			continue
		}
		b := &buckets[idx]
		b.sum += p.V
		b.count++
		if p.V < b.min {
			b.min = p.V
		}
		if p.V > b.max {
			b.max = p.V
		}
		if p.T > b.lastT {
			b.lastT = p.T
			b.last = p.V
		}
	}

	result := make([]Point, 0, bucketCount)
	for i, b := range buckets {
		if b.count == 0 {
			continue
		}
		t := bucketStart + int64(i)*stepMs
		var v float64
		switch fn {
		case Sum:
			v = b.sum
		case Avg:
			v = b.sum / float64(b.count)
		case Min:
			v = b.min
		case Max:
			v = b.max
		case Count:
			v = float64(b.count)
		case Last:
			v = b.last
		}
		result = append(result, Point{T: t, V: v})
	}

	return result
}
