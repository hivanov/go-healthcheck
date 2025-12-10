package solace

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/Azure/go-amqp" // Official Solace Go AMQP client
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// mockSolaceConnection implements solaceConnection for mocking in tests.
type mockSolaceConnection struct {
	closeFunc      func() error
	newSessionFunc func(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error)
}

// newMockSolaceConnection creates a new mockSolaceConnection with default behavior.
func newMockSolaceConnection() *mockSolaceConnection {
	return &mockSolaceConnection{
		closeFunc: func() error { return nil },
		newSessionFunc: func(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error) {
			return newMockSolaceSession(), nil
		},
	}
}

func (m *mockSolaceConnection) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockSolaceConnection) NewSession(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error) {
	if m.newSessionFunc != nil {
		return m.newSessionFunc(ctx, opts)
	}
	return newMockSolaceSession(), nil // Default mock session
}

// mockSolaceSession implements solaceSession for mocking in tests.
type mockSolaceSession struct {
	newSenderFunc   func(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error)
	newReceiverFunc func(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error)
	closeFunc       func() error
}

// newMockSolaceSession creates a new mockSolaceSession with default behavior.
func newMockSolaceSession() *mockSolaceSession {
	return &mockSolaceSession{
		closeFunc: func() error { return nil },
		newSenderFunc: func(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error) {
			return newMockSolaceSender(), nil
		},
		newReceiverFunc: func(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error) {
			return newMockSolaceReceiver(), nil
		},
	}
}

func (m *mockSolaceSession) NewSender(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error) {
	if m.newSenderFunc != nil {
		return m.newSenderFunc(ctx, target, opts)
	}
	return &mockSolaceSender{}, nil // Default mock sender
}

func (m *mockSolaceSession) NewReceiver(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error) {
	if m.newReceiverFunc != nil {
		return m.newReceiverFunc(ctx, source, opts)
	}
	return &mockSolaceReceiver{}, nil // Default mock receiver
}

func (m *mockSolaceSession) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// mockSolaceSender implements solaceSender for mocking in tests.
type mockSolaceSender struct {
	sendFunc  func(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error
	closeFunc func() error
}

// newMockSolaceSender creates a new mockSolaceSender with default behavior.
func newMockSolaceSender() *mockSolaceSender {
	return &mockSolaceSender{
		sendFunc:  func(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error { return nil },
		closeFunc: func() error { return nil },
	}
}

func (m *mockSolaceSender) Send(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, msg, opts)
	}
	return nil
}

func (m *mockSolaceSender) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// mockSolaceReceiver implements solaceReceiver for mocking in tests.
type mockSolaceReceiver struct {
	receiveFunc       func(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error)
	acceptMessageFunc func(ctx context.Context, msg *amqp.Message) error
	closeFunc         func() error
}

// newMockSolaceReceiver creates a new mockSolaceReceiver with default behavior.
func newMockSolaceReceiver() *mockSolaceReceiver {
	return &mockSolaceReceiver{
		receiveFunc: func(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error) {
			return amqp.NewMessage([]byte("mock message")), nil
		},
		acceptMessageFunc: func(ctx context.Context, msg *amqp.Message) error { return nil },
		closeFunc:         func() error { return nil },
	}
}

func (m *mockSolaceReceiver) Receive(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error) {
	if m.receiveFunc != nil {
		return m.receiveFunc(ctx, opts)
	}
	return amqp.NewMessage([]byte("mock message")), nil // Default mock message
}

func (m *mockSolaceReceiver) AcceptMessage(ctx context.Context, msg *amqp.Message) error {
	if m.acceptMessageFunc != nil {
		return m.acceptMessageFunc(ctx, msg)
	}
	return nil
}

func (m *mockSolaceReceiver) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// setupSolaceContainer starts a Solace PubSub+ container and returns its connection string.
func setupSolaceContainer(tb testing.TB, ctx context.Context) (testcontainers.Container, string, func()) {
	// Create a context with a timeout for container startup
	startupCtx, startupCancel := context.WithTimeout(ctx, 10*time.Minute) // Increased timeout for Solace startup
	defer startupCancel()

	// Using the Solace PubSub+ container. This image needs acceptance of license.
	// Ensure you have accepted the license at https://solace.com/downloads/
	// and are logged into Docker Hub with 'docker login' to pull the image.
	req := testcontainers.ContainerRequest{
		Image:        "solace/solace-pubsub-standard:latest", // Or a specific version like "solace/solace-pubsub-standard:10.7.0.26"
		ExposedPorts: []string{"8080/tcp", "55555/tcp"},      // 8080 is admin, 55555 is AMQP
		ShmSize:      1073741824,                             // 1GB in bytes - this should be sufficient, but setting Tmpfs explicitly just in case.
		Tmpfs: map[string]string{
			"/dev/shm": "rw,size=1g",
		},
		User: "0:0", // Run as root to avoid permission issues
		Env: map[string]string{
			"PUBSUB_EMPVPN_NAME":     "default",
			"PUBSUB_MSG_SPOOL_SIZE":  "1500", // MB
			"PUBSUB_MAX_CONNECTIONS": "100",
			"SOLACE_ADMIN_USERNAME":  "admin",
			"SOLACE_ADMIN_PASSWORD":  "admin",
			// Enable AMQP specifically
			"solace_enable_default_amqp_listeners": "true",
		},
		WaitingFor: wait.ForLog("Solace PubSub+ Standard Edition is READY.").WithStartupTimeout(5 * time.Minute),
	}
	solaceContainer, err := testcontainers.GenericContainer(startupCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(tb, err, "Failed to start Solace PubSub+ container")
	require.NotNil(tb, solaceContainer, "Solace PubSub+ container is nil")

	// Get AMQP port
	amqpPort, err := solaceContainer.MappedPort(ctx, "55555/tcp")
	require.NoError(tb, err, "Failed to get AMQP mapped port")

	// Get host
	host, err := solaceContainer.Host(ctx)
	require.NoError(tb, err, "Failed to get container host")

	// Solace AMQP connection string format
	connStr := fmt.Sprintf("amqp://admin:admin@%s:%s", host, amqpPort.Port())

	return solaceContainer, connStr, func() {
		if err := solaceContainer.Terminate(ctx); err != nil {
			tb.Logf("Failed to terminate Solace PubSub+ container: %v", err)
		}
	}
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
			tb.Fatalf("Timed out waiting for status '%s'. Current status: '%s', Output: '%s'", expectedStatus, currentStatus.Status, currentStatus.Output)
		case newStatus := <-statusChangeChan:
			if newStatus.Status == expectedStatus {
				return
			}
			currentStatus = newStatus // Update current status to reflect the latest received
		case <-time.After(5 * time.Millisecond): // Small delay to avoid busy-waiting for initial status
			currentStatus = checker.Status()
			if currentStatus.Status == expectedStatus {
				return
			}
		}
	}
}
