# Redis Health Checker

This library provides a health checker component for Redis, integrating with the `github.com/hivanov/go-healthcheck/src/core` package. It uses the official `github.com/redis/go-redis/v9` client library for Go.

## Health Check Details

The Redis health checker performs the following operations to determine the health of the Redis server:

- **Connection Check**: Attempts to establish a connection to the Redis server using the provided connection options.
- **Ping Command**: Sends a `PING` command to the Redis server. A successful `PONG` response indicates that the server is alive and responsive.

## How to Use

The `redis` package provides functions to create a new Redis health checker component.

### `NewRedisChecker`

This is the primary constructor for creating a Redis health checker.

```go
package main

import (
	"context"
	"fmt"
	"github.com/hivanov/go-healthcheck/src/core"
	"github.com/hivanov/go-healthcheck/src/redis"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	descriptor := core.Descriptor{
		ComponentID:   "my-redis-instance",
		ComponentType: "datastore",
		Description:   "Health check for the main Redis data store.",
	}

	redisOptions := &redis.Options{
		Addr:     "localhost:6379", // Redis server address
		Password: "",               // No password
		DB:       0,                // Default DB
	}

	// Create a new Redis checker component
	// The checker will ping Redis every 5 seconds with a 1-second ping timeout.
	checker := redis.NewRedisChecker(descriptor, 5*time.Second, 1*time.Second, redisOptions)
	defer checker.Close()

	// You can subscribe to status changes
	statusChanges := checker.StatusChange()
	go func() {
		for status := range statusChanges {
			fmt.Printf("Redis status changed: %s - %s\n", status.Status, status.Output)
		}
	}()

	// Continuously get the health status
	for {
		health := checker.Health()
		fmt.Printf("Current Redis Health: %s - %s\n", health.Status, health.Output)
		time.Sleep(1 * time.Second)
	}
}

```

### `NewRedisCheckerWithOpenRedisFunc` (For Advanced Usage/Testing)

This constructor allows you to inject a custom `OpenRedisFunc`, which is useful for mocking the Redis client in tests or for custom connection logic.

```go
// Example usage with a mocked Redis client
type mockRedisClient struct {
	// Implement redis.Cmdable methods
}

func (m *mockRedisClient) Ping(ctx context.Context) *redis.Cmd {
	cmd := redis.NewCmd(ctx, "PING")
	cmd.SetVal("PONG")
	return cmd
}

func (m *mockRedisClient) Close() error {
	return nil
}

func mockOpenRedisFunc(options *redis.Options) (redis.Cmdable, error) {
	return &mockRedisClient{}, nil
}

// checker := redis.NewRedisCheckerWithOpenRedisFunc(descriptor, 5*time.Second, 1*time.Second, redisOptions, mockOpenRedisFunc)
```

## Configuration Options

- **`descriptor core.Descriptor`**: Metadata about the component (ID, type, description).
- **`checkInterval time.Duration`**: How often the health check should be performed (e.g., `5*time.Second`).
- **`pingTimeout time.Duration`**: The maximum time to wait for a `PING` command response (e.g., `1*time.Second`).
- **`options *redis.Options`**: Configuration options for the `github.com/redis/go-redis/v9` client (e.g., address, password, DB).

## Performance (SLA)

The `Health()` method of the Redis checker is designed for high-concurrency reads of the last known status. It performs very fast, typically supporting well over **200,000 calls per second** when using a mock client (which simulates a fast read of an in-memory status). Actual performance when interacting with a real Redis instance will depend on network latency and Redis server load, but the `Health()` method itself introduces minimal overhead.

## CGO Support

This library uses `github.com/redis/go-redis/v9` which is a pure Go client and **does NOT require CGO**.
