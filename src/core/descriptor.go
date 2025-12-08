package core

// Descriptor holds the descriptive information for a component.
type Descriptor struct {
	// ComponentID: (optional) a unique identifier of an instance of a specific sub-component/dependency of a service.
	ComponentID string `json:"componentId,omitempty"`
	// ComponentType: (optional) SHOULD be present if componentName is present. It's a type of the component and could be one of "component", "datastore", "system", or a common/standard term from a well-known source, or a URI.
	ComponentType string `json:"componentType,omitempty"`
	// Version: (optional) public version of the service.
	Version string `json:"version,omitempty"`
	// ReleaseID: (optional) internal release identifier of the service.
	ReleaseID string `json:"releaseId,omitempty"`
	// Notes: (optional) array of notes relevant to the current state of health.
	Notes []string `json:"notes,omitempty"`
	// Links: (optional) has the exact same meaning as the top-level "links" element, but for the sub-component/downstream dependency represented by the details object.
	Links map[string]string `json:"links,omitempty"`
	// ServiceID: (optional) a unique identifier of the service, in the application scope.
	ServiceID string `json:"serviceId,omitempty"`
	// Description: (optional) a human-friendly description of the service.
	Description string `json:"description,omitempty"`
}
