package redis

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisConnection interface for mocking *redis.Client in tests.
type redisConnection interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// realRedisConnection implements redisConnection for *redis.Client.
type realRedisConnection struct {
	client *redis.Client
}

func (r *realRedisConnection) Ping(ctx context.Context) *redis.StatusCmd {
	return r.client.Ping(ctx)
}

func (r *realRedisConnection) Close() error {
	return r.client.Close()
}

type redisChecker struct {
	checkInterval     time.Duration
	pingTimeout       time.Duration
	connectionOptions *redis.Options
	client            redisConnection
	descriptor        core.Descriptor
	currentStatus     core.ComponentStatus
	statusChangeChan  chan core.ComponentStatus
	quit              chan struct{}
	mutex             sync.RWMutex
	cancelFunc        context.CancelFunc
	ctx               context.Context
	disabled          bool
}

// OpenRedisFunc defines the signature for a function that can open a Redis client connection.
// This is used to allow mocking of redis.NewClient in tests.
type OpenRedisFunc func(options *redis.Options) (redisConnection, error)

// newRealRedisClient is a helper function to create a real redisConnection from *redis.Client
func newRealRedisClient(options *redis.Options) (redisConnection, error) {
	client := redis.NewClient(options)
	// Ping the server to ensure a connection is established.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Initial connection ping timeout
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &realRedisConnection{client: client}, nil
}

// NewRedisChecker creates a new Redis health checker component.
// It continuously pings the Redis server and updates its status.
func NewRedisChecker(descriptor core.Descriptor, checkInterval, pingTimeout time.Duration, options *redis.Options) core.Component {
	return NewRedisCheckerWithOpenRedisFunc(descriptor, checkInterval, pingTimeout, options, newRealRedisClient)
}

// NewRedisCheckerWithOpenRedisFunc creates a new Redis health checker component,
// allowing a custom OpenRedisFunc to be provided for opening the Redis client.
// This is useful for testing scenarios where mocking the Redis client is required.
func NewRedisCheckerWithOpenRedisFunc(descriptor core.Descriptor, checkInterval, pingTimeout time.Duration, options *redis.Options, openRedis OpenRedisFunc) core.Component {
	redisConn, err := openRedis(options)
	if err != nil {
		dummyChecker := &redisChecker{
			descriptor: descriptor,
			currentStatus: core.ComponentStatus{
				Status: core.StatusFail,
				Output: fmt.Sprintf("Failed to open Redis connection: %v", err),
			},
			statusChangeChan: make(chan core.ComponentStatus, 1),
			quit:             make(chan struct{}),
			ctx:              context.Background(),
			cancelFunc:       func() {},
			disabled:         false,
		}
		return dummyChecker
	}

	return newRedisCheckerInternal(descriptor, checkInterval, pingTimeout, options, redisConn)
}

// newRedisCheckerInternal creates a new Redis health checker component with a provided redisConnection.
// It continuously pings the Redis server and updates its status.
func newRedisCheckerInternal(descriptor core.Descriptor, checkInterval, pingTimeout time.Duration, options *redis.Options, conn redisConnection) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "Redis checker initializing...",
	}

	checker := &redisChecker{
		checkInterval:     checkInterval,
		pingTimeout:       pingTimeout,
		connectionOptions: options,
		descriptor:        descriptor,
		currentStatus:     initialStatus,
		statusChangeChan:  make(chan core.ComponentStatus, 1),
		quit:              make(chan struct{}),
		ctx:               ctx,
		cancelFunc:        cancelFunc,
		disabled:          false,
		client:            conn,
	}

	go checker.startHealthCheckLoop()

	return checker
}

// Close stops the checker's background operations and closes the Redis client.
func (r *redisChecker) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.cancelFunc()
	close(r.quit)

	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// ChangeStatus updates the internal status of the component.
func (r *redisChecker) ChangeStatus(newStatus core.ComponentStatus) {
	r.updateStatus(newStatus)
}

// Disable sets the component to a disabled state.
func (r *redisChecker) Disable() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.disabled {
		r.disabled = true
		r.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "Redis checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case r.statusChangeChan <- r.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component.
func (r *redisChecker) Enable() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.disabled {
		r.disabled = false
		r.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "Redis checker enabled, re-initializing...",
		}
		select {
		case r.statusChangeChan <- r.currentStatus:
		default:
		}
		go r.performHealthCheck()
	}
}

// Status returns the current health status of the Redis component.
func (r *redisChecker) Status() core.ComponentStatus {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.currentStatus
}

// Descriptor returns the descriptor for the Redis component.
func (r *redisChecker) Descriptor() core.Descriptor {
	return r.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (r *redisChecker) StatusChange() <-chan core.ComponentStatus {
	return r.statusChangeChan
}

// Health returns a detailed health report for the Redis component.
func (r *redisChecker) Health() core.ComponentHealth {
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

// startHealthCheckLoop runs in a goroutine, periodically checking the Redis health.
func (r *redisChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	r.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			r.performHealthCheck()
		case <-r.quit:
			if r.client != nil {
				if err := r.client.Close(); err != nil {
					log.Printf("Error closing Redis client on quit: %v", err)
				}
			}
			return
		case <-r.ctx.Done():
			if r.client != nil {
				if err := r.client.Close(); err != nil {
					log.Printf("Error closing Redis client on context done: %v", err)
				}
			}
			return
		}
	}
}

// performHealthCheck pings the Redis server and updates the component's status.
func (r *redisChecker) performHealthCheck() {
	r.mutex.RLock()
	isDisabled := r.disabled
	r.mutex.RUnlock()

	if isDisabled {
		return
	}

	if r.client == nil {
		r.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "Redis client is nil",
		})
		return
	}

	checkCtx, cancelCheck := context.WithTimeout(r.ctx, r.pingTimeout)
	defer cancelCheck()

	startTime := time.Now()
	err := r.client.Ping(checkCtx).Err()
	elapsedTime := time.Since(startTime)

	if err != nil {
		r.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("Redis ping failed: %v", err),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else {
		r.updateStatus(core.ComponentStatus{
			Status:        core.StatusPass,
			Output:        "Redis is healthy",
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	}
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (r *redisChecker) updateStatus(newStatus core.ComponentStatus) {
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
