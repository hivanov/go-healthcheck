# Solace Health Checker

This library provides a health checker component for Solace PubSub+, integrating with the `github.com/hivanov/go-healthcheck/src/core` package. It uses the `github.com/SolaceLabs/solace-amqp-go` client library for Go.

## Health Check Details

The Solace health checker performs the following operations to determine the health and message flow capability of the Solace PubSub+ broker:

-   **Connection Establishment**: Attempts to establish an AMQP connection to the Solace broker using the provided connection string.
-   **Session Creation**: Creates a new session on the established connection.
-   **Sender/Receiver Creation**: Creates a temporary sender and receiver for a test topic.
-   **Message Send/Receive**: Sends a test message and then attempts to receive it within a defined timeout. This verifies end-to-end message flow.

## How to Use

The `solace` package provides functions to create a new Solace health checker component.

### `NewSolaceChecker`

This is the primary constructor for creating a Solace health checker.

```go
package main

import (
	"context"
	"fmt"
	"github.com/hivanov/go-healthcheck/src/core"
	"github.com/hivanov/go-healthcheck/src/solace"
	"time"
)

func main() {
	descriptor := core.Descriptor{
		ComponentID:   "my-solace-broker",
		ComponentType: "messaging",
		Description:   "Health check for the Solace PubSub+ broker.",
	}

	connectionString := "amqp://admin:admin@localhost:5672" // Replace with your Solace broker connection string

	// Create a new Solace checker component
	// The checker will perform operations every 5 seconds with a 5-second operation timeout.
	checker := solace.NewSolaceChecker(descriptor, 5*time.Second, 5*time.Second, connectionString)
	defer checker.Close()

	// You can subscribe to status changes
	statusChanges := checker.StatusChange()
	go func() {
		for status := range statusChanges {
			fmt.Printf("Solace status changed: %s - %s\n", status.Status, status.Output)
		}
	}()

	// Continuously get the health status
	for {
		health := checker.Health()
		fmt.Printf("Current Solace Health: %s - %s\n", health.Status, health.Output)
		time.Sleep(1 * time.Second)
	}
}

```

### `NewSolaceCheckerWithOpenSolaceFunc` (For Advanced Usage/Testing)

This constructor allows you to inject a custom `OpenSolaceFunc`, which is useful for mocking the Solace client in tests or for custom connection logic.

```go
// Example usage with a mocked Solace client
import (
	"context"
	"fmt"
	"github.com/hivanov/go-healthcheck/src/solace"
	"github.com/SolaceLabs/solace-amqp-go/amqp"
)

type mockSolaceConnection struct { /* ... */ }
func (m *mockSolaceConnection) Open() error { /* ... */ return nil }
func (m *mockSolaceConnection) Close() error { /* ... */ return nil }
func (m *mockSolaceSession) NewSender(linkName string) (solace.solaceSender, error) { /* ... */ return &mockSolaceSender{}, nil }
func (m *mockSolaceSession) NewReceiver(linkName string) (solace.solaceReceiver, error) { /* ... */ return &mockSolaceReceiver{}, nil }
func (m *mockSolaceSession) Close() error { /* ... */ return nil }

type mockSolaceSender struct { /* ... */ }
func (m *mockSolaceSender) Send(msg amqp.Message, ctx context.Context) error { /* ... */ return nil }
func (m *mockSolaceSender) Close() error { /* ... */ return nil }

type mockSolaceReceiver struct { /* ... */ }
func (m *mockSolaceReceiver) Receive(ctx context.Context) (amqp.Message, error) { /* ... */ return amqp.NewMessageWithText("mock"), nil }
func (m *mockSolaceReceiver) Close() error { /* ... */ return nil }


func mockOpenSolaceFunc(connectionString string) (solace.solaceConnection, error) {
	return &mockSolaceConnection{}, nil
}

// checker := solace.NewSolaceCheckerWithOpenSolaceFunc(descriptor, 5*time.Second, 1*time.Second, connectionString, mockOpenSolaceFunc)
```

## Configuration Options

-   **`descriptor core.Descriptor`**: Metadata about the component (ID, type, description).
-   **`checkInterval time.Duration`**: How often the health check should be performed (e.g., `5*time.Second`).
-   **`operationsTimeout time.Duration`**: The maximum time to wait for individual Solace operations (e.g., session creation, message send/receive) (e.g., `5*time.Second`).
-   **`connectionString string`**: The AMQP connection string for the Solace broker (e.g., `amqp://admin:admin@localhost:5672`).

## Performance (SLA)

The `Health()` method of the Solace checker is designed for high-concurrency reads of the last known status. It performs very fast, typically supporting well over **200,000 calls per second** when using a mock client (which simulates a fast read of an in-memory status). Actual performance when interacting with a real Solace broker will depend on network latency, broker load, and the complexity of the test message flow. The `Health()` method itself introduces minimal overhead.

## CGO Support

This library uses `github.com/SolaceLabs/solace-amqp-go` which is a pure Go client and **does NOT require CGO**.
