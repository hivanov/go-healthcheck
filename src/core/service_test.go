package core

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Helper function to create a new component for service tests
func newTestComponent(id string, initialStatus StatusEnum) Component { // Updated type
	return New(Descriptor{ComponentID: id, ComponentType: "test-type"}, initialStatus) // Updated type
}

func TestNewService(t *testing.T) {
	t.Run("no components", func(t *testing.T) {
		s := NewService(Descriptor{ServiceID: "test-service"}) // Updated type
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error")
		}()
		time.Sleep(10 * time.Millisecond) // allow for initial recalculation
		assert.Equal(t, StatusPass, s.Health().Status, "Expected status 'pass' for service with no components")
	})

	t.Run("all pass", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusPass)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error")
		}()
		time.Sleep(10 * time.Millisecond)
		assert.Equal(t, StatusPass, s.Health().Status, "Expected status 'pass'")
	})

	t.Run("one warn", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusWarn)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error")
		}()
		time.Sleep(10 * time.Millisecond)
		assert.Equal(t, StatusWarn, s.Health().Status, "Expected status 'warn'")
	})

	t.Run("one fail", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusFail)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error")
		}()
		time.Sleep(10 * time.Millisecond)
		assert.Equal(t, StatusFail, s.Health().Status, "Expected status 'fail'")
	})

	t.Run("fail overrides warn", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusWarn)                       // Updated type
		c2 := newTestComponent("c2", StatusFail)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error")
		}()
		time.Sleep(10 * time.Millisecond)
		assert.Equal(t, StatusFail, s.Health().Status, "Expected status 'fail'")
	})
}

func TestServiceStatusRecalculation(t *testing.T) {
	c1 := newTestComponent("c1", StatusPass)                       // Updated type
	c2 := newTestComponent("c2", StatusPass)                       // Updated type
	s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
	defer func() {
		assert.NoError(t, s.Close(), "Close() returned an error")
	}()

	// Initial: pass
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, StatusPass, s.Health().Status, "Initial status should be 'pass'")

	// Change c1 to warn -> service should be warn
	c1.ChangeStatus(ComponentStatus{Status: StatusWarn}) // Updated type
	time.Sleep(10 * time.Millisecond)                    // allow for recalculation
	assert.Equal(t, StatusWarn, s.Health().Status, "Status should be 'warn' after one component warns")

	// Change c2 to fail -> service should be fail
	c2.ChangeStatus(ComponentStatus{Status: StatusFail}) // Updated type
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, StatusFail, s.Health().Status, "Status should be 'fail' after one component fails")

	// Change c2 back to pass -> service should be warn (because of c1)
	c2.ChangeStatus(ComponentStatus{Status: StatusPass}) // Updated type
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, StatusWarn, s.Health().Status, "Status should be 'warn' after failing component recovers")

	// Change c1 back to pass -> service should be pass
	c1.ChangeStatus(ComponentStatus{Status: StatusPass}) // Updated type
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, StatusPass, s.Health().Status, "Status should be 'pass' after all components recover")
}

func TestServiceHealth(t *testing.T) {
	descriptor := Descriptor{ // Updated type
		ServiceID:   "my-app",
		Version:     "2.0",
		Description: "My awesome application",
	}
	c1 := newTestComponent("c1", StatusPass) // Updated type
	c2 := newTestComponent("c2", StatusWarn) // Updated type

	s := NewService(descriptor, c1, c2)
	defer func() {
		assert.NoError(t, s.Close(), "Close() returned an error")
	}()

	time.Sleep(10 * time.Millisecond)
	health := s.Health()

	assert.Equal(t, descriptor.ServiceID, health.ServiceID, "Health.ServiceID mismatch")
	assert.Equal(t, descriptor.Version, health.Version, "Health.Version mismatch")
	assert.Equal(t, descriptor.Description, health.Description, "Health.Description mismatch")

	require.Len(t, health.Checks, 1, "Expected 1 check group")
	_, ok := health.Checks["test-type"]
	require.True(t, ok, "Expected checks to be grouped by 'test-type'")
	assert.Len(t, health.Checks["test-type"], 2, "Expected 2 checks in 'test-type' group")

	t.Run("component with no type", func(t *testing.T) {
		cNoType := New(Descriptor{ComponentID: "no-type"}, StatusPass) // Updated type
		sNoType := NewService(Descriptor{}, cNoType)                   // Updated type
		defer func() {
			assert.NoError(t, sNoType.Close(), "Close() returned an error")
		}()

		time.Sleep(10 * time.Millisecond)
		healthNoType := sNoType.Health()

		_, ok := healthNoType.Checks["default"]
		require.True(t, ok, "Expected checks to be grouped under 'default' for component with no type")
		assert.Len(t, healthNoType.Checks["default"], 1, "Expected 1 check in 'default' group")
	})
}

func TestServiceClose(t *testing.T) {
	c := newTestComponent("c1", StatusPass) // Updated type
	s := NewService(Descriptor{}, c)        // Updated type

	assert.NoError(t, s.Close(), "First Close() returned an error")
	assert.NoError(t, s.Close(), "Second Close() should not return an error")

	// After closing, the component's internal goroutine should have stopped.
	// We can test this by trying to send a status change, which should not block
	// because the context passed to it is now 'Done'.
	done := make(chan bool)
	go func() {
		c.ChangeStatus(ComponentStatus{Status: StatusWarn}) // Updated type
		done <- true
	}()

	select {
	case <-done:
		// success
	case <-time.After(100 * time.Millisecond):
		t.Error("ChangeStatus on a component of a closed service blocked, indicating it may not be closed properly.")
	}
}

func TestServiceRecalculatesOnComponentStatusChange(t *testing.T) {
	t.Parallel() // Run in parallel as it's an independent test

	// Create a component that we can control
	ctrlComp := newTestComponent("controllable-comp", StatusPass)

	// Create a service with this component
	s := NewService(Descriptor{ServiceID: "recalc-service"}, ctrlComp)
	defer func() {
		assert.NoError(t, s.Close(), "Service close returned error")
	}()

	// Give time for initial status calculation
	time.Sleep(50 * time.Millisecond)

	// Verify initial status is Pass
	require.Equal(t, StatusPass, s.Health().Status, "Expected initial service status to be 'pass'")

	// Change component status to Warn
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusWarn})

	// Give time for the service's goroutine to pick up the change and recalculate
	time.Sleep(50 * time.Millisecond)

	// Verify service status is now Warn
	assert.Equal(t, StatusWarn, s.Health().Status, "Expected service status to be 'warn' after component change")

	// Change component status to Fail
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusFail})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, StatusFail, s.Health().Status, "Expected service status to be 'fail' after component change")

	// Change component status back to Pass
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusPass})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, StatusPass, s.Health().Status, "Expected service status to be 'pass' after component change")
}

// FuzzService tests the Service's ability to handle various component status changes without panicking.
func FuzzService(f *testing.F) {
	// Seed with some initial valid numbers of components and operation counts
	f.Add(3, 10)
	f.Add(1, 5)
	f.Add(10, 20)

	f.Fuzz(func(t *testing.T, numComponents, numOperations int) {
		// Ensure reasonable bounds for fuzzed inputs
		if numComponents < 1 || numComponents > 10 {
			numComponents = 3
		}
		if numOperations < 1 || numOperations > 100 {
			numOperations = 20
		}

		// Create components
		components := make([]Component, numComponents)
		for i := 0; i < numComponents; i++ {
			components[i] = newTestComponent(
				// Fuzzer doesn't provide strings for ComponentID directly, so use an int
				// This also means ComponentType will always be "test-type"
				// which is fine for fuzzing the core service logic.
				"comp-"+fmt.Sprintf("%d", i), StatusPass)
			defer components[i].Close() // Ensure components are closed
		}

		s := NewService(Descriptor{ServiceID: "fuzz-service"}, components...)
		defer func() {
			assert.NoError(t, s.Close(), "Close() returned an error in fuzz test")
		}()

		// Give a moment for initial status calculation
		time.Sleep(10 * time.Millisecond)

		// Perform random operations on components
		for i := 0; i < numOperations; i++ {
			compIndex := i % numComponents // Cycle through components
			opType := i % 3                // Cycle through operation types

			switch opType {
			case 0: // Change status to Pass
				components[compIndex].ChangeStatus(ComponentStatus{Status: StatusPass})
			case 1: // Change status to Warn
				components[compIndex].ChangeStatus(ComponentStatus{Status: StatusWarn, Output: "fuzz warn"})
			case 2: // Change status to Fail
				components[compIndex].ChangeStatus(ComponentStatus{Status: StatusFail, Output: "fuzz fail"})
			}
			_ = s.Health()               // Check overall service health
			time.Sleep(time.Millisecond) // Small delay to allow goroutines to process
		}

		// Final check and close for good measure
		_ = s.Health()
		assert.NoError(t, s.Close(), "Close() returned an error at end of fuzz test")
	})
}
