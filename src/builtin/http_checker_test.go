package builtin

import (
	"fmt"
	"healthcheck/core"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPCheckerComponent(t *testing.T) {
	// Test case: Empty URL
	_, err := NewHTTPCheckerComponent(HTTPCheckerOptions{URL: ""})
	assert.Error(t, err, "Expected error for empty URL")
	assert.Contains(t, err.Error(), "URL cannot be empty")

	// Test case: Valid URL, default options
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	descriptor := c.Descriptor()
	assert.Equal(t, fmt.Sprintf("http-get-%s", server.URL), descriptor.ComponentID)
	assert.Equal(t, "http", descriptor.ComponentType)
	assert.Equal(t, server.URL, descriptor.Links["url"])

	// Test case: Valid URL, custom options
	customOpts := HTTPCheckerOptions{
		URL:      server.URL,
		Timeout:  2 * time.Second,
		Interval: 3 * time.Second,
	}
	c2, err := NewHTTPCheckerComponent(customOpts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c2.Close(), "Close() returned an error")
	}()

	// Ensure options are set correctly (internal check, not directly exposed)
	// We can't directly verify internal options, but we can trust the constructor if it doesn't error.
}

func TestHTTPChecker_SuccessfulCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL + "/health", Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	observer := c.StatusChange()

	// Wait for the first successful check
	var status core.ComponentStatus
	require.Eventually(t, func() bool {
		select {
		case status = <-observer:
			return status.Status == core.StatusPass
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "Expected initial pass status")

	assert.Equal(t, core.StatusPass, status.Status)
	assert.Contains(t, status.Output, fmt.Sprintf("HTTP GET to %s returned status 200", opts.URL))
}

func TestHTTPChecker_FailedCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	observer := c.StatusChange()

	// Wait for the first failed check
	var status core.ComponentStatus
	require.Eventually(t, func() bool {
		select {
		case status = <-observer:
			return status.Status == core.StatusFail
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "Expected initial fail status")

	assert.Equal(t, core.StatusFail, status.Status)
	assert.Contains(t, status.Output, fmt.Sprintf("HTTP GET to %s returned status 500", opts.URL))
}

func TestHTTPChecker_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Simulate a slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Timeout: 100 * time.Millisecond, Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	observer := c.StatusChange()

	// Wait for the timeout to occur
	var status core.ComponentStatus
	require.Eventually(t, func() bool {
		select {
		case status = <-observer:
			return status.Status == core.StatusFail
		default:
			return false
		}
	}, 1*time.Second, 10*time.Millisecond, "Expected timeout fail status")

	assert.Equal(t, core.StatusFail, status.Status)
	assert.Contains(t, status.Output, "HTTP GET request failed:")
	assert.Contains(t, status.Output, "context deadline exceeded", "Expected timeout error message to indicate context deadline exceeded")
	assert.Contains(t, status.Output, "Client.Timeout exceeded", "Expected timeout error message to indicate client timeout exceeded")
}

func TestHTTPChecker_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)

	observer := c.StatusChange()

	// Ensure at least one status update before closing
	require.Eventually(t, func() bool {
		select {
		case <-observer:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected initial status update before closing")

	// Close the component
	assert.NoError(t, c.Close(), "Close() returned an error")

	// After closing, the observer channel should be drained and then closed.
	assert.Eventually(t, func() bool {
		_, ok := <-observer
		return !ok
	}, 200*time.Millisecond, 10*time.Millisecond, "Timeout waiting for observer channel to close.")
}

func TestHTTPChecker_HealthAndDescriptor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()

	// Wait for initial status
	require.Eventually(t, func() bool {
		s := c.Status().Status
		return s == core.StatusPass || s == core.StatusFail
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected initial status to be known (pass or fail)")

	// Test Descriptor
	descriptor := c.Descriptor()
	assert.Equal(t, fmt.Sprintf("http-get-%s", server.URL), descriptor.ComponentID)
	assert.Equal(t, "http", descriptor.ComponentType)
	assert.Equal(t, server.URL, descriptor.Links["url"])

	// Test Health
	health := c.Health()
	assert.Equal(t, c.Status().Status, health.Status, "Health status should match current status")
	assert.Equal(t, fmt.Sprintf("http-get-%s", server.URL), health.ComponentID)
	assert.Nil(t, health.ObservedValue, "HTTP checker should not have an observed value")
	assert.Equal(t, "", health.ObservedUnit)
}

func TestHTTPChecker_ExternalChangesAreNoOp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Interval: 10 * time.Millisecond}
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, c.Close(), "Close() returned an error")
	}()
	observer := c.StatusChange()

	// Wait for initial status
	require.Eventually(t, func() bool {
		select {
		case <-observer:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Did not receive initial status update")

	// Attempt to disable the component
	c.Disable()

	// The component should ignore the disable command and continue updating.
	require.Eventually(t, func() bool {
		select {
		case update := <-observer:
			return update.Status == core.StatusPass
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "Did not receive expected status update after Disable()")

	// Attempt to change status directly
	c.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "test"})

	require.Eventually(t, func() bool {
		select {
		case update := <-observer:
			return update.Status == core.StatusPass
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "Did not receive expected status update after ChangeStatus()")

	// Attempt to enable the component
	c.Enable()

	require.Eventually(t, func() bool {
		select {
		case update := <-observer:
			return update.Status == core.StatusPass
		default:
			return false
		}
	}, 200*time.Millisecond, 10*time.Millisecond, "Did not receive expected status update after Enable()")
}

func FuzzHTTPChecker(f *testing.F) {
	// Seed corpus for fuzzing
	f.Add("http://localhost:8080", int64(100), int64(100)) // Valid URL, short timeout/interval
	f.Add("invalid-url", int64(10), int64(10))             // Invalid URL
	f.Add("http://example.com", int64(500), int64(500))

	f.Fuzz(func(t *testing.T, url string, timeoutMs int64, intervalMs int64) {
		// Ensure positive values for timeout and interval
		if timeoutMs <= 0 {
			timeoutMs = 1
		}
		if intervalMs <= 0 {
			intervalMs = 1
		}

		opts := HTTPCheckerOptions{
			URL:      url,
			Timeout:  time.Duration(timeoutMs) * time.Millisecond,
			Interval: time.Duration(intervalMs) * time.Millisecond,
		}

		c, err := NewHTTPCheckerComponent(opts)
		if err != nil {
			// If URL is invalid, NewHTTPCheckerComponent will return an error, which is expected.
			// We just need to ensure it doesn't crash.
			return
		}
		defer func() {
			// Always attempt to close, but expect no error from close if already closed.
			_ = c.Close()
		}()

		// Allow some time for checks to run
		time.Sleep(100 * time.Millisecond)

		// Check basic functions don't panic
		_ = c.Status()
		_ = c.Descriptor()
		_ = c.Health()
		c.Disable()
		c.Enable()
		c.ChangeStatus(core.ComponentStatus{Status: core.StatusFail})

		assert.NoError(t, c.Close(), "Close() returned an error in fuzz test")
	})
}

func BenchmarkHTTPChecker_HealthLoad(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := HTTPCheckerOptions{URL: server.URL, Interval: 1 * time.Second} // Interval doesn't matter for benchmark, checks are on-demand
	c, err := NewHTTPCheckerComponent(opts)
	require.NoError(b, err)
	defer func() {
		assert.NoError(b, c.Close(), "Close() returned an error in benchmark cleanup")
	}()

	// Ensure the component is initialized and stable before benchmarking
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.Health()
		}
	})

	opsPerSecond := float64(b.N) / b.Elapsed().Seconds()
	require.GreaterOrEqual(b, opsPerSecond, 200.0, "Expected at least 200 operations per second for Health()")
}
