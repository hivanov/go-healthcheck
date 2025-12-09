package rabbitmq

import (
	"context"
	"fmt"
	"healthcheck/core"
	"log"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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

	amqpURL, err := rabbitmqContainer.GetAMQPURL(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get AMQP URL: %w", err)
	}

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
		Status: core.StatusCrit,
		Output: "Manually set to critical",
	}
	checker.ChangeStatus(newStatus)

	changedStatus := checker.Status()
	assert.Equal(t, core.StatusCrit, changedStatus.Status, "Expected status to be CRIT after change")
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

	time.Sleep(2 * time.Second) // Allow initial health check to pass

	statusBeforeDisable := checker.Status()
	assert.Equal(t, core.StatusPass, statusBeforeDisable.Status, "Expected status to be PASS before disable")

	checker.Disable()
	time.Sleep(1500 * time.Millisecond) // Give time for disable to propagate and for potential new checks to not run

	statusAfterDisable := checker.Status()
	assert.Equal(t, core.StatusWarn, statusAfterDisable.Status, "Expected status to be WARN after disable")
	assert.Contains(t, statusAfterDisable.Output, "RabbitMQ checker disabled", "Expected output to indicate disabled state")

	checker.Enable()
	time.Sleep(2 * time.Second) // Give time for enable to propagate and for a new check to run

	statusAfterEnable := checker.Status()
	assert.Equal(t, core.StatusPass, statusAfterEnable.Status, "Expected status to be PASS after enable")
	assert.Contains(t, statusAfterEnable.Output, "RabbitMQ is healthy", "Expected output to indicate health after enable")
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

	// Wait for the initial WARN status
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusWarn, status.Status, "Expected initial status from channel to be WARN")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial WARN status")
	}

	// Wait for the PASS status after the first successful check
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusPass, status.Status, "Expected PASS status from channel")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for PASS status")
	}

	// Test ChangeStatus causing a channel update
	newStatus := core.ComponentStatus{Status: core.StatusCrit}
	checker.ChangeStatus(newStatus)
	select {
	case status := <-statusChan:
		assert.Equal(t, core.StatusCrit, status.Status, "Expected CRIT status from channel after ChangeStatus")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for CRIT status from channel")
	}
}
