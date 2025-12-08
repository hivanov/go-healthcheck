package postgres

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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



// TestPostgresChecker_Integration_HappyPath tests a successful connection and health check.
func TestPostgresChecker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-happy-path", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing

	checker := NewPostgresChecker(desc, checkInterval, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn, 1*time.Second) // Initializing
	waitForStatus(t, checker, core.StatusPass, 2*time.Second) // Healthy

	status := checker.Status()
	if status.Status != core.StatusPass {
		t.Errorf("Expected final status %s, got %s", core.StatusPass, status.Status)
	}
	if status.Output == "" {
		t.Error("Expected output message, got empty")
	}
	if status.ObservedValue == 0 {
		t.Error("Expected ObservedValue to be non-zero")
	}
	if status.ObservedUnit != "s" {
		t.Errorf("Expected ObservedUnit 's', got '%s'", status.ObservedUnit)
	}
}

// TestPostgresChecker_Integration_Fail_NoConnection tests when the database is unreachable.
func TestPostgresChecker_Integration_Fail_NoConnection(t *testing.T) {
	// No container needed, just an invalid connection string
	invalidConnStr := "user=baduser dbname=baddb host=localhost port=1234 sslmode=disable"

	desc := core.Descriptor{ComponentID: "pg-fail-noconn", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, invalidConnStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail, 1*time.Second)

	status := checker.Status()
	if status.Status != core.StatusFail {
		t.Errorf("Expected status %s, got %s", core.StatusFail, status.Status)
	}
	if status.Output == "" {
		t.Error("Expected output message, got empty")
	}
}

// TestPostgresChecker_Integration_Fail_DBDown tests the checker reacting to a database going down.
func TestPostgresChecker_Integration_Fail_DBDown(t *testing.T) {
	ctx := t.Context()
	pgContainer, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-fail-dbdown", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Now stop the container
	t.Log("Stopping PostgreSQL container...")
	if err := pgContainer.Stop(ctx, nil); err != nil { // Use Stop instead of Terminate for cleaner restart potential in other tests (though not used here)
		t.Fatalf("Failed to stop PostgreSQL container: %v", err)
	}
	t.Log("PostgreSQL container stopped.")

	// It should now transition to a Fail state
	waitForStatus(t, checker, core.StatusFail, 2*time.Second)

	status := checker.Status()
	if status.Status != core.StatusFail {
		t.Errorf("Expected status %s after container stopped, got %s", core.StatusFail, status.Status)
	}
	if status.Output == "" {
		t.Error("Expected output message indicating connection error, got empty")
	}
}

// TestPostgresChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestPostgresChecker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-disable-enable", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)
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
	if status.Status != core.StatusWarn || status.Output != "PostgreSQL checker disabled" {
		t.Errorf("Expected status 'warn' and output 'PostgreSQL checker disabled' after Disable, got %s and '%s'", status.Status, status.Output)
	}

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
	assert.Contains(t, statusAfterSleep.Output, "PostgreSQL checker disabled", "Expected output to remain 'PostgreSQL checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	if status.Status != core.StatusWarn || status.Output != "PostgreSQL checker enabled, re-initializing..." {
		t.Errorf("Expected status 'warn' and output 'PostgreSQL checker enabled, re-initializing...' after Enable, got %s and '%s'", status.Status, status.Output)
	}

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	status = checker.Status()
	if status.Status != core.StatusPass {
		t.Errorf("Expected status %s after re-enable, got %s", core.StatusPass, status.Status)
	}
}

// TestPostgresChecker_Integration_Close tests graceful shutdown.
func TestPostgresChecker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup() // This ensures container is terminated even if checker.Close() fails

	desc := core.Descriptor{ComponentID: "pg-close", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Close the checker
	err := checker.Close()
	if err != nil {
		t.Errorf("Close() returned an unexpected error: %v", err)
	}

	// Verify that the internal context is cancelled after Close()
	select {
	case <-checker.(*postgresChecker).ctx.Done(): // Access internal context to verify cancellation
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

// TestPostgresChecker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestPostgresChecker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-change-status", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)
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
	if status.Status != manualWarnStatus.Status || status.Output != manualWarnStatus.Output {
		t.Errorf("Expected status %v, got %v after manual change", manualWarnStatus, status)
	}

	// Verify the manual status change is broadcast
	select {
	case s := <-checker.StatusChange():
		if s.Status != manualWarnStatus.Status {
			t.Errorf("Expected status %s from channel, got %s", manualWarnStatus.Status, s.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for manual status change notification")
	}

	// The periodic check should eventually revert it to Pass
	waitForStatus(t, checker, core.StatusPass, 1*time.Second)

	status = checker.Status()
	if status.Status != core.StatusPass {
		t.Errorf("Expected status %s after periodic check, got %s", core.StatusPass, status.Status)
	}
}

// TestPostgresChecker_Getters tests the Descriptor and Health methods.
func TestPostgresChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "test-type"}
	checker := &postgresChecker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

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



// TestPostgresChecker_Close_NilDB tests the Close method when the internal *sql.DB is nil.
func TestPostgresChecker_Close_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "pg-close-nil-db", ComponentType: "postgres"}
	checker := &postgresChecker{
		descriptor: desc,
		// Simulate a checker where DB connection failed to open
		db:         nil,
		ctx:        t.Context(),
		cancelFunc: func(){},
		quit:       make(chan struct{}),
	}

	err := checker.Close()
	assert.NoError(t, err, "Close() on nil DB should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Quit channel not closed")
	}
}
// TestPostgresChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestPostgresChecker_QuitChannelStopsLoop(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupPostgresContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "pg-quit-loop", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := NewPostgresChecker(desc, checkInterval, connStr)
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 2*time.Second)

	// Manually close the quit channel to stop the loop
	close(checker.(*postgresChecker).quit)

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
	if checker.(*postgresChecker).db != nil {
		err := checker.(*postgresChecker).db.Close()
		assert.NoError(t, err, "Error closing database connection after quit")
	}
}

// TestPostgresChecker_PerformHealthCheck_NilDB tests performHealthCheck when p.db is nil.
func TestPostgresChecker_PerformHealthCheck_NilDB(t *testing.T) {
	desc := core.Descriptor{ComponentID: "pg-perform-nil-db", ComponentType: "postgres"}
	checker := &postgresChecker{
		descriptor: desc,
		db:         nil, // Simulate nil DB connection
		ctx:        t.Context(),
		cancelFunc: context.CancelFunc(func() {}),
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected status Fail when DB is nil")
	assert.Contains(t, status.Output, "Database connection object is nil", "Expected specific output")
}

// TestNewPostgresChecker_OpenError tests the scenario where newSQLDBConnection (wrapping sql.Open) returns an error.
func TestNewPostgresChecker_OpenError(t *testing.T) {
	// Create a mock OpenDBFunc that always returns an error
	mockOpenDB := func(driverName, connectionString string) (dbConnection, error) {
		return nil, fmt.Errorf("mocked sql.Open error: cannot connect to %s", connectionString)
	}

	desc := core.Descriptor{ComponentID: "pg-sql-open-error", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond
	connectionString := "some-conn-string" // The content doesn't matter, as our mock will return an error

	checker := NewPostgresCheckerWithOpenDBFunc(desc, checkInterval, connectionString, mockOpenDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newSQLDBConnection fails")
	assert.Contains(t, status.Output, "Failed to open DB connection", "Expected output to indicate DB connection failure")
	assert.Contains(t, status.Output, "mocked sql.Open error", "Expected output to contain the mocked error")


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
	assert.Error(t, err, "Expected an error when opening DB with an invalid driver name")
	assert.Nil(t, dbConn, "Expected dbConn to be nil when sql.Open fails")
	assert.Contains(t, err.Error(), "sql: unknown driver", "Expected error message to indicate unknown driver")
}
// TestPostgresChecker_PerformHealthCheck_ResultNotOne tests performHealthCheck when the query returns a result other than 1.
func TestPostgresChecker_PerformHealthCheck_ResultNotOne(t *testing.T) {
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

	desc := core.Descriptor{ComponentID: "pg-perform-result-not-one", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := newPostgresCheckerInternal(desc, checkInterval, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*postgresChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected status Fail when result is not 1")
	assert.Contains(t, status.Output, "PostgreSQL health check query returned unexpected result: 0 (expected 1)", "Expected specific output")
}
// TestPostgresChecker_PerformHealthCheck_ScanError tests performHealthCheck when row.Scan returns an error.
func TestPostgresChecker_PerformHealthCheck_ScanError(t *testing.T) {
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

	desc := core.Descriptor{ComponentID: "pg-perform-scan-error", ComponentType: "postgres"}
	checkInterval := 50 * time.Millisecond

	checker := newPostgresCheckerInternal(desc, checkInterval, mockDB)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*postgresChecker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected status Fail when scan returns an error")
	assert.Contains(t, status.Output, "PostgreSQL health check query failed", "Expected specific output")
	assert.Contains(t, status.Output, "mocked scan error", "Expected output to contain the mocked error")
}