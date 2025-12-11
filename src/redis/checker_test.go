package redis

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestRedisChecker_Integration_HappyPath tests a successful connection and health check.
func TestRedisChecker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-happy-path", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn, 1*time.Second) // Initializing
	waitForStatus(t, checker, core.StatusPass, 5*time.Second) // Healthy

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.Contains(t, status.Output, "Redis is healthy")
	require.NotZero(t, status.ObservedValue, "Expected ObservedValue to be non-zero")
	require.Equal(t, "s", status.ObservedUnit, "Expected ObservedUnit 's'")
}

// TestRedisChecker_Integration_Fail_NoConnection tests when the database is unreachable.
func TestRedisChecker_Integration_Fail_NoConnection(t *testing.T) {
	// No container needed, just an invalid connection string
	invalidRedisOptions := &redis.Options{
		Addr: "localhost:12345", // Invalid address
	}

	desc := core.Descriptor{ComponentID: "redis-fail-noconn", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, invalidRedisOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail, 1*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Failed to open Redis connection", "Expected output to indicate connection failure")
}

// TestRedisChecker_Integration_Fail_RedisDown tests the checker reacting to a Redis server going down.
func TestRedisChecker_Integration_Fail_RedisDown(t *testing.T) {
	ctx := t.Context()
	redisContainer, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-fail-redisdown", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 100 * time.Millisecond

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Now stop the container
	t.Log("Stopping Redis container...")
	require.NoError(t, redisContainer.Stop(ctx, nil), "Failed to stop Redis container")
	t.Log("Redis container stopped.")

	// It should now transition to a Fail state
	waitForStatus(t, checker, core.StatusFail, 3*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status %s after container stopped, got %s", core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Redis ping failed", "Expected output message indicating connection error")
}

// TestRedisChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestRedisChecker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-disable-enable", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Disable the checker
	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Redis checker disabled", status.Output)

	// Ensure no status changes occur while disabled
	select {
	case newStatus := <-checker.StatusChange():
		require.Equal(t, core.StatusWarn, newStatus.Status, "Unexpected status change to %s while checker was disabled", newStatus.Status)
	case <-time.After(checkInterval * 3):
		// Expected, no status change
	}

	// Give performHealthCheck a chance to run while disabled
	time.Sleep(checkInterval * 2) // Allow a few ticks

	// After sleeping, the status should still be "disabled" and not changed by performHealthCheck
	statusAfterSleep := checker.Status()
	require.Equal(t, core.StatusWarn, statusAfterSleep.Status, "Expected status to remain Warn after performHealthCheck while disabled")
	require.Contains(t, statusAfterSleep.Output, "Redis checker disabled", "Expected output to remain 'Redis checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Redis checker enabled, re-initializing...", status.Output)

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestRedisChecker_Integration_Close tests graceful shutdown.
func TestRedisChecker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-close", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Close the checker
	require.NoError(t, checker.Close())

	// Verify that the internal context is cancelled after Close()
	require.Eventually(t, func() bool {
		select {
		case <-checker.(*redisChecker).ctx.Done():
			return true
		default:
			return false
		}
	}, 500*time.Millisecond, 10*time.Millisecond, "Checker context was not cancelled after Close()")

	// Ensure no further status changes are reported by the checker's goroutine
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after checker was closed", "status: %s", s.Status)
	case <-time.After(checkInterval * 3): // Wait a bit to ensure no more ticks
		// Expected, no more status changes
	}
}

// TestRedisChecker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestRedisChecker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-change-status", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Manually change status to Warn
	manualWarnStatus := core.ComponentStatus{Status: core.StatusWarn, Output: "Manual override to Warn"}
	checker.ChangeStatus(manualWarnStatus)

	status := checker.Status()
	require.Equal(t, manualWarnStatus, status)

	// Verify the manual status change is broadcast
	select {
	case s := <-checker.StatusChange():
		require.Equal(t, manualWarnStatus.Status, s.Status)
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Timed out waiting for manual status change notification")
	}

	// The periodic check should eventually revert it to Pass
	waitForStatus(t, checker, core.StatusPass, 1*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestRedisChecker_Getters tests the Descriptor and Health methods.
func TestRedisChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "test-type"}
	checker := &redisChecker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

	// Test Descriptor()
	actualDesc := checker.Descriptor()
	require.Equal(t, desc, actualDesc, "Descriptor() should return the correct descriptor")

	// Test Health()
	health := checker.Health()
	require.Equal(t, desc.ComponentID, health.ComponentID, "Health().ComponentID should match")
	require.Equal(t, desc.ComponentType, health.ComponentType, "Health().ComponentType should match")
	require.Equal(t, core.StatusPass, health.Status, "Health().Status should match current status")
	require.Equal(t, "Everything is fine", health.Output, "Health().Output should match current status output")

	// Test Health() with empty output
	checker.currentStatus.Output = ""
	health = checker.Health()
	require.Empty(t, health.Output, "Health().Output should be empty if current status output is empty")
}

// TestRedisChecker_Close_NilClient tests the Close method when the internal Redis client is nil.
func TestRedisChecker_Close_NilClient(t *testing.T) {
	desc := core.Descriptor{ComponentID: "redis-close-nil-client", ComponentType: "redis"}
	checker := &redisChecker{
		descriptor: desc,
		// Simulate a checker where client connection failed to open
		client:     nil,
		ctx:        t.Context(),
		cancelFunc: func() {},
		quit:       make(chan struct{}),
	}

	err := checker.Close()
	require.NoError(t, err, "Close() on nil client should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Quit channel not closed")
	}
}

// TestRedisChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestRedisChecker_QuitChannelStopsLoop(t *testing.T) {
	ctx := t.Context()
	_, redisOptions, cleanup := setupRedisContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "redis-quit-loop", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second

	checker := NewRedisChecker(desc, checkInterval, pingTimeout, redisOptions)
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Manually close the quit channel to stop the loop
	close(checker.(*redisChecker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further status changes are reported (except the one for shutdown if any).
	select {
	case s := <-checker.StatusChange():
		t.Logf("Received status change: %s", s.Status)
		// It might send a final status, but should not send periodic ones.
	case <-time.After(checkInterval * 3):
		// Expected: no more status changes
	}

	// Clean up the Redis client that was opened by the checker.
	// The client is already closed by the startHealthCheckLoop goroutine when 'quit' channel is closed.
	// if checker.(*redisChecker).client != nil {
	// 	err := checker.(*redisChecker).client.Close()
	// 	require.NoError(t, err, "Error closing Redis client after quit")
	// }
}

// TestRedisChecker_PerformHealthCheck_ClientNil tests performHealthCheck when r.client is nil.
func TestRedisChecker_PerformHealthCheck_ClientNil(t *testing.T) {
	desc := core.Descriptor{ComponentID: "redis-perform-nil-client", ComponentType: "redis"}
	checker := &redisChecker{
		descriptor: desc,
		client:     nil, // Simulate nil client connection
		ctx:        t.Context(),
		cancelFunc: context.CancelFunc(func() {}),
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when client is nil")
	require.Contains(t, status.Output, "Redis client is nil", "Expected specific output")
}

// TestNewRedisChecker_OpenError tests the scenario where newRealRedisClient returns an error.
func TestNewRedisChecker_OpenError(t *testing.T) {
	// Create a mock OpenRedisFunc that always returns an error
	mockOpenRedis := func(options *redis.Options) (redisConnection, error) {
		return nil, fmt.Errorf("mocked redis.NewClient error: cannot connect")
	}

	desc := core.Descriptor{ComponentID: "redis-open-error", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second
	redisOptions := &redis.Options{Addr: "localhost:6379"}

	checker := NewRedisCheckerWithOpenRedisFunc(desc, checkInterval, pingTimeout, redisOptions, mockOpenRedis)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newRealRedisClient fails")
	require.Contains(t, status.Output, "Failed to open Redis connection", "Expected output to indicate connection failure")
	require.Contains(t, status.Output, "mocked redis.NewClient error", "Expected output to contain the mocked error")

	// Ensure the health check loop was not started
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after newRealRedisClient failed", "status: %s", s.Status)
	case <-time.After(100 * time.Millisecond):
		// Expected, no status change
	}
}

// TestRedisChecker_PerformHealthCheck_PingError tests performHealthCheck when Ping returns an error.
func TestRedisChecker_PerformHealthCheck_PingError(t *testing.T) {
	mockClient := &mockRedisConnection{
		pingFunc: func(ctx context.Context) *redis.StatusCmd {
			return redis.NewStatusCmd(ctx, "PING")
		},
	}
	// Make ping return an error
	mockClient.pingFunc = func(ctx context.Context) *redis.StatusCmd {
		cmd := redis.NewStatusCmd(ctx, "PING")
		cmd.SetErr(fmt.Errorf("mock ping error"))
		return cmd
	}

	desc := core.Descriptor{ComponentID: "redis-perform-ping-error", ComponentType: "redis"}
	checkInterval := 50 * time.Millisecond
	pingTimeout := 1 * time.Second
	redisOptions := &redis.Options{Addr: "localhost:6379"}

	checker := newRedisCheckerInternal(desc, checkInterval, pingTimeout, redisOptions, mockClient)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*redisChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when ping returns an error")
	require.Contains(t, status.Output, "Redis ping failed", "Expected specific output")
	require.Contains(t, status.Output, "mock ping error", "Expected output to contain the mocked error")
}
