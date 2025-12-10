package rabbitmq

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

// mockAMQPConnection implements amqpConnection for testing purposes.
type mockAMQPConnection struct {
	mu             sync.Mutex
	isClosed       bool
	channelErr     error
	closeErr       error
	mockChannel    *mockAMQPChannel
	channelsOpened int
	channelsClosed int
}

func newMockAMQPConnection() *mockAMQPConnection {
	return &mockAMQPConnection{
		isClosed:    false,
		mockChannel: newMockAMQPChannel(),
	}
}

func (m *mockAMQPConnection) Channel() (amqpChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.channelErr != nil {
		return nil, m.channelErr
	}
	if m.isClosed {
		return nil, fmt.Errorf("connection is closed")
	}
	m.channelsOpened++
	return m.mockChannel, nil
}

func (m *mockAMQPConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isClosed {
		return nil // Already closed
	}
	m.isClosed = true
	return m.closeErr
}

func (m *mockAMQPConnection) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isClosed
}

// SetChannelError configures the mock to return an error when Channel() is called.
func (m *mockAMQPConnection) SetChannelError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channelErr = err
}

// SetCloseError configures the mock to return an error when Close() is called.
func (m *mockAMQPConnection) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

// mockAMQPChannel implements amqpChannel for testing purposes.
type mockAMQPChannel struct {
	mu           sync.Mutex
	publishErr   error
	closeErr     error
	confirmErr   error
	publishCount int
}

func newMockAMQPChannel() *mockAMQPChannel {
	return &mockAMQPChannel{}
}

func (m *mockAMQPChannel) Confirm(noWait bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.confirmErr
}

func (m *mockAMQPChannel) Publish(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCount++
	return m.publishErr
}

func (m *mockAMQPChannel) PublishWithDeferredConfirm(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) (*amqp.DeferredConfirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, m.publishErr
}

func (m *mockAMQPChannel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeErr
}

// SetPublishError configures the mock to return an error when Publish() is called.
func (m *mockAMQPChannel) SetPublishError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishErr = err
}

// SetCloseError configures the mock to return an error when Close() is called.
func (m *mockAMQPChannel) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

// SetConfirmError configures the mock to return an error when Confirm() is called.
func (m *mockAMQPChannel) SetConfirmError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmErr = err
}

// setupRabbitMQ starts a RabbitMQ container and returns its connection string.
func setupRabbitMQ(ctx context.Context) (*rabbitmq.RabbitMQContainer, string, error) {
	rabbitmqContainer, err := rabbitmq.Run(ctx, "rabbitmq:3.12.11-management-alpine",
		rabbitmq.WithAdminUsername("guest"),
		rabbitmq.WithAdminPassword("guest"),
		testcontainers.WithImage("rabbitmq:3.12.11-management-alpine"),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to start RabbitMQ container: %w", err)
	}

	host, err := rabbitmqContainer.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get RabbitMQ host: %w", err)
	}
	port, err := rabbitmqContainer.MappedPort(ctx, "5672/tcp")
	if err != nil {
		return nil, "", fmt.Errorf("failed to get RabbitMQ AMQP port: %w", err)
	}
	amqpURL := fmt.Sprintf("amqp://guest:guest@%s:%s/", host, port.Port())

	return rabbitmqContainer, amqpURL, nil
}

// waitForStatus helper waits for the checker to report a specific status.
func waitForStatus(tb testing.TB, checker core.Component, expectedStatus core.StatusEnum, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusChangeChan := checker.StatusChange()

	// Check current status first
	currentStatus := checker.Status()
	if currentStatus.Status == expectedStatus {
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, checker.Status().Status, checker.Status().Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
		case <-time.After(5 * time.Millisecond):
			currentStatus = checker.Status()
			if currentStatus.Status == expectedStatus {
				return
			}
		}
	}
}
