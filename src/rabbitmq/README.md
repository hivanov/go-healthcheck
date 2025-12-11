# RabbitMQ Health Checker

This module provides a health checker component for RabbitMQ, designed to be integrated into a larger health-check system. It continuously monitors the connection to a RabbitMQ instance and performs a test by opening a channel and publishing a simple message.

## Features

- **Connection Monitoring**: Periodically checks the connectivity to the RabbitMQ server.
- **Message Publishing Test**: Verifies the ability to open a channel and publish messages, ensuring basic functionality.
- **Status Reporting**: Provides real-time health status, including detailed output and observed metrics.
- **Integration with `go-healthcheck/core`**: Implements the `core.Component` interface for seamless integration.
- **Testcontainers Support**: Integration tests leverage Testcontainers for spinning up a real RabbitMQ instance.
- **Fuzz Testing**: Includes fuzz tests to ensure robust handling of various input parameters.
- **Load Testing**: Benchmarks the `Health()` method to verify performance under load.

## Getting Started

### Prerequisites

- Go (version 1.25.5 or higher)
- Docker (for running integration and load tests with Testcontainers)

### Installation

To add this module to your project, use `go get`:

```bash
go get healthcheck/rabbitmq
```

Or if you are working within the `go-work` workspace:

```bash
go mod tidy
go work sync
```

### Usage

Here's a basic example of how to use the RabbitMQ health checker:

```go
import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"time"
)

func main() {
	descriptor := core.Descriptor{
		ComponentID:   "my-rabbitmq-instance",
		ComponentType: "rabbitmq",
		Description:   "Primary RabbitMQ broker for application messages",
	}

	// Replace with your RabbitMQ connection string
	connectionString := "amqp://guest:guest@localhost:5672/"

	// Create a new RabbitMQ checker that checks every 5 seconds with a 1-second operation timeout
	checker := rabbitmq.NewRabbitMQChecker(descriptor, 5*time.Second, 1*time.Second, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing RabbitMQ checker: %v", err)
		}
	}()

	// Listen for status changes
	statusChanges := checker.StatusChange()
	go func() {
		for status := range statusChanges {
			fmt.Printf("RabbitMQ Status Changed: %s - %s\n", status.Status, status.Output)
		}
	}()

	// Periodically get the current status
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for i := 0; i < 10; i++ {
		<-ticker.C
		currentStatus := checker.Status()
		healthReport := checker.Health()
		fmt.Printf("Current RabbitMQ Status: %s - %s (Health Report: %s)\n", currentStatus.Status, currentStatus.Output, healthReport.Status)
	}

	// Example of disabling and enabling the checker
	fmt.Println("Disabling RabbitMQ checker...")
	checker.Disable()
	time.Sleep(3 * time.Second)
	fmt.Println("Enabling RabbitMQ checker...")
	checker.Enable()
	time.Sleep(3 * time.Second)
}
```

### Running Tests

#### Unit Tests

Unit tests are integrated within the `checker.go` and `checker_test.go` files (though most logic is covered by integration tests).

```bash
go test healthcheck/rabbitmq
```

#### Integration Tests

Integration tests require a running Docker daemon as they utilize Testcontainers to spin up a temporary RabbitMQ instance.

```bash
go test -tags integration healthcheck/rabbitmq
```

To run a specific integration test:

```bash
go test -tags integration healthcheck/rabbitmq -run TestRabbitMQHealthCheck_Pass
```

#### Fuzz Tests

Fuzz tests are designed to find unexpected behavior by providing randomized inputs.

```bash
go test -fuzz=FuzzNewRabbitMQChecker healthcheck/rabbitmq
```

#### Load Tests

Load tests verify the performance of the `Health()` routine under a specified load. These also require a running Docker daemon.

```bash
go test -bench=. -benchtime=5s -run=^$ healthcheck/rabbitmq
```
*(Note: Adjust `-benchtime` as needed for longer or shorter test runs. The output will show operations per second.)*

## Contributing

Please report any issues or submit pull requests.

## License

This project is licensed under the [LICENSE Name] - see the [LICENSE.md](LICENSE.md) file for details.
