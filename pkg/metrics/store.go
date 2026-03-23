package metrics

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/stanza-go/framework/pkg/log"
)

// Store is the column-oriented metrics storage engine. It buffers incoming
// samples in memory and periodically flushes them to daily partition
// directories on disk. Each partition contains three column files (timestamps,
// series IDs, values) for efficient columnar reads.
//
// The store is safe for concurrent use by multiple goroutines.
type Store struct {
	dir    string
	series *seriesRegistry

	mu     sync.Mutex
	buffer []sample

	partMu     sync.RWMutex
	partitions map[string]*partition

	retention     time.Duration
	flushSize     int
	flushInterval time.Duration
	systemMetrics bool

	logger *log.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new metrics store that persists data under the given directory.
//
//	store := metrics.New(filepath.Join(dataDir, "metrics"),
//	    metrics.WithSystemMetrics(),
//	    metrics.WithLogger(logger),
//	)
func New(dir string, opts ...Option) *Store {
	cfg := storeConfig{
		retention:     30 * 24 * time.Hour,
		flushSize:     1024,
		flushInterval: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Store{
		dir:           dir,
		buffer:        make([]sample, 0, cfg.flushSize),
		partitions:    make(map[string]*partition),
		retention:     cfg.retention,
		flushSize:     cfg.flushSize,
		flushInterval: cfg.flushInterval,
		systemMetrics: cfg.systemMetrics,
		logger:        cfg.logger,
	}
}

// Start initializes the store directory, loads the series registry, and
// starts background goroutines for periodic flushing, pruning, and
// (optionally) system metrics collection.
func (s *Store) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	reg, err := loadSeriesRegistry(filepath.Join(s.dir, "series.txt"))
	if err != nil {
		return err
	}
	s.series = reg

	ctx, s.cancel = context.WithCancel(ctx)

	s.wg.Add(1)
	go s.flushLoop(ctx)

	s.wg.Add(1)
	go s.pruneLoop(ctx)

	if s.systemMetrics {
		s.wg.Add(1)
		go s.collectSystemMetrics(ctx)
	}

	if s.logger != nil {
		s.logger.Info("metrics store started", log.String("dir", s.dir))
	}
	return nil
}

// Stop flushes remaining buffered data, closes all partitions, and waits
// for background goroutines to finish.
func (s *Store) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	// Final flush.
	if err := s.flush(); err != nil && s.logger != nil {
		s.logger.Error("metrics final flush failed", log.Err(err))
	}

	// Close series registry.
	if s.series != nil {
		s.series.close()
	}

	// Close all partitions.
	s.partMu.Lock()
	for _, p := range s.partitions {
		p.close()
	}
	s.partitions = make(map[string]*partition)
	s.partMu.Unlock()

	if s.logger != nil {
		s.logger.Info("metrics store stopped")
	}
	return nil
}

// Record records a metric sample. Labels are passed as alternating key-value
// string pairs. Odd trailing label arguments are ignored.
//
//	store.Record("http_request_duration", 0.123, "method", "GET", "status", "200")
//	store.Record("cpu_usage", 0.85)
func (s *Store) Record(name string, value float64, labels ...string) {
	labelMap := make(map[string]string, len(labels)/2)
	for i := 0; i+1 < len(labels); i += 2 {
		labelMap[labels[i]] = labels[i+1]
	}

	seriesID := s.series.resolve(name, labelMap)

	s.mu.Lock()
	s.buffer = append(s.buffer, sample{
		ts:       time.Now().UnixMilli(),
		seriesID: seriesID,
		value:    value,
	})
	needFlush := len(s.buffer) >= s.flushSize
	s.mu.Unlock()

	if needFlush {
		_ = s.flush()
	}
}

// Names returns all registered metric names, sorted alphabetically.
func (s *Store) Names() []string {
	return s.series.names()
}

// LabelValues returns all unique values for a label key on the given metric.
func (s *Store) LabelValues(name, labelKey string) []string {
	return s.series.labelValues(name, labelKey)
}

// Stats returns storage statistics.
func (s *Store) Stats() StoreStats {
	stats := StoreStats{
		SeriesCount: s.series.count(),
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return stats
	}

	for _, entry := range entries {
		if !entry.IsDir() || !isDateDir(entry.Name()) {
			continue
		}
		stats.PartitionCount++
		if stats.OldestDate == "" || entry.Name() < stats.OldestDate {
			stats.OldestDate = entry.Name()
		}
		if entry.Name() > stats.NewestDate {
			stats.NewestDate = entry.Name()
		}

		// Sum file sizes in partition directory.
		partDir := filepath.Join(s.dir, entry.Name())
		partEntries, err := os.ReadDir(partDir)
		if err != nil {
			continue
		}
		for _, pe := range partEntries {
			if info, err := pe.Info(); err == nil {
				stats.DiskBytes += info.Size()
			}
		}
	}

	// Include series.txt size.
	if info, err := os.Stat(filepath.Join(s.dir, "series.txt")); err == nil {
		stats.DiskBytes += info.Size()
	}

	return stats
}

// flush drains the in-memory buffer to disk, sorted by timestamp and
// grouped by daily partition.
func (s *Store) flush() error {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return nil
	}
	buf := s.buffer
	s.buffer = make([]sample, 0, s.flushSize)
	s.mu.Unlock()

	// Sort by timestamp for ordered column files.
	sort.Slice(buf, func(i, j int) bool {
		return buf[i].ts < buf[j].ts
	})

	// Group by date partition.
	groups := make(map[string][]sample)
	for _, sm := range buf {
		date := time.UnixMilli(sm.ts).UTC().Format("2006-01-02")
		groups[date] = append(groups[date], sm)
	}

	for date, samples := range groups {
		p, err := s.getOrCreatePartition(date)
		if err != nil {
			return err
		}
		if err := p.append(samples); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) getOrCreatePartition(date string) (*partition, error) {
	s.partMu.RLock()
	p, ok := s.partitions[date]
	s.partMu.RUnlock()
	if ok {
		return p, nil
	}

	s.partMu.Lock()
	defer s.partMu.Unlock()

	// Double-check after acquiring write lock.
	if p, ok := s.partitions[date]; ok {
		return p, nil
	}

	dir := filepath.Join(s.dir, date)
	p, err := openPartition(dir)
	if err != nil {
		return nil, err
	}
	s.partitions[date] = p
	return p, nil
}

func (s *Store) flushLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.flush(); err != nil && s.logger != nil {
				s.logger.Error("metrics flush failed", log.Err(err))
			}
		}
	}
}

func (s *Store) pruneLoop(ctx context.Context) {
	defer s.wg.Done()

	// Prune once on startup.
	s.prune()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.prune()
		}
	}
}

func (s *Store) prune() {
	cutoff := time.Now().UTC().Add(-s.retention).Format("2006-01-02")

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !isDateDir(entry.Name()) {
			continue
		}
		if entry.Name() >= cutoff {
			continue
		}

		// Close partition if open.
		s.partMu.Lock()
		if p, ok := s.partitions[entry.Name()]; ok {
			p.close()
			delete(s.partitions, entry.Name())
		}
		s.partMu.Unlock()

		// Remove the directory.
		dir := filepath.Join(s.dir, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			if s.logger != nil {
				s.logger.Error("metrics prune failed", log.String("dir", dir), log.Err(err))
			}
		} else if s.logger != nil {
			s.logger.Info("metrics pruned partition", log.String("date", entry.Name()))
		}
	}
}

// isDateDir checks if a directory name matches YYYY-MM-DD format.
func isDateDir(name string) bool {
	if len(name) != 10 {
		return false
	}
	return name[4] == '-' && name[7] == '-'
}
