package cache

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func benchCache(b *testing.B, maxSize int) *Cache[string] {
	b.Helper()
	opts := []Option[string]{
		WithTTL[string](5 * time.Minute),
		WithCleanupInterval[string](time.Hour), // don't clean during bench
	}
	if maxSize > 0 {
		opts = append(opts, WithMaxSize[string](maxSize))
	}
	c := New(opts...)
	b.Cleanup(func() { c.Close() })
	return c
}

// --- Basic operations ---

func BenchmarkCache_Set(b *testing.B) {
	c := benchCache(b, 0)
	b.ResetTimer()
	for i := range b.N {
		c.Set(fmt.Sprintf("key_%d", i), "value")
	}
}

func BenchmarkCache_Get_Hit(b *testing.B) {
	c := benchCache(b, 0)
	c.Set("key", "value")

	b.ResetTimer()
	for range b.N {
		c.Get("key")
	}
}

func BenchmarkCache_Get_Miss(b *testing.B) {
	c := benchCache(b, 0)
	b.ResetTimer()
	for range b.N {
		c.Get("nonexistent")
	}
}

func BenchmarkCache_SetGet_Mixed(b *testing.B) {
	c := benchCache(b, 0)
	// Pre-populate with some data
	for i := range 1000 {
		c.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("val_%d", i))
	}

	b.ResetTimer()
	for i := range b.N {
		if i%5 == 0 {
			c.Set(fmt.Sprintf("key_%d", i%1000), "updated")
		} else {
			c.Get(fmt.Sprintf("key_%d", i%1000))
		}
	}
}

// --- LRU eviction ---

func BenchmarkCache_LRU_Eviction(b *testing.B) {
	c := benchCache(b, 1000)
	// Fill to capacity
	for i := range 1000 {
		c.Set(fmt.Sprintf("key_%d", i), "value")
	}

	b.ResetTimer()
	for i := range b.N {
		// Each set triggers an eviction since cache is full
		c.Set(fmt.Sprintf("new_key_%d", i), "value")
	}
}

// --- Concurrent access ---

func BenchmarkCache_ConcurrentRead(b *testing.B) {
	c := benchCache(b, 0)
	for i := range 1000 {
		c.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("val_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Get(fmt.Sprintf("key_%d", i%1000))
			i++
		}
	})
}

func BenchmarkCache_ConcurrentReadWrite(b *testing.B) {
	c := benchCache(b, 0)
	for i := range 1000 {
		c.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("val_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Set(fmt.Sprintf("key_%d", i%1000), "updated")
			} else {
				c.Get(fmt.Sprintf("key_%d", i%1000))
			}
			i++
		}
	})
}

// --- GetOrSet (cache-aside) ---

func BenchmarkCache_GetOrSet_Hit(b *testing.B) {
	c := benchCache(b, 0)
	c.Set("key", "cached_value")

	b.ResetTimer()
	for range b.N {
		c.GetOrSet("key", func() (string, error) {
			return "computed", nil
		})
	}
}

func BenchmarkCache_GetOrSet_Miss(b *testing.B) {
	c := benchCache(b, 0)

	var counter atomic.Int64
	b.ResetTimer()
	for range b.N {
		key := fmt.Sprintf("key_%d", counter.Add(1))
		c.GetOrSet(key, func() (string, error) {
			return "computed", nil
		})
	}
}
