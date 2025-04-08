package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

// InMemoryCache is a concrete implementation of the Cache interface
type InMemoryCache struct {
	data            map[string]*Item
	evictionPolicy  EvictionPolicy
	maxItems        int
	maxMemory       int64
	defaultTTL      time.Duration
	mutex           sync.RWMutex
	stats           Stats
	cleanupInterval time.Duration
	stopCleanup     chan bool
}

// Errors defined for cache operations
var (
	ErrKeyNotFound     = errors.New("key not found")
	ErrMemoryExceeded  = errors.New("memory limit exceeded")
	ErrItemLimitExceeded = errors.New("item limit exceeded")
)

// NewInMemoryCache creates a new in-memory cache with provided options
func NewInMemoryCache(opts Options) (*InMemoryCache, error) {
	if opts.MaxItems <= 0 && opts.MaxMemory <= 0 {
		return nil, errors.New("either MaxItems or MaxMemory must be set")
	}
	
	// Create appropriate eviction policy
	var policy EvictionPolicy
	switch opts.EvictionPolicy {
	case "lru":
		policy = NewLRUEviction()
	case "lfu":
		policy = NewLFUEviction()
	case "fifo":
		policy = NewFIFOEviction()
	case "ttl":
		policy = NewTTLEviction(int64(opts.DefaultTTL.Seconds()))
	case "random":
		policy = NewRandomEviction()
	default:
		policy = NewLRUEviction() // Default to LRU
	}
	
	cache := &InMemoryCache{
		data:            make(map[string]*Item),
		evictionPolicy:  policy,
		maxItems:        opts.MaxItems,
		maxMemory:       opts.MaxMemory,
		defaultTTL:      opts.DefaultTTL,
		stats:           Stats{},
		cleanupInterval: opts.CleanupInterval,
		stopCleanup:     make(chan bool),
	}
	
	// Start background cleanup if interval is set
	if opts.CleanupInterval > 0 {
		go cache.startCleanupWorker()
	}
	
	return cache, nil
}

// startCleanupWorker starts a background goroutine that periodically cleans up expired items
func (c *InMemoryCache) startCleanupWorker() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.cleanupExpired()
		case <-c.stopCleanup:
			return
		}
	}
}

// cleanupExpired removes expired items from the cache
func (c *InMemoryCache) cleanupExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	now := time.Now().UnixNano()
	for key, item := range c.data {
		// If item has an expiration and it's in the past
		if item.Expiration > 0 && now > item.Expiration {
			c.removeItemLocked(key)
			c.stats.EvictionCount++
		}
	}
}

// Set adds or updates an item in the cache
func (c *InMemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	// Check if the key already exists (update)
	if existingItem, found := c.data[key]; found {
		c.updateItemLocked(key, value, ttl)
		// Update the memory usage approximation
		c.stats.TotalMemoryUsed -= estimateSize(existingItem.Value)
		c.stats.TotalMemoryUsed += estimateSize(value)
		return nil
	}
	
	// Handle memory/item limits and eviction if needed
	if c.maxItems > 0 && len(c.data) >= c.maxItems {
		if !c.evictItemLocked() {
			return ErrItemLimitExceeded
		}
	}
	
	// Calculate expiration time
	var exp int64
	if ttl > 0 {
		exp = time.Now().Add(ttl).UnixNano()
	} else if c.defaultTTL > 0 {
		exp = time.Now().Add(c.defaultTTL).UnixNano()
	}
	
	// Create new item
	item := &Item{
		Key:          key,
		Value:        value,
		Expiration:   exp,
		LastAccessed: time.Now().UnixNano(),
		CreatedTime:  time.Now().UnixNano(),
		AccessCount:  0,
	}
	
	// Check memory usage
	newSize := estimateSize(value)
	if c.maxMemory > 0 && c.stats.TotalMemoryUsed+newSize > c.maxMemory {
		// Try to evict until we have enough space
		for c.stats.TotalMemoryUsed+newSize > c.maxMemory {
			if !c.evictItemLocked() {
				return ErrMemoryExceeded
			}
		}
	}
	
	// Add to cache and update stats
	c.data[key] = item
	c.evictionPolicy.Add(key)
	c.stats.TotalMemoryUsed += newSize
	c.stats.ItemCount = len(c.data)
	
	return nil
}

// updateItemLocked updates an existing item in the cache
// Caller must hold the lock
func (c *InMemoryCache) updateItemLocked(key string, value interface{}, ttl time.Duration) {
	item := c.data[key]
	item.Value = value
	
	// Update expiration if ttl is provided
	if ttl > 0 {
		item.Expiration = time.Now().Add(ttl).UnixNano()
	}
	
	item.LastAccessed = time.Now().UnixNano()
	item.AccessCount++
	
	// Update policy to reflect this access
	c.evictionPolicy.Add(key)
}

// Get retrieves an item from the cache
func (c *InMemoryCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	c.mutex.RLock()
	item, found := c.data[key]
	c.mutex.RUnlock()
	
	if !found {
		c.mutex.Lock()
		c.stats.Misses++
		c.updateHitRateLocked()
		c.mutex.Unlock()
		return nil, false, nil
	}
	
	// Check if item has expired
	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		c.mutex.Lock()
		c.removeItemLocked(key)
		c.stats.Misses++
		c.updateHitRateLocked()
		c.mutex.Unlock()
		return nil, false, nil
	}
	
	// Update stats and item metadata
	c.mutex.Lock()
	item.LastAccessed = time.Now().UnixNano()
	item.AccessCount++
	c.stats.Hits++
	c.updateHitRateLocked()
	
	// Update policy to reflect this access
	c.evictionPolicy.Add(key)
	c.mutex.Unlock()
	
	return item.Value, true, nil
}

// Delete removes an item from the cache
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if _, found := c.data[key]; !found {
		return ErrKeyNotFound
	}
	
	c.removeItemLocked(key)
	return nil
}

// removeItemLocked removes an item from the cache
// Caller must hold the lock
func (c *InMemoryCache) removeItemLocked(key string) {
	if item, found := c.data[key]; found {
		c.stats.TotalMemoryUsed -= estimateSize(item.Value)
		delete(c.data, key)
		c.evictionPolicy.Remove(key)
		c.stats.ItemCount = len(c.data)
	}
}

// Clear removes all items from the cache
func (c *InMemoryCache) Clear(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	c.data = make(map[string]*Item)
	// Recreate the eviction policy based on its current type
	switch c.evictionPolicy.(type) {
	case *LRUEviction:
		c.evictionPolicy = NewLRUEviction()
	case *LFUEviction:
		c.evictionPolicy = NewLFUEviction()
	case *FIFOEviction:
		c.evictionPolicy = NewFIFOEviction()
	case *TTLEviction:
		c.evictionPolicy = NewTTLEviction(int64(c.defaultTTL.Seconds()))
	case *RandomEviction:
		c.evictionPolicy = NewRandomEviction()
	}
	
	c.stats.TotalMemoryUsed = 0
	c.stats.ItemCount = 0
	
	return nil
}

// SetMany adds multiple items to the cache
func (c *InMemoryCache) SetMany(ctx context.Context, items map[string]interface{}, ttl time.Duration) error {
	for key, value := range items {
		err := c.Set(ctx, key, value, ttl)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetMany retrieves multiple items from the cache
func (c *InMemoryCache) GetMany(ctx context.Context, keys []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	
	for _, key := range keys {
		if value, found, _ := c.Get(ctx, key); found {
			result[key] = value
		}
	}
	
	return result, nil
}

// DeleteMany removes multiple items from the cache
func (c *InMemoryCache) DeleteMany(ctx context.Context, keys []string) error {
	for _, key := range keys {
		_ = c.Delete(ctx, key) // Ignore not found errors
	}
	return nil
}

// Keys returns all keys in the cache
func (c *InMemoryCache) Keys(ctx context.Context) ([]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	keys := make([]string, 0, len(c.data))
	for key := range c.data {
		keys = append(keys, key)
	}
	return keys, nil
}

// Len returns the number of items in the cache
func (c *InMemoryCache) Len(ctx context.Context) (int, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return len(c.data), nil
}

// Stats returns cache statistics
func (c *InMemoryCache) Stats(ctx context.Context) (Stats, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return c.stats, nil
}

// updateHitRateLocked updates the hit rate stat
// Caller must hold the lock
func (c *InMemoryCache) updateHitRateLocked() {
	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	} else {
		c.stats.HitRate = 0
	}
}

// evictItemLocked evicts an item based on the eviction policy
// Caller must hold the lock
// Returns true if an item was evicted, false otherwise
func (c *InMemoryCache) evictItemLocked() bool {
	key, ok := c.evictionPolicy.GetEvictionCandidate()
	if !ok {
		return false
	}
	
	c.removeItemLocked(key)
	c.stats.EvictionCount++
	return true
}

// estimateSize provides a rough estimate of the memory size of a value
// This is a simplified approach. For production use, you might want more accurate measurement
func estimateSize(value interface{}) int64 {
	switch v := value.(type) {
	case string:
		return int64(len(v))
	case []byte:
		return int64(len(v))
	case int, int32, float32, bool:
		return 4
	case int64, float64:
		return 8
	case map[string]interface{}:
		var size int64
		for k, val := range v {
			size += int64(len(k)) + estimateSize(val)
		}
		return size
	case []interface{}:
		var size int64
		for _, item := range v {
			size += estimateSize(item)
		}
		return size
	default:
		// For complex types, use a reasonable approximation
		return 64 // Arbitrary size for unknown types
	}
}

// Close cleans up resources used by the cache
func (c *InMemoryCache) Close() error {
	close(c.stopCleanup)
	return nil
}