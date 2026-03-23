package metrics

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// seriesRegistry maps (metric_name, labels) to unique series IDs.
// It persists the mapping to a tab-separated text file for crash recovery.
type seriesRegistry struct {
	mu      sync.RWMutex
	entries map[string]uint64     // serialized key → series ID
	info    map[uint64]seriesInfo // series ID → metadata
	nextID  atomic.Uint64
	file    *os.File // append-only persistence file
}

type seriesInfo struct {
	name   string
	labels map[string]string
}

// loadSeriesRegistry reads existing series from the persistence file and
// opens it for appending new entries.
func loadSeriesRegistry(path string) (*seriesRegistry, error) {
	r := &seriesRegistry{
		entries: make(map[string]uint64),
		info:    make(map[uint64]seriesInfo),
	}

	// Read existing entries if the file exists.
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 2 {
				continue
			}
			id, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil {
				continue
			}
			name := parts[1]
			labels := make(map[string]string)
			if len(parts) == 3 && parts[2] != "" {
				for _, kv := range strings.Split(parts[2], ",") {
					eqIdx := strings.IndexByte(kv, '=')
					if eqIdx > 0 {
						labels[kv[:eqIdx]] = kv[eqIdx+1:]
					}
				}
			}
			key := seriesKey(name, labels)
			r.entries[key] = id
			r.info[id] = seriesInfo{name: name, labels: labels}
			if id >= r.nextID.Load() {
				r.nextID.Store(id)
			}
		}
	}

	// Open file for appending new series.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	r.file = f

	return r, nil
}

// resolve returns the series ID for the given metric name and labels,
// creating a new entry if one does not exist.
func (r *seriesRegistry) resolve(name string, labels map[string]string) uint64 {
	key := seriesKey(name, labels)

	// Fast path: read lock.
	r.mu.RLock()
	id, ok := r.entries[key]
	r.mu.RUnlock()
	if ok {
		return id
	}

	// Slow path: write lock + create.
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if id, ok := r.entries[key]; ok {
		return id
	}

	id = r.nextID.Add(1)
	r.entries[key] = id
	r.info[id] = seriesInfo{name: name, labels: labels}

	// Persist to disk.
	labelStr := encodeLabelsSorted(labels)
	line := fmt.Sprintf("%d\t%s\t%s\n", id, name, labelStr)
	_, _ = r.file.WriteString(line)

	return id
}

// lookup returns the name and labels for a series ID.
func (r *seriesRegistry) lookup(id uint64) (string, map[string]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.info[id]
	if !ok {
		return "", nil, false
	}
	// Return a copy to avoid mutations.
	labels := make(map[string]string, len(info.labels))
	for k, v := range info.labels {
		labels[k] = v
	}
	return info.name, labels, true
}

// matchingSeries returns all series IDs where the name matches and all
// label filters are satisfied (exact match on each filter key).
func (r *seriesRegistry) matchingSeries(name string, labelFilters map[string]string) []uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var ids []uint64
	for id, info := range r.info {
		if info.name != name {
			continue
		}
		if matchLabels(info.labels, labelFilters) {
			ids = append(ids, id)
		}
	}
	return ids
}

// names returns all unique metric names.
func (r *seriesRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, info := range r.info {
		seen[info.name] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// labelValues returns all unique values for a given label key on a metric.
func (r *seriesRegistry) labelValues(name, labelKey string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, info := range r.info {
		if info.name != name {
			continue
		}
		if v, ok := info.labels[labelKey]; ok {
			seen[v] = struct{}{}
		}
	}

	values := make([]string, 0, len(seen))
	for v := range seen {
		values = append(values, v)
	}
	sort.Strings(values)
	return values
}

// count returns the number of registered series.
func (r *seriesRegistry) count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.info)
}

func (r *seriesRegistry) close() {
	if r.file != nil {
		r.file.Close()
	}
}

// seriesKey builds a canonical string key from a metric name and label set.
func seriesKey(name string, labels map[string]string) string {
	return name + "\x00" + encodeLabelsSorted(labels)
}

// encodeLabelsSorted returns labels as a sorted, comma-separated "k=v" string.
func encodeLabelsSorted(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// matchLabels returns true if the series labels contain all filter entries.
func matchLabels(series, filters map[string]string) bool {
	for k, v := range filters {
		if series[k] != v {
			return false
		}
	}
	return true
}
