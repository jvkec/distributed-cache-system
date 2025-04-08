package cache

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"sync"
	"time"
	"encoding/json"
	"bytes"
	"io"
)

// NodeInfo contains information about a cache node
type NodeInfo struct {
	ID      string
	Address string // HTTP address like "http://localhost:8080"
}

// DistributedCache implements the Cache interface with distribution across multiple nodes
type DistributedCache struct {
	localCache  Cache
	nodes       []NodeInfo
	localNodeID string
	client      *http.Client
	mutex       sync.RWMutex
}

// NewDistributedCache creates a new distributed cache
func NewDistributedCache(localCache Cache, localNodeID string, localAddress string) *DistributedCache {
	return &DistributedCache{
		localCache:  localCache,
		nodes:       []NodeInfo{{ID: localNodeID, Address: localAddress}},
		localNodeID: localNodeID,
		client:      &http.Client{Timeout: 5 * time.Second},
	}
}

// AddNode adds a new node to the cluster
func (dc *DistributedCache) AddNode(nodeID, address string) error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	// Check if node already exists
	for _, node := range dc.nodes {
		if node.ID == nodeID {
			return errors.New("node already exists")
		}
	}

	// Add the new node
	dc.nodes = append(dc.nodes, NodeInfo{ID: nodeID, Address: address})
	return nil
}

// RemoveNode removes a node from the cluster
func (dc *DistributedCache) RemoveNode(nodeID string) error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	// Cannot remove self
	if nodeID == dc.localNodeID {
		return errors.New("cannot remove local node")
	}

	// Find and remove the node
	for i, node := range dc.nodes {
		if node.ID == nodeID {
			dc.nodes = append(dc.nodes[:i], dc.nodes[i+1:]...)
			return nil
		}
	}

	return errors.New("node not found")
}

// getNodeForKey determines which node should handle a given key
func (dc *DistributedCache) getNodeForKey(key string) NodeInfo {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		// This should never happen since local node is always in the list
		return NodeInfo{ID: dc.localNodeID}
	}

	// Use consistent hashing to determine the node
	h := fnv.New32()
	h.Write([]byte(key))
	hashValue := h.Sum32()
	nodeIndex := int(hashValue) % len(dc.nodes)

	return dc.nodes[nodeIndex]
}

// Set adds or updates an item in the cache
func (dc *DistributedCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	node := dc.getNodeForKey(key)

	// If it's the local node, use local cache
	if node.ID == dc.localNodeID {
		return dc.localCache.Set(ctx, key, value, ttl)
	}

	// Otherwise, forward the request to the appropriate node
	return dc.forwardSet(ctx, node, key, value, ttl)
}

// forwardSet sends a Set request to another node
func (dc *DistributedCache) forwardSet(ctx context.Context, node NodeInfo, key string, value interface{}, ttl time.Duration) error {
	// Serialize the value
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to serialize value: %w", err)
	}

	// Construct URL with TTL parameter
	u, err := url.Parse(fmt.Sprintf("%s/cache/%s", node.Address, key))
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	q := u.Query()
	q.Set("ttl", fmt.Sprintf("%d", int(ttl.Seconds())))
	u.RawQuery = q.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "PUT", u.String(), bytes.NewReader(valueJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	return nil
}

// Get retrieves an item from the cache
func (dc *DistributedCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	node := dc.getNodeForKey(key)

	// If it's the local node, use local cache
	if node.ID == dc.localNodeID {
		return dc.localCache.Get(ctx, key)
	}

	// Otherwise, forward the request to the appropriate node
	return dc.forwardGet(ctx, node, key)
}

// forwardGet sends a Get request to another node
func (dc *DistributedCache) forwardGet(ctx context.Context, node NodeInfo, key string) (interface{}, bool, error) {
	// Construct URL
	u := fmt.Sprintf("%s/cache/%s", node.Address, key)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Handle not found
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}

	// Check for other errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", err)
	}

	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, false, fmt.Errorf("failed to parse response: %w", err)
	}

	return value, true, nil
}

// Delete removes an item from the cache
func (dc *DistributedCache) Delete(ctx context.Context, key string) error {
	node := dc.getNodeForKey(key)

	// If it's the local node, use local cache
	if node.ID == dc.localNodeID {
		return dc.localCache.Delete(ctx, key)
	}

	// Otherwise, forward the request to the appropriate node
	return dc.forwardDelete(ctx, node, key)
}

// forwardDelete sends a Delete request to another node
func (dc *DistributedCache) forwardDelete(ctx context.Context, node NodeInfo, key string) error {
	// Construct URL
	u := fmt.Sprintf("%s/cache/%s", node.Address, key)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	return nil
}

// Clear removes all items from the cache
func (dc *DistributedCache) Clear(ctx context.Context) error {
	dc.mutex.RLock()
	nodes := make([]NodeInfo, len(dc.nodes))
	copy(nodes, dc.nodes)
	dc.mutex.RUnlock()

	// Send clear request to all nodes
	var errs []error
	for _, node := range nodes {
		if node.ID == dc.localNodeID {
			if err := dc.localCache.Clear(ctx); err != nil {
				errs = append(errs, fmt.Errorf("failed to clear local cache: %w", err))
			}
		} else {
			if err := dc.forwardClear(ctx, node); err != nil {
				errs = append(errs, fmt.Errorf("failed to clear node %s: %w", node.ID, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some nodes failed to clear: %v", errs)
	}
	return nil
}

// forwardClear sends a Clear request to another node
func (dc *DistributedCache) forwardClear(ctx context.Context, node NodeInfo) error {
	// Construct URL
	u := fmt.Sprintf("%s/clear", node.Address)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", u, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	return nil
}

// SetMany adds multiple items to the cache
func (dc *DistributedCache) SetMany(ctx context.Context, items map[string]interface{}, ttl time.Duration) error {
	// Group items by node
	nodeItems := make(map[string]map[string]interface{})
	
	for key, value := range items {
		node := dc.getNodeForKey(key)
		if _, exists := nodeItems[node.ID]; !exists {
			nodeItems[node.ID] = make(map[string]interface{})
		}
		nodeItems[node.ID][key] = value
	}

	// Process items for each node
	var errs []error
	for nodeID, nodeMap := range nodeItems {
		if nodeID == dc.localNodeID {
			if err := dc.localCache.SetMany(ctx, nodeMap, ttl); err != nil {
				errs = append(errs, err)
			}
		} else {
			// Find the node
			var node NodeInfo
			for _, n := range dc.nodes {
				if n.ID == nodeID {
					node = n
					break
				}
			}
			
			// Forward to each node one by one
			// In a production system, you might want to batch these
			for k, v := range nodeMap {
				if err := dc.forwardSet(ctx, node, k, v, ttl); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some set operations failed: %v", errs)
	}
	return nil
}

// GetMany retrieves multiple items from the cache
func (dc *DistributedCache) GetMany(ctx context.Context, keys []string) (map[string]interface{}, error) {
	// Group keys by node
	nodeKeys := make(map[string][]string)
	
	for _, key := range keys {
		node := dc.getNodeForKey(key)
		nodeKeys[node.ID] = append(nodeKeys[node.ID], key)
	}

	// Process keys for each node and collect results
	result := make(map[string]interface{})
	var errs []error
	
	for nodeID, nodeKeyList := range nodeKeys {
		if nodeID == dc.localNodeID {
			localResults, err := dc.localCache.GetMany(ctx, nodeKeyList)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			
			// Merge results
			for k, v := range localResults {
				result[k] = v
			}
		} else {
			// Find the node
			var node NodeInfo
			for _, n := range dc.nodes {
				if n.ID == nodeID {
					node = n
					break
				}
			}
			
			// Forward to node one by one
			// In a production system, you might want to batch these
			for _, k := range nodeKeyList {
				if v, found, err := dc.forwardGet(ctx, node, k); err != nil {
					errs = append(errs, err)
				} else if found {
					result[k] = v
				}
			}
		}
	}

	if len(errs) > 0 {
		return result, fmt.Errorf("some get operations failed: %v", errs)
	}
	return result, nil
}

// DeleteMany removes multiple items from the cache
func (dc *DistributedCache) DeleteMany(ctx context.Context, keys []string) error {
	// Group keys by node
	nodeKeys := make(map[string][]string)
	
	for _, key := range keys {
		node := dc.getNodeForKey(key)
		nodeKeys[node.ID] = append(nodeKeys[node.ID], key)
	}

	// Process keys for each node
	var errs []error
	
	for nodeID, nodeKeyList := range nodeKeys {
		if nodeID == dc.localNodeID {
			if err := dc.localCache.DeleteMany(ctx, nodeKeyList); err != nil {
				errs = append(errs, err)
			}
		} else {
			// Find the node
			var node NodeInfo
			for _, n := range dc.nodes {
				if n.ID == nodeID {
					node = n
					break
				}
			}
			
			// Forward to node one by one
			for _, k := range nodeKeyList {
				if err := dc.forwardDelete(ctx, node, k); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some delete operations failed: %v", errs)
	}
	return nil
}

// Keys returns all keys in the cache across all nodes
func (dc *DistributedCache) Keys(ctx context.Context) ([]string, error) {
	dc.mutex.RLock()
	nodes := make([]NodeInfo, len(dc.nodes))
	copy(nodes, dc.nodes)
	dc.mutex.RUnlock()

	// Collect keys from all nodes
	var allKeys []string
	var errs []error
	
	for _, node := range nodes {
		if node.ID == dc.localNodeID {
			localKeys, err := dc.localCache.Keys(ctx)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			allKeys = append(allKeys, localKeys...)
		} else {
			nodeKeys, err := dc.forwardKeys(ctx, node)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			allKeys = append(allKeys, nodeKeys...)
		}
	}

	if len(errs) > 0 {
		return allKeys, fmt.Errorf("failed to get keys from some nodes: %v", errs)
	}
	return allKeys, nil
}

// forwardKeys sends a Keys request to another node
func (dc *DistributedCache) forwardKeys(ctx context.Context, node NodeInfo) ([]string, error) {
	// Construct URL
	u := fmt.Sprintf("%s/keys", node.Address)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	// Parse response
	var keys []string
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, fmt.Errorf("failed to parse keys from node %s: %w", node.ID, err)
	}

	return keys, nil
}

// Len returns the total number of items in the cache across all nodes
func (dc *DistributedCache) Len(ctx context.Context) (int, error) {
	dc.mutex.RLock()
	nodes := make([]NodeInfo, len(dc.nodes))
	copy(nodes, dc.nodes)
	dc.mutex.RUnlock()

	// Count items from all nodes
	var totalItems int
	var errs []error
	
	for _, node := range nodes {
		if node.ID == dc.localNodeID {
			localCount, err := dc.localCache.Len(ctx)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			totalItems += localCount
		} else {
			nodeCount, err := dc.forwardLen(ctx, node)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			totalItems += nodeCount
		}
	}

	if len(errs) > 0 {
		return totalItems, fmt.Errorf("failed to get count from some nodes: %v", errs)
	}
	return totalItems, nil
}

// forwardLen sends a Len request to another node
func (dc *DistributedCache) forwardLen(ctx context.Context, node NodeInfo) (int, error) {
	// Construct URL
	u := fmt.Sprintf("%s/len", node.Address)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	// Parse response
	var count int
	if err := json.NewDecoder(resp.Body).Decode(&count); err != nil {
		return 0, fmt.Errorf("failed to parse count from node %s: %w", node.ID, err)
	}

	return count, nil
}

// Stats returns aggregated cache statistics from all nodes
func (dc *DistributedCache) Stats(ctx context.Context) (Stats, error) {
	dc.mutex.RLock()
	nodes := make([]NodeInfo, len(dc.nodes))
	copy(nodes, dc.nodes)
	dc.mutex.RUnlock()

	// Collect stats from all nodes
	var aggStats Stats
	var errs []error
	
	for _, node := range nodes {
		if node.ID == dc.localNodeID {
			localStats, err := dc.localCache.Stats(ctx)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			aggStats.ItemCount += localStats.ItemCount
			aggStats.Hits += localStats.Hits
			aggStats.Misses += localStats.Misses
			aggStats.EvictionCount += localStats.EvictionCount
			aggStats.TotalMemoryUsed += localStats.TotalMemoryUsed
		} else {
			nodeStats, err := dc.forwardStats(ctx, node)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			aggStats.ItemCount += nodeStats.ItemCount
			aggStats.Hits += nodeStats.Hits
			aggStats.Misses += nodeStats.Misses
			aggStats.EvictionCount += nodeStats.EvictionCount
			aggStats.TotalMemoryUsed += nodeStats.TotalMemoryUsed
		}
	}

	// Calculate aggregated hit rate
	totalAccess := aggStats.Hits + aggStats.Misses
	if totalAccess > 0 {
		aggStats.HitRate = float64(aggStats.Hits) / float64(totalAccess)
	}

	if len(errs) > 0 {
		return aggStats, fmt.Errorf("failed to get stats from some nodes: %v", errs)
	}
	return aggStats, nil
}

// forwardStats sends a Stats request to another node
func (dc *DistributedCache) forwardStats(ctx context.Context, node NodeInfo) (Stats, error) {
	// Construct URL
	u := fmt.Sprintf("%s/stats", node.Address)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := dc.client.Do(req)
	if err != nil {
		return Stats{}, fmt.Errorf("failed to send request to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Stats{}, fmt.Errorf("node %s returned error: %s", node.ID, string(body))
	}

	// Parse response
	var stats Stats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return Stats{}, fmt.Errorf("failed to parse stats from node %s: %w", node.ID, err)
	}

	return stats, nil
}

// GetNodes returns a list of all nodes in the cluster
func (dc *DistributedCache) GetNodes() []NodeInfo {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	
	nodes := make([]NodeInfo, len(dc.nodes))
	copy(nodes, dc.nodes)
	return nodes
}