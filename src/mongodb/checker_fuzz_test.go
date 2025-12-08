package mongodb

import (
	"healthcheck/core"
	"testing"
	"time"
)

func FuzzMongoChecker_NewMongoChecker(f *testing.F) {
	f.Add("mongodb://user:pass@localhost:27017/test")
	f.Fuzz(func(t *testing.T, connectionString string) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Panic caught for connection string: %s, panic: %v", connectionString, r)
			}
		}()

		descriptor := core.Descriptor{
			ComponentID:   "fuzz-test",
			ComponentType: "mongodb",
		}
		checkInterval := 1 * time.Second

		// We call NewMongoChecker, which is the public entry point.
		// We expect this function to not panic.
		checker := NewMongoChecker(descriptor, checkInterval, connectionString)
		if checker == nil {
			t.Errorf("NewMongoChecker returned nil for connection string: %s", connectionString)
		}

		// We can also check the status. It should be either warn or fail.
		status := checker.Status()
		if status.Status != core.StatusWarn && status.Status != core.StatusFail {
			t.Errorf("Unexpected status %s for connection string: %s", status.Status, connectionString)
		}
	})
}
