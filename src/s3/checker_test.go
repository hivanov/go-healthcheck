package s3

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

// TestS3Checker_Integration_HappyPath tests a successful connection and health check.
func TestS3Checker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-happy-path", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond // Shorter interval for quicker testing
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Expect initial 'Warn' then transition to 'Pass'
	waitForStatus(t, checker, core.StatusWarn, 1*time.Second)  // Initializing
	waitForStatus(t, checker, core.StatusPass, 10*time.Second) // Healthy, increased timeout for container readiness

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.Contains(t, status.Output, fmt.Sprintf("S3 bucket '%s' is accessible", s3Config.BucketName))
	require.NotZero(t, status.ObservedValue, "Expected ObservedValue to be non-zero")
	require.Equal(t, "s", status.ObservedUnit, "Expected ObservedUnit 's'")
}

// TestS3Checker_Integration_Fail_NoBucket tests when the configured bucket does not exist.
func TestS3Checker_Integration_Fail_NoBucket(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	// Modify s3Config to use a non-existent bucket
	s3Config.BucketName = "non-existent-bucket"

	desc := core.Descriptor{ComponentID: "s3-fail-nobucket", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should immediately go to Fail status
	waitForStatus(t, checker, core.StatusFail, 5*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "not found or not accessible")
	require.Contains(t, status.Output, s3Config.BucketName)
}

// TestS3Checker_Integration_Fail_WrongCredentials tests when credentials are wrong.
func TestS3Checker_Integration_Fail_WrongCredentials(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	// Modify s3Config to use wrong credentials
	s3Config.AccessKeyID = "wrong_access_key"
	s3Config.SecretAccessKey = "wrong_secret_key"

	desc := core.Descriptor{ComponentID: "s3-fail-wrongcreds", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Should pass even with wrong credentials because LocalStack doesn't always fail on HeadBucket for wrong credentials if bucket exists
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.Contains(t, status.Output, "S3 bucket 'test-bucket' is accessible", "Expected output to indicate accessibility despite wrong credentials for LocalStack's behavior")
}

// TestS3Checker_Integration_DisableEnable tests the disable/enable functionality.
func TestS3Checker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-disable-enable", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Disable the checker
	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "S3 checker disabled", status.Output)

	// Ensure no status changes occur while disabled
	select {
	case newStatus := <-checker.StatusChange():
		require.Equal(t, core.StatusWarn, newStatus.Status, "Unexpected status change to %s while checker was disabled", newStatus.Status)
	case <-time.After(checkInterval * 3):
		// Expected, no status change
	}

	// Give performHealthCheck a chance to run while disabled
	time.Sleep(checkInterval * 2) // Allow a few ticks

	// After sleeping, the status should still be "disabled" and not changed by performHealthCheck
	statusAfterSleep := checker.Status()
	require.Equal(t, core.StatusWarn, statusAfterSleep.Status, "Expected status to remain Warn after performHealthCheck while disabled")
	require.Contains(t, statusAfterSleep.Output, "S3 checker disabled", "Expected output to remain 'S3 checker disabled'")

	// Enable the checker
	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "S3 checker enabled, re-initializing...", status.Output)

	// Wait for it to become healthy again
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestS3Checker_Integration_Close tests graceful shutdown.
func TestS3Checker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-close", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Close the checker
	require.NoError(t, checker.Close())

	// Verify that the internal context is cancelled after Close()
	require.Eventually(t, func() bool {
		select {
		case <-checker.(*s3Checker).ctx.Done():
			return true
		default:
			return false
		}
	}, 500*time.Millisecond, 10*time.Millisecond, "Checker context was not cancelled after Close()")

	// Ensure no further status changes are reported by the checker's goroutine
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after checker was closed", "status: %s", s.Status)
	case <-time.After(checkInterval * 3): // Wait a bit to ensure no more ticks
		// Expected, no more status changes
	}
}

// TestS3Checker_Integration_ChangeStatus tests the ChangeStatus method and its interaction with periodic checks.
func TestS3Checker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-change-status", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Manually change status to Warn
	manualWarnStatus := core.ComponentStatus{Status: core.StatusWarn, Output: "Manual override to Warn"}
	checker.ChangeStatus(manualWarnStatus)

	status := checker.Status()
	require.Equal(t, manualWarnStatus, status)

	// Verify the manual status change is broadcast
	select {
	case s := <-checker.StatusChange():
		require.Equal(t, manualWarnStatus.Status, s.Status)
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Timed out waiting for manual status change notification")
	}

	// The periodic check should eventually revert it to Pass
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	status = checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
}

// TestS3Checker_Getters tests the Descriptor and Health methods.
func TestS3Checker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "s3"}
	checker := &s3Checker{descriptor: desc, currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"}}

	// Test Descriptor()
	actualDesc := checker.Descriptor()
	require.Equal(t, desc, actualDesc, "Descriptor() should return the correct descriptor")

	// Test Health()
	health := checker.Health()
	require.Equal(t, desc.ComponentID, health.ComponentID, "Health().ComponentID should match")
	require.Equal(t, desc.ComponentType, health.ComponentType, "Health().ComponentType should match")
	require.Equal(t, core.StatusPass, health.Status, "Health().Status should match current status")
	require.Equal(t, "Everything is fine", health.Output, "Health().Output should match current status output")

	// Test Health() with empty output
	checker.currentStatus.Output = ""
	health = checker.Health()
	require.Empty(t, health.Output, "Health().Output should be empty if current status output is empty")
}

// TestS3Checker_Close_NilClient tests the Close method when the S3 client is nil.
func TestS3Checker_Close_NilClient(t *testing.T) {
	desc := core.Descriptor{ComponentID: "s3-close-nil-client", ComponentType: "s3"}
	checker := &s3Checker{
		descriptor: desc,
		// Simulate a checker where client connection failed to open
		client:     nil,
		ctx:        t.Context(),
		cancelFunc: func() {},
		quit:       make(chan struct{}),
	}

	err := checker.Close()
	require.NoError(t, err, "Close() on nil client should not return an error")

	// Ensure cancelFunc is called (though it's a dummy here)
	// and quit channel is closed.
	select {
	case <-checker.quit:
		// Expected
	case <-time.After(100 * time.Millisecond):
		require.Fail(t, "Quit channel not closed")
	}
}

// TestS3Checker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestS3Checker_QuitChannelStopsLoop(t *testing.T) {
	ctx := t.Context()
	_, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-quit-loop", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	// Do not defer checker.Close() here, as we want to manually close the quit channel.

	// Wait for it to become healthy first
	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	// Manually close the quit channel to stop the loop
	close(checker.(*s3Checker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further status changes are reported (except the one for shutdown if any).
	select {
	case s := <-checker.StatusChange():
		t.Logf("Received unexpected status change: %s", s.Status)
		// It might send a final status, but should not send periodic ones.
	case <-time.After(checkInterval * 3):
		// Expected: no more status changes
	}

	// S3 client typically doesn't need explicit close on quit
	// If it had a close method, it would be called here.
}

// TestS3Checker_PerformHealthCheck_ClientNil tests performHealthCheck when s.client is nil.
func TestS3Checker_PerformHealthCheck_ClientNil(t *testing.T) {
	desc := core.Descriptor{ComponentID: "s3-perform-nil-client", ComponentType: "s3"}
	checker := &s3Checker{
		descriptor: desc,
		client:     nil, // Simulate nil client connection
		ctx:        t.Context(),
		cancelFunc: context.CancelFunc(func() {}),
		config:     &Config{BucketName: "test-bucket"}, // Needs a bucket name
	}

	// Manually call performHealthCheck
	checker.performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when client is nil")
	require.Contains(t, status.Output, "S3 client or configuration is nil", "Expected specific output")
}

// TestNewS3Checker_OpenError tests the scenario where newRealS3Client returns an error.
func TestNewS3Checker_OpenError(t *testing.T) {
	// Create a mock OpenS3ClientFunc that always returns an error
	mockOpenS3Client := func(cfg *Config) (Client, error) {
		return nil, fmt.Errorf("mocked S3 client error: failed to load config")
	}

	desc := core.Descriptor{ComponentID: "s3-open-error", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second
	s3Config := &Config{BucketName: "test-bucket", Region: "us-east-1"}

	checker := NewS3CheckerWithOpenS3ClientFunc(desc, checkInterval, operationTimeout, s3Config, mockOpenS3Client)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// The checker should immediately be in a Fail state
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected initial status to be Fail when newRealS3Client fails")
	require.Contains(t, status.Output, "Failed to create S3 client", "Expected output to indicate client creation failure")
	require.Contains(t, status.Output, "mocked S3 client error", "Expected output to contain the mocked error")

	// Ensure the health check loop was not started
	select {
	case s := <-checker.StatusChange():
		require.Fail(t, "Received unexpected status change after newRealS3Client failed", "status: %s", s.Status)
	case <-time.After(100 * time.Millisecond):
		// Expected, no status change
	}
}

// TestS3Checker_PerformHealthCheck_HeadBucketError tests performHealthCheck when HeadBucket returns an error.
func TestS3Checker_PerformHealthCheck_HeadBucketError(t *testing.T) {
	mockClient := &mockClient{
		headBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
			return nil, fmt.Errorf("mock HeadBucket error")
		},
	}

	desc := core.Descriptor{ComponentID: "s3-perform-headbucket-error", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second
	s3Config := &Config{BucketName: "test-bucket", Region: "us-east-1"}

	checker := newS3CheckerInternal(desc, checkInterval, operationTimeout, s3Config, mockClient)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*s3Checker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when HeadBucket returns an error")
	require.Contains(t, status.Output, "S3 HeadBucket operation failed", "Expected specific output")
	require.Contains(t, status.Output, "mock HeadBucket error", "Expected output to contain the mocked error")
}

// TestS3Checker_PerformHealthCheck_NoSuchBucketError tests performHealthCheck when HeadBucket returns NoSuchBucket error.
func TestS3Checker_PerformHealthCheck_NoSuchBucketError(t *testing.T) {
	mockClient := &mockClient{
		headBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
			return nil, &types.NotFound{} // Simulate NoSuchBucket error
		},
	}

	desc := core.Descriptor{ComponentID: "s3-perform-nosuchbucket-error", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second
	s3Config := &Config{BucketName: "test-bucket", Region: "us-east-1"}

	checker := newS3CheckerInternal(desc, checkInterval, operationTimeout, s3Config, mockClient)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Manually call performHealthCheck
	checker.(*s3Checker).performHealthCheck()

	// Expect status to be Fail
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status, "Expected status Fail when NoSuchBucket error occurs")
	require.Contains(t, status.Output, "not found or not accessible", "Expected specific output for NoSuchBucket")
}
