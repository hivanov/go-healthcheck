package mariadb

import (
	"healthcheck/core"
	"testing"
	"time"
)

func FuzzMariaDBChecker_NewMariaDBChecker(f *testing.F) {
	// A typical MariaDB connection string (using the mysql driver format)
	f.Add("testuser:testpass@tcp(localhost:3306)/testdb")
	f.Fuzz(func(t *testing.T, connectionString string) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-test",
			ComponentType: "mariadb",
		}
		checkInterval := 1 * time.Second

		// We call NewMariaDBChecker, which is the public entry point.
		// We expect this function to not panic.
		checker := NewMariaDBChecker(descriptor, checkInterval, connectionString)
		if checker == nil {
			t.Errorf("NewMariaDBChecker returned nil for connection string: %s", connectionString)
		}

		// We can also check the status. It should be either warn or fail.
		status := checker.Status()
		if status.Status != core.StatusWarn && status.Status != core.StatusFail {
			t.Errorf("Unexpected status %s for connection string: %s", status.Status, connectionString)
		}
	})
}
