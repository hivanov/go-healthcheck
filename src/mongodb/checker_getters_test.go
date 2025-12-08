package mongodb

import (
	"healthcheck/core"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMongoChecker_Getters tests the Descriptor and Health methods.
func TestMongoChecker_Getters(t *testing.T) {
	desc := core.Descriptor{ComponentID: "test-id", ComponentType: "test-type"}
	checker := &mongoChecker{
		descriptor:    desc,
		currentStatus: core.ComponentStatus{Status: core.StatusPass, Output: "Everything is fine"},
	}

	// Test Descriptor()
	actualDesc := checker.Descriptor()
	assert.Equal(t, desc, actualDesc, "Descriptor() should return the correct descriptor")

	// Test Health()
	health := checker.Health()
	assert.Equal(t, desc.ComponentID, health.ComponentID, "Health().ComponentID should match")
	assert.Equal(t, desc.ComponentType, health.ComponentType, "Health().ComponentType should match")
	assert.Equal(t, core.StatusPass, health.Status, "Health().Status should match current status")
	assert.Equal(t, "Everything is fine", health.Output, "Health().Output should match current status output")
}
