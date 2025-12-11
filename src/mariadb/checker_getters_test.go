package mariadb

import (
	"context"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMariaDBChecker_Getters tests the simple getter methods of the checker.
func TestMariaDBChecker_Getters(t *testing.T) {
	// We use a real container to get a valid checker instance,
	// but the focus is on the getter methods, not the DB interaction itself.
	ctx := context.Background()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{
		ComponentID:   "mariadb-getters-test",
		ComponentType: "database",
		Version:       "1.0.0",
		ReleaseID:     "v1",
	}
	checkInterval := 100 * time.Millisecond
	queryTimeout := 5 * time.Second

	checker, err := New(desc, checkInterval, queryTimeout, connStr)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// 1. Test Health() method
	// Wait for the first check to pass to get a non-warn status
	waitForStatus(t, checker, core.StatusPass, 20*time.Second)
	health := checker.Health()

	assert.Equal(t, desc.ComponentID, health.ComponentID)
	assert.Equal(t, desc.ComponentType, health.ComponentType)
	assert.Equal(t, desc.Version, health.Version)
	assert.Equal(t, desc.ReleaseID, health.ReleaseID)
	assert.Equal(t, core.StatusPass, health.Status)
	assert.Equal(t, "MariaDB is healthy", health.Output)
	assert.NotZero(t, health.Time)

	// 2. Test the no-op methods for coverage
	checker.ChangeStatus(core.ComponentStatus{Status: core.StatusFail}) // Should have no effect
	checker.Disable()                                                   // Should have no effect
	checker.Enable()                                                    // Should have no effect

	// Verify that the status wasn't actually changed by the no-op call
	status := checker.Status()
	assert.Equal(t, core.StatusPass, status.Status, "ChangeStatus should be a no-op")
}
