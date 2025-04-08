package cache

import (
	"container/list"
	"sync"
)
type LRUEviction struct {
	items 		map[string]*list.Element
	evictList	*list.List
	mutex 		sync.Mutex
}

// NewLRUEviction creates a new LRU eviction policy
func NewLRUEviction() *LRUEviction {
	return &LRUEviction{
		items:		make(map[string]*list.Element),
		evictList:	list.New(),
	}
}

// Add or update an item in the LRU policy
func (lru *LRUEviction) Add(key string) {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	// If key exists already, move to front
	if element, exists := lru.items[key]; exists {
		lru.evictList.MoveToFront(element)
		return
	}

	// Add new item to the front of the list
	element := lru.evictList.PushFront(key)
	lru.items[key] = element
}

// Remove removes an item from the LRU policy
func (lru *LRUEviction) Remove(key string) {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	if element, exists := lru.items[key]; exists {
		lru.evictList.Remove(element)
		delete(lru.items, key)
	}
}

// This function returns the least recently used key
func (lru *LRUEviction) GetEvictionCandidate() (string, bool) {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	if lru.evictList.Len() == 0 {
		return "", false
	}

	// Get the oldest element (back of the list)
	element := lru.evictList.Back()
	return element.Value.(string), true
}