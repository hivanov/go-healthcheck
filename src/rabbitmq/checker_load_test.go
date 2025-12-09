package rabbitmq

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This test assumes a RabbitMQ instance is running via Testcontainers.
// We'll reuse the setupRabbitMQ from checker_test.go for consistency.

// BenchmarkRabbitMQHealthCheck measures the performance of the Health() method.
func BenchmarkRabbitMQHealthCheck(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(b, err, "Failed to set up RabbitMQ container for benchmarking")
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container after benchmark: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-benchmark",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check benchmark test",
	}

	// Create a checker with a long interval so that background checks don't interfere with Health() calls
	checker := NewRabbitMQChecker(descriptor, 1*time.Hour, amqpURL)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker after benchmark: %v", err)
		}
	}()

	// Wait for an initial successful health check to ensure the connection is established
	// and the checker's internal status is stable before benchmarking.
	// We'll manually trigger a check once.
	// The checker's internal loop is running with 1 hour interval, so we need to
	// explicitly trigger the check or wait for its internal state to stabilize.
	// For benchmarking Health(), we primarily test the speed of Status() and Health()
	// which are mutex-protected reads. The performHealthCheck() is more for async checks.

	// To ensure the checker is initialized and has a valid status,
	// we'll wait for the initial status to be updated.
	// NewRabbitMQCheckerInternal calls startHealthCheckLoop which performs an initial check.
	// We'll wait for the first non-initial status.
	statusChan := checker.StatusChange()
	select {
	case status := <-statusChan:
		if status.Status != core.StatusPass {
			b.Fatalf("Initial health check did not pass, status: %v", status)
		}
	case <-time.After(10 * time.Second): // Give it some time
		b.Fatal("Timed out waiting for initial health check to pass")
	}

	b.ResetTimer() // Reset timer to exclude setup time

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = checker.Health()
		}
	})

	// Assert that the number of operations per second is at least 200
	opsPerSecond := float64(b.N) / b.Elapsed().Seconds()
	fmt.Printf("RabbitMQ Health() operations per second: %f\n", opsPerSecond)
	require.GreaterOrEqual(b, opsPerSecond, 200.0, "Expected at least 200 operations per second for Health()")
}
