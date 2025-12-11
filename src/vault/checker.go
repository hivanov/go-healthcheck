package vault

import (
	"context"
	"fmt"
	"sync"
	"time"

	"healthcheck/core"

	"github.com/hashicorp/vault/api"
)

// SysInterface defines the methods of *api.Sys used by the checker.
type SysInterface interface {
	Health() (*api.HealthResponse, error)
}

// Client interface for mocking *api.Client in tests.
type Client interface {
	Sys() SysInterface // Now returns an interface
	Close()
}

// realVaultClient implements vaultClient for *api.Client.
type realVaultClient struct {
	client *api.Client
}

func (r *realVaultClient) Sys() SysInterface { // Now returns an interface
	return r.client.Sys()
}

func (r *realVaultClient) Close() {
	// The HashiCorp Vault API client doesn't have a Close() method in the traditional sense
	// because it manages HTTP connections which are usually handled by the underlying http.Client.
	// For testing, we might want to ensure connections are not left open, but for the real client,
	// this is effectively a no-op or could be used to clean up any custom transports if implemented.
	// For now, we'll keep it as a no-op to satisfy the interface.
}

type vaultChecker struct {
	checkInterval    time.Duration
	vaultConfig      *api.Config
	client           Client // Changed from *api.Client
	descriptor       core.Descriptor
	currentStatus    core.ComponentStatus
	statusChangeChan chan core.ComponentStatus
	quit             chan struct{}
	mutex            sync.RWMutex
	cancelFunc       context.CancelFunc
	ctx              context.Context
	disabled         bool
}

// OpenVaultClientFunc defines the signature for a function that can open a Vault client.
// This is used to allow mocking of api.NewClient in tests.
type OpenVaultClientFunc func(config *api.Config) (Client, error)

// newRealVaultClient is a helper function to create a real vaultClient from *api.Client
// This will be the default OpenVaultClientFunc used by NewVaultChecker.
func newRealVaultClient(config *api.Config) (Client, error) {
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}
	return &realVaultClient{client: client}, nil
}

// NewVaultChecker creates a new HashiCorp Vault health checker component.
// It continuously checks the Vault's health status and updates its own status.
// This is the public constructor that handles creating the Vault client.
func NewVaultChecker(descriptor core.Descriptor, checkInterval time.Duration, vaultConfig *api.Config) core.Component {
	return NewVaultCheckerWithOpenVaultClientFunc(descriptor, checkInterval, vaultConfig, newRealVaultClient)
}

// NewVaultCheckerWithOpenVaultClientFunc creates a new HashiCorp Vault health checker component,
// allowing a custom OpenVaultClientFunc to be provided for creating the Vault client.
// This is useful for testing scenarios where mocking the Vault client is required.
func NewVaultCheckerWithOpenVaultClientFunc(descriptor core.Descriptor, checkInterval time.Duration, vaultConfig *api.Config, openClient OpenVaultClientFunc) core.Component {
	vaultClient, err := openClient(vaultConfig)
	if err != nil {
		dummyChecker := &vaultChecker{
			descriptor: descriptor,
			currentStatus: core.ComponentStatus{
				Status: core.StatusFail,
				Output: fmt.Sprintf("Failed to create Vault client: %v", err),
			},
			statusChangeChan: make(chan core.ComponentStatus, 1),
			ctx:              context.Background(),
			cancelFunc:       func() {},
			disabled:         false,
		}
		return dummyChecker
	}

	return newVaultCheckerInternal(descriptor, checkInterval, vaultClient)
}

// NewVaultCheckerWithClient creates a new HashiCorp Vault health checker component using an already initialized Vault client.
// This allows users to provide a Vault client that has been set up and authenticated externally.
func NewVaultCheckerWithClient(descriptor core.Descriptor, checkInterval time.Duration, client Client) core.Component {
	return newVaultCheckerInternal(descriptor, checkInterval, client)
}

// newVaultCheckerInternal creates a new HashiCorp Vault health checker component with a provided vaultClient.
// It continuously checks the Vault's health status and updates its own status.
// This is an internal constructor used for testing purposes and for injecting a vaultClient.
func newVaultCheckerInternal(descriptor core.Descriptor, checkInterval time.Duration, client Client) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// Initial status is initializing
	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "Vault checker initializing...",
	}

	checker := &vaultChecker{
		checkInterval:    checkInterval,
		vaultConfig:      nil, // Not applicable when client is provided directly
		descriptor:       descriptor,
		currentStatus:    initialStatus,
		statusChangeChan: make(chan core.ComponentStatus, 1), // Buffered to prevent blocking
		ctx:              ctx,
		cancelFunc:       cancelFunc,
		disabled:         false,
		client:           client, // Use the provided vaultClient
	}

	go checker.startHealthCheckLoop()

	return checker
}

// Close stops the checker's background operations.
func (v *vaultChecker) Close() error {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	v.cancelFunc() // Signal context cancellation to stop the loop

	if v.client != nil {
		v.client.Close() // Call the client's Close method, even if it's a no-op for real client
	}
	return nil
}

// ChangeStatus updates the internal status of the component.
func (v *vaultChecker) ChangeStatus(newStatus core.ComponentStatus) {
	v.updateStatus(newStatus)
}

// Disable sets the component to a disabled state, halting active health checks.
func (v *vaultChecker) Disable() {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if !v.disabled {
		v.disabled = true // Set disabled flag first
		v.cancelFunc()    // Stop the current health check loop
		v.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "Vault checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case v.statusChangeChan <- v.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component, resuming active health checks.
func (v *vaultChecker) Enable() {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if v.disabled {
		v.disabled = false
		// Create a new context for the new health check loop
		v.ctx, v.cancelFunc = context.WithCancel(context.Background())
		v.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "Vault checker enabled, re-initializing...",
		}
		select {
		case v.statusChangeChan <- v.currentStatus:
		default:
		}
		go v.startHealthCheckLoop() // Start a new health check loop
	}
}

// Status returns the current health status of the Vault component.
func (v *vaultChecker) Status() core.ComponentStatus {
	v.mutex.RLock()
	defer v.mutex.RUnlock()
	return v.currentStatus
}

// Descriptor returns the descriptor for the Vault component.
func (v *vaultChecker) Descriptor() core.Descriptor {
	return v.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (v *vaultChecker) StatusChange() <-chan core.ComponentStatus {
	return v.statusChangeChan
}

// Health returns a detailed health report for the Vault component.
func (v *vaultChecker) Health() core.ComponentHealth {
	v.mutex.RLock()
	defer v.mutex.RUnlock()

	var output string

	if v.currentStatus.Output != "" {
		output = v.currentStatus.Output
	}

	return core.ComponentHealth{
		ComponentID:   v.descriptor.ComponentID,
		ComponentType: v.descriptor.ComponentType,
		Status:        v.currentStatus.Status,
		Output:        output,
	}
}

// startHealthCheckLoop runs in a goroutine, periodically checking the Vault health.
func (v *vaultChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(v.checkInterval)
	defer ticker.Stop()

	// Perform an initial check immediately
	v.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			v.performHealthCheck()
		case <-v.ctx.Done(): // Context cancellation signal
			return
		}
	}
}

// performHealthCheck executes a health check against the Vault instance
// and updates the component's status based on the result.
func (v *vaultChecker) performHealthCheck() {
	v.mutex.RLock()
	isDisabled := v.disabled
	v.mutex.RUnlock()

	if isDisabled {
		return
	}

	if v.client == nil {
		v.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "Vault client object is nil",
		})
		return
	}

	startTime := time.Now()
	healthResponse, err := v.client.Sys().Health()
	elapsedTime := time.Since(startTime)

	if err != nil {
		v.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("Vault health check failed: %v", err),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
		return
	}

	// Interpret Vault's health status
	// https://developer.hashicorp.com/vault/api-references/system#health
	// 200 (Active), 429 (Standby, sealed or degraded), 500 (Uninitialized, sealed, or in recovery)
	if healthResponse.Initialized && !healthResponse.Sealed && !healthResponse.Standby && !healthResponse.PerformanceStandby {
		v.updateStatus(core.ComponentStatus{
			Status:        core.StatusPass,
			Output:        "Vault is healthy and active",
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else {
		statusOutput := fmt.Sprintf("Vault status: Initialized=%t, Sealed=%t, Standby=%t, PerformanceStandby=%t, ReplicationPerformanceMode=%s, ReplicationDRMode=%s",
			healthResponse.Initialized,
			healthResponse.Sealed,
			healthResponse.Standby,
			healthResponse.PerformanceStandby,
			healthResponse.ReplicationPerformanceMode,
			healthResponse.ReplicationDRMode,
		)

		// Decide status based on Vault's health response
		if !healthResponse.Initialized {
			v.updateStatus(core.ComponentStatus{
				Status:        core.StatusFail,
				Output:        "Vault is uninitialized. " + statusOutput,
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		} else if healthResponse.Sealed {
			v.updateStatus(core.ComponentStatus{
				Status:        core.StatusFail,
				Output:        "Vault is sealed. " + statusOutput,
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		} else if healthResponse.Standby {
			v.updateStatus(core.ComponentStatus{
				Status:        core.StatusWarn, // Standby is often a warning, not a failure for overall system health
				Output:        "Vault is in standby mode. " + statusOutput,
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		} else if healthResponse.PerformanceStandby { // Add this specific check
			v.updateStatus(core.ComponentStatus{
				Status:        core.StatusFail,
				Output:        "Vault is in performance standby. " + statusOutput,
				Time:          startTime,
				ObservedValue: elapsedTime.Seconds(),
				ObservedUnit:  "s",
			})
		}
	}
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (v *vaultChecker) updateStatus(newStatus core.ComponentStatus) {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if v.currentStatus.Status != newStatus.Status || v.currentStatus.Output != newStatus.Output {
		v.currentStatus = newStatus
		select {
		case v.statusChangeChan <- newStatus:
		default:
			// Non-blocking send: if the channel buffer is full,
			// it means no one is listening fast enough or the buffer is too small.
			// This prevents blocking the health check loop itself.
		}
	}
}
