package mongodb

import (
	"context"
	"healthcheck/core"
	"testing"
	"time"
)

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(t *testing.T, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	statusChangeChan := checker.StatusChange()

	// Check current status first
	if checker.Status().Status == expectedStatus {
		return
	}

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, checker.Status().Status, checker.Status().Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
		case <-time.After(5 * time.Millisecond):
			if checker.Status().Status == expectedStatus {
				return
			}
		}
	}
}

// TestWaitForStatus_InitialStatusIsExpected tests the waitForStatus helper when the initial status is the expected one.
func TestWaitForStatus_InitialStatusIsExpected(t *testing.T) {
	checker := &mongoChecker{
		currentStatus: core.ComponentStatus{Status: core.StatusPass},
	}
	// This should return immediately
	waitForStatus(t, checker, core.StatusPass, 1*time.Second)
}
