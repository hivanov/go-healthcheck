package rabbitmq

import (
	"context"
	"healthcheck/core"
	"net/url"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

// MockAMQPConnection is a mock implementation of amqpConnection for fuzz testing.
type MockAMQPConnection struct {
	failDial      bool
	failChannel   bool
	failPublish   bool
	connectionURL string
	closed        bool
}

func (m *MockAMQPConnection) Channel() (*amqp.Channel, error) {
	if m.failChannel {
		return nil, assert.AnError
	}
	return &amqp.Channel{}, nil // Return a dummy channel
}

func (m *MockAMQPConnection) Close() error {
	m.closed = true
	return nil
}

func (m *MockAMQPConnection) IsClosed() bool {
	return m.closed
}

// MockOpenAMQPFunc returns a mocked connection based on fuzz inputs.
func MockOpenAMQPFunc(failDial bool, connectionURL string) OpenAMQPFunc {
	return func(url string) (amqpConnection, error) {
		if failDial {
			return nil, assert.AnError
		}
		return &MockAMQPConnection{
			connectionURL: connectionURL,
			closed:        false,
		}, nil
	}
}

func FuzzNewRabbitMQChecker(f *testing.F) {
	// Seed corpus with valid and invalid connection strings and intervals
	f.Add("amqp://guest:guest@localhost:5672/", int64(1)) // Valid connection (will fail during actual dial for mock)
	f.Add("invalid-amqp-url", int64(1))                   // Invalid URL format
	f.Add("amqp://", int64(1))                            // Incomplete URL
	f.Add("amqp://guest:guest@host:1234/", int64(0))      // Zero interval
	f.Add("amqp://guest:guest@host:1234/", int64(-1))     // Negative interval
	f.Add("amqp://guest:guest@host:1234/", int64(1000))   // Large interval

	f.Fuzz(func(t *testing.T, connStr string, interval int64) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-rabbitmq",
			ComponentType: "rabbitmq",
			Description:   "Fuzz test for RabbitMQ checker",
		}

		// Ensure checkInterval is positive
		if interval <= 0 {
			interval = 1 // Default to 1ms to avoid panics from NewTicker
		}
		checkInterval := time.Duration(interval) * time.Millisecond

		// Test with failing dial
		checkerFailDial := NewRabbitMQCheckerWithOpenAMQPFunc(descriptor, checkInterval, connStr, MockOpenAMQPFunc(true, connStr))
		statusFailDial := checkerFailDial.Status()
		assert.Equal(t, core.StatusFail, statusFailDial.Status)
		assert.Contains(t, statusFailDial.Output, "Failed to open RabbitMQ connection")
		assert.NoError(t, checkerFailDial.Close())

		// Test with successful dial (but will use mock, so connection is not real)
		checkerSuccessDial := NewRabbitMQCheckerWithOpenAMQPFunc(descriptor, checkInterval, connStr, MockOpenAMQPFunc(false, connStr))
		// Give it a moment to run health check if interval is small
		time.Sleep(5 * time.Millisecond) // enough for a very quick check, if interval is tiny

		statusSuccessDial := checkerSuccessDial.Status()
		// The status could be WARN (initializing) or FAIL (mock channel/publish failure)
		// We primarily want to ensure it doesn't crash.
		assert.True(t, statusSuccessDial.Status == core.StatusWarn || statusSuccessDial.Status == core.StatusFail || statusSuccessDial.Status == core.StatusPass)
		assert.NoError(t, checkerSuccessDial.Close())

		// Test with unparseable connection string
		_, err := url.Parse(connStr)
		if err != nil {
			// If the URL is unparseable, NewRabbitMQChecker will fail to create a connection
			// and thus the initial status will be FAIL.
			// This is already covered by the MockOpenAMQPFunc(true, connStr) case,
			// but it's good to ensure the underlying parsing is robust.
			t.Logf("Connection string '%s' is unparseable: %v", connStr, err)
		}
	})
}
