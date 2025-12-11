package rabbitmq

import (
	"context"
	"healthcheck/core"
	"net/url"
	"sync" // Import sync package
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

// MockAMQPChannel is a mock implementation of amqpChannel for fuzz testing.
type MockAMQPChannel struct {
	failConfirm bool
	failPublish bool
	closed      bool
}

func (m *MockAMQPChannel) Confirm(noWait bool) error {
	if m.failConfirm {
		return assert.AnError
	}
	return nil
}

func (m *MockAMQPChannel) Publish(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) error {
	if m.failPublish { // Assuming failPublish applies to Publish as well
		return assert.AnError
	}
	return nil
}

func (m *MockAMQPChannel) PublishWithDeferredConfirm(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) (*amqp.DeferredConfirmation, error) {
	if m.failPublish {
		return nil, assert.AnError
	}
	return &amqp.DeferredConfirmation{}, nil
}

func (m *MockAMQPChannel) PublishWithContext(ctx context.Context, exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) error {
	if m.failPublish {
		return assert.AnError
	}
	return nil
}

func (m *MockAMQPChannel) Close() error {
	m.closed = true
	return nil
}

// MockAMQPConnection is a mock implementation of amqpConnection for fuzz testing.
type MockAMQPConnection struct {
	failDial      bool
	failChannel   bool
	connectionURL string
	mu            sync.Mutex // Mutex to protect 'closed'
	closed        bool
}

func (m *MockAMQPConnection) Channel() (amqpChannel, error) { // Corrected return type
	if m.failChannel {
		return nil, assert.AnError
	}
	return &MockAMQPChannel{}, nil // Returns our mock channel
}

func (m *MockAMQPConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *MockAMQPConnection) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	f.Add("amqp://guest:guest@localhost:5672/", int64(1), int64(100)) // Valid connection (will fail during actual dial for mock)
	f.Add("invalid-amqp-url", int64(1), int64(100))                   // Invalid URL format
	f.Add("amqp://", int64(1), int64(100))                            // Incomplete URL
	f.Add("amqp://guest:guest@host:1234/", int64(0), int64(100))      // Zero interval
	f.Add("amqp://guest:guest@host:1234/", int64(-1), int64(100))     // Negative interval
	f.Add("amqp://guest:guest@host:1234/", int64(1000), int64(100))   // Large interval

	f.Fuzz(func(t *testing.T, connStr string, interval int64, opTimeout int64) {
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

		// Ensure opTimeout is positive
		if opTimeout <= 0 {
			opTimeout = 1 // Default to 1ms
		}
		operationsTimeout := time.Duration(opTimeout) * time.Millisecond

		// Test with failing dial
		checkerFailDial := NewRabbitMQCheckerWithOpenAMQPFunc(descriptor, checkInterval, operationsTimeout, connStr, MockOpenAMQPFunc(true, connStr))
		defer func() {
			if err := checkerFailDial.Close(); err != nil {
				t.Errorf("CheckerFailDial Close() returned an unexpected error: %v", err)
			}
		}()

		statusFailDial := checkerFailDial.Status()
		assert.Equal(t, core.StatusFail, statusFailDial.Status)
		assert.Contains(t, statusFailDial.Output, "Failed to open RabbitMQ connection")

		// Exercise other methods
		_ = checkerFailDial.Health()
		checkerFailDial.Disable()
		checkerFailDial.Enable()
		checkerFailDial.ChangeStatus(core.ComponentStatus{Status: core.StatusPass})

		// Test with successful dial (but will use mock, so connection is not real)
		checkerSuccessDial := NewRabbitMQCheckerWithOpenAMQPFunc(descriptor, checkInterval, operationsTimeout, connStr, MockOpenAMQPFunc(false, connStr))
		defer func() {
			if err := checkerSuccessDial.Close(); err != nil {
				t.Errorf("CheckerSuccessDial Close() returned an unexpected error: %v", err)
			}
		}()

		// Give it a moment to run health check if interval is small
		time.Sleep(5 * time.Millisecond) // enough for a very quick check, if interval is tiny

		statusSuccessDial := checkerSuccessDial.Status()
		// The status could be WARN (initializing) or FAIL (mock channel/publish failure)
		// We primarily want to ensure it doesn't crash.
		assert.True(t, statusSuccessDial.Status == core.StatusWarn || statusSuccessDial.Status == core.StatusFail || statusSuccessDial.Status == core.StatusPass)

		// Exercise other methods
		_ = checkerSuccessDial.Health()
		checkerSuccessDial.Disable()
		checkerSuccessDial.Enable()
		checkerSuccessDial.ChangeStatus(core.ComponentStatus{Status: core.StatusFail})

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
