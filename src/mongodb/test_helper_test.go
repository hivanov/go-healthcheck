package mongodb

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/mongo/readpref" // Added for readpref.ReadPref
)

// mockMongoConnection is a mock implementation of mongoConnection for testing purposes.
type mockMongoConnection struct {
	pingFunc       func(ctx context.Context, rp *readpref.ReadPref) error
	disconnectFunc func(ctx context.Context) error
}

func (m *mockMongoConnection) Ping(ctx context.Context, rp *readpref.ReadPref) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx, rp)
	}
	return nil
}

func (m *mockMongoConnection) Disconnect(ctx context.Context) error {
	if m.disconnectFunc != nil {
		return m.disconnectFunc(ctx)
	}
	return nil
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(tb testing.TB, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusChangeChan := checker.StatusChange()

	// Check current status first
	if checker.Status().Status == expectedStatus {
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, checker.Status().Status, checker.Status().Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
		case <-time.After(5 * time.Millisecond):
			if checker.Status().Status == expectedStatus {
				return
			}
		}
	}
}

// setupMongoContainer starts a MongoDB container and returns its connection string.
func setupMongoContainer(tb testing.TB, ctx context.Context) (testcontainers.Container, string, func()) {
	// Create a context with a timeout for container startup
	startupCtx, startupCancel := context.WithTimeout(ctx, 2*time.Minute) // Increased timeout for container startup
	defer startupCancel()

	req := testcontainers.ContainerRequest{
		Image:        "mongo:6",
		ExposedPorts: []string{"27017/tcp"},
		WaitingFor:   wait.ForListeningPort("27017/tcp"),
	}
	mongoContainer, err := testcontainers.GenericContainer(startupCtx, testcontainers.GenericContainerRequest{ // Use startupCtx
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		tb.Fatalf("Failed to start MongoDB container: %v", err) // Use tb
	}

	assert.NotNil(tb, mongoContainer, "MongoDB container is nil") // Use tb

	host, err := mongoContainer.Host(ctx) // Use passed ctx
	if err != nil {
		tb.Fatalf("Failed to get container host: %v", err) // Use tb
	}
	port, err := mongoContainer.MappedPort(ctx, "27017") // Use passed ctx
	if err != nil {
		tb.Fatalf("Failed to get container port: %v", err) // Use tb
	}

	connStr := fmt.Sprintf("mongodb://%s:%s", host, port.Port())

	return mongoContainer, connStr, func() {
		if err := mongoContainer.Terminate(ctx); err != nil { // Use passed ctx
			tb.Logf("Failed to terminate MongoDB container: %v", err) // Use tb
		}
	}
}
