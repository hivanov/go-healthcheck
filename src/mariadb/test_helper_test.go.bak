package mariadb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mariadb"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupMariaDBContainer starts a MariaDB container and returns its connection string.
// It also returns a cleanup function to stop the container.
func setupMariaDBContainer(tb testing.TB, ctx context.Context) (testcontainers.Container, string, func()) {
	tb.Helper()

	startupCtx, startupCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer startupCancel()

	mariadbContainer, err := mariadb.Run(startupCtx,
		"mariadb:10.6",
		mariadb.WithDatabase("testdb"),
		mariadb.WithUsername("testuser"),
		mariadb.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("mariadbd: ready for connections.").WithStartupTimeout(3*time.Minute),
		),
	)
	require.NoError(tb, err, "Failed to start MariaDB container")
	require.NotNil(tb, mariadbContainer, "MariaDB container is nil")

	connStr, err := mariadbContainer.ConnectionString(ctx, "tls=false")
	if err != nil {
		_ = mariadbContainer.Terminate(ctx)
		require.FailNow(tb, "Failed to get connection string", err)
	}

	return mariadbContainer, connStr, func() {
		if err := mariadbContainer.Terminate(ctx); err != nil {
			tb.Logf("Failed to terminate MariaDB container: %v", err)
		}
	}
}
