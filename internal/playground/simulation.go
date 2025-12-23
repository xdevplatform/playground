// Package playground implements rate limiting simulation.
//
// This file provides the RateLimiter type that tracks requests per endpoint
// and per API credentials (Bearer token), matching real X API behavior. It
// enforces rate limits, tracks remaining requests, and provides reset times
// for rate limit headers.
package playground

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter manages rate limiting simulation
// Rate limits are tracked per endpoint and per API credentials (Bearer token), matching the real X API behavior
type RateLimiter struct {
	configGetter func() *RateLimitConfig // Function to get current config (allows dynamic reloading)
	requests     map[string][]time.Time  // "credentials:endpoint" -> request timestamps (each endpoint has its own limit)
	mu           sync.RWMutex
}

// NewRateLimiter creates a new rate limiter with a static config
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	if config == nil {
		config = &RateLimitConfig{Enabled: false}
	}
	// Store config in a closure for backward compatibility
	finalConfig := config
	return &RateLimiter{
		configGetter: func() *RateLimitConfig {
			return finalConfig
		},
		requests: make(map[string][]time.Time),
	}
}

// NewRateLimiterWithGetter creates a new rate limiter with a config getter function
// This allows the rate limiter to read config dynamically
func NewRateLimiterWithGetter(configGetter func() *RateLimitConfig) *RateLimiter {
	if configGetter == nil {
		configGetter = func() *RateLimitConfig {
			return &RateLimitConfig{Enabled: false}
		}
	}
	return &RateLimiter{
		configGetter: configGetter,
		requests:     make(map[string][]time.Time),
	}
}

// UpdateConfig updates the config getter function (for dynamic reloading)
func (rl *RateLimiter) UpdateConfig(configGetter func() *RateLimitConfig) {
	if configGetter == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.configGetter = configGetter
}

// CheckRateLimit checks if a request should be rate limited based on API credentials and endpoint
// Each endpoint has its own independent rate limit tracking
// Returns (allowed, remaining, resetTime)
func (rl *RateLimiter) CheckRateLimit(credentials string, endpoint string) (bool, int, time.Time) {
	// Get current config dynamically
	config := rl.configGetter()
	if config == nil {
		config = &RateLimitConfig{Enabled: false}
	}
	
	if !config.Enabled {
		return true, config.Limit, time.Now().Add(time.Duration(config.WindowSec) * time.Second)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Create a unique key for this credentials+endpoint combination
	// This ensures each endpoint has its own independent rate limit tracking
	key := credentials + ":" + endpoint

	now := time.Now()
	windowStart := now.Add(-time.Duration(config.WindowSec) * time.Second)

	// Clean old requests and check limit in one pass
	requests := rl.requests[key]
	validRequests := make([]time.Time, 0, len(requests))
	for _, reqTime := range requests {
		if reqTime.After(windowStart) {
			validRequests = append(validRequests, reqTime)
		}
	}

	// Check if limit exceeded before adding new request
	if len(validRequests) >= config.Limit {
		resetTime := validRequests[0].Add(time.Duration(config.WindowSec) * time.Second)
		// Only update if we cleaned up old requests
		if len(validRequests) < len(requests) {
			rl.requests[key] = validRequests
			// If entry is now empty after cleanup, remove it
			if len(validRequests) == 0 {
				delete(rl.requests, key)
			}
		}
		return false, 0, resetTime
	}

	// Add current request
	validRequests = append(validRequests, now)
	rl.requests[key] = validRequests

	remaining := config.Limit - len(validRequests)
	resetTime := now.Add(time.Duration(config.WindowSec) * time.Second)

	// Periodically clean up empty or fully-expired entries to prevent memory leak
	// Clean up every N requests to balance performance and memory usage
	// Only clean up a limited number of entries per call to avoid holding lock too long
	if len(rl.requests) > 0 && len(rl.requests)%RateLimiterCleanupInterval == 0 {
		rl.cleanupExpiredEntriesLimited(now, windowStart, RateLimiterCleanupMaxEntries)
	}

	return true, remaining, resetTime
}

// cleanupExpiredEntriesLimited removes a limited number of expired entries from the requests map
// This prevents memory leaks while avoiding holding the lock for too long
// maxEntries limits how many entries to check/remove per call
func (rl *RateLimiter) cleanupExpiredEntriesLimited(now time.Time, windowStart time.Time, maxEntries int) {
	checked := 0
	for key, requests := range rl.requests {
		if checked >= maxEntries {
			break // Stop after checking maxEntries to avoid holding lock too long
		}
		checked++
		
		// Check if all requests in this entry are expired
		allExpired := true
		for _, reqTime := range requests {
			if reqTime.After(windowStart) {
				allExpired = false
				break
			}
		}
		// Remove entry if all requests are expired or entry is empty
		if allExpired || len(requests) == 0 {
			delete(rl.requests, key)
		}
	}
}

// ShouldSimulateError determines if an error should be simulated based on config
func ShouldSimulateError(config *ErrorConfig) bool {
	if config == nil || !config.Enabled {
		return false
	}
	return rand.Float64() < config.ErrorRate
}

// GetAPICredentials extracts API credentials from the request
// Returns the Bearer token from Authorization header, or a default key if not present
// This matches the real X API behavior where rate limits are per API key/credentials
func GetAPICredentials(r *http.Request) string {
	// Extract Bearer token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Check for Bearer token format: "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
		// Also support just the token without "Bearer" prefix
		if len(parts) == 1 {
			return parts[0]
		}
	}
	
	// Fallback: use a default key for requests without credentials
	// This allows the playground to work without authentication for convenience
	return "default_playground_key"
}

// writeRateLimitError writes a rate limit error response matching X API format
func writeRateLimitError(w http.ResponseWriter, config *RateLimitConfig, resetTime time.Time) {
	AddXAPIHeadersWithRateLimit(w, config, 0, resetTime)
	
	// X API returns errors array format for 429 errors with code 88 (Rate limit exceeded)
	// Always use code 88, never use code 131 (which is for server errors)
	errorResponse := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"message": "Rate limit exceeded",
				"code":    88, // Always use code 88 for rate limits, not 131
				"title":   "Rate Limit Exceeded",
				"type":    "https://api.twitter.com/2/problems/rate-limit-exceeded",
			},
		},
		"title":  "Rate Limit Exceeded",
		"detail": "Rate limit exceeded",
		"type":   "https://api.twitter.com/2/problems/rate-limit-exceeded",
	}
	
	WriteJSONSafe(w, 429, errorResponse)
}

// getEndpointSpecificError extracts an error from example responses or OpenAPI spec for a specific endpoint
// Returns nil if no endpoint-specific error found
func getEndpointSpecificError(method, path string, statusCode int, examples *ExampleStore, op *Operation) []map[string]interface{} {
	// First, try to get errors from example responses
	if examples != nil {
		example := examples.FindBestMatch(method, path)
		if example != nil && example.Response != nil {
			if errors, ok := example.Response["errors"].([]interface{}); ok && len(errors) > 0 {
				// Find an error matching the requested status code
				for _, err := range errors {
					if errMap, ok := err.(map[string]interface{}); ok {
						// Check if this error matches the requested status code
						if errStatus, ok := errMap["status"].(float64); ok && int(errStatus) == statusCode {
							// Convert to the format we need
							formattedErr := make(map[string]interface{})
							if detail, ok := errMap["detail"].(string); ok {
								formattedErr["message"] = detail
							}
							// CRITICAL: Override error code based on status code, not example
							// Example responses may have wrong codes (e.g., code 131 for rate limits)
							// Always use the correct code for the status code
							formattedErr["code"] = mapStatusCodeToErrorCode(statusCode)
							if title, ok := errMap["title"].(string); ok {
								formattedErr["title"] = title
							}
							if errType, ok := errMap["type"].(string); ok {
								formattedErr["type"] = errType
							}
							return []map[string]interface{}{formattedErr}
						}
					}
				}
				// If no exact match, use the first error and adjust status code
				if errMap, ok := errors[0].(map[string]interface{}); ok {
					formattedErr := make(map[string]interface{})
					if detail, ok := errMap["detail"].(string); ok {
						formattedErr["message"] = detail
					}
					// CRITICAL: Override error code based on status code, not example
					formattedErr["code"] = mapStatusCodeToErrorCode(statusCode)
					if title, ok := errMap["title"].(string); ok {
						formattedErr["title"] = title
					}
					if errType, ok := errMap["type"].(string); ok {
						formattedErr["type"] = errType
					}
					return []map[string]interface{}{formattedErr}
				}
			}
		}
	}
	
	// Second, try to get error response schema from OpenAPI spec
	if op != nil {
		statusCodeStr := fmt.Sprintf("%d", statusCode)
		errorSchema := op.GetResponseSchema(statusCodeStr)
		if errorSchema != nil {
			// Try to get example from schema
			if example := op.GetResponseExample(statusCodeStr); example != nil {
				if exampleMap, ok := example.(map[string]interface{}); ok {
					if errors, ok := exampleMap["errors"].([]interface{}); ok && len(errors) > 0 {
						formattedErrors := make([]map[string]interface{}, 0, len(errors))
						for _, err := range errors {
							if errMap, ok := err.(map[string]interface{}); ok {
								formattedErr := make(map[string]interface{})
								if message, ok := errMap["message"].(string); ok {
									formattedErr["message"] = message
								}
								// CRITICAL: Always use correct error code for status code, not from example
								// Example responses may have wrong codes (e.g., code 131 for rate limits)
								formattedErr["code"] = mapStatusCodeToErrorCode(statusCode)
								formattedErrors = append(formattedErrors, formattedErr)
							}
						}
						if len(formattedErrors) > 0 {
							return formattedErrors
						}
					}
				}
			}
		}
	}
	
	return nil
}

// mapStatusCodeToErrorCode maps HTTP status codes to X API error codes
func mapStatusCodeToErrorCode(statusCode int) int {
	switch statusCode {
	case 400:
		return 214 // Bad Request
	case 401:
		return 32 // Authentication failed
	case 403:
		return 200 // Forbidden
	case 404:
		return 50 // Not Found
	case 429:
		return 88 // Rate limit exceeded
	case 500:
		return 131 // Internal server error
	case 503:
		return 130 // Service unavailable
	default:
		return 0
	}
}

// writeSimulatedError writes a simulated error response based on config
// Error format matches X API error response structure exactly
func writeSimulatedError(w http.ResponseWriter, config *ErrorConfig, method, path string, examples *ExampleStore, op *Operation) {
	AddXAPIHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(config.StatusCode)
	
	var errorResponse map[string]interface{}
	
	// X API uses different formats for different error types:
	// - 404 (not_found): Uses errors array with resource details
	// - 401, 429, 500: Simple format with title, detail, type, status (no errors array)
	switch config.ErrorType {
	case "not_found":
		// Not Found errors use errors array format with resource details
		// Try to infer resource type from path
		resourceType := "resource"
		resourceID := "unknown"
		parameter := "id"
		
		// Try to extract resource type and ID from path
		if strings.Contains(path, "/users/") {
			resourceType = "user"
			if parts := strings.Split(path, "/users/"); len(parts) > 1 {
				resourceID = strings.Split(parts[1], "?")[0]
			}
		} else if strings.Contains(path, "/tweets/") {
			resourceType = "tweet"
			if parts := strings.Split(path, "/tweets/"); len(parts) > 1 {
				resourceID = strings.Split(parts[1], "?")[0]
			}
		} else if strings.Contains(path, "/lists/") {
			resourceType = "list"
			if parts := strings.Split(path, "/lists/"); len(parts) > 1 {
				resourceID = strings.Split(parts[1], "?")[0]
			}
		} else if strings.Contains(path, "/spaces/") {
			resourceType = "space"
			if parts := strings.Split(path, "/spaces/"); len(parts) > 1 {
				resourceID = strings.Split(parts[1], "?")[0]
			}
		}
		
		errorResponse = map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"value":         resourceID,
					"detail":        fmt.Sprintf("Could not find %s with id: [%s].", resourceType, resourceID),
					"title":         "Not Found Error",
					"resource_type": resourceType,
					"parameter":     parameter,
					"resource_id":   resourceID,
					"type":          "https://api.twitter.com/2/problems/resource-not-found",
				},
			},
		}
	case "rate_limit":
		// Rate limit errors use simple format (no errors array)
		errorResponse = map[string]interface{}{
			"title":  "Too Many Requests",
			"detail": "Too Many Requests",
			"type":   "about:blank",
			"status": 429,
		}
	case "server_error":
		// Internal server error uses simple format (no errors array)
		errorResponse = map[string]interface{}{
			"title":  "Internal Server Error",
			"detail": "Something is broken. This is usually a temporary error, for example in a high load situation or if an endpoint is temporarily having issues. Check in the developer forums in case others are having similar issues, or try again later. ",
			"type":   "about:blank",
			"status": 500,
		}
	case "unauthorized":
		// Unauthorized error uses simple format (no errors array)
		errorResponse = map[string]interface{}{
			"title":  "Unauthorized",
			"type":   "about:blank",
			"status": 401,
			"detail": "Unauthorized",
		}
	default:
		// Default to rate_limit format for unknown types
		errorResponse = map[string]interface{}{
			"title":  "Too Many Requests",
			"detail": "Too Many Requests",
			"type":   "about:blank",
			"status": 429,
		}
	}
	
	WriteJSONSafe(w, config.StatusCode, errorResponse)
}

