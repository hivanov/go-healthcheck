package solace

import (
	"context"
	"fmt"
	"healthcheck/core"
	"net/url" // For URL parsing validation
	"testing"
	"time"

	"github.com/Azure/go-amqp" // Updated import
	"github.com/stretchr/testify/assert"
)

func FuzzSolaceChecker_NewSolaceChecker(f *testing.F) {
	// Seed corpus with various connection strings and intervals
	f.Add("amqp://admin:admin@localhost:5672", int64(1), int64(100)) // Valid Solace AMQP config
	f.Add("invalid-amqp-url", int64(1), int64(100))                  // Invalid URL format
	f.Add("amqp://", int64(1), int64(100))                           // Incomplete URL
	f.Add("amqp://admin:admin@host:1234", int64(0), int64(0))        // Zero intervals
	f.Add("amqp://admin:admin@host:1234", int64(-1), int64(-1))      // Negative intervals

	f.Fuzz(func(t *testing.T, connectionString string, checkIntervalMs, operationsTimeoutMs int64) {
		descriptor := core.Descriptor{
			ComponentID:   "fuzz-solace",
			ComponentType: "solace",
			Description:   "Fuzz test for Solace checker",
		}

		// Ensure intervals are positive
		if checkIntervalMs <= 0 {
			checkIntervalMs = 1
		}
		checkInterval := time.Duration(checkIntervalMs) * time.Millisecond

		if operationsTimeoutMs <= 0 {
			operationsTimeoutMs = 1
		}
		operationsTimeout := time.Duration(operationsTimeoutMs) * time.Millisecond

		// Mock OpenSolaceFunc to simulate connection success/failure and Solace operations
		mockOpenSolace := func(connStr string) (solaceConnection, error) {
			_, err := url.Parse(connStr) // Basic URL parsing check
			if err != nil {
				return nil, fmt.Errorf("invalid connection string format")
			}
			// Simulate connection failure based on some criteria, e.g., "invalid" in URL
			if connStr == "amqp://invalid" {
				return nil, fmt.Errorf("mocked connection refused")
			}

			// Create concrete mock instances and configure their internal functions
			mockConnectionConcrete := newMockSolaceConnection() // Default instance
			mockSessionConcrete := newMockSolaceSession()       // Default instance
			mockSenderConcrete := newMockSolaceSender()         // Default instance
			mockReceiverConcrete := newMockSolaceReceiver()     // Default instance

			mockConnectionConcrete.newSessionFunc = func(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error) {
				if connStr == "amqp://admin:admin@session-fail" {
					return nil, fmt.Errorf("mock session creation error")
				}
				mockSessionConcrete.newSenderFunc = func(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error) {
					if target == "sender-fail" {
						return nil, fmt.Errorf("mock sender creation error")
					}
					mockSenderConcrete.sendFunc = func(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error {
						if target == "send-fail" {
							return fmt.Errorf("mock send error")
						}
						return nil
					}
					return mockSenderConcrete, nil
				}
				mockSessionConcrete.newReceiverFunc = func(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error) {
					if source == "receiver-fail" {
						return nil, fmt.Errorf("mock receiver creation error")
					}
					mockReceiverConcrete.receiveFunc = func(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error) {
						if source == "receive-fail" {
							return nil, fmt.Errorf("mock receive error")
						}
						return amqp.NewMessage([]byte("fuzz message")), nil
					}
					return mockReceiverConcrete, nil
				}
				return mockSessionConcrete, nil
			}
			return mockConnectionConcrete, nil
		}

		checker := NewSolaceCheckerWithOpenSolaceFunc(descriptor, checkInterval, operationsTimeout, connectionString, mockOpenSolace)
		defer func() {
			if err := checker.Close(); err != nil {
				t.Errorf("Checker Close() returned an unexpected error: %v", err)
			}
		}()

		if checker == nil {
			t.Skipf("NewSolaceChecker returned nil for connStr: %s", connectionString)
			return
		}

		// Exercise other methods
		_ = checker.Status()
		_ = checker.Descriptor()
		_ = checker.Health()
		checker.Disable()
		checker.Enable()
		checker.ChangeStatus(core.ComponentStatus{Status: core.StatusFail, Output: "fuzz"})

		// Allow some time for goroutines to process if necessary
		time.Sleep(checkInterval * 2)

		status := checker.Status()
		// Depending on the config, status could be Warn (initializing/disabled) or Fail (connection/operation error).
		// We primarily want to ensure it doesn't crash.
		assert.True(t, status.Status == core.StatusWarn || status.Status == core.StatusFail || status.Status == core.StatusPass,
			"Unexpected status %s for connStr: %s, output: %s", status.Status, connectionString, status.Output)
	})
}
