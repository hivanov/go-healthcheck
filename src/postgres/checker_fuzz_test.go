package postgres

import (
	"healthcheck/core"
	"testing"
	"time"
)

func FuzzPostgresChecker_NewPostgresChecker(f *testing.F) {
	f.Add("user=test password=password host=localhost port=5432 dbname=test sslmode=disable")
	f.Fuzz(func(t *testing.T, connectionString string) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-test",
			ComponentType: "postgres",
		}
		checkInterval := 1 * time.Second

		// We call NewPostgresChecker, which is the public entry point.
		// We expect this function to not panic.
		checker := NewPostgresChecker(descriptor, checkInterval, connectionString)
		if checker == nil {
			t.Errorf("NewPostgresChecker returned nil for connection string: %s", connectionString)
		}

		// We can also check the status. It should be either warn or fail.
		status := checker.Status()
		if status.Status != core.StatusWarn && status.Status != core.StatusFail {
			t.Errorf("Unexpected status %s for connection string: %s", status.Status, connectionString)
		}
	})
}
