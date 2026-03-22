// Package cache provides an in-memory key-value cache with TTL-based
// expiration, optional maximum size with LRU eviction, and automatic
// background cleanup of expired entries.
//
// Basic usage:
//
//	c := cache.New[string](
//		cache.WithTTL[string](5 * time.Minute),
//	)
//	defer c.Close()
//
//	c.Set("greeting", "hello")
//	val, ok := c.Get("greeting") // "hello", true
//
// Cache-aside pattern:
//
//	user, err := c.GetOrSet("user:42", func() (*User, error) {
//		return db.FindUser(42)
//	})
//
// Integration with lifecycle:
//
//	lc.Append(lifecycle.Hook{
//		OnStop: func(ctx context.Context) error {
//			c.Close()
//			return nil
//		},
//	})
package cache
