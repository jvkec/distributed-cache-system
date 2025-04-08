package cache

import (
	"container/list"
	"sync"
)
type FIFOEviction struct {
	items     map[string]*list.Element
	evictList *list.List
	mutex     sync.Mutex
}

// NewFIFOEviction creates a new FIFO eviction policy
func NewFIFOEviction() *FIFOEviction {
	return &FIFOEviction{
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// Add an item to the FIFO policy
func (fifo *FIFOEviction) Add(key string) {
	fifo.mutex.Lock()
	defer fifo.mutex.Unlock()

	// If key already exists, we don't change its position
	if _, exists := fifo.items[key]; exists {
		return
	}

	// Add new item to the back of the list (newest items at the back)
	element := fifo.evictList.PushBack(key)
	fifo.items[key] = element
}

// Remove an item from the FIFO policy
func (fifo *FIFOEviction) Remove(key string) {
	fifo.mutex.Lock()
	defer fifo.mutex.Unlock()

	if element, exists := fifo.items[key]; exists {
		fifo.evictList.Remove(element)
		delete(fifo.items, key)
	}
}

// GetEvictionCandidate returns the oldest inserted key
func (fifo *FIFOEviction) GetEvictionCandidate() (string, bool) {
	fifo.mutex.Lock()
	defer fifo.mutex.Unlock()

	if fifo.evictList.Len() == 0 {
		return "", false
	}

	// Get the oldest element (front of the list)
	element := fifo.evictList.Front()
	return element.Value.(string), true
}