package mongodb

import (
	"context"
	"fmt"
	"healthcheck/core"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// TestMongoChecker_Integration_HappyPath tests a successful connection and health check.
func TestMongoChecker_Integration_HappyPath(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-happy-path", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMongoChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusPass, status.Status)
	require.Contains(t, status.Output, "MongoDB is healthy")
}

// TestMongoChecker_Integration_Fail_NoConnection tests when the database is unreachable.
func TestMongoChecker_Integration_Fail_NoConnection(t *testing.T) {
	invalidConnStr := "mongodb://localhost:12345"

	desc := core.Descriptor{ComponentID: "mongo-fail-noconn", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	clientOptions := options.Client().ApplyURI(invalidConnStr).SetServerSelectionTimeout(2 * time.Second)
	checker := NewMongoCheckerWithOptions(desc, checkInterval, 1*time.Second, clientOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Failed to create MongoDB client")
}

// TestMongoChecker_Integration_Fail_DBDown tests the checker reacting to a database going down.
func TestMongoChecker_Integration_Fail_DBDown(t *testing.T) {
	ctx := t.Context()
	mongoContainer, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-fail-dbdown", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	clientOptions := options.Client().ApplyURI(connStr).SetServerSelectionTimeout(2 * time.Second)
	checker := NewMongoCheckerWithOptions(desc, checkInterval, 1*time.Second, clientOptions)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	if err := mongoContainer.Stop(ctx, nil); err != nil {
		t.Fatalf("Failed to stop MongoDB container: %v", err)
	}

	waitForStatus(t, checker, core.StatusFail, 5*time.Second)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "MongoDB health check failed")
}

// TestMongoChecker_Integration_DisableEnable tests the disable/enable functionality.
func TestMongoChecker_Integration_DisableEnable(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-disable-enable", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMongoChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	checker.Disable()
	status := checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "MongoDB checker disabled", status.Output)

	time.Sleep(checkInterval * 2)

	statusAfterSleep := checker.Status()
	require.Equal(t, core.StatusWarn, statusAfterSleep.Status)
	require.Equal(t, "MongoDB checker disabled", statusAfterSleep.Output)

	checker.Enable()
	status = checker.Status()
	require.Equal(t, core.StatusWarn, status.Status)
	require.Equal(t, "MongoDB checker enabled, re-initializing...", status.Output)

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)
}

// TestMongoChecker_Integration_Close tests graceful shutdown.
func TestMongoChecker_Integration_Close(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-close", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMongoChecker(desc, checkInterval, 1*time.Second, connStr)

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	err := checker.Close()
	require.NoError(t, err)

	select {
	case <-checker.(*mongoChecker).ctx.Done():
		// expected
	case <-time.After(1 * time.Second):
		t.Error("context was not cancelled after Close()")
	}
}

// TestMongoChecker_Integration_ChangeStatus tests the ChangeStatus method.
func TestMongoChecker_Integration_ChangeStatus(t *testing.T) {
	ctx := t.Context()
	_, connStr, cleanup := setupMongoContainer(t, ctx)
	defer cleanup()

	desc := core.Descriptor{ComponentID: "mongo-change-status", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	checker := NewMongoChecker(desc, checkInterval, 1*time.Second, connStr)
	defer func() {
		if err := checker.Close(); err != nil {
			t.Errorf("Checker Close() returned an unexpected error: %v", err)
		}
	}()

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)

	newStatus := core.ComponentStatus{Status: core.StatusFail, Output: "manual override"}
	checker.ChangeStatus(newStatus)

	status := checker.Status()
	require.Equal(t, newStatus.Status, status.Status)
	require.Equal(t, newStatus.Output, status.Output)

	waitForStatus(t, checker, core.StatusPass, 5*time.Second)
}

// TestMongoChecker_OpenError tests the scenario where mongo.Connect returns an error.
func TestMongoChecker_OpenError(t *testing.T) {
	mockOpenDB := func(ctx context.Context, opts *options.ClientOptions) (mongoConnection, error) {
		return nil, fmt.Errorf("mocked mongo.Connect error for context: %v", ctx)
	}

	desc := core.Descriptor{ComponentID: "mongo-open-error", ComponentType: "mongodb"}
	checkInterval := 50 * time.Millisecond

	clientOptions := options.Client().ApplyURI("mongodb://localhost:12345")
	checker := NewMongoCheckerWithOpenDBFunc(desc, checkInterval, 1*time.Second, clientOptions, mockOpenDB)

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "Failed to create MongoDB client")
	require.Contains(t, status.Output, "mocked mongo.Connect error for context", "Expected output to contain the mocked error with context")
}

// TestMongoChecker_PerformHealthCheck_ClientNil tests performHealthCheck when the client is nil.
func TestMongoChecker_PerformHealthCheck_ClientNil(t *testing.T) {
	checker := &mongoChecker{
		currentStatus: core.ComponentStatus{Status: core.StatusWarn},
	}
	checker.performHealthCheck()
	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Equal(t, "MongoDB client is nil", status.Output)
}

// TestMongoChecker_PerformHealthCheck_PingError tests performHealthCheck when Ping returns an error.
func TestMongoChecker_PerformHealthCheck_PingError(t *testing.T) {
	mockClient := &mockMongoConnection{
		pingFunc: func(ctx context.Context, rp *readpref.ReadPref) error {
			return fmt.Errorf("mocked ping error")
		},
	}

	checker := newMongoCheckerInternal(core.Descriptor{}, 1*time.Second, 1*time.Second, mockClient, "")
	defer func() {
		err := checker.Close()
		require.NoError(t, err, "Checker Close() returned an unexpected error: %v", err)
	}()

	checker.(*mongoChecker).performHealthCheck()

	status := checker.Status()
	require.Equal(t, core.StatusFail, status.Status)
	require.Contains(t, status.Output, "mocked ping error")
}

// TestMongoChecker_Close_NilClient tests the Close method when the client is nil.
func TestMongoChecker_Close_NilClient(t *testing.T) {
	checker := &mongoChecker{
		cancelFunc: func() {},
		quit:       make(chan struct{}),
	}
	err := checker.Close()
	require.NoError(t, err)
}

// TestNewMongoConnection_ContextCanceled tests newMongoConnection with a canceled context.
func TestNewMongoConnection_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel the context immediately

	opts := options.Client().ApplyURI("mongodb://localhost:12345")
	client, err := newMongoConnection(ctx, opts)
	require.Error(t, err)
	require.Nil(t, client)
}

// TestMongoChecker_QuitChannelStopsLoop tests that closing the quit channel stops the health check loop.
func TestMongoChecker_QuitChannelStopsLoop(t *testing.T) {
	mockClient := &mockMongoConnection{}
	checker := newMongoCheckerInternal(core.Descriptor{}, 1*time.Second, 1*time.Second, mockClient, "")

	// consume the initial status
	<-checker.StatusChange()

	// Manually close the quit channel to stop the loop
	close(checker.(*mongoChecker).quit)

	// Give some time for the goroutine to pick up the signal and stop.
	// We verify by ensuring no further status changes are reported.
	select {
	case <-checker.StatusChange():
		t.Fatal("Received unexpected status change after quit")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
