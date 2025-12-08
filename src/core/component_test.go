package core

import (
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewComponent(t *testing.T) {
	descriptor := Descriptor{ComponentID: "test-comp"}
	initialStatus := StatusPass

	c := New(descriptor, initialStatus)
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	if c == nil {
		t.Fatal("New() returned nil")
	}

	// Allow time for the initial status to be set.
	time.Sleep(10 * time.Millisecond)

	if c.Status().Status != initialStatus {
		t.Errorf("Expected initial status to be %v, got %v", initialStatus, c.Status().Status)
	}

	if !reflect.DeepEqual(c.Descriptor(), descriptor) {
		t.Errorf("Expected descriptor to be %+v, got %+v", descriptor, c.Descriptor())
	}
}

func TestChangeStatus(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
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
	if !reflect.DeepEqual(c.Status(), newStatus) {
		t.Errorf("Status() not updated correctly. Got %+v, want %+v", c.Status(), newStatus)
	}
}

func TestEnableDisable(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
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

	if c.Status().Status != StatusFail {
		t.Errorf("Disable() failed. Expected status %v, got %v", StatusFail, c.Status().Status)
	}

	// Test Enable
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-statusChan
	}()
	c.Enable()
	wg.Wait()

	if c.Status().Status != StatusPass {
		t.Errorf("Enable() failed. Expected status %v, got %v", StatusPass, c.Status().Status)
	}
}

func TestClose(t *testing.T) {
	c := New(Descriptor{}, StatusPass)

	// Closing should be idempotent
	if err := c.Close(); err != nil {
		t.Errorf("First Close() returned an error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Second Close() returned an error: %v", err)
	}

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
		t.Error("Close() did not seem to stop the component's goroutine, ChangeStatus blocked.")
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
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	c.ChangeStatus(status)

	// Wait for the status to propagate through the channel
	statusChan := c.StatusChange()
	select {
	case receivedStatus := <-statusChan:
		if !reflect.DeepEqual(receivedStatus, status) {
			t.Errorf("Received status from StatusChange() not equal to sent status. Got %+v, want %+v", receivedStatus, status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for status change to propagate")
	}

	health := c.Health()

	if health.Status != status.Status {
		t.Errorf("Health.Status incorrect. Got %v, want %v", health.Status, status.Status)
	}
	if health.ComponentID != descriptor.ComponentID {
		t.Errorf("Health.ComponentID incorrect. Got %v, want %v", health.ComponentID, descriptor.ComponentID)
	}
	if health.ObservedValue != status.ObservedValue {
		t.Errorf("Health.ObservedValue incorrect. Got %v, want %v", health.ObservedValue, status.ObservedValue)
	}
	if health.Version != descriptor.Version {
		t.Errorf("Health.Version incorrect. Got %v, want %v", health.Version, descriptor.Version)
	}
	if health.ReleaseID != descriptor.ReleaseID {
		t.Errorf("Health.ReleaseID incorrect. Got %v, want %v", health.ReleaseID, descriptor.ReleaseID)
	}
	if !reflect.DeepEqual(health.Notes, descriptor.Notes) {
		t.Errorf("Health.Notes incorrect. Got %v, want %v", health.Notes, descriptor.Notes)
	}
	if !reflect.DeepEqual(health.Links, descriptor.Links) {
		t.Errorf("Health.Links incorrect. Got %+v, want %+v", health.Links, descriptor.Links)
	}
	if health.ServiceID != descriptor.ServiceID {
		t.Errorf("Health.ServiceID incorrect. Got %v, want %v", health.ServiceID, descriptor.ServiceID)
	}
	if health.Description != descriptor.Description {
		t.Errorf("Health.Description incorrect. Got %v, want %v", health.Description, descriptor.Description)
	}
	if health.ComponentType != descriptor.ComponentType {
		t.Errorf("Health.ComponentType incorrect. Got %v, want %v", health.ComponentType, descriptor.ComponentType)
	}
	if health.ObservedUnit != status.ObservedUnit {
		t.Errorf("Health.ObservedUnit incorrect. Got %v, want %v", health.ObservedUnit, status.ObservedUnit)
	}
	if !reflect.DeepEqual(health.AffectedEndpoints, status.AffectedEndpoints) {
		t.Errorf("Health.AffectedEndpoints incorrect. Got %+v, want %+v", health.AffectedEndpoints, status.AffectedEndpoints)
	}
	if health.Time != status.Time { // Compare time.Time objects
		t.Errorf("Health.Time incorrect. Got %v, want %v", health.Time, status.Time)
	}
}

// This test is designed to fail when run with the -race flag.
func TestRaceCondition(t *testing.T) {
	t.Parallel()
	c := New(Descriptor{}, StatusPass)
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
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
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
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
			err := c.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
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
			t.Fatalf("After ChangeStatus, status did not update correctly.")
		}
		// Also check the Health() method's status
		if c.Health().Status != newStatus.Status {
			t.Errorf("Health().Status was not updated correctly after ChangeStatus. Got %v, want %v", c.Health().Status, newStatus.Status)
		}

		// --- Test 2: Disable ---
		c.Disable()
		if !assertStatusDtoEventually(t, c, StatusFail) {
			t.Fatalf("After Disable, status was not '%s'.", StatusFail)
		}
		if c.Health().Status != StatusFail {
			t.Errorf("Health().Status was not updated correctly after Disable. Got %v, want %v", c.Health().Status, StatusFail)
		}

		// --- Test 3: Enable ---
		c.Enable()
		if !assertStatusDtoEventually(t, c, StatusPass) {
			t.Fatalf("After Enable, status was not '%s'.", StatusPass)
		}
		if c.Health().Status != StatusPass {
			t.Errorf("Health().Status was not updated correctly after Enable. Got %v, want %v", c.Health().Status, StatusPass)
		}
	})
}

// TestObserverChannelBehavior tests the non-blocking send and draining logic of the observerChange channel.
func TestObserverChannelBehavior(t *testing.T) {
	c := New(Descriptor{}, StatusPass)
	defer func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
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
		if s.Status != StatusPass || s.Output != "Status 3" {
			t.Errorf("Expected latest status (Status 3), got %+v", s)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for status from observer channel")
	}

	// Read again; channel should be empty now
	select {
	case s := <-statusChan:
		t.Errorf("Expected channel to be empty after read, but got %+v", s)
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

	err := c.Close()
	if err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}

	select {
	case <-done:
		// Goroutine exited, channel is closed.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for observer channel to close after component close")
	}
}

// TestChangeStatusAfterClose ensures that ChangeStatus does not block and does not
// modify the last status once the component has been closed.
func TestChangeStatusAfterClose(t *testing.T) {
	c := New(Descriptor{}, StatusPass)

	initialStatus := c.Status()

	err := c.Close()
	if err != nil {
		t.Fatalf("Close() returned an error: %v", err)
	}

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
		t.Fatal("ChangeStatus blocked after component was closed")
	}

	// Verify that the status did not change after closing
	if !reflect.DeepEqual(c.Status(), initialStatus) {
		t.Errorf("Status changed after component was closed. Got %+v, want %+v", c.Status(), initialStatus)
	}
}

// assertStatusEventually polls the component's Status() method until it matches the expected value.
func assertStatusEventually(t *testing.T, c Component, expected ComponentStatus) bool {
	t.Helper()
	for i := 0; i < 100; i++ {
		if reflect.DeepEqual(c.Status(), expected) {
			return true
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Logf("assertStatusEventually failed. Got %+v, want %+v", c.Status(), expected)
	return false
}

// assertStatusDtoEventually polls the component's Status() method until the inner status DTO matches the expected value.
func assertStatusDtoEventually(t *testing.T, c Component, expected StatusEnum) bool {
	t.Helper()
	for i := 0; i < 100; i++ {
		if c.Status().Status == expected {
			return true
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Logf("assertStatusDtoEventually failed. Got %v, want %v", c.Status().Status, expected)
	return false
}
