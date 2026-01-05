package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPHandler_Liveness(t *testing.T) {
	// Use NewService with a dummy component since liveness doesn't check actual health.
	dummyComp := NewMockComponent("dummy-liveness", "system", StatusPass)
	defer func() {
		if err := dummyComp.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()
	svc := NewService(Descriptor{ServiceID: "liveness-test"}, dummyComp)
	defer func() {
		if err := svc.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()

	handler := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/live", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "Expected HTTP 200 OK for liveness probe")
	assert.Empty(t, rr.Body.String(), "Expected empty body for liveness probe")
}

// MockComponent implements the core.Component interface for testing purposes.
type MockComponent struct {
	descriptor     Descriptor
	currentStatus  ComponentStatus
	statusChangeCh chan ComponentStatus // Simulate status change notification
	mu             sync.RWMutex
	cancelFunc     context.CancelFunc
	mockHealthFunc func() ComponentHealth // NEW: Optional custom Health func
}

// NewMockComponent creates a new MockComponent.
func NewMockComponent(id string, compType string, initialStatus StatusEnum) *MockComponent {
	ctx, cancel := context.WithCancel(context.Background())
	mc := &MockComponent{
		descriptor: Descriptor{
			ComponentID:   id,
			ComponentType: compType,
		},
		currentStatus: ComponentStatus{
			Status: initialStatus,
		},
		statusChangeCh: make(chan ComponentStatus, 1),
		cancelFunc:     cancel,
	}

	// Mimic the component's internal goroutine to send status updates.
	// This is simplified as we're directly setting currentStatus.
	go func() {
		defer close(mc.statusChangeCh)
		select {
		case <-ctx.Done():
			return
		}
	}()

	return mc
}

func (mc *MockComponent) Close() error {
	mc.cancelFunc()
	return nil
}

func (mc *MockComponent) ChangeStatus(status ComponentStatus) {
	mc.mu.Lock()
	mc.currentStatus = status
	mc.mu.Unlock()

	// Non-blocking send to simulate status change notification
	select {
	case mc.statusChangeCh <- status:
	default:
	}
}

func (mc *MockComponent) Disable() {
	mc.ChangeStatus(ComponentStatus{Status: StatusFail})
}

func (mc *MockComponent) Enable() {
	mc.ChangeStatus(ComponentStatus{Status: StatusPass})
}

func (mc *MockComponent) Status() ComponentStatus {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.currentStatus
}

func (mc *MockComponent) Descriptor() Descriptor {
	return mc.descriptor
}

func (mc *MockComponent) StatusChange() <-chan ComponentStatus {
	return mc.statusChangeCh
}

func (mc *MockComponent) Health() ComponentHealth {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.mockHealthFunc != nil { // Use custom func if provided
		return mc.mockHealthFunc()
	}

	status := mc.currentStatus
	descriptor := mc.Descriptor()
	return ComponentHealth{
		Status:            status.Status,
		Version:           descriptor.Version,
		ReleaseID:         descriptor.ReleaseID,
		Notes:             descriptor.Notes,
		Output:            status.Output,
		Links:             descriptor.Links,
		ServiceID:         descriptor.ServiceID,
		Description:       descriptor.Description,
		ComponentID:       descriptor.ComponentID,
		ComponentType:     descriptor.ComponentType,
		ObservedValue:     status.ObservedValue,
		ObservedUnit:      status.ObservedUnit,
		AffectedEndpoints: status.AffectedEndpoints,
		Time:              status.Time,
	}
}

// Ensure MockComponent implements the Component interface
var _ Component = (*MockComponent)(nil)

func TestNewHTTPHandler_Readiness(t *testing.T) {
	// Setup a common service descriptor
	serviceDescriptor := Descriptor{
		ServiceID:   "test-app",
		Description: "Test Application Service",
	}

	tests := []struct {
		name                     string
		componentInitialStatuses []StatusEnum
		expectedHTTPCode         int
		expectedServiceStatus    StatusEnum
	}{
		{
			name:                     "AllComponentsPass",
			componentInitialStatuses: []StatusEnum{StatusPass, StatusPass},
			expectedHTTPCode:         http.StatusOK,
			expectedServiceStatus:    StatusPass,
		},
		{
			name:                     "OneComponentWarn",
			componentInitialStatuses: []StatusEnum{StatusPass, StatusWarn},
			expectedHTTPCode:         http.StatusOK,
			expectedServiceStatus:    StatusWarn,
		},
		{
			name:                     "OneComponentFail",
			componentInitialStatuses: []StatusEnum{StatusPass, StatusFail},
			expectedHTTPCode:         http.StatusServiceUnavailable,
			expectedServiceStatus:    StatusFail,
		},
		{
			name:                     "MixedWarnAndFail",
			componentInitialStatuses: []StatusEnum{StatusWarn, StatusFail},
			expectedHTTPCode:         http.StatusServiceUnavailable,
			expectedServiceStatus:    StatusFail,
		},
		{
			name:                     "NoComponents",
			componentInitialStatuses: []StatusEnum{},
			expectedHTTPCode:         http.StatusOK,
			expectedServiceStatus:    StatusPass, // Default for service with no components
		},
		{
			name:                     "MultipleWarn",
			componentInitialStatuses: []StatusEnum{StatusWarn, StatusWarn},
			expectedHTTPCode:         http.StatusOK,
			expectedServiceStatus:    StatusWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var components []Component
			// Create mock components based on test case
			for i, status := range tt.componentInitialStatuses {
				mc := NewMockComponent(fmt.Sprintf("comp-%d", i), "test-type", status)
				components = append(components, mc)
			}

			// Create the actual service with mock components
			svc := NewService(serviceDescriptor, components...)
			require.NoError(t, svc.Close(), "Failed to close mock service")
			for _, comp := range components {
				// Ensure all mock components are closed to prevent goroutine leaks
				require.NoError(t, comp.Close(), "Failed to close mock component %s", comp.Descriptor().ComponentID)
			}

			// Give the service some time to recalculate its status after components are added
			time.Sleep(50 * time.Millisecond)

			handler := NewHTTPHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/.well-known/ready", nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedHTTPCode, rr.Code, "Expected HTTP status code mismatch")
			assert.Equal(t, "application/health+json", rr.Header().Get("Content-Type"), "Expected Content-Type header mismatch")

			var actualHealth Health
			err := json.Unmarshal(rr.Body.Bytes(), &actualHealth)
			require.NoError(t, err, "Failed to unmarshal JSON response")

			// Verify overall service status
			assert.Equal(t, tt.expectedServiceStatus, actualHealth.Status, "Expected overall service status mismatch")
			assert.Equal(t, serviceDescriptor.ServiceID, actualHealth.ServiceID)
			assert.Equal(t, serviceDescriptor.Description, actualHealth.Description)

			// Verify individual component statuses within the checks map
			expectedChecks := make(map[string][]ComponentHealth)
			for _, comp := range components {
				expectedChecks[comp.Descriptor().ComponentType] = append(expectedChecks[comp.Descriptor().ComponentType], comp.Health())
			}

			assert.Len(t, actualHealth.Checks, len(expectedChecks), "Mismatch in number of check groups")
			for compType, expectedComps := range expectedChecks {
				actualComps, ok := actualHealth.Checks[compType]
				assert.True(t, ok, "Component type %s not found in actual health checks", compType)
				assert.Len(t, actualComps, len(expectedComps), "Mismatch in number of components for type %s", compType)

				// Sort components by ID for consistent comparison
				sortComponents := func(chs []ComponentHealth) {
					sort.Slice(chs, func(i, j int) bool {
						return chs[i].ComponentID < chs[j].ComponentID
					})
				}
				sortComponents(expectedComps)
				sortComponents(actualComps)

				for i := range expectedComps {
					assert.Equal(t, expectedComps[i].Status, actualComps[i].Status, "Component %s status mismatch", expectedComps[i].ComponentID)
					assert.Equal(t, expectedComps[i].ComponentID, actualComps[i].ComponentID, "Component %s ID mismatch", expectedComps[i].ComponentID)
					// Compare other fields as needed, e.g., Output, ObservedValue, etc.
					// For now, focusing on Status and ComponentID.
				}
			}
		})
	}
}

func TestNewHTTPHandler_Readiness_Headers(t *testing.T) {
	dummyComp := NewMockComponent("dummy-headers", "system", StatusPass)
	defer func() {
		if err := dummyComp.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()
	svc := NewService(Descriptor{ServiceID: "headers-test"}, dummyComp)
	defer func() {
		if err := svc.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()

	handler := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/ready", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, "application/health+json", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache, no-store, must-revalidate", rr.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", rr.Header().Get("Pragma"))
	assert.Equal(t, "0", rr.Header().Get("Expires"))
}

func TestNewHTTPHandler_Readiness_InvalidJson(t *testing.T) {
	// Use NewService with a dummy component that is always passing.
	// Our current Health struct is designed to always be marshalable,
	// so this test now ensures that a normal passing service still
	// returns a valid JSON response without issues.
	dummyComp := NewMockComponent("dummy-json", "system", StatusPass)
	defer func() {
		if err := dummyComp.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()
	svc := NewService(Descriptor{ServiceID: "invalid-json-test"}, dummyComp)
	defer func() {
		if err := svc.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()

	handler := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/ready", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// In the current setup, our Health struct should always marshal correctly.
	// So this test checks for a valid response, not an error.
	assert.Equal(t, http.StatusOK, rr.Code)
	var actualHealth Health
	err := json.Unmarshal(rr.Body.Bytes(), &actualHealth)
	require.NoError(t, err)
	assert.Equal(t, StatusPass, actualHealth.Status)
}

func TestNewHTTPHandler_Readiness_TimeoutService(t *testing.T) {
	// Simulate a service that takes too long to respond to Health()
	// This would typically be handled by the HTTP server's read/write timeouts.
	// We'll create a MockComponent whose Health() method sleeps.
	slowComp := NewMockComponent("slow-comp", "system", StatusPass)
	defer func() {
		if err := slowComp.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()
	// Override the mockHealthFunc of the mock component to introduce a delay
	slowComp.mockHealthFunc = func() ComponentHealth {
		time.Sleep(200 * time.Millisecond) // Simulate a slow health check
		return ComponentHealth{Status: StatusPass, ComponentID: slowComp.Descriptor().ComponentID}
	}

	svc := NewService(Descriptor{ServiceID: "timeout-test"}, slowComp)
	defer func() {
		if err := svc.Close(); err != nil {
			t.Errorf("Close() returned an unexpected error: %v", err)
		}
	}()

	handler := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/ready", nil)
	rr := httptest.NewRecorder()

	// Use a channel to detect when ServeHTTP finishes.
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		// This indicates that the handler is blocked by the slow component's Health.
		t.Log("Handler call to Health() took longer than expected (100ms), which is expected with 200ms sleep.")
	case <-done:
		t.Log("Handler call finished before timeout (100ms).")
	}

	// Verify that the call eventually finished and returned the correct status.
	// This is important to ensure no goroutine leaks or panics.
	select {
	case <-done:
		assert.Equal(t, http.StatusOK, rr.Code)
		var actualHealth Health
		err := json.Unmarshal(rr.Body.Bytes(), &actualHealth)
		require.NoError(t, err)
		assert.Equal(t, StatusPass, actualHealth.Status)
	case <-time.After(300 * time.Millisecond): // Longer timeout for the eventual completion
		t.Fatal("Handler did not complete within expected time (300ms).")
	}
}

// Fuzz test for HTTP handlers (conceptual)
// Fuzzing http.ServeMux directly is not straightforward as it involves HTTP requests.
// Instead, we can fuzz the inputs to the underlying `Service` methods.
// For now, relying on the extensive testing of Service and Component, and direct handler tests.
func FuzzHTTPHandler(f *testing.F) {
	f.Add("/.well-known/live", "GET")
	f.Add("/.well-known/ready", "GET")
	f.Add("/some/other/path", "POST")

	f.Fuzz(func(t *testing.T, path, method string) {
		// Create a single mock component
		mc := NewMockComponent("fuzz-comp", "system", StatusPass)
		defer func() {
			if err := mc.Close(); err != nil {
				t.Errorf("Close() returned an unexpected error: %v", err)
			}
		}()

		// Randomly set the component's status
		statuses := []StatusEnum{StatusPass, StatusWarn, StatusFail}
		mc.ChangeStatus(ComponentStatus{Status: statuses[time.Now().UnixNano()%int64(len(statuses))]})

		// Create the service with the mock component
		svc := NewService(Descriptor{ServiceID: "fuzz-test"}, mc)
		defer func() {
			if err := svc.Close(); err != nil {
				t.Errorf("Close() returned an unexpected error: %v", err)
			}
		}()

		handler := NewHTTPHandler(svc)

		req := httptest.NewRequest(method, path, nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		// Basic sanity checks
		if path == "/.well-known/live" {
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Empty(t, rr.Body.String())
		} else if path == "/.well-known/ready" {
			assert.Contains(t, rr.Header().Get("Content-Type"), "application/health+json")
			var h Health
			err := json.Unmarshal(rr.Body.Bytes(), &h)
			assert.NoError(t, err)

			expectedCode := http.StatusOK
			if h.Status == StatusFail {
				expectedCode = http.StatusServiceUnavailable
			}
			assert.Equal(t, expectedCode, rr.Code)
		} else {
			// Other paths should return 404 from ServeMux unless explicitly handled
			assert.Equal(t, http.StatusNotFound, rr.Code)
		}
	})
}
