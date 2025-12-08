package builtin

import (
	"healthcheck/core"
	"testing"
	"time"
)

func TestUptimeInitialStatus(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	// Wait for the initial status to be sent
	time.Sleep(50 * time.Millisecond)

	status := c.Status()
	if status.Status != core.StatusPass { // Renamed type
		t.Errorf("Expected initial status to be 'pass', got '%s'", status.Status)
	}

	uptime, ok := status.ObservedValue.(float64)
	if !ok {
		t.Fatalf("Expected ObservedValue to be a float64, but it was not.")
	}
	if uptime < 0 || uptime > 0.1 {
		t.Errorf("Expected initial uptime to be between 0 and 0.1 seconds, got %f", uptime)
	}
}

func TestUptimeUpdates(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	observer := c.StatusChange()

	// Read the initial status
	var initialStatus core.ComponentStatus // Renamed type
	select {
	case initialStatus = <-observer:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Did not receive initial status update")
	}

	// Wait for the next tick (a bit more than 1 second)
	var firstUpdate core.ComponentStatus // Renamed type
	select {
	case firstUpdate = <-observer:
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive first 1-second status update")
	}

	initialUptime := initialStatus.ObservedValue.(float64)
	firstUptime := firstUpdate.ObservedValue.(float64)

	if firstUptime <= initialUptime {
		t.Errorf("Expected uptime to increase. Initial: %f, First update: %f", initialUptime, firstUptime)
	}

	if firstUptime < 1.0 || firstUptime > 1.2 {
		t.Errorf("Expected first update uptime to be around 1 second, got %f", firstUptime)
	}
}

func TestUptimeClose(t *testing.T) {
	c := NewUptimeComponent()
	observer := c.StatusChange()

	// Close the component
	err := c.Close()
	if err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}

	// After closing, the observer channel should be drained and then closed.
	// We loop with a timeout to confirm this happens.
	timeout := time.After(200 * time.Millisecond)
	for {
		select {
		case _, ok := <-observer:
			if !ok {
				// Success: channel is closed.
				return
			}
			// A value was drained, continue looping until the channel is closed.
		case <-timeout:
			t.Fatal("Timeout waiting for observer channel to close.")
		}
	}
}

func TestUptimeExternalChangesAreNoOp(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()
	observer := c.StatusChange()

	// Wait for initial status
	<-observer

	// Attempt to disable the component
	c.Disable()

	// The component should ignore the disable command and send its regular "pass" update.
	select {
	case update := <-observer:
		if update.Status != core.StatusPass { // Renamed type
			t.Errorf("Expected status to remain 'pass' after Disable(), got '%s'", update.Status)
		}
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling Disable()")
	}

	// Attempt to change status directly
	c.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "test"}) // Renamed type

	select {
	case update := <-observer:
		if update.Status != core.StatusPass { // Renamed type
			t.Errorf("Expected status to remain 'pass' after ChangeStatus(), got '%s'", update.Status)
		}
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling ChangeStatus()")
	}

	// Attempt to enable the component
	c.Enable()

	select {
	case update := <-observer:
		if update.Status != core.StatusPass { // Renamed type
			t.Errorf("Expected status to remain 'pass' after Enable(), got '%s'", update.Status)
		}
	case <-time.After(1100 * time.Millisecond):
		t.Fatal("Did not receive any status update after calling Enable()")
	}
}

func TestUptimeHealthAndDescriptor(t *testing.T) {
	c := NewUptimeComponent()
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	// Wait for initial status
	time.Sleep(50 * time.Millisecond)

	// Test Descriptor
	descriptor := c.Descriptor()
	if descriptor.ComponentID != "uptime" {
		t.Errorf("Expected ComponentID to be 'uptime', got '%s'", descriptor.ComponentID)
	}
	if descriptor.ComponentType != "system" {
		t.Errorf("Expected ComponentType to be 'system', got '%s'", descriptor.ComponentType)
	}

	// Test Health
	health := c.Health()
	if health.Status != core.StatusPass { // Renamed type
		t.Errorf("Health status should always be 'pass', got '%s'", health.Status)
	}
	if health.ComponentID != "uptime" {
		t.Errorf("Health ComponentID should be 'uptime', got '%s'", health.ComponentID)
	}
	uptime, ok := health.ObservedValue.(float64)
	if !ok {
		t.Fatalf("Health ObservedValue should be a float64")
	}
	if uptime < 0 {
		t.Errorf("Health uptime should be non-negative, got %f", uptime)
	}
}
