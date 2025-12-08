package postgres

import (
	"context"
	"healthcheck/core"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestPostgresChecker_HealthLoad tests the load handling of the Health() method.
func TestPostgresChecker_HealthLoad(t *testing.T) {
	// Create a mock dbConnection that does nothing
	mockDB := &mockDBConnection{
		queryRowContextFunc: func(ctx context.Context, query string, args ...interface{}) rowScanner {
			return &mockRowScanner{
				scanFunc: func(dest ...interface{}) error {
					if len(dest) > 0 {
						if ptr, ok := dest[0].(*int); ok {
							*ptr = 1
						}
					}
					return nil
				},
			}
		},
		closeFunc: func() error {
			return nil
		},
	}

	desc := core.Descriptor{ComponentID: "pg-load-test", ComponentType: "postgres"}
	checkInterval := 1 * time.Second // A longer interval as we are not testing the check loop itself

	// Use the internal constructor with the mock DB
	checker := newPostgresCheckerInternal(desc, checkInterval, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Load test parameters
	numCalls := 2000
	concurrency := 50
	expectedCallsPerSecond := 200.0

	var wg sync.WaitGroup
	wg.Add(concurrency)

	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numCalls/concurrency; j++ {
				_ = checker.Health()
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(numCalls) / duration.Seconds()

	t.Logf("Health() method handled %d calls in %v", numCalls, duration)
	t.Logf("Actual calls per second: %.2f", actualCallsPerSecond)

	assert.GreaterOrEqual(t, actualCallsPerSecond, expectedCallsPerSecond, "Health() method should handle at least 200 calls per second")
}

// TestPostgresChecker_HealthLoad_WithRealDB tests the load handling of the Health() method against a real containerized DB.
// It also verifies that Health() continues to perform well even when the database goes down.
func TestPostgresChecker_HealthLoad_WithRealDB(t *testing.T) {
	ctx := t.Context()
	pgContainer, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-load-test-real-db", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for the checker to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Start a goroutine to stop the container after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		t.Log("Stopping PostgreSQL container during load test...")
		if err := pgContainer.Stop(ctx, nil); err != nil {
			t.Logf("Failed to stop PostgreSQL container: %v", err)
		}
		t.Log("PostgreSQL container stopped during load test.")
	}()

	// Load test parameters
	numCalls := 2000
	concurrency := 50
	expectedCallsPerSecond := 200.0

	var wg sync.WaitGroup
	wg.Add(concurrency)

	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numCalls/concurrency; j++ {
				_ = checker.Health()
				time.Sleep(1 * time.Millisecond) // Small sleep to avoid overwhelming the CPU and scheduler
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(numCalls) / duration.Seconds()

	t.Logf("Health() method (with real DB) handled %d calls in %v", numCalls, duration)
	t.Logf("Actual calls per second (with real DB): %.2f", actualCallsPerSecond)

	assert.GreaterOrEqual(t, actualCallsPerSecond, expectedCallsPerSecond, "Health() method should handle at least 200 calls per second even when DB goes down")

	// Final check: the checker should be in a Fail state
	waitForStatus(t, checker, core.StatusFail, 5*time.Second)
}