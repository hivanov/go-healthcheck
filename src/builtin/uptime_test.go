package builtin

import (
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUptimeInitialStatus(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	// Wait for the initial status to be sent
	time.Sleep(50 * time.Millisecond)

	status := c.Status()
	assert.Equal(t, core.StatusPass, status.Status, "Expected initial status to be 'pass'")

	uptime, ok := status.ObservedValue.(float64)
	require.True(t, ok, "Expected ObservedValue to be a float64")
	assert.InDelta(t, 0.05, uptime, 0.05, "Expected initial uptime to be around 0.05 seconds")
}

func TestUptimeUpdates(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	observer := c.StatusChange()

	// Read the initial status
	var initialStatus core.ComponentStatus
	require.Eventually(t, func() bool {
		select {
		case initialStatus = <-observer:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Did not receive initial status update")

	// Wait for the next tick (a bit more than 1 second)
	var firstUpdate core.ComponentStatus
	require.Eventually(t, func() bool {
		select {
		case firstUpdate = <-observer:
			return true
		default:
			return false
		}
	}, 1100*time.Millisecond, 100*time.Millisecond, "Did not receive first 1-second status update")

	initialUptime := initialStatus.ObservedValue.(float64)
	firstUptime := firstUpdate.ObservedValue.(float64)

	assert.Greater(t, firstUptime, initialUptime, "Expected uptime to increase")
	assert.InDelta(t, 1.0, firstUptime, 0.2, "Expected first update uptime to be around 1 second")
}

func TestUptimeClose(t *testing.T) {
	c := NewUptimeComponent()
	observer := c.StatusChange()

	// Close the component
	assert.NoError(t, c.Close(), "Close() returned an error")

	// After closing, the observer channel should be drained and then closed.
	assert.Eventually(t, func() bool {
		_, ok := <-observer
		return !ok
	}, 200*time.Millisecond, 10*time.Millisecond, "Timeout waiting for observer channel to close.")
}

func TestUptimeExternalChangesAreNoOp(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()
	observer := c.StatusChange()

	// Wait for initial status
	<-observer

	// Attempt to disable the component
	c.Disable()

	// The component should ignore the disable command and send its regular "pass" update.
	select {
	case update := <-observer:
		assert.Equal(t, core.StatusPass, update.Status, "Expected status to remain 'pass' after Disable()")
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling Disable()")
	}

	// Attempt to change status directly
	c.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "test"})

	select {
	case update := <-observer:
		assert.Equal(t, core.StatusPass, update.Status, "Expected status to remain 'pass' after ChangeStatus()")
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling ChangeStatus()")
	}

	// Attempt to enable the component
	c.Enable()

	select {
	case update := <-observer:
		assert.Equal(t, core.StatusPass, update.Status, "Expected status to remain 'pass' after Enable()")
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling Enable()")
	}
}

func TestUptimeHealthAndDescriptor(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	// Wait for initial status
	time.Sleep(50 * time.Millisecond)

	// Test Descriptor
	descriptor := c.Descriptor()
	assert.Equal(t, "uptime", descriptor.ComponentID, "Expected ComponentID to be 'uptime'")
	assert.Equal(t, "system", descriptor.ComponentType, "Expected ComponentType to be 'system'")

	// Test Health
	health := c.Health()
	assert.Equal(t, core.StatusPass, health.Status, "Health status should always be 'pass'")
	assert.Equal(t, "uptime", health.ComponentID, "Health ComponentID should be 'uptime'")
	uptime, ok := health.ObservedValue.(float64)
	require.True(t, ok, "Health ObservedValue should be a float64")
	assert.GreaterOrEqual(t, uptime, 0.0, "Health uptime should be non-negative")
}
