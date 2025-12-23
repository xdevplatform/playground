// Package playground provides HTTP middleware for request processing.
//
// This file contains middleware functions for adding request IDs, CORS headers,
// and tracking response times. The responseTimeWriter wraps http.ResponseWriter
// to capture response duration for statistics and monitoring.
package playground

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// responseTimeWriter wraps http.ResponseWriter to track response time.
// It captures the time when WriteHeader is called and stores it for later use.
type responseTimeWriter struct {
	http.ResponseWriter
	startTime     time.Time
	written       bool
	responseTimeMs int64 // Store response time when calculated
}

// WriteHeader captures response time before writing headers.
// Records the response time for statistics and sets internal headers.
func (w *responseTimeWriter) WriteHeader(statusCode int) {
	if !w.written {
		responseTime := time.Since(w.startTime)
		w.responseTimeMs = responseTime.Milliseconds()
		
		w.Header().Set("X-Internal-Response-Time-Ms", strconv.FormatInt(w.responseTimeMs, 10))
		
		path := w.Header().Get("X-Internal-Path")
		if path != "" && !strings.HasPrefix(path, "/health") && !strings.HasPrefix(path, "/rate-limits") {
			RecordResponseTime(w.responseTimeMs)
		}
		
		w.written = true
		w.ResponseWriter.WriteHeader(statusCode)
	}
	// If already written, don't call WriteHeader again (prevents superfluous WriteHeader warnings)
}

// GetResponseTime returns the captured response time in milliseconds.
// Calculates on-demand if WriteHeader hasn't been called yet.
func (w *responseTimeWriter) GetResponseTime() int64 {
	if !w.written {
		// Calculate on-demand if WriteHeader hasn't been called yet
		return time.Since(w.startTime).Milliseconds()
	}
	return w.responseTimeMs
}

// Write ensures response time is set even if WriteHeader wasn't called.
// Automatically calls WriteHeader with StatusOK if not already called.
func (w *responseTimeWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it.
// Forwards the Flush call to the wrapped ResponseWriter.
func (w *responseTimeWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// AddRequestID adds a unique request ID to the response headers.
// The request ID can be used for tracing requests through logs.
// If a request ID already exists in the request header, it is reused.
func AddRequestID(w http.ResponseWriter, r *http.Request) string {
	// Check if request ID already exists in request header
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = generateRequestID()
	}
	// Add to response headers
	w.Header().Set("X-Request-ID", requestID)
	return requestID
}

// AddCORSHeaders adds CORS headers to enable cross-origin requests.
// Only adds headers if an Origin header is present in the request.
func AddCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "3600")
	}
}

// HandleOptions handles OPTIONS requests for CORS preflight.
// Adds CORS headers and returns 204 No Content.
func HandleOptions(w http.ResponseWriter, r *http.Request) {
	AddCORSHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}
