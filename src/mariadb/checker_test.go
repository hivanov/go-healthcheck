package mariadb

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDBConnection is a mock implementation of dbConnection for testing purposes.
type mockDBConnection struct {
	queryRowContextFunc func(ctx context.Context, query string, args ...interface{}) rowScanner
	closeFunc           func() error
}

func (m *mockDBConnection) QueryRowContext(ctx context.Context, query string, args ...interface{}) rowScanner {
	if m.queryRowContextFunc != nil {
		return m.queryRowContextFunc(ctx, query, args...)
	}
	return &mockRowScanner{} // Default mock row scanner
}

func (m *mockDBConnection) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil // Default close behavior
}

// mockRowScanner is a mock implementation of rowScanner for testing purposes.
type mockRowScanner struct {
	scanFunc func(dest ...interface{}) error
}

func (m *mockRowScanner) Scan(dest ...interface{}) error {
	if m.scanFunc != nil {
		return m.scanFunc(dest...)
	}
	// Default scan behavior: pretend to scan a '1' successfully
	if len(dest) > 0 {
		if ptr, ok := dest[0].(*int); ok {
			*ptr = 1
		}
	}
	return nil
}

// TestMariaDBChecker_Integration_HappyPath tests a successful connection and health check.
func TestMariaDBChecker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-happy-path", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn, 1*time.Second) // Initializing
	waitForStatus(t, checker, core.StatusPass, 5*time.Second) // Healthy, increased timeout for container readiness

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status, "Expected final status to be 'pass'")
	require.NotEmpty(t, status.Output, "Expected output message")
	require.NotZero(t, status.ObservedValue, "Expected ObservedValue to be non-zero")
	require.Equal(t, "s", status.ObservedUnit, "Expected ObservedUnit 's'")
}

// TestMariaDBChecker_Integration_Fail_NoConnection tests when the database is unreachable.
func TestMariaDBChecker_Integration_Fail_NoConnection(t *testing.T) {
	// No container needed, just an invalid connection string
	invalidConnStr := "user=baduser dbname=baddb host=localhost port=1234"

	desc := core.Descriptor{ComponentID: "mariadb-fail-noconn", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, invalidConnStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail, 1*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status to be 'fail'")
	require.NotEmpty(t, status.Output, "Expected output message")
}

// TestMariaDBChecker_Integration_Fail_DBDown tests the checker reacting to a database going down.
func TestMariaDBChecker_Integration_Fail_DBDown(t *testing.T) {
	ctx := t.Context()
	mariadbContainer, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-fail-dbdown", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Now stop the container
	t.Log("Stopping MariaDB container...")
	if err := mariadbContainer.Stop(ctx, nil); err != nil { // Use Stop instead of Terminate for cleaner restart potential in other tests (though not used here)
		t.Fatalf("Failed to stop MariaDB container: %v", err)
	}
	t.Log("MariaDB container stopped.")

	// It should now transition to a Fail state
	waitForStatus(t, checker, core.StatusFail, 2*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status 'fail' after container stopped")
	require.NotEmpty(t, status.Output, "Expected output message indicating connection error")
}

// TestMariaDBChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestMariaDBChecker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-disable-enable", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Disable the checker
	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status, "Expected status 'warn' after Disable")
	require.Equal(t, "MariaDB checker disabled", status.Output, "Expected output 'MariaDB checker disabled' after Disable")

	// Ensure no status changes occur while disabled
	select {
	case newStatus := <-checker.StatusChange():
		assert.Equal(t, core.StatusWarn, newStatus.Status, "Unexpected status change to %s while checker was disabled", newStatus.Status)
	case <-time.After(checkInterval * 3):
		// Expected, no status change
	}

	// Give performHealthCheck a chance to run while disabled
	time.Sleep(checkInterval * 2) // Allow a few ticks

	// After sleeping, the status should still be "disabled" and not changed by performHealthCheck
	statusAfterSleep := checker.Status()
	assert.Equal(t, core.StatusWarn, statusAfterSleep.Status, "Expected status to remain Warn after performHealthCheck while disabled")
	assert.Contains(t, statusAfterSleep.Output, "MariaDB checker disabled", "Expected output to remain 'MariaDB checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status, "Expected status 'warn' after Enable")
	require.Equal(t, "MariaDB checker enabled, re-initializing...", status.Output, "Expected output 'MariaDB checker enabled, re-initializing...' after Enable")

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status, "Expected status 'pass' after re-enable")
}

// TestMariaDBChecker_Integration_Close tests graceful shutdown.
func TestMariaDBChecker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup() // This ensures container is terminated even if checker.Close() fails

	desc := core.Descriptor{ComponentID: "mariadb-close", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Close the checker
	err := checker.Close()
	require.NoError(t, err, "Close() returned an unexpected error")

	// Verify that the internal context is cancelled after Close()
	select {
	case <-checker.(*mariadbChecker).ctx.Done(): // Access internal context to verify cancellation
		// Expected, context was cancelled by Close()
	case <-time.After(500 * time.Millisecond):
		t.Error("Checker context was not cancelled after Close() within expected time")
	}

	// Ensure no further status changes are reported by the checker's goroutine
	select {
	case s := <-checker.StatusChange():
		t.Fatalf("Received unexpected status change '%s' after checker was closed", s.Status)
	case <-time.After(checkInterval * 3): // Wait a bit to ensure no more ticks
		// Expected, no more status changes
	}
}

// TestMariaDBChecker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestMariaDBChecker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-change-status", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Manually change status to Warn
	manualWarnStatus := core.ComponentStatus{Status: core.StatusWarn, Output: "Manual override to Warn"}
	checker.ChangeStatus(manualWarnStatus)

	status := checker.Status()
	require.Equal(t, manualWarnStatus.Status, status.Status, "Expected status after manual change")
	require.Equal(t, manualWarnStatus.Output, status.Output, "Expected output after manual change")

	// Verify the manual status change is broadcast
	select {
	case s := <-checker.StatusChange():
		require.Equal(t, manualWarnStatus.Status, s.Status, "Expected status from channel")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for manual status change notification")
	}

	// The periodic check should eventually revert it to Pass
	waitForStatus(t, checker, core.StatusPass, 1*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status, "Expected status after periodic check")
}

// TestMariaDBChecker_Getters tests the Descriptor and Health methods.
func TestMariaDBChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "test-type"}
	checker := &mariadbChecker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

	// Test Descriptor()
	actualDesc := checker.Descriptor()
	assert.Equal(t, desc, actualDesc, "Descriptor() should return the correct descriptor")

	// Test Health()
	health := checker.Health()
	assert.Equal(t, desc.ComponentID, health.ComponentID, "Health().ComponentID should match")
	assert.Equal(t, desc.ComponentType, health.ComponentType, "Health().ComponentType should match")
	assert.Equal(t, core.StatusPass, health.Status, "Health().Status should match current status")
	assert.Equal(t, "Everything is fine", health.Output, "Health().Output should match current status output")

	// Test Health() with empty output
	checker.currentStatus.Output = ""
	health = checker.Health()
	assert.Empty(t, health.Output, "Health().Output should be empty if current status output is empty")
}

// TestMariaDBChecker_Close_NilDB tests the Close method when the internal *sql.DB is nil.
func TestMariaDBChecker_Close_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "mariadb-close-nil-db", ComponentType: "mariadb"}
	checker := &mariadbChecker{
		descriptor: desc,
		// Simulate a checker where DB connection failed to open
		db:         nil,
		ctx:        context.Background(),
		cancelFunc: func() {},
		quit:       make(chan struct{}),
	}

	err := checker.Close()
	require.NoError(t, err, "Close() on nil DB should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Quit channel not closed")
	}
}

// TestMariaDBChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestMariaDBChecker_QuitChannelStopsLoop(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMariaDBContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mariadb-quit-loop", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMariaDBChecker(desc, checkInterval, 1*time.Second, connStr)
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Manually close the quit channel to stop the loop
	close(checker.(*mariadbChecker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further status changes are reported (except the one for shutdown if any).
	select {
	case s := <-checker.StatusChange():
		t.Logf("Received status change: %s", s.Status)
		// It might send a final status, but should not send periodic ones.
	case <-time.After(checkInterval * 3):
		// Expected: no more status changes after loop termination.
	}

	// Clean up the DB connection that was opened by the checker.
	if checker.(*mariadbChecker).db != nil {
		err := checker.(*mariadbChecker).db.Close()
		require.NoError(t, err, "Error closing database connection after quit")
	}
}

// TestMariaDBChecker_PerformHealthCheck_NilDB tests performHealthCheck when m.db is nil.
func TestMariaDBChecker_PerformHealthCheck_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "mariadb-perform-nil-db", ComponentType: "mariadb"}
	checker := &mariadbChecker{
		descriptor: desc,
		db:         nil, // Simulate nil DB connection
		ctx:        context.Background(),
		cancelFunc: context.CancelFunc(func() {}),
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected status Fail when DB is nil")
	assert.Contains(t, status.Output, "Database connection object is nil", "Expected specific output")
}

// TestNewMariaDBChecker_OpenError tests the scenario where newSQLDBConnection (wrapping sql.Open) returns an error.
func TestNewMariaDBChecker_OpenError(t *testing.T) {
	// Create a mock OpenDBFunc that always returns an error
	mockOpenDB := func(driverName, connectionString string) (dbConnection, error) {
		return nil, fmt.Errorf("mocked sql.Open error: cannot connect to %s", connectionString)
	}

	desc := core.Descriptor{ComponentID: "mariadb-sql-open-error", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond
	connectionString := "some-conn-string" // The content doesn't matter, as our mock will return an error

	checker := NewMariaDBCheckerWithOpenDBFunc(desc, checkInterval, 1*time.Second, connectionString, mockOpenDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newSQLDBConnection fails")
	require.Contains(t, status.Output, "Failed to open DB connection", "Expected output to indicate DB connection failure")
	require.Contains(t, status.Output, "mocked sql.Open error", "Expected output to contain the mocked error")

	// Ensure the health check loop was not started
	select {
	case s := <-checker.StatusChange():
		t.Fatalf("Received unexpected status change '%s' after newSQLDBConnection failed", s.Status)
	case <-time.After(100 * time.Millisecond):
		// Expected, no status change
	}
}

// TestNewSQLDBConnection_OpenError tests newSQLDBConnection when sql.Open returns an error.
func TestNewSQLDBConnection_OpenError(t *testing.T) {
	// Use an invalid driver name to force sql.Open to return an error immediately
	invalidDriverName := "non-existent-driver"
	connStr := "some-connection-string"

	dbConn, err := newSQLDBConnection(invalidDriverName, connStr)
	require.Error(t, err, "Expected an error when opening DB with an invalid driver name")
	require.Nil(t, dbConn, "Expected dbConn to be nil when sql.Open fails")
	require.Contains(t, err.Error(), "sql: unknown driver", "Expected error message to indicate unknown driver")
}

// TestMariaDBChecker_PerformHealthCheck_ResultNotOne tests performHealthCheck when the query returns a result other than 1.
func TestMariaDBChecker_PerformHealthCheck_ResultNotOne(t *testing.T) {
	mockRow := &mockRowScanner{
		scanFunc: func(dest ...interface{}) error {
			if len(dest) > 0 {
				if ptr, ok := dest[0].(*int); ok {
					*ptr = 0 // Simulate result is not 1
				}
			}
			return nil
		},
	}

	mockDB := &mockDBConnection{
		queryRowContextFunc: func(ctx context.Context, query string, args ...interface{}) rowScanner {
			return mockRow
		},
		closeFunc: func() error {
			return nil
		},
	}

	desc := core.Descriptor{ComponentID: "mariadb-perform-result-not-one", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := newMariaDBCheckerInternal(desc, checkInterval, 1*time.Second, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*mariadbChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when result is not 1")
	require.Contains(t, status.Output, "MariaDB health check query returned unexpected result: 0 (expected 1)", "Expected specific output")
}

// TestMariaDBChecker_PerformHealthCheck_ScanError tests performHealthCheck when row.Scan returns an error.
func TestMariaDBChecker_PerformHealthCheck_ScanError(t *testing.T) {
	mockRow := &mockRowScanner{
		scanFunc: func(dest ...interface{}) error {
			return fmt.Errorf("mocked scan error")
		},
	}

	mockDB := &mockDBConnection{
		queryRowContextFunc: func(ctx context.Context, query string, args ...interface{}) rowScanner {
			return mockRow
		},
		closeFunc: func() error {
			return nil
		},
	}

	desc := core.Descriptor{ComponentID: "mariadb-perform-scan-error", ComponentType: "mariadb"}
	checkInterval := 50 * time.Millisecond

	checker := newMariaDBCheckerInternal(desc, checkInterval, 1*time.Second, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*mariadbChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when scan returns an error")
	require.Contains(t, status.Output, "MariaDB health check query failed", "Expected specific output")
	require.Contains(t, status.Output, "mocked scan error", "Expected output to contain the mocked error")
}
