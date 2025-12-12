package builtin

import (
	"context"
	"fmt"
	"healthcheck/core"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPCheckerOptions defines options for the HTTPChecker.
type HTTPCheckerOptions struct {
	URL      string
	Timeout  time.Duration
	Interval time.Duration
}

type httpChecker struct {
	lastStatus     core.ComponentStatus
	observerChange chan core.ComponentStatus
	descriptor     core.Descriptor
	ctx            context.Context
	cancelFunc     context.CancelFunc
	mutex          sync.RWMutex
	options        HTTPCheckerOptions
	disabled       bool
}

// Ensure httpChecker implements the Component interface
var _ core.Component = (*httpChecker)(nil)

// NewHTTPCheckerComponent creates a new component that performs HTTP GET health checks.
func NewHTTPCheckerComponent(options HTTPCheckerOptions) (core.Component, error) {
	if options.URL == "" {
		return nil, fmt.Errorf("URL cannot be empty")
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second // Default timeout
	}
	if options.Interval <= 0 {
		options.Interval = 5 * time.Second // Default interval
	}

	ctx, cancelFunc := context.WithCancel(context.Background())

	hc := &httpChecker{
		observerChange: make(chan core.ComponentStatus, 1),
		descriptor: core.Descriptor{
			ComponentID:   fmt.Sprintf("http-get-%s", options.URL),
			ComponentType: "http",
			Description:   fmt.Sprintf("Performs HTTP GET health check on %s", options.URL),
			Links:         map[string]string{"url": options.URL},
		},
		ctx:        ctx,
		cancelFunc: cancelFunc,
		options:    options,
		disabled:   false,
	}

	go hc.start()

	return hc, nil
}

func (hc *httpChecker) start() {
	defer close(hc.observerChange)
	ticker := time.NewTicker(hc.options.Interval)
	defer ticker.Stop()

	// Perform the initial check immediately
	hc.performCheck()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.performCheck()
		}
	}
}

func (hc *httpChecker) performCheck() {
	select {
	case <-hc.ctx.Done():
		return
	default:
	}

	hc.mutex.RLock()
	if hc.disabled {
		hc.mutex.RUnlock()
		return
	}
	hc.mutex.RUnlock()

	client := &http.Client{
		Timeout: hc.options.Timeout,
	}

	req, err := http.NewRequestWithContext(hc.ctx, "GET", hc.options.URL, nil)
	if err != nil {
		hc.updateStatus(core.StatusFail, fmt.Sprintf("Failed to create HTTP request: %v", err))
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		hc.updateStatus(core.StatusFail, fmt.Sprintf("HTTP GET request failed: %v", err))
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			hc.updateStatus(core.StatusFail, fmt.Sprintf("Failed to close HTTP response body: %v", err))
		}
	}(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		hc.updateStatus(core.StatusPass, fmt.Sprintf("HTTP GET to %s returned status %d", hc.options.URL, resp.StatusCode))
	} else {
		hc.updateStatus(core.StatusFail, fmt.Sprintf("HTTP GET to %s returned status %d", hc.options.URL, resp.StatusCode))
	}
}

func (hc *httpChecker) updateStatus(status core.StatusEnum, output string) {
	select {
	case <-hc.ctx.Done():
		return // Component is closed.
	default:
	}

	newStatus := core.ComponentStatus{
		Status: status,
		Time:   time.Now(),
		Output: output,
	}

	hc.mutex.Lock()
	hc.lastStatus = newStatus
	hc.mutex.Unlock()

	// Broadcast the change, but don't block or panic if the component is closed.
	select {
	case <-hc.observerChange: // Drain old status if buffer is full
	default:
	}
	select {
	case hc.observerChange <- newStatus:
	case <-hc.ctx.Done(): // Abort if closed during drain.
	}
}

func (hc *httpChecker) Close() error {
	hc.cancelFunc()
	return nil
}

// ChangeStatus is a no-op for HTTPChecker component as its status is internally managed.
func (hc *httpChecker) ChangeStatus(status core.ComponentStatus) {}

// Disable stops the health check ticker.
func (hc *httpChecker) Disable() {
	select {
	case <-hc.ctx.Done():
		return
	default:
	}
	hc.mutex.Lock()
	if hc.disabled {
		hc.mutex.Unlock()
		return
	}
	hc.disabled = true
	hc.mutex.Unlock()
	hc.updateStatus(core.StatusWarn, "health check disabled")
}

// Enable resumes the health check ticker and performs an immediate check.
func (hc *httpChecker) Enable() {
	select {
	case <-hc.ctx.Done():
		return
	default:
	}
	hc.mutex.Lock()
	if !hc.disabled {
		hc.mutex.Unlock()
		return
	}
	hc.disabled = false
	hc.mutex.Unlock()

	go hc.performCheck()
}

func (hc *httpChecker) Status() core.ComponentStatus {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()
	return hc.lastStatus
}

func (hc *httpChecker) Descriptor() core.Descriptor {
	return hc.descriptor
}

func (hc *httpChecker) StatusChange() <-chan core.ComponentStatus {
	return hc.observerChange
}

func (hc *httpChecker) Health() core.ComponentHealth {
	hc.mutex.RLock()
	status := hc.lastStatus
	hc.mutex.RUnlock()

	descriptor := hc.Descriptor()
	return core.ComponentHealth{
		Status:            status.Status,
		Version:           descriptor.Version,
		ReleaseID:         descriptor.ReleaseID,
		Notes:             descriptor.Notes,
		Output:            status.Output,
		Links:             descriptor.Links,
		ServiceID:         descriptor.ServiceID,
		Description:       descriptor.Description,
		ComponentID:       descriptor.ComponentID,
		ComponentType:     descriptor.ComponentType,
		ObservedValue:     status.ObservedValue, // HTTP checker does not have a specific observed value
		ObservedUnit:      status.ObservedUnit,
		AffectedEndpoints: status.AffectedEndpoints,
		Time:              status.Time,
	}
}
