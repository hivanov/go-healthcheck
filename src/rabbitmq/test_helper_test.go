package rabbitmq

import (
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
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
