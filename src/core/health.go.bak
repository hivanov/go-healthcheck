package core

import "time"

// StatusEnum is a health-check status enum.
type StatusEnum string

const (
	// StatusPass is a "pass" status.
	StatusPass StatusEnum = "pass"
	// StatusFail is a "fail" status.
	StatusFail StatusEnum = "fail"
	// StatusWarn is a "warn" status.
	StatusWarn StatusEnum = "warn"
)

// Health provides a health-check response object.
type Health struct {
	// Status: (required) indicates whether the service status is acceptable or not.
	// API publishers SHOULD use "pass" (healthy), "fail" (unhealthy), or "warn" (healthy, with some concerns).
	Status StatusEnum `json:"status"` // Renamed type
	// Version: (optional) public version of the service.
	Version string `json:"version,omitempty"`
	// ReleaseID: (optional) in well-designed APIs, backwards-compatible changes in the service should not update a version
	// number. APIs usually change their version number as infrequently as possible, to preserve a stable interface.
	// However, the implementation of an API may change much more frequently, which leads to the importance of having a
	// separate "release number" or "releaseId" that is different from the public version of the API.
	ReleaseID string `json:"releaseId,omitempty"`
	// Notes: (optional) array of notes relevant to the current state of health.
	Notes []string `json:"notes,omitempty"`
	// Output: (optional) raw error output, in case of "fail" or "warn" states. This field SHOULD be omitted for
	// "pass" state.
	Output string `json:"output,omitempty"`
	// Checks: (optional) an object that provides detailed health statuses of additional downstream systems and endpoints
	// which can affect the overall health of the main API.
	Checks map[string][]ComponentHealth `json:"checks,omitempty"` // Renamed type
	// Links: (optional) an object containing link relations and URIs for external links that MAY contain more
	// information about the health of the endpoint.
	Links map[string]string `json:"links,omitempty"`
	// ServiceID: (optional) a unique identifier of the service, in the application scope.
	ServiceID string `json:"serviceId,omitempty"`
	// Description: (optional) a human-friendly description of the service.
	Description string `json:"description,omitempty"`
}

// ComponentHealth provides a component-specific health-check response object.
type ComponentHealth struct {
	// from the spec: "component-specific health-check object has the same structure as the
	// top-level health-check object, but without the "checks" key."

	// Status: (optional) has the exact same meaning as the top-level "status" element, but for the
	// sub-component/downstream dependency represented by the details object.
	Status StatusEnum `json:"status,omitempty"` // Renamed type
	// Version: (optional) public version of the service.
	Version string `json:"version,omitempty"`
	// ReleaseID: (optional) internal release identifier of the service.
	ReleaseID string `json:"releaseId,omitempty"`
	// Notes: (optional) array of notes relevant to the current state of health.
	Notes []string `json:"notes,omitempty"`
	// Output: (optional) has the exact same meaning as the top-level "output" element, but for the sub-component/downstream
	// dependency represented by the details object. This field SHOULD be omitted for "pass" state of a downstream dependency.
	Output string `json:"output,omitempty"`
	// Links: (optional) has the exact same meaning as the top-level "links" element, but for the
	// sub-component/downstream dependency represented by the details object.
	Links map[string]string `json:"links,omitempty"`
	// ServiceID: (optional) a unique identifier of the service, in the application scope.
	ServiceID string `json:"serviceId,omitempty"`
	// Description: (optional) a human-friendly description of the service.
	Description string `json:"description,omitempty"`

	// additional fields for component health

	// ComponentID: (optional) a unique identifier of an instance of a specific sub-component/dependency of a service.
	ComponentID string `json:"componentId,omitempty"`
	// ComponentType: (optional) SHOULD be present if componentName is present. It's a type of the component and could be
	// one of "component", "datastore", "system", or a common/standard term from a well-known source, or a URI.
	ComponentType string `json:"componentType,omitempty"`
	// ObservedValue: (optional) could be any valid JSON value, such as string, number, object, array, or literal.
	ObservedValue interface{} `json:"observedValue,omitempty"`
	// ObservedUnit: (optional) SHOULD be present if observedValue is present. Clarifies the unit of measurement in which
	// observedValue is reported.
	ObservedUnit string `json:"observedUnit,omitempty"`
	// Time: (optional) the date-time, in ISO8601 format, at which the reading of the observedValue was recorded.
	Time time.Time `json:"time,omitempty"`
	// AffectedEndpoints: (optional) a JSON array containing URI Templates. This field SHOULD be omitted if the "status" field
	// is present and has a value equal to "pass". It indicates which particular endpoints are affected by a check's troubles.
	AffectedEndpoints []string `json:"affectedEndpoints,omitempty"`
}
