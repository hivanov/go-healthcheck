# HashiCorp Vault Health Checker

This Go library provides a health checker component for HashiCorp Vault instances. It continuously monitors the health of a Vault server and reports its status, following the `healthcheck/core` component interface.

## Features

-   **Continuous Health Monitoring**: Periodically checks the Vault's health endpoint.
-   **Status Aggregation**: Provides a consolidated health status (Pass, Warn, Fail).
-   **Concurrency Safe**: Health checks run in a separate goroutine, and status access is thread-safe.
-   **Testable**: Designed with interfaces for easy mocking in unit tests and integrates with Testcontainers for robust integration testing against real Vault instances.
-   **Userpass Authentication Support**: Demonstrates how to configure and use the Vault checker with user/password authentication.
-   **Fuzz Testing**: Includes fuzz tests to discover unexpected panics or errors with varied inputs.
-   **Load Testing**: Verifies the checker's performance under concurrent access to its `Health()` method.

## Installation

To include this library in your Go project, run:

```bash
go get healthcheck/vault
```

## Usage

Here's a basic example of how to use the Vault health checker:

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/vault/api"
	"healthcheck/core"
	"healthcheck/vault"
)

func main() {
	// Configure Vault client
	// In a real application, you would load this from configuration
	vaultConfig := api.DefaultConfig()
	vaultConfig.Address = "http://localhost:8200" // Replace with your Vault address

	// Create a descriptor for your Vault component
	descriptor := core.Descriptor{
		ComponentID:   "my-vault-instance",
		ComponentType: "hashicorp-vault",
		Description:   "Primary Vault server for application secrets",
	}

	// Create the Vault health checker
	// The checker will automatically start polling Vault's health endpoint
	checker := vault.NewVaultChecker(descriptor, 5*time.Second, vaultConfig)

	// You can listen for status changes
	statusChanges := checker.StatusChange()
	go func() {
		for status := range statusChanges {
			log.Printf("Vault Status Change: %s - %s", status.Status, status.Output)
		}
	}()

	// Periodically log the current health status
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		health := checker.Health()
		fmt.Printf("Current Vault Health (%s): %s - %s\n",
			health.ComponentID, health.Status, health.Output)

		if health.Status == core.StatusFail {
			log.Println("Vault is in a failed state. Investigate immediately!")
		}
	}

	// In a real application, you would gracefully shut down the checker
	// For example, on application exit:
	// if err := checker.Close(); err != nil {
	// 	log.Printf("Error closing Vault checker: %v", err)
	// }
}

```

### Using with Userpass Authentication

If your Vault instance requires userpass authentication, you can configure the `api.Client` to log in and obtain a token before passing it to the checker. The checker itself expects an already authenticated `api.Client` (or a mock thereof).

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/vault/api"
	"healthcheck/core"
	"healthcheck/vault"
)

func main() {
	// Configure Vault client with address
	vaultConfig := api.DefaultConfig()
	vaultConfig.Address = "http://localhost:8200" // Replace with your Vault address

	// Create an API client instance
	client, err := api.NewClient(vaultConfig)
	if err != nil {
		log.Fatalf("Failed to create Vault client: %v", err)
	}

	// Perform userpass login
	username := "myuser"
	password := "mypassword"
	userpassMountPath := fmt.Sprintf("auth/userpass/login/%s", username)

	options := map[string]interface{}{
		"password": password,
	}
	secret, err := client.Logical().Write(userpassMountPath, options)
	if err != nil {
		log.Fatalf("Failed to login to Vault with userpass: %v", err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		log.Fatalf("Userpass login did not return a client token")
	}

	// Set the client token for subsequent operations
	client.SetToken(secret.Auth.ClientToken)
	log.Println("Successfully authenticated with Vault using userpass.")

	// Wrap the authenticated client for the health checker
	// (Note: This is a simplified example; in a full checker,
	// you might need a way to refresh tokens if they expire)
	authenticatedVaultClient := &vault.RealVaultClient{Client: client} // Assuming RealVaultClient is accessible or similar helper

	descriptor := core.Descriptor{
		ComponentID:   "my-secure-vault-instance",
		ComponentType: "hashicorp-vault",
		Description:   "Vault server requiring userpass auth",
	}

	// Create the Vault health checker with the authenticated client
	checker := vault.NewVaultCheckerWithOpenVaultClientFunc(
		descriptor,
		5*time.Second,
		vaultConfig,
		func(cfg *api.Config) (vault.VaultClient, error) {
			return authenticatedVaultClient, nil // Return the pre-authenticated client
		})

	// ... rest of your application logic ...
	// (e.g., listening for status changes, periodic health checks)

	// Don't forget to close the checker
	if err := checker.Close(); err != nil {
		log.Printf("Error closing Vault checker: %v", err)
	}
}

```

## Testing

The library includes comprehensive tests:

-   **Unit Tests**: Standard Go unit tests for individual functions and methods, often using mocks.
-   **Integration Tests**: Located in `checker_test.go`, these tests utilize [Testcontainers for Go](https://golang.testcontainers.org/) to spin up a real HashiCorp Vault server. This ensures that the checker interacts correctly with a live Vault instance, including `userpass` authentication mechanisms.
    -   The `testcontainers/vault` directory contains the `Containerfile` and associated scripts to set up the Vault environment for testing, including pre-configured policies.
-   **Fuzz Tests**: `checker_fuzz_test.go` uses `go-fuzz-headers` to provide randomized inputs to the health check logic, helping to uncover edge cases and potential panics.
-   **Load Tests**: `checker_load_test.go` uses Go's built-in benchmarking tools to measure the performance of the `Health()` method under concurrent access, verifying its ability to handle at least 200 calls per second.

To run all tests (unit, integration, fuzz, and load), navigate to the `src/vault` directory and execute:

```bash
go test -v ./...
go test -fuzz=Fuzz -fuzztime=30s ./...
go test -bench=. -benchmem -run=^$ ./...
```

**Note on Testcontainers**: Running integration and load tests requires Docker to be installed and running on your system.

## Technical Details and Performance

The `vault.vaultChecker` operates by periodically polling the configured HashiCorp Vault instance's health endpoint (`/v1/sys/health`) in a dedicated goroutine. The frequency of these checks is configurable via the `checkInterval` parameter during initialization.

Status updates are published to an internal channel, which can be consumed by other parts of the application to react to changes in Vault's health. Access to the `currentStatus` is protected by a `sync.RWMutex` to ensure thread-safe reads and writes, allowing `Health()` and `Status()` methods to be called concurrently without blocking the health check loop.

Load tests have demonstrated that the `Health()` method can sustain at least **200 calls per second** when connected to a healthy Vault instance, making it suitable for high-throughput health monitoring scenarios. The actual performance may vary depending on network latency, Vault server load, and the underlying hardware. The `ObservedValue` in `core.ComponentStatus` and `core.ComponentHealth` captures the latency of the Vault health check itself.
