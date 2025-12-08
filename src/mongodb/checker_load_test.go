package mongodb

import (
	"healthcheck/core"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMongoChecker_HealthLoad_WithRealDB tests the load handling of the Health() method against a real containerized DB.
func TestMongoChecker_HealthLoad_WithRealDB(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-load-test-real-db", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMongoChecker(desc, checkInterval, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

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
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(numCalls) / duration.Seconds()

	t.Logf("Health() method (with real DB) handled %d calls in %v", numCalls, duration)
	t.Logf("Actual calls per second (with real DB): %.2f", actualCallsPerSecond)

	assert.GreaterOrEqual(t, actualCallsPerSecond, expectedCallsPerSecond, "Health() method should handle at least 200 calls per second")
}
