package core

import (
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
			err := s.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()
		time.Sleep(10 * time.Millisecond)    // allow for initial recalculation
		if s.Health().Status != StatusPass { // Updated type
			t.Errorf("Expected status 'pass' for service with no components, got '%s'", s.Health().Status)
		}
	})

	t.Run("all pass", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusPass)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			err := s.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if s.Health().Status != StatusPass { // Updated type
			t.Errorf("Expected status 'pass', got '%s'", s.Health().Status)
		}
	})

	t.Run("one warn", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusWarn)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			err := s.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if s.Health().Status != StatusWarn { // Updated type
			t.Errorf("Expected status 'warn', got '%s'", s.Health().Status)
		}
	})

	t.Run("one fail", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusPass)                       // Updated type
		c2 := newTestComponent("c2", StatusFail)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			err := s.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if s.Health().Status != StatusFail { // Updated type
			t.Errorf("Expected status 'fail', got '%s'", s.Health().Status)
		}
	})

	t.Run("fail overrides warn", func(t *testing.T) {
		c1 := newTestComponent("c1", StatusWarn)                       // Updated type
		c2 := newTestComponent("c2", StatusFail)                       // Updated type
		s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
		defer func() {
			err := s.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if s.Health().Status != StatusFail { // Updated type
			t.Errorf("Expected status 'fail', got '%s'", s.Health().Status)
		}
	})
}

func TestServiceStatusRecalculation(t *testing.T) {
	c1 := newTestComponent("c1", StatusPass)                       // Updated type
	c2 := newTestComponent("c2", StatusPass)                       // Updated type
	s := NewService(Descriptor{ServiceID: "test-service"}, c1, c2) // Updated type
	defer func() {
		err := s.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	// Initial: pass
	time.Sleep(10 * time.Millisecond)
	if s.Health().Status != StatusPass { // Updated type
		t.Fatalf("Initial status should be 'pass', got '%s'", s.Health().Status)
	}

	// Change c1 to warn -> service should be warn
	c1.ChangeStatus(ComponentStatus{Status: StatusWarn}) // Updated type
	time.Sleep(10 * time.Millisecond)                    // allow for recalculation
	if s.Health().Status != StatusWarn {                 // Updated type
		t.Fatalf("Status should be 'warn' after one component warns, got '%s'", s.Health().Status)
	}

	// Change c2 to fail -> service should be fail
	c2.ChangeStatus(ComponentStatus{Status: StatusFail}) // Updated type
	time.Sleep(10 * time.Millisecond)
	if s.Health().Status != StatusFail { // Updated type
		t.Fatalf("Status should be 'fail' after one component fails, got '%s'", s.Health().Status)
	}

	// Change c2 back to pass -> service should be warn (because of c1)
	c2.ChangeStatus(ComponentStatus{Status: StatusPass}) // Updated type
	time.Sleep(10 * time.Millisecond)
	if s.Health().Status != StatusWarn { // Updated type
		t.Fatalf("Status should be 'warn' after failing component recovers, got '%s'", s.Health().Status)
	}

	// Change c1 back to pass -> service should be pass
	c1.ChangeStatus(ComponentStatus{Status: StatusPass}) // Updated type
	time.Sleep(10 * time.Millisecond)
	if s.Health().Status != StatusPass { // Updated type
		t.Fatalf("Status should be 'pass' after all components recover, got '%s'", s.Health().Status)
	}
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
		err := s.Close()
		if err != nil {
			t.Errorf("Close() returned an error: %v", err)
		}
	}()

	time.Sleep(10 * time.Millisecond)
	health := s.Health()

	if health.ServiceID != descriptor.ServiceID {
		t.Errorf("Health.ServiceID mismatch. Got '%s', want '%s'", health.ServiceID, descriptor.ServiceID)
	}
	if health.Version != descriptor.Version {
		t.Errorf("Health.Version mismatch. Got '%s', want '%s'", health.Version, descriptor.Version)
	}
	if health.Description != descriptor.Description {
		t.Errorf("Health.Description mismatch. Got '%s', want '%s'", health.Description, descriptor.Description)
	}

	if len(health.Checks) != 1 {
		t.Fatalf("Expected 1 check group, got %d", len(health.Checks))
	}
	if _, ok := health.Checks["test-type"]; !ok {
		t.Fatalf("Expected checks to be grouped by 'test-type'")
	}
	if len(health.Checks["test-type"]) != 2 {
		t.Fatalf("Expected 2 checks in 'test-type' group, got %d", len(health.Checks["test-type"]))
	}

	t.Run("component with no type", func(t *testing.T) {
		cNoType := New(Descriptor{ComponentID: "no-type"}, StatusPass) // Updated type
		sNoType := NewService(Descriptor{}, cNoType)                   // Updated type
		defer func() {
			err := sNoType.Close()
			if err != nil {
				t.Errorf("Close() returned an error: %v", err)
			}
		}()

		time.Sleep(10 * time.Millisecond)
		healthNoType := sNoType.Health()

		if _, ok := healthNoType.Checks["default"]; !ok {
			t.Fatalf("Expected checks to be grouped under 'default' for component with no type")
		}
		if len(healthNoType.Checks["default"]) != 1 {
			t.Fatalf("Expected 1 check in 'default' group, got %d", len(healthNoType.Checks["default"]))
		}
	})
}

func TestServiceClose(t *testing.T) {
	c := newTestComponent("c1", StatusPass) // Updated type
	s := NewService(Descriptor{}, c)        // Updated type

	if err := s.Close(); err != nil {
		t.Errorf("First Close() returned an error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Second Close() returned an error: %v", err)
	}

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
		if err := s.Close(); err != nil {
			t.Errorf("Service close returned error: %v", err)
		}
	}()

	// Give time for initial status calculation
	time.Sleep(50 * time.Millisecond)

	// Verify initial status is Pass
	if s.Health().Status != StatusPass {
		t.Fatalf("Expected initial service status to be 'pass', got '%s'", s.Health().Status)
	}

	// Change component status to Warn
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusWarn})

	// Give time for the service's goroutine to pick up the change and recalculate
	time.Sleep(50 * time.Millisecond)

	// Verify service status is now Warn
	if s.Health().Status != StatusWarn {
		t.Errorf("Expected service status to be 'warn' after component change, got '%s'", s.Health().Status)
	}

	// Change component status to Fail
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusFail})
	time.Sleep(50 * time.Millisecond)
	if s.Health().Status != StatusFail {
		t.Errorf("Expected service status to be 'fail' after component change, got '%s'", s.Health().Status)
	}

	// Change component status back to Pass
	ctrlComp.ChangeStatus(ComponentStatus{Status: StatusPass})
	time.Sleep(50 * time.Millisecond)
	if s.Health().Status != StatusPass {
		t.Errorf("Expected service status to be 'pass' after component change, got '%s'", s.Health().Status)
	}
}
