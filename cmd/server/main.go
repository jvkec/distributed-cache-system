package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jvkec/distributed-cache-system/cache"
	"github.com/google/uuid"
)

var (
	evictionPolicy  = flag.String("eviction", "lru", "Eviction policy (lru, lfu, fifo, ttl, random)")
	maxItems        = flag.Int("max-items", 1000, "Maximum number of items in cache")
	defaultTTL      = flag.Duration("ttl", 5*time.Minute, "Default time-to-live for cache items")
	cleanupInterval = flag.Duration("cleanup", 30*time.Second, "Interval for cleanup of expired items")
	port            = flag.Int("port", 8080, "Port to listen on")
	nodeID          = flag.String("node-id", "", "Unique ID for this node (generated if not provided)")
	clusterNodes    = flag.String("cluster", "", "Comma-separated list of other nodes in the cluster (format: id=host:port)")
	host            = flag.String("host", "localhost", "Hostname or IP address for this node")
	distributed     = flag.Bool("distributed", false, "Enable distributed mode")
)

func main() {
	flag.Parse()

	// Generate node ID if not provided
	if *nodeID == "" {
		*nodeID = uuid.New().String()[:8]
	}

	// Create local cache with provided options
	localCache, err := cache.NewInMemoryCache(cache.Options{
		MaxItems:        *maxItems,
		DefaultTTL:      *defaultTTL,
		EvictionPolicy:  *evictionPolicy,
		CleanupInterval: *cleanupInterval,
	})
	if err != nil {
		log.Fatalf("Failed to create cache: %v", err)
	}
	defer localCache.Close()
	
	// Set up cache instance - either local or distributed
	var cacheInstance cache.Cache
	cacheInstance = localCache
	
	// If distributed mode is enabled, set up distributed cache
	if *distributed {
		// Local node address for other nodes to connect to
		localAddress := fmt.Sprintf("http://%s:%d", *host, *port)
		
		// Create distributed cache with local cache as backend
		distCache := cache.NewDistributedCache(localCache, *nodeID, localAddress)
		
		// Add other nodes if specified
		if *clusterNodes != "" {
			nodes := strings.Split(*clusterNodes, ",")
			for _, nodeInfo := range nodes {
				parts := strings.Split(nodeInfo, "=")
				if len(parts) != 2 {
					log.Printf("Warning: Invalid node format: %s, expected id=host:port", nodeInfo)
					continue
				}
				
				id := parts[0]
				addr := fmt.Sprintf("http://%s", parts[1])
				
				if err := distCache.AddNode(id, addr); err != nil {
					log.Printf("Warning: Failed to add node %s: %v", id, err)
				} else {
					log.Printf("Added node %s at %s to cluster", id, addr)
				}
			}
		}
		
		cacheInstance = distCache
		log.Printf("Running in distributed mode with node ID: %s", *nodeID)
	} else {
		log.Printf("Running in standalone mode")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	
	// Add cluster management routes if in distributed mode
	if *distributed {
		distCache, ok := cacheInstance.(*cache.DistributedCache)
		if ok {
			// Get node information
			mux.HandleFunc("GET /cluster/nodes", func(w http.ResponseWriter, r *http.Request) {
				nodes := distCache.GetNodes()
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(nodes)
			})
			
			// Add a node to the cluster
			mux.HandleFunc("POST /cluster/nodes", func(w http.ResponseWriter, r *http.Request) {
				var node struct {
					ID      string `json:"id"`
					Address string `json:"address"`
				}
				
				if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
					http.Error(w, "Invalid request body", http.StatusBadRequest)
					return
				}
				
				if node.ID == "" || node.Address == "" {
					http.Error(w, "ID and address are required", http.StatusBadRequest)
					return
				}
				
				if err := distCache.AddNode(node.ID, node.Address); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				
				w.WriteHeader(http.StatusOK)
			})
			
			// Remove a node from the cluster
			mux.HandleFunc("DELETE /cluster/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
				nodeID := r.PathValue("id")
				
				if err := distCache.RemoveNode(nodeID); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				
				w.WriteHeader(http.StatusOK)
			})
		}
	}

	// Register cache routes
	mux.HandleFunc("GET /cache/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		value, found, err := cacheInstance.Get(r.Context(), key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(value)
	})

	mux.HandleFunc("PUT /cache/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		
		// Parse TTL from query parameter if provided
		ttl := *defaultTTL
		if ttlStr := r.URL.Query().Get("ttl"); ttlStr != "" {
			ttlSeconds, err := strconv.Atoi(ttlStr)
			if err != nil {
				http.Error(w, "Invalid TTL parameter", http.StatusBadRequest)
				return
			}
			ttl = time.Duration(ttlSeconds) * time.Second
		}
		
		// Parse JSON value
		var value interface{}
		if err := json.Unmarshal(body, &value); err != nil {
			http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
			return
		}
		
		// Set value in cache
		if err := cacheInstance.Set(r.Context(), key, value, ttl); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("DELETE /cache/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		err := cacheInstance.Delete(r.Context(), key)
		if err == cache.ErrKeyNotFound {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Add bulk operations
	mux.HandleFunc("POST /cache/bulk/get", func(w http.ResponseWriter, r *http.Request) {
		var keys []string
		if err := json.NewDecoder(r.Body).Decode(&keys); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		
		values, err := cacheInstance.GetMany(r.Context(), keys)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(values)
	})
	
	mux.HandleFunc("POST /cache/bulk/set", func(w http.ResponseWriter, r *http.Request) {
		var items map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		
		// Parse TTL from query parameter if provided
		ttl := *defaultTTL
		if ttlStr := r.URL.Query().Get("ttl"); ttlStr != "" {
			ttlSeconds, err := strconv.Atoi(ttlStr)
			if err != nil {
				http.Error(w, "Invalid TTL parameter", http.StatusBadRequest)
				return
			}
			ttl = time.Duration(ttlSeconds) * time.Second
		}
		
		if err := cacheInstance.SetMany(r.Context(), items, ttl); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
	})
	
	mux.HandleFunc("POST /cache/bulk/delete", func(w http.ResponseWriter, r *http.Request) {
		var keys []string
		if err := json.NewDecoder(r.Body).Decode(&keys); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		
		if err := cacheInstance.DeleteMany(r.Context(), keys); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
	})

	// Add additional endpoints needed for distributed cache
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		keys, err := cacheInstance.Keys(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keys)
	})
	
	mux.HandleFunc("GET /len", func(w http.ResponseWriter, r *http.Request) {
		count, err := cacheInstance.Len(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(count)
	})
	
	mux.HandleFunc("POST /clear", func(w http.ResponseWriter, r *http.Request) {
		if err := cacheInstance.Clear(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := cacheInstance.Stats(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})
	
	// Add node info endpoint
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"node_id":         *nodeID,
			"distributed":     *distributed,
			"eviction_policy": *evictionPolicy,
			"max_items":       *maxItems,
			"default_ttl":     (*defaultTTL).String(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting cache server on port %d with %s eviction policy", *port, *evictionPolicy)
		if *distributed {
			log.Printf("Node ID: %s, Address: http://%s:%d", *nodeID, *host, *port)
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	
	log.Println("Server exited properly")
}