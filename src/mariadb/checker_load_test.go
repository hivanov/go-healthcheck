package mariadb

import (
	"context"
	"healthcheck/core"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	minCallsPerSecond = 200
	testDuration      = 2 * time.Second
	testConcurrency   = 50
)

// TestMariaDBChecker_HealthLoad tests the load handling of the Health() method with a mock DB.
func TestMariaDBChecker_HealthLoad(t *testing.T) {
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

	desc := core.Descriptor{ComponentID: "mariadb-load-test", ComponentType: "mariadb"}
	checkInterval := 1 * time.Second

	checker := newMariaDBCheckerInternal(desc, checkInterval, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	totalCalls := 0
	var wg sync.WaitGroup
	wg.Add(testConcurrency)

	startTime := time.Now()

	for i := 0; i < testConcurrency; i++ {
		go func() {
			defer wg.Done()
			for time.Since(startTime) < testDuration {
				_ = checker.Health()
				totalCalls++
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(totalCalls) / duration.Seconds()

	t.Logf("Health() method handled %d calls in %v", totalCalls, duration)
	t.Logf("Actual calls per second: %.2f", actualCallsPerSecond)

	assert.GreaterOrEqual(t, actualCallsPerSecond, float64(minCallsPerSecond), "Health() method should handle at least %d calls per second", minCallsPerSecond)
}

// TestMariaDBChecker_HealthLoad_WithRealDB tests the load handling of the Health() method against a real containerized DB.
// It also verifies that Health() continues to perform well even when the database goes down.
func TestMariaDBChecker_HealthLoad_WithRealDB(t *testing.T) {
	ctx := t.Context()
	mariadbContainer, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-load-test-real-db", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for the checker to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	var stopWg sync.WaitGroup
	stopWg.Add(1)
	// Start a goroutine to stop the container after a short delay
	go func() {
		defer stopWg.Done()
		time.Sleep(testDuration / 2) // Stop container halfway through the test duration
		t.Log("Stopping MariaDB container during load test...")
		if err := mariadbContainer.Stop(ctx, nil); err != nil {
			t.Logf("Failed to stop MariaDB container: %v", err)
		}
		t.Log("MariaDB container stopped during load test.")
	}()

	totalCalls := 0
	var wg sync.WaitGroup
	wg.Add(testConcurrency)

	startTime := time.Now()

	for i := 0; i < testConcurrency; i++ {
		go func() {
			defer wg.Done()
			for time.Since(startTime) < testDuration {
				_ = checker.Health()
				totalCalls++
			}
		}()
	}

	wg.Wait()
	stopWg.Wait() // Ensure container stop goroutine finishes

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(totalCalls) / duration.Seconds()

	t.Logf("Health() method (with real DB) handled %d calls in %v", totalCalls, duration)
	t.Logf("Actual calls per second (with real DB): %.2f", actualCallsPerSecond)

	assert.GreaterOrEqual(t, actualCallsPerSecond, float64(minCallsPerSecond), "Health() method should handle at least %d calls per second even when DB goes down", minCallsPerSecond)

	// Final check: the checker should be in a Fail state
	waitForStatus(t, checker, core.StatusFail, 5*time.Second)
}
