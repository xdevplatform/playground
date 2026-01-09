// Package playground provides the unified OpenAPI-driven request handler.
//
// This file contains the main request handler that processes all API requests
// using the OpenAPI specification. It validates requests, applies rate limiting,
// handles authentication, processes stateful operations, and generates responses
// using examples or schema-based generation. This is the core routing and request
// processing logic for the playground server.
package playground

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Constants for request/response limits
const (
	// MaxRequestSize is the maximum size for request bodies (10MB)
	MaxRequestSize = 10 << 20
	// MaxQueryParameterIDs is the maximum number of IDs in query parameters
	MaxQueryParameterIDs = 100
	// MaxPaginationResults is the maximum number of results per page
	MaxPaginationResults = 1000
	// DefaultStreamingDelayMs is the default delay between streamed items (200ms)
	DefaultStreamingDelayMs = 200
	// MaxStreamingDelayMs is the maximum allowed delay for streaming (10 seconds)
	MaxStreamingDelayMs = 10000
	// StreamingRecentWindow is the number of recent items to track to avoid duplicates
	StreamingRecentWindow = 10
)

// Constants for context cancellation and iteration checks
const (
	// ContextCheckIntervalLarge is the interval for checking context cancellation in large loops (1000 iterations)
	// Used for state copying operations that may process many items
	ContextCheckIntervalLarge = 1000
	// ContextCheckIntervalMedium is the interval for checking context cancellation in medium loops (100 iterations)
	// Used for search and iteration operations
	ContextCheckIntervalMedium = 100
)

// Constants for rate limiter cleanup
const (
	// RateLimiterCleanupInterval is the number of requests between cleanup operations
	RateLimiterCleanupInterval = 50
	// RateLimiterCleanupMaxEntries is the maximum number of entries to clean up per operation
	RateLimiterCleanupMaxEntries = 50
)

// getSchemaKeys returns the keys of a schema map.
func getSchemaKeys(schema map[string]interface{}) []string {
	keys := make([]string, 0, len(schema))
	for k := range schema {
		keys = append(keys, k)
	}
	return keys
}

// idValueToString converts an ID value (which may be a string or number in JSON) to a string.
// Handles string, float64 (JSON numbers), and other numeric types.
func idValueToString(idVal interface{}) (string, bool) {
	switch v := idVal.(type) {
	case string:
		if v != "" {
			return v, true
		}
		return "", false
	case float64:
		// JSON numbers unmarshal as float64
		// Convert to string without decimal if it's a whole number
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	default:
		return "", false
	}
}

// getAuthenticatedUserID returns the authenticated user ID from the request.
// For placeholder credentials, this is always "0" (the playground user).
// In a real implementation, this would extract the user ID from the OAuth token.
func getAuthenticatedUserID(r *http.Request, state *State) string {
	// For placeholder credentials, the authenticated user is always the playground user (ID "0")
	// In a real implementation, this would extract the user ID from the OAuth token
	if state != nil {
		defaultUser := state.GetDefaultUser()
		if defaultUser != nil {
			return defaultUser.ID
		}
	}
	return "0"
}

// getDeveloperAccountID returns the developer account ID from the request.
// In the real X API, this is the account that owns the API keys/apps.
// This function extracts or derives the developer account ID from the authentication token.
//
// Strategy:
//   - For Bearer tokens: Derive from token hash or extract if token contains account info
//   - For OAuth 1.0a: Use consumer key to derive account ID
//   - For OAuth 2.0: Extract from token claims
//   - Fallback: Use authenticated user ID (for backward compatibility with simple tokens)
func getDeveloperAccountID(r *http.Request, state *State) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// No auth header, use default user ID as fallback
		return getAuthenticatedUserID(r, state)
	}

	// Try to extract developer account ID from different auth methods
	authLower := strings.ToLower(authHeader)
	
	// Bearer token (OAuth 2.0 App-Only or User Context)
	if strings.HasPrefix(authLower, "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		// For simple tokens like "test" or "Bearer test", use a hash-based approach
		// In production, this would decode the JWT and extract the developer account ID
		if token == "test" || token == "" {
			// Default playground token maps to developer account "0"
			return "0"
		}
		// For other tokens, derive developer account ID from token hash
		// This ensures consistent mapping: same token = same developer account
		return deriveDeveloperAccountFromToken(token)
	}
	
	// OAuth 1.0a: Extract consumer key from Authorization header
	// Format: OAuth oauth_consumer_key="...", oauth_token="...", etc.
	if strings.HasPrefix(authLower, "oauth ") {
		consumerKey := extractOAuthConsumerKey(authHeader)
		if consumerKey != "" {
			// Derive developer account ID from consumer key
			return deriveDeveloperAccountFromToken(consumerKey)
		}
	}
	
	// Fallback: Use authenticated user ID
	// This maintains backward compatibility for simple auth scenarios
	return getAuthenticatedUserID(r, state)
}

// deriveDeveloperAccountFromToken derives a consistent developer account ID from a token/key.
// Uses a simple hash-based approach to ensure the same token always maps to the same account.
// In production, this would extract the actual developer account ID from the token.
func deriveDeveloperAccountFromToken(token string) string {
	if token == "" {
		return "0"
	}
	
	// Simple hash to convert token to a consistent account ID
	// This ensures same token = same developer account
	hash := 0
	for _, c := range token {
		hash = hash*31 + int(c)
	}
	
	// Convert to positive number and use modulo to keep IDs reasonable
	accountID := (hash & 0x7FFFFFFF) % 1000
	
	// Special case: common test tokens map to account "0"
	if token == "test" || token == "Bearer test" {
		return "0"
	}
	
	return strconv.Itoa(accountID)
}

// extractOAuthConsumerKey extracts the consumer key from an OAuth 1.0a Authorization header.
func extractOAuthConsumerKey(authHeader string) string {
	// Parse OAuth header: OAuth oauth_consumer_key="...", oauth_token="...", etc.
	// Look for oauth_consumer_key="value" or oauth_consumer_key=value
	re := regexp.MustCompile(`oauth_consumer_key=["']?([^"',\s]+)["']?`)
	matches := re.FindStringSubmatch(authHeader)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// validateUserIDMatchesAuthenticatedUser checks if the path parameter user ID matches the authenticated user ID.
// Returns an error response if they don't match, or nil if they do match.
func validateUserIDMatchesAuthenticatedUser(pathUserID string, authenticatedUserID string) ([]byte, int) {
	if pathUserID != authenticatedUserID {
		errorResp := CreateValidationErrorResponse("id", pathUserID, fmt.Sprintf("The `id` query parameter value [%s] must be the same as the authenticating user [%s]", pathUserID, authenticatedUserID))
		data, statusCode := MarshalJSONErrorResponse(errorResp)
		return data, statusCode
	}
	return nil, 0
}

// countResourcesFromResponse counts the number of resources in a JSON response.
// For eventTypePricing endpoints, this represents the number of resources fetched.
// Returns 0 if the response cannot be parsed or doesn't contain a data array.
func countResourcesFromResponse(responseData []byte) int {
	if len(responseData) == 0 {
		return 0
	}
	
	var response map[string]interface{}
	if err := json.Unmarshal(responseData, &response); err != nil {
		return 0
	}
	
	// Check for data array
	if data, exists := response["data"]; exists {
		switch v := data.(type) {
		case []interface{}:
			// Array of resources
			return len(v)
		case map[string]interface{}:
			// Single resource object
			return 1
		}
	}
	
	// Check for includes (expansions)
	if includes, exists := response["includes"]; exists {
		if includesMap, ok := includes.(map[string]interface{}); ok {
			total := 0
			for _, items := range includesMap {
				if itemsArray, ok := items.([]interface{}); ok {
					total += len(itemsArray)
				}
			}
			// If we have includes, also count data if it exists
			if data, exists := response["data"]; exists {
				if dataArray, ok := data.([]interface{}); ok {
					return len(dataArray) + total
				} else if _, ok := data.(map[string]interface{}); ok {
					return 1 + total
				}
			}
			return total
		}
	}
	
	return 0
}

// createUnifiedOpenAPIHandler creates a handler that uses OpenAPI for all endpoints.
// It integrates with state for stateful operations and handles validation, rate limiting, and response generation.
func createUnifiedOpenAPIHandler(spec *OpenAPISpec, state *State, examples *ExampleStore, server *Server) http.HandlerFunc {
	// Initialize rate limiter with dynamic config getter (allows runtime config changes)
	var rateLimiter *RateLimiter
	if state != nil {
		// Create rate limiter that reads config dynamically from state
		rateLimiter = NewRateLimiterWithGetter(func() *RateLimitConfig {
			if state != nil && state.config != nil {
				return state.config.GetRateLimitConfig()
			}
			return &RateLimitConfig{Enabled: false}
		})
	}
	
	return func(w http.ResponseWriter, r *http.Request) {
		// Track active requests for graceful shutdown (skip health/status endpoints)
		if server != nil && !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/rate-limits") {
			atomic.AddInt64(&server.activeReqs, 1)
			defer atomic.AddInt64(&server.activeReqs, -1)
		}
		
		// Handle CORS preflight requests
		if r.Method == "OPTIONS" {
			HandleOptions(w, r)
			return
		}
		
		// Check original request context first (before creating timeout context)
		// This handles cases where the client already disconnected
		select {
		case <-r.Context().Done():
			err := r.Context().Err()
			if err == context.Canceled {
				// Client disconnected, no response needed
				return
			}
			// If it's a deadline exceeded, we'll handle it below after creating timeout context
		default:
		}
		
		// Add request timeout for non-streaming endpoints (30 seconds)
		// Streaming endpoints check context in their loops
		var ctx context.Context
		var cancel context.CancelFunc
		if !strings.Contains(r.URL.Path, "/stream") {
			ctx, cancel = context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		
		// Check for timeout after creating timeout context
		select {
		case <-r.Context().Done():
			err := r.Context().Err()
			if err == context.DeadlineExceeded {
				WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
					"errors": []map[string]interface{}{
						{"message": "Request timeout", "code": 408},
					},
				})
			} else if err == context.Canceled {
				// Client disconnected, no response needed
				return
			}
			return
		default:
		}
		
		requestID := AddRequestID(w, r)
		_ = requestID
		
		AddCORSHeaders(w, r)
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestSize)
		startTime := time.Now()
		
		if !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/rate-limits") {
			IncrementRequestsTotal()
		}
		
		path := r.URL.Path
		method := r.Method
		pathWithoutQuery := strings.Split(path, "?")[0]
		
		wrappedWriter := &responseTimeWriter{
			ResponseWriter: w,
			startTime:      startTime,
		}
		wrappedWriter.Header().Set("X-Internal-Path", path)
		w = wrappedWriter
		
		// Find matching operations in OpenAPI spec (needed for auth validation)
		// Find operation matching the HTTP method
		// CRITICAL: Check exact path FIRST before pattern matching
		// This prevents paths like /2/users/personalized_trends from matching /2/users/{username}
		var matchedOp *EndpointOperation
		
		// First, check if exact path exists in spec (highest priority)
		if pathItem, exists := spec.Paths[pathWithoutQuery]; exists {
			// Exact path exists - extract operations and find method match
			exactOps := extractOperations(pathWithoutQuery, pathItem)
			for i := range exactOps {
				if exactOps[i].Method == method {
					matchedOp = &exactOps[i]
					break
				}
			}
		}
		
		// Only if no exact match, try pattern matching
		if matchedOp == nil {
			// SIMPLE CHECK: If the last segment of the path has underscores, it's likely a literal endpoint
			// Don't match it to parameterized paths (usernames don't have underscores in X API)
			pathParts := strings.Split(pathWithoutQuery, "/")
			lastSegment := ""
			if len(pathParts) > 0 {
				lastSegment = pathParts[len(pathParts)-1]
			}
			hasUnderscoreInLastSegment := strings.Contains(lastSegment, "_")
			
			operations := spec.GetEndpointOperations(pathWithoutQuery)
			for _, op := range operations {
				if op.Method == method {
					// Only use if it's actually a pattern match (contains {)
					// Don't use if it's the same path (should have been caught above)
					if op.Path != pathWithoutQuery && strings.Contains(op.Path, "{") {
						// CRITICAL: If last segment has underscore, don't match to any parameterized path
						// This prevents /2/users/personalized_trends from matching /2/users/{username}
						if hasUnderscoreInLastSegment {
							// Check if this pattern would extract the last segment as a parameter
							opParts := strings.Split(op.Path, "/")
							if len(opParts) == len(pathParts) {
								opLastPart := opParts[len(opParts)-1]
								if strings.HasPrefix(opLastPart, "{") && strings.HasSuffix(opLastPart, "}") {
									log.Printf("DEBUG: REJECTING pattern match '%s' for path '%s' - last segment '%s' has underscore, likely literal endpoint", op.Path, pathWithoutQuery, lastSegment)
									continue
								}
							}
						}
						
						matchedOp = &op
						break
					}
				}
			}
		}
		
		// Check authentication requirements (before rate limiting)
		// This ensures we return proper auth errors matching the real X API
		// Use OpenAPI operation's security requirements if available
		var opForAuth *Operation
		if matchedOp != nil {
			opForAuth = matchedOp.Operation
		}
		var authConfig *AuthConfig
		if state != nil && state.config != nil {
			authConfig = state.config.GetAuthConfig()
		}
		// Check rate limiting BEFORE auth (so we can show correct rate limits even for auth errors)
		// Rate limits are tracked by API credentials (Bearer token), matching real X API behavior
		// Use endpoint-specific rate limits if available, otherwise use default
		var rateLimitRemaining int = -1
		var rateLimitResetTime time.Time
		var activeRateLimitConfig *RateLimitConfig
		
		// Always check for endpoint-specific rate limit first (regardless of rate limiter config)
		// This ensures endpoint-specific limits are always applied when available
		credentials := GetAPICredentials(r)
		var rateLimitConfig *RateLimitConfig
		if state != nil && state.config != nil {
			rateLimitConfig = state.config.GetRateLimitConfig()
		}
		endpointLimit := GetEndpointRateLimit(method, pathWithoutQuery, rateLimitConfig)
		if endpointLimit != nil {
			// Use endpoint-specific rate limit
			activeRateLimitConfig = &RateLimitConfig{
				Limit:     endpointLimit.Limit,
				WindowSec: endpointLimit.WindowSec,
				Enabled:   true,
			}
			
			// CRITICAL: Use the matched endpoint pattern as the rate limit key, not the full path
			// This ensures /2/lists/0, /2/lists/1, etc. all share the same rate limit as /2/lists
			// The endpointLimit.Endpoint is the pattern that matched (e.g., "/2/lists")
			rateLimitKey := endpointLimit.Endpoint
			if rateLimitKey == "" {
				// Fallback to normalized path if no pattern matched
				rateLimitKey = pathWithoutQuery
				if idx := strings.IndexByte(rateLimitKey, '?'); idx != -1 {
					rateLimitKey = rateLimitKey[:idx]
				}
				if len(rateLimitKey) > 1 && rateLimitKey[len(rateLimitKey)-1] == '/' {
					rateLimitKey = rateLimitKey[:len(rateLimitKey)-1]
				}
			}
			
			if rateLimiter != nil {
				// Create rate limiter with endpoint-specific config but share request tracking
				// Use a config getter that returns the endpoint-specific config
				endpointRateLimiter := &RateLimiter{
					configGetter: func() *RateLimitConfig {
						return activeRateLimitConfig
					},
					requests: rateLimiter.requests, // Share the same request tracking map (but keys include endpoint)
					mu:       rateLimiter.mu,
				}
				allowed, remaining, resetTime := endpointRateLimiter.CheckRateLimit(credentials, rateLimitKey)
				if !allowed {
					writeRateLimitError(w, activeRateLimitConfig, resetTime)
					return
				}
				rateLimitRemaining = remaining
				rateLimitResetTime = resetTime
			} else {
				// Create a new rate limiter for this endpoint with endpoint-specific config
				finalConfig := activeRateLimitConfig
				rateLimiter = NewRateLimiterWithGetter(func() *RateLimitConfig {
					return finalConfig
				})
				allowed, remaining, resetTime := rateLimiter.CheckRateLimit(credentials, rateLimitKey)
				if !allowed {
					writeRateLimitError(w, activeRateLimitConfig, resetTime)
					return
				}
				rateLimitRemaining = remaining
				rateLimitResetTime = resetTime
			}
		} else if rateLimiter != nil {
			// No endpoint-specific limit, use configured rate limiter's config
			activeRateLimitConfig = rateLimiter.configGetter()
			// Normalize endpoint path for consistent tracking (remove query params, trailing slashes)
			endpointKey := pathWithoutQuery
			if idx := strings.IndexByte(endpointKey, '?'); idx != -1 {
				endpointKey = endpointKey[:idx]
			}
			if len(endpointKey) > 1 && endpointKey[len(endpointKey)-1] == '/' {
				endpointKey = endpointKey[:len(endpointKey)-1]
			}
			allowed, remaining, resetTime := rateLimiter.CheckRateLimit(credentials, endpointKey)
			if !allowed {
				config := rateLimiter.configGetter()
				if config == nil {
					config = &RateLimitConfig{Enabled: false}
				}
				writeRateLimitError(w, config, resetTime)
				return
			}
			rateLimitRemaining = remaining
			rateLimitResetTime = resetTime
		} else {
			// No rate limiter and no endpoint-specific limit, use default
			activeRateLimitConfig = GetDefaultRateLimit()
		}
		
		
		// Check authentication requirements (after rate limiting so we can show correct limits)
		if isValid, authError := ValidateAuth(method, path, r, opForAuth, authConfig); !isValid {
			// Set rate limit headers before writing auth error
			if activeRateLimitConfig != nil {
				// Use config limit if remaining not set
				remaining := rateLimitRemaining
				if remaining < 0 {
					remaining = activeRateLimitConfig.Limit - 1 // Assume one request used
				}
				resetTime := rateLimitResetTime
				if resetTime.IsZero() {
					resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
				}
				AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
			} else {
				AddXAPIHeaders(w)
			}
			WriteAuthError(w, authError)
			return
		}
		
		// Get pathItem for path-level parameters
		var pathItem *PathItem
		if matchedOp != nil {
			if pathItemData, exists := spec.Paths[matchedOp.Path]; exists {
				pathItem = &pathItemData
			}
		}
		
		// Parse query parameters (pass operation, spec, and pathItem for OpenAPI-based defaults/limits)
		var op *Operation
		if matchedOp != nil {
			op = matchedOp.Operation
		}
		queryParams := ParseQueryParams(r, op, spec, pathItem)

		// Check for stateful POST/DELETE operations that might not be in OpenAPI spec
		// These need to be checked BEFORE logging warnings or returning 404
		normalizedPathForStateful := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
		hasStatefulHandler := false
		
		// Check for GET /2/media/{media_key} - handle before OpenAPI matching
		if method == "GET" && matchedOp == nil && strings.HasPrefix(normalizedPathForStateful, "/2/media/") && normalizedPathForStateful != "/2/media" {
			if state != nil {
				// Extract media_key from path (e.g., /2/media/1000_1 -> 1000_1)
				mediaKey := strings.TrimPrefix(normalizedPathForStateful, "/2/media/")
				if mediaKey != "" {
					media := state.GetMediaByKey(mediaKey)
					if media != nil {
						// Create a dummy EndpointOperation for formatStateDataToOpenAPI
						dummyOp := &EndpointOperation{
							Path:      normalizedPathForStateful,
							Method:    method,
							Operation: &Operation{},
						}
						responseData := formatStateDataToOpenAPI(media, dummyOp, spec, queryParams, state)
						AddXAPIHeaders(w)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if _, err := w.Write(responseData); err != nil {
							log.Printf("Error writing response: %v", err)
						}
						// Track credit usage at the developer account level (matches real X API behavior)
						if server != nil && server.creditTracker != nil {
							developerAccountID := getDeveloperAccountID(r, state)
							server.creditTracker.TrackUsage(developerAccountID, method, normalizedPathForStateful, responseData, http.StatusOK)
						}
						return
					} else {
						// Media not found - return error matching real API format
						errorResponse := map[string]interface{}{
							"errors": []map[string]interface{}{
								{
									"value":         mediaKey,
									"detail":        fmt.Sprintf("Could not find media with media_keys: [%s].", mediaKey),
									"title":         "Not Found Error",
									"resource_type": "media",
									"parameter":     "media_key",
									"resource_id":   mediaKey,
									"type":          "https://api.twitter.com/2/problems/resource-not-found",
								},
							},
							"title":  "Not Found Error",
							"detail": fmt.Sprintf("Could not find media with media_keys: [%s].", mediaKey),
							"type":   "https://api.twitter.com/2/problems/resource-not-found",
						}
						data, _ := json.Marshal(errorResponse)
						AddXAPIHeaders(w)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusNotFound)
						if _, err := w.Write(data); err != nil {
							log.Printf("Error writing response: %v", err)
						}
						return
					}
				}
			}
		}
		
		if matchedOp == nil {
			// Check for known stateful POST endpoints
			if method == "POST" {
				if (strings.HasPrefix(normalizedPathForStateful, "/2/users/") && strings.HasSuffix(normalizedPathForStateful, "/blocking")) ||
				   (strings.HasPrefix(normalizedPathForStateful, "/2/users/") && strings.HasSuffix(normalizedPathForStateful, "/muting")) ||
				   (strings.HasPrefix(normalizedPathForStateful, "/2/users/") && strings.HasSuffix(normalizedPathForStateful, "/following")) ||
				   (normalizedPathForStateful == "/2/lists") {
					hasStatefulHandler = true
				}
			}
			// Check for known stateful DELETE endpoints
			if method == "DELETE" {
				if (strings.HasPrefix(normalizedPathForStateful, "/2/users/") && strings.Contains(normalizedPathForStateful, "/blocking/")) ||
				   (strings.HasPrefix(normalizedPathForStateful, "/2/users/") && strings.Contains(normalizedPathForStateful, "/muting/")) {
					hasStatefulHandler = true
				}
			}
		}

		// If no exact match, try to find any operation for this path (fallback)
		if matchedOp == nil {
			operations := spec.GetEndpointOperations(pathWithoutQuery)
			if len(operations) > 0 {
				matchedOp = &operations[0]
				// Only log warning if we don't have a stateful handler for this
				if !hasStatefulHandler {
					log.Printf("Method %s not found for %s, using %s", method, path, matchedOp.Method)
				}
			}
		}

		// Check for stateful DELETE operations that might not be in OpenAPI spec
		// These need to be checked BEFORE returning 404
		if matchedOp == nil && method == "DELETE" {
			normalizedPathForDelete := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
			// DELETE /2/users/{id}/blocking/{target_user_id}
			if strings.HasPrefix(normalizedPathForDelete, "/2/users/") && strings.Contains(normalizedPathForDelete, "/blocking/") {
				parts := strings.Split(normalizedPathForDelete, "/blocking/")
				if len(parts) == 2 {
					userIDStr := strings.TrimPrefix(parts[0], "/2/users/")
					targetUserID := parts[1]
					if userIDStr != "" && targetUserID != "" && state != nil {
						state.UnblockUser(userIDStr, targetUserID)
						response := map[string]interface{}{
							"data": map[string]interface{}{
								"blocking": false,
							},
						}
						data, statusCode := MarshalJSONResponse(response)
						AddXAPIHeaders(w)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					if _, err := w.Write(data); err != nil {
						log.Printf("Error writing response: %v", err)
					}
					return
					}
				}
			}
			// DELETE /2/users/{id}/muting/{target_user_id}
			if strings.HasPrefix(normalizedPathForDelete, "/2/users/") && strings.Contains(normalizedPathForDelete, "/muting/") {
				parts := strings.Split(normalizedPathForDelete, "/muting/")
				if len(parts) == 2 {
					userIDStr := strings.TrimPrefix(parts[0], "/2/users/")
					targetUserID := parts[1]
					if userIDStr != "" && targetUserID != "" && state != nil {
						state.UnmuteUser(userIDStr, targetUserID)
						response := map[string]interface{}{
							"data": map[string]interface{}{
								"muting": false,
							},
						}
						data, statusCode := MarshalJSONResponse(response)
						AddXAPIHeaders(w)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					if _, err := w.Write(data); err != nil {
						log.Printf("Error writing response: %v", err)
					}
					return
					}
				}
			}
		}

		if matchedOp == nil {
			WriteError(w, http.StatusNotFound, fmt.Sprintf("Endpoint not found: %s %s", method, path), 404)
			return
		}

		// Check for error simulation (after we have the matched operation for endpoint-specific errors)
		if state != nil && state.config != nil {
			errorConfig := state.config.GetErrorConfig()
			if ShouldSimulateError(errorConfig) {
				var op *Operation
				if matchedOp != nil {
					op = matchedOp.Operation
				}
				writeSimulatedError(w, errorConfig, method, pathWithoutQuery, examples, op)
				return
			}
		}

		// Validate query parameters (max_results bounds, etc.)
		var opForValidation *Operation
		if matchedOp != nil {
			opForValidation = matchedOp.Operation
		}
		if err := ValidateMaxResults(r, opForValidation, spec, pathItem); err != nil {
			errorResponse := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
			errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
			// Set rate limit headers if available
			if activeRateLimitConfig != nil && rateLimitRemaining >= 0 {
				AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, rateLimitRemaining, rateLimitResetTime)
			} else {
				AddXAPIHeaders(w)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			if _, err := w.Write(errorJSON); err != nil {
				log.Printf("Error writing error response: %v", err)
			}
			return
		}

		// Validate ids parameter for /2/tweets endpoint BEFORE OpenAPI validation
		// This ensures we return the correct error format matching X API
		if method == "GET" && pathWithoutQuery == "/2/tweets" {
			idsParam := r.URL.Query().Get("ids")
			if idsParam != "" {
				ids := strings.Split(idsParam, ",")
			// Limit to maximum number of IDs
			if len(ids) > MaxQueryParameterIDs {
				errorResp := CreateValidationErrorResponse("ids", "", fmt.Sprintf("Maximum %d IDs allowed per request", MaxQueryParameterIDs))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				AddXAPIHeaders(w)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					if _, err := w.Write(data); err != nil {
						log.Printf("Error writing response: %v", err)
					}
					return
			}
			var invalidIds []string
				for _, id := range ids {
					id = strings.TrimSpace(id)
					if err := ValidateSnowflakeID(id); err != nil {
						invalidIds = append(invalidIds, id)
					}
				}
				if len(invalidIds) > 0 {
					// Format error response matching X API format exactly
					errorResp := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"parameters": map[string]interface{}{
									"id": invalidIds,
								},
								"message": fmt.Sprintf("The `id` query parameter value [%s] is not valid", invalidIds[0]),
							},
						},
						"title":  "Invalid Request",
						"detail": "One or more parameters to your request was invalid.",
						"type":   "https://api.twitter.com/2/problems/invalid-request",
					}
					errorJSON, statusCode := MarshalJSONErrorResponse(errorResp)
					// Set rate limit headers if available
					if activeRateLimitConfig != nil {
						remaining := rateLimitRemaining
						if remaining < 0 {
							remaining = activeRateLimitConfig.Limit - 1
						}
						resetTime := rateLimitResetTime
						if resetTime.IsZero() {
							resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
						}
						AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
					} else {
						AddXAPIHeaders(w)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					w.Write(errorJSON)
					return
				}
			}
		}

		// Handle /2/spaces?ids= BEFORE OpenAPI validation
		// Space IDs are alphanumeric (not Snowflake IDs), so we handle this endpoint
		// before OpenAPI validation to avoid incorrect Snowflake ID validation
		if method == "GET" && pathWithoutQuery == "/2/spaces" {
			idsParam := r.URL.Query().Get("ids")
			if idsParam != "" {
				ids := strings.Split(idsParam, ",")
				// Trim whitespace from IDs
				for i := range ids {
					ids[i] = strings.TrimSpace(ids[i])
				}
				// Limit to maximum number of IDs
				if len(ids) > MaxQueryParameterIDs {
					errorResp := CreateValidationErrorResponse("ids", "", fmt.Sprintf("Maximum %d IDs allowed per request", MaxQueryParameterIDs))
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					AddXAPIHeaders(w)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(statusCode)
					if _, err := w.Write(data); err != nil {
						log.Printf("Error writing response: %v", err)
					}
					return
				}
				// Limit to 100 spaces (X API limit)
				if len(ids) > 100 {
					ids = ids[:100]
				}
				spaces := state.GetSpaces(ids)
				// Track which IDs were found
				foundIDs := make(map[string]bool)
				for _, space := range spaces {
					foundIDs[space.ID] = true
				}
				// Find IDs that were not found
				var notFoundIDs []string
				for _, id := range ids {
					if !foundIDs[id] {
						notFoundIDs = append(notFoundIDs, id)
					}
				}
				// Format as array response
				spacesData := make([]interface{}, len(spaces))
				for i, space := range spaces {
					spacesData[i] = formatSpace(space)
				}
				response := map[string]interface{}{
					"data": spacesData,
				}
				// Add "Not Found" errors for any IDs that don't exist
				if len(notFoundIDs) > 0 {
					var errors []map[string]interface{}
					for _, id := range notFoundIDs {
						errors = append(errors, map[string]interface{}{
							"value":         id,
							"detail":        fmt.Sprintf("Could not find space with ids: [%s].", id),
							"title":         "Not Found Error",
							"resource_type": "space",
							"parameter":     "ids",
							"resource_id":   id,
							"type":          "https://api.twitter.com/2/problems/resource-not-found",
						})
					}
					response["errors"] = errors
				}
				data, statusCode := MarshalJSONResponse(response)
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(data); err != nil {
					log.Printf("Error writing response: %v", err)
				}
				return
			}
		}

		// Validate request against OpenAPI spec (path and query parameters)
		// CRITICAL: Clear matchedOp if it's a bad pattern match BEFORE validation
		// This prevents /2/users/personalized_trends from being validated against /2/users/{username}
		if matchedOp != nil && matchedOp.Path != pathWithoutQuery {
			// SIMPLE CHECK: If last segment has underscore and would be matched to a parameter, clear it
			pathParts := strings.Split(pathWithoutQuery, "/")
			opParts := strings.Split(matchedOp.Path, "/")
			if len(pathParts) == len(opParts) && len(pathParts) > 0 {
				lastSegment := pathParts[len(pathParts)-1]
				opLastPart := opParts[len(opParts)-1]
				if strings.Contains(lastSegment, "_") && strings.HasPrefix(opLastPart, "{") && strings.HasSuffix(opLastPart, "}") {
					log.Printf("DEBUG: CLEARING bad pattern match - path '%s' matched to '%s' - last segment '%s' has underscore", pathWithoutQuery, matchedOp.Path, lastSegment)
					matchedOp = nil
				}
			}
		}
		
		// Only validate if we have a matched operation
		if matchedOp != nil {
			// Use pathItem to include path-level parameters in validation
			var pathItemForValidation *PathItem
			if spec != nil {
				if pathItemData, exists := spec.Paths[matchedOp.Path]; exists {
					pathItemForValidation = &pathItemData
				}
			}
			if validationErrors := ValidateRequestWithPathItem(r, matchedOp.Operation, matchedOp.Path, spec, pathItemForValidation); len(validationErrors) > 0 {
				errorResponse := FormatValidationErrors(validationErrors)
				errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(errorJSON); err != nil {
					log.Printf("Error writing error response: %v", err)
				}
				return
			}
		}

		// Validate field names (return error if invalid fields are requested)
		if matchedOp != nil && matchedOp.Operation != nil {
			// Get path item to access path-level parameters
			var pathItem *PathItem
			if spec != nil {
				if pathItemData, exists := spec.Paths[matchedOp.Path]; exists {
					pathItem = &pathItemData
				}
			}
			if fieldValidationErrors := queryParams.ValidateFields(matchedOp.Operation, spec, pathItem); len(fieldValidationErrors) > 0 {
				errorResponse := FormatValidationErrors(fieldValidationErrors)
				errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(errorJSON); err != nil {
					log.Printf("Error writing error response: %v", err)
				}
				return
			}

			// Validate expansions (return error if invalid expansions are requested)
			if expansionValidationErrors := queryParams.ValidateExpansions(matchedOp.Operation, spec, r.URL.Path); len(expansionValidationErrors) > 0 {
				errorResponse := FormatValidationErrors(expansionValidationErrors)
				errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(errorJSON); err != nil {
					log.Printf("Error writing error response: %v", err)
				}
				return
			}
		}

		// Validate request body for POST/PUT/PATCH requests
		if method == "POST" || method == "PUT" || method == "PATCH" {
			// Read body (we need to read it to validate, but also need it for processing)
			// So we'll read it once and store it
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResponse := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(errorJSON); err != nil {
					log.Printf("Error writing error response: %v", err)
				}
				return
			}

			// Validate request body against OpenAPI schema
			if bodyValidationErrors := ValidateRequestBody(bodyBytes, matchedOp.Operation, spec); len(bodyValidationErrors) > 0 {
				errorResponse := FormatValidationErrors(bodyValidationErrors)
				errorJSON, statusCode := MarshalJSONErrorResponse(errorResponse)
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(errorJSON); err != nil {
					log.Printf("Error writing error response: %v", err)
				}
				return
			}

			// Restore body for further processing
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Check if this is a streaming endpoint (must be checked early)
		// Exclude rule management endpoints FIRST - these are NOT streaming endpoints
		// Rule management endpoints: /2/tweets/search/stream/rules and /2/tweets/search/stream/rules/*
		// Check this BEFORE any streaming pattern matching
		isRuleManagementEndpoint := strings.Contains(path, "/stream/rules")
		
		// Skip streaming check entirely for rule management endpoints
		// This must happen before any streaming detection
		if !isRuleManagementEndpoint {
			// Check both by path pattern and OpenAPI spec
			// For /search/stream, only match if it's exactly /search/stream (not /search/stream/rules)
			// But allow /firehose/stream/lang/{lang} and similar patterns
			isStreamingPath := strings.HasSuffix(path, "/stream") ||
			                  (strings.Contains(path, "/sample/stream") && !strings.Contains(path, "/sample/stream/rules")) ||
			                  (strings.Contains(path, "/sample10/stream") && !strings.Contains(path, "/sample10/stream/rules")) ||
			                  (path == "/2/tweets/search/stream" || strings.HasPrefix(path, "/2/tweets/search/stream?") || strings.Contains(path, "/search/stream/lang/")) ||
			                  (strings.Contains(path, "/firehose/stream") && !strings.Contains(path, "/firehose/stream/rules")) ||
			                  (strings.Contains(path, "/compliance/stream") && !strings.Contains(path, "/compliance/stream/rules"))
			
			// Check OpenAPI spec
			isStreamingInSpec := false
			if matchedOp != nil && matchedOp.Operation != nil {
				isStreamingInSpec = matchedOp.Operation.IsStreamingEndpoint()
			}
			
			if strings.Contains(path, "/firehose/stream") {
				log.Printf("DEBUG handlers_unified: path='%s', isStreamingPath=%v, isStreamingInSpec=%v, will call handleStreamingEndpoint=%v", path, isStreamingPath, isStreamingInSpec, isStreamingPath || isStreamingInSpec)
			}
			
			if isStreamingPath || isStreamingInSpec {
				log.Printf("DEBUG handlers_unified: Calling handleStreamingEndpoint for path='%s'", path)
				var creditTracker *CreditTracker
				if server != nil {
					creditTracker = server.creditTracker
				}
				if handleStreamingEndpoint(w, matchedOp, path, method, r, state, spec, queryParams, creditTracker) {
					return
				}
				log.Printf("DEBUG handlers_unified: handleStreamingEndpoint returned false for path='%s'", path)
			}
		}

		// Handle relationship endpoints first (user, tweet, list relationships)
		// Get pathItem and operation for relationship handlers
		var opForRelationship *Operation
		var pathItemForRelationship *PathItem
		if matchedOp != nil {
			opForRelationship = matchedOp.Operation
			if pathItemData, exists := spec.Paths[matchedOp.Path]; exists {
				pathItemForRelationship = &pathItemData
			}
		}
		if responseData, statusCode := handleUserRelationshipEndpoints(path, method, r, state, spec, queryParams, opForRelationship, pathItemForRelationship); responseData != nil {
			// Set rate limit headers if available
			if activeRateLimitConfig != nil {
				remaining := rateLimitRemaining
				if remaining < 0 {
					remaining = activeRateLimitConfig.Limit - 1
				}
				resetTime := rateLimitResetTime
				if resetTime.IsZero() {
					resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
				}
				AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
			} else {
				AddXAPIHeaders(w)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			if _, err := w.Write(responseData); err != nil {
				log.Printf("Error writing response: %v", err)
			}
			return
		}

		// Handle tweet relationship endpoints
		if responseData, statusCode := handleTweetRelationshipEndpoints(path, method, r, state, spec, queryParams, opForRelationship, pathItemForRelationship); responseData != nil {
			// Set rate limit headers if available
			if activeRateLimitConfig != nil {
				remaining := rateLimitRemaining
				if remaining < 0 {
					remaining = activeRateLimitConfig.Limit - 1
				}
				resetTime := rateLimitResetTime
				if resetTime.IsZero() {
					resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
				}
				AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
			} else {
				AddXAPIHeaders(w)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			if _, err := w.Write(responseData); err != nil {
				log.Printf("Error writing response: %v", err)
			}
			return
		}

		// Handle list relationship endpoints
		if responseData, statusCode := handleListRelationshipEndpoints(path, method, r, state, spec, queryParams, opForRelationship, pathItemForRelationship); responseData != nil {
			// Set rate limit headers if available
			if activeRateLimitConfig != nil {
				remaining := rateLimitRemaining
				if remaining < 0 {
					remaining = activeRateLimitConfig.Limit - 1
				}
				resetTime := rateLimitResetTime
				if resetTime.IsZero() {
					resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
				}
				AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
			} else {
				AddXAPIHeaders(w)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			if _, err := w.Write(responseData); err != nil {
				log.Printf("Error writing response: %v", err)
			}
			return
		}

		// Handle stateful operations (check before examples to use real data)
		responseData, statusCode := handleStatefulOperation(matchedOp, path, method, r, state, spec, queryParams, pathItem)
		
		if responseData != nil {
			if statusCode == http.StatusNoContent || len(responseData) == 0 {
				// Empty response (e.g., 204 No Content for append)
				w.WriteHeader(http.StatusNoContent)
			} else {
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					// Use rate limit remaining if set, otherwise calculate from config
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1 // Assume one request used
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					// Capture response time before setting headers (if using responseTimeWriter)
					if rtw, ok := w.(*responseTimeWriter); ok && !rtw.written {
						// Calculate time now, before headers are set
						responseTimeMs := time.Since(rtw.startTime).Milliseconds()
						w.Header().Set("X-Internal-Response-Time-Ms", strconv.FormatInt(responseTimeMs, 10))
						path := w.Header().Get("X-Internal-Path")
						if path != "" && !strings.HasPrefix(path, "/health") && !strings.HasPrefix(path, "/rate-limits") {
							RecordResponseTime(responseTimeMs)
						}
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					// Capture response time before setting headers
					if rtw, ok := w.(*responseTimeWriter); ok && !rtw.written {
						responseTimeMs := time.Since(rtw.startTime).Milliseconds()
						w.Header().Set("X-Internal-Response-Time-Ms", strconv.FormatInt(responseTimeMs, 10))
						path := w.Header().Get("X-Internal-Path")
						if path != "" && !strings.HasPrefix(path, "/health") && !strings.HasPrefix(path, "/rate-limits") {
							RecordResponseTime(responseTimeMs)
						}
					}
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				if _, err := w.Write(responseData); err != nil {
					log.Printf("Error writing response: %v", err)
					return
				}
				// Track successful responses
				if statusCode >= 200 && statusCode < 300 {
					IncrementRequestsSuccess()
					// Track credit usage
					if server != nil && server.creditTracker != nil {
					// Track credit usage at the developer account level (matches real X API behavior)
					developerAccountID := getDeveloperAccountID(r, state)
					server.creditTracker.TrackUsage(developerAccountID, method, pathWithoutQuery, responseData, statusCode)
					}
				} else if statusCode >= 400 {
					IncrementRequestsError()
				}
			}
			return
		}

		// Check for example response (only if stateful handler didn't handle it)
		// Skip examples for streaming and search endpoints - they should use dedicated handlers
		// Also skip DELETE /2/connections/all - it has a dedicated handler
		// Check both with and without trailing slash
		if !strings.Contains(path, "/stream") && !strings.Contains(path, "/search/recent") && pathWithoutQuery != "/2/connections/all" && pathWithoutQuery != "/2/connections/all/" {
			example := examples.FindBestMatch(method, pathWithoutQuery)
			if example != nil {
			// Use example as template, but filter based on query params
			responseData := filterResponseByQueryParams(example.Response, queryParams, matchedOp)
			
			// CRITICAL: Remove errors from example responses - examples shouldn't include errors in successful responses
			// Example files may contain errors for testing, but we shouldn't return them for normal requests
			// Only return errors if this is actually an error response (4xx/5xx), not a 200 OK response
			// responseData is already map[string]interface{} from filterResponseByQueryParams
			if errors, hasErrors := responseData["errors"]; hasErrors {
				// Check if errors array has entries
				if errorsArray, ok := errors.([]interface{}); ok && len(errorsArray) > 0 {
					// Remove errors from successful responses - examples shouldn't pollute normal responses
					// Errors should only come from actual error conditions (validation, auth, rate limits, etc.)
					delete(responseData, "errors")
					log.Printf("DEBUG: Removed %d errors from example response for %s %s", len(errorsArray), method, pathWithoutQuery)
				}
			}
			
			responseJSON, err := json.MarshalIndent(responseData, "", "  ")
			if err == nil {
				// Set rate limit headers if available
				if activeRateLimitConfig != nil {
					// Use rate limit remaining if set, otherwise calculate from config
					remaining := rateLimitRemaining
					if remaining < 0 {
						remaining = activeRateLimitConfig.Limit - 1 // Assume one request used
					}
					resetTime := rateLimitResetTime
					if resetTime.IsZero() {
						resetTime = time.Now().Add(time.Duration(activeRateLimitConfig.WindowSec) * time.Second)
					}
					AddXAPIHeadersWithRateLimit(w, activeRateLimitConfig, remaining, resetTime)
				} else {
					AddXAPIHeaders(w)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write(responseJSON); err != nil {
					log.Printf("Error writing response: %v", err)
				}
				// Track credit usage for example responses
				if server != nil && server.creditTracker != nil {
					accountID := getAuthenticatedUserID(r, state)
					server.creditTracker.TrackUsage(accountID, method, pathWithoutQuery, responseJSON, http.StatusOK)
				}
				return
			}
		}
		}

		// Fall back to OpenAPI schema generation
		responseSchema := matchedOp.Operation.GetResponseSchema("200")
		if responseSchema == nil {
			responseSchema = matchedOp.Operation.GetResponseSchema("default")
		}

		if responseSchema != nil {
			
			responseData, err := GenerateResponseFromSchemaWithState(responseSchema, spec, queryParams, state)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, "Failed to generate response", 500)
				return
			}

			AddXAPIHeaders(w)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(responseData); err != nil {
				log.Printf("Error writing response: %v", err)
			}
			// Track credit usage for schema-generated responses
			if server != nil && server.creditTracker != nil {
				accountID := getAuthenticatedUserID(r, state)
				server.creditTracker.TrackUsage(accountID, method, pathWithoutQuery, responseData, http.StatusOK)
			}
		} else {
			// Fallback to generic response
			WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
				"data": map[string]interface{}{
					"message": fmt.Sprintf("Mock response for %s %s", method, path),
					"operation_id": matchedOp.Operation.OperationID,
				},
			})
		}
	}
}

// formatStateNilError returns a standard error response when state is nil
func formatStateNilError() ([]byte, int) {
	errorResp := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"message": "Internal server error",
				"code":    131,
				"title":   "Server Error",
				"type":    "https://api.twitter.com/2/problems/server-error",
			},
		},
		"title":  "Server Error",
		"detail": "Internal server error",
		"type":   "https://api.twitter.com/2/problems/server-error",
	}
	data, _ := MarshalJSONResponse(errorResp)
	return data, http.StatusInternalServerError
}

// handleStatefulOperation handles operations that need state management
// Returns (response data, status code) if handled, (nil, 0) otherwise
func handleStatefulOperation(op *EndpointOperation, path, method string, r *http.Request, state *State, spec *OpenAPISpec, queryParams *QueryParams, pathItem *PathItem) ([]byte, int) {
	// CRITICAL: All operations in this function require state - check first
	if state == nil {
		log.Printf("ERROR: state is nil for %s %s - this should never happen. State should be initialized at server startup.", method, path)
		return formatStateNilError()
	}
	
	// Normalize path once at the start
	normalizedPath := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	
	// DELETE /2/connections/all - handle FIRST before other handlers
	// Check both with and without trailing slash to be safe
	// Also check the raw path to catch any edge cases
	if method == "DELETE" {
		pathCheck := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/")
		if normalizedPath == "/2/connections/all" || normalizedPath == "/2/connections/all/" || 
		   pathCheck == "2/connections/all" || strings.HasSuffix(path, "/2/connections/all") || 
		   strings.HasSuffix(path, "/2/connections/all/") {
			// Get authenticated user ID and close all their streaming connections
			authenticatedUserID := getAuthenticatedUserID(r, state)
			closedCount := state.CloseAllStreamConnectionsForUser(authenticatedUserID)
			log.Printf("DELETE /2/connections/all: Closed %d stream connection(s) for user %s", closedCount, authenticatedUserID)
			
			// Return correct format matching real API: {"data": {"attempted": true}}
			// Real API returns 200 with {"data": {"attempted": true}} - no errors
			// Always return this format, don't try to use OpenAPI schema generation
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"attempted": true,
				},
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}
	
	// DELETE /2/users/{id}/blocking/{target_user_id} (OpenAPI path) - check FIRST
	if method == "DELETE" && strings.HasPrefix(normalizedPath, "/2/users/") && strings.Contains(normalizedPath, "/blocking/") {
		// Split on /blocking/ to separate user path from target user ID
		parts := strings.Split(normalizedPath, "/blocking/")
		if len(parts) == 2 {
			// parts[0] = "/2/users/0", parts[1] = "10"
			// Extract userID from /2/users/{id}
			userIDStr := strings.TrimPrefix(parts[0], "/2/users/")
			targetUserID := parts[1]
			if userIDStr != "" && targetUserID != "" {
				// Idempotent: return success even if not blocked
				state.UnblockUser(userIDStr, targetUserID)
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"blocking": false,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/users/{id}/muting/{target_user_id} (OpenAPI path) - check FIRST
	if method == "DELETE" && strings.HasPrefix(normalizedPath, "/2/users/") && strings.Contains(normalizedPath, "/muting/") {
		// Split on /muting/ to separate user path from target user ID
		parts := strings.Split(normalizedPath, "/muting/")
		if len(parts) == 2 {
			// parts[0] = "/2/users/0", parts[1] = "10"
			// Extract userID from /2/users/{id}
			userIDStr := strings.TrimPrefix(parts[0], "/2/users/")
			targetUserID := parts[1]
			if userIDStr != "" && targetUserID != "" {
				// Idempotent: return success even if not muted
				state.UnmuteUser(userIDStr, targetUserID)
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"muting": false,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}
	
	// POST /2/users/{id}/blocking (OpenAPI path) - check VERY FIRST
	if method == "POST" && strings.HasPrefix(normalizedPath, "/2/users/") && strings.HasSuffix(normalizedPath, "/blocking") {
		parts := strings.Split(normalizedPath, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/blocking")
			if userID != "" {
				var req struct {
					TargetUserID string `json:"target_user_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if req.TargetUserID == "" {
					errorResp := CreateValidationErrorResponse("target_user_id", "", "target_user_id field is required")
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				// Check if users exist
				sourceUser := state.GetUserByID(userID)
				targetUser := state.GetUserByID(req.TargetUserID)
				if sourceUser == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				if targetUser == nil {
					return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
				}
				
				if state.BlockUser(userID, req.TargetUserID) {
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"blocking": true,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already blocked - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"blocking": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// POST /2/users/{id}/muting (OpenAPI path) - check VERY FIRST
	if method == "POST" && strings.HasPrefix(normalizedPath, "/2/users/") && strings.HasSuffix(normalizedPath, "/muting") {
		parts := strings.Split(normalizedPath, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/muting")
			if userID != "" {
				// Validate that the path parameter user ID matches the authenticated user ID
				authenticatedUserID := getAuthenticatedUserID(r, state)
				if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
					return errorData, statusCode
				}
				
				var req struct {
					TargetUserID string `json:"target_user_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				if req.TargetUserID == "" {
					errorResp := CreateValidationErrorResponse("target_user_id", "", "target_user_id field is required")
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
				
				// Check if users exist
				sourceUser := state.GetUserByID(userID)
				targetUser := state.GetUserByID(req.TargetUserID)
				if sourceUser == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				if targetUser == nil {
					return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
				}
				
				if state.MuteUser(userID, req.TargetUserID) {
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"muting": true,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already muted - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"muting": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// POST /2/users/{id}/following (OpenAPI path) - check FIRST before any other handlers
	normalizedPathForFollowing := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	if method == "POST" && strings.Contains(normalizedPathForFollowing, "/users/") && strings.HasSuffix(normalizedPathForFollowing, "/following") {
		// Extract user ID from path like /2/users/0/following
		parts := strings.Split(normalizedPathForFollowing, "/users/")
		if len(parts) == 2 {
			userID := strings.Split(parts[1], "/following")[0]
			if userID != "" {
				// Validate that the path parameter user ID matches the authenticated user ID
				authenticatedUserID := getAuthenticatedUserID(r, state)
				if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
					return errorData, statusCode
				}
				
				var req struct {
					TargetUserID string `json:"target_user_id"`
				}
				// Read body and decode
				bodyBytes, err := io.ReadAll(r.Body)
				if err == nil {
					if err := json.Unmarshal(bodyBytes, &req); err == nil && req.TargetUserID != "" {
						// Check if users exist
						sourceUser := state.GetUserByID(userID)
						targetUser := state.GetUserByID(req.TargetUserID)
						if sourceUser == nil {
							return formatResourceNotFoundError("user", "id", userID), http.StatusOK
						}
						if targetUser == nil {
							return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
						}
						if state.FollowUser(userID, req.TargetUserID) {
							response := map[string]interface{}{
								"data": map[string]interface{}{
									"following":     true,
									"pending_follow": false,
								},
							}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
						} else {
							// FollowUser failed for other reasons (e.g., already following, same user)
							return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
						}
					}
				}
			}
		}
	}

	// GET /2/users/me
	if method == "GET" && path == "/2/users/me" {
		user := state.GetDefaultUser()
		if user != nil {
			return formatStateDataToOpenAPI(user, op, spec, queryParams, state), http.StatusOK
		}
	}

	// GET /2/users/search (must come before generic /2/users/{id} handler)
	if method == "GET" && path == "/2/users/search" {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		
		// Search through all users
		allUsers := state.GetAllUsers()
		var results []*User
		seenIDs := make(map[string]bool) // Track user IDs to prevent duplicates
		queryLower := strings.ToLower(query)
		
	userLoop:
		for _, user := range allUsers {
			// Check for context cancellation in long-running loops
			select {
			case <-r.Context().Done():
				// Client disconnected, return partial results
				break userLoop
			default:
			}
			
			if len(results) >= limit {
				break
			}
			
			// Skip if we've already added this user
			if seenIDs[user.ID] {
				continue
			}
			
			// Search in name, username, description
			if query == "" ||
			   strings.Contains(strings.ToLower(user.Name), queryLower) ||
			   strings.Contains(strings.ToLower(user.Username), queryLower) ||
			   strings.Contains(strings.ToLower(user.Description), queryLower) {
				results = append(results, user)
				seenIDs[user.ID] = true
			}
		}
		
		return formatStateDataToOpenAPI(results, op, spec, queryParams, state), http.StatusOK
	}

	// GET /2/users/personalized_trends (must come before generic /2/users/{id} handler)
	if method == "GET" && path == "/2/users/personalized_trends" {
		// Return personalized trending topics from seeded data
		trends := state.GetPersonalizedTrends()
		trendsData := make([]map[string]interface{}, len(trends))
		for i, trend := range trends {
			trendsData[i] = map[string]interface{}{
				"category":       trend.Category,
				"post_count":     trend.PostCount,
				"trend_name":     trend.TrendName,
				"trending_since": trend.TrendingSince,
			}
		}
		response := map[string]interface{}{
			"data": trendsData,
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/users/by/username/{username} (must come before generic /2/users/{id} handler)
	if method == "GET" && strings.HasPrefix(path, "/2/users/by/username/") {
		username := strings.TrimPrefix(path, "/2/users/by/username/")
		// Remove query parameters
		if idx := strings.Index(username, "?"); idx != -1 {
			username = username[:idx]
		}
		// Validate username format first (before checking existence)
		if err := ValidateUsername(username); err != nil {
			errorResp := FormatSingleValidationError(err)
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		user := state.GetUserByUsername(username)
		if user != nil {
			return formatStateDataToOpenAPI(user, op, spec, queryParams, state), http.StatusOK
		} else {
			// Return 200 OK with errors array (matching X API behavior)
			return formatResourceNotFoundError("user", "username", username), http.StatusOK
		}
	}

	// GET /2/users (multiple users by IDs)
	if method == "GET" && path == "/2/users" {
		idsParam := r.URL.Query().Get("ids")
		if idsParam != "" {
			ids := strings.Split(idsParam, ",")
			// Limit to maximum number of IDs
			if len(ids) > MaxQueryParameterIDs {
				errorResp := CreateValidationErrorResponse("ids", "", fmt.Sprintf("Maximum %d IDs allowed per request", MaxQueryParameterIDs))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			// Limit to 100 users (X API limit)
			if len(ids) > 100 {
				ids = ids[:100]
			}
			// Validate all IDs first
			var validationErrors []*ValidationError
			for i, id := range ids {
				// Check context cancellation every 50 iterations
				if i%50 == 0 {
					select {
					case <-r.Context().Done():
						errorResp := CreateValidationErrorResponse("ids", "", "Request cancelled")
						data, statusCode := MarshalJSONErrorResponse(errorResp)
						return data, statusCode
					default:
					}
				}
				id = strings.TrimSpace(id)
				if err := ValidateSnowflakeID(id); err != nil {
					validationErrors = append(validationErrors, err)
				}
			}
			if len(validationErrors) > 0 {
				errorResp := FormatValidationErrors(validationErrors)
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			users := state.GetUsers(ids)
			// Format as array response
			return formatStateDataToOpenAPI(users, op, spec, queryParams, state), http.StatusOK
		}
	}

	// GET /2/users/by (multiple users by usernames)
	if method == "GET" && path == "/2/users/by" {
		usernamesParam := r.URL.Query().Get("usernames")
		if usernamesParam != "" {
			usernames := strings.Split(usernamesParam, ",")
			// Limit to maximum number of usernames
			if len(usernames) > MaxQueryParameterIDs {
				errorResp := CreateValidationErrorResponse("usernames", "", fmt.Sprintf("Maximum %d usernames allowed per request", MaxQueryParameterIDs))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			// Validate all usernames first
			var validationErrors []*ValidationError
			for _, username := range usernames {
				username = strings.TrimSpace(username)
				if err := ValidateUsername(username); err != nil {
					validationErrors = append(validationErrors, err)
				}
			}
			if len(validationErrors) > 0 {
				errorResp := FormatValidationErrors(validationErrors)
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			var users []*User
			for _, username := range usernames {
				username = strings.TrimSpace(username)
				if user := state.GetUserByUsername(username); user != nil {
					users = append(users, user)
				}
			}
			// Format as array response
			return formatStateDataToOpenAPI(users, op, spec, queryParams, state), http.StatusOK
		}
	}

	// GET /2/users/reposts_of_me (must come before generic /2/users/{id} handler)
	if method == "GET" && path == "/2/users/reposts_of_me" {
		user := state.GetDefaultUser()
		if user != nil {
			// Get all tweets by this user (need to get fresh pointers from state)
			userTweets := state.GetTweets(user.Tweets)
			if len(userTweets) == 0 {
				// No tweets by this user, return empty result
				return formatStateDataToOpenAPI([]*Tweet{}, op, spec, queryParams, state), http.StatusOK
			}
			
			userTweetIDs := make(map[string]bool)
			for _, tweet := range userTweets {
				userTweetIDs[tweet.ID] = true
			}
			
			// Find all tweets that retweet any of the authenticated user's tweets
			// Check ReferencedTweets to find retweet tweet objects
			allTweets := state.GetAllTweets()
			retweetTweets := make([]*Tweet, 0)
			processedRetweets := make(map[string]bool) // Track to avoid duplicates
			
		tweetLoop:
			for i, tweet := range allTweets {
				// Check context cancellation every 100 iterations
				if i%100 == 0 {
					select {
					case <-r.Context().Done():
						// Client disconnected, return partial results
						break tweetLoop
					default:
					}
				}
				
				// Check if this tweet is a retweet of any of the user's tweets
				for _, ref := range tweet.ReferencedTweets {
					if ref.Type == "retweeted" && userTweetIDs[ref.ID] {
						if !processedRetweets[tweet.ID] {
							retweetTweets = append(retweetTweets, tweet)
							processedRetweets[tweet.ID] = true
						}
						break // Found a match, move to next tweet
					}
				}
			}
			
			// Also check RetweetedBy relationships as a fallback
			// Get fresh tweets from state to ensure we have the latest RetweetedBy data
			for _, tweetID := range user.Tweets {
				originalTweet := state.GetTweet(tweetID)
				if originalTweet != nil {
					for _, retweeterID := range originalTweet.RetweetedBy {
						// Check if we already have a retweet tweet object for this relationship
						foundRetweetTweet := false
						for _, rt := range retweetTweets {
							for _, ref := range rt.ReferencedTweets {
								if ref.Type == "retweeted" && ref.ID == originalTweet.ID && rt.AuthorID == retweeterID {
									foundRetweetTweet = true
									break
								}
							}
							if foundRetweetTweet {
								break
							}
						}
						
						// If no retweet tweet object exists, create one on the fly
						if !foundRetweetTweet {
							retweeter := state.GetUserByID(retweeterID)
							if retweeter != nil {
								// Create a retweet tweet object on the fly
								retweetID := state.generateID()
								retweet := &Tweet{
									ID:        retweetID,
									Text:      originalTweet.Text, // Retweets typically have same text
									AuthorID:  retweeterID,
									CreatedAt: originalTweet.CreatedAt.Add(time.Duration(rand.Intn(30)) * time.Hour), // Sometime after original
									EditHistoryTweetIDs: []string{retweetID},
									ReferencedTweets: []ReferencedTweet{
										{
											Type: "retweeted",
											ID:   originalTweet.ID,
										},
									},
									PublicMetrics: TweetMetrics{
										LikeCount:    rand.Intn(50),
										RetweetCount: 0,
										ReplyCount:   0,
										QuoteCount:   0,
									},
									Source: "Twitter Web App",
									Lang:   originalTweet.Lang,
									LikedBy:         make([]string, 0),
									RetweetedBy:     make([]string, 0),
									Replies:         make([]string, 0),
									Quotes:          make([]string, 0),
									Media:           make([]string, 0),
								}
								if !processedRetweets[retweetID] {
									retweetTweets = append(retweetTweets, retweet)
									processedRetweets[retweetID] = true
								}
							}
						}
					}
				}
			}
			
			// Sort by created_at descending (newest first)
			sort.Slice(retweetTweets, func(i, j int) bool {
				return retweetTweets[i].CreatedAt.After(retweetTweets[j].CreatedAt)
			})
			
			// Apply pagination
			limit := 10
			if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
				if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
					limit = parsed
				}
			}
			if len(retweetTweets) > limit {
				retweetTweets = retweetTweets[:limit]
			}
			
			return formatStateDataToOpenAPI(retweetTweets, op, spec, queryParams, state), http.StatusOK
		}
		// If no authenticated user, return empty result
		return formatStateDataToOpenAPI([]*Tweet{}, op, spec, queryParams, state), http.StatusOK
	}

	// GET /2/users/{id} (generic handler - must come after specific paths)
	if method == "GET" && strings.HasPrefix(path, "/2/users/") && path != "/2/users/me" && path != "/2/users/search" && path != "/2/users/personalized_trends" && path != "/2/users/reposts_of_me" && !strings.HasPrefix(path, "/2/users/by/username/") {
		userID := extractPathParam(path, "/2/users/")
		if userID != "" && userID != "by" {
			// Validate ID format first (before checking existence)
			if numericIDRegex.MatchString(userID) {
				if err := ValidateSnowflakeID(userID); err != nil {
					errorResp := FormatSingleValidationError(err)
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
			} else {
				// It's a username, validate format
				if err := ValidateUsername(userID); err != nil {
					errorResp := FormatSingleValidationError(err)
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
			}
			user := state.GetUserByID(userID)
			if user != nil {
				return formatStateDataToOpenAPI(user, op, spec, queryParams, state), http.StatusOK
			} else {
				// Try username lookup
				user = state.GetUserByUsername(userID)
				if user != nil {
					return formatStateDataToOpenAPI(user, op, spec, queryParams, state), http.StatusOK
				} else {
					// Return 200 OK with errors array (matching X API behavior)
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
			}
		}
	}


	// POST /2/tweets
	if method == "POST" && path == "/2/tweets" {
		// Body validation already happened above, so body should be restored
		// Read body again for processing
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil || len(bodyBytes) == 0 {
			// Empty or invalid body - return error (validation should have caught this, but double-check)
			errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Parse full request body to check for mutually exclusive parameters
		var reqBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", string(bodyBytes), "invalid JSON in request body")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Check for mutually exclusive parameters: poll, quote_tweet_id, direct_message_deep_link, media, card_uri
		// You can only provide one of these
		exclusiveParams := []string{"poll", "quote_tweet_id", "direct_message_deep_link", "media", "card_uri"}
		var providedParams []string
		paramValues := make(map[string]interface{})
		
		for _, param := range exclusiveParams {
			if val, exists := reqBody[param]; exists && val != nil {
				// Check if value is non-empty
				isEmpty := false
				switch v := val.(type) {
				case string:
					isEmpty = v == ""
				case map[string]interface{}:
					isEmpty = len(v) == 0
				case []interface{}:
					isEmpty = len(v) == 0
				}
				if !isEmpty {
					providedParams = append(providedParams, param)
					paramValues[param] = val
				}
			}
		}
		
		// If multiple mutually exclusive parameters are provided, return error
		if len(providedParams) > 1 {
			errorResp := CreateMutuallyExclusiveErrorResponse(
				paramValues,
				"You can only provide one of poll or quote_tweet_id or direct_message_deep_link or media or card_uri.",
			)
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Validate referenced resources exist (media IDs, user IDs, tweet IDs, etc.)
		// These are business logic validations not covered by OpenAPI spec
		
		// Validate media.media_ids if provided
		if mediaVal, exists := reqBody["media"]; exists {
			if mediaMap, ok := mediaVal.(map[string]interface{}); ok {
				if mediaIDsVal, hasMediaIDs := mediaMap["media_ids"]; hasMediaIDs {
					if mediaIDs, ok := mediaIDsVal.([]interface{}); ok {
						var invalidMediaIDs []string
						for _, idVal := range mediaIDs {
							if mediaID, ok := idValueToString(idVal); ok {
								if state.GetMedia(mediaID) == nil {
									invalidMediaIDs = append(invalidMediaIDs, mediaID)
								}
							}
						}
						if len(invalidMediaIDs) > 0 {
							// Format error matching real API format
							errorResp := CreateMutuallyExclusiveErrorResponse(
								map[string]interface{}{
									"media.media_ids": invalidMediaIDs,
								},
								"Your media IDs are invalid.",
							)
							data, statusCode := MarshalJSONErrorResponse(errorResp)
							return data, statusCode
						}
					}
				}
				
				// Validate tagged_user_ids if provided
				if taggedUsersVal, hasTaggedUsers := mediaMap["tagged_user_ids"]; hasTaggedUsers {
					if taggedUsers, ok := taggedUsersVal.([]interface{}); ok {
						var invalidUserIDs []string
						for _, idVal := range taggedUsers {
							if userID, ok := idValueToString(idVal); ok {
								if state.GetUserByID(userID) == nil {
									invalidUserIDs = append(invalidUserIDs, userID)
								}
							}
						}
						if len(invalidUserIDs) > 0 {
							errorResp := CreateMutuallyExclusiveErrorResponse(
								map[string]interface{}{
									"media.tagged_user_ids": invalidUserIDs,
								},
								"Your tagged user IDs are invalid.",
							)
							data, statusCode := MarshalJSONErrorResponse(errorResp)
							return data, statusCode
						}
					}
				}
			}
		}
		
		// Validate quote_tweet_id if provided
		if quoteTweetIDVal, exists := reqBody["quote_tweet_id"]; exists {
			if quoteTweetID, ok := idValueToString(quoteTweetIDVal); ok {
				if state.GetTweet(quoteTweetID) == nil {
					errorResp := CreateMutuallyExclusiveErrorResponse(
						map[string]interface{}{
							"quote_tweet_id": []string{quoteTweetID},
						},
						"Your quote tweet ID is invalid.",
					)
					data, statusCode := MarshalJSONErrorResponse(errorResp)
					return data, statusCode
				}
			}
		}
		
		// Validate reply.in_reply_to_tweet_id if provided
		if replyVal, exists := reqBody["reply"]; exists {
			if replyMap, ok := replyVal.(map[string]interface{}); ok {
				if replyToTweetIDVal, hasReplyTo := replyMap["in_reply_to_tweet_id"]; hasReplyTo {
					if replyToTweetID, ok := idValueToString(replyToTweetIDVal); ok {
						if state.GetTweet(replyToTweetID) == nil {
							errorResp := CreateMutuallyExclusiveErrorResponse(
								map[string]interface{}{
									"reply.in_reply_to_tweet_id": []string{replyToTweetID},
								},
								"Your reply tweet ID is invalid.",
							)
							data, statusCode := MarshalJSONErrorResponse(errorResp)
							return data, statusCode
						}
					}
				}
				
				// Validate reply.exclude_reply_user_ids if provided
				if excludeUsersVal, hasExclude := replyMap["exclude_reply_user_ids"]; hasExclude {
					if excludeUsers, ok := excludeUsersVal.([]interface{}); ok {
						var invalidUserIDs []string
						for _, idVal := range excludeUsers {
							if userID, ok := idValueToString(idVal); ok {
								if state.GetUserByID(userID) == nil {
									invalidUserIDs = append(invalidUserIDs, userID)
								}
							}
						}
						if len(invalidUserIDs) > 0 {
							errorResp := CreateMutuallyExclusiveErrorResponse(
								map[string]interface{}{
									"reply.exclude_reply_user_ids": invalidUserIDs,
								},
								"Your exclude reply user IDs are invalid.",
							)
							data, statusCode := MarshalJSONErrorResponse(errorResp)
							return data, statusCode
						}
					}
				}
			}
		}
		
		// Extract text field
		text, _ := reqBody["text"].(string)
		
		// Check if text field is present and non-empty
		if text == "" {
			errorResp := CreateValidationErrorResponse("text", "", "text field is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Validate tweet text length
		if len(text) > MaxTweetLength {
			errorResp := CreateValidationErrorResponse("text", text, fmt.Sprintf("text field exceeds maximum length of %d characters", MaxTweetLength))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Sanitize input before creating tweet
		sanitizedText := SanitizeInput(text)
		
		// Valid request - create tweet
		user := state.GetDefaultUser()
		if user == nil {
			errorResp := CreateValidationErrorResponse("authorization", "", "no authenticated user found")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		tweet := state.CreateTweet(sanitizedText, user.ID)
		if tweet == nil {
			errorResp := CreateValidationErrorResponse("server", "", "failed to create tweet")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		return formatStateDataToOpenAPI(tweet, op, spec, queryParams, state), http.StatusCreated
	}

	// GET /2/tweets/search/recent (must be before /2/tweets/{id} to avoid path collision)
	if method == "GET" && strings.HasPrefix(path, "/2/tweets/search/recent") {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 10 && parsed <= 100 {
				limit = parsed
			}
		}
		
		// Parse time filtering parameters
		sinceID := r.URL.Query().Get("since_id")
		untilID := r.URL.Query().Get("until_id")
		var startTime, endTime *time.Time
		if startTimeStr := r.URL.Query().Get("start_time"); startTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
				startTime = &t
			}
		}
		if endTimeStr := r.URL.Query().Get("end_time"); endTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
				endTime = &t
			}
		}
		
		tweets := state.SearchTweets(r.Context(), query, limit, sinceID, untilID, startTime, endTime)
		// Always return a response, even if empty (prevents falling through to examples)
		return formatSearchTweetsResponse(tweets, queryParams, state, spec, limit), http.StatusOK
	}

	// GET /2/tweets/search/all (full archive search - same as recent but no 7-day restriction)
	if method == "GET" && strings.HasPrefix(path, "/2/tweets/search/all") {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 10 && parsed <= 500 {
				limit = parsed // search/all allows up to 500 results
			}
		}
		
		// Parse time filtering parameters
		sinceID := r.URL.Query().Get("since_id")
		untilID := r.URL.Query().Get("until_id")
		var startTime, endTime *time.Time
		if startTimeStr := r.URL.Query().Get("start_time"); startTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
				startTime = &t
			}
		}
		if endTimeStr := r.URL.Query().Get("end_time"); endTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
				endTime = &t
			}
		}
		
		// search/all searches all tweets (no 7-day restriction like search/recent)
		// In playground, we search all seeded tweets
		tweets := state.SearchTweets(r.Context(), query, limit, sinceID, untilID, startTime, endTime)
		// Always return a response, even if empty (prevents falling through to examples)
		return formatSearchTweetsResponse(tweets, queryParams, state, spec, limit), http.StatusOK
	}

	// GET /2/tweets/counts/recent (must be before /2/tweets/{id} to avoid path collision)
	if method == "GET" && path == "/2/tweets/counts/recent" {
		query := r.URL.Query().Get("query")
		// Parse start_time and end_time (default to last 7 days for recent)
		endTime := time.Now()
		startTime := endTime.Add(-7 * 24 * time.Hour)

		if startTimeStr := r.URL.Query().Get("start_time"); startTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
				startTime = t
			}
		}
		if endTimeStr := r.URL.Query().Get("end_time"); endTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
				endTime = t
			}
		}

		// Round start_time down to the hour and end_time up to the hour
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), startTime.Hour(), 0, 0, 0, startTime.Location())
		endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), endTime.Hour(), 0, 0, 0, endTime.Location())

		// Get all tweets matching the query
		allTweets := state.GetAllTweets()
		queryLower := strings.ToLower(query)
		matchingTweets := make([]*Tweet, 0)

		for _, tweet := range allTweets {
			if query == "" || strings.Contains(strings.ToLower(tweet.Text), queryLower) {
				if tweet.CreatedAt.After(startTime) && tweet.CreatedAt.Before(endTime.Add(time.Hour)) {
					matchingTweets = append(matchingTweets, tweet)
				}
			}
		}

		// Create hourly buckets
		buckets := make([]map[string]interface{}, 0)
		current := startTime
		totalCount := 0

		for current.Before(endTime) {
			bucketEnd := current.Add(time.Hour)
			if bucketEnd.After(endTime) {
				bucketEnd = endTime
			}

			// Count tweets in this hour bucket
			bucketCount := 0
			for _, tweet := range matchingTweets {
				if tweet.CreatedAt.After(current.Add(-time.Second)) && tweet.CreatedAt.Before(bucketEnd) {
					bucketCount++
				}
			}
			totalCount += bucketCount

			buckets = append(buckets, map[string]interface{}{
				"start":       current.Format(time.RFC3339),
				"end":         bucketEnd.Format(time.RFC3339),
				"tweet_count": bucketCount,
			})

			current = bucketEnd
			// Break if we've reached endTime to avoid infinite loop
			if !current.Before(endTime) {
				break
			}
		}
		
		// Handle edge case where startTime == endTime (single hour bucket)
		if startTime.Equal(endTime) {
			bucketCount := 0
			for _, tweet := range matchingTweets {
				if tweet.CreatedAt.After(startTime.Add(-time.Second)) && tweet.CreatedAt.Before(endTime.Add(time.Second)) {
					bucketCount++
				}
			}
			totalCount += bucketCount
			
			buckets = append(buckets, map[string]interface{}{
				"start":       startTime.Format(time.RFC3339),
				"end":         endTime.Format(time.RFC3339),
				"tweet_count": bucketCount,
			})
		}
		
		response := map[string]interface{}{
			"data": buckets,
			"meta": map[string]interface{}{
				"total_tweet_count": totalCount,
			},
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/tweets/counts/all (must be before /2/tweets/{id} to avoid path collision)
	if method == "GET" && path == "/2/tweets/counts/all" {
		query := r.URL.Query().Get("query")
		// Parse start_time and end_time (required for counts/all)
		var startTime, endTime time.Time
		var hasStartTime, hasEndTime bool
		
		if startTimeStr := r.URL.Query().Get("start_time"); startTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
				startTime = t
				hasStartTime = true
			}
		}
		if endTimeStr := r.URL.Query().Get("end_time"); endTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
				endTime = t
				hasEndTime = true
			}
		}
		
		// If no times provided, use defaults (last 30 days)
		if !hasStartTime {
			endTime = time.Now()
			startTime = endTime.Add(-30 * 24 * time.Hour)
		} else if !hasEndTime {
			endTime = time.Now()
		}
		
		// Round start_time down to the hour and end_time up to the hour
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), startTime.Hour(), 0, 0, 0, startTime.Location())
		endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), endTime.Hour(), 0, 0, 0, endTime.Location())
		
		// Get all tweets matching the query
		allTweets := state.GetAllTweets()
		queryLower := strings.ToLower(query)
		matchingTweets := make([]*Tweet, 0)
		
		for _, tweet := range allTweets {
			if query == "" || strings.Contains(strings.ToLower(tweet.Text), queryLower) {
				if tweet.CreatedAt.After(startTime) && tweet.CreatedAt.Before(endTime.Add(time.Hour)) {
					matchingTweets = append(matchingTweets, tweet)
				}
			}
		}
		
		// Create hourly buckets
		buckets := make([]map[string]interface{}, 0)
		current := startTime
		totalCount := 0
		
		for current.Before(endTime) {
			bucketEnd := current.Add(time.Hour)
			if bucketEnd.After(endTime) {
				bucketEnd = endTime
			}
			
			// Count tweets in this hour bucket
			bucketCount := 0
			for _, tweet := range matchingTweets {
				if tweet.CreatedAt.After(current.Add(-time.Second)) && tweet.CreatedAt.Before(bucketEnd) {
					bucketCount++
				}
			}
			totalCount += bucketCount
			
			buckets = append(buckets, map[string]interface{}{
				"start":       current.Format(time.RFC3339),
				"end":         bucketEnd.Format(time.RFC3339),
				"tweet_count": bucketCount,
			})
			
			current = bucketEnd
			// Break if we've reached endTime to avoid infinite loop
			if !current.Before(endTime) {
				break
			}
		}
		
		// Handle edge case where startTime == endTime (single hour bucket)
		if startTime.Equal(endTime) {
			bucketCount := 0
			for _, tweet := range matchingTweets {
				if tweet.CreatedAt.After(startTime.Add(-time.Second)) && tweet.CreatedAt.Before(endTime.Add(time.Second)) {
					bucketCount++
				}
			}
			totalCount += bucketCount
			
			buckets = append(buckets, map[string]interface{}{
				"start":       startTime.Format(time.RFC3339),
				"end":         endTime.Format(time.RFC3339),
				"tweet_count": bucketCount,
			})
		}
		
		response := map[string]interface{}{
			"data": buckets,
			"meta": map[string]interface{}{
				"total_tweet_count": totalCount,
			},
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/tweets (multiple tweets by IDs)
	if method == "GET" && path == "/2/tweets" {
		idsParam := r.URL.Query().Get("ids")
		if idsParam != "" {
			ids := strings.Split(idsParam, ",")
			// Limit to maximum number of IDs
			if len(ids) > MaxQueryParameterIDs {
				errorResp := CreateValidationErrorResponse("ids", "", fmt.Sprintf("Maximum %d IDs allowed per request", MaxQueryParameterIDs))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			// Limit to 100 tweets (X API limit)
			if len(ids) > 100 {
				ids = ids[:100]
			}
			// Validate all IDs first
			var validationErrors []*ValidationError
			var invalidIds []string
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if err := ValidateSnowflakeID(id); err != nil {
					invalidIds = append(invalidIds, id)
					// Update parameter name to "ids" and message to match X API format
					validationErrors = append(validationErrors, &ValidationError{
						Parameter: "ids",
						Message:   fmt.Sprintf("The `ids` query parameter value [%s] is not valid", id),
						Value:     id,
					})
				}
			}
			if len(validationErrors) > 0 {
				// Format error response matching X API format exactly
				errorResp := map[string]interface{}{
					"errors": []map[string]interface{}{
						{
							"parameters": map[string]interface{}{
								"ids": invalidIds,
							},
							"message": fmt.Sprintf("The `ids` query parameter value [%s] is not valid", invalidIds[0]),
						},
					},
					"title":  "Invalid Request",
					"detail": "One or more parameters to your request was invalid.",
					"type":   "https://api.twitter.com/2/problems/invalid-request",
				}
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			tweets := state.GetTweets(ids)
			// Format as array response
			return formatStateDataToOpenAPI(tweets, op, spec, queryParams, state), http.StatusOK
		}
	}

	// GET /2/tweets/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/tweets/") {
		// Exclude search, streaming, counts, analytics, and relationship endpoints
		if strings.HasPrefix(path, "/2/tweets/search") || 
		   strings.Contains(path, "/stream") ||
		   strings.Contains(path, "/sample") ||
		   strings.Contains(path, "/firehose") ||
		   strings.Contains(path, "/counts") ||
		   strings.Contains(path, "/analytics") ||
		   strings.HasSuffix(path, "/liking_users") ||
		   strings.HasSuffix(path, "/retweeted_by") ||
		   strings.HasSuffix(path, "/quote_tweets") ||
		   strings.HasSuffix(path, "/retweets") {
			// Already handled above or by relationship handlers, skip
		} else {
			tweetID := extractPathParam(path, "/2/tweets/")
			if tweetID != "" {
				// Validate ID format first (before checking existence)
				if err := ValidateSnowflakeID(tweetID); err != nil {
					errorResp := FormatSingleValidationError(err)
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				tweet := state.GetTweet(tweetID)
				if tweet != nil {
					return formatStateDataToOpenAPI(tweet, op, spec, queryParams, state), http.StatusOK
				} else {
					// Return 200 OK with errors array (matching X API behavior)
					return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
				}
			}
		}
	}

	// DELETE /2/tweets/{id}
	if method == "DELETE" && strings.HasPrefix(path, "/2/tweets/") {
		tweetID := extractPathParam(path, "/2/tweets/")
		if tweetID != "" {
			// Validate ID format first (before checking existence)
			if err := ValidateSnowflakeID(tweetID); err != nil {
				errorResp := FormatSingleValidationError(err)
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			
			// Get the tweet first to check ownership
			tweet := state.GetTweet(tweetID)
			if tweet == nil {
				return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusNotFound
			}
			
			// Check if authenticated user is the author
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if tweet.AuthorID != authenticatedUserID {
				// User is not authorized to delete this tweet - return 403 error matching real API format
				errorResponse := map[string]interface{}{
					"detail": "You are not authorized to delete this Tweet.",
					"type":   "about:blank",
					"title":  "Forbidden",
					"status": 403,
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 403 error response: %v", err)
					fallback := map[string]interface{}{
						"detail": "You are not authorized to delete this Tweet.",
						"type":   "about:blank",
						"title":  "Forbidden",
						"status": 403,
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusForbidden
				}
				return data, http.StatusForbidden
			}
			
			// User is the author, proceed with deletion
			if state.DeleteTweet(tweetID) {
				response := map[string]interface{}{
					"data": map[string]bool{"deleted": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusNotFound
			}
		}
	}


	// POST /2/media/upload/initialize
	if method == "POST" && path == "/2/media/upload/initialize" {
		var req struct {
			TotalBytes    int64  `json:"total_bytes"`
			MediaType     string `json:"media_type"`
			MediaCategory string `json:"media_category"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			// Media keys must match pattern: ^([0-9]+)_([0-9]+)$ (e.g., "123_456")
			// Use timestamp components to create valid format
			now := time.Now().Unix()
			mediaKey := fmt.Sprintf("%d_%d", now/1000, now%1000)
			media := state.CreateMedia(mediaKey, 3600)
			return formatStateDataToOpenAPI(media, op, spec, queryParams, state), http.StatusOK
		}
	}

	// POST /2/media/upload/{id}/append
	if method == "POST" && strings.Contains(path, "/2/media/upload/") && strings.Contains(path, "/append") {
		mediaID := extractMediaIDFromPath(path)
		if mediaID != "" {
			media := state.GetMedia(mediaID)
			if media != nil {
				// Just accept the append - return empty response for 204 No Content
				return []byte(""), http.StatusNoContent
			}
		}
	}

	// POST /2/media/upload/{id}/finalize
	if method == "POST" && strings.Contains(path, "/2/media/upload/") && strings.Contains(path, "/finalize") {
		mediaID := extractMediaIDFromPath(path)
		if mediaID != "" {
			media := state.GetMedia(mediaID)
			if media != nil {
				processingInfo := &ProcessingInfo{
					State:           "processing",
					CheckAfterSecs:  1,
					ProgressPercent: 50,
				}
				state.UpdateMediaState(mediaID, "processing", processingInfo)
				return formatStateDataToOpenAPI(media, op, spec, queryParams, state), http.StatusOK
			}
		}
	}

	// GET /2/media/upload?command=STATUS&media_id={id}
	if method == "GET" && strings.HasPrefix(path, "/2/media/upload") {
		mediaID := r.URL.Query().Get("media_id")
		if mediaID != "" {
			media := state.GetMedia(mediaID)
			if media != nil {
				// Simulate processing progression
				if media.State == "processing" && media.ProcessingInfo != nil {
					if media.ProcessingInfo.ProgressPercent >= 100 {
						state.UpdateMediaState(mediaID, "succeeded", nil)
					} else {
						media.ProcessingInfo.ProgressPercent += 25
						if media.ProcessingInfo.ProgressPercent > 100 {
							media.ProcessingInfo.ProgressPercent = 100
						}
					}
				}
				return formatStateDataToOpenAPI(media, op, spec, queryParams, state), http.StatusOK
			} else {
				// Media not found - return error matching real API format
				errorResponse := map[string]interface{}{
					"errors": []map[string]interface{}{
						{
							"message": "BadRequest: Not found",
						},
					},
					"title":   "Invalid Request",
					"detail":  "One or more parameters to your request was invalid.",
					"type":    "https://api.twitter.com/2/problems/invalid-request",
				}
				data, _ := json.Marshal(errorResponse)
				return data, http.StatusBadRequest
			}
		}
	}

	// GET /2/lists (multiple lists by IDs)
	if method == "GET" && path == "/2/lists" {
		idsParam := r.URL.Query().Get("ids")
		if idsParam != "" {
			ids := strings.Split(idsParam, ",")
			// Limit to maximum number of IDs
			if len(ids) > MaxQueryParameterIDs {
				errorResp := CreateValidationErrorResponse("ids", "", fmt.Sprintf("Maximum %d IDs allowed per request", MaxQueryParameterIDs))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			// Limit to 100 lists (X API limit)
			if len(ids) > 100 {
				ids = ids[:100]
			}
			lists := state.GetLists(ids)
			return formatStateDataToOpenAPI(lists, op, spec, queryParams, state), http.StatusOK
		}
	}

	// POST /2/lists
	if method == "POST" && normalizedPath == "/2/lists" {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Private     bool   `json:"private,omitempty"`
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if len(bodyBytes) == 0 {
			errorResp := CreateValidationErrorResponse("name", "", "request body is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if req.Name == "" {
			errorResp := CreateValidationErrorResponse("name", "", "name field is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Validate list name length
		if len(req.Name) > MaxListNameLength {
			errorResp := CreateValidationErrorResponse("name", req.Name, fmt.Sprintf("name field exceeds maximum length of %d characters", MaxListNameLength))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Validate list description length if provided
		if req.Description != "" && len(req.Description) > MaxListDescriptionLength {
			errorResp := CreateValidationErrorResponse("description", req.Description, fmt.Sprintf("description field exceeds maximum length of %d characters", MaxListDescriptionLength))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Get user ID first (releases read lock before CreateList tries to acquire write lock)
		user := state.GetDefaultUser()
		if user == nil {
			return formatResourceNotFoundError("user", "id", "0"), http.StatusOK
		}
		ownerID := user.ID // Get ID while read lock is held, then lock is released
		
		// Sanitize input
		sanitizedName := SanitizeInput(req.Name)
		sanitizedDescription := SanitizeInput(req.Description)
		
		// Now create list (write lock can be acquired)
		list := state.CreateList(sanitizedName, sanitizedDescription, ownerID, req.Private)
		
		// Format list with field filtering
		listMap := formatList(list)
		// Always apply field filtering - if no fields specified, returns only defaults (id, name)
		requestedFields := []string{}
		if queryParams != nil && len(queryParams.ListFields) > 0 {
			requestedFields = queryParams.ListFields
		}
		listMap = filterListFields(listMap, requestedFields)
		response := map[string]interface{}{
			"data": listMap,
		}
		jsonData, _ := json.MarshalIndent(response, "", "  ")
		return jsonData, http.StatusCreated
	}

	// GET /2/lists/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/lists/") {
		listID := extractPathParam(path, "/2/lists/")
		if listID != "" && !strings.Contains(listID, "/") {
			list := state.GetList(listID)
			if list != nil {
				return formatStateDataToOpenAPI(list, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("list", "id", listID), http.StatusOK
			}
		}
	}

	// PUT /2/lists/{id}
	if method == "PUT" && strings.HasPrefix(path, "/2/lists/") {
		listID := extractPathParam(path, "/2/lists/")
		if listID != "" && !strings.Contains(listID, "/") {
			var req struct {
				Name        string `json:"name,omitempty"`
				Description string `json:"description,omitempty"`
				Private     *bool  `json:"private,omitempty"`
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Validate list name length if provided
			if req.Name != "" && len(req.Name) > MaxListNameLength {
				errorResp := CreateValidationErrorResponse("name", req.Name, fmt.Sprintf("name field exceeds maximum length of %d characters", MaxListNameLength))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			
			// Validate list description length if provided
			if req.Description != "" && len(req.Description) > MaxListDescriptionLength {
				errorResp := CreateValidationErrorResponse("description", req.Description, fmt.Sprintf("description field exceeds maximum length of %d characters", MaxListDescriptionLength))
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			
			// Check if list exists
			list := state.GetList(listID)
			if list == nil {
				// List doesn't exist - return 400 error matching real API format
				errorResponse := map[string]interface{}{
					"errors": []map[string]interface{}{
						{
							"parameters": map[string]interface{}{
								"id": []string{listID},
							},
							"message": "You cannot update a List that does not exist.",
						},
					},
					"title":  "Invalid Request",
					"detail": "One or more parameters to your request was invalid.",
					"type":   "https://api.twitter.com/2/problems/invalid-request",
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 400 error response: %v", err)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"parameters": map[string]interface{}{
									"id": []string{listID},
								},
								"message": "You cannot update a List that does not exist.",
							},
						},
						"title":  "Invalid Request",
						"detail": "One or more parameters to your request was invalid.",
						"type":   "https://api.twitter.com/2/problems/invalid-request",
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusBadRequest
				}
				return data, http.StatusBadRequest
			}
			
			// Check if authenticated user is the owner
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if list.OwnerID != authenticatedUserID {
				// User is not allowed to update this list - return 403 error matching real API format
				errorResponse := map[string]interface{}{
					"detail": "You are not allowed to update this List.",
					"type":   "about:blank",
					"title":  "Forbidden",
					"status": 403,
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 403 error response: %v", err)
					fallback := map[string]interface{}{
						"detail": "You are not allowed to update this List.",
						"type":   "about:blank",
						"title":  "Forbidden",
						"status": 403,
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusForbidden
				}
				return data, http.StatusForbidden
			}
			
			// Sanitize input
			sanitizedName := ""
			if req.Name != "" {
				sanitizedName = SanitizeInput(req.Name)
			}
			sanitizedDescription := ""
			if req.Description != "" {
				sanitizedDescription = SanitizeInput(req.Description)
			}
			
			if state.UpdateList(listID, sanitizedName, sanitizedDescription, req.Private) {
				updatedList := state.GetList(listID)
				return formatStateDataToOpenAPI(updatedList, op, spec, queryParams, state), http.StatusOK
			} else {
				// This shouldn't happen since we already checked the list exists, but handle it anyway
				errorResponse := map[string]interface{}{
					"errors": []map[string]interface{}{
						{
							"parameters": map[string]interface{}{
								"id": []string{listID},
							},
							"message": "You cannot update a List that does not exist.",
						},
					},
					"title":  "Invalid Request",
					"detail": "One or more parameters to your request was invalid.",
					"type":   "https://api.twitter.com/2/problems/invalid-request",
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 400 error response: %v", err)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"parameters": map[string]interface{}{
									"id": []string{listID},
								},
								"message": "You cannot update a List that does not exist.",
							},
						},
						"title":  "Invalid Request",
						"detail": "One or more parameters to your request was invalid.",
						"type":   "https://api.twitter.com/2/problems/invalid-request",
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusBadRequest
				}
				return data, http.StatusBadRequest
			}
		}
	}

	// DELETE /2/lists/{id}
	if method == "DELETE" && strings.HasPrefix(path, "/2/lists/") {
		listID := extractPathParam(path, "/2/lists/")
		if listID != "" && !strings.Contains(listID, "/") {
			// Get the list first to check ownership
			list := state.GetList(listID)
			if list == nil {
				return formatResourceNotFoundError("list", "id", listID), http.StatusNotFound
			}
			
			// Check if authenticated user is the owner
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if list.OwnerID != authenticatedUserID {
				// User is not allowed to delete this list - return 403 error matching real API format
				errorResponse := map[string]interface{}{
					"detail": "You are not allowed to delete this List.",
					"type":   "about:blank",
					"title":  "Forbidden",
					"status": 403,
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 403 error response: %v", err)
					fallback := map[string]interface{}{
						"detail": "You are not allowed to delete this List.",
						"type":   "about:blank",
						"title":  "Forbidden",
						"status": 403,
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusForbidden
				}
				return data, http.StatusForbidden
			}
			
			// User is the owner, proceed with deletion
			if state.DeleteList(listID) {
				response := map[string]interface{}{
					"data": map[string]bool{"deleted": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("list", "id", listID), http.StatusNotFound
			}
		}
	}

	// POST /2/users/{id}/likes
	normalizedPathForLikes := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	if method == "POST" && strings.Contains(normalizedPathForLikes, "/users/") && strings.HasSuffix(normalizedPathForLikes, "/likes") {
		parts := strings.Split(normalizedPathForLikes, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/likes")
			if userID != "" {
				// Validate that the path parameter user ID matches the authenticated user ID
				authenticatedUserID := getAuthenticatedUserID(r, state)
				if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
					return errorData, statusCode
				}
				
				var req struct {
					TweetID string `json:"tweet_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if req.TweetID == "" {
					errorResp := CreateValidationErrorResponse("tweet_id", "", "tweet_id field is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				// Check if tweet and user exist
				tweet := state.GetTweet(req.TweetID)
				user := state.GetUserByID(userID)
				if tweet == nil {
					return formatResourceNotFoundError("tweet", "id", req.TweetID), http.StatusOK
				}
				if user == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				
				if state.LikeTweet(userID, req.TweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"liked": true},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already liked - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"liked": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/users/{id}/likes/{tweet_id}
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/likes/") {
		parts := strings.Split(path, "/likes/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			tweetID := parts[1]
			if userID != "" && tweetID != "" {
				if state.UnlikeTweet(userID, tweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"liked": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if tweet or user doesn't exist
					tweet := state.GetTweet(tweetID)
					user := state.GetUserByID(userID)
					if tweet == nil {
						return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
					}
					if user == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					// Already unliked - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"liked": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// POST /2/users/{id}/retweets
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/retweets") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/retweets")
		if userID != "" {
			// Validate that the path parameter user ID matches the authenticated user ID
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
				return errorData, statusCode
			}
			
			var req struct {
				TweetID string `json:"tweet_id"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if req.TweetID == "" {
				errorResp := CreateValidationErrorResponse("tweet_id", "", "tweet_id field is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if tweet and user exist
			tweet := state.GetTweet(req.TweetID)
			user := state.GetUserByID(userID)
			if tweet == nil {
				return formatResourceNotFoundError("tweet", "id", req.TweetID), http.StatusOK
			}
			if user == nil {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
			
			if state.Retweet(userID, req.TweetID) {
				response := map[string]interface{}{
					"data": map[string]bool{"retweeted": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already retweeted - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"retweeted": true},
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// DELETE /2/users/{id}/retweets/{source_tweet_id}
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/retweets/") {
		parts := strings.Split(path, "/retweets/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			tweetID := parts[1]
			if userID != "" && tweetID != "" {
				if state.Unretweet(userID, tweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"retweeted": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if tweet or user doesn't exist
					tweet := state.GetTweet(tweetID)
					user := state.GetUserByID(userID)
					if tweet == nil {
						return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
					}
					if user == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					// Already unretweeted - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"retweeted": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// POST /2/users/{id}/follows (alternative path)
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/follows") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/follows")
		if userID != "" {
			var req struct {
				TargetUserID string `json:"target_user_id"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if req.TargetUserID == "" {
				errorResp := CreateValidationErrorResponse("target_user_id", "", "target_user_id field is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if users exist
			sourceUser := state.GetUserByID(userID)
			targetUser := state.GetUserByID(req.TargetUserID)
			if sourceUser == nil {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
			if targetUser == nil {
				return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
			}
			
			if state.FollowUser(userID, req.TargetUserID) {
				response := map[string]interface{}{
					"data": map[string]bool{"following": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already following - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"following": true},
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// DELETE /2/users/{source_user_id}/following/{target_user_id} (OpenAPI path)
	normalizedPathForUnfollow := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	if method == "DELETE" && strings.Contains(normalizedPathForUnfollow, "/users/") && strings.Contains(normalizedPathForUnfollow, "/following/") {
		parts := strings.Split(normalizedPathForUnfollow, "/following/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			targetUserID := parts[1]
			if userID != "" && targetUserID != "" {
				if state.UnfollowUser(userID, targetUserID) {
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"following": false,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if users exist
					sourceUser := state.GetUserByID(userID)
					targetUser := state.GetUserByID(targetUserID)
					if sourceUser == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					if targetUser == nil {
						return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
					}
					// Already unfollowed - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"following": false,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// DELETE /2/users/{id}/follows/{target_user_id} (alternative path)
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/follows/") {
		parts := strings.Split(path, "/follows/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			targetUserID := parts[1]
			if userID != "" && targetUserID != "" {
				if state.UnfollowUser(userID, targetUserID) {
					response := map[string]interface{}{
						"data": map[string]bool{"following": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if users exist
					sourceUser := state.GetUserByID(userID)
					targetUser := state.GetUserByID(targetUserID)
					if sourceUser == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					if targetUser == nil {
						return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
					}
					// Already unfollowed - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"following": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}


	// POST /2/users/{id}/blocks (alternative path)
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/blocks") {
		normalizedPathForBlocks := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
		parts := strings.Split(normalizedPathForBlocks, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/blocks")
			if userID != "" {
				var req struct {
					TargetUserID string `json:"target_user_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if req.TargetUserID == "" {
					errorResp := CreateValidationErrorResponse("target_user_id", "", "target_user_id field is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				// Check if users exist
				sourceUser := state.GetUserByID(userID)
				targetUser := state.GetUserByID(req.TargetUserID)
				if sourceUser == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				if targetUser == nil {
					return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
				}
				
				if state.BlockUser(userID, req.TargetUserID) {
					response := map[string]interface{}{
						"data": map[string]bool{"blocking": true},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already blocked - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"blocking": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/users/{id}/blocks/{target_user_id} (alternative path)
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/blocks/") {
		parts := strings.Split(path, "/blocks/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			targetUserID := strings.Split(parts[1], "?")[0] // Remove query params
			if userID != "" && targetUserID != "" {
				if state.UnblockUser(userID, targetUserID) {
					response := map[string]interface{}{
						"data": map[string]bool{"blocking": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if users exist
					sourceUser := state.GetUserByID(userID)
					targetUser := state.GetUserByID(targetUserID)
					if sourceUser == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					if targetUser == nil {
						return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
					}
					// Already unblocked - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"blocking": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}


	// POST /2/users/{id}/mutes (alternative path)
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/mutes") {
		normalizedPathForMutes := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
		parts := strings.Split(normalizedPathForMutes, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/mutes")
			if userID != "" {
				var req struct {
					TargetUserID string `json:"target_user_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if req.TargetUserID == "" {
					errorResp := CreateValidationErrorResponse("target_user_id", "", "target_user_id field is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				// Check if users exist
				sourceUser := state.GetUserByID(userID)
				targetUser := state.GetUserByID(req.TargetUserID)
				if sourceUser == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				if targetUser == nil {
					return formatResourceNotFoundError("user", "id", req.TargetUserID), http.StatusOK
				}
				
				if state.MuteUser(userID, req.TargetUserID) {
					response := map[string]interface{}{
						"data": map[string]bool{"muting": true},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already muted - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"muting": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/users/{id}/muting/{target_user_id} (OpenAPI path)
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/muting/") {
		parts := strings.Split(path, "/muting/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			targetUserID := strings.Split(parts[1], "?")[0] // Remove query params
			if userID != "" && targetUserID != "" {
				if state.UnmuteUser(userID, targetUserID) {
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"muting": false,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if users exist
					sourceUser := state.GetUserByID(userID)
					targetUser := state.GetUserByID(targetUserID)
					if sourceUser == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					if targetUser == nil {
						return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
					}
					// Already unmuted - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]interface{}{
							"muting": false,
						},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// DELETE /2/users/{id}/mutes/{target_user_id} (alternative path)
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/mutes/") {
		parts := strings.Split(path, "/mutes/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			targetUserID := strings.Split(parts[1], "?")[0] // Remove query params
			if userID != "" && targetUserID != "" {
				if state.UnmuteUser(userID, targetUserID) {
					response := map[string]interface{}{
						"data": map[string]bool{"muting": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if users exist
					sourceUser := state.GetUserByID(userID)
					targetUser := state.GetUserByID(targetUserID)
					if sourceUser == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					if targetUser == nil {
						return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
					}
					// Already unmuted - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"muting": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// POST /2/users/{id}/bookmarks
	normalizedPathForBookmarks := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	if method == "POST" && strings.Contains(normalizedPathForBookmarks, "/users/") && strings.HasSuffix(normalizedPathForBookmarks, "/bookmarks") {
		parts := strings.Split(normalizedPathForBookmarks, "/users/")
		if len(parts) == 2 {
			userID := strings.TrimSuffix(parts[1], "/bookmarks")
			if userID != "" {
				// Validate that the path parameter user ID matches the authenticated user ID
				authenticatedUserID := getAuthenticatedUserID(r, state)
				if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
					return errorData, statusCode
				}
				
				var req struct {
					TweetID string `json:"tweet_id"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if len(bodyBytes) == 0 {
					errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				if req.TweetID == "" {
					errorResp := CreateValidationErrorResponse("tweet_id", "", "tweet_id field is required")
					data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
				}
				
				// Check if tweet and user exist
				tweet := state.GetTweet(req.TweetID)
				user := state.GetUserByID(userID)
				if tweet == nil {
					return formatResourceNotFoundError("tweet", "id", req.TweetID), http.StatusOK
				}
				if user == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				
				if state.BookmarkTweet(userID, req.TweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"bookmarked": true},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already bookmarked - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"bookmarked": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/users/{id}/bookmarks/{tweet_id}
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/bookmarks/") {
		parts := strings.Split(path, "/bookmarks/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			tweetID := parts[1]
			if userID != "" && tweetID != "" {
				if state.UnbookmarkTweet(userID, tweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"bookmarked": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Check if tweet or user doesn't exist
					tweet := state.GetTweet(tweetID)
					user := state.GetUserByID(userID)
					if tweet == nil {
						return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
					}
					if user == nil {
						return formatResourceNotFoundError("user", "id", userID), http.StatusOK
					}
					// Already unbookmarked - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"bookmarked": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// POST /2/spaces
	if method == "POST" && path == "/2/spaces" {
		var req struct {
			Title          string `json:"title"`
			ScheduledStart string `json:"scheduled_start,omitempty"`
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if len(bodyBytes) == 0 {
			errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if req.Title == "" {
			errorResp := CreateValidationErrorResponse("title", "", "title field is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Validate space title length
		if len(req.Title) > MaxSpaceTitleLength {
			errorResp := CreateValidationErrorResponse("title", req.Title, fmt.Sprintf("title field exceeds maximum length of %d characters", MaxSpaceTitleLength))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Sanitize input
		sanitizedTitle := SanitizeInput(req.Title)
		
		user := state.GetDefaultUser()
		if user != nil {
			var scheduledStart time.Time
			if req.ScheduledStart != "" {
				if t, err := time.Parse(time.RFC3339, req.ScheduledStart); err == nil {
					scheduledStart = t
				} else {
					scheduledStart = time.Now().Add(1 * time.Hour)
				}
			} else {
				scheduledStart = time.Now().Add(1 * time.Hour)
			}
			space := state.CreateSpace(sanitizedTitle, user.ID, scheduledStart)
			return formatStateDataToOpenAPI(space, op, spec, queryParams, state), http.StatusCreated
		}
	}


	// GET /2/spaces/by/creator_ids
	if method == "GET" && strings.HasPrefix(path, "/2/spaces/by/creator_ids") {
		creatorIDsParam := r.URL.Query().Get("user_ids")
		if creatorIDsParam != "" {
			creatorIDs := strings.Split(creatorIDsParam, ",")
			var allSpaces []*Space
			for _, creatorID := range creatorIDs {
				creatorID = strings.TrimSpace(creatorID)
				spaces := state.GetSpacesByCreator(creatorID)
				allSpaces = append(allSpaces, spaces...)
			}
			// Format as array response
			spacesData := make([]interface{}, len(allSpaces))
			for i, space := range allSpaces {
				spacesData[i] = formatSpace(space)
			}
			response := map[string]interface{}{
				"data": spacesData,
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// GET /2/spaces/search (must be before /2/spaces/{id} handler)
	if method == "GET" && strings.HasPrefix(path, "/2/spaces/search") {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		
		spaces := state.SearchSpaces(query, limit)
		// Format spaces response
		spacesData := make([]interface{}, len(spaces))
		for i, space := range spaces {
			spacesData[i] = formatSpace(space)
		}
		response := map[string]interface{}{
			"meta": map[string]interface{}{
				"result_count": len(spacesData),
			},
		}
		// Only include "data" field if there are results (matching real API behavior)
		if len(spacesData) > 0 {
			response["data"] = spacesData
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/spaces/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/spaces/") {
		spaceID := extractPathParam(path, "/2/spaces/")
		// Exclude "search" and other special paths
		if spaceID != "" && !strings.Contains(spaceID, "/") && spaceID != "search" && !strings.HasPrefix(spaceID, "search") {
			space := state.GetSpace(spaceID)
			if space != nil {
				return formatStateDataToOpenAPI(space, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("space", "id", spaceID), http.StatusOK
			}
		}
	}

	// GET /2/spaces/{id}/tweets
	if method == "GET" && strings.HasPrefix(path, "/2/spaces/") && strings.HasSuffix(path, "/tweets") {
		spaceID := extractPathParam(path, "/2/spaces/")
		spaceID = strings.TrimSuffix(spaceID, "/tweets")
		if spaceID != "" {
			space := state.GetSpace(spaceID)
			if space != nil {
				tweets := state.GetSpaceTweets(spaceID)
				// If no tweets, return only meta (matching real API behavior)
				if len(tweets) == 0 {
					response := map[string]interface{}{
						"meta": map[string]interface{}{
							"result_count": 0,
						},
					}
					data, _ := MarshalJSONResponse(response)
					return data, http.StatusOK
				}
				// Use formatStateDataToOpenAPI to handle field filtering and query params properly
				return formatStateDataToOpenAPI(tweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("space", "id", spaceID), http.StatusOK
			}
		}
	}

	// PUT /2/spaces/{id}
	if method == "PUT" && strings.HasPrefix(path, "/2/spaces/") {
		spaceID := extractPathParam(path, "/2/spaces/")
		// Exclude "search" and "by" paths
		if spaceID != "" && !strings.Contains(spaceID, "/") && spaceID != "search" && !strings.HasPrefix(spaceID, "search") && !strings.HasPrefix(spaceID, "by") {
			var req struct {
				Title string `json:"title,omitempty"`
				State string `json:"state,omitempty"`
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if state.UpdateSpace(spaceID, req.Title, req.State) {
				space := state.GetSpace(spaceID)
				return formatStateDataToOpenAPI(space, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("space", "id", spaceID), http.StatusOK
			}
		}
	}

	// POST /2/lists/{id}/members
	if method == "POST" && strings.Contains(path, "/lists/") && strings.Contains(path, "/members") && !strings.Contains(path, "/members/") {
		listID := extractPathParam(path, "/2/lists/")
		listID = strings.TrimSuffix(listID, "/members")
		if listID != "" {
			var req struct {
				UserID string `json:"user_id"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if req.UserID == "" {
				errorResp := CreateValidationErrorResponse("user_id", "", "user_id field is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if list exists
			list := state.GetList(listID)
			if list == nil {
				return formatResourceNotFoundError("list", "id", listID), http.StatusNotFound
			}
			
			// Check if authenticated user is the owner
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if list.OwnerID != authenticatedUserID {
				// User is not allowed to add members to this list - return 403 error matching real API format
				errorResponse := map[string]interface{}{
					"detail": "You are not allowed to add members to this List.",
					"type":   "about:blank",
					"title":  "Forbidden",
					"status": 403,
				}
				data, err := json.Marshal(errorResponse)
				if err != nil {
					log.Printf("Error marshaling 403 error response: %v", err)
					fallback := map[string]interface{}{
						"detail": "You are not allowed to add members to this List.",
						"type":   "about:blank",
						"title":  "Forbidden",
						"status": 403,
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusForbidden
				}
				return data, http.StatusForbidden
			}
			
			// Check if user exists
			user := state.GetUserByID(req.UserID)
			if user == nil {
				return formatResourceNotFoundError("user", "id", req.UserID), http.StatusNotFound
			}
			
			if state.AddListMember(listID, req.UserID) {
				response := map[string]interface{}{
					"data": map[string]bool{"is_member": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			
			// If AddListMember returns false but list and user exist, it means they're already a member
			// Return success anyway (idempotent operation)
			response := map[string]interface{}{
				"data": map[string]bool{"is_member": true},
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}

	// DELETE /2/lists/{id}/members/{user_id}
	if method == "DELETE" && strings.Contains(path, "/lists/") && strings.Contains(path, "/members/") {
		parts := strings.Split(path, "/members/")
		if len(parts) == 2 {
			listID := extractPathParam(parts[0], "/2/lists/")
			userID := parts[1]
			if listID != "" && userID != "" {
				// Check if list exists
				list := state.GetList(listID)
				if list == nil {
					return formatResourceNotFoundError("list", "id", listID), http.StatusNotFound
				}
				
				// Check if authenticated user is the owner
				authenticatedUserID := getAuthenticatedUserID(r, state)
				if list.OwnerID != authenticatedUserID {
					// User is not allowed to delete members from this list - return 403 error matching real API format
					errorResponse := map[string]interface{}{
						"detail": "You are not allowed to delete members from this List.",
						"type":   "about:blank",
						"title":  "Forbidden",
						"status": 403,
					}
					data, err := json.Marshal(errorResponse)
					if err != nil {
						log.Printf("Error marshaling 403 error response: %v", err)
						fallback := map[string]interface{}{
							"detail": "You are not allowed to delete members from this List.",
							"type":   "about:blank",
							"title":  "Forbidden",
							"status": 403,
						}
						data = marshalFallbackError(fallback)
						return data, http.StatusForbidden
					}
					return data, http.StatusForbidden
				}
				
				// Check if user exists
				user := state.GetUserByID(userID)
				if user == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusNotFound
				}
				
				if state.RemoveListMember(listID, userID) {
					response := map[string]interface{}{
						"data": map[string]bool{"is_member": false},
					}
					data, statusCode := MarshalJSONResponse(response)
					return data, statusCode
				} else {
					// Already not a member - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"is_member": false},
					}
					data, statusCode := MarshalJSONResponse(response)
					return data, statusCode
				}
			}
		}
	}

	// POST /2/users/{id}/followed_lists
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/followed_lists") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/followed_lists")
		if userID != "" {
			// Validate that the path parameter user ID matches the authenticated user ID
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
				return errorData, statusCode
			}
			
			var req struct {
				ListID string `json:"list_id"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if req.ListID == "" {
				errorResp := CreateValidationErrorResponse("list_id", "", "list_id field is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if list exists
			list := state.GetList(req.ListID)
			if list == nil {
				return formatResourceNotFoundError("list", "id", req.ListID), http.StatusOK
			}
			
			// Check if user exists
			user := state.GetUserByID(userID)
			if user == nil {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
			
			if state.FollowList(req.ListID, userID) {
				response := map[string]interface{}{
					"data": map[string]bool{"following": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already following - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"following": true},
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// DELETE /2/users/{source_user_id}/followed_lists/{target_list_id}
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/followed_lists/") {
		parts := strings.Split(path, "/followed_lists/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			listID := parts[1]
			if userID != "" && listID != "" {
				// Check if list exists
				list := state.GetList(listID)
				if list == nil {
					return formatResourceNotFoundError("list", "id", listID), http.StatusOK
				}
				
				// Check if user exists
				user := state.GetUserByID(userID)
				if user == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				
				if state.UnfollowList(listID, userID) {
					response := map[string]interface{}{
						"data": map[string]bool{"following": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Already unfollowed - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"following": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// PUT /2/tweets/{id}/hidden
	if method == "PUT" && strings.HasPrefix(path, "/2/tweets/") && strings.HasSuffix(path, "/hidden") {
		tweetID := extractPathParam(path, "/2/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/hidden")
		if tweetID != "" {
			var req struct {
				Hidden bool `json:"hidden"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if tweet exists
			tweet := state.GetTweet(tweetID)
			if tweet == nil {
				return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
			}
			
			if req.Hidden {
				if state.HideReply(tweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"hidden": true},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already hidden - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"hidden": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				if state.UnhideReply(tweetID) {
					response := map[string]interface{}{
						"data": map[string]bool{"hidden": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
				// Already unhidden - return success (idempotent)
				response := map[string]interface{}{
					"data": map[string]bool{"hidden": false},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// GET /2/users/{id}/pinned_lists
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/pinned_lists") && !strings.Contains(path, "/pinned_lists/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/pinned_lists")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				lists := state.GetLists(user.PinnedLists)
				return formatStateDataToOpenAPI(lists, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// POST /2/users/{id}/pinned_lists
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/pinned_lists") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/pinned_lists")
		if userID != "" {
			// Validate that the path parameter user ID matches the authenticated user ID
			authenticatedUserID := getAuthenticatedUserID(r, state)
			if errorData, statusCode := validateUserIDMatchesAuthenticatedUser(userID, authenticatedUserID); errorData != nil {
				return errorData, statusCode
			}
			
			var req struct {
				ListID string `json:"list_id"`
			}
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if len(bodyBytes) == 0 {
				errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			if req.ListID == "" {
				errorResp := CreateValidationErrorResponse("list_id", "", "list_id field is required")
				data, jsonErr := json.Marshal(errorResp)
				if jsonErr != nil {
					log.Printf("Error marshaling error response: %v", jsonErr)
					fallback := map[string]interface{}{
						"errors": []map[string]interface{}{
							{
								"message": "Internal server error",
								"code":    500,
							},
						},
					}
					data = marshalFallbackError(fallback)
					return data, http.StatusInternalServerError
				}
				return data, http.StatusBadRequest
			}
			
			// Check if list exists
			list := state.GetList(req.ListID)
			if list == nil {
				return formatResourceNotFoundError("list", "id", req.ListID), http.StatusOK
			}
			
			// Check if user exists
			user := state.GetUserByID(userID)
			if user == nil {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
			
			if state.PinList(req.ListID, userID) {
				response := map[string]interface{}{
					"data": map[string]bool{"pinned": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already pinned - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"pinned": true},
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// DELETE /2/users/{id}/pinned_lists/{list_id}
	if method == "DELETE" && strings.Contains(path, "/users/") && strings.Contains(path, "/pinned_lists/") {
		parts := strings.Split(path, "/pinned_lists/")
		if len(parts) == 2 {
			userID := extractPathParam(parts[0], "/2/users/")
			listID := parts[1]
			if userID != "" && listID != "" {
				// Check if list exists
				list := state.GetList(listID)
				if list == nil {
					return formatResourceNotFoundError("list", "id", listID), http.StatusOK
				}
				
				// Check if user exists
				user := state.GetUserByID(userID)
				if user == nil {
					return formatResourceNotFoundError("user", "id", userID), http.StatusOK
				}
				
				if state.UnpinList(listID, userID) {
					response := map[string]interface{}{
						"data": map[string]bool{"pinned": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				} else {
					// Already unpinned - return success (idempotent)
					response := map[string]interface{}{
						"data": map[string]bool{"pinned": false},
					}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
				}
			}
		}
	}

	// GET /2/media
	if method == "GET" && path == "/2/media" {
		// Check if media_keys parameter is provided
		mediaKeysStr := r.URL.Query().Get("media_keys")
		if mediaKeysStr != "" {
			// Parse media_keys (comma-separated)
			requestedMediaKeys := strings.Split(mediaKeysStr, ",")
			for i := range requestedMediaKeys {
				requestedMediaKeys[i] = strings.TrimSpace(requestedMediaKeys[i])
			}
			
			// Get media objects matching the requested keys
			var matchingMedia []*Media
			var notFoundKeys []string
			for _, key := range requestedMediaKeys {
				if media := state.GetMediaByKey(key); media != nil {
					matchingMedia = append(matchingMedia, media)
				} else {
					notFoundKeys = append(notFoundKeys, key)
				}
			}
			
			// Build response with data
			response := map[string]interface{}{
				"data": matchingMedia,
			}
			
			// Add "Not Found" errors for any media keys that don't exist
			if len(notFoundKeys) > 0 {
				var errors []map[string]interface{}
				for _, key := range notFoundKeys {
					errors = append(errors, map[string]interface{}{
						"value":         key,
						"detail":        fmt.Sprintf("Could not find media with media_keys: [%s].", key),
						"title":         "Not Found Error",
						"resource_type": "media",
						"parameter":     "media_keys",
						"resource_id":   key,
						"type":          "https://api.twitter.com/2/problems/resource-not-found",
					})
				}
				response["errors"] = errors
			}
			
			// Format response (formatStateDataToOpenAPI will handle the data array, but we need to preserve errors)
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		} else {
			// Get all media from state if no media_keys specified
			allMedia := state.GetAllMedia()
			return formatStateDataToOpenAPI(allMedia, op, spec, queryParams, state), http.StatusOK
		}
	}

	// POST /2/media/metadata
	if method == "POST" && path == "/2/media/metadata" {
		var req struct {
			MediaID string                 `json:"media_id"`
			AltText string                 `json:"alt_text,omitempty"`
			Metadata map[string]interface{} `json:"metadata,omitempty"`
		}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("failed to read request body: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if len(bodyBytes) == 0 {
			errorResp := CreateValidationErrorResponse("requestBody", "", "request body is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		if req.MediaID == "" {
			errorResp := CreateValidationErrorResponse("media_id", "", "media_id field is required")
			data, statusCode := MarshalJSONErrorResponse(errorResp)
			return data, statusCode
		}
		
		// Check if media exists
		media := state.GetMedia(req.MediaID)
		if media == nil {
			return formatResourceNotFoundError("media", "id", req.MediaID), http.StatusOK
		}
		
		if state.UpdateMediaMetadata(req.MediaID, req.AltText, req.Metadata) {
			updatedMedia := state.GetMedia(req.MediaID)
			if updatedMedia != nil {
				return formatStateDataToOpenAPI(updatedMedia, op, spec, queryParams, state), http.StatusOK
			}
		}
		// Update succeeded but media not found (shouldn't happen, but handle gracefully)
		return formatResourceNotFoundError("media", "id", req.MediaID), http.StatusOK
	}

	// POST /2/media/subtitles
	if method == "POST" && path == "/2/media/subtitles" {
		var req struct {
			MediaID string `json:"media_id"`
			LanguageCode string `json:"language_code"`
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.MediaID != "" {
			// For playground, we'll just acknowledge the subtitle was added
			media := state.GetMedia(req.MediaID)
			if media != nil {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"media_id": req.MediaID,
						"subtitle": map[string]interface{}{
							"language_code": req.LanguageCode,
							"display_name": req.DisplayName,
						},
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// DELETE /2/media/subtitles
	if method == "DELETE" && path == "/2/media/subtitles" {
		mediaID := r.URL.Query().Get("media_id")
		languageCode := r.URL.Query().Get("language_code")
		if mediaID != "" && languageCode != "" {
			media := state.GetMedia(mediaID)
			if media != nil {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"media_id": mediaID,
						"deleted": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// GET /2/tweets/analytics
	if method == "GET" && path == "/2/tweets/analytics" {
		// Return analytics based on state metrics
		allTweets := state.GetAllTweets()
		totalTweets := len(allTweets)
		var totalLikes, totalRetweets, totalReplies, totalQuotes int64
		
	analyticsLoop:
		for i, tweet := range allTweets {
			// Check context cancellation every 100 iterations
			if i%100 == 0 {
				select {
				case <-r.Context().Done():
					// Client disconnected, return partial analytics
					break analyticsLoop
				default:
				}
			}
			totalLikes += int64(tweet.PublicMetrics.LikeCount)
			totalRetweets += int64(tweet.PublicMetrics.RetweetCount)
			totalReplies += int64(tweet.PublicMetrics.ReplyCount)
			totalQuotes += int64(tweet.PublicMetrics.QuoteCount)
		}
		
		response := map[string]interface{}{
			"data": map[string]interface{}{
				"total_tweets":   totalTweets,
				"total_likes":    totalLikes,
				"total_retweets": totalRetweets,
				"total_replies":  totalReplies,
				"total_quotes":   totalQuotes,
			},
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// GET /2/usage/tweets
	if method == "GET" && path == "/2/usage/tweets" {
		// Parse days parameter (optional, valid range: 1-90, default: 7 per OpenAPI spec)
		daysParam := r.URL.Query().Get("days")
		days := 7 // default
		if daysParam != "" {
			if d, err := strconv.Atoi(daysParam); err == nil && d >= 1 && d <= 90 {
				days = d
			}
		}
		
		// Parse usage.fields parameter
		usageFieldsParam := r.URL.Query().Get("usage.fields")
		var requestedFields []string
		if usageFieldsParam != "" {
			requestedFields = strings.Split(usageFieldsParam, ",")
			// Trim whitespace
			for i, f := range requestedFields {
				requestedFields[i] = strings.TrimSpace(f)
			}
		}
		
		// Determine which fields to include
		// X API always includes default fields (cap_reset_day, project_cap, project_id, project_usage)
		// even when usage.fields is specified, then adds requested fields on top
		// Always include default fields
		includeCapResetDay := true
		includeProjectCap := true
		includeProjectID := true
		includeProjectUsage := true
		// Additional fields are only included if explicitly requested
		includeDailyClientAppUsage := contains(requestedFields, "daily_client_app_usage")
		includeDailyProjectUsage := contains(requestedFields, "daily_project_usage")
		
		// Build response data
		responseData := make(map[string]interface{})
		
		// Generate a realistic project ID (Snowflake ID format)
		projectID := state.generateID()
		
		// Calculate cap_reset_day (day of month when cap resets, typically 1-28)
		now := time.Now()
		capResetDay := now.Day()
		if capResetDay > 28 {
			capResetDay = 1 // Reset to 1 if beyond 28
		}
		
		// Generate realistic usage values
		// Project cap is typically a large number (billions)
		projectCap := "5000000000" // 5 billion default
		
		// Project usage should be less than cap, generate a realistic value
		// Using seeded tweet count as a base for usage
		allTweets := state.GetAllTweets()
		baseUsage := len(allTweets) * 10 // Multiply by 10 for realistic API usage
		if baseUsage < 100 {
			baseUsage = 4203 // Default minimum
		}
		projectUsage := strconv.Itoa(baseUsage)
		
		// Add fields based on what's requested
		if includeCapResetDay {
			responseData["cap_reset_day"] = capResetDay
		}
		if includeProjectCap {
			responseData["project_cap"] = projectCap
		}
		if includeProjectID {
			responseData["project_id"] = projectID
		}
		if includeProjectUsage {
			responseData["project_usage"] = projectUsage
		}
		
		// Generate daily_client_app_usage if requested
		if includeDailyClientAppUsage {
			// Generate 3-6 client apps with usage data
			numApps := 3 + (len(allTweets) % 4) // 3-6 apps
			clientApps := make([]map[string]interface{}, 0, numApps)
			
			// Base client app IDs (realistic shorter IDs)
			baseClientAppIDs := []int{29035897, 30355442, 30388029, 31485888, 31585955, 31832838}
			
			// Generate usage for the last N days (where N = days parameter)
			for i := 0; i < numApps; i++ {
				clientAppID := strconv.Itoa(baseClientAppIDs[i%len(baseClientAppIDs)])
				
				// Some apps have usage, some don't (matching real API pattern)
				hasUsage := (i % 3) != 2 // 2 out of 3 apps have usage
				
				clientApp := map[string]interface{}{
					"client_app_id": clientAppID,
				}
				
				if hasUsage {
					// Generate daily usage for some days (not all days)
					usageEntries := make([]map[string]interface{}, 0)
					// Determine which days have usage (sparse, like real API)
					numDaysWithUsage := 1 + (i % 6) // 1 to 6 entries
					if numDaysWithUsage > days {
						numDaysWithUsage = days
					}
					
					// Select specific days (sparse pattern, in chronological order)
					dayOffsets := make([]int, 0, numDaysWithUsage)
					for j := 0; j < numDaysWithUsage; j++ {
						// Select days with gaps (like real API)
						dayOffset := (j * 2) + (i % 3)
						if dayOffset >= days {
							dayOffset = days - 1 - (numDaysWithUsage - j - 1)
						}
						dayOffsets = append(dayOffsets, dayOffset)
					}
					
					// Sort day offsets in ascending order (oldest first, matching real API)
					for k := 0; k < len(dayOffsets)-1; k++ {
						for l := k + 1; l < len(dayOffsets); l++ {
							if dayOffsets[k] > dayOffsets[l] {
								dayOffsets[k], dayOffsets[l] = dayOffsets[l], dayOffsets[k]
							}
						}
					}
					
					for _, dayOffset := range dayOffsets {
						// Generate date for dayOffset days ago, rounded to midnight UTC
						usageDate := now.AddDate(0, 0, -dayOffset)
						usageDate = time.Date(usageDate.Year(), usageDate.Month(), usageDate.Day(), 0, 0, 0, 0, time.UTC)
						
						// Generate usage value (varying values like real API)
						usageValue := baseUsage/(numDaysWithUsage+1) + (numDaysWithUsage-dayOffset)*100 + (i*50) + (dayOffset*10)
						if usageValue < 1 {
							usageValue = 1
						}
						// Cap at reasonable values
						if usageValue > 4000 {
							usageValue = 4000 - (dayOffset * 100)
							if usageValue < 1 {
								usageValue = 1
							}
						}
						
						usageEntries = append(usageEntries, map[string]interface{}{
							"date":  usageDate.Format("2006-01-02T15:04:05.000Z"),
							"usage": strconv.Itoa(usageValue),
						})
					}
					
					clientApp["usage"] = usageEntries
					clientApp["usage_result_count"] = len(usageEntries)
				} else {
					clientApp["usage_result_count"] = 0
				}
				
				clientApps = append(clientApps, clientApp)
			}
			
			responseData["daily_client_app_usage"] = clientApps
		}
		
		// Generate daily_project_usage if requested
		if includeDailyProjectUsage {
			// Generate daily usage for the project (only days with actual usage, like real API)
			usageEntries := make([]map[string]interface{}, 0)
			// Select some days within the range (not all days)
			numDaysWithUsage := 3 + (len(allTweets) % 4) // 3-6 days
			if numDaysWithUsage > days {
				numDaysWithUsage = days
			}
			
			// Select specific days (sparse pattern like real API, in chronological order)
			dayOffsets := make([]int, 0, numDaysWithUsage)
			for j := 0; j < numDaysWithUsage; j++ {
				// Select days with gaps
				dayOffset := (j * 2) + (j % 3)
				if dayOffset >= days {
					dayOffset = days - 1 - (numDaysWithUsage - j - 1)
				}
				dayOffsets = append(dayOffsets, dayOffset)
			}
			
			// Sort day offsets in ascending order (oldest first, matching real API)
			for k := 0; k < len(dayOffsets)-1; k++ {
				for l := k + 1; l < len(dayOffsets); l++ {
					if dayOffsets[k] > dayOffsets[l] {
						dayOffsets[k], dayOffsets[l] = dayOffsets[l], dayOffsets[k]
					}
				}
			}
			
			for _, dayOffset := range dayOffsets {
				usageDate := now.AddDate(0, 0, -dayOffset)
				usageDate = time.Date(usageDate.Year(), usageDate.Month(), usageDate.Day(), 0, 0, 0, 0, time.UTC)
				
				// Generate usage value (varying values like real API)
				usageValue := baseUsage/(numDaysWithUsage+1) + (numDaysWithUsage-dayOffset)*100 + (dayOffset*10)
				if usageValue < 1 {
					usageValue = 1
				}
				// Cap at reasonable values
				if usageValue > 4000 {
					usageValue = 4000 - (dayOffset * 100)
					if usageValue < 1 {
						usageValue = 1
					}
				}
				
				usageEntries = append(usageEntries, map[string]interface{}{
					"date":  usageDate.Format("2006-01-02T15:04:05.000Z"),
					"usage": strconv.Itoa(usageValue),
				})
			}
			
			// project_id should be a string (matching real API)
			responseData["daily_project_usage"] = map[string]interface{}{
				"project_id": projectID,
				"usage":      usageEntries,
			}
		}
		
		response := map[string]interface{}{
			"data": responseData,
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/tweets/search/stream/rules
	if method == "GET" && path == "/2/tweets/search/stream/rules" {
		if state == nil {
			errorResp := map[string]interface{}{
				"errors": []map[string]interface{}{
					{
						"message": "Internal server error",
						"code":    131,
						"title":   "Server Error",
						"type":    "https://api.twitter.com/2/problems/server-error",
					},
				},
				"title":  "Server Error",
				"detail": "Internal server error",
				"type":   "https://api.twitter.com/2/problems/server-error",
			}
			data, _ := MarshalJSONResponse(errorResp)
			return data, http.StatusInternalServerError
		}
		rules := state.GetSearchStreamRules()
		rulesData := make([]map[string]interface{}, len(rules))
		for i, rule := range rules {
			rulesData[i] = map[string]interface{}{
				"id":    rule.ID,
				"value": rule.Value,
			}
			if rule.Tag != "" {
				rulesData[i]["tag"] = rule.Tag
			}
		}
		response := map[string]interface{}{
			"data": rulesData,
			"meta": map[string]interface{}{
				"result_count": len(rulesData),
			},
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// POST /2/tweets/search/stream/rules
	if method == "POST" && path == "/2/tweets/search/stream/rules" {
		var req struct {
			Add []struct {
				Value string `json:"value"`
				Tag   string `json:"tag,omitempty"`
			} `json:"add"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Handle JSON decode errors properly
			errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
			data, statusCode := MarshalJSONResponse(errorResp)
			return data, statusCode
		}

		// Validate that add array is not empty
		if len(req.Add) == 0 {
			errorResp := CreateValidationErrorResponse("add", "", "The `add` field is required and cannot be empty")
			data, statusCode := MarshalJSONResponse(errorResp)
			return data, statusCode
		}

		createdRules := make([]map[string]interface{}, 0)
		duplicateErrors := make([]map[string]interface{}, 0)
		createdCount := 0
		notCreatedCount := 0
		validCount := 0
		invalidCount := 0

		for _, rule := range req.Add {
			// Check for duplicate rule (by value)
			existingRule := state.FindSearchStreamRuleByValue(rule.Value)
			if existingRule != nil {
				// Duplicate rule - add to errors
				duplicateErrors = append(duplicateErrors, map[string]interface{}{
					"value": rule.Value,
					"id":    existingRule.ID,
					"title": "DuplicateRule",
					"type":  "https://api.twitter.com/2/problems/duplicate-rules",
				})
				notCreatedCount++
				invalidCount++
			} else {
				// Create new rule
				ruleID := state.CreateSearchStreamRule(rule.Value, rule.Tag)
				createdRules = append(createdRules, map[string]interface{}{
					"id":    ruleID,
					"value": rule.Value,
					"tag":   rule.Tag,
				})
				createdCount++
				validCount++
			}
		}

		// Build response matching real API format
		// Format timestamp to match real API: "2025-12-19T21:17:21.512Z" (RFC3339 with milliseconds)
		now := time.Now().UTC()
		timestamp := now.Format("2006-01-02T15:04:05.000Z")

		response := map[string]interface{}{
			"meta": map[string]interface{}{
				"sent": timestamp,
				"summary": map[string]interface{}{
					"created":     createdCount,
					"not_created": notCreatedCount,
					"valid":       validCount,
					"invalid":     invalidCount,
				},
			},
		}

		// Add data array if any rules were created
		if len(createdRules) > 0 {
			response["data"] = createdRules
		}

		// Add errors array if any duplicates were found
		if len(duplicateErrors) > 0 {
			response["errors"] = duplicateErrors
		}

		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/tweets/search/stream/rules/counts
	if method == "GET" && path == "/2/tweets/search/stream/rules/counts" {
		if state == nil {
			errorResp := map[string]interface{}{
				"errors": []map[string]interface{}{
					{
						"message": "Internal server error",
						"code":    131,
						"title":   "Server Error",
						"type":    "https://api.twitter.com/2/problems/server-error",
					},
				},
				"title":  "Server Error",
				"detail": "Internal server error",
				"type":   "https://api.twitter.com/2/problems/server-error",
			}
			data, _ := MarshalJSONResponse(errorResp)
			return data, http.StatusInternalServerError
		}
		
		rules := state.GetSearchStreamRules()
		ruleCount := len(rules)
		
		// Build response matching real API format exactly (no example/mock data)
		// The real API only returns these specific fields, not all_project_client_apps or errors
		response := map[string]interface{}{
			"data": map[string]interface{}{
				"cap_per_client_app": "2500000",
				"cap_per_project":    "2500000",
				"client_app_rules_count": map[string]interface{}{
					"client_app_id": "123456", // Placeholder client app ID
					"rule_count":    ruleCount, // Actual count from state
				},
				"project_rules_count": fmt.Sprintf("%d", ruleCount), // Actual count from state as string
			},
		}
		
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// Search webhook endpoints removed - not ready to support yet

	// REMOVED: GET /2/users/personalized_trends
	// This endpoint should be handled by the OpenAPI spec handler, not hardcoded
	// The OpenAPI spec will generate the correct response format with proper field names

	// POST /2/users/{id}/dm/block
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/dm/block") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/dm/block")
		if userID != "" {
			// Read body if present (optional for this endpoint)
			bodyBytes, _ := io.ReadAll(r.Body)
			var req struct {
				TargetUserID string `json:"target_user_id"`
			}
			
			// Parse body if provided, but don't require it
			if len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
					if jsonErr != nil {
						log.Printf("Error marshaling error response: %v", jsonErr)
						fallback := map[string]interface{}{
							"errors": []map[string]interface{}{
								{
									"message": "Internal server error",
									"code":    500,
								},
							},
						}
						data = marshalFallbackError(fallback)
						return data, http.StatusInternalServerError
					}
					return data, http.StatusBadRequest
				}
			}
			
			// For DM block, the user ID to block is in the path parameter {id}
			// The body is optional and not used
			targetUserID := userID
			
			// Check if users exist
			sourceUser := state.GetUserByID("0") // Authenticated user (simplified)
			targetUser := state.GetUserByID(targetUserID)
			if sourceUser == nil {
				return formatResourceNotFoundError("user", "id", "0"), http.StatusOK
			}
			if targetUser == nil {
				return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
			}
			
			// For DM block, the user ID to block is in the path parameter {id}
			// Block the user specified in the path
			if state.BlockUser("0", targetUserID) { // "0" is the authenticated user (simplified)
				response := map[string]interface{}{
					"data": map[string]bool{"blocking": true},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already blocked - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"blocking": true},
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}

	// POST /2/users/{id}/dm/unblock
	if method == "POST" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/dm/unblock") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/dm/unblock")
		if userID != "" {
			// Read body if present (optional for this endpoint)
			bodyBytes, _ := io.ReadAll(r.Body)
			var req struct {
				TargetUserID string `json:"target_user_id"`
			}
			
			// Parse body if provided, but don't require it
			if len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, &req); err != nil {
					errorResp := CreateValidationErrorResponse("requestBody", "", fmt.Sprintf("invalid JSON: %v", err))
					data, jsonErr := json.Marshal(errorResp)
					if jsonErr != nil {
						log.Printf("Error marshaling error response: %v", jsonErr)
						fallback := map[string]interface{}{
							"errors": []map[string]interface{}{
								{
									"message": "Internal server error",
									"code":    500,
								},
							},
						}
						data = marshalFallbackError(fallback)
						return data, http.StatusInternalServerError
					}
					return data, http.StatusBadRequest
				}
			}
			
			// For DM unblock, the user ID to unblock is in the path parameter {id}
			// The body is optional and not used
			targetUserID := userID
			
			// Check if users exist
			sourceUser := state.GetUserByID("0") // Authenticated user (simplified)
			targetUser := state.GetUserByID(targetUserID)
			if sourceUser == nil {
				return formatResourceNotFoundError("user", "id", "0"), http.StatusOK
			}
			if targetUser == nil {
				return formatResourceNotFoundError("user", "id", targetUserID), http.StatusOK
			}
			
			// For DM unblock, the user ID to unblock is in the path parameter {id}
			if state.UnblockUser("0", targetUserID) {
				response := map[string]interface{}{
					"data": map[string]bool{"blocking": false},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			// Already unblocked - return success (idempotent)
			response := map[string]interface{}{
				"data": map[string]bool{"blocking": false},
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// GET /2/spaces/{id}/buyers
	if method == "GET" && strings.Contains(path, "/spaces/") && strings.HasSuffix(path, "/buyers") {
		spaceID := extractPathParam(path, "/2/spaces/")
		spaceID = strings.TrimSuffix(spaceID, "/buyers")
		if spaceID != "" {
			space := state.GetSpace(spaceID)
			if space != nil {
				buyers := state.GetSpaceBuyers(spaceID)
				// Format buyers response
				buyersData := make([]interface{}, len(buyers))
				for i, buyer := range buyers {
					buyersData[i] = FormatUser(buyer)
				}
				response := map[string]interface{}{
					"meta": map[string]interface{}{
						"result_count": len(buyersData),
					},
				}
				// Only include "data" field if there are results (matching real API behavior)
				if len(buyersData) > 0 {
					response["data"] = buyersData
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("space", "id", spaceID), http.StatusOK
			}
		}
	}

	// GET /2/media/analytics
	if method == "GET" && path == "/2/media/analytics" {
		// Required parameters: start_time, end_time, granularity
		startTimeStr := r.URL.Query().Get("start_time")
		endTimeStr := r.URL.Query().Get("end_time")
		granularity := r.URL.Query().Get("granularity")
		mediaKeysStr := r.URL.Query().Get("media_keys")
		
		// Validate required parameters
		if startTimeStr == "" || endTimeStr == "" {
			errorResponse := CreateValidationErrorResponse("start_time", startTimeStr, "start_time and end_time are required parameters")
			data, statusCode := MarshalJSONErrorResponse(errorResponse)
			return data, statusCode
		}
		
		// Parse timestamps
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			errorResponse := CreateValidationErrorResponse("start_time", startTimeStr, fmt.Sprintf("Invalid start_time format: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResponse)
			return data, statusCode
		}
		
		endTime, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			errorResponse := CreateValidationErrorResponse("end_time", endTimeStr, fmt.Sprintf("Invalid end_time format: %v", err))
			data, statusCode := MarshalJSONErrorResponse(errorResponse)
			return data, statusCode
		}
		
		// Default granularity to "hourly" if not specified (OpenAPI spec uses: hourly, daily, total)
		if granularity == "" {
			granularity = "hourly"
		}
		// OpenAPI spec validation will handle invalid values, so we don't need to validate here
		
		// Parse media_keys (comma-separated) and validate format
		var requestedMediaKeys []string
		if mediaKeysStr != "" {
			requestedMediaKeys = strings.Split(mediaKeysStr, ",")
			for i := range requestedMediaKeys {
				requestedMediaKeys[i] = strings.TrimSpace(requestedMediaKeys[i])
			}
			
			// Validate media_keys format: must match ^([0-9]+)_([0-9]+)$
			mediaKeyPattern := regexp.MustCompile(`^([0-9]+)_([0-9]+)$`)
			var invalidKeys []string
			for _, key := range requestedMediaKeys {
				if !mediaKeyPattern.MatchString(key) {
					invalidKeys = append(invalidKeys, key)
				}
			}
			
			if len(invalidKeys) > 0 {
				// Return error matching X API format
				errorResponse := map[string]interface{}{
					"errors": []map[string]interface{}{
						{
							"parameters": map[string]interface{}{
								"media_keys": invalidKeys,
							},
							"message": fmt.Sprintf("The `media_keys` query parameter value [%s] does not match ^([0-9]+)_([0-9]+)$", strings.Join(invalidKeys, ", ")),
						},
					},
					"title":   "Invalid Request",
					"detail":  "One or more parameters to your request was invalid.",
					"type":    "https://api.twitter.com/2/problems/invalid-request",
				}
				data, _ := json.Marshal(errorResponse)
				return data, http.StatusBadRequest
			}
		}
		
		// Get media objects and track which keys were not found
		var mediaToAnalyze []*Media
		var notFoundKeys []string
		if len(requestedMediaKeys) > 0 {
			// Filter by requested media keys
			for _, key := range requestedMediaKeys {
				if media := state.GetMediaByKey(key); media != nil {
					mediaToAnalyze = append(mediaToAnalyze, media)
				} else {
					notFoundKeys = append(notFoundKeys, key)
				}
			}
		} else {
			// Return analytics for all media
			allMedia := state.GetAllMedia()
			mediaToAnalyze = allMedia
		}
		
		// Generate timestamped metrics based on granularity
		var analyticsData []map[string]interface{}
		for _, media := range mediaToAnalyze {
			mediaAnalytics := map[string]interface{}{
				"media_key": media.MediaKey,
			}
			
			var timestampedMetrics []map[string]interface{}
			
			if granularity == "total" {
				// Single timestamped metric for the entire period
				timestampedMetrics = append(timestampedMetrics, map[string]interface{}{
					"timestamp": startTime.Format(time.RFC3339),
					"metrics": map[string]interface{}{
						"video_views":           100 + len(media.MediaKey)%50,
						"playback_start":        80 + len(media.MediaKey)%30,
						"playback25":            60 + len(media.MediaKey)%25,
						"playback50":            40 + len(media.MediaKey)%20,
						"playback75":            25 + len(media.MediaKey)%15,
						"playback_complete":     15 + len(media.MediaKey)%10,
						"play_from_tap":         20 + len(media.MediaKey)%10,
						"cta_url_clicks":        10 + len(media.MediaKey)%5,
						"cta_watch_clicks":      5 + len(media.MediaKey)%3,
						"watch_time_ms":         50000 + len(media.MediaKey)*1000,
					},
				})
			} else {
				// Generate metrics for each time period (hourly or daily)
				var periodDuration time.Duration
				if granularity == "hourly" {
					periodDuration = time.Hour
				} else if granularity == "daily" {
					periodDuration = 24 * time.Hour
				} else {
					// Fallback to hourly if somehow an invalid value got through
					periodDuration = time.Hour
				}
				
				currentTime := startTime
				for currentTime.Before(endTime) || currentTime.Equal(endTime) {
					// Generate mock metrics for this time period
					// Vary metrics based on media key and time to make it realistic
					timeHash := int64(currentTime.Unix()) % 100
					keyHash := int64(len(media.MediaKey)) % 50
					
					timestampedMetrics = append(timestampedMetrics, map[string]interface{}{
						"timestamp": currentTime.Format(time.RFC3339),
						"metrics": map[string]interface{}{
							"video_views":           int64(50 + timeHash + keyHash),
							"playback_start":        int64(40 + timeHash + keyHash),
							"playback25":            int64(30 + timeHash + keyHash),
							"playback50":            int64(20 + timeHash + keyHash),
							"playback75":            int64(10 + timeHash + keyHash),
							"playback_complete":     int64(5 + timeHash/2 + keyHash/2),
							"play_from_tap":         int64(10 + timeHash/2 + keyHash/2),
							"cta_url_clicks":        int64(5 + timeHash/3 + keyHash/3),
							"cta_watch_clicks":      int64(2 + timeHash/4 + keyHash/4),
							"watch_time_ms":         int64(20000 + timeHash*100 + keyHash*100),
						},
					})
					
					currentTime = currentTime.Add(periodDuration)
				}
			}
			
			mediaAnalytics["timestamped_metrics"] = timestampedMetrics
			analyticsData = append(analyticsData, mediaAnalytics)
		}
		
		// Build response with data and errors (if any media keys were not found)
		response := map[string]interface{}{
			"data": analyticsData,
		}
		
		// Add "Not Found" errors for any media keys that don't exist
		if len(notFoundKeys) > 0 {
			var errors []map[string]interface{}
			for _, key := range notFoundKeys {
				errors = append(errors, map[string]interface{}{
					"value":         key,
					"detail":        fmt.Sprintf("Could not find media_analytics with media_keys: [%s].", key),
					"title":         "Not Found Error",
					"resource_type": "media_analytics",
					"parameter":     "media_keys",
					"resource_id":   key,
					"type":          "https://api.twitter.com/2/problems/resource-not-found",
				})
			}
			response["errors"] = errors
		}
		
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// POST /2/media/upload (alternative upload method)
	if method == "POST" && path == "/2/media/upload" {
		// Use existing media upload logic
		mediaKey := r.URL.Query().Get("media_key")
		if mediaKey == "" {
			// Media keys must match pattern: ^([0-9]+)_([0-9]+)$ (e.g., "123_456")
			// Use timestamp components to create valid format
			now := time.Now().UnixNano()
			mediaKey = fmt.Sprintf("%d_%d", now/1000000, now%1000000)
		}
		_ = state.CreateMedia(mediaKey, 86400) // 24 hours
		createdMedia := state.GetMediaByKey(mediaKey)
		if createdMedia != nil {
			return formatStateDataToOpenAPI(createdMedia, op, spec, queryParams, state), http.StatusCreated
		}
	}

	// DMs endpoints

	// GET /2/dm_events
	if method == "GET" && path == "/2/dm_events" {
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		events := state.GetDMEvents("", "", limit)
		eventsData := make([]map[string]interface{}, len(events))
		for i, event := range events {
			eventsData[i] = formatDMEvent(event)
		}
		response := map[string]interface{}{
			"data": eventsData,
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// POST /2/dm_events
	if method == "POST" && path == "/2/dm_events" {
		var req struct {
			ConversationID string   `json:"dm_conversation_id"`
			Text           string   `json:"text"`
			ParticipantIDs []string `json:"participant_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.ConversationID != "" {
			// Get sender from auth context (for now, use first participant or default user)
			senderID := "0" // Default to playground user
			if len(req.ParticipantIDs) > 0 {
				senderID = req.ParticipantIDs[0]
			}
			event := state.CreateDMEvent(req.ConversationID, senderID, "MessageCreate", req.Text, req.ParticipantIDs)
			return formatStateDataToOpenAPI(event, op, spec, queryParams, state), http.StatusCreated
		}
	}

	// GET /2/dm_conversations
	if method == "GET" && path == "/2/dm_conversations" {
		// Get user ID from query or default to "0"
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			userID = "0"
		}
		conversations := state.GetDMConversations(userID)
		conversationsData := make([]map[string]interface{}, len(conversations))
		for i, conv := range conversations {
			conversationsData[i] = map[string]interface{}{
				"id":              conv.ID,
				"created_at":      conv.CreatedAt.Format(time.RFC3339),
				"participant_ids": conv.ParticipantIDs,
			}
		}
		response := map[string]interface{}{
			"data": conversationsData,
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// POST /2/dm_conversations
	if method == "POST" && path == "/2/dm_conversations" {
		var req struct {
			ParticipantIDs []string `json:"participant_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && len(req.ParticipantIDs) > 0 {
			// Check if conversation already exists
			existing := state.GetDMConversationByParticipants(req.ParticipantIDs)
			if existing != nil {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"id": existing.ID,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
			conversation := state.CreateDMConversation(req.ParticipantIDs)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"id":              conversation.ID,
					"created_at":      conversation.CreatedAt.Format(time.RFC3339),
					"participant_ids": conversation.ParticipantIDs,
				},
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}

	// GET /2/dm_conversations/{dm_conversation_id}/messages
	if method == "GET" && strings.HasPrefix(path, "/2/dm_conversations/") && strings.HasSuffix(path, "/messages") {
		conversationID := extractPathParam(path, "/2/dm_conversations/")
		conversationID = strings.TrimSuffix(conversationID, "/messages")
		if conversationID != "" {
			limit := 10
			if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
				if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
					limit = parsed
				}
			}
			events := state.GetDMEventsByConversation(conversationID, limit)
			eventsData := make([]map[string]interface{}, len(events))
			for i, event := range events {
				eventsData[i] = formatDMEvent(event)
			}
			response := map[string]interface{}{
				"data": eventsData,
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// POST /2/dm_conversations/with/{participant_id}/messages
	if method == "POST" && strings.HasPrefix(path, "/2/dm_conversations/with/") && strings.HasSuffix(path, "/messages") {
		participantID := extractPathParam(path, "/2/dm_conversations/with/")
		participantID = strings.TrimSuffix(participantID, "/messages")
		if participantID != "" {
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Text != "" {
				// Get sender (default to "0")
				senderID := "0"
				// Find or create conversation between sender and participant
				participantIDs := []string{senderID, participantID}
				conversation := state.GetDMConversationByParticipants(participantIDs)
				if conversation == nil {
					conversation = state.CreateDMConversation(participantIDs)
				}
				event := state.CreateDMEvent(conversation.ID, senderID, "MessageCreate", req.Text, participantIDs)
				response := map[string]interface{}{
					"data": formatDMEvent(event),
				}
				data, _ := MarshalJSONResponse(response)
				// Override status code to Created for POST operations
				return data, http.StatusCreated
			}
		}
	}

	// POST /2/dm_conversations/{dm_conversation_id}/messages
	if method == "POST" && strings.HasPrefix(path, "/2/dm_conversations/") && strings.Contains(path, "/messages") && !strings.Contains(path, "/with/") {
		conversationID := extractPathParam(path, "/2/dm_conversations/")
		conversationID = strings.TrimSuffix(conversationID, "/messages")
		if conversationID != "" {
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Text != "" {
				conversation := state.GetDMConversation(conversationID)
				if conversation != nil {
					senderID := "0" // Default sender
					if len(conversation.ParticipantIDs) > 0 {
						senderID = conversation.ParticipantIDs[0]
					}
					event := state.CreateDMEvent(conversationID, senderID, "MessageCreate", req.Text, conversation.ParticipantIDs)
					response := map[string]interface{}{
						"data": formatDMEvent(event),
					}
					data, _ := MarshalJSONResponse(response)
					// Override status code to Created for POST operations
					return data, http.StatusCreated
				}
			}
		}
	}

	// GET /2/dm_conversations/with/{participant_id}/dm_events
	if method == "GET" && strings.HasPrefix(path, "/2/dm_conversations/with/") && strings.HasSuffix(path, "/dm_events") {
		participantID := extractPathParam(path, "/2/dm_conversations/with/")
		participantID = strings.TrimSuffix(participantID, "/dm_events")
		if participantID != "" {
			limit := 10
			if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
				if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
					limit = parsed
				}
			}
			events := state.GetDMEventsByParticipant(participantID, limit)
			eventsData := make([]map[string]interface{}, len(events))
			for i, event := range events {
				eventsData[i] = formatDMEvent(event)
			}
			response := map[string]interface{}{
				"data": eventsData,
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// GET /2/dm_conversations/{id}/dm_events
	if method == "GET" && strings.HasPrefix(path, "/2/dm_conversations/") && strings.HasSuffix(path, "/dm_events") && !strings.Contains(path, "/with/") {
		conversationID := extractPathParam(path, "/2/dm_conversations/")
		conversationID = strings.TrimSuffix(conversationID, "/dm_events")
		if conversationID != "" {
			limit := 10
			if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
				if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
					limit = parsed
				}
			}
			events := state.GetDMEventsByConversation(conversationID, limit)
			eventsData := make([]map[string]interface{}, len(events))
			for i, event := range events {
				eventsData[i] = formatDMEvent(event)
			}
			response := map[string]interface{}{
				"data": eventsData,
			}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
		}
	}

	// GET /2/dm_events/{event_id}
	if method == "GET" && strings.HasPrefix(path, "/2/dm_events/") {
		eventID := strings.TrimPrefix(path, "/2/dm_events/")
		if eventID != "" {
			event := state.GetDMEvent(eventID)
			if event != nil {
				return formatStateDataToOpenAPI(event, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("dm_event", "id", eventID), http.StatusOK
			}
		}
	}

	// DELETE /2/dm_events/{event_id}
	if method == "DELETE" && strings.HasPrefix(path, "/2/dm_events/") {
		eventID := strings.TrimPrefix(path, "/2/dm_events/")
		if eventID != "" {
			if state.DeleteDMEvent(eventID) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"deleted": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("dm_event", "id", eventID), http.StatusOK
			}
		}
	}

	// Compliance Jobs endpoints

	// POST /2/compliance/jobs
	if method == "POST" && path == "/2/compliance/jobs" {
		var req struct {
			Type string `json:"type"` // "tweets" or "users"
			Name string `json:"name,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Type != "" {
			if req.Name == "" {
				req.Name = fmt.Sprintf("job-%d", time.Now().Unix())
			}
			job := state.CreateComplianceJob(req.Name, req.Type)
			jobMap := map[string]interface{}{
				"id":                job.ID,
				"name":              job.Name,
				"type":              job.Type,
				"status":            job.Status,
				"created_at":        job.CreatedAt.Format(time.RFC3339),
				"upload_url":        job.UploadURL,
				"upload_expires_at": job.UploadExpiresAt.Format(time.RFC3339),
			}
			response := map[string]interface{}{
				"data": jobMap,
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}

	// GET /2/compliance/jobs
	if method == "GET" && path == "/2/compliance/jobs" {
		jobType := r.URL.Query().Get("type")
		jobs := state.GetComplianceJobs(jobType)
		jobsData := make([]map[string]interface{}, len(jobs))
		for i, job := range jobs {
			jobMap := map[string]interface{}{
				"id":         job.ID,
				"name":       job.Name,
				"type":       job.Type,
				"status":     job.Status,
				"created_at": job.CreatedAt.Format(time.RFC3339),
			}
			if job.UploadURL != "" {
				jobMap["upload_url"] = job.UploadURL
				jobMap["upload_expires_at"] = job.UploadExpiresAt.Format(time.RFC3339)
			}
			if job.DownloadURL != "" {
				jobMap["download_url"] = job.DownloadURL
				jobMap["download_expires_at"] = job.DownloadExpiresAt.Format(time.RFC3339)
			}
			jobsData[i] = jobMap
		}
		response := map[string]interface{}{
			"data": jobsData,
			"meta": map[string]interface{}{
				"result_count": len(jobsData),
			},
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// GET /2/compliance/jobs/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/compliance/jobs/") {
		jobID := strings.TrimPrefix(path, "/2/compliance/jobs/")
		if jobID != "" {
			job := state.GetComplianceJob(jobID)
			if job != nil {
				jobMap := map[string]interface{}{
					"id":         job.ID,
					"name":       job.Name,
					"type":       job.Type,
					"status":     job.Status,
					"created_at": job.CreatedAt.Format(time.RFC3339),
				}
				if job.UploadURL != "" {
					jobMap["upload_url"] = job.UploadURL
					jobMap["upload_expires_at"] = job.UploadExpiresAt.Format(time.RFC3339)
				}
				if job.DownloadURL != "" {
					jobMap["download_url"] = job.DownloadURL
					jobMap["download_expires_at"] = job.DownloadExpiresAt.Format(time.RFC3339)
				}
				response := map[string]interface{}{
					"data": jobMap,
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("compliance_job", "id", jobID), http.StatusOK
			}
		}
	}

	// Communities endpoints

	// GET /2/communities/search
	if method == "GET" && path == "/2/communities/search" {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		communities := state.SearchCommunities(query, limit)
		communitiesData := make([]map[string]interface{}, len(communities))
		for i, community := range communities {
			communityMap := formatCommunity(community)
			// Always apply field filtering - if no fields specified, returns only defaults (id, name)
			requestedFields := []string{}
			if queryParams != nil && len(queryParams.CommunityFields) > 0 {
				requestedFields = queryParams.CommunityFields
			}
			communityMap = filterCommunityFields(communityMap, requestedFields)
			communitiesData[i] = communityMap
		}
		response := map[string]interface{}{
			"data": communitiesData,
			"meta": map[string]interface{}{
				"result_count": len(communitiesData),
			},
		}
		data, statusCode := MarshalJSONResponse(response)
		return data, statusCode
	}

	// GET /2/communities/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/communities/") {
		communityID := strings.TrimPrefix(path, "/2/communities/")
		if communityID != "" {
			community := state.GetCommunity(communityID)
			if community != nil {
				communityMap := formatCommunity(community)
				// Always apply field filtering - if no fields specified, returns only defaults (id, name)
				requestedFields := []string{}
				if queryParams != nil && len(queryParams.CommunityFields) > 0 {
					requestedFields = queryParams.CommunityFields
				}
				communityMap = filterCommunityFields(communityMap, requestedFields)
				response := map[string]interface{}{
					"data": communityMap,
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("community", "id", communityID), http.StatusOK
			}
		}
	}

	// News endpoints

	// GET /2/news/search
	if method == "GET" && path == "/2/news/search" {
		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		news := state.SearchNews(query, limit)
		newsData := make([]map[string]interface{}, len(news))
		for i, article := range news {
			articleMap := map[string]interface{}{
				"id":        article.ID,
				"name":      article.Name,
				"summary":   article.Summary,
				"hook":      article.Hook,
				"category":  article.Category,
				"updated_at": article.UpdatedAt.Format(time.RFC3339),
				"disclaimer": article.Disclaimer,
			}
			if article.Contexts != nil {
				articleMap["contexts"] = article.Contexts
			}
			newsData[i] = articleMap
		}
		response := map[string]interface{}{
			"data": newsData,
			"meta": map[string]interface{}{
				"result_count": len(newsData),
			},
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// GET /2/news/{id}
	if method == "GET" && strings.HasPrefix(path, "/2/news/") {
		newsID := strings.TrimPrefix(path, "/2/news/")
		if newsID != "" {
			article := state.GetNews(newsID)
			if article != nil {
				articleMap := map[string]interface{}{
					"id":         article.ID,
					"name":       article.Name,
					"summary":    article.Summary,
					"hook":       article.Hook,
					"category":   article.Category,
					"updated_at": article.UpdatedAt.Format(time.RFC3339),
					"disclaimer": article.Disclaimer,
				}
				if article.Contexts != nil {
					articleMap["contexts"] = article.Contexts
				}
				response := map[string]interface{}{
					"data": articleMap,
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				// News not found - return 200 OK with error object (matching X API behavior)
				return formatResourceNotFoundError("news", "id", newsID), http.StatusOK
			}
		}
	}

	// Notes endpoints

	// GET /2/notes/search/notes_written
	if method == "GET" && path == "/2/notes/search/notes_written" {
		authorID := r.URL.Query().Get("author_id")
		if authorID == "" {
			authorID = "0" // Default to playground user
		}
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		notes := state.SearchNotesWritten(authorID, limit)
		notesData := make([]map[string]interface{}, len(notes))
		for i, note := range notes {
			notesData[i] = map[string]interface{}{
				"id":         note.ID,
				"text":       note.Text,
				"author_id":  note.AuthorID,
				"created_at": note.CreatedAt.Format(time.RFC3339),
			}
			if note.PostID != "" {
				notesData[i]["post_id"] = note.PostID
			}
		}
		response := map[string]interface{}{
			"data": notesData,
			"meta": map[string]interface{}{
				"result_count": len(notesData),
			},
		}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
	}

	// GET /2/notes/search/posts_eligible_for_notes
	if method == "GET" && path == "/2/notes/search/posts_eligible_for_notes" {
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		tweets := state.SearchPostsEligibleForNotes(limit)
		return formatTweetsResponse(tweets, queryParams, state, spec, ""), http.StatusOK
	}

	// POST /2/notes
	if method == "POST" && path == "/2/notes" {
		var req struct {
			Text   string `json:"text"`
			PostID string `json:"post_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Text != "" {
			authorID := "0" // Default to playground user
			note := state.CreateNote(req.Text, authorID, req.PostID)
			noteMap := map[string]interface{}{
				"id":         note.ID,
				"text":       note.Text,
				"author_id":  note.AuthorID,
				"created_at": note.CreatedAt.Format(time.RFC3339),
			}
			if note.PostID != "" {
				noteMap["post_id"] = note.PostID
			}
			response := map[string]interface{}{
				"data": noteMap,
			}
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
	}

	// DELETE /2/notes/{id}
	if method == "DELETE" && strings.HasPrefix(path, "/2/notes/") {
		noteID := strings.TrimPrefix(path, "/2/notes/")
		if noteID != "" {
			if state.DeleteNote(noteID) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"deleted": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("note", "id", noteID), http.StatusOK
			}
		}
	}

	// POST /2/evaluate_note
	if method == "POST" && path == "/2/evaluate_note" {
		var req struct {
			NoteID string `json:"note_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.NoteID != "" {
			note := state.GetNote(req.NoteID)
			if note != nil {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"note_id": req.NoteID,
						"evaluated": true,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			} else {
				return formatResourceNotFoundError("note", "id", req.NoteID), http.StatusOK
			}
		}
	}

	// Trends & Insights endpoints

	// GET /2/trends/by/woeid/{woeid}
	if method == "GET" && strings.HasPrefix(path, "/2/trends/by/woeid/") {
		log.Printf("DEBUG trends: Matched trends endpoint for path='%s'", path)
		woeid := strings.TrimPrefix(path, "/2/trends/by/woeid/")
		// Remove query params from woeid if present
		if idx := strings.Index(woeid, "?"); idx != -1 {
			woeid = woeid[:idx]
		}
		log.Printf("DEBUG trends: Extracted woeid='%s'", woeid)
		if woeid != "" {
			// Parse max_trends query parameter (default: no limit, but we'll cap at reasonable number)
			maxTrends := 50 // Default max if not specified
			if maxTrendsStr := r.URL.Query().Get("max_trends"); maxTrendsStr != "" {
				if parsed, err := strconv.Atoi(maxTrendsStr); err == nil && parsed > 0 {
					maxTrends = parsed
				}
			}
			
			// Calculate trends from state data (hashtags)
			allTweets := state.GetAllTweets()
			hashtagCounts := make(map[string]int)
			for _, tweet := range allTweets {
				if tweet.Entities != nil && tweet.Entities.Hashtags != nil {
					for _, hashtag := range tweet.Entities.Hashtags {
						hashtagCounts[hashtag.Tag]++
					}
				}
			}
			
			type trend struct {
				Name  string
				Count int
			}
			trends := make([]trend, 0, len(hashtagCounts))
			for name, count := range hashtagCounts {
				trends = append(trends, trend{Name: name, Count: count})
			}
			
			// Sort by count (descending) - use proper sorting
			for i := 0; i < len(trends)-1; i++ {
				for j := i + 1; j < len(trends); j++ {
					if trends[j].Count > trends[i].Count {
						trends[i], trends[j] = trends[j], trends[i]
					}
				}
			}
			
			// Limit to max_trends
			if len(trends) > maxTrends {
				trends = trends[:maxTrends]
			}
			
			// Use correct field names: trend_name and tweet_count (matching real API)
			trendsData := make([]map[string]interface{}, len(trends))
			for i, t := range trends {
				trendsData[i] = map[string]interface{}{
					"trend_name": t.Name,
					"tweet_count": t.Count,
				}
			}
			response := map[string]interface{}{
				"data": trendsData,
			}
			
			// Apply field filtering using the standard filter function
			// This handles trend.fields parameter properly
			response = filterResponseByQueryParams(response, queryParams, op)
			
			log.Printf("DEBUG trends: Returning %d trends (max_trends=%d), response has %d items in data array", len(trends), maxTrends, len(trendsData))
			data, statusCode := MarshalJSONResponse(response)
			return data, statusCode
		}
		log.Printf("DEBUG trends: woeid was empty after extraction, path='%s'", path)
	}
	log.Printf("DEBUG trends: Trends endpoint not matched, path='%s', method='%s'", path, method)

	// Activity subscription endpoints removed - not ready to support yet


	// GET /2/users/{id}/following - Get users that a user follows
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/following") && !strings.Contains(path, "/following/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/following")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				followingUsers := state.GetUsers(user.Following)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				followingUsers, nextToken, err := applyUserPagination(followingUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(followingUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/followers - Get users following a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/followers") && !strings.Contains(path, "/followers/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/followers")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				followerUsers := state.GetUsers(user.Followers)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				followerUsers, nextToken, err := applyUserPagination(followerUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(followerUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/likes - Get tweets liked by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/likes") && !strings.Contains(path, "/likes/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/likes")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				likedTweets := state.GetTweets(user.LikedTweets)
				// Apply pagination
				limit := 10
				if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
					if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
						limit = parsed
					}
				}
				if len(likedTweets) > limit {
					likedTweets = likedTweets[:limit]
				}
				return formatStateDataToOpenAPI(likedTweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/tweets - Get tweets by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/tweets") && !strings.Contains(path, "/tweets/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/tweets")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				userTweets := state.GetTweets(user.Tweets)
				// Apply pagination
				limit := 10
				if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
					if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
						limit = parsed
					}
				}
				if len(userTweets) > limit {
					userTweets = userTweets[:limit]
				}
				return formatStateDataToOpenAPI(userTweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/retweets - Get retweets by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/retweets") && !strings.Contains(path, "/retweets/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/retweets")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				retweetedTweets := state.GetTweets(user.RetweetedTweets)
				// Apply pagination
				limit := 10
				if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
					if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
						limit = parsed
					}
				}
				if len(retweetedTweets) > limit {
					retweetedTweets = retweetedTweets[:limit]
				}
				return formatStateDataToOpenAPI(retweetedTweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/bookmarks - Get bookmarked tweets by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/bookmarks") && !strings.Contains(path, "/bookmarks/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/bookmarks")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				bookmarkedTweets := state.GetTweets(user.BookmarkedTweets)
				// Apply pagination
				limit := 10
				if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
					if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
						limit = parsed
					}
				}
				if len(bookmarkedTweets) > limit {
					bookmarkedTweets = bookmarkedTweets[:limit]
				}
				return formatStateDataToOpenAPI(bookmarkedTweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/blocking - Get users blocked by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/blocking") && !strings.Contains(path, "/blocking/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/blocking")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				blockedUsers := state.GetUsers(user.BlockedUsers)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				blockedUsers, nextToken, err := applyUserPagination(blockedUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(blockedUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/muting - Get users muted by a user
	if method == "GET" && strings.Contains(path, "/users/") && strings.HasSuffix(path, "/muting") && !strings.Contains(path, "/muting/") {
		userID := extractPathParam(path, "/2/users/")
		userID = strings.TrimSuffix(userID, "/muting")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				mutedUsers := state.GetUsers(user.MutedUsers)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				mutedUsers, nextToken, err := applyUserPagination(mutedUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(mutedUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/tweets/{id}/liking_users - Get users who liked a tweet
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/liking_users") {
		tweetID := extractPathParam(path, "/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/liking_users")
		if tweetID != "" {
			tweet := state.GetTweet(tweetID)
			if tweet != nil {
				likingUsers := state.GetUsers(tweet.LikedBy)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				likingUsers, nextToken, err := applyUserPagination(likingUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(likingUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
			}
		}
	}

	// GET /2/tweets/{id}/retweeted_by - Get users who retweeted a tweet
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/retweeted_by") {
		tweetID := extractPathParam(path, "/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/retweeted_by")
		if tweetID != "" {
			tweet := state.GetTweet(tweetID)
			if tweet != nil {
				retweetingUsers := state.GetUsers(tweet.RetweetedBy)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				retweetingUsers, nextToken, err := applyUserPagination(retweetingUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(retweetingUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("tweet", "id", tweetID), http.StatusOK
			}
		}
	}

	// GET /2/lists/{id}/members - Get members of a list
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/members") && !strings.Contains(path, "/members/") {
		listID := extractPathParam(path, "/2/lists/")
		listID = strings.TrimSuffix(listID, "/members")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				memberUsers := state.GetUsers(list.Members)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				memberUsers, nextToken, err := applyUserPagination(memberUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(memberUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("list", "id", listID), http.StatusOK
			}
		}
	}

	// GET /2/lists/{id}/followers - Get followers of a list
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/followers") && !strings.Contains(path, "/followers/") {
		listID := extractPathParam(path, "/2/lists/")
		listID = strings.TrimSuffix(listID, "/followers")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				followerUsers := state.GetUsers(list.Followers)
				var opForPagination *Operation
				if op != nil {
					opForPagination = op.Operation
				}
				followerUsers, nextToken, err := applyUserPagination(followerUsers, r, opForPagination, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data := marshalFallbackError(errorResp)
					statusCode := http.StatusBadRequest
					return data, statusCode
				}
				return formatUsersResponse(followerUsers, queryParams, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("list", "id", listID), http.StatusOK
			}
		}
	}

	// GET /2/lists/{id}/tweets - Get tweets from a list (tweets by list members)
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/tweets") && !strings.Contains(path, "/tweets/") {
		listID := extractPathParam(path, "/2/lists/")
		listID = strings.TrimSuffix(listID, "/tweets")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				// Get tweets from all list members
				var allTweets []*Tweet
				for _, memberID := range list.Members {
					member := state.GetUserByID(memberID)
					if member != nil {
						memberTweets := state.GetTweets(member.Tweets)
						allTweets = append(allTweets, memberTweets...)
					}
				}
				// Sort by created_at descending (newest first)
				sort.Slice(allTweets, func(i, j int) bool {
					return allTweets[i].CreatedAt.After(allTweets[j].CreatedAt)
				})
				// Apply pagination
				limit := 10
				if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
					if parsed, err := strconv.Atoi(limitStr); err == nil && parsed >= 5 && parsed <= 100 {
						limit = parsed
					}
				}
				if len(allTweets) > limit {
					allTweets = allTweets[:limit]
				}
				return formatStateDataToOpenAPI(allTweets, op, spec, queryParams, state), http.StatusOK
			} else {
				return formatResourceNotFoundError("list", "id", listID), http.StatusOK
			}
		}
	}

	return nil, 0 // Not a stateful operation, use OpenAPI schema generation
}

// formatStateDataToOpenAPI formats state data using OpenAPI schema structure
// It respects query parameters for field filtering and only returns default fields when none are specified
func formatStateDataToOpenAPI(data interface{}, op *EndpointOperation, spec *OpenAPISpec, queryParams *QueryParams, state *State) []byte {
	// Convert state data to map
	var dataMap map[string]interface{}
	var isArray bool
	var arrayData []map[string]interface{}
	
		if user, ok := data.(*User); ok {
		dataMap = FormatUser(user)
		// Apply field filtering - default fields: id, name, username (matching X API)
		if queryParams != nil {
			if len(queryParams.UserFields) > 0 {
				dataMap = filterUserFields(dataMap, queryParams.UserFields)
			} else {
				// Default fields only
				dataMap = filterUserFields(dataMap, []string{"id", "name", "username"})
			}
		} else {
			// No query params - default fields only
			dataMap = filterUserFields(dataMap, []string{"id", "name", "username"})
		}
		// Add expansion fields to user if expansions are requested
		if queryParams != nil && len(queryParams.Expansions) > 0 {
			addExpansionFieldsToUser(dataMap, user, queryParams.Expansions)
		}
	} else if tweet, ok := data.(*Tweet); ok {
		if tweet == nil {
			// Return empty data if tweet is nil
			dataMap = make(map[string]interface{})
		} else {
			dataMap = FormatTweet(tweet)
			// Apply field filtering - default fields: id, text (matching X API)
			// filterTweetFields now always includes id and text, so we can just pass requested fields
			if queryParams != nil {
				if len(queryParams.TweetFields) > 0 {
					dataMap = filterTweetFields(dataMap, queryParams.TweetFields)
				} else {
					// Default fields only (id, text are included automatically)
					dataMap = filterTweetFields(dataMap, []string{})
				}
			} else {
				// No query params - default fields only (id, text are included automatically)
				dataMap = filterTweetFields(dataMap, []string{})
			}
			// Add expansion fields to tweet if expansions are requested
			if queryParams != nil && len(queryParams.Expansions) > 0 {
				addExpansionFieldsToTweet(dataMap, tweet, queryParams.Expansions)
			}
		}
	} else if media, ok := data.(*Media); ok {
		dataMap = formatMedia(media)
	} else if mediaList, ok := data.([]*Media); ok {
		isArray = true
		arrayData = make([]map[string]interface{}, len(mediaList))
		for i, m := range mediaList {
			arrayData[i] = formatMedia(m)
		}
		// Media fields are usually minimal, no filtering needed
	} else if dmEvent, ok := data.(*DMEvent); ok {
		dataMap = formatDMEvent(dmEvent)
	} else if lists, ok := data.([]*List); ok {
		isArray = true
		arrayData = make([]map[string]interface{}, len(lists))
		for i, l := range lists {
			listMap := formatList(l)
			// Always apply field filtering - if no fields specified, returns only defaults (id, name)
			requestedFields := []string{}
			if queryParams != nil && len(queryParams.ListFields) > 0 {
				requestedFields = queryParams.ListFields
			}
			arrayData[i] = filterListFields(listMap, requestedFields)
		}
	} else if list, ok := data.(*List); ok {
		dataMap = formatList(list)
		// Always apply field filtering - if no fields specified, returns only defaults (id, name)
		requestedFields := []string{}
		if queryParams != nil && len(queryParams.ListFields) > 0 {
			requestedFields = queryParams.ListFields
		}
		dataMap = filterListFields(dataMap, requestedFields)
	} else if space, ok := data.(*Space); ok {
		dataMap = formatSpace(space)
		// Always apply field filtering - if no fields specified, returns only defaults (id, state)
		requestedFields := []string{}
		if queryParams != nil && len(queryParams.SpaceFields) > 0 {
			requestedFields = queryParams.SpaceFields
		}
		dataMap = filterSpaceFields(dataMap, requestedFields)
	} else if users, ok := data.([]*User); ok {
		isArray = true
		arrayData = make([]map[string]interface{}, len(users))
		for i, u := range users {
			userMap := FormatUser(u)
			// Apply field filtering
			if queryParams != nil {
				if len(queryParams.UserFields) > 0 {
					userMap = filterUserFields(userMap, queryParams.UserFields)
				} else {
					userMap = filterUserFields(userMap, []string{"id", "name", "username"})
				}
		} else {
			userMap = filterUserFields(userMap, []string{"id", "name", "username"})
		}
		// Add expansion fields to user if expansions are requested
		if queryParams != nil && len(queryParams.Expansions) > 0 {
			addExpansionFieldsToUser(userMap, u, queryParams.Expansions)
		}
		arrayData[i] = userMap
	}
	} else if tweets, ok := data.([]*Tweet); ok {
		isArray = true
		arrayData = make([]map[string]interface{}, len(tweets))
		for i, t := range tweets {
			tweetMap := FormatTweet(t)
			// Apply field filtering
			if queryParams != nil {
				if len(queryParams.TweetFields) > 0 {
					tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
				} else {
					tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
				}
			} else {
				tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
			}
			// Add expansion fields to tweet if expansions are requested
			if queryParams != nil && len(queryParams.Expansions) > 0 {
				addExpansionFieldsToTweet(tweetMap, t, queryParams.Expansions)
			}
			arrayData[i] = tweetMap
		}
	} else {
		// Fallback: convert to JSON and back
		jsonData, _ := json.Marshal(data)
		json.Unmarshal(jsonData, &dataMap)
	}

	// Build response - NO includes unless expansions are explicitly requested
	response := map[string]interface{}{}
	if isArray {
		response["data"] = arrayData
		response["meta"] = map[string]interface{}{
			"result_count": len(arrayData),
		}
	} else {
		// Ensure dataMap is never nil
		if dataMap == nil {
			dataMap = make(map[string]interface{})
		}
		response["data"] = dataMap
	}

	// Only add includes if expansions are explicitly requested
	if queryParams != nil && len(queryParams.Expansions) > 0 && state != nil {
		includes := make(map[string]interface{})
		
		// Handle expansions for tweets
		var tweetsForExpansion []*Tweet
		if tweet, ok := data.(*Tweet); ok {
			tweetsForExpansion = []*Tweet{tweet}
		} else if tweets, ok := data.([]*Tweet); ok {
			tweetsForExpansion = tweets
		}
		
		if len(tweetsForExpansion) > 0 {
			tweetIncludes := buildExpansions(tweetsForExpansion, queryParams.Expansions, state, spec, queryParams)
			// Merge tweet expansions into includes
			for k, v := range tweetIncludes {
				includes[k] = v
			}
		}
		
		// Handle expansions for users (e.g., pinned_tweet_id)
		var usersForExpansion []*User
		if user, ok := data.(*User); ok {
			usersForExpansion = []*User{user}
		} else if users, ok := data.([]*User); ok {
			usersForExpansion = users
		}
		
		if len(usersForExpansion) > 0 {
			userIncludes := buildUserExpansions(usersForExpansion, queryParams.Expansions, state, spec, queryParams)
			// Merge user expansions into includes
			for k, v := range userIncludes {
				// Merge arrays if key already exists (e.g., tweets from both tweets and users)
				if existing, ok := includes[k].([]map[string]interface{}); ok {
					if newItems, ok := v.([]map[string]interface{}); ok {
						// Avoid duplicates
						seenIDs := make(map[string]bool)
						for _, item := range existing {
							if id, ok := item["id"].(string); ok {
								seenIDs[id] = true
							}
						}
						for _, item := range newItems {
							if id, ok := item["id"].(string); ok && !seenIDs[id] {
								existing = append(existing, item)
								seenIDs[id] = true
							}
						}
						includes[k] = existing
					}
				} else {
					includes[k] = v
				}
			}
		}
		
		// Always add includes object (even if empty) when expansions are requested
		// This matches real API behavior where includes is always present when requested
		response["includes"] = includes
	}

	jsonData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil
	}

	return jsonData
}

// addExpansionFieldsToTweet adds expansion-related fields to a tweet map when expansions are requested
// This ensures that when an expansion is requested, the corresponding field appears in the tweet object
func addExpansionFieldsToTweet(tweetMap map[string]interface{}, tweet *Tweet, expansions []string) {
	for _, exp := range expansions {
		switch exp {
		case "author_id":
			// Add author_id field if not already present
			if _, exists := tweetMap["author_id"]; !exists && tweet.AuthorID != "" {
				tweetMap["author_id"] = tweet.AuthorID
			}
		case "in_reply_to_user_id":
			// Add in_reply_to_user_id field if not already present
			if _, exists := tweetMap["in_reply_to_user_id"]; !exists && tweet.InReplyToID != "" {
				tweetMap["in_reply_to_user_id"] = tweet.InReplyToID
			}
		case "referenced_tweets.id":
			// Add referenced_tweets field if not already present
			if _, exists := tweetMap["referenced_tweets"]; !exists && len(tweet.ReferencedTweets) > 0 {
				tweetMap["referenced_tweets"] = tweet.ReferencedTweets
			}
		case "attachments.media_keys", "media_keys":
			// Ensure attachments object exists and has media_keys
			if tweet.Attachments != nil && len(tweet.Attachments.MediaKeys) > 0 {
				attachments, ok := tweetMap["attachments"].(map[string]interface{})
				if !ok {
					// Create attachments object if it doesn't exist
					attachments = make(map[string]interface{})
					tweetMap["attachments"] = attachments
				}
				if _, exists := attachments["media_keys"]; !exists {
					attachments["media_keys"] = tweet.Attachments.MediaKeys
				}
			}
		case "attachments.poll_ids", "poll_ids":
			// Ensure attachments object exists and has poll_ids
			if tweet.Attachments != nil && len(tweet.Attachments.PollIDs) > 0 {
				attachments, ok := tweetMap["attachments"].(map[string]interface{})
				if !ok {
					// Create attachments object if it doesn't exist
					attachments = make(map[string]interface{})
					tweetMap["attachments"] = attachments
				}
				if _, exists := attachments["poll_ids"]; !exists {
					attachments["poll_ids"] = tweet.Attachments.PollIDs
				}
			} else if tweet.PollID != "" {
				// Handle legacy PollID field
				attachments, ok := tweetMap["attachments"].(map[string]interface{})
				if !ok {
					attachments = make(map[string]interface{})
					tweetMap["attachments"] = attachments
				}
				if _, exists := attachments["poll_ids"]; !exists {
					attachments["poll_ids"] = []string{tweet.PollID}
				}
			}
		case "geo.place_id":
			// Add geo.place_id field if tweet has a place
			if tweet.PlaceID != "" {
				geo, ok := tweetMap["geo"].(map[string]interface{})
				if !ok {
					geo = make(map[string]interface{})
					tweetMap["geo"] = geo
				}
				if _, exists := geo["place_id"]; !exists {
					geo["place_id"] = tweet.PlaceID
				}
			}
		}
	}
}

// addExpansionFieldsToUser adds expansion-related fields to a user map when expansions are requested
// This ensures that when an expansion is requested, the corresponding field appears in the user object
func addExpansionFieldsToUser(userMap map[string]interface{}, user *User, expansions []string) {
	for _, exp := range expansions {
		switch exp {
		case "pinned_tweet_id":
			// Add pinned_tweet_id field if not already present
			if _, exists := userMap["pinned_tweet_id"]; !exists && user.PinnedTweetID != "" {
				userMap["pinned_tweet_id"] = user.PinnedTweetID
			}
		}
	}
}

// formatDMEvent formats a DM event for response
func formatDMEvent(event *DMEvent) map[string]interface{} {
	eventMap := map[string]interface{}{
		"id":                 event.ID,
		"event_type":         event.EventType,
		"dm_conversation_id": event.DMConversationID,
		"sender_id":          event.SenderID,
		"created_at":         event.CreatedAt.Format(time.RFC3339),
		"participant_ids":    event.ParticipantIDs,
	}
	if event.Text != "" {
		eventMap["text"] = event.Text
	}
	if event.Attachments != nil {
		eventMap["attachments"] = event.Attachments
	}
	if event.Entities != nil {
		eventMap["entities"] = event.Entities
	}
	if len(event.ReferencedTweets) > 0 {
		eventMap["referenced_tweets"] = event.ReferencedTweets
	}
	return eventMap
}

// formatMedia formats media for response
func formatMedia(media *Media) map[string]interface{} {
	result := map[string]interface{}{
		"id":                media.ID,
		"media_key":         media.MediaKey,
		"expires_after_secs": media.ExpiresAfterSecs,
	}

	if media.Type != "" {
		result["type"] = media.Type
	}
	if media.URL != "" {
		result["url"] = media.URL
	}
	if media.Width > 0 {
		result["width"] = media.Width
	}
	if media.Height > 0 {
		result["height"] = media.Height
	}
	if media.DurationMs > 0 {
		result["duration_ms"] = media.DurationMs
	}
	if media.PreviewImageURL != "" {
		result["preview_image_url"] = media.PreviewImageURL
	}
	if media.AltText != "" {
		result["alt_text"] = media.AltText
	}
	if media.ProcessingInfo != nil {
		result["processing_info"] = media.ProcessingInfo
	}

	return result
}

// formatSearchTweetsResponse formats search results with newest_id/oldest_id in meta
func formatSearchTweetsResponse(tweets []*Tweet, queryParams *QueryParams, state *State, spec *OpenAPISpec, limit int) []byte {
	response := map[string]interface{}{
		"data": make([]map[string]interface{}, 0),
	}

	// Format tweets
	tweetData := make([]map[string]interface{}, 0, len(tweets))
	for _, tweet := range tweets {
		tweetMap := FormatTweet(tweet)
		// Apply field filtering if specified
		if queryParams != nil && len(queryParams.TweetFields) > 0 {
			tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
		} else {
			// Default fields for search
			tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
		}
		tweetData = append(tweetData, tweetMap)
	}
	response["data"] = tweetData

	// Add search-specific meta with newest_id/oldest_id
	meta := map[string]interface{}{
		"result_count": len(tweetData),
	}
	if len(tweets) > 0 {
		// Find newest and oldest by created_at
		newestID := tweets[0].ID
		oldestID := tweets[0].ID
		newestTime := tweets[0].CreatedAt
		oldestTime := tweets[0].CreatedAt
		
		for _, tweet := range tweets {
			if tweet.CreatedAt.After(newestTime) {
				newestID = tweet.ID
				newestTime = tweet.CreatedAt
			}
			if tweet.CreatedAt.Before(oldestTime) {
				oldestID = tweet.ID
				oldestTime = tweet.CreatedAt
			}
		}
		meta["newest_id"] = newestID
		meta["oldest_id"] = oldestID
		
		// Add next_token if we got the full limit (simplified pagination)
		// In a real implementation, this would check if there are more results available
		// For now, we'll add next_token if we got exactly the requested limit
		if len(tweets) == limit {
			// Use realistic base64-encoded token format (function defined in relationship_handlers.go)
			meta["next_token"] = encodePaginationToken(len(tweetData))
		}
	} else {
		// Empty result - still include meta
		meta["newest_id"] = ""
		meta["oldest_id"] = ""
	}
	response["meta"] = meta

	// Handle expansions
	if queryParams != nil && len(queryParams.Expansions) > 0 && state != nil {
		includes := buildExpansions(tweets, queryParams.Expansions, state, spec, queryParams)
		// Always add includes object (even if empty) when expansions are requested
		// This matches real API behavior where includes is always present when requested
		response["includes"] = includes
	}

	data, _ := MarshalJSONResponse(response)
	return data
}

// formatArrayResponse formats an array response
func formatArrayResponse(items []map[string]interface{}, schema map[string]interface{}) []byte {
	response := map[string]interface{}{
		"data": items,
		"meta": map[string]interface{}{
			"result_count": len(items),
		},
	}

	jsonData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil
	}

	return jsonData
}

// extractPathParam extracts a parameter from a path
func extractPathParam(path, prefix string) string {
	path = strings.TrimPrefix(path, prefix)
	// Remove query parameters
	path = strings.Split(path, "?")[0]
	// Remove trailing slashes
	path = strings.TrimSuffix(path, "/")
	return path
}

// extractMediaIDFromPath extracts media ID from upload path
func extractMediaIDFromPath(path string) string {
	if !strings.Contains(path, "/2/media/upload/") {
		return ""
	}

	parts := strings.Split(path, "/2/media/upload/")
	if len(parts) < 2 {
		return ""
	}

	pathPart := parts[1]
	// Remove /append or /finalize
	pathPart = strings.TrimSuffix(pathPart, "/append")
	pathPart = strings.TrimSuffix(pathPart, "/finalize")
	pathPart = strings.TrimSuffix(pathPart, "/")

	return pathPart
}

// setupFallbackHandlers sets up basic handlers if OpenAPI spec is not available
func setupFallbackHandlers(mux *http.ServeMux, state *State) {
	mux.HandleFunc("/2/users/me", handleGetMe(state))
	mux.HandleFunc("/2/tweets", handleTweets(state))
	mux.HandleFunc("/2/oauth2/token", handleOAuthToken(state))
}

