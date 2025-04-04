package cache

type EvictionPolicy interface {
	Add(key string)
	Remove(key string)
	GetEvictionCandidate() (string, bool)
}
type PolicyType string

const (
	LRU PolicyType = "lru"
	LFU PolicyType = "lfu"
	FIFO PolicyType = "fifo"
	TTL PolicyType = "ttl"
	RANDOM PolicyType = "random"
)
const DefaultTTLDuration int64 = 300

// NewEvictionPolicy creates a new eviction policy of the specified type
func NewEvictionPolicy(policyType PolicyType, options ...interface{}) EvictionPolicy {
	switch policyType {
	case LRU:
		return NewLRUEviction()
	case LFU:
		return NewLFUEviction()
	case FIFO:
		return NewFIFOEviction()
	case TTL:
		// Default TTL duration if not specified
		ttlDuration := DefaultTTLDuration
		if len(options) > 0 {
			if duration, ok := options[0].(int64); ok {
				ttlDuration = duration
			}
		}
		return NewTTLEviction(ttlDuration)
	case RANDOM:
		return NewRandomEviction()
	default:
		// Default to LRU if not specified
		return NewLRUEviction()
	}
}
