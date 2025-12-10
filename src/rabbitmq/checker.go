package rabbitmq

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// connection interface for mocking *amqp.Connection in tests.
type amqpConnection interface {
	Channel() (amqpChannel, error)
	Close() error
	IsClosed() bool
}

// channel interface for mocking *amqp.Channel in tests.
type amqpChannel interface {
	Confirm(noWait bool) error
	Publish(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) error
	PublishWithDeferredConfirm(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) (*amqp.DeferredConfirmation, error)
	Close() error
}

// realAMQPConnection implements amqpConnection for *amqp.Connection.
type realAMQPConnection struct {
	conn *amqp.Connection
}

func (r *realAMQPConnection) Channel() (amqpChannel, error) { // Corrected return type
	ch, err := r.conn.Channel()
	if err != nil {
		return nil, err
	}
	return &realAMQPChannel{ch: ch}, nil // Returns our realAMQPChannel wrapper
}

func (r *realAMQPConnection) Close() error {
	return r.conn.Close()
}

func (r *realAMQPConnection) IsClosed() bool {
	return r.conn.IsClosed()
}

// realAMQPChannel implements amqpChannel for *amqp.Channel.
type realAMQPChannel struct {
	ch *amqp.Channel
}

func (r *realAMQPChannel) Confirm(noWait bool) error {
	return r.ch.Confirm(noWait)
}

func (r *realAMQPChannel) Publish(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) error {
	return r.ch.Publish(exchange, routingKey, mandatory, immediate, msg)
}

func (r *realAMQPChannel) PublishWithDeferredConfirm(exchange, routingKey string, mandatory, immediate bool, msg amqp.Publishing) (*amqp.DeferredConfirmation, error) {
	return r.ch.PublishWithDeferredConfirm(exchange, routingKey, mandatory, immediate, msg)
}

func (r *realAMQPChannel) Close() error {
	return r.ch.Close()
}

type rabbitMQChecker struct {
	checkInterval     time.Duration
	operationsTimeout time.Duration // New field for RabbitMQ operation timeout
	connectionString  string
	conn              amqpConnection // Changed from *amqp.Connection
	descriptor        core.Descriptor
	currentStatus     core.ComponentStatus
	statusChangeChan  chan core.ComponentStatus
	quit              chan struct{}
	mutex             sync.RWMutex
	cancelFunc        context.CancelFunc
	ctx               context.Context
	disabled          bool
}

// OpenAMQPFunc defines the signature for a function that can open a RabbitMQ connection.
// This is used to allow mocking of amqp.Dial in tests.
type OpenAMQPFunc func(url string) (amqpConnection, error)

// newRealAMQPConnection is a helper function to create a real amqpConnection from *amqp.Connection
func newRealAMQPConnection(url string) (amqpConnection, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	return &realAMQPConnection{conn: conn}, nil
}

// NewRabbitMQChecker creates a new RabbitMQ health checker component.
// It continuously checks the connection and publishes a test message.
func NewRabbitMQChecker(descriptor core.Descriptor, checkInterval, operationsTimeout time.Duration, connectionString string) core.Component {
	return NewRabbitMQCheckerWithOpenAMQPFunc(descriptor, checkInterval, operationsTimeout, connectionString, newRealAMQPConnection)
}

// NewRabbitMQCheckerWithOpenAMQPFunc creates a new RabbitMQ health checker component,
// allowing a custom OpenAMQPFunc to be provided for opening the connection.
// This is useful for testing scenarios where mocking the connection is required.
func NewRabbitMQCheckerWithOpenAMQPFunc(descriptor core.Descriptor, checkInterval, operationsTimeout time.Duration, connectionString string, openAMQP OpenAMQPFunc) core.Component {
	amqpConn, err := openAMQP(connectionString)
	if err != nil {
		dummyChecker := &rabbitMQChecker{
			descriptor: descriptor,
			currentStatus: core.ComponentStatus{
				Status: core.StatusFail,
				Output: fmt.Sprintf("Failed to open RabbitMQ connection: %v", err),
			},
			statusChangeChan: make(chan core.ComponentStatus, 1),
			quit:             make(chan struct{}),
			ctx:              context.Background(),
			cancelFunc:       func() {},
			disabled:         false,
		}
		return dummyChecker
	}

	return newRabbitMQCheckerInternal(descriptor, checkInterval, operationsTimeout, amqpConn)
}

// newRabbitMQCheckerInternal creates a new RabbitMQ health checker component with a provided amqpConnection.
// It continuously checks the connection and publishes a test message.
func newRabbitMQCheckerInternal(descriptor core.Descriptor, checkInterval, operationsTimeout time.Duration, conn amqpConnection) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "RabbitMQ checker initializing...",
	}

	checker := &rabbitMQChecker{
		checkInterval:     checkInterval,
		operationsTimeout: operationsTimeout, // Initialize operationsTimeout
		connectionString:  "",                // Not applicable when amqpConnection is provided directly
		descriptor:        descriptor,
		currentStatus:     initialStatus,
		statusChangeChan:  make(chan core.ComponentStatus, 1),
		quit:              make(chan struct{}),
		ctx:               ctx,
		cancelFunc:        cancelFunc,
		disabled:          false,
		conn:              conn,
	}

	go checker.startHealthCheckLoop()

	return checker
}

// Close stops the checker's background operations and closes the RabbitMQ connection.
func (r *rabbitMQChecker) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.cancelFunc()
	close(r.quit)

	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// ChangeStatus updates the internal status of the component.
func (r *rabbitMQChecker) ChangeStatus(newStatus core.ComponentStatus) {
	r.updateStatus(newStatus)
}

// Disable sets the component to a disabled state.
func (r *rabbitMQChecker) Disable() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.disabled {
		r.disabled = true
		r.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "RabbitMQ checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case r.statusChangeChan <- r.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component.
func (r *rabbitMQChecker) Enable() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.disabled {
		r.disabled = false
		r.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "RabbitMQ checker enabled, re-initializing...",
		}
		select {
		case r.statusChangeChan <- r.currentStatus:
		default:
		}
		go r.performHealthCheck()
	}
}

// Status returns the current health status of the RabbitMQ component.
func (r *rabbitMQChecker) Status() core.ComponentStatus {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.currentStatus
}

// Descriptor returns the descriptor for the RabbitMQ component.
func (r *rabbitMQChecker) Descriptor() core.Descriptor {
	return r.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (r *rabbitMQChecker) StatusChange() <-chan core.ComponentStatus {
	return r.statusChangeChan
}

// Health returns a detailed health report for the RabbitMQ component.
func (r *rabbitMQChecker) Health() core.ComponentHealth {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var output string

	if r.currentStatus.Output != "" {
		output = r.currentStatus.Output
	}

	return core.ComponentHealth{
		ComponentID:   r.descriptor.ComponentID,
		ComponentType: r.descriptor.ComponentType,
		Status:        r.currentStatus.Status,
		Output:        output,
	}
}

// startHealthCheckLoop runs in a goroutine, periodically checking the RabbitMQ health.
func (r *rabbitMQChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	r.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			r.performHealthCheck()
		case <-r.quit:
			if r.conn != nil && !r.conn.IsClosed() {
				if err := r.conn.Close(); err != nil {
					log.Printf("Error closing RabbitMQ connection on quit: %v", err)
				}
			}
			return
		case <-r.ctx.Done():
			if r.conn != nil && !r.conn.IsClosed() {
				if err := r.conn.Close(); err != nil {
					log.Printf("Error closing RabbitMQ connection on context done: %v", err)
				}
			}
			return
		}
	}
}

// performHealthCheck attempts to open a channel and publish a message to a temporary queue.
func (r *rabbitMQChecker) performHealthCheck() {
	r.mutex.RLock()
	isDisabled := r.disabled
	r.mutex.RUnlock()

	if isDisabled {
		return
	}

	if r.conn == nil || r.conn.IsClosed() {
		r.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "RabbitMQ connection is closed or nil",
		})
		return
	}

	startTime := time.Now()
	opCtx, cancelOp := context.WithTimeout(r.ctx, r.operationsTimeout)
	defer cancelOp()

	channel, err := r.conn.Channel()
	if err != nil {
		r.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("Failed to open RabbitMQ channel: %v", err),
			Time:          startTime,
			ObservedValue: time.Since(startTime).Seconds(),
			ObservedUnit:  "s",
		})
		return
	}
	defer func() {
		if err := channel.Close(); err != nil {
			log.Printf("Error closing RabbitMQ channel: %v", err)
		}
	}()

	// Publish a test message
	testQueue := "health_check_queue"
	err = channel.PublishWithContext(opCtx, // Use context with timeout
		"",        // exchange
		testQueue, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte("health check message"),
		})
	if err != nil {
		r.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("Failed to publish RabbitMQ test message: %v", err),
			Time:          startTime,
			ObservedValue: time.Since(startTime).Seconds(),
			ObservedUnit:  "s",
		})
		return
	}

	r.updateStatus(core.ComponentStatus{
		Status:        core.StatusPass,
		Output:        "RabbitMQ is healthy",
		Time:          startTime,
		ObservedValue: time.Since(startTime).Seconds(),
		ObservedUnit:  "s",
	})
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (r *rabbitMQChecker) updateStatus(newStatus core.ComponentStatus) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.currentStatus.Status != newStatus.Status || r.currentStatus.Output != newStatus.Output {
		r.currentStatus = newStatus
		select {
		case r.statusChangeChan <- newStatus:
		default:
			// Non-blocking send
		}
	}
}
