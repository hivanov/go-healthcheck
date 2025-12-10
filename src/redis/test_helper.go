package redis

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/go-redis/redis/v8" // Using v8 for redis
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
)

// mockRedisConnection implements redisConnection for testing purposes.
type mockRedisConnection struct {
	pingFunc  func(ctx context.Context) *redis.StatusCmd
	closeFunc func() error
}

func (m *mockRedisConnection) Ping(ctx context.Context) *redis.StatusCmd {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return redis.NewStatusCmd(ctx) // Default: return a successful status command
}

func (m *mockRedisConnection) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil // Default close behavior
}

// setupRedisContainer starts a Redis container and returns its connection options.
// It also returns a cleanup function to stop the container.
func setupRedisContainer(tb testing.TB, ctx context.Context) (testcontainers.Container, *redis.Options, func()) {
	// Create a context with a timeout for container startup
	startupCtx, startupCancel := context.WithTimeout(ctx, 2*time.Minute) // Increased timeout for container startup
	defer startupCancel()

	redisContainer, err := redis.Run(startupCtx,
		"redis:latest",
	)
	if err != nil {
		tb.Fatalf("Failed to start Redis container: %v", err)
	}

	require.NotNil(tb, redisContainer, "Redis container is nil")

	host, err := redisContainer.Host(ctx)
	if err != nil {
		tb.Fatalf("Failed to get container host: %v", err)
	}
	port, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		tb.Fatalf("Failed to get container port: %v", err)
	}

	// Connection string format for go-redis: host:port
	redisOptions := &redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	}

	return redisContainer, redisOptions, func() {
		if err := redisContainer.Terminate(ctx); err != nil {
			tb.Logf("Failed to terminate Redis container: %v", err)
		}
	}
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(tb testing.TB, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusChangeChan := checker.StatusChange()

	// Check current status first
	currentStatus := checker.Status()
	if currentStatus.Status == expectedStatus {
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, currentStatus.Status, currentStatus.Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
			currentStatus = newStatus // Update current status to reflect the latest received
		case <-time.After(5 * time.Millisecond): // Small delay to avoid busy-waiting for initial status
			currentStatus = checker.Status()
			if currentStatus.Status == expectedStatus {
				return
			}
		}
	}
}
