// Package playground provides debug flags for controlling verbose logging.
//
// This file defines global debug flags (HandlerDebug, ResponseDebug, SchemaDebug)
// that can be enabled to get detailed logging output for debugging request
// handling, response generation, and schema processing.
package playground
var (
	// HandlerDebug enables debug logging in request handlers
	HandlerDebug = false
	
	// ResponseDebug enables debug logging in response generation
	ResponseDebug = false
	
	// SchemaDebug enables debug logging in schema processing (uses existing debugMode)
	SchemaDebug = false
)

// SetDebugFlags sets all debug flags at once
func SetDebugFlags(enabled bool) {
	HandlerDebug = enabled
	ResponseDebug = enabled
	SchemaDebug = enabled
}
