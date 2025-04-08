package cache

import (
	"container/list"
	"sync"
)

type LFUEviction struct {
	items       map[string]*lfuItem
	frequencies map[int]*list.List // Map of frequency -> list of keys with that frequency
	minFreq     int
	mutex       sync.Mutex
}

// lfuItem represents an item in the LFU cache
type lfuItem struct {
	key       string
	frequency int
	element   *list.Element
}

// NewLFUEviction creates a new LFU eviction policy
func NewLFUEviction() *LFUEviction {
	return &LFUEviction{
		items:       make(map[string]*lfuItem),
		frequencies: make(map[int]*list.List),
		minFreq:     0,
	}
}

// Add adds or updates an item in the LFU policy
func (lfu *LFUEviction) Add(key string) {
	lfu.mutex.Lock()
	defer lfu.mutex.Unlock()

	// If key already exists, increase its frequency
	if item, exists := lfu.items[key]; exists {
		lfu.incrementFrequency(item)
		return
	}

	// If this is a new key, add it with frequency 1
	if _, exists := lfu.frequencies[1]; !exists {
		lfu.frequencies[1] = list.New()
	}
	
	element := lfu.frequencies[1].PushFront(key)
	lfu.items[key] = &lfuItem{
		key:       key,
		frequency: 1,
		element:   element,
	}
	
	lfu.minFreq = 1
}

// incrementFrequency increments the frequency of an item
func (lfu *LFUEviction) incrementFrequency(item *lfuItem) {
	// Remove from current frequency list
	currentFreq := item.frequency
	lfu.frequencies[currentFreq].Remove(item.element)
	
	// Update minimum frequency if necessary
	if currentFreq == lfu.minFreq && lfu.frequencies[currentFreq].Len() == 0 {
		lfu.minFreq++
	}
	
	// Increment frequency
	newFreq := currentFreq + 1
	if _, exists := lfu.frequencies[newFreq]; !exists {
		lfu.frequencies[newFreq] = list.New()
	}
	
	// Add to new frequency list
	item.frequency = newFreq
	item.element = lfu.frequencies[newFreq].PushFront(item.key)
}

// Remove removes an item from the LFU policy
func (lfu *LFUEviction) Remove(key string) {
	lfu.mutex.Lock()
	defer lfu.mutex.Unlock()

	if item, exists := lfu.items[key]; exists {
		lfu.frequencies[item.frequency].Remove(item.element)
		delete(lfu.items, key)
		
		// Update minFreq if necessary
		if item.frequency == lfu.minFreq && lfu.frequencies[lfu.minFreq].Len() == 0 {
			// Find the new minimum frequency
			found := false
			for i := lfu.minFreq + 1; i < len(lfu.frequencies)+lfu.minFreq+1; i++ {
				if list, exists := lfu.frequencies[i]; exists && list.Len() > 0 {
					lfu.minFreq = i
					found = true
					break
				}
			}
			
			if !found {
				lfu.minFreq = 0
			}
		}
	}
}

// GetEvictionCandidate returns the least frequently used key
func (lfu *LFUEviction) GetEvictionCandidate() (string, bool) {
	lfu.mutex.Lock()
	defer lfu.mutex.Unlock()

	if len(lfu.items) == 0 {
		return "", false
	}

	// Get an element with the minimum frequency
	if list, exists := lfu.frequencies[lfu.minFreq]; exists && list.Len() > 0 {
		// Return the least recently used among least frequently used
		element := list.Back()
		return element.Value.(string), true
	}

	return "", false
}