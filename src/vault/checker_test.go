package vault_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"

	"healthcheck/core"
	checker "healthcheck/vault"
	// test_utils_test is in the same package, so no import needed.
)

// TestMain sets up and tears down the Vault container for integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	GlobalVaultContainer, GlobalVaultAddr, GlobalVaultClient, err = SetupVaultContainerForTests(ctx)
	if err != nil {
		log.Fatalf("Failed to setup Vault container for tests: %v", err)
	}

	log.Printf("Vault running at: %s", GlobalVaultAddr)

	code := m.Run()

	// Teardown container
	if err := GlobalVaultContainer.Terminate(ctx); err != nil {
		log.Fatalf("Failed to terminate Vault container: %v", err)
	}

	os.Exit(code)
}

// TestNewVaultChecker_Success tests the successful creation of a Vault checker.
func TestNewVaultChecker_Success(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault",
		ComponentType: "vault",
		Description:   "Test Vault checker",
	}

	testChecker := checker.NewVaultChecker(descriptor, 1*time.Second, api.DefaultConfig())
	assert.NotNil(t, testChecker)

	// Ensure the checker starts in a "warn" or "pass" state after initialization
	status := testChecker.Status()
	assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusPass, "Expected initial status to be Warn or Pass, got %s", status.Status)

	// Clean up
	err := testChecker.Close()
	assert.NoError(t, err)
}

// TestVaultChecker_Health_Pass verifies that a healthy Vault returns a "pass" status.
func TestVaultChecker_Health_Pass(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-health-pass",
		ComponentType: "vault",
		Description:   "Test Vault health pass",
	}

	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}

	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Second, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)

	// Give it a moment to perform the initial check
	time.Sleep(2 * time.Second)

	health := testChecker.Health()
	assert.Equal(t, core.StatusPass, health.Status, "Expected Vault health to be 'pass', got '%s'", health.Status)
	assert.Contains(t, health.Output, "Vault is healthy and active")

	// Clean up
	err := testChecker.Close()
	assert.NoError(t, err)
}

type mockVaultClient struct {
	sysFunc        func() checker.VaultSysInterface
	closeFunc      func()
	healthResponse *api.HealthResponse
	healthError    error
}

func (m *mockVaultClient) Sys() checker.VaultSysInterface {
	if m.sysFunc != nil {
		return m.sysFunc()
	}
	return &mockSysClient{
		healthResponse: m.healthResponse,
		healthError:    m.healthError,
	}
}

func (m *mockVaultClient) Close() {
	if m.closeFunc != nil {
		m.closeFunc()
	}
}

type mockSysClient struct {
	healthResponse *api.HealthResponse
	healthError    error
}

func (m *mockSysClient) Health() (*api.HealthResponse, error) {
	return m.healthResponse, m.healthError
}

// TestNewVaultChecker_ErrorOpenClient tests the error path during Vault client creation.
func TestNewVaultChecker_ErrorOpenClient(t *testing.T) {
	descriptor := core.Descriptor{
		ComponentID:   "test-vault-error-client",
		ComponentType: "vault",
		Description:   "Test Vault checker with client creation error",
	}
	expectedError := fmt.Errorf("failed to create client for test")

	// Mock OpenVaultClientFunc to return an error
	errorOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return nil, expectedError
	}

	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Second, api.DefaultConfig(), errorOpenClient)
	assert.NotNil(t, testChecker)

	// The checker should be in a Fail state with the error message
	status := testChecker.Status()
	assert.Equal(t, core.StatusFail, status.Status)
	assert.Contains(t, status.Output, expectedError.Error())

	// Ensure Close does not panic on a dummy checker
	err := testChecker.Close()
	assert.NoError(t, err)
}

// TestVaultChecker_VariousHealthStates tests different Vault health scenarios.
func TestVaultChecker_VariousHealthStates(t *testing.T) {
	testCases := []struct {
		name           string
		healthResponse *api.HealthResponse
		healthError    error
		expectedStatus core.StatusEnum
		expectedOutput string
	}{
		{
			name:           "Vault client Sys().Health() returns error",
			healthResponse: nil,
			healthError:    fmt.Errorf("network error"),
			expectedStatus: core.StatusFail,
			expectedOutput: "Vault health check failed: network error",
		},
		{
			name: "Vault Uninitialized",
			healthResponse: &api.HealthResponse{
				Initialized: false,
				Sealed:      true,
				Standby:     true,
			},
			healthError:    nil,
			expectedStatus: core.StatusFail,
			expectedOutput: "Vault is uninitialized",
		},
		{
			name: "Vault Sealed",
			healthResponse: &api.HealthResponse{
				Initialized: true,
				Sealed:      true,
				Standby:     false,
			},
			healthError:    nil,
			expectedStatus: core.StatusFail,
			expectedOutput: "Vault is sealed",
		},
		{
			name: "Vault Standby",
			healthResponse: &api.HealthResponse{
				Initialized: true,
				Sealed:      false,
				Standby:     true,
			},
			healthError:    nil,
			expectedStatus: core.StatusWarn,
			expectedOutput: "Vault is in standby mode",
		},
		{
			name: "Vault Performance Standby",
			healthResponse: &api.HealthResponse{
				Initialized:        true,
				Sealed:             false,
				Standby:            false,
				PerformanceStandby: true,
			},
			healthError:    nil,
			expectedStatus: core.StatusFail,
			expectedOutput: "Vault is in performance standby",
		},
		{
			name: "Vault Replicated Degraded",
			healthResponse: &api.HealthResponse{
				Initialized:                true,
				Sealed:                     false,
				Standby:                    true, // Make it standby so it's a WARN
				PerformanceStandby:         false,
				ReplicationPerformanceMode: "secondary",
				ReplicationDRMode:          "dr-secondary",
			},
			healthError:    nil,
			expectedStatus: core.StatusWarn,
			expectedOutput: "Vault is in standby mode. Vault status: Initialized=true, Sealed=false, Standby=true, PerformanceStandby=false, ReplicationPerformanceMode=secondary, ReplicationDRMode=dr-secondary",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			descriptor := core.Descriptor{
				ComponentID:   fmt.Sprintf("test-vault-%s", tc.name),
				ComponentType: "vault",
				Description:   fmt.Sprintf("Test Vault health check for %s", tc.name),
			}

			// Create a mock Vault client that returns the specified health response/error
			mockClient := &mockVaultClient{
				healthResponse: tc.healthResponse,
				healthError:    tc.healthError,
			}

			testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 10*time.Millisecond, api.DefaultConfig(),
				func(config *api.Config) (checker.VaultClient, error) {
					return mockClient, nil
				})
			assert.NotNil(t, testChecker)
			defer func() {
				err := testChecker.Close()
				assert.NoError(t, err)
			}()

			// Give it a moment to perform the check
			time.Sleep(50 * time.Millisecond) // Allow at least one check to run

			status := testChecker.Status()
			assert.Equal(t, tc.expectedStatus, status.Status, "Expected status for %s", tc.name)
			assert.Contains(t, status.Output, tc.expectedOutput, "Expected output for %s", tc.name)
		})
	}
}

// TestVaultChecker_NilClientInPerformHealthCheck tests the case where the client object is nil within performHealthCheck.
func TestVaultChecker_NilClientInPerformHealthCheck(t *testing.T) {
	descriptor := core.Descriptor{
		ComponentID:   "test-vault-nil-client",
		ComponentType: "vault",
		Description:   "Test Vault checker with nil client in performHealthCheck",
	}

	// This mockOpenClient returns a nil checker.VaultClient, but no error,
	// so NewVaultCheckerWithOpenVaultClientFunc proceeds to call newVaultCheckerInternal
	// with a nil client.
	nilClientOpenFunc := func(config *api.Config) (checker.VaultClient, error) {
		return nil, nil // Return nil client, no error
	}

	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 10*time.Millisecond, api.DefaultConfig(), nilClientOpenFunc)
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	// Give it a moment to perform the check
	time.Sleep(50 * time.Millisecond) // Allow at least one check to run

	status := testChecker.Status()
	assert.Equal(t, core.StatusFail, status.Status)
	assert.Contains(t, status.Output, "Vault client object is nil")
}

// TestVaultChecker_UpdateStatus_ChannelFull tests the default case of non-blocking send to statusChangeChan.
func TestVaultChecker_UpdateStatus_ChannelFull(t *testing.T) {
	descriptor := core.Descriptor{
		ComponentID:   "test-vault-channel-full",
		ComponentType: "vault",
		Description:   "Test updateStatus with full channel",
	}

	// Create a mock Vault client that always returns a Pass status
	mockClient := &mockVaultClient{
		healthResponse: &api.HealthResponse{Initialized: true, Sealed: false, Standby: false, PerformanceStandby: false},
		healthError:    nil,
	}

	// Create a checker instance using the mock client. Use a very long check interval
	// to prevent periodic background checks from interfering with the manual status updates.
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Hour, api.DefaultConfig(),
		func(config *api.Config) (checker.VaultClient, error) {
			return mockClient, nil
		})
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	// Give the checker time to perform its initial health check and send the PASS status to the channel.
	time.Sleep(100 * time.Millisecond) // Short delay to allow goroutine to run

	statusChan := testChecker.StatusChange()

	// Consume the first actual PASS status from the checker's initial health check
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected first status to be PASS")
	case <-time.After(2 * time.Second): // Increased timeout
		t.Fatal("Timed out waiting for first PASS status from checker's initial health check")
	}

	// Now the channel is empty. We will force a status change to fill the buffer (size 1).
	// This status needs to be different from the current PASS status to trigger an update.
	testChecker.ChangeStatus(core.ComponentStatus{
		Status: core.StatusFail,
		Output: "Forced fail 1 to fill channel",
		Time:   time.Now(),
	})

	// Wait briefly to ensure the channel has received the update and is now full.
	time.Sleep(10 * time.Millisecond)

	// Now force another status change. This status must be different from the previous one (FAIL)
	// to trigger an attempt to send. This should attempt to send to a full channel, hitting the default case.
	testChecker.ChangeStatus(core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "Forced warn 2 to hit default case",
		Time:   time.Now(),
	})

	// Give it a moment for the non-blocking send to occur
	time.Sleep(50 * time.Millisecond)

	// Verify that only the first forced status (FAIL) is present in the channel
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusFail, status.Status)
		assert.Contains(t, status.Output, "Forced fail 1 to fill channel")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for first forced status (FAIL)")
	}

	// The second forced status (WARN) should have been dropped due to the channel being full.
	// No further status should be immediately available.
	select {
	case status := <-statusChan:
		t.Fatalf("Unexpected status received: %v", status)
	case <-time.After(50 * time.Millisecond):
		// Expected: no status received, default case was hit.
	}
}

// TestVaultChecker_Close tests the Close method.
func TestVaultChecker_Close(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-close",
		ComponentType: "vault",
		Description:   "Test Vault close",
	}

	// Create a checker that uses the real Vault client indirectly
	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Second, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)

	// Close the checker
	err := testChecker.Close()
	assert.NoError(t, err)

	// Verify that after closing, further health checks might fail or the status remains stable
	// (depending on implementation - here, it should stop updates and retain last known status or error)
	time.Sleep(1 * time.Second) // Give time for goroutine to stop
	statusAfterClose := testChecker.Status()
	// The status should ideally not change after close, or indicate a shutdown state.
	// For simplicity, we just check no error on close and that the status accessor still works.
	assert.NotEqual(t, core.StatusWarn, statusAfterClose.Status) // Should not be unknown
}

// TestVaultChecker_Disable_Enable tests the Disable and Enable methods.
func TestVaultChecker_Disable_Enable(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-disable-enable",
		ComponentType: "vault",
		Description:   "Test Vault disable/enable",
	}

	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Second, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	// Give it a moment to perform initial check and propagate to channel
	statusChan := testChecker.StatusChange()
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected initial status to be Pass, got %s", status.Status)
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for initial Pass status")
	}

	// Disable the checker
	testChecker.Disable()
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusWarn, status.Status, "Expected status to be Warn after Disable, got %s", status.Status)
		assert.Contains(t, status.Output, "Vault checker disabled")
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for disabled status")
	}

	// Ensure status doesn't change when disabled by waiting for another period.
	// If a status comes through, it means the disable logic failed.
	select {
	case status := <-statusChan:
		t.Fatalf("Received unexpected status while disabled: %v", status)
	case <-time.After(3 * time.Second): // Wait for a few check intervals
		// Expected: no status update received, meaning the checker remained disabled.
	}
	assert.Equal(t, core.StatusWarn, testChecker.Status().Status, "Expected status to remain Warn when disabled, got %s", testChecker.Status().Status)
	assert.Contains(t, testChecker.Status().Output, "Vault checker disabled")

	// Enable the checker
	testChecker.Enable()
	// Expect "re-initializing" status first
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusWarn, status.Status, "Expected status to be Warn after Enable (re-initializing), got %s", status.Status)
		assert.Contains(t, status.Output, "Vault checker enabled, re-initializing...")
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for re-initializing status")
	}

	// Then expect "pass" status after the first successful check
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected status to be Pass after Enable and first check, got %s", status.Status)
		assert.Contains(t, status.Output, "Vault is healthy and active")
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for initial Pass status after re-enabling")
	}
}

// TestVaultChecker_ChangeStatus tests the ChangeStatus method.
func TestVaultChecker_ChangeStatus(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-changestatus",
		ComponentType: "vault",
		Description:   "Test Vault change status",
	}

	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 10*time.Minute, api.DefaultConfig(), mockOpenClient) // Long interval to avoid immediate overwrite
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	// Give it a moment to perform initial check
	time.Sleep(1 * time.Second)
	initialStatus := testChecker.Status()
	assert.Equal(t, core.StatusPass, initialStatus.Status)

	// Change status externally
	externalStatus := core.ComponentStatus{
		Status: core.StatusFail,
		Output: "External system forced fail",
		Time:   time.Now(),
	}
	testChecker.ChangeStatus(externalStatus)
	time.Sleep(100 * time.Millisecond) // Allow channel send
	changedStatus := testChecker.Status()
	assert.Equal(t, core.StatusFail, changedStatus.Status)
	assert.Contains(t, changedStatus.Output, "External system forced fail")

	// Ensure the external status holds until the next periodic check (which is long)
	time.Sleep(1 * time.Second)
	statusAfterWait := testChecker.Status()
	assert.Equal(t, core.StatusFail, statusAfterWait.Status)
	assert.Contains(t, statusAfterWait.Output, "External system forced fail")
}

// TestVaultChecker_Descriptor tests the Descriptor method.
func TestVaultChecker_Descriptor(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	expectedDescriptor := core.Descriptor{
		ComponentID:   "test-vault-descriptor",
		ComponentType: "vault",
		Description:   "Test Vault descriptor",
		Version:       "1.0.0",
		ReleaseID:     "v1.0.0-rc1",
	}

	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(expectedDescriptor, 1*time.Second, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	actualDescriptor := testChecker.Descriptor()
	assert.Equal(t, expectedDescriptor, actualDescriptor)
}

// TestVaultChecker_StatusChange tests the StatusChange channel.
func TestVaultChecker_StatusChange(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-status-change",
		ComponentType: "vault",
		Description:   "Test Vault status change",
	}

	mockOpenClient := func(config *api.Config) (checker.VaultClient, error) {
		return &realVaultClient{client: GlobalVaultClient}, nil
	}
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 500*time.Millisecond, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	statusChan := testChecker.StatusChange()

	// Expect an initial status update (either Warn or Pass)
	select {
	case status := <-statusChan:
		assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusPass, "Expected initial status to be Warn or Pass, got %s", status.Status)
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for initial status")
	}

	// Change status externally to trigger an update
	newStatusFail := core.ComponentStatus{
		Status: core.StatusFail,
		Output: "Force failed for channel test",
	}
	testChecker.ChangeStatus(newStatusFail)
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusFail, status.Status, "Expected status to be Fail after ChangeStatus, got %s", status.Status)
		assert.Contains(t, status.Output, "Force failed for channel test")
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for ChangeStatus to Fail update")
	}

	// Change status back to Pass externally to trigger another update
	newStatusPass := core.ComponentStatus{
		Status: core.StatusPass,
		Output: "Force passed for channel test",
	}
	testChecker.ChangeStatus(newStatusPass)
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected status to be Pass after second ChangeStatus, got %s", status.Status)
		assert.Contains(t, status.Output, "Force passed for channel test")
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for ChangeStatus to Pass update")
	}
}

// TestVaultChecker_UserPassAuth tests authentication using the userpass method and then performs a health check.
func TestVaultChecker_UserPassAuth(t *testing.T) {
	assert.NotNil(t, GlobalVaultClient, "Vault client should be initialized in TestMain")

	descriptor := core.Descriptor{
		ComponentID:   "test-vault-userpass-auth",
		ComponentType: "vault",
		Description:   "Test Vault userpass authentication",
	}

	// This mockOpenClient will perform the full authentication flow each time it's called.
	mockOpenClient := func(cfg *api.Config) (checker.VaultClient, error) {
		// 1. Construct a new Vault API client configured for the GlobalVaultAddr.
		config := api.DefaultConfig()
		config.Address = GlobalVaultAddr // Use the global address from Testcontainers
		client, err := api.NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create client in mockOpenClient: %v", err)
		}

		// 2. Authenticate using the userpass method with "testuser" and "testpassword".
		options := map[string]interface{}{
			"password": "testpassword",
		}
		secret, err := client.Logical().Write("auth/userpass/login/testuser", options)
		if err != nil {
			return nil, fmt.Errorf("failed to login to Vault with userpass in mockOpenClient: %v", err)
		}
		if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
			return nil, fmt.Errorf("userpass login did not return a client token in mockOpenClient")
		}

		// 3. Set the client token for subsequent operations.
		client.SetToken(secret.Auth.ClientToken)
		return &realVaultClient{client: client}, nil
	}

	// Initialize a checker.NewVaultCheckerWithOpenVaultClientFunc with the mockOpenClient.
	testChecker := checker.NewVaultCheckerWithOpenVaultClientFunc(descriptor, 1*time.Second, api.DefaultConfig(), mockOpenClient)
	assert.NotNil(t, testChecker)
	defer func() {
		err := testChecker.Close()
		assert.NoError(t, err)
	}()

	// Give it a moment to perform the initial check
	time.Sleep(2 * time.Second)

	// 6. Verify that the health check status is core.StatusPass.
	health := testChecker.Health()
	assert.Equal(t, core.StatusPass, health.Status, "Expected Vault health to be 'pass' after userpass auth, got '%s'", health.Status)
	assert.Contains(t, health.Output, "Vault is healthy and active")
}

// realVaultClient (used internally by checker) needs to be defined in the same package
// for direct access here or wrap it in a function. Since it's internal to checker.go,
// I have defined a mockVaultClient locally for cases where direct access is needed,
// but for most integration tests, NewVaultCheckerWithOpenVaultClientFunc is sufficient
// to inject the real *api.Client wrapped by an internal realVaultClient.
type realVaultClient struct {
	client *api.Client
}

func (r *realVaultClient) Sys() checker.VaultSysInterface {
	return r.client.Sys()
}

func (r *realVaultClient) Close() {
	// No-op for real client, as explained in checker.go
}
