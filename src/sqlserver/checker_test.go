package sqlserver

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

// TestSQLServerChecker_Getters tests the Descriptor and Health methods.
func TestSQLServerChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "test-type"}
	checker := &sqlServerChecker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

	// Test Descriptor()
	actualDesc := checker.Descriptor()
	require.Equal(t, desc, actualDesc, "Descriptor() should return the correct descriptor")

	// Test Health()
	health := checker.Health()
	require.Equal(t, desc.ComponentID, health.ComponentID, "Health().ComponentID should match")
	require.Equal(t, desc.ComponentType, health.ComponentType, "Health().ComponentType should match")
	require.Equal(t, core.StatusPass, health.Status, "Health().Status should match current status")
	require.Equal(t, "Everything is fine", health.Output, "Health().Output should be empty if current status output is empty")
}

// TestSQLServerChecker_Integration_HappyPath tests a successful connection and health check.
func TestSQLServerChecker_Integration_HappyPath(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-happy-path", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout for SQL Server
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn, 1*time.Second)  // Initializing
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Healthy, increased timeout for SQL Server startup

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.NotEmpty(t, status.Output, "Expected output message")
	require.NotZero(t, status.ObservedValue, "Expected ObservedValue to be non-zero")
	require.Equal(t, "s", status.ObservedUnit, "Expected ObservedUnit 's'")
}

// TestSQLServerChecker_Integration_Fail_NoConnection tests when the database is unreachable.
func TestSQLServerChecker_Integration_Fail_NoConnection(t *testing.T) {
	// No container needed, just an invalid connection string
	invalidConnStr := "sqlserver://baduser:baddb@localhost:1234?database=baddb&encrypt=disable" // Example invalid connection string for SQL Server

	desc := core.Descriptor{ComponentID: "sql-fail-noconn", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, invalidConnStr) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail, 10*time.Second) // Increased timeout for SQL Server connection attempts

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.NotEmpty(t, status.Output, "Expected output message")
}

// TestSQLServerChecker_Integration_Fail_DBDown tests the checker reacting to a database going down.
func TestSQLServerChecker_Integration_Fail_DBDown(t *testing.T) {
	mssqlContainer, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-fail-dbdown", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

	// Now stop the container
	t.Log("Stopping Microsoft SQL Server container...")
	require.NoError(t, mssqlContainer.Stop(t.Context(), nil), "Failed to stop Microsoft SQL Server container")
	t.Log("Microsoft SQL Server container stopped.")

	// It should now transition to a Fail state
	waitForStatus(t, checker, core.StatusFail, 10*time.Second) // Increased timeout

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status %s after container stopped, got %s", core.StatusFail, status.Status)
	require.NotEmpty(t, status.Output, "Expected output message indicating connection error")
}

// TestSQLServerChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestSQLServerChecker_Integration_DisableEnable(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-disable-enable", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

	// Disable the checker
	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Microsoft SQL Server checker disabled", status.Output)

	// Ensure no status changes occur while disabled
	select {
	case newStatus := <-checker.StatusChange():
		require.Equal(t, core.StatusWarn, newStatus.Status, "Unexpected status change to %s while checker was disabled", newStatus.Status)
	case <-time.After(checkInterval * 3):
		// Expected, no status change
	}

	// The status should still be "disabled" and not changed by performHealthCheck
	statusAfterSleep := checker.Status()
	require.Equal(t, core.StatusWarn, statusAfterSleep.Status, "Expected status to remain Warn after performHealthCheck while disabled")
	require.Contains(t, statusAfterSleep.Output, "Microsoft SQL Server checker disabled", "Expected output to remain 'Microsoft SQL Server checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "Microsoft SQL Server checker enabled, re-initializing...", status.Output)

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass, 10*time.Second) // Increased timeout

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestSQLServerChecker_Integration_Close tests graceful shutdown.
func TestSQLServerChecker_Integration_Close(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup() // This ensures container is terminated even if checker.Close() fails

	desc := core.Descriptor{ComponentID: "sql-close", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

	// Close the checker
	require.NoError(t, checker.Close())

	// Verify that the internal context is cancelled after Close()
	require.Eventually(t, func() bool {
		select {
		case <-checker.(*sqlServerChecker).ctx.Done():
			return true
		default:
			return false
		}
	}, 500*time.Millisecond, 10*time.Millisecond, "Checker context was not cancelled after Close()")

	// Ensure no further actual status changes are reported by the checker's goroutine
	select {
	case s, ok := <-checker.StatusChange():
		if ok { // received a value, channel is not closed
			require.Fail(t, "Received unexpected actual status change after checker was closed", "status: %s", s.Status)
		}
		// else: channel was closed, received zero value, which is expected.
	case <-time.After(checkInterval * 3):
		// Expected, no more status changes or channel already closed and drained.
	}
}

// TestSQLServerChecker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestSQLServerChecker_Integration_ChangeStatus(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-change-status", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

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
	waitForStatus(t, checker, core.StatusPass, 10*time.Second) // Increased timeout

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestSQLServerChecker_Close_NilDB tests the Close method when the internal *sql.DB is nil.
func TestSQLServerChecker_Close_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "sql-close-nil-db", ComponentType: "sqlserver"}
	checker := &sqlServerChecker{
		descriptor:       desc,
		db:               nil, // Simulate nil DB connection
		ctx:              t.Context(),
		cancelFunc:       func() {},
		quit:             make(chan struct{}),
		statusChangeChan: make(chan core.ComponentStatus, 1),
	}

	err := checker.Close()
	require.NoError(t, err, "Close() on nil DB should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Quit channel not closed")
	}
}

// TestSQLServerChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestSQLServerChecker_QuitChannelStopsLoop(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-quit-loop", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

	// Manually close the quit channel to stop the loop
	close(checker.(*sqlServerChecker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further actual status changes are reported by the checker's goroutine
	select {
	case s, ok := <-checker.StatusChange():
		if ok { // received a value, channel is not closed
			require.Fail(t, "Received unexpected actual status change after checker was closed", "status: %s", s.Status)
		}
		// else: channel was closed, received zero value, which is expected.
	case <-time.After(checkInterval * 3):
		// Expected, no more status changes or channel already closed and drained.
	}

	// Clean up the DB connection that was opened by the checker.
	if checker.(*sqlServerChecker).db != nil {
		err := checker.(*sqlServerChecker).db.Close()
		require.NoError(t, err, "Error closing database connection after quit")
	}
}

// TestSQLServerChecker_CancelContextStopsLoop tests that canceling the context stops the health check loop.
func TestSQLServerChecker_CancelContextStopsLoop(t *testing.T) {
	_, connStr, cleanup := setupSQLServerContainer(t, t.Context())
	defer cleanup()

	desc := core.Descriptor{ComponentID: "sql-context-loop", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := NewSQLServerChecker(desc, checkInterval, 10*time.Second, connStr) // Increased query timeout
	// Do not defer checker.Close() here, as we want to manually cancel the context.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 30*time.Second) // Increased timeout

	// Manually cancel the context to stop the loop
	checker.(*sqlServerChecker).cancelFunc()

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further actual status changes are reported by the checker's goroutine
	select {
	case s, ok := <-checker.StatusChange():
		if ok { // received a value, channel is not closed
			require.Fail(t, "Received unexpected actual status change after checker was closed", "status: %s", s.Status)
		}
		// else: channel was closed, received zero value, which is expected.
	case <-time.After(checkInterval * 3):
		// Expected, no more status changes or channel already closed and drained.
	}

	// Clean up the DB connection that was opened by the checker.
	if checker.(*sqlServerChecker).db != nil {
		err := checker.(*sqlServerChecker).db.Close()
		require.NoError(t, err, "Error closing database connection after quit")
	}
}

// TestSQLServerChecker_PerformHealthCheck_NilDB tests performHealthCheck when p.db is nil.
func TestSQLServerChecker_PerformHealthCheck_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "sql-perform-nil-db", ComponentType: "sqlserver"}
	checker := &sqlServerChecker{
		descriptor:       desc,
		db:               nil, // Simulate nil DB connection
		ctx:              t.Context(),
		cancelFunc:       context.CancelFunc(func() {}),
		statusChangeChan: make(chan core.ComponentStatus, 1),
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when DB is nil")
	require.Contains(t, status.Output, "Database connection object is nil", "Expected specific output")
}

// TestNewSQLServerChecker_OpenError tests the scenario where newSQLDBConnection (wrapping sql.Open) returns an error.
func TestNewSQLServerChecker_OpenError(t *testing.T) {
	// Create a mock OpenDBFunc that always returns an error
	mockOpenDB := func(driverName, connectionString string) (dbConnection, error) {
		return nil, fmt.Errorf("mocked sql.Open error: cannot connect to %s", connectionString)
	}

	desc := core.Descriptor{ComponentID: "sql-sql-open-error", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond
	connectionString := "some-conn-string" // The content doesn't matter, as our mock will return an error

	checker := NewSQLServerCheckerWithOpenDBFunc(desc, checkInterval, 10*time.Second, connectionString, mockOpenDB) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newSQLDBConnection fails")
	require.Contains(t, status.Output, "Failed to open DB connection", "Expected output to indicate DB connection failure")
	require.Contains(t, status.Output, "mocked sql.Open error", "Expected output to contain the mocked error")

	// Ensure the health check loop was not started
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after newSQLDBConnection failed", "status: %s", s.Status)
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

// TestSQLServerChecker_PerformHealthCheck_ResultNotOne tests performHealthCheck when the query returns a result other than 1.
func TestSQLServerChecker_PerformHealthCheck_ResultNotOne(t *testing.T) {
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

	desc := core.Descriptor{ComponentID: "sql-perform-result-not-one", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := newSQLServerCheckerInternal(desc, checkInterval, 10*time.Second, mockDB) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Manually call performHealthCheck
	checker.(*sqlServerChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when result is not 1")
	require.Contains(t, status.Output, "Microsoft SQL Server health check query returned unexpected result: 0 (expected 1)", "Expected specific output")
}

// TestSQLServerChecker_PerformHealthCheck_ScanError tests performHealthCheck when row.Scan returns an error.
func TestSQLServerChecker_PerformHealthCheck_ScanError(t *testing.T) {
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

	desc := core.Descriptor{ComponentID: "sql-perform-scan-error", ComponentType: "sqlserver"}
	checkInterval := 50 * time.Millisecond

	checker := newSQLServerCheckerInternal(desc, checkInterval, 10*time.Second, mockDB) // Increased query timeout
	defer func() {
		assert.NoError(t, checker.Close(), "Checker Close() returned an unexpected error")
	}()

	// Manually call performHealthCheck
	checker.(*sqlServerChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when scan returns an error")
	require.Contains(t, status.Output, "Microsoft SQL Server health check query failed", "Expected specific output")
	require.Contains(t, status.Output, "mocked scan error", "Expected output to contain the mocked error")
}
