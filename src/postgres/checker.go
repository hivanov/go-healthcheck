package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"healthcheck/core"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
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

type postgresChecker struct {
	checkInterval    time.Duration
	queryTimeout     time.Duration // New field for query timeout
	connectionString string
	db               dbConnection // Changed from *sql.DB
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
// This will be the default OpenDBFunc used by NewPostgresChecker.
func newSQLDBConnection(driverName, connectionString string) (dbConnection, error) {
	db, err := sql.Open(driverName, connectionString)
	if err != nil {
		return nil, err
	}
	return &realDBConnection{db: db}, nil
}

// NewPostgresChecker creates a new PostgreSQL health checker component.
// It continuously pings the database with "SELECT 1" and updates its status.
// This is the public constructor that handles opening the SQL DB connection.
func NewPostgresChecker(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, connectionString string) core.Component {
	return NewPostgresCheckerWithOpenDBFunc(descriptor, checkInterval, queryTimeout, connectionString, newSQLDBConnection)
}

// NewPostgresCheckerWithOpenDBFunc creates a new PostgreSQL health checker component,
// allowing a custom OpenDBFunc to be provided for opening the database connection.
// This is useful for testing scenarios where mocking the database connection is required.
func NewPostgresCheckerWithOpenDBFunc(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, connectionString string, openDB OpenDBFunc) core.Component {
	dbConn, err := openDB("postgres", connectionString) // Use the provided OpenDBFunc
	if err != nil {
		dummyChecker := &postgresChecker{
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

	return newPostgresCheckerInternal(descriptor, checkInterval, queryTimeout, dbConn)
}

// newPostgresCheckerInternal creates a new PostgreSQL health checker component with a provided dbConnection.
// It continuously pings the database with "SELECT 1" and updates its status.
// This is an internal constructor used for testing purposes and for injecting a dbConnection.
func newPostgresCheckerInternal(descriptor core.Descriptor, checkInterval, queryTimeout time.Duration, conn dbConnection) core.Component {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// Initial status is initializing
	initialStatus := core.ComponentStatus{
		Status: core.StatusWarn,
		Output: "PostgreSQL checker initializing...",
	}

	checker := &postgresChecker{
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
func (p *postgresChecker) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.cancelFunc() // Signal context cancellation to stop the loop
	close(p.quit)  // Also signal the older quit channel if anything is listening

	// Ensure DB connection is closed. The health check loop also closes it,
	// but this provides a fallback and immediate closure if the loop isn't running.
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// ChangeStatus updates the internal status of the component.
// This might be used by an external orchestrator to temporarily override status.
// Periodic checks will eventually re-evaluate and potentially overwrite this.
func (p *postgresChecker) ChangeStatus(newStatus core.ComponentStatus) {
	p.updateStatus(newStatus)
}

// Disable sets the component to a disabled state, halting active health checks.
func (p *postgresChecker) Disable() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if !p.disabled {
		p.disabled = true
		p.currentStatus = core.ComponentStatus{
			Status:            core.StatusWarn,
			Time:              time.Now().UTC(),
			Output:            "PostgreSQL checker disabled",
			AffectedEndpoints: nil,
		}
		select {
		case p.statusChangeChan <- p.currentStatus:
		default:
		}
	}
}

// Enable reactivates the component, resuming active health checks.
func (p *postgresChecker) Enable() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.disabled {
		p.disabled = false
		p.currentStatus = core.ComponentStatus{
			Status: core.StatusWarn,
			Output: "PostgreSQL checker enabled, re-initializing...",
		}
		select {
		case p.statusChangeChan <- p.currentStatus:
		default:
		}
		go p.performHealthCheck() // Trigger an immediate check upon re-enabling
	}
}

// Status returns the current health status of the PostgreSQL component.
func (p *postgresChecker) Status() core.ComponentStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.currentStatus
}

// Descriptor returns the descriptor for the PostgreSQL component.
func (p *postgresChecker) Descriptor() core.Descriptor {
	return p.descriptor
}

// StatusChange returns a channel that sends updates whenever the component's status changes.
func (p *postgresChecker) StatusChange() <-chan core.ComponentStatus {
	return p.statusChangeChan
}

// Health returns a detailed health report for the PostgreSQL component.
func (p *postgresChecker) Health() core.ComponentHealth {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	var output string

	if p.currentStatus.Output != "" {
		output = p.currentStatus.Output
	}

	return core.ComponentHealth{
		ComponentID:   p.descriptor.ComponentID,
		ComponentType: p.descriptor.ComponentType,
		Status:        p.currentStatus.Status,
		Output:        output,
	}
}

// startHealthCheckLoop runs in a goroutine, periodically checking the PostgreSQL database health.
func (p *postgresChecker) startHealthCheckLoop() {
	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	// Perform an initial check immediately
	p.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			p.performHealthCheck()
		case <-p.quit: // Explicit quit signal
			if err := p.db.Close(); err != nil {
				log.Printf("Error closing PostgreSQL connection on quit: %v", err)
			}
			return
		case <-p.ctx.Done(): // Context cancellation signal
			if err := p.db.Close(); err != nil {
				log.Printf("Error closing PostgreSQL connection on context done: %v", err)
			}
			return
		}
	}
}

// performHealthCheck executes a "SELECT 1" query against the PostgreSQL database
// and updates the component's status based on the result.
func (p *postgresChecker) performHealthCheck() {
	p.mutex.RLock()
	isDisabled := p.disabled
	p.mutex.RUnlock()

	if isDisabled {
		// If disabled, we should not perform active checks.
		// The status was already set to "disabled" by the Disable method.
		return
	}

	if p.db == nil {
		p.updateStatus(core.ComponentStatus{
			Status: core.StatusFail,
			Output: "Database connection object is nil",
		})
		return
	}

	// Create a new context with a timeout for the health check query.
	// This ensures that if the database is unresponsive, the check doesn't hang forever.
	checkCtx, checkCancel := context.WithTimeout(p.ctx, p.queryTimeout)
	defer checkCancel()

	startTime := time.Now()
	row := p.db.QueryRowContext(checkCtx, "SELECT 1")
	var result int
	err := row.Scan(&result)
	elapsedTime := time.Since(startTime)

	if err != nil {
		p.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("PostgreSQL health check query failed: %v", err),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else if result == 1 {
		p.updateStatus(core.ComponentStatus{
			Status:        core.StatusPass,
			Output:        "PostgreSQL is healthy",
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	} else {
		p.updateStatus(core.ComponentStatus{
			Status:        core.StatusFail,
			Output:        fmt.Sprintf("PostgreSQL health check query returned unexpected result: %d (expected 1)", result),
			Time:          startTime,
			ObservedValue: elapsedTime.Seconds(),
			ObservedUnit:  "s",
		})
	}
}

// updateStatus safely updates the current status and notifies listeners if the status has changed.
func (p *postgresChecker) updateStatus(newStatus core.ComponentStatus) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.currentStatus.Status != newStatus.Status || p.currentStatus.Output != newStatus.Output {
		p.currentStatus = newStatus
		select {
		case p.statusChangeChan <- newStatus:
		default:
			// Non-blocking send: if the channel buffer is full,
			// it means no one is listening fast enough or the buffer is too small.
			// This prevents blocking the health check loop itself.
		}
	}
}
