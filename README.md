# Distributed Cache System

A Redis-like in-memory cache system with distributed capabilities, written in Go.

## Features

- Fast in-memory caching
- Multiple eviction policies (LRU, LFU, FIFO, Random, TTL)
- Configurable TTL (Time-To-Live) for cache entries
- HTTP API for cache operations
- Distributed caching across multiple nodes
- Consistent hashing for key distribution
- Statistics and monitoring

## Getting Started

### Prerequisites

- Go 1.24 or higher

### Installation

```bash
# Clone the repository
git clone https://github.com/jvkec/distributed-cache-system.git
cd distributed-cache-system

# Build the server
go build -o bin/server cmd/server/main.go
```

### Running in Standalone Mode

```bash
# Run with default settings (LRU eviction, port 8080)
./bin/server

# Run with LFU eviction policy
./bin/server -eviction=lfu

# Run with FIFO eviction policy
./bin/server -eviction=fifo

# More options
./bin/server -max-items=5000 -ttl=10m -cleanup=1m -port=9090
```

### Running in Distributed Mode

Start the first node:
```bash
./bin/server -distributed -node-id=node1 -host=localhost -port=8081
```

Start additional nodes and connect them to the cluster:
```bash
./bin/server -distributed -node-id=node2 -host=localhost -port=8082 -cluster="node1=localhost:8081"

# Add a third node connecting to the first two
./bin/server -distributed -node-id=node3 -host=localhost -port=8083 -cluster="node1=localhost:8081,node2=localhost:8082"
```

Available command-line options:
- `-eviction`: Eviction policy to use (lru, lfu, fifo, ttl, random) [default: lru]
- `-max-items`: Maximum number of items in cache [default: 1000]
- `-ttl`: Default time-to-live for cache items [default: 5m]
- `-cleanup`: Interval for cleanup of expired items [default: 30s]
- `-port`: Port to listen on [default: 8080]
- `-distributed`: Enable distributed mode
- `-node-id`: Unique ID for this node (generated if not provided)
- `-host`: Hostname or IP address for this node [default: localhost]
- `-cluster`: Comma-separated list of other nodes in the cluster (format: id=host:port)

## API Usage

### Cache Operations

#### Set a value
```bash
curl -X PUT -H "Content-Type: application/json" -d '"Hello, World!"' \
  http://localhost:8080/cache/mykey?ttl=300
```

#### Get a value
```bash
curl http://localhost:8080/cache/mykey
```

#### Delete a value
```bash
curl -X DELETE http://localhost:8080/cache/mykey
```

#### Bulk Operations
```bash
# Get multiple values
curl -X POST -H "Content-Type: application/json" -d '["key1", "key2", "key3"]' \
  http://localhost:8080/cache/bulk/get

# Set multiple values
curl -X POST -H "Content-Type: application/json" \
  -d '{"key1": "value1", "key2": 42, "key3": {"nested": "object"}}' \
  http://localhost:8080/cache/bulk/set?ttl=300

# Delete multiple values
curl -X POST -H "Content-Type: application/json" -d '["key1", "key2", "key3"]' \
  http://localhost:8080/cache/bulk/delete
```

### Cluster Management (Distributed Mode)

#### Get all nodes in the cluster
```bash
curl http://localhost:8080/cluster/nodes
```

#### Add a node to the cluster
```bash
curl -X POST -H "Content-Type: application/json" \
  -d '{"id": "node4", "address": "http://localhost:8084"}' \
  http://localhost:8080/cluster/nodes
```

#### Remove a node from the cluster
```bash
curl -X DELETE http://localhost:8080/cluster/nodes/node4
```

### Monitoring

#### Get cache statistics
```bash
curl http://localhost:8080/stats
```

#### Get node information
```bash
curl http://localhost:8080/info
```

## How It Works

### Eviction Policies

- **LRU (Least Recently Used)**: Removes the items that haven't been accessed for the longest time
- **LFU (Least Frequently Used)**: Removes the items that are used least often
- **FIFO (First In First Out)**: Removes the oldest items first
- **TTL (Time To Live)**: Removes items based on their expiration time
- **Random**: Randomly selects items for removal when cache is full

### Distributed Architecture

This cache system uses a distributed architecture with consistent hashing to distribute keys across multiple nodes:

1. **Key Distribution**: Each key is assigned to a specific node based on its hash value
2. **Request Routing**: Requests are automatically routed to the appropriate node
3. **Node Management**: Nodes can be added or removed dynamically
4. **Aggregated Stats**: Statistics can be collected from all nodes

## Current Status

The project currently implements:
- In-memory cache with configurable options
- Five eviction policies:
  - LRU (Least Recently Used)
  - LFU (Least Frequently Used)
  - FIFO (First In First Out)
  - TTL (Time To Live)
  - Random
- Time-based expiration of cache items
- HTTP API for cache operations
- Distributed caching across multiple nodes
- Consistent hashing for key distribution

Future versions will include:
- Data replication for fault tolerance
- Automatic rebalancing when nodes join/leave
- Authentication and TLS support
- Additional storage backends

## License

[MIT](LICENSE)