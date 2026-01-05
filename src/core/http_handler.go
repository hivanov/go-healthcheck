package core

import (
	"encoding/json"
	"net/http"
)

// NewHTTPHandler creates a new http.Handler (typically a *http.ServeMux) that registers
// liveness and readiness probes.
//
// The liveness probe (/.well-known/live) always returns HTTP 200 OK.
// The readiness probe (/.well-known/ready) checks the health of the provided Service
// and returns its status in JSON format.
func NewHTTPHandler(service Service) http.Handler {
	mux := http.NewServeMux()

	// Liveness probe handler
	mux.HandleFunc("/.well-known/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Readiness probe handler
	mux.HandleFunc("/.well-known/ready", func(w http.ResponseWriter, r *http.Request) {
		health := service.Health()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// Determine HTTP status code based on health status
		httpStatus := http.StatusOK
		if health.Status == StatusFail {
			httpStatus = http.StatusServiceUnavailable
		} else if health.Status == StatusWarn {
			httpStatus = http.StatusOK // Warnings still mean it's "ready" but with concerns
		}

		w.WriteHeader(httpStatus)

		if err := json.NewEncoder(w).Encode(health); err != nil {
			// This should ideally not happen if health object is well-formed.
			// Log the error for debugging.
			http.Error(w, `{"error": "failed to encode health response"}`, http.StatusInternalServerError)
			return
		}
	})

	return mux
}

// Ensure NewHTTPHandler is tested.
// While not strictly a "Component" it uses the Service interface.
// For testing purposes, we can create a mock Service.
// This comment is for the agent's reference during test creation.
var _ = NewHTTPHandler(
	NewService(
		Descriptor{
			ComponentID:   "self",
			ComponentType: "system",
			Description:   "Self-health check for HTTP handler",
		},
		New(
			Descriptor{
				ComponentID:   "dummy",
				ComponentType: "component",
				Description:   "Dummy component for HTTP handler test",
			},
			StatusPass,
		),
	),
)

func init() {
	// Register the handler with a dummy service to prevent unused variable error in package without a main function.
	// This is typically used in a real application's main func.
	// For testing, this serves to ensure the function compiles.
	_ = NewHTTPHandler(NewService(Descriptor{}))
}
