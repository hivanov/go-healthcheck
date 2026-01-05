# MariaDB Health Checker

This package provides a health checker for MariaDB databases.

## Usage

Here's a basic example of how to initialize and use the MariaDB health checker:

```go
package main

import (
	"fmt"
	"github.com/hivanov/go-healthcheck/src/core"
	"github.com/hivanov/go-healthcheck/src/mariadb"
	"log"
	"time"
)

func main() {
	// Define a descriptor for your MariaDB component
	descriptor := core.Descriptor{
		ComponentID:   "my-mariadb-instance",
		ComponentType: "mariadb",
		ComponentName: "Production MariaDB",
	}

	// MariaDB connection string (replace with your actual connection details)
	connectionString := "user:password@tcp(127.0.0.1:3306)/database" // Example for local setup

	// Create a new MariaDB health checker with a check interval of 5 seconds
	checker, err := mariadb.New(descriptor, 5*time.Second, 2*time.Second, connectionString)
	if err != nil {
		log.Fatalf("Failed to create MariaDB checker: %v", err)
	}

	// Ensure the checker is gracefully closed when the application exits
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing MariaDB checker: %v", err)
		}
	}()

	// Listen for status changes (optional)
	go func() {
		for status := range checker.StatusChange() {
			fmt.Printf("MariaDB Status Changed: %s - %s (Latency: %.2f%s)\n",
				status.Status, status.Output, status.ObservedValue, status.ObservedUnit)
		}
	}()

	// Periodically get the health status
	for i := 0; i < 10; i++ {
		health := checker.Health()
		fmt.Printf("Current MariaDB Health: %s - %s\n", health.Status, health.Output)
		time.Sleep(2 * time.Second)
	}

	fmt.Println("Exiting main application.")
}
```

## Health Check Logic

The checker performs a `PING` on the database at the specified interval.
- If the `PING` is successful, the status is `core.StatusPass`.
- If the `PING` fails, the status is `core.StatusFail`.

## Integration Tests

The integration tests use [Testcontainers](https://golang.testcontainers.org/) to spin up a MariaDB container. To run the integration tests, you need to have Docker installed and running.

Then, you can run the tests with the following command:

```
TC_MARIADB_TEST=1 go test -v ./...
```