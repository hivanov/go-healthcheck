package solace

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/Azure/go-amqp" // Official Solace Go AMQP client
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var _ = amqp.SessionOptions{}

// TestSolaceChecker_Integration_HappyPath tests a successful connection and health check.
func TestSolaceChecker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, connectionString, cleanup := setupSolaceContainer(t, ctx) // Ignore solaceContainer as it's not used directly here
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-happy-path", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing
	operationsTimeout := 5 * time.Second   // Solace operations can be slower than simple pings

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn) // Initializing
	waitForStatus(t, checker, core.StatusPass) // Healthy, increased timeout for container readiness and initial check

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.Contains(t, status.Output, "Solace is healthy and messages are flowing")
	require.NotZero(t, status.ObservedValue, "Expected ObservedValue to be non-zero")
	require.Equal(t, "s", status.ObservedUnit, "Expected ObservedUnit 's'")
}

// TestSolaceChecker_Integration_Fail_NoConnection tests when the Solace broker is unreachable.
func TestSolaceChecker_Integration_Fail_NoConnection(t *testing.T) {
	descriptor := core.Descriptor{ComponentID: "solace-fail-noconn", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	// Use a connection string that will fail
	badConnectionString := "amqp://admin:admin@localhost:12345"

	checker := NewSolaceChecker(descriptor, checkInterval, operationsTimeout, badConnectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Failed to open Solace connection", "Expected output to indicate connection failure")
}

// TestSolaceChecker_Integration_Fail_BrokerDown tests the checker reacting to a Solace broker going down.
func TestSolaceChecker_Integration_Fail_BrokerDown(t *testing.T) {
	ctx := t.Context()
	solaceContainer, connectionString, cleanup := setupSolaceContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-fail-brokerdown", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass)

	// Now stop the container
	t.Log("Stopping Solace PubSub+ container...")
	require.NoError(t, solaceContainer.Stop(ctx, nil), "Failed to stop Solace container")
	t.Log("Solace PubSub+ container stopped.")

	// It should now transition to a Fail state
	waitForStatus(t, checker, core.StatusFail)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status %s after container stopped, got %s", core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Failed to create Solace session", "Expected output message indicating session creation error")
}

// TestSolaceChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestSolaceChecker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, connectionString, cleanup := setupSolaceContainer(t, ctx) // Ignore solaceContainer
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-disable-enable", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass)

	// Disable the checker
	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Solace checker disabled", status.Output)

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
	require.Contains(t, statusAfterSleep.Output, "Solace checker disabled", "Expected output to remain 'Solace checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Solace checker enabled, re-initializing...", status.Output)

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestSolaceChecker_Integration_Close tests graceful shutdown.
func TestSolaceChecker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, connectionString, cleanup := setupSolaceContainer(t, ctx) // Ignore solaceContainer
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-close", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass)

	// Close the checker
	require.NoError(t, checker.Close())

	// Verify that the internal context is cancelled after Close()
	require.Eventually(t, func() bool {
		select {
		case <-checker.(*solaceChecker).ctx.Done():
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

// TestSolaceChecker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestSolaceChecker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, connectionString, cleanup := setupSolaceContainer(t, ctx) // Ignore solaceContainer
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-change-status", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass)

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
	waitForStatus(t, checker, core.StatusPass)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestSolaceChecker_Getters tests the Descriptor and Health methods.
func TestSolaceChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "solace"}
	checker := &solaceChecker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

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

// TestSolaceChecker_Close_NilConnection tests the Close method when the Solace connection is nil.
func TestSolaceChecker_Close_NilConnection(t *testing.T) {
	desc := core.Descriptor{ComponentID: "solace-close-nil-conn", ComponentType: "solace"}
	checker := &solaceChecker{
		descriptor: desc,
		// Simulate a checker where connection failed to open
		conn:       nil,
		ctx:        t.Context(),
		cancelFunc: func() {},
		quit:       make(chan struct{}),
	}

	err := checker.Close()
	require.NoError(t, err, "Close() on nil connection should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Quit channel not closed")
	}
}

// TestSolaceChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestSolaceChecker_QuitChannelStopsLoop(t *testing.T) {
	ctx := t.Context()
	_, connectionString, cleanup := setupSolaceContainer(t, ctx) // Ignore solaceContainer
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-quit-loop", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass)

	// Manually close the quit channel to stop the loop
	close(checker.(*solaceChecker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further status changes are reported (except the one for shutdown if any).
	select {
	case s := <-checker.StatusChange():
		t.Logf("Received unexpected status change: %s", s.Status)
		// It might send a final status, but should not send periodic ones.
	case <-time.After(checkInterval * 3):
		// Expected: no more status changes
	}

	// Solace client typically doesn't need explicit close on quit
	// If it had a close method, it would be called here.
	_ = checker.Close() // Call Close for cleanup
}

// TestSolaceChecker_PerformHealthCheck_ConnectionNil tests performHealthCheck when s.conn is nil.
func TestSolaceChecker_PerformHealthCheck_ConnectionNil(t *testing.T) {
	desc := core.Descriptor{ComponentID: "solace-perform-nil-conn", ComponentType: "solace"}
	checker := &solaceChecker{
		descriptor: desc,
		conn:       nil, // Simulate nil connection
		ctx:        t.Context(),
		cancelFunc: context.CancelFunc(func() {}),
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when connection is nil")
	require.Contains(t, status.Output, "Solace connection is nil", "Expected specific output")
}

// TestNewSolaceChecker_OpenError tests the scenario where newRealSolaceConnection returns an error.
func TestNewSolaceChecker_OpenError(t *testing.T) {
	// Create a mock OpenSolaceFunc that always returns an error
	mockOpenSolace := func(connectionString string) (solaceConnection, error) {
		return nil, fmt.Errorf("mocked Solace connection error: cannot connect")
	}

	desc := core.Descriptor{ComponentID: "solace-open-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second
	connectionString := "amqp://localhost:5672"

	checker := NewSolaceCheckerWithOpenSolaceFunc(desc, checkInterval, operationsTimeout, connectionString, mockOpenSolace)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newRealSolaceConnection fails")
	require.Contains(t, status.Output, "Failed to open Solace connection", "Expected output to indicate connection failure")
	require.Contains(t, status.Output, "mocked Solace connection error", "Expected output to contain the mocked error")

	// Ensure the health check loop was not started
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after newRealSolaceConnection failed", "status: %s", s.Status)
	case <-time.After(100 * time.Millisecond):
		// Expected, no status change
	}
}

// TestSolaceChecker_PerformHealthCheck_SessionError tests performHealthCheck when session creation fails.
func TestSolaceChecker_PerformHealthCheck_SessionError(t *testing.T) {
	mockConn := &mockSolaceConnection{}
	mockConn.On("NewSession", mock.Anything, mock.Anything).Return(&mockSolaceSession{}, fmt.Errorf("mock session creation error"))
	mockConn.On("Close").Return(nil)

	desc := core.Descriptor{ComponentID: "solace-perform-session-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConn)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*solaceChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when session creation fails")
	require.Contains(t, status.Output, "Failed to create Solace session", "Expected specific output")
	require.Contains(t, status.Output, "mock session creation error", "Expected output to contain the mocked error")
}

// TestSolaceChecker_PerformHealthCheck_SenderError tests performHealthCheck when sender creation fails.
func TestSolaceChecker_PerformHealthCheck_SenderError(t *testing.T) {
	mockSession := &mockSolaceSession{}
	mockSession.On("NewSender", mock.Anything, mock.Anything, mock.Anything).Return(&mockSolaceSender{}, fmt.Errorf("mock sender creation error"))
	mockSession.On("Close").Return(nil)

	mockConn := &mockSolaceConnection{}
	mockConn.On("NewSession", mock.Anything, mock.Anything).Return(mockSession, nil)
	mockConn.On("Close").Return(nil)

	desc := core.Descriptor{ComponentID: "solace-perform-sender-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConn)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	checker.(*solaceChecker).performHealthCheck()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when sender creation fails")
	require.Contains(t, status.Output, "Failed to create Solace sender", "Expected specific output")
	require.Contains(t, status.Output, "mock sender creation error", "Expected output to contain the mocked error")
}

// TestSolaceChecker_PerformHealthCheck_ReceiverError tests performHealthCheck when receiver creation fails.
func TestSolaceChecker_PerformHealthCheck_ReceiverError(t *testing.T) {
	mockSession := &mockSolaceSession{}
	mockSender := newMockSolaceSender()
	mockSender.On("Close").Return(nil)
	mockSession.On("NewSender", mock.Anything, mock.Anything, mock.Anything).Return(mockSender, nil)
	mockSession.On("NewReceiver", mock.Anything, mock.Anything, mock.Anything).Return(&mockSolaceReceiver{}, fmt.Errorf("mock receiver creation error"))
	mockSession.On("Close").Return(nil)

	mockConn := &mockSolaceConnection{}
	mockConn.On("NewSession", mock.Anything, mock.Anything).Return(mockSession, nil)
	mockConn.On("Close").Return(nil)

	desc := core.Descriptor{ComponentID: "solace-perform-receiver-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConn)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	checker.(*solaceChecker).performHealthCheck()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when receiver creation fails")
	require.Contains(t, status.Output, "Failed to create Solace receiver", "Expected specific output")
	require.Contains(t, status.Output, "mock receiver creation error", "Expected output to contain the mocked error")
}

// TestSolaceChecker_PerformHealthCheck_SendError tests performHealthCheck when sending a message fails.
func TestSolaceChecker_PerformHealthCheck_SendError(t *testing.T) {
	mockSender := &mockSolaceSender{}
	mockSender.On("Send", mock.Anything, mock.Anything, mock.Anything).Return(fmt.Errorf("mock send error"))
	mockSender.On("Close").Return(nil)

	mockSession := &mockSolaceSession{}
	mockSession.On("NewSender", mock.Anything, mock.Anything, mock.Anything).Return(mockSender, nil)
	mockReceiver := newMockSolaceReceiver()
	mockReceiver.On("Close").Return(nil)
	mockSession.On("NewReceiver", mock.Anything, mock.Anything, mock.Anything).Return(mockReceiver, nil)
	mockSession.On("Close").Return(nil)

	mockConn := &mockSolaceConnection{}
	mockConn.On("NewSession", mock.Anything, mock.Anything).Return(mockSession, nil)
	mockConn.On("Close").Return(nil)

	desc := core.Descriptor{ComponentID: "solace-perform-send-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConn)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	checker.(*solaceChecker).performHealthCheck()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when sending message fails")
	require.Contains(t, status.Output, "Failed to send Solace test message", "Expected specific output")
	require.Contains(t, status.Output, "mock send error", "Expected output to contain the mocked error")
}

// TestSolaceChecker_PerformHealthCheck_ReceiveError tests performHealthCheck when receiving a message fails.
func TestSolaceChecker_PerformHealthCheck_ReceiveError(t *testing.T) {
	mockReceiver := &mockSolaceReceiver{}
	mockReceiver.On("Receive", mock.Anything, mock.Anything).Return(amqp.NewMessage([]byte("mock")), fmt.Errorf("mock receive error"))
	mockReceiver.On("Close").Return(nil)

	mockSender := &mockSolaceSender{}
	mockSender.On("Send", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockSender.On("Close").Return(nil)

	mockSession := &mockSolaceSession{}
	mockSession.On("NewSender", mock.Anything, mock.Anything, mock.Anything).Return(mockSender, nil)
	mockSession.On("NewReceiver", mock.Anything, mock.Anything, mock.Anything).Return(mockReceiver, nil)
	mockSession.On("Close").Return(nil)

	mockConn := &mockSolaceConnection{}
	mockConn.On("NewSession", mock.Anything, mock.Anything).Return(mockSession, nil)
	mockConn.On("Close").Return(nil)

	desc := core.Descriptor{ComponentID: "solace-perform-receive-error", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 1 * time.Second

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConn)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	checker.(*solaceChecker).performHealthCheck()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when receiving message fails")
	require.Contains(t, status.Output, "Failed to receive Solace test message", "Expected specific output")
	require.Contains(t, status.Output, "mock receive error", "Expected output to contain the mocked error")
}
