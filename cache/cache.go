package cache

import (
	"context"
	"time"
)

// Exported structs
type Item struct {
	Key 			string
	Expiration 		int64
	Value 			interface{}	// Actual data being cached
	LastAccessed 	int64
	AccessCount 	int64 	// Keeping track for LFU
	CreatedTime 	int64	// Keeping track for FIFO
}
type Stats struct {
	ItemCount 		int
	Hits 			int64
	Misses 			int64
	HitRate 		float64
	EvictionCount 	int64 	// Number of Items evicted
	TotalMemoryUsed int64	// Approximation in bytes
}
type Options struct {
	MaxItems		int
	DefaultTTL		time.Duration // Default time-to-live for items
	MaxMemory		int64
	EvictionPolicy	string
	CleanupInterval	time.Duration // How often to run cleanup of expired items
}

// Exported interface
type Cache interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
    Get(ctx context.Context, key string) (interface{}, bool, error)
    Delete(ctx context.Context, key string) error
    Clear(ctx context.Context) error

	SetMany(ctx context.Context, items map[string]interface{}, ttl time.Duration) error
	GetMany(ctx context.Context, keys []string) (map[string]interface{}, error)
	DeleteMany(ctx context.Context, keys []string) error

	Keys(ctx context.Context) ([]string, error)
	Len(ctx context.Context) (int, error)
	Stats(ctx context.Context) (Stats, error)
}