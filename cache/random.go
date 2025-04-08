package cache

import (
	"math/rand"
	"sync"
	"time"
)
type RandomEviction struct {
	keys  []string
	mutex sync.Mutex
	r     *rand.Rand
}

// NewRandomEviction creates a new random eviction policy
func NewRandomEviction() *RandomEviction {
	return &RandomEviction{
		keys: make([]string, 0),
		r:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Add adds an item to the random eviction policy
func (r *RandomEviction) Add(key string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Check if key already exists
	for _, k := range r.keys {
		if k == key {
			return
		}
	}

	r.keys = append(r.keys, key)
}

// Remove removes an item from the random eviction policy
func (r *RandomEviction) Remove(key string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for i, k := range r.keys {
		if k == key {
			// Remove the item by swapping with the last element and truncating
			r.keys[i] = r.keys[len(r.keys)-1]
			r.keys = r.keys[:len(r.keys)-1]
			return
		}
	}
}

// GetEvictionCandidate returns a random key for eviction
func (r *RandomEviction) GetEvictionCandidate() (string, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.keys) == 0 {
		return "", false
	}

	// Get a random index
	index := r.r.Intn(len(r.keys))
	return r.keys[index], true
}