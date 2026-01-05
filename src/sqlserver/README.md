# Microsoft SQL Server Health Checker

This library provides a health checker component for Microsoft SQL Server databases, allowing you to monitor the availability and responsiveness of your SQL Server instance within your application's health-check system.

## Features

- **Connection Monitoring**: Continuously pings the SQL Server database to ensure connectivity.
- **Configurable Intervals**: Allows setting custom check intervals and query timeouts.
- **Status Reporting**: Provides detailed status information, including `pass`, `fail`, or `warn`, along with output messages and observed values (e.g., query response time).
- **Graceful Shutdown**: Properly closes database connections and stops background goroutines when the component is closed.
- **Integration with `github.com/hivanov/go-healthcheck/src/core`**: Implements the `core.Component` interface, making it pluggable into the main health-check framework.

## Installation

To use this component, you need to import it into your Go project:

```bash
go get github.com/hivanov/go-healthcheck/src/sqlserver
go get github.com/microsoft/go-mssqldb
```

## Usage

Here's a basic example of how to initialize and use the Microsoft SQL Server health checker:

```go
package main

import (
	"context"
	"fmt"
	"github.com/hivanov/go-healthcheck/src/core"
	"github.com/hivanov/go-healthcheck/src/sqlserver"
	"log"
	time "time"
)

func main() {
	// Replace with your actual SQL Server connection string
	// Example for local SQL Server: "sqlserver://user:password@localhost:1433?database=master&encrypt=disable"
	connectionString := "sqlserver://sa:Strong_Password_123!@localhost:1433?database=master&encrypt=disable"

	descriptor := core.Descriptor{
		ComponentID:   "sqlserver-db-01",
		ComponentType: "datastore",
		Description:   "Primary Microsoft SQL Server Database",
		// You can add more fields like Version, ReleaseID, Links, etc.
	}

	// Create the SQL Server health checker component
	// Checks every 5 seconds, with a query timeout of 2 seconds
	sqlServerChecker := sqlserver.NewSQLServerChecker(descriptor, 5*time.Second, 2*time.Second, connectionString)

	// Ensure the checker is closed when the application exits
	defer func() {
		if err := sqlServerChecker.Close(); err != nil {
			log.Printf("Error closing SQL Server checker: %v", err)
		}
	}()

	// Listen for status changes
	go func() {
		for status := range sqlServerChecker.StatusChange() {
			fmt.Printf("SQL Server Status Change: %s - %s (Value: %.2f%s)\n",
				status.Status, status.Output, status.ObservedValue, status.ObservedUnit)
		}
	}()

	// Periodically get the current health status
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		health := sqlServerChecker.Health()
		fmt.Printf("[%s] Overall SQL Server Health: %s - %s\n",
			time.Now().Format("15:04:05"), health.Status, health.Output)

		// Example of disabling and enabling
		if time.Now().Second()%20 == 0 {
			if sqlServerChecker.Status().Status != core.StatusWarn || sqlServerChecker.Status().Output != "Microsoft SQL Server checker disabled" {
				fmt.Println("Disabling SQL Server checker...")
				sqlServerChecker.Disable()
			}
		} else if time.Now().Second()%25 == 0 {
			if sqlServerChecker.Status().Output == "Microsoft SQL Server checker disabled" {
				fmt.Println("Enabling SQL Server checker...")
				sqlServerChecker.Enable()
			}
		}
	}
}

// Helper function to simulate waiting for a status (for example purposes)
func waitForStatus(checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Timed out waiting for status '%s'", expectedStatus)
			return
		case <-time.After(100 * time.Millisecond):
			if checker.Status().Status == expectedStatus {
				log.Printf("Status is now '%s'", expectedStatus)
				return
			}
		}
	}
}
```

## Configuration

The `NewSQLServerChecker` function accepts the following parameters:

- `descriptor`: A `core.Descriptor` struct containing metadata about the component (ID, type, description, etc.).
- `checkInterval`: The duration between consecutive health checks (e.g., `5*time.Second`).
- `queryTimeout`: The maximum time to wait for a single health check query to complete (e.g., `2*time.Second`).
- `connectionString`: The full connection string to your Microsoft SQL Server database.

### Connection String Format

The connection string for `github.com/microsoft/go-mssqldb` typically follows this format:

`sqlserver://username:password@host:port?database=dbname&encrypt=disable`

Example: `sqlserver://sa:MyStrongPassword!@localhost:1433?database=master&encrypt=disable`

**Important Note on ODBC Drivers**:
Depending on your environment and the specific SQL Server setup, you might need to ensure the appropriate ODBC drivers are installed on the system where your application is running. The `go-mssqldb` driver often relies on `libodbc` for communication. Refer to the `go-mssqldb` documentation for more details on driver dependencies if you encounter connection issues.

## SLA Data

- **Supported Calls Per Second (CPS)**: The component performs health checks at the `checkInterval` you configure. Therefore, the CPS is `1 / checkInterval`. For example, a `checkInterval` of `1*time.Second` results in 1 CPS. The `Health()` method itself is thread-safe and designed for high concurrency, allowing for many reads per second without significant overhead.
- **Performance**: Each health check involves a simple `SELECT 1` query. The observed value in the status (`ObservedValue`) represents the duration of this query. Aim for query timeouts that reflect your performance expectations (e.g., 500ms to 2s).

## Error Handling

All errors arising from spawned goroutines (e.g., during database connection attempts or query execution) are captured and reflected in the `ComponentStatus` output. The component is designed to be resilient, continuously attempting to re-establish a healthy status even after failures.

## Testcontainers Setup

For integration testing, you can use Testcontainers with the `mssql` module to spin up a temporary SQL Server instance. An example setup is provided in `checker_test.go` and `test_helper.go` within this package.

To run integration tests, set the environment variable `TC_SQLSERVER_TEST=1` before running `go test`:

```bash
TC_SQLSERVER_TEST=1 go test -v ./...
```
