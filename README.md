# Go Health Check Library

This library provides a comprehensive and extensible framework for performing health checks in your Go applications. It allows you to monitor the status of various downstream services and dependencies, ensuring your application runs smoothly and reliably.

The library is designed to be modular, with each component checker living in its own Go module. This allows you to import only the checkers you need, keeping your application's binary size and dependencies to a minimum.

## Installation

To use the library, you can use `go get` to fetch the core module and any component-specific modules you need.

```bash
go get github.com/hivanov/go-healthcheck/src/core
go get github.com/hivanov/go-healthcheck/src/postgres
go get github.com/hivanov/go-healthcheck/src/redis
go get github.com/hivanov/go-healthcheck/src/builtin
```

Then, import the necessary packages into your application.

## Sample Usage

Here is an example of how to set up a health check service that monitors a PostgreSQL database, a Redis cache, and an external HTTP service.

```go
package main

import (
	"encoding/json"
	"fmt"
	"github.com/hivanov/go-healthcheck/src/builtin"
	"github.com/hivanov/go-healthcheck/src/core"
	"github.com/hivanov/go-healthcheck/src/postgres"
	"github.com/hivanov/go-healthcheck/src/redis"
	"log"
	"net/http"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
)

func main() {
	// 1. Create individual health check components.

	// --- PostgreSQL Checker ---
	// Connection string for PostgreSQL.
	pgConnStr := "user=testuser password=testpass host=localhost port=5432 dbname=testdb sslmode=disable"
	pgChecker := postgres.NewPostgresChecker(
		core.Descriptor{ComponentID: "postgres-db", ComponentType: "datastore"},
		15*time.Second, // checkInterval
		5*time.Second,  // queryTimeout
		pgConnStr,
	)

	// --- Redis Checker ---
	// Options for the Redis client.
	redisOpts, err := redisv9.ParseURL("redis://user:password@localhost:6379/0")
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	redisChecker := redis.NewRedisChecker(
		core.Descriptor{ComponentID: "redis-cache", ComponentType: "cache"},
		10*time.Second, // checkInterval
		3*time.Second,  // pingTimeout
		redisOpts,
	)

	// --- HTTP Checker ---
	httpChecker, err := builtin.NewHTTPCheckerComponent(
		builtin.HTTPCheckerOptions{
			URL:      "https://example.com",
			Interval: 20 * time.Second,
			Timeout:  5 * time.Second,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create HTTP checker: %v", err)
	}

	// 2. Create a new health check service and add components.
	service := core.NewService(
		core.Descriptor{
			ServiceID:   "my-app",
			Version:     "1.0.0",
			Description: "My Awesome Application",
		},
		pgChecker,
		redisChecker,
		httpChecker,
	)
	defer func() {
		if err := service.Close(); err != nil {
			log.Printf("Error closing health check service: %v", err)
		}
	}()

	// 3. Expose the health check endpoint.
	// The core library doesn't provide a handler out-of-the-box, so you must create one
	// that calls service.Health() and marshals the result to JSON.
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		healthStatus := service.Health()
		w.Header().Set("Content-Type", "application/health+json")
		if err := json.NewEncoder(w).Encode(healthStatus); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"status": "fail", "output": "error encoding health status: %v"}`, err)
		}
	})

	fmt.Println("Health check server listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
```

## Writing a Custom Health Check Component

You can easily extend the library by creating your own component. A component is any struct that implements the `core.Component` interface. The PostgreSQL checker (`src/postgres/checker.go`) is a great reference.

Here are the key steps:

### 1. Implement the `core.Component` Interface

Your custom checker must implement the methods defined in `core.Component`:

```go
// from src/core/component.go
type Component interface {
	io.Closer
	ChangeStatus(status ComponentStatus)
	Disable()
	Enable()
	Status() ComponentStatus
	Descriptor() Descriptor
	StatusChange() <-chan ComponentStatus
	Health() ComponentHealth
}
```

### 2. Define the Checker Struct

Your checker struct will hold the state, configuration, and dependencies for your health check.

```go
// Based on src/postgres/checker.go
type postgresChecker struct {
	checkInterval    time.Duration
	queryTimeout     time.Duration
	connectionString string
	db               dbConnection // An interface for testability
	descriptor       core.Descriptor
	currentStatus    core.ComponentStatus
	statusChangeChan chan core.ComponentStatus
	quit             chan struct{}
	mutex            sync.RWMutex
	cancelFunc       context.CancelFunc
	ctx              context.Context
	disabled         bool
}
```

Key fields include:
-   `descriptor`: Metadata about the component.
-   `currentStatus`: The latest health status, protected by a `sync.RWMutex`.
-   `statusChangeChan`: A channel to broadcast status changes.
-   `ctx` and `cancelFunc`: For managing the lifecycle and graceful shutdown of background goroutines.
-   `mutex`: To ensure thread-safe access to shared state like `currentStatus` and `disabled`.

### 3. Create a Public Factory Function

Provide a public constructor to make your component easy to use. This function should initialize the checker struct, open any necessary connections, and start the background health check loop.

```go
// Based on src/postgres/checker.go
func NewPostgresChecker(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, connectionString string) core.Component {
	// ... logic to open DB connection ...
	
	checker := &postgresChecker{
		// ... initialize fields ...
	}

	// Start the background process
	go checker.startHealthCheckLoop()

	return checker
}
```

### 4. Implement the Health Check Loop

The core logic of your checker will run in a separate goroutine. Use a `time.Ticker` to perform the check at a regular interval.

```go
// Based on src/postgres/checker.go
func (p *postgresChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	// Perform an initial check immediately
	p.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			p.performHealthCheck()
		case <-p.ctx.Done(): // Handle shutdown
			// ... clean up resources ...
			return
		}
	}
}
```

### 5. Perform the Check and Update Status

The `performHealthCheck` method contains the actual logic for checking the dependency (e.g., pinging a database, calling an API). Based on the result, it updates the component's status.

Updating the status should be done in a thread-safe way.

```go
// Based on src/postgres/checker.go
func (p *postgresChecker) performHealthCheck() {
    // ... check if disabled ...
    // ... perform the actual health check logic ...
    
    var err error
    // ...
    
	if err != nil {
		p.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: fmt.Sprintf("Health check failed: %v", err),
		})
	} else {
		p.updateStatus(core.ComponentStatus{
			Status: core.StatusPass,
			Output: "Service is healthy",
		})
	}
}

func (p *postgresChecker) updateStatus(newStatus core.ComponentStatus) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Only update and notify if the status has actually changed
	if p.currentStatus.Status != newStatus.Status || p.currentStatus.Output != newStatus.Output {
		p.currentStatus = newStatus
		// Non-blocking send to the change channel
		select {
		case p.statusChangeChan <- newStatus:
		default: 
		}
	}
}
```

By following this pattern, you can create robust, reusable, and testable health check components that integrate seamlessly with the core service.

## Available Components

The following table lists the available health check components and the technologies they monitor.

| Component | Technology Checked |
|---|---|
| [MariaDB](src/mariadb/README.md) | MariaDB |
| [MongoDB](src/mongodb/README.md) | MongoDB |
| [PostgreSQL](src/postgres/README.md) | PostgreSQL |
| [RabbitMQ](src/rabbitmq/README.md) | RabbitMQ |
| [Redis](src/redis/README.md) | Redis |
| [Amazon S3](src/s3/README.md) | Amazon S3 |
| [Solace](src/solace/README.md) | Solace PubSub+ |
| [SQL Server](src/sqlserver/README.md) | Microsoft SQL Server |
| [Vault](src/vault/README.md) | HashiCorp Vault |
| [Built-in](src/builtin/GEMINI.md) | Uptime, HTTP |