# MariaDB Health Checker

This package provides a health checker for MariaDB databases.

## Usage

To create a new MariaDB health checker, use the `New` function:

```go
package main

import (
	"fmt"
	"healthcheck/core"
	"healthcheck/mariadb"
	"time"
)

func main() {
	descriptor := core.Descriptor{
		ComponentID:   "mariadb-checker",
		ComponentType: "database",
	}

	// Create a new checker
	checker, err := mariadb.New(
		descriptor,
		10*time.Second, // check interval
		5*time.Second,  // query timeout
		"user:password@tcp(127.0.0.1:3306)/database", // connection string
	)
	if err != nil {
		fmt.Printf("Failed to create checker: %v\n", err)
		return
	}
	defer checker.Close()

	// Wait for status changes
	for status := range checker.StatusChange() {
		fmt.Printf("Status changed: %+v\n", status)
	}
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