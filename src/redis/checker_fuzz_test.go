package redis

import (
	"healthcheck/core"
	"net/url"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

func FuzzRedisChecker_NewRedisChecker(f *testing.F) {
	// Seed corpus with valid and invalid connection strings and intervals
	f.Add("localhost:6379", int64(1), int64(100)) // Valid connection (will fail during actual dial for mock)
	f.Add("invalid-redis-addr", int64(1), int64(100))
	f.Add("", int64(1), int64(100))
	f.Add("localhost:6379", int64(0), int64(100))    // Zero interval
	f.Add("localhost:6379", int64(-1), int64(100))   // Negative interval
	f.Add("localhost:6379", int64(1000), int64(100)) // Large interval

	f.Fuzz(func(t *testing.T, addr string, interval int64, pingTimeoutMs int64) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-redis",
			ComponentType: "redis",
			Description:   "Fuzz test for Redis checker",
		}

		// Ensure checkInterval is positive
		if interval <= 0 {
			interval = 1 // Default to 1ms to avoid panics from NewTicker
		}
		checkInterval := time.Duration(interval) * time.Millisecond

		// Ensure pingTimeout is positive
		if pingTimeoutMs <= 0 {
			pingTimeoutMs = 1 // Default to 1ms
		}
		pingTimeout := time.Duration(pingTimeoutMs) * time.Millisecond

		redisOptions := &redis.Options{Addr: addr}

		// Mock OpenRedisFunc to return success or failure based on fuzzed data or address parsing
		mockOpenRedis := func(opts *redis.Options) (redisConnection, error) {
			_, err := url.ParseRequestURI("redis://" + opts.Addr) // Validate address format somewhat
			if err != nil {
				return nil, err
			}
			return &mockRedisConnection{}, nil // Always return a mock connection that pings successfully
		}

		checker := NewRedisCheckerWithOpenRedisFunc(descriptor, checkInterval, pingTimeout, redisOptions, mockOpenRedis)
		defer func() {
			if err := checker.Close(); err != nil {
				t.Errorf("Checker Close() returned an unexpected error: %v", err)
			}
		}()

		if checker == nil {
			t.Skipf("NewRedisChecker returned nil for addr: %s", addr)
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
		// We primarily want to ensure it doesn't crash.
		assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusFail || status.Status == core.StatusPass,
			"Unexpected status %s for connection string: %s", status.Status, addr)
	})
}
