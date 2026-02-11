package cache

import (
	"sync"
	"time"
)

type entry[T any] struct {
	value     T
	timestamp time.Time
}

// Cache is a generic thread-safe TTL cache keyed by string.
type Cache[T any] struct {
	mu       sync.RWMutex
	items    map[string]entry[T]
	duration time.Duration
}

// New creates a Cache with the given TTL duration.
func New[T any](duration time.Duration) *Cache[T] {
	return &Cache[T]{
		items:    make(map[string]entry[T]),
		duration: duration,
	}
}

// Get returns the cached value and true if the key exists and hasn't expired.
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.items[key]
	if !ok || time.Since(e.timestamp) >= c.duration {
		var zero T
		return zero, false
	}
	return e.value, true
}

// Set stores a value under the given key with the current timestamp.
func (c *Cache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = entry[T]{value: value, timestamp: time.Now()}
}

// Delete removes a key from the cache.
func (c *Cache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
}
