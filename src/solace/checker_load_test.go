package solace

import (
	"context"
	"healthcheck/core"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/stretchr/testify/require"
)

const (
	minCallsPerSecond = 200
	testDuration      = 2 * time.Second
	testConcurrency   = 50
)

// TestSolaceChecker_HealthLoad tests the load handling of the Health() method with a mock client.
func TestSolaceChecker_HealthLoad(t *testing.T) {
	mockSenderConcrete := newMockSolaceSender()
	mockSenderConcrete.sendFunc = func(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error { return nil }

	mockReceiverConcrete := newMockSolaceReceiver()
	mockReceiverConcrete.receiveFunc = func(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error) {
		return amqp.NewMessage([]byte("mock")), nil
	}

	mockSessionConcrete := newMockSolaceSession()
	mockSessionConcrete.newSenderFunc = func(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error) {
		return mockSenderConcrete, nil
	}
	mockSessionConcrete.newReceiverFunc = func(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error) {
		return mockReceiverConcrete, nil
	}

	mockConnectionConcrete := newMockSolaceConnection()
	mockConnectionConcrete.newSessionFunc = func(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error) {
		return mockSessionConcrete, nil
	}

	desc := core.Descriptor{ComponentID: "solace-load-test", ComponentType: "solace"}
	checkInterval := 1 * time.Second
	operationsTimeout := 1 * time.Second
	// connectionString := "amqp://localhost:5672" // Not needed for mock calls

	checker := newSolaceCheckerInternal(desc, checkInterval, operationsTimeout, mockConnectionConcrete)
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

// TestSolaceChecker_HealthLoad_WithRealSolace tests the load handling of the Health() method against a real containerized Solace.
func TestSolaceChecker_HealthLoad_WithRealSolace(t *testing.T) {
	ctx := t.Context()
	solaceContainer, connectionString, cleanup := setupSolaceContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "solace-load-test-real-solace", ComponentType: "solace"}
	checkInterval := 50 * time.Millisecond
	operationsTimeout := 5 * time.Second // Increased timeout for real operations

	checker := NewSolaceChecker(desc, checkInterval, operationsTimeout, connectionString)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	// Wait for the checker to become healthy first
	waitForStatus(t, checker, core.StatusPass, 20*time.Second) // Increased timeout for initial check

	var stopWg sync.WaitGroup
	stopWg.Add(1)
	go func() {
		defer stopWg.Done()
		time.Sleep(testDuration / 2) // Stop container halfway through the test duration
		t.Log("Stopping Solace PubSub+ container during load test...")
		if err := solaceContainer.Stop(ctx, nil); err != nil {
			t.Logf("Failed to stop Solace PubSub+ container: %v", err)
		}
		t.Log("Solace PubSub+ container stopped during load test.")
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

	t.Logf("Health() method (with real Solace) handled %d calls in %v", atomic.LoadInt64(&totalCalls), duration)
	t.Logf("Actual calls per second (with real Solace): %.2f", actualCallsPerSecond)

	require.GreaterOrEqual(t, actualCallsPerSecond, float64(minCallsPerSecond), "Health() method should handle at least %d calls per second even when Solace goes down", minCallsPerSecond)

	// Final check: the checker should be in a Fail state
	waitForStatus(t, checker, core.StatusFail, 5*time.Second)
}
