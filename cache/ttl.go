package cache

import (
	"sync"
	"time"
)

type TTLEviction struct {
	items      map[string]time.Time
	expiration int64 // in seconds
	mutex      sync.Mutex
}

// NewTTLEviction creates a new TTL eviction policy
func NewTTLEviction(expiration int64) *TTLEviction {
	return &TTLEviction{
		items:      make(map[string]time.Time),
		expiration: expiration,
	}
}

// Add adds or updates an item in the TTL policy
func (ttl *TTLEviction) Add(key string) {
	ttl.mutex.Lock()
	defer ttl.mutex.Unlock()

	ttl.items[key] = time.Now()
}

// Remove removes an item from the TTL policy
func (ttl *TTLEviction) Remove(key string) {
	ttl.mutex.Lock()
	defer ttl.mutex.Unlock()

	delete(ttl.items, key)
}

// GetEvictionCandidate returns the oldest key that has expired
func (ttl *TTLEviction) GetEvictionCandidate() (string, bool) {
	ttl.mutex.Lock()
	defer ttl.mutex.Unlock()

	now := time.Now()
	var oldestKey string
	var oldestTime time.Time
	found := false

	for key, timestamp := range ttl.items {
		expirationTime := timestamp.Add(time.Duration(ttl.expiration) * time.Second)
		if now.After(expirationTime) {
			if !found || timestamp.Before(oldestTime) {
				oldestKey = key
				oldestTime = timestamp
				found = true
			}
		}
	}

	return oldestKey, found
}

// IsExpired checks if a particular key has expired
func (ttl *TTLEviction) IsExpired(key string) bool {
	ttl.mutex.Lock()
	defer ttl.mutex.Unlock()

	timestamp, exists := ttl.items[key]
	if !exists {
		return false
	}

	expirationTime := timestamp.Add(time.Duration(ttl.expiration) * time.Second)
	return time.Now().After(expirationTime)
}