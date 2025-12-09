package rabbitmq

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"testing"
	"time"


	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

// Helper function to create a test RabbitMQ container
func setupRabbitMQ(ctx context.Context) (*rabbitmq.RabbitMQContainer, string, error) {
	rabbitmqContainer, err := rabbitmq.Run(ctx, "rabbitmq:3.12.11-management-alpine",
		rabbitmq.WithAdminUsername("guest"),
		rabbitmq.WithAdminPassword("guest"),
		testcontainers.WithImage("rabbitmq:3.12.11-management-alpine"),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to start RabbitMQ container: %w", err)
	}

	host, err := rabbitmqContainer.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get RabbitMQ host: %w", err)
	}
	port, err := rabbitmqContainer.MappedPort(ctx, "5672/tcp")
	if err != nil {
		return nil, "", fmt.Errorf("failed to get RabbitMQ AMQP port: %w", err)
	}
	amqpURL := fmt.Sprintf("amqp://guest:guest@%s:%s/", host, port.Port())

	return rabbitmqContainer, amqpURL, nil
}

// TestRabbitMQHealthCheck_Pass verifies that the health check passes when RabbitMQ is healthy.
func TestRabbitMQHealthCheck_Pass(t *testing.T) {
	ctx := context.Background()
	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(t, err)
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-pass",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check passing test",
	}

	checker := NewRabbitMQChecker(descriptor, 1*time.Second, amqpURL)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker: %v", err)
		}
	}()

	// Give the checker some time to perform initial checks
	time.Sleep(2 * time.Second)

	status := checker.Status()
	assert.Equal(t, core.StatusPass, status.Status, "Expected status to be PASS")
	assert.Contains(t, status.Output, "RabbitMQ is healthy", "Expected output to indicate health")

	health := checker.Health()
	assert.Equal(t, core.StatusPass, health.Status, "Expected health status to be PASS")
	assert.Contains(t, health.Output, "RabbitMQ is healthy", "Expected health output to indicate health")
}

// TestRabbitMQHealthCheck_Fail verifies that the health check fails when RabbitMQ is unavailable.
func TestRabbitMQHealthCheck_Fail(t *testing.T) {
	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-fail",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check failing test",
	}

	// Use a connection string that will fail
	badAmqpURL := "amqp://guest:guest@localhost:5679/bad_host"

	checker := NewRabbitMQChecker(descriptor, 1*time.Second, badAmqpURL)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker: %v", err)
		}
	}()

	// Give the checker some time to perform initial checks
	time.Sleep(2 * time.Second)

	status := checker.Status()
	assert.Equal(t, core.StatusFail, status.Status, "Expected status to be FAIL")
	assert.Contains(t, status.Output, "Failed to open RabbitMQ connection", "Expected output to indicate connection failure")

	health := checker.Health()
	assert.Equal(t, core.StatusFail, health.Status, "Expected health status to be FAIL")
	assert.Contains(t, health.Output, "Failed to open RabbitMQ connection", "Expected health output to indicate connection failure")
}

// TestRabbitMQHealthCheck_Close verifies that closing the checker works correctly.
func TestRabbitMQHealthCheck_Close(t *testing.T) {
	ctx := context.Background()
	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(t, err)
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-close",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check close test",
	}

	checker := NewRabbitMQChecker(descriptor, 1*time.Second, amqpURL)

	// Give the checker some time to start
	time.Sleep(500 * time.Millisecond)

	err = checker.Close()
	assert.NoError(t, err, "Expected no error when closing the checker")

	// After closing, the status should eventually reflect a stopped state or retain last known good status
	// The exact behavior depends on how the checker handles being closed.
	// For now, let's just ensure it doesn't panic and the Close method returns no error.
}

// TestRabbitMQHealthCheck_ChangeStatus verifies ChangeStatus method.
func TestRabbitMQHealthCheck_ChangeStatus(t *testing.T) {
	ctx := context.Background()
	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(t, err)
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-change-status",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check change status test",
	}

	checker := NewRabbitMQChecker(descriptor, 1*time.Minute, amqpURL) // Long interval to prevent auto-updates
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker: %v", err)
		}
	}()

	initialStatus := checker.Status()
	assert.Equal(t, core.StatusWarn, initialStatus.Status, "Expected initial status to be WARN")

	newStatus := core.ComponentStatus{
		Status: core.StatusFail,
		Output: "Manually set to critical",
	}
	checker.ChangeStatus(newStatus)

	changedStatus := checker.Status()
	assert.Equal(t, core.StatusFail, changedStatus.Status, "Expected status to be CRIT after change")
	assert.Equal(t, "Manually set to critical", changedStatus.Output, "Expected output to match manual set")
}

// TestRabbitMQHealthCheck_DisableEnable verifies Disable and Enable methods.
func TestRabbitMQHealthCheck_DisableEnable(t *testing.T) {
	ctx := context.Background()
	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(t, err)
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-disable-enable",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check disable/enable test",
	}

	checker := NewRabbitMQChecker(descriptor, 1*time.Second, amqpURL)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker: %v", err)
		}
	}()

	statusChan := checker.StatusChange()

	// The initial status is WARN, but it immediately starts a health check
	// which will likely change the status to PASS very quickly.
	// We'll consume statuses from the channel until we see PASS.
	// We expect at least one status update.
	var initialStatus core.ComponentStatus
	select {
	case initialStatus = <-statusChan:
		// We might get WARN first, or PASS directly if the check is fast
		assert.True(t, initialStatus.Status == core.StatusWarn || initialStatus.Status == core.StatusPass, "Expected initial status from channel to be WARN or PASS")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial status update")
	}

	// If the first status was WARN, wait for PASS. If it was PASS, we are good.
	if initialStatus.Status == core.StatusWarn {
		select {
		case status := <-statusChan:
			assert.Equal(t, core.StatusPass, status.Status, "Expected PASS status from channel after initial WARN")
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for PASS status after initial WARN")
		}
	} else {
		// If initial status was PASS, we are good to go.
		// No further action needed here.
	}

	checker.Disable()
	// Wait for the WARN status after disabling
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusWarn, status.Status, "Expected WARN status from channel after Disable")
		assert.Contains(t, status.Output, "RabbitMQ checker disabled", "Expected output to indicate disabled state")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for WARN status from channel after Disable")
	}

	checker.Enable()
	// Wait for the WARN status after enabling (re-initializing)
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusWarn, status.Status, "Expected WARN status from channel after Enable")
		assert.Contains(t, status.Output, "RabbitMQ checker enabled, re-initializing...", "Expected output to indicate enabled state")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for WARN status from channel after Enable")
	}

	// Wait for the PASS status after a successful check post-enable
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected PASS status from channel after re-enable check")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for PASS status from channel after re-enable check")
	}
}

// TestRabbitMQHealthCheck_StatusChangeChannel verifies that the status change channel works.
func TestRabbitMQHealthCheck_StatusChangeChannel(t *testing.T) {
	ctx := context.Background()
	rabbitmqContainer, amqpURL, err := setupRabbitMQ(ctx)
	require.NoError(t, err)
	defer func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate RabbitMQ container: %s", err)
		}
	}()

	descriptor := core.Descriptor{
		ComponentID:   "rabbitmq-test-status-channel",
		ComponentType: "rabbitmq",
		Description:   "RabbitMQ health check status change channel test",
	}

	checker := NewRabbitMQChecker(descriptor, 1*time.Second, amqpURL)
	defer func() {
		if err := checker.Close(); err != nil {
			log.Printf("Error closing checker: %v", err)
		}
	}()

	statusChan := checker.StatusChange()

	// The initial status is WARN, but it immediately starts a health check
	// which will likely change the status to PASS very quickly.
	// We'll consume statuses from the channel until we see PASS.
	// We expect at least one status update.
	var initialStatus core.ComponentStatus
	select {
	case initialStatus = <-statusChan:
		// We might get WARN first, or PASS directly if the check is fast
		assert.True(t, initialStatus.Status == core.StatusWarn || initialStatus.Status == core.StatusPass, "Expected initial status from channel to be WARN or PASS")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial status update")
	}

	// If the first status was WARN, wait for PASS. If it was PASS, we are good.
	if initialStatus.Status == core.StatusWarn {
		select {
		case status := <-statusChan:
			assert.Equal(t, core.StatusPass, status.Status, "Expected PASS status from channel after initial WARN")
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for PASS status after initial WARN")
		}
	} else {
		// If initial status was PASS, we are good to go.
		// No further action needed here.
	}


	// Test ChangeStatus causing a channel update
	newStatus := core.ComponentStatus{Status: core.StatusFail}
	checker.ChangeStatus(newStatus)
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusFail, status.Status, "Expected FAIL status from channel after ChangeStatus")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for FAIL status from channel")
	}
}
