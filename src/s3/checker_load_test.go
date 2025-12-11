package s3

import (
	"context"
	"healthcheck/core"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

const (
	minCallsPerSecond = 200
	testDuration      = 2 * time.Second
	testConcurrency   = 50
)

// TestS3Checker_HealthLoad tests the load handling of the Health() method with a mock client.
func TestS3Checker_HealthLoad(t *testing.T) {
	mockClient := &mockClient{
		headBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
			return &s3.HeadBucketOutput{}, nil // Always return success
		},
	}

	desc := core.Descriptor{ComponentID: "s3-load-test", ComponentType: "s3"}
	checkInterval := 1 * time.Second
	operationTimeout := 1 * time.Second
	s3Config := &Config{BucketName: "test-bucket", Region: "us-east-1"}

	checker := newS3CheckerInternal(desc, checkInterval, operationTimeout, s3Config, mockClient)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	var totalCalls int64
	var wg sync.WaitGroup
	wg.Add(testConcurrency)

	startTime := time.Now()

	for i := 0; i < testConcurrency; i++ {
		go func() {
			defer wg.Done()
			for time.Since(startTime) < testDuration {
				_ = checker.Health()
				atomic.AddInt64(&totalCalls, 1)
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(atomic.LoadInt64(&totalCalls)) / duration.Seconds()

	t.Logf("Health() method handled %d calls in %v", atomic.LoadInt64(&totalCalls), duration)
	t.Logf("Actual calls per second: %.2f", actualCallsPerSecond)

	require.GreaterOrEqual(t, actualCallsPerSecond, float64(minCallsPerSecond), "Health() method should handle at least %d calls per second", minCallsPerSecond)
}

// TestS3Checker_HealthLoad_WithRealS3 tests the load handling of the Health() method against a real containerized S3.
func TestS3Checker_HealthLoad_WithRealS3(t *testing.T) {
	ctx := t.Context()
	localstackContainer, s3Config, cleanup := setupS3Container(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "s3-load-test-real-s3", ComponentType: "s3"}
	checkInterval := 50 * time.Millisecond
	operationTimeout := 1 * time.Second

	checker := NewS3Checker(desc, checkInterval, operationTimeout, s3Config)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for the checker to become healthy first
	waitForStatus(t, checker, core.StatusPass, 10*time.Second)

	var stopWg sync.WaitGroup
	stopWg.Add(1)
	// Start a goroutine to stop the container after a short delay
	go func() {
		defer stopWg.Done()
		time.Sleep(testDuration / 2) // Stop container halfway through the test duration
		t.Log("Stopping LocalStack container during load test...")
		if err := localstackContainer.Stop(ctx, nil); err != nil {
			t.Logf("Failed to stop LocalStack container: %v", err)
		}
		t.Log("LocalStack container stopped during load test.")
	}()

	var totalCalls int64
	var wg sync.WaitGroup
	wg.Add(testConcurrency)

	startTime := time.Now()

	for i := 0; i < testConcurrency; i++ {
		go func() {
			defer wg.Done()
			for time.Since(startTime) < testDuration {
				_ = checker.Health()
				atomic.AddInt64(&totalCalls, 1)
			}
		}()
	}

	wg.Wait()
	stopWg.Wait()

	duration := time.Since(startTime)
	actualCallsPerSecond := float64(atomic.LoadInt64(&totalCalls)) / duration.Seconds()

	t.Logf("Health() method (with real S3) handled %d calls in %v", atomic.LoadInt64(&totalCalls), duration)
	t.Logf("Actual calls per second (with real S3): %.2f", actualCallsPerSecond)

	require.GreaterOrEqual(t, actualCallsPerSecond, float64(minCallsPerSecond), "Health() method should handle at least %d calls per second even when S3 goes down", minCallsPerSecond)

	// Final check: the checker should be in a Fail state
	waitForStatus(t, checker, core.StatusFail, 5*time.Second)
}
