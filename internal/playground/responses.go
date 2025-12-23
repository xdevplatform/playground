// Package playground provides utility functions for generating HTTP responses.
//
// This file contains helpers for writing JSON responses, setting X API headers
// (including request IDs and rate limit information), and handling errors safely.
// It ensures consistent response formatting and robust error handling across
// all API endpoints.
package playground

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// WriteJSON writes a JSON response.
// Returns an error if JSON encoding fails.
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	AddXAPIHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteJSONSafe writes a JSON response and handles errors.
// If encoding fails, writes an error response with request ID if available.
// This function ensures that a response is always sent, even if JSON encoding fails.
func WriteJSONSafe(w http.ResponseWriter, statusCode int, data interface{}) {
	if err := WriteJSON(w, statusCode, data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		// Check if headers were already written - if so, we can't send a different status
		headersWritten := false
		if rtw, ok := w.(*responseTimeWriter); ok {
			headersWritten = rtw.written
		}
		
		// If headers were already written, we can't send a different response
		// Just log the error and return
		if headersWritten {
			log.Printf("Warning: Cannot send fallback error response - headers already written")
			return
		}
		
		// Try to write a fallback error response with request ID
		requestID := w.Header().Get("x-request-id")
		if requestID == "" {
			requestID = generateRequestID()
			w.Header().Set("x-request-id", requestID)
		}
		fallbackError := map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"message": "Internal server error",
					"code":    500,
				},
			},
		}
		// Try to write JSON error, fall back to plain text if that also fails
		if jsonErr := WriteJSON(w, http.StatusInternalServerError, fallbackError); jsonErr != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

// marshalFallbackError safely marshals a fallback error response.
// If marshaling fails, returns a hardcoded error response as a last resort.
// This ensures that error responses can always be sent even if JSON marshaling fails.
func marshalFallbackError(fallback map[string]interface{}) []byte {
	data, err := json.Marshal(fallback)
	if err != nil {
		log.Printf("Error marshaling fallback error response: %v", err)
		// Use a simple hardcoded error response as last resort
		return []byte(`{"errors":[{"message":"Internal server error","code":500}]}`)
	}
	return data
}

// MarshalJSONResponse safely marshals a response, handling errors.
// Returns the marshaled JSON bytes and the appropriate HTTP status code.
// If marshaling fails, returns a fallback error response.
func MarshalJSONResponse(response interface{}) ([]byte, int) {
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		errorResp := map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"message": "Internal server error",
					"code":    500,
				},
			},
		}
		fallbackData, fallbackErr := json.Marshal(errorResp)
		if fallbackErr != nil {
			log.Printf("Error marshaling fallback error response: %v", fallbackErr)
			return []byte(`{"errors":[{"message":"Internal server error","code":500}]}`), http.StatusInternalServerError
		}
		return fallbackData, http.StatusInternalServerError
	}
	return data, http.StatusOK
}

// MarshalJSONErrorResponse safely marshals an error response, handling errors.
// Returns the marshaled JSON bytes and the appropriate HTTP status code.
// If marshaling fails, returns a fallback error response.
func MarshalJSONErrorResponse(errorResp interface{}) ([]byte, int) {
	data, err := json.MarshalIndent(errorResp, "", "  ")
	if err != nil {
		log.Printf("Error marshaling error response: %v", err)
		// Return a fallback error response
		fallback := map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"message": "Internal server error",
					"code":    500,
				},
			},
		}
		fallbackData, fallbackErr := json.Marshal(fallback)
		if fallbackErr != nil {
			log.Printf("Error marshaling fallback error response: %v", fallbackErr)
			return []byte(`{"errors":[{"message":"Internal server error","code":500}]}`), http.StatusInternalServerError
		}
		return fallbackData, http.StatusInternalServerError
	}
	return data, http.StatusBadRequest
}

// setResponseTimeHeader sets the x-response-time header from tracked time.
// If response time cannot be determined, the header is omitted rather than using an arbitrary value.
func setResponseTimeHeader(w http.ResponseWriter) {
	var responseTimeMs int64
	var hasResponseTime bool
	
	// First check if header was already set by handler
	responseTimeStr := w.Header().Get("X-Internal-Response-Time-Ms")
	if responseTimeStr != "" {
		// Header was set by handler before AddXAPIHeaders was called
		if ms, err := strconv.ParseInt(responseTimeStr, 10, 64); err == nil {
			responseTimeMs = ms
			hasResponseTime = true
		}
		w.Header().Del("X-Internal-Response-Time-Ms") // Remove internal header
	} else if rtw, ok := w.(*responseTimeWriter); ok {
		// Calculate time now if using responseTimeWriter (even if WriteHeader hasn't been called)
		responseTimeMs = time.Since(rtw.startTime).Milliseconds()
		hasResponseTime = true
		// Store it for later use when WriteHeader is called
		w.Header().Set("X-Internal-Response-Time-Ms", strconv.FormatInt(responseTimeMs, 10))
	}
	
	if hasResponseTime {
		// Use tracked response time (even if 0ms - very fast response)
		w.Header().Set("x-response-time", strconv.FormatInt(responseTimeMs, 10))
	}
	// If response time cannot be determined, omit the header rather than using an arbitrary value
}

// AddXAPIHeaders adds standard X API response headers.
// If rateLimitConfig is provided, uses dynamic values; otherwise uses defaults.
func AddXAPIHeaders(w http.ResponseWriter) {
	// Generate and add request ID header
	requestID := generateRequestID()
	w.Header().Set("x-request-id", requestID)
	
	// Call the rate limit version with nil config to use defaults
	AddXAPIHeadersWithRateLimit(w, nil, 0, time.Time{})
}

// generateRequestID generates a unique request ID for tracking requests.
// Uses 12 random bytes encoded as base64 with a timestamp prefix for uniqueness.
// Falls back to timestamp-based ID if crypto/rand fails.
func generateRequestID() string {
	// Generate 12 random bytes
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
	}
	// Encode to base64 and add timestamp prefix for uniqueness
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%d-%s", timestamp, base64.URLEncoding.EncodeToString(b))
}

// AddXAPIHeadersWithRateLimit adds X API headers with dynamic rate limit values.
// Ensures request ID is set and includes rate limit headers if configuration is provided.
func AddXAPIHeadersWithRateLimit(w http.ResponseWriter, rateLimitConfig *RateLimitConfig, remaining int, resetTime time.Time) {
	
	// Ensure request ID is set (in case this is called directly)
	if w.Header().Get("x-request-id") == "" {
		requestID := generateRequestID()
		w.Header().Set("x-request-id", requestID)
	}
	
	// Rate limiting headers
	if rateLimitConfig != nil && rateLimitConfig.Enabled {
		w.Header().Set("x-rate-limit-limit", fmt.Sprintf("%d", rateLimitConfig.Limit))
		if remaining >= 0 {
			w.Header().Set("x-rate-limit-remaining", fmt.Sprintf("%d", remaining))
		} else {
			w.Header().Set("x-rate-limit-remaining", fmt.Sprintf("%d", rateLimitConfig.Limit))
		}
		if !resetTime.IsZero() {
			w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", resetTime.Unix()))
		} else {
			w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", time.Now().Add(15*time.Minute).Unix()))
		}
	} else {
		// Default static values
		w.Header().Set("x-rate-limit-limit", "15")
		w.Header().Set("x-rate-limit-remaining", "14")
		w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", time.Now().Add(15*time.Minute).Unix()))
	}
	
	// Set response time header (shared logic)
	setResponseTimeHeader(w)
	
	// Other X API headers
	w.Header().Set("x-access-level", "read")
	
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("x-frame-options", "SAMEORIGIN")
	w.Header().Set("x-xss-protection", "0")
	w.Header().Set("strict-transport-security", "max-age=300")
}

// AddStreamingHeaders adds comprehensive headers for streaming endpoints to match real X API behavior
func AddStreamingHeaders(w http.ResponseWriter, r *http.Request, state *State) {
	// Generate request ID and transaction ID
	requestID := generateRequestID()
	w.Header().Set("x-request-id", requestID)
	
	// Generate transaction ID (similar format to request ID)
	transactionID := generateTransactionID()
	w.Header().Set("x-transaction-id", transactionID)
	
	// Server identification
	w.Header().Set("server", "envoy")
	
	// Performance header (random value similar to real API)
	w.Header().Set("perf", generatePerfHeader())
	
	// Cache control (already set in streaming.go, but ensure it's here too)
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0")
	
	// Set-Cookie headers for session tracking (simulate guest session)
	guestID := generateGuestID()
	cookieExpires := time.Now().Add(63072000 * time.Second) // 2 years
	expiresStr := cookieExpires.UTC().Format(http.TimeFormat)
	
	// Set multiple cookies similar to real API
	cookies := []string{
		fmt.Sprintf("guest_id_marketing=v1%%3A%s; Max-Age=63072000; Expires=%s; Path=/; Domain=.twitter.com; Secure; SameSite=None", guestID, expiresStr),
		fmt.Sprintf("guest_id_ads=v1%%3A%s; Max-Age=63072000; Expires=%s; Path=/; Domain=.twitter.com; Secure; SameSite=None", guestID, expiresStr),
		fmt.Sprintf("personalization_id=\"v1_%s\"; Max-Age=63072000; Expires=%s; Path=/; Domain=.twitter.com; Secure; SameSite=None", generatePersonalizationID(), expiresStr),
		fmt.Sprintf("guest_id=v1%%3A%s; Max-Age=63072000; Expires=%s; Path=/; Domain=.twitter.com; Secure; SameSite=None", guestID, expiresStr),
	}
	for _, cookie := range cookies {
		w.Header().Add("Set-Cookie", cookie)
	}
	
	// Access level
	w.Header().Set("x-access-level", "read")
	
	// Security headers
	w.Header().Set("x-frame-options", "SAMEORIGIN")
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("x-xss-protection", "0")
	w.Header().Set("Strict-Transport-Security", "max-age=300")
	
	// Rate limiting headers (use state config if available)
	if state != nil && state.config != nil {
		rateLimitConfig := state.config.GetRateLimitConfig()
		if rateLimitConfig != nil && rateLimitConfig.Enabled {
			w.Header().Set("x-rate-limit-limit", fmt.Sprintf("%d", rateLimitConfig.Limit))
			w.Header().Set("x-rate-limit-remaining", fmt.Sprintf("%d", rateLimitConfig.Limit-1))
			resetTime := time.Now().Add(time.Duration(rateLimitConfig.WindowSec) * time.Second)
			w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", resetTime.Unix()))
		} else {
			// Default values
			w.Header().Set("x-rate-limit-limit", "450")
			w.Header().Set("x-rate-limit-remaining", "449")
			resetTime := time.Now().Add(15 * time.Minute)
			w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", resetTime.Unix()))
		}
	} else {
		// Default values
		w.Header().Set("x-rate-limit-limit", "450")
		w.Header().Set("x-rate-limit-remaining", "449")
		resetTime := time.Now().Add(15 * time.Minute)
		w.Header().Set("x-rate-limit-reset", fmt.Sprintf("%d", resetTime.Unix()))
	}
	
	// Accept-Ranges
	w.Header().Set("Accept-Ranges", "bytes")
	
	// Via header (simulating proxy chain)
	w.Header().Set("Via", "1.1 varnish, 1.1 varnish")
	
	// Response time (will be set by responseTimeWriter, but set a default here)
	if w.Header().Get("x-response-time") == "" {
		w.Header().Set("x-response-time", "100")
	}
	
	// Date header
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	
	// X-Served-By (simulating cache servers)
	w.Header().Set("X-Served-By", "t4_a, cache-pdk-kpdk2140038-PDK, cache-pao-kpao1770055-PAO")
	
	// X-Cache
	w.Header().Set("X-Cache", "MISS, MISS")
	
	// X-Cache-Hits
	w.Header().Set("X-Cache-Hits", "0, 0")
	
	// X-Timer (simulating timing information)
	timestamp := time.Now().Unix()
	w.Header().Set("X-Timer", fmt.Sprintf("S%d.946394,VS0,VE162", timestamp))
}

// generateTransactionID generates a transaction ID similar to X API format
func generateTransactionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%016x", b)
}

// generatePerfHeader generates a performance header value
func generatePerfHeader() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()/1000)
}

// generateGuestID generates a guest ID for cookies
func generateGuestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d", time.Now().UnixNano()/1000)
}

// generatePersonalizationID generates a personalization ID for cookies
func generatePersonalizationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.URLEncoding.EncodeToString(b)
}

// WriteError writes an error response.
// Increments the error request counter and writes a JSON error response.
func WriteError(w http.ResponseWriter, statusCode int, message string, code int) {
	IncrementRequestsError()
	WriteJSONSafe(w, statusCode, CreateErrorResponse(message, code))
}

// GenerateUserResponse generates a user response in X API format.
// Returns nil if user is nil.
func GenerateUserResponse(user *User) map[string]interface{} {
	if user == nil {
		return nil
	}
	return map[string]interface{}{
		"data": FormatUser(user),
	}
}

// GenerateUsersResponse generates a users response in X API format.
// Formats all users and returns them in a data array.
func GenerateUsersResponse(users []*User) map[string]interface{} {
	formatted := make([]map[string]interface{}, len(users))
	for i, user := range users {
		formatted[i] = FormatUser(user)
	}
	return map[string]interface{}{
		"data": formatted,
	}
}

// GenerateTweetResponse generates a tweet response in X API format.
// Returns nil if tweet is nil.
func GenerateTweetResponse(tweet *Tweet) map[string]interface{} {
	if tweet == nil {
		return nil
	}
	return map[string]interface{}{
		"data": FormatTweet(tweet),
	}
}

// GenerateTweetsResponse generates a tweets response in X API format.
// Formats all tweets and includes result count in meta.
func GenerateTweetsResponse(tweets []*Tweet) map[string]interface{} {
	formatted := make([]map[string]interface{}, len(tweets))
	for i, tweet := range tweets {
		formatted[i] = FormatTweet(tweet)
	}
	return map[string]interface{}{
		"data": formatted,
		"meta": map[string]interface{}{
			"result_count": len(tweets),
		},
	}
}

// GenerateMediaInitResponse generates a media initialization response in X API format.
// Returns the media ID, expires_after_secs, and media_key.
func GenerateMediaInitResponse(media *Media) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"id":                media.ID,
			"expires_after_secs": media.ExpiresAfterSecs,
			"media_key":          media.MediaKey,
		},
	}
}

// GenerateMediaStatusResponse generates a media status response in X API format.
// Includes processing_info if available.
func GenerateMediaStatusResponse(media *Media) map[string]interface{} {
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"id":                media.ID,
			"media_key":         media.MediaKey,
			"expires_after_secs": media.ExpiresAfterSecs,
		},
	}

	if media.ProcessingInfo != nil {
		response["data"].(map[string]interface{})["processing_info"] = media.ProcessingInfo
	}

	return response
}

// GenerateOAuthTokenResponse generates an OAuth2 token response.
// Creates a new access token with timestamp-based suffix for uniqueness.
func GenerateOAuthTokenResponse() map[string]interface{} {
	return map[string]interface{}{
		"access_token":  "playground_access_token_" + time.Now().Format("20060102150405"),
		"token_type":    "bearer",
		"expires_in":    7200,
		"refresh_token":  "playground_refresh_token_" + time.Now().Format("20060102150405"),
		"scope":         "tweet.read tweet.write users.read",
	}
}

