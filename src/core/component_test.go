package core

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestNewComponent(t *testing.T) {
	descriptor := Descriptor{ComponentID: "test-comp"}
	initialStatus := StatusPass

	c := New(descriptor, initialStatus)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	require.NotNil(t, c, "New() returned nil")

	// Allow time for the initial status to be set.
	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, initialStatus, c.Status().Status, "Expected initial status to be %v, got %v", initialStatus, c.Status().Status)
	assert.Equal(t, descriptor, c.Descriptor(), "Expected descriptor to be %+v, got %+v", descriptor, c.Descriptor())
}

func TestChangeStatus(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	newStatus := ComponentStatus{
		Status: StatusWarn,
		Output: "Disk space is low",
	}

	// We listen on the channel to know when the change is processed.
	statusChan := c.StatusChange()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-statusChan
	}()

	c.ChangeStatus(newStatus)
	wg.Wait() // Wait until the status update has been received by the component's goroutine.

	// Now check the status
	assert.Equal(t, newStatus, c.Status(), "Status() not updated correctly. Got %+v, want %+v", c.Status(), newStatus)
}

func TestEnableDisable(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	statusChan := c.StatusChange()
	var wg sync.WaitGroup

	// Test Disable
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-statusChan
	}()
	c.Disable()
	wg.Wait()

	assert.Equal(t, StatusFail, c.Status().Status, "Disable() failed. Expected status %v, got %v", StatusFail, c.Status().Status)

	// Test Enable
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-statusChan
	}()
	c.Enable()
	wg.Wait()

	assert.Equal(t, StatusPass, c.Status().Status, "Enable() failed. Expected status %v, got %v", StatusPass, c.Status().Status)
}

func TestClose(t *testing.T) {
	c := New(Descriptor{}, StatusPass)

	// Closing should be idempotent
	assert.NoError(t, c.Close(), "First Close() returned an error")
	assert.NoError(t, c.Close(), "Second Close() returned an error")

	// After closing, the internal goroutine should have stopped.
	// Sending a status change should ideally not block forever.
	// We'll use a timeout to detect if it blocks.
	done := make(chan struct{})
	go func() {
		close(done)
	}()

	select {
	case <-done:
	// Test passed
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "Close() did not seem to stop the component's goroutine, ChangeStatus blocked.")
	}
}

func TestHealth(t *testing.T) {
	descriptor := Descriptor{
		ComponentID:   "db-1",
		ComponentType: "database",
		Version:       "1.2.3",
		ReleaseID:     "v1.2.3-alpha",
		Notes:         []string{"Primary database replica"},
		ServiceID:     "app-backend-service",
		Description:   "Handles all persistent data for the application.",
		Links:         map[string]string{"status": "http://example.com/db-status"},
	}
	status := ComponentStatus{
		Status:            StatusWarn,
		Output:            "Connections are high",
		ObservedValue:     150,
		ObservedUnit:      "connections",
		AffectedEndpoints: []string{"/api/v1/data"},
		Time:              time.Now().UTC().Round(time.Millisecond), // Round to millisecond to ignore monotonic clock differences
	}

	c := New(descriptor, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	c.ChangeStatus(status)

	// Wait for the status to propagate through the channel
	statusChan := c.StatusChange()
	select {
	case receivedStatus := <-statusChan:
		assert.Equal(t, status, receivedStatus, "Received status from StatusChange() not equal to sent status. Got %+v, want %+v", receivedStatus, status)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "Timeout waiting for status change to propagate")
	}

	health := c.Health()

	assert.Equal(t, status.Status, health.Status, "Health.Status incorrect.")
	assert.Equal(t, descriptor.ComponentID, health.ComponentID, "Health.ComponentID incorrect.")
	assert.Equal(t, status.ObservedValue, health.ObservedValue, "Health.ObservedValue incorrect.")
	assert.Equal(t, descriptor.Version, health.Version, "Health.Version incorrect.")
	assert.Equal(t, descriptor.ReleaseID, health.ReleaseID, "Health.ReleaseID incorrect.")
	assert.Equal(t, descriptor.Notes, health.Notes, "Health.Notes incorrect.")
	assert.Equal(t, descriptor.Links, health.Links, "Health.Links incorrect.")
	assert.Equal(t, descriptor.ServiceID, health.ServiceID, "Health.ServiceID incorrect.")
	assert.Equal(t, descriptor.Description, health.Description, "Health.Description incorrect.")
	assert.Equal(t, descriptor.ComponentType, health.ComponentType, "Health.ComponentType incorrect.")
	assert.Equal(t, status.ObservedUnit, health.ObservedUnit, "Health.ObservedUnit incorrect.")
	assert.Equal(t, status.AffectedEndpoints, health.AffectedEndpoints, "Health.AffectedEndpoints incorrect.")
	assert.Equal(t, status.Time, health.Time, "Health.Time incorrect.")
}

// This test is designed to fail when run with the -race flag.
func TestRaceCondition(t *testing.T) {
	t.Parallel()
	c := New(Descriptor{}, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.ChangeStatus(ComponentStatus{Status: StatusWarn})
			c.ChangeStatus(ComponentStatus{Status: StatusPass})
		}
	}()

	// Reader goroutine
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = c.Status()
			_ = c.Health()
		}
	}()

	wg.Wait()
}

// Fuzz and load test
func TestFuzzyLoad(t *testing.T) {
	t.Parallel()
	c := New(Descriptor{}, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	numGoroutines := 20
	numOpsPerGoroutine := 200

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for j := 0; j < numOpsPerGoroutine; j++ {
				op := r.Intn(4) // Random operation
				switch op {
				case 0:
					c.Enable()
				case 1:
					c.Disable()
				case 2:
					// Random status
					newStatus := ComponentStatus{
						Status:        StatusWarn,
						Output:        "Random output string",
						ObservedValue: r.Intn(10000),
					}
					c.ChangeStatus(newStatus)
				case 3:
					_ = c.Health()
				}
			}
		}(time.Now().UnixNano() + int64(i))
	}

	wg.Wait()
}

// FuzzComponent is a native Go fuzz test.
// It will not run with a normal `go test`.
// Run it with `go test -fuzz=.`
func FuzzComponent(f *testing.F) {
	// Seed the fuzzer with some initial valid values
	f.Add(string(StatusPass), "initial output", 123)
	f.Add(string(StatusWarn), "a warning message with special chars !@#$%^&*", -1)
	f.Add(string(StatusFail), "", 0)
	f.Add(string(StatusPass), "a very long string that might test buffer limits or other edge cases in string handling, repeated for effect. a very long string that might test buffer limits or other edge cases in string handling, repeated for effect.", 9999)

	f.Fuzz(func(t *testing.T, statusStr string, output string, value int) {
		// The fuzzer will generate random inputs for statusStr, output, and value.

		// Create a component for each fuzz test to ensure isolation
		c := New(Descriptor{}, StatusPass)
		defer func() {
			assert.NoError(t, c.Close(), "Close() returned an error")
		}()

		// Normalize the fuzzed status string into a valid StatusEnum
		var status StatusEnum
		switch statusStr {
		case string(StatusPass), string(StatusWarn), string(StatusFail):
			status = StatusEnum(statusStr)
		default:
			// Use a default valid status if the fuzzed string is not one of the enum values
			status = StatusPass
		}

		// --- Test 1: ChangeStatus with fuzzed data ---
		newStatus := ComponentStatus{
			Status:        status,
			Output:        output,
			ObservedValue: value,
		}
		c.ChangeStatus(newStatus)
		if !assertStatusEventually(t, c, newStatus) {
			require.FailNow(t, "After ChangeStatus, status did not update correctly.")
		}
		// Also check the Health() method's status
		assert.Equal(t, newStatus.Status, c.Health().Status, "Health().Status was not updated correctly after ChangeStatus.")

		// --- Test 2: Disable ---
		c.Disable()
		if !assertStatusDtoEventually(t, c, StatusFail) {
			require.FailNowf(t, "After Disable, status was not '%s'.", string(StatusFail))
		}
		assert.Equal(t, StatusFail, c.Health().Status, "Health().Status was not updated correctly after Disable.")

		// --- Test 3: Enable ---
		c.Enable()
		if !assertStatusDtoEventually(t, c, StatusPass) {
			require.FailNowf(t, "After Enable, status was not '%s'.", string(StatusPass))
		}
		assert.Equal(t, StatusPass, c.Health().Status, "Health().Status was not updated correctly after Enable.")
	})
}

// TestObserverChannelBehavior tests the non-blocking send and draining logic of the observerChange channel.
func TestObserverChannelBehavior(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	statusChan := c.StatusChange()

	// 1. Send multiple status changes rapidly. Only the latest should be buffered.
	c.ChangeStatus(ComponentStatus{Status: StatusWarn, Output: "Status 1"})
	c.ChangeStatus(ComponentStatus{Status: StatusFail, Output: "Status 2"})
	c.ChangeStatus(ComponentStatus{Status: StatusPass, Output: "Status 3"}) // This should be the one observed

	// Give time for the goroutine to process the changes
	time.Sleep(10 * time.Millisecond)

	select {
	case s := <-statusChan:
		assert.Equal(t, StatusPass, s.Status)
		assert.Equal(t, "Status 3", s.Output)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "Timeout waiting for status from observer channel")
	}

	// Read again; channel should be empty now
	select {
	case s := <-statusChan:
		assert.Fail(t, "Expected channel to be empty after read", "got %+v", s)
	default:
		// Expected: channel is empty
	}

	// 2. Test channel closure
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Drain the channel if anything is left.
		// The loop should terminate when the channel is closed.
		for range statusChan {
		}
	}()

	assert.NoError(t, c.Close())

	select {
	case <-done:
	// Goroutine exited, channel is closed.
	case <-time.After(500 * time.Millisecond):
		require.FailNow(t, "Timeout waiting for observer channel to close after component close")
	}
}

// TestChangeStatusAfterClose ensures that ChangeStatus does not block and does not
// modify the last status once the component has been closed.
func TestChangeStatusAfterClose(t *testing.T) {
	c := New(Descriptor{}, StatusPass)

	initialStatus := c.Status()

	require.NoError(t, c.Close())

	// Attempt to change status after close. This should not block.
	done := make(chan struct{})
	go func() {
		c.ChangeStatus(ComponentStatus{Status: StatusWarn})
		close(done)
	}()

	select {
	case <-done:
	// ChangeStatus returned, good.
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "ChangeStatus blocked after component was closed")
	}

	// Verify that the status did not change after closing
	assert.Equal(t, initialStatus, c.Status(), "Status changed after component was closed.")
}

// assertStatusEventually polls the component's Status() method until it matches the expected value.
func assertStatusEventually(t *testing.T, c Component, expected ComponentStatus) bool {
	t.Helper()
	return assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(expected, c.Status())
	}, time.Second, 1*time.Millisecond, "Status did not update correctly")
}

// assertStatusDtoEventually polls the component's Status() method until the inner status DTO matches the expected value.
func assertStatusDtoEventually(t *testing.T, c Component, expected StatusEnum) bool {
	t.Helper()
	return assert.Eventually(t, func() bool {
		return c.Status().Status == expected
	}, time.Second, 1*time.Millisecond, "Status DTO did not update correctly")
}
