package core

import "time"

// ComponentStatus holds the detailed status of a component for internal use.
type ComponentStatus struct {
	// Status: (optional) has the exact same meaning as the top-level "status" element, but for the
	// sub-component/downstream dependency represented by the details object.
	Status StatusEnum `json:"status,omitempty"` // Now refers to the enum
	// ObservedValue: (optional) could be any valid JSON value, such as string, number, object, array, or literal.
	ObservedValue interface{} `json:"observedValue,omitempty"`
	// ObservedUnit: (optional) SHOULD be present if observedValue is present. Clarifies the unit of measurement in which
	// observedValue is reported.
	ObservedUnit string `json:"observedUnit,omitempty"`
	// Time: (optional) the date-time, in ISO8601 format, at which the reading of the observedValue was recorded.
	Time time.Time `json:"time,omitempty"`
	// Output: (optional) has the exact same meaning as the top-level "output" element, but for the sub-component/downstream
	// dependency represented by the details object. This field SHOULD be omitted for "pass" state of a downstream dependency.
	Output string `json:"output,omitempty"`
	// AffectedEndpoints: (optional) a JSON array containing URI Templates. This field SHOULD be omitted if the "status" field
	// is present and has a value equal to "pass". It indicates which particular endpoints are affected by a check's troubles.
	AffectedEndpoints []string `json:"affectedEndpoints,omitempty"`
}
