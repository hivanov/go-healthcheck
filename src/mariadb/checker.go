package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // MariaDB driver
)

// dbConnection interface for mocking *sql.DB in tests.
type dbConnection interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) rowScanner
	Close() error
}

// rowScanner interface for mocking *sql.Row in tests.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// realDBConnection implements dbConnection for *sql.DB.
type realDBConnection struct {
	db *sql.DB
}

// realRowScanner implements rowScanner for *sql.Row.
type realRowScanner struct {
	row *sql.Row
}

func (r *realRowScanner) Scan(dest ...interface{}) error {
	return r.row.Scan(dest...)
}

func (r *realDBConnection) QueryRowContext(ctx context.Context, query string, args ...interface{}) rowScanner {
	return &realRowScanner{row: r.db.QueryRowContext(ctx, query, args...)}
}

func (r *realDBConnection) Close() error {
	return r.db.Close()
}

type mariadbChecker struct {
	checkInterval    time.Duration
	queryTimeout     time.Duration // New field for query timeout
	connectionString string
	db               dbConnection
	descriptor       core.Descriptor
	currentStatus    core.ComponentStatus
	statusChangeChan chan core.ComponentStatus
	quit             chan struct{}
	mutex            sync.RWMutex
	cancelFunc       context.CancelFunc
	ctx              context.Context
	disabled         bool
}

// OpenDBFunc defines the signature for a function that can open a database connection.
// This is used to allow mocking of sql.Open in tests.
type OpenDBFunc func(driverName, connectionString string) (dbConnection, error)

// newSQLDBConnection is a helper function to create a real dbConnection from *sql.DB
// This will be the default OpenDBFunc used by NewMariaDBChecker.
func newSQLDBConnection(driverName, connectionString string) (dbConnection, error) {
	db, err := sql.Open(driverName, connectionString)
	if err != nil {
		return nil, err
	}
	return &realDBConnection{db: db}, nil
}

// NewMariaDBChecker creates a new MariaDB health checker component.
// It continuously pings the database with "SELECT 1" and updates its status.
// This is the public constructor that handles opening the SQL DB connection.
func NewMariaDBChecker(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, connectionString string) core.Component {
	return NewMariaDBCheckerWithOpenDBFunc(descriptor, checkInterval, queryTimeout, connectionString, newSQLDBConnection)
}

// NewMariaDBCheckerWithOpenDBFunc creates a new MariaDB health checker component,
// allowing a custom OpenDBFunc to be provided for opening the database connection.
// This is useful for testing scenarios where mocking the database connection is required.
func NewMariaDBCheckerWithOpenDBFunc(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, connectionString string, openDB OpenDBFunc) core.Component {
	dbConn, err := openDB("mysql", connectionString) // Use the provided OpenDBFunc and "mysql" driver
	if err != nil {
		dummyChecker := &mariadbChecker{
			descriptor: descriptor,
			currentStatus: core.ComponentStatus{
				Status: core.StatusFail,
				Output: fmt.Sprintf("Failed to open DB connection: %v", err),
			},
			statusChangeChan: make(chan core.ComponentStatus, 1),
			quit:             make(chan struct{}),
			ctx:              context.Background(),
			cancelFunc:       func() {},
			disabled:         false,
		}
		return dummyChecker
	}

	return newMariaDBCheckerInternal(descriptor, checkInterval, queryTimeout, dbConn)
}

// newMariaDBCheckerInternal creates a new MariaDB health checker component with a provided dbConnection.
// It continuously pings the database with "SELECT 1" and updates its status.
// This is an internal constructor used for testing purposes and for injecting a dbConnection.
func newMariaDBCheckerInternal(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, conn dbConnection) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// Initial status is initializing
	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "MariaDB checker initializing...",
	}

	checker := &mariadbChecker{
		checkInterval:    checkInterval,
		queryTimeout:     queryTimeout, // Initialize queryTimeout
		connectionString: "",           // Not applicable when dbConnection is provided directly
		descriptor:       descriptor,
		currentStatus:    initialStatus,
		statusChangeChan: make(chan core.ComponentStatus, 1), // Buffered to prevent blocking
		quit:             make(chan struct{}),
		ctx:              ctx,
		cancelFunc:       cancelFunc,
		disabled:         false,
		db:               conn, // Use the provided dbConnection
	}

	// If a connection is provided, start the health check loop.
	// In this internal constructor, we assume the dbConnection is already valid or will be handled by the caller.
	// Any error handling for dbConnection opening is expected to be done before calling this function.
	go checker.startHealthCheckLoop()

	return checker
}

// Close stops the checker's background operations and closes the database connection.
func (m *mariadbChecker) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.cancelFunc() // Signal context cancellation to stop the loop
	close(m.quit)  // Also signal the older quit channel if anything is listening

	// Ensure DB connection is closed. The health check loop also closes it,
	// but this provides a fallback and immediate closure if the loop isn't running.
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// ChangeStatus updates the internal status of the component.
// This might be used by an external orchestrator to temporarily override status.
// Periodic checks will eventually re-evaluate and potentially overwrite this.
func (m *mariadbChecker) ChangeStatus(newStatus core.ComponentStatus) {
	m.updateStatus(newStatus)
}

// Disable sets the component to a disabled state, halting active health checks.
func (m *mariadbChecker) Disable() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if !m.disabled {
		m.disabled = true
		m.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "MariaDB checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case m.statusChangeChan <- m.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component, resuming active health checks.
func (m *mariadbChecker) Enable() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.disabled {
		m.disabled = false
		m.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "MariaDB checker enabled, re-initializing...",
		}
		select {
		case m.statusChangeChan <- m.currentStatus:
		default:
		}
		go m.performHealthCheck() // Trigger an immediate check upon re-enabling
	}
}

// Status returns the current health status of the MariaDB component.
func (m *mariadbChecker) Status() core.ComponentStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.currentStatus
}

// Descriptor returns the descriptor for the MariaDB component.
func (m *mariadbChecker) Descriptor() core.Descriptor {
	return m.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (m *mariadbChecker) StatusChange() <-chan core.ComponentStatus {
	return m.statusChangeChan
}

// Health returns a detailed health report for the MariaDB component.
func (m *mariadbChecker) Health() core.ComponentHealth {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var output string

	if m.currentStatus.Output != "" {
		output = m.currentStatus.Output
	}

	return core.ComponentHealth{
		ComponentID:   m.descriptor.ComponentID,
		ComponentType: m.descriptor.ComponentType,
		Status:        m.currentStatus.Status,
		Output:        output,
	}
}

// startHealthCheckLoop runs in a goroutine, periodically checking the MariaDB database health.
func (m *mariadbChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// Perform an initial check immediately
	m.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			m.performHealthCheck()
		case <-m.quit: // Explicit quit signal
			if m.db != nil {
				if err := m.db.Close(); err != nil {
					log.Printf("Error closing MariaDB connection on quit: %v", err)
				}
			}
			return
		case <-m.ctx.Done(): // Context cancellation signal
			if m.db != nil {
				if err := m.db.Close(); err != nil {
					log.Printf("Error closing MariaDB connection on context done: %v", err)
				}
			}
			return
		}
	}
}

// performHealthCheck executes a "SELECT 1" query against the MariaDB database
// and updates the component's status based on the result.
func (m *mariadbChecker) performHealthCheck() {
	m.mutex.RLock()
	isDisabled := m.disabled
	m.mutex.RUnlock()

	if isDisabled {
		// If disabled, we should not perform active checks.
		// The status was already set to "disabled" by the Disable method.
		return
	}

	if m.db == nil {
		m.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "Database connection object is nil",
		})
		return
	}

	startTime := time.Now()
	queryCtx, cancelQuery := context.WithTimeout(m.ctx, m.queryTimeout)
	defer cancelQuery()
	row := m.db.QueryRowContext(queryCtx, "SELECT 1")
	var result int
	err := row.Scan(&result)
	elapsedTime := time.Since(startTime)

	if err != nil {
		m.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("MariaDB health check query failed: %v", err),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else if result == 1 {
		m.updateStatus(core.ComponentStatus{
			Status:        core.StatusPass,
			Output:        "MariaDB is healthy",
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else {
		m.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("MariaDB health check query returned unexpected result: %d (expected 1)", result),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	}
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (m *mariadbChecker) updateStatus(newStatus core.ComponentStatus) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.currentStatus.Status != newStatus.Status || m.currentStatus.Output != newStatus.Output {
		m.currentStatus = newStatus
		select {
		case m.statusChangeChan <- newStatus:
		default:
			// Non-blocking send: if the channel buffer is full,
			// it means no one is listening fast enough or the buffer is too small.
			// This prevents blocking the health check loop itself.
		}
	}
}
