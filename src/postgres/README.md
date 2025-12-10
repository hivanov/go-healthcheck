# PostgreSQL Health Checker

This Go module provides a health checker for PostgreSQL databases, designed to be integrated into a larger health-check system. It continuously monitors the availability and responsiveness of a PostgreSQL instance by periodically executing a simple query (`SELECT 1`).

## Features

*   **Asynchronous Checks:** Health checks are performed in a separate goroutine, keeping the `Health()` method non-blocking and always returning the last known status.
*   **Status Reporting:** Provides detailed status information including current health status (Pass, Fail, Warn), output messages, and observed metrics (e.g., query duration).
*   **Dynamic Control:** Supports enabling and disabling the checker, as well as manually changing its status.
*   **Testcontainers Integration:** Integration tests leverage Testcontainers for spinning up real PostgreSQL instances, ensuring robust and realistic testing.
*   **Fuzz Testing:** Includes fuzz tests to discover unexpected behavior with varied connection string inputs.
*   **Load Testing:** Verifies the `Health()` method's performance under load, ensuring it can handle a high volume of requests efficiently.

## Installation

To use this health checker in your Go project, first ensure you have Go installed (version 1.22 or higher recommended).

```bash
go get github.com/lib/pq
go get healthcheck/postgres
```

This module depends on the `github.com/lib/pq` driver, which is automatically fetched by `go get`.

## Usage

Here's a basic example of how to initialize and use the PostgreSQL health checker:

```go
package main

import (
	"fmt"
	"healthcheck/core"
	"healthcheck/postgres"
	"log"
	"time"
)

func main() {
	// Define a descriptor for your PostgreSQL component
	descriptor := core.Descriptor{
		ComponentID:   "my-postgres-instance",
		ComponentType: "postgresql",
		ComponentName: "Production PostgreSQL",
	}

	// PostgreSQL connection string (replace with your actual connection details)
	// Example for a local PostgreSQL: "user=test password=password host=localhost port=5432 dbname=test sslmode=disable"
	connectionString := "user=testuser password=testpass host=localhost port=5432 dbname=testdb sslmode=disable" // Example for Testcontainers setup

	// Create a new PostgreSQL health checker with a check interval of 5 seconds
	checker := postgres.NewPostgresChecker(descriptor, 5*time.Second, connectionString)

	// Ensure the checker is gracefully closed when the application exits
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing PostgreSQL checker: %v", err)
		}
	}()

	// Listen for status changes (optional)
	go func() {
		for status := range checker.StatusChange() {
			fmt.Printf("PostgreSQL Status Changed: %s - %s (Latency: %.2f%s)\n",
				status.Status, status.Output, status.ObservedValue, status.ObservedUnit)
		}
	}()

	// Periodically get the health status
	for i := 0; i < 10; i++ {
		health := checker.Health()
		fmt.Printf("Current PostgreSQL Health: %s - %s\n", health.Status, health.Output)
		time.Sleep(2 * time.Second)
	}

	fmt.Println("Exiting main application.")
}
```

## Running Tests

### Unit and Integration Tests

To run all tests, including unit tests and integration tests that use Testcontainers:

```bash
go test ./...
```

**Note:** Integration tests require Docker to be running, as they spin up a real PostgreSQL container using Testcontainers.

### Fuzz Tests

To run fuzz tests, use the following command:

```bash
go test -fuzz=FuzzPostgresChecker -fuzztime=30s ./...
```

This will run the fuzz tests for 30 seconds. Adjust `fuzztime` as needed.

### Load Tests

To run load tests:

```bash
go test -run TestPostgresChecker_HealthLoad_WithRealDB -count=1 -v -test.parallel 1 ./...
```

This will run the load test against a real PostgreSQL instance spawned by Testcontainers, simulating concurrent calls to the `Health()` method.

## Configuration

The `NewPostgresChecker` function takes the following parameters:

*   `descriptor core.Descriptor`: A descriptor containing `ComponentID`, `ComponentType`, and `ComponentName`.
*   `checkInterval time.Duration`: How often the health check should ping the database.
*   `connectionString string`: The DSN (Data Source Name) for connecting to the PostgreSQL database.

## Technical Details and Performance

The `Health()` method is designed to be extremely lightweight and fast. It returns the `currentStatus` field, which is protected by a `sync.RWMutex` for concurrent read/write safety. This design ensures that calls to `Health()` do not block and provide immediate access to the latest known status.

Performance benchmarks from load tests (e.g., `TestPostgresChecker_HealthLoad_WithRealDB`) aim to verify that the `Health()` routine can handle at least 200 calls per second, even under adverse conditions such as the database temporarily going down. The actual health check (pinging the database) runs in a separate goroutine on a defined interval, ensuring the `Health()` method itself remains performant.
