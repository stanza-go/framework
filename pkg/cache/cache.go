package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Cache is a thread-safe in-memory key-value store with TTL-based expiration.
// The type parameter V is the value type. Keys are always strings.
type Cache[V any] struct {
	mu       sync.RWMutex
	items    map[string]*entry[V]
	ttl      time.Duration
	maxSize  int
	onEvict  func(string, V)
	stopCh   chan struct{}
	stopped  bool
	now      func() time.Time // for testing; defaults to time.Now

	totalHits      atomic.Int64
	totalMisses    atomic.Int64
	totalEvictions atomic.Int64
}

type entry[V any] struct {
	value      V
	expiresAt  time.Time
	accessedAt time.Time
}

type config[V any] struct {
	ttl             time.Duration
	maxSize         int
	cleanupInterval time.Duration
	onEvict         func(string, V)
}

// Option configures the Cache.
type Option[V any] func(*config[V])

// WithTTL sets the default time-to-live for cache entries. Entries are
// considered expired after this duration and will be removed on next access
// or during background cleanup. Default is 5 minutes.
func WithTTL[V any](d time.Duration) Option[V] {
	return func(c *config[V]) {
		c.ttl = d
	}
}

// WithMaxSize sets the maximum number of entries in the cache. When the limit
// is reached, the least recently accessed entry is evicted to make room.
// A value of 0 means unlimited. Default is 0.
func WithMaxSize[V any](n int) Option[V] {
	return func(c *config[V]) {
		c.maxSize = n
	}
}

// WithCleanupInterval sets how often the background goroutine removes expired
// entries. A value of 0 disables background cleanup entirely. Default is
// 1 minute.
func WithCleanupInterval[V any](d time.Duration) Option[V] {
	return func(c *config[V]) {
		c.cleanupInterval = d
	}
}

// WithOnEvict registers a callback that fires when an entry is evicted due
// to expiration, LRU eviction, or explicit deletion. The callback runs
// synchronously under the cache lock — keep it fast.
func WithOnEvict[V any](fn func(key string, value V)) Option[V] {
	return func(c *config[V]) {
		c.onEvict = fn
	}
}

// New creates a new Cache with the given options. The cache starts a
// background goroutine for periodic cleanup of expired entries. Call Close
// to stop the goroutine when the cache is no longer needed.
func New[V any](opts ...Option[V]) *Cache[V] {
	cfg := config[V]{
		ttl:             5 * time.Minute,
		cleanupInterval: 1 * time.Minute,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	c := &Cache[V]{
		items:   make(map[string]*entry[V]),
		ttl:     cfg.ttl,
		maxSize: cfg.maxSize,
		onEvict: cfg.onEvict,
		stopCh:  make(chan struct{}),
		now:     time.Now,
	}

	if cfg.cleanupInterval > 0 {
		go c.cleanup(cfg.cleanupInterval)
	}

	return c
}

// Get retrieves a value by key. Returns the value and true if found and not
// expired, or the zero value and false otherwise. Accessing an entry updates
// its last-accessed time for LRU purposes.
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		c.totalMisses.Add(1)
		var zero V
		return zero, false
	}
	if c.now().After(e.expiresAt) {
		c.deleteLocked(key)
		c.totalMisses.Add(1)
		c.totalEvictions.Add(1)
		var zero V
		return zero, false
	}
	e.accessedAt = c.now()
	c.totalHits.Add(1)
	return e.value, true
}

// Set stores a value with the cache's default TTL.
func (c *Cache[V]) Set(key string, value V) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL stores a value with a specific TTL. A TTL of 0 uses the cache's
// default TTL.
func (c *Cache[V]) SetWithTTL(key string, value V, ttl time.Duration) {
	if ttl == 0 {
		ttl = c.ttl
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update in place.
	if e, ok := c.items[key]; ok {
		if c.onEvict != nil {
			c.onEvict(key, e.value)
		}
		e.value = value
		e.expiresAt = now.Add(ttl)
		e.accessedAt = now
		return
	}

	// Evict if at capacity.
	if c.maxSize > 0 && len(c.items) >= c.maxSize {
		c.evictLRU()
	}

	c.items[key] = &entry[V]{
		value:      value,
		expiresAt:  now.Add(ttl),
		accessedAt: now,
	}
}

// GetOrSet returns the cached value for key if it exists and is not expired.
// Otherwise it calls fn to compute the value, stores it with the default TTL,
// and returns it. If fn returns an error, the value is not cached.
func (c *Cache[V]) GetOrSet(key string, fn func() (V, error)) (V, error) {
	if val, ok := c.Get(key); ok {
		return val, nil
	}

	val, err := fn()
	if err != nil {
		return val, err
	}

	c.Set(key, val)
	return val, nil
}

// GetOrSetWithTTL is like GetOrSet but stores the computed value with a
// specific TTL.
func (c *Cache[V]) GetOrSetWithTTL(key string, ttl time.Duration, fn func() (V, error)) (V, error) {
	if val, ok := c.Get(key); ok {
		return val, nil
	}

	val, err := fn()
	if err != nil {
		return val, err
	}

	c.SetWithTTL(key, val, ttl)
	return val, nil
}

// Delete removes an entry by key. If the entry exists and an OnEvict callback
// is registered, it is called with the evicted key and value.
func (c *Cache[V]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(key)
}

// Clear removes all entries from the cache. OnEvict is called for each entry.
func (c *Cache[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.onEvict != nil {
		for k, e := range c.items {
			c.onEvict(k, e.value)
		}
	}
	c.items = make(map[string]*entry[V])
}

// Len returns the number of entries in the cache, including expired entries
// that have not yet been cleaned up.
func (c *Cache[V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Keys returns all keys in the cache, including expired entries that have
// not yet been cleaned up.
func (c *Cache[V]) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// CacheStats holds cache performance counters.
type CacheStats struct {
	Size      int   `json:"size"`      // current number of entries
	MaxSize   int   `json:"max_size"`  // configured maximum (0 = unlimited)
	Hits      int64 `json:"hits"`      // total cache hits (key found and not expired)
	Misses    int64 `json:"misses"`    // total cache misses (key not found or expired)
	Evictions int64 `json:"evictions"` // total involuntary removals (TTL expiry + LRU)
}

// Stats returns a snapshot of cache performance counters. The counters
// are cumulative since the cache was created. Size is the current number
// of entries (including expired entries not yet cleaned up).
func (c *Cache[V]) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()

	return CacheStats{
		Size:      size,
		MaxSize:   c.maxSize,
		Hits:      c.totalHits.Load(),
		Misses:    c.totalMisses.Load(),
		Evictions: c.totalEvictions.Load(),
	}
}

// Close stops the background cleanup goroutine. After Close, the cache
// can still be used for Get/Set/Delete but no automatic cleanup occurs.
// Close is safe to call multiple times.
func (c *Cache[V]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped {
		close(c.stopCh)
		c.stopped = true
	}
}

// cleanup runs periodically to remove expired entries.
func (c *Cache[V]) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.removeExpired()
		}
	}
}

// removeExpired deletes all expired entries.
func (c *Cache[V]) removeExpired() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var evicted int64
	for k, e := range c.items {
		if now.After(e.expiresAt) {
			if c.onEvict != nil {
				c.onEvict(k, e.value)
			}
			delete(c.items, k)
			evicted++
		}
	}
	if evicted > 0 {
		c.totalEvictions.Add(evicted)
	}
}

// evictLRU removes the least recently accessed entry. Must be called under
// write lock.
func (c *Cache[V]) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, e := range c.items {
		if first || e.accessedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.accessedAt
			first = false
		}
	}

	if !first {
		c.deleteLocked(oldestKey)
		c.totalEvictions.Add(1)
	}
}

// deleteLocked removes an entry. Must be called under write lock.
func (c *Cache[V]) deleteLocked(key string) {
	if e, ok := c.items[key]; ok {
		if c.onEvict != nil {
			c.onEvict(key, e.value)
		}
		delete(c.items, key)
	}
}
