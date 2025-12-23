// Package playground defines endpoint-specific rate limit configurations.
//
// This file contains rate limit definitions for X API v2 endpoints, matching
// the real API's rate limits. It provides functions to look up rate limits
// for specific endpoints, with support for config overrides and default limits.
package playground

import (
	"log"
	"strings"
)

// EndpointRateLimit defines rate limits for specific endpoints
type EndpointRateLimit struct {
	Limit     int    // Number of requests allowed
	WindowSec int    // Time window in seconds
	Endpoint  string // Endpoint pattern (e.g., "/2/users/me", "/2/tweets/search/recent")
	Method    string // HTTP method (e.g., "GET", "POST") - empty string matches all methods
}

// init function to verify package is loaded and endpointRateLimits is initialized
func init() {
	// Package initialization - rate limits are ready
}

// rateLimitDebug enables verbose debug logging for rate limit matching
// Set to true via environment variable or build tag for debugging
var rateLimitDebug = false

// GetEndpointRateLimit returns the rate limit configuration for a specific endpoint
// Checks config overrides first, then hardcoded defaults, then returns nil for default limit
// config can be nil - if provided, checks for endpoint overrides
func GetEndpointRateLimit(method, path string, config *RateLimitConfig) *EndpointRateLimit {
	// Normalize method: HEAD requests should use GET rate limits (standard HTTP behavior)
	methodToCheck := method
	if method == "HEAD" {
		methodToCheck = "GET"
	}
	
	// Normalize path (remove query params, trailing slashes)
	normalizedPath := normalizePath(path)
	
	if rateLimitDebug {
		log.Printf("[RATE_LIMIT_DEBUG] Checking: method=%s, path=%s, normalized=%s", 
			method, path, normalizedPath)
	}
	
	// First, check for config overrides (user-configured per-endpoint limits)
	if config != nil && config.EndpointOverrides != nil {
		// Try exact method:path match first
		overrideKey := methodToCheck + ":" + normalizedPath
		if override, exists := config.EndpointOverrides[overrideKey]; exists {
			if rateLimitDebug {
				log.Printf("[RATE_LIMIT_DEBUG] Found config override: %s -> limit %d", overrideKey, override.Limit)
			}
			return &EndpointRateLimit{
				Endpoint:  normalizedPath,
				Method:    methodToCheck,
				Limit:     override.Limit,
				WindowSec: override.WindowSec,
			}
		}
		// Try path-only match (applies to all methods)
		if override, exists := config.EndpointOverrides[normalizedPath]; exists {
			if rateLimitDebug {
				log.Printf("[RATE_LIMIT_DEBUG] Found config override (all methods): %s -> limit %d", normalizedPath, override.Limit)
			}
			return &EndpointRateLimit{
				Endpoint:  normalizedPath,
				Method:    methodToCheck,
				Limit:     override.Limit,
				WindowSec: override.WindowSec,
			}
		}
		// Try prefix matches (check if any override path is a prefix of this path)
		for overrideKey, override := range config.EndpointOverrides {
			// Skip method:path format for prefix matching
			if strings.Contains(overrideKey, ":") {
				continue
			}
			if strings.HasPrefix(normalizedPath, overrideKey) {
				if rateLimitDebug {
					log.Printf("[RATE_LIMIT_DEBUG] Found config override (prefix): %s -> limit %d", overrideKey, override.Limit)
				}
				return &EndpointRateLimit{
					Endpoint:  normalizedPath,
					Method:    methodToCheck,
					Limit:     override.Limit,
					WindowSec: override.WindowSec,
				}
			}
		}
	}
	
	// First, collect all potential matches (exact and prefix)
	var exactMatch *EndpointRateLimit
	var prefixMatches []EndpointRateLimit
	
	for _, limit := range endpointRateLimits {
		// Check method match first
		if limit.Method != "" && limit.Method != methodToCheck {
			continue
		}
		
		if limit.Endpoint != "" {
			// Check for exact match first (most specific) - use normalized path first for performance
			if limit.Endpoint == normalizedPath {
				exactMatch = &limit
				if rateLimitDebug {
					log.Printf("[RATE_LIMIT_DEBUG] Found exact match: %s -> limit %d", limit.Endpoint, limit.Limit)
				}
				break // Exact match takes precedence
			}
			// Fallback to original path only if normalized didn't match
			if normalizedPath != path && limit.Endpoint == path {
				exactMatch = &limit
				if rateLimitDebug {
					log.Printf("[RATE_LIMIT_DEBUG] Found exact match: %s -> limit %d", limit.Endpoint, limit.Limit)
				}
				break
			}
			
			// Check for prefix match (path starts with endpoint) - prefer normalized path
			if strings.HasPrefix(normalizedPath, limit.Endpoint) {
				prefixMatches = append(prefixMatches, limit)
				if rateLimitDebug {
					log.Printf("[RATE_LIMIT_DEBUG] Found prefix match: %s -> limit %d", limit.Endpoint, limit.Limit)
				}
			} else if normalizedPath != path && strings.HasPrefix(path, limit.Endpoint) {
				// Only check original path if it differs from normalized
				prefixMatches = append(prefixMatches, limit)
				if rateLimitDebug {
					log.Printf("[RATE_LIMIT_DEBUG] Found prefix match: %s -> limit %d", limit.Endpoint, limit.Limit)
				}
			}
		}
	}
	
	// Return exact match if found
	if exactMatch != nil {
		return exactMatch
	}
	
	// Return longest prefix match (most specific)
	if len(prefixMatches) > 0 {
		longest := prefixMatches[0]
		for _, match := range prefixMatches[1:] {
			if len(match.Endpoint) > len(longest.Endpoint) {
				longest = match
			}
		}
		if rateLimitDebug {
			log.Printf("[RATE_LIMIT_DEBUG] Selected longest prefix match: %s -> limit %d", longest.Endpoint, longest.Limit)
		}
		return &longest
	}
	
	if rateLimitDebug {
		log.Printf("[RATE_LIMIT_DEBUG] No match found for %s %s, using default", method, normalizedPath)
	}
	return nil // Use default rate limit
}

// normalizePath normalizes a path for comparison
// Optimized: uses strings.Index for faster query param removal
func normalizePath(path string) string {
	// Remove query parameters (more efficient than manual loop)
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		path = path[:idx]
	}
	// Remove trailing slash
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// matchesEndpoint checks if a method and path match an endpoint rate limit pattern
func matchesEndpoint(limit EndpointRateLimit, method, path string) bool {
	// Check method match (empty method matches all)
	if limit.Method != "" && limit.Method != method {
		return false
	}
	
	// Check exact path match
	if limit.Endpoint == path {
		return true
	}
	
	return false
}

// endpointRateLimits defines rate limits for specific endpoints matching real X API
// Based on X API v2 rate limits as of 2024
var endpointRateLimits = []EndpointRateLimit{
	// User endpoints - most specific first to ensure correct matching
	{Endpoint: "/2/users/reposts_of_me", Method: "GET", Limit: 75, WindowSec: 900}, // 75 per 15 min
	{Endpoint: "/2/users/search", Method: "GET", Limit: 60, WindowSec: 900},  // 60 per 15 min
	{Endpoint: "/2/users/by/username", Method: "GET", Limit: 75, WindowSec: 900}, // 75 per 15 min
	{Endpoint: "/2/users/by", Method: "GET", Limit: 75, WindowSec: 900},       // 75 per 15 min
	{Endpoint: "/2/users/me", Method: "GET", Limit: 75, WindowSec: 900},        // 75 per 15 min
	{Endpoint: "/2/users", Method: "GET", Limit: 75, WindowSec: 900},          // 75 per 15 min (covers /2/users and /2/users/{id})
	
	// Individual user lookup (matches /2/users/{id} but not /2/users/me or /2/users/by)
	// Note: More specific endpoints above take precedence
	
	// User relationship endpoints (blocking, muting, following)
	{Endpoint: "/blocking", Method: "GET", Limit: 15, WindowSec: 900},         // 15 per 15 min
	{Endpoint: "/blocking", Method: "POST", Limit: 50, WindowSec: 900},        // 50 per 15 min
	{Endpoint: "/muting", Method: "GET", Limit: 15, WindowSec: 900},          // 15 per 15 min
	{Endpoint: "/muting", Method: "POST", Limit: 50, WindowSec: 900},         // 50 per 15 min
	{Endpoint: "/following", Method: "GET", Limit: 15, WindowSec: 900},       // 15 per 15 min
	{Endpoint: "/following", Method: "POST", Limit: 50, WindowSec: 900},      // 50 per 15 min
	
	// Tweet endpoints - most specific first
	{Endpoint: "/2/tweets/search/recent", Method: "GET", Limit: 180, WindowSec: 900}, // 180 per 15 min
	{Endpoint: "/2/tweets/search/all", Method: "GET", Limit: 300, WindowSec: 10800}, // 300 per 3 hours
	{Endpoint: "/2/tweets", Method: "GET", Limit: 300, WindowSec: 900},         // 300 per 15 min (general)
	{Endpoint: "/2/tweets", Method: "POST", Limit: 300, WindowSec: 10800},      // 300 per 3 hours
	{Endpoint: "/2/tweets", Method: "DELETE", Limit: 50, WindowSec: 900},       // 50 per 15 min
	
	// Individual tweet lookup (matches /2/tweets/{id} but not /2/tweets/search/*)
	// Note: More specific endpoints above take precedence
	
	// Tweet engagement endpoints (likes, retweets, bookmarks)
	{Endpoint: "/likes", Method: "POST", Limit: 50, WindowSec: 900},           // 50 per 15 min
	{Endpoint: "/likes", Method: "DELETE", Limit: 50, WindowSec: 900},          // 50 per 15 min
	{Endpoint: "/retweets", Method: "POST", Limit: 50, WindowSec: 900},        // 50 per 15 min
	{Endpoint: "/retweets", Method: "DELETE", Limit: 50, WindowSec: 900},      // 50 per 15 min
	{Endpoint: "/bookmarks", Method: "POST", Limit: 50, WindowSec: 900},       // 50 per 15 min
	{Endpoint: "/bookmarks", Method: "DELETE", Limit: 50, WindowSec: 900},     // 50 per 15 min
	
	// Streaming endpoints
	{Endpoint: "/2/tweets/sample/stream", Method: "GET", Limit: 50, WindowSec: 900}, // 50 per 15 min
	{Endpoint: "/2/tweets/search/stream", Method: "GET", Limit: 50, WindowSec: 900},  // 50 per 15 min
	
	// List endpoints - specific sub-endpoints first, then general
	{Endpoint: "/2/lists", Method: "GET", Limit: 75, WindowSec: 900},           // 75 per 15 min
	{Endpoint: "/2/lists", Method: "POST", Limit: 75, WindowSec: 900},          // 75 per 15 min
	{Endpoint: "/2/lists", Method: "PUT", Limit: 75, WindowSec: 900},           // 75 per 15 min
	{Endpoint: "/2/lists", Method: "DELETE", Limit: 75, WindowSec: 900},        // 75 per 15 min
	
	// List membership endpoints
	{Endpoint: "/members", Method: "GET", Limit: 75, WindowSec: 900},         // 75 per 15 min
	{Endpoint: "/members", Method: "POST", Limit: 50, WindowSec: 900},        // 50 per 15 min
	{Endpoint: "/members", Method: "DELETE", Limit: 50, WindowSec: 900},      // 50 per 15 min
	{Endpoint: "/followers", Method: "GET", Limit: 75, WindowSec: 900},        // 75 per 15 min
	{Endpoint: "/pinned_lists", Method: "GET", Limit: 75, WindowSec: 900},      // 75 per 15 min
	{Endpoint: "/pinned_lists", Method: "POST", Limit: 50, WindowSec: 900},   // 50 per 15 min
	{Endpoint: "/pinned_lists", Method: "DELETE", Limit: 50, WindowSec: 900},  // 50 per 15 min
	{Endpoint: "/followed_lists", Method: "GET", Limit: 75, WindowSec: 900},   // 75 per 15 min
	{Endpoint: "/followed_lists", Method: "POST", Limit: 50, WindowSec: 900},  // 50 per 15 min
	{Endpoint: "/followed_lists", Method: "DELETE", Limit: 50, WindowSec: 900}, // 50 per 15 min
	
	// Space endpoints - most specific first
	{Endpoint: "/2/spaces/by/creator_ids", Method: "GET", Limit: 75, WindowSec: 900}, // 75 per 15 min
	{Endpoint: "/2/spaces/search", Method: "GET", Limit: 75, WindowSec: 900},   // 75 per 15 min
	{Endpoint: "/2/spaces", Method: "GET", Limit: 75, WindowSec: 900},          // 75 per 15 min (general)
	{Endpoint: "/2/spaces", Method: "POST", Limit: 75, WindowSec: 900},         // 75 per 15 min
	
	// Media endpoints
	{Endpoint: "/2/media/upload", Method: "POST", Limit: 1000, WindowSec: 86400}, // 1000 per 24 hours
	{Endpoint: "/2/media/upload/", Method: "GET", Limit: 1000, WindowSec: 86400}, // 1000 per 24 hours (status check)
}

// GetDefaultRateLimit returns the default rate limit configuration
func GetDefaultRateLimit() *RateLimitConfig {
	return &RateLimitConfig{
		Limit:     15,  // Default: 15 requests
		WindowSec: 900, // Default: 15 minutes
	}
}
