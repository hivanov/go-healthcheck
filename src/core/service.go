package core

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Service defines the interface for a health-checkable service, which is composed of multiple components.
// A Service needs at least one Component to control its health.
type Service interface {
	io.Closer
	Health() *Health
}

type service struct {
	components []Component
	descriptor Descriptor
	lastStatus ComponentStatus // Should be ComponentStatus, not StatusEnum for s.lastStatus
	ctx        context.Context
	cancelFunc context.CancelFunc
	mutex      sync.RWMutex
}

// NewService creates a new service composed of the given components and with the given descriptor.
func NewService(descriptor Descriptor, components ...Component) Service {
	ctx, cancelFunc := context.WithCancel(context.Background())
	s := &service{
		components: components,
		descriptor: descriptor,
		ctx:        ctx,
		cancelFunc: cancelFunc,
		// Initialize lastStatus to a default pass state
		lastStatus: ComponentStatus{
			Status: StatusPass,
		},
	}

	recalcChan := make(chan struct{}, 1)

	for _, comp := range components {
		go func(c Component) {
			for {
				select {
				case <-s.ctx.Done():
					return
				case <-c.StatusChange():
					select {
					case recalcChan <- struct{}{}:
					default:
					}
				}
			}
		}(comp)
	}

	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-recalcChan:
				s.recalculateStatus()
			}
		}
	}()

	s.recalculateStatus() // Initial status calculation

	return s
}

func (s *service) Close() error {
	s.cancelFunc()
	var err error = nil
	for _, c := range s.components {
		err = errors.Join(err, c.Close())
	}
	return err
}

func (s *service) recalculateStatus() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	overallStatus := StatusPass
	for _, c := range s.components {
		componentStatus := c.Status().Status // c.Status() returns ComponentStatus, then .Status is StatusEnum
		if componentStatus == StatusFail {
			overallStatus = StatusFail
			break
		}
		if componentStatus == StatusWarn && overallStatus == StatusPass {
			overallStatus = StatusWarn
		}
	}
	s.lastStatus.Status = overallStatus // Update only the StatusEnum part of ComponentStatus
}

func (s *service) Health() *Health {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	checks := make(map[string][]ComponentHealth)
	for _, c := range s.components {
		componentType := c.Descriptor().ComponentType
		if componentType == "" {
			componentType = "default"
		}
		checks[componentType] = append(checks[componentType], c.Health())
	}

	// This is important: s.lastStatus is ComponentStatus, but Health.Status expects StatusEnum.
	// So we need s.lastStatus.Status (which is the StatusEnum part).
	return &Health{
		Status:      s.lastStatus.Status, // Access the StatusEnum from ComponentStatus
		Version:     s.descriptor.Version,
		ReleaseID:   s.descriptor.ReleaseID,
		Notes:       s.descriptor.Notes,
		Checks:      checks,
		Links:       s.descriptor.Links,
		ServiceID:   s.descriptor.ServiceID,
		Description: s.descriptor.Description,
	}
}
