package postgres

import (
	"context"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// setupPostgresContainer starts a PostgreSQL container and returns its connection string.
// It also returns a cleanup function to stop the container.
func setupPostgresContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string, func()) {
	pgContainer, err := postgres.Run(t.Context(), // Use t.Context() here
		"postgres:15-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	assert.NotNil(t, pgContainer, "PostgreSQL container is nil")

	connStr, err := pgContainer.ConnectionString(t.Context(), "sslmode=disable") // Use t.Context() here
	if err != nil && pgContainer != nil {
		_ = pgContainer.Terminate(t.Context()) // Use t.Context() here
		t.Fatalf("Failed to get connection string: %v", err)
	}

	return pgContainer, connStr, func() {
		if err := pgContainer.Terminate(t.Context()); err != nil { // Use t.Context() here
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(t *testing.T, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout) // Use t.Context() here
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
			t.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, checker.Status().Status, checker.Status().Output)
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
