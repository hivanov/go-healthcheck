package mariadb

import (
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func FuzzMariaDBChecker_NewMariaDBChecker(f *testing.F) {
	// A typical MariaDB connection string (using the mysql driver format)
	f.Add("testuser:testpass@tcp(localhost:3306)/testdb")
	f.Fuzz(func(t *testing.T, connectionString string) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-test",
			ComponentType: "mariadb",
		}
		checkInterval := 10 * time.Millisecond // Shorter interval for fuzzing
		queryTimeout := 50 * time.Millisecond  // Short query timeout for fuzzing

		// We call NewMariaDBChecker, which is the public entry point.
		// We expect this function to not panic.
		checker := NewMariaDBChecker(descriptor, checkInterval, queryTimeout, connectionString)
		defer func() {
			if err := checker.Close(); err != nil {
				t.Errorf("Checker Close() returned an unexpected error: %v", err)
			}
		}()

		if checker == nil {
			t.Skipf("NewMariaDBChecker returned nil for connection string: %s", connectionString)
			return
		}

		// Exercise other methods
		_ = checker.Status()
		_ = checker.Descriptor()
		_ = checker.Health()
		checker.Disable()
		checker.Enable()
		checker.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "fuzz"})

		// Allow some time for goroutines to process if necessary
		time.Sleep(checkInterval * 2)

		status := checker.Status()
		// Depending on the connection string, status could be Warn (initializing/disabled) or Fail (connection error).
		// We primarily care that it doesn't panic and that the status is one of the expected ones.
		assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusFail,
			"Unexpected status %s for connection string: %s", status.Status, connectionString)
	})
}
