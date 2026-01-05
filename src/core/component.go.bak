package core

import (
	"context"
	"io"
	"sync"
)

type component struct {
	lastStatus     ComponentStatus      // Renamed type
	statusChange   chan ComponentStatus // Renamed type
	observerChange chan ComponentStatus // Renamed type
	descriptor     Descriptor           // Renamed type
	ctx            *context.Context
	cancelFunc     context.CancelFunc
	mutex          sync.RWMutex
}

type Component interface {
	io.Closer
	ChangeStatus(status ComponentStatus) // Renamed type
	Disable()
	Enable()
	Status() ComponentStatus              // Renamed type
	Descriptor() Descriptor               // Renamed type
	StatusChange() <-chan ComponentStatus // Renamed type
	Health() ComponentHealth              // Renamed type
}

func New(descriptor Descriptor, initialStatus StatusEnum) Component { // Renamed types
	ctx, cancelFunc := context.WithCancel(context.Background())
	c := &component{
		statusChange:   make(chan ComponentStatus),    // Renamed type
		observerChange: make(chan ComponentStatus, 1), // Renamed type
		descriptor:     descriptor,
		ctx:            &ctx,
		cancelFunc:     cancelFunc,
		lastStatus: ComponentStatus{ // Renamed type
			Status: initialStatus,
		},
	}

	go func() {
		defer close(c.observerChange)
		for {
			select {
			case <-ctx.Done():
				return
			case status := <-c.statusChange:
				c.mutex.Lock()
				c.lastStatus = status
				c.mutex.Unlock()

				// Broadcast the change to the observer channel.
				// This is a non-blocking send that overwrites any old, unread status
				// ensuring the observer always gets the latest value.
				select {
				case <-c.observerChange: // Drain old status if buffer is full
				default:
				}
				c.observerChange <- status // Send the new status
			}
		}
	}()
	return c
}

func (c *component) Close() error {
	c.cancelFunc()
	return nil
}

func (c *component) ChangeStatus(status ComponentStatus) { // Renamed type
	select {
	case c.statusChange <- status:
	case <-(*c.ctx).Done():
	}
}

func (c *component) Disable() {
	c.ChangeStatus(ComponentStatus{ // Renamed type
		Status: StatusFail, // Renamed type
	})
}

func (c *component) Enable() {
	c.ChangeStatus(ComponentStatus{ // Renamed type
		Status: StatusPass, // Renamed type
	})
}

func (c *component) Status() ComponentStatus { // Renamed type
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastStatus
}

func (c *component) Descriptor() Descriptor { // Renamed type
	return c.descriptor
}

func (c *component) StatusChange() <-chan ComponentStatus { // Renamed type
	return c.observerChange
}

func (c *component) Health() ComponentHealth { // Renamed type
	c.mutex.RLock()
	status := c.lastStatus
	c.mutex.RUnlock()

	descriptor := c.Descriptor()
	return ComponentHealth{
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
		ObservedValue:     status.ObservedValue,
		ObservedUnit:      status.ObservedUnit,
		AffectedEndpoints: status.AffectedEndpoints,
		Time:              status.Time,
	}
}
