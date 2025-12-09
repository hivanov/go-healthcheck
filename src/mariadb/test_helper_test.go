package mariadb

import (
	"context"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mariadb"
)

// setupMariaDBContainer starts a MariaDB container and returns its connection string.
// It also returns a cleanup function to stop the container.
func setupMariaDBContainer(tb testing.TB, ctx context.Context) (testcontainers.Container, string, func()) {
	// Create a context with a timeout for container startup
	startupCtx, startupCancel := context.WithTimeout(ctx, 2*time.Minute) // Increased timeout for container startup
	defer startupCancel()

	mariadbContainer, err := mariadb.Run(startupCtx, // Use startupCtx
		"mariadb:10.6", // Using MariaDB 10.6
		mariadb.WithDatabase("testdb"),
		mariadb.WithUsername("testuser"),
		mariadb.WithPassword("testpass"),
	)
	if err != nil {
		tb.Fatalf("Failed to start MariaDB container: %v", err) // Use tb
	}

	assert.NotNil(tb, mariadbContainer, "MariaDB container is nil") // Use tb

	connStr, err := mariadbContainer.ConnectionString(ctx, "tls=false") // Use passed ctx, tls=false for local testing
	if err != nil && mariadbContainer != nil {
		_ = mariadbContainer.Terminate(ctx)                   // Use passed ctx
		tb.Fatalf("Failed to get connection string: %v", err) // Use tb
	}

	return mariadbContainer, connStr, func() {
		if err := mariadbContainer.Terminate(ctx); err != nil { // Use passed ctx
			tb.Logf("Failed to terminate MariaDB container: %v", err) // Use tb
		}
	}
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(tb testing.TB, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout) // Use context.Background()
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
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, checker.Status().Status, checker.Status().Output) // Use tb
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
		case <-time.After(5 * time.Millisecond): // Small delay to avoid busy-waiting for initial status
			currentStatus = checker.Status()
			if currentStatus.Status == expectedStatus {
				return
			}
		}
	}
}
