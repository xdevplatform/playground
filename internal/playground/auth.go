// Package playground handles authentication validation for API requests.
//
// This file validates authentication requirements for endpoints, checking
// whether requests need Bearer tokens, OAuth 1.0a, OAuth 2.0 user context, or
// no authentication. It maps endpoints to their required authentication methods
// based on X API documentation and real API behavior.
package playground

import (
	"net/http"
	"strings"
)

// AuthMethod represents the type of authentication
type AuthMethod string

const (
	// AuthBearerToken - OAuth 2.0 Application-Only (Bearer token)
	AuthBearerToken AuthMethod = "BearerToken"
	// AuthOAuth1a - OAuth 1.0a User Context
	AuthOAuth1a AuthMethod = "OAuth1a"
	// AuthOAuth2User - OAuth 2.0 User Context
	AuthOAuth2User AuthMethod = "OAuth2UserToken"
	// AuthAny - Any authentication method is acceptable
	AuthAny AuthMethod = "Any"
	// AuthNone - No authentication required
	AuthNone AuthMethod = "None"
)

// EndpointAuthRequirements maps endpoint paths to their required authentication methods
// This is based on X API documentation and real API behavior
var EndpointAuthRequirements = map[string][]AuthMethod{
	// User endpoints
	"/2/users/me": {AuthOAuth1a, AuthOAuth2User}, // Requires User Context, not Bearer token
	
	// Tweet creation/modification endpoints - require User Context
	"/2/tweets":                    {AuthOAuth1a, AuthOAuth2User},
	"/2/tweets/{id}":              {AuthAny}, // GET works with Bearer, DELETE requires User Context
	"/2/tweets/{id}/retweets":     {AuthOAuth1a, AuthOAuth2User},
	"/2/tweets/{id}/likes":        {AuthOAuth1a, AuthOAuth2User},
	"/2/tweets/{id}/hidden":       {AuthOAuth1a, AuthOAuth2User},
	
	// User relationship endpoints - require User Context
	"/2/users/{id}/following":     {AuthOAuth1a, AuthOAuth2User},
	"/2/users/{id}/followers":     {AuthAny}, // GET works with Bearer, POST/DELETE require User Context
	"/2/users/{id}/blocking":      {AuthOAuth1a, AuthOAuth2User},
	"/2/users/{id}/muting":        {AuthOAuth1a, AuthOAuth2User},
	"/2/users/{id}/likes":         {AuthOAuth1a, AuthOAuth2User},
	"/2/users/{id}/bookmarks":     {AuthOAuth1a, AuthOAuth2User},
	"/2/users/{id}/bookmarks/folders": {AuthOAuth1a, AuthOAuth2User},
	
	// List endpoints - require User Context for write operations
	"/2/lists":                    {AuthOAuth1a, AuthOAuth2User}, // POST requires User Context
	"/2/lists/{id}":               {AuthAny}, // GET works with Bearer, DELETE requires User Context
	"/2/lists/{id}/members":       {AuthOAuth1a, AuthOAuth2User},
	"/2/lists/{id}/followers":     {AuthOAuth1a, AuthOAuth2User},
	
	// DMs - require User Context
	"/2/dm_events":                 {AuthOAuth1a, AuthOAuth2User},
	"/2/dm_conversations":          {AuthOAuth1a, AuthOAuth2User},
	"/2/dm_conversations/with/{participant_id}/dm_events": {AuthOAuth1a, AuthOAuth2User},
	
	// Media upload - requires User Context
	"/2/media/upload":              {AuthOAuth1a, AuthOAuth2User},
	"/2/media/upload/initialize":   {AuthOAuth1a, AuthOAuth2User},
	"/2/media/metadata":            {AuthOAuth1a, AuthOAuth2User},
	"/2/media/subtitles":           {AuthOAuth1a, AuthOAuth2User},
	
	// Search stream rules - require User Context
	"/2/tweets/search/stream/rules": {AuthOAuth1a, AuthOAuth2User},
	
	// Activity subscriptions - removed (not ready to support yet)
}

// GetRequiredAuthForOperation returns the required authentication methods from an OpenAPI operation
// Returns AuthAny if no specific requirement is defined (defaults to accepting any auth)
func GetRequiredAuthForOperation(op *Operation) []AuthMethod {
	if op == nil || op.Security == nil || len(op.Security) == 0 {
		// No security requirements defined - accept any auth
		return []AuthMethod{AuthAny}
	}
	
	// Security is an array of objects, each object can have multiple auth schemes
	// We need to collect all unique auth schemes from all security requirements
	authMethods := make(map[AuthMethod]bool)
	
	for _, securityReq := range op.Security {
		// Each security requirement is a map like {"BearerToken": []} or {"OAuth2UserToken": ["scope1", "scope2"]}
		for schemeName := range securityReq {
			switch schemeName {
			case "BearerToken":
				authMethods[AuthBearerToken] = true
			case "OAuth2UserToken":
				authMethods[AuthOAuth2User] = true
			case "UserToken":
				// UserToken is OAuth 1.0a User Context
				authMethods[AuthOAuth1a] = true
			}
		}
	}
	
	// Convert map to slice
	result := make([]AuthMethod, 0, len(authMethods))
	for auth := range authMethods {
		result = append(result, auth)
	}
	
	// If no auth methods found, accept any
	if len(result) == 0 {
		return []AuthMethod{AuthAny}
	}
	
	return result
}

// GetRequiredAuthForEndpoint returns the required authentication methods for an endpoint
// This is a fallback for when we don't have the OpenAPI operation
// Returns AuthAny if no specific requirement is defined (defaults to accepting any auth)
func GetRequiredAuthForEndpoint(method, path string) []AuthMethod {
	// Normalize path (remove query params, trailing slashes)
	normalizedPath := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
	
	// Check exact path match first
	if auth, ok := EndpointAuthRequirements[normalizedPath]; ok {
		return auth
	}
	
	// Check path patterns (e.g., /2/users/{id}/following)
	// We'll match against known patterns
	for pattern, auth := range EndpointAuthRequirements {
		if matchesPathPattern(normalizedPath, pattern) {
			return auth
		}
	}
	
	// Default: accept any authentication method
	return []AuthMethod{AuthAny}
}

// matchesPathPattern checks if a path matches a pattern (e.g., /2/users/{id}/following)
func matchesPathPattern(path, pattern string) bool {
	pathParts := strings.Split(path, "/")
	patternParts := strings.Split(pattern, "/")
	
	if len(pathParts) != len(patternParts) {
		return false
	}
	
	for i, patternPart := range patternParts {
		if strings.HasPrefix(patternPart, "{") && strings.HasSuffix(patternPart, "}") {
			// This is a path parameter, match any value
			continue
		}
		if pathParts[i] != patternPart {
			return false
		}
	}
	
	return true
}

// DetectAuthMethod detects the authentication method from the request
func DetectAuthMethod(r *http.Request) AuthMethod {
	// First check for explicit auth method header (set by playground UI)
	// This allows us to distinguish between Bearer Token (App-Only) and OAuth 2.0 User Context
	if authMethodHeader := r.Header.Get("X-Auth-Method"); authMethodHeader != "" {
		switch strings.ToLower(authMethodHeader) {
		case "bearer", "bearertoken", "oauth2app":
			return AuthBearerToken
		case "oauth1", "oauth1a", "usertoken":
			return AuthOAuth1a
		case "oauth2", "oauth2user", "oauth2usertoken":
			return AuthOAuth2User
		case "none":
			return AuthNone
		}
	}
	
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return AuthNone
	}
	
	// Check for OAuth 1.0a (Authorization header with oauth parameters)
	// Format: OAuth oauth_consumer_key="...", oauth_token="...", oauth_signature_method="...", etc.
	// Must check this BEFORE checking for Bearer token, as OAuth 1.0a headers start with "OAuth "
	// Use case-insensitive check to be safe
	authHeaderLower := strings.ToLower(authHeader)
	if strings.HasPrefix(authHeaderLower, "oauth ") {
		// Check if it contains OAuth 1.0a parameters (not OAuth 2.0)
		// Use case-insensitive check for parameters too
		if strings.Contains(authHeaderLower, "oauth_consumer_key") || 
		   strings.Contains(authHeaderLower, "oauth_signature") ||
		   strings.Contains(authHeaderLower, "oauth_token") ||
		   strings.Contains(authHeaderLower, "oauth_signature_method") {
			return AuthOAuth1a
		}
		// If it starts with "OAuth " but doesn't have OAuth 1.0a params, might be OAuth 2.0 User Context
		// (though this is uncommon - most OAuth 2.0 uses "Bearer")
		return AuthOAuth2User
	}
	
	// Check for Bearer token (could be OAuth 2.0 Application-Only or User Context)
	// Both use "Bearer <token>" format, so we can't distinguish them from the header alone
	// Without the X-Auth-Method header, we default to BearerToken (Application-Only)
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return AuthBearerToken
	}
	
	// Default: assume Bearer token (most common)
	return AuthBearerToken
}

// ValidateAuth checks if the request's authentication method is acceptable for the endpoint
// Returns (isValid, errorResponse)
func ValidateAuth(method, path string, r *http.Request, op *Operation, authConfig *AuthConfig) (bool, map[string]interface{}) {
	// If auth validation is disabled (for testing), allow all requests
	if authConfig != nil && authConfig.DisableValidation {
		return true, nil
	}
	var requiredAuth []AuthMethod
	// Use OpenAPI spec as the source of truth
	if op != nil {
		requiredAuth = GetRequiredAuthForOperation(op)
	} else {
		// Fallback to hardcoded map only if OpenAPI spec not available
		requiredAuth = GetRequiredAuthForEndpoint(method, path)
	}
	
	detectedAuth := DetectAuthMethod(r)
	
	// If endpoint accepts any auth, allow it
	for _, auth := range requiredAuth {
		if auth == AuthAny {
			return true, nil
		}
		if auth == detectedAuth {
			return true, nil
		}
	}
	
	// Special case: if endpoint requires User Context but we only have Bearer token
	if detectedAuth == AuthBearerToken {
		for _, auth := range requiredAuth {
			if auth == AuthOAuth1a || auth == AuthOAuth2User {
				// Return 403 error matching real X API format
				return false, map[string]interface{}{
					"title":   "Unsupported Authentication",
					"detail":  "Authenticating with OAuth 2.0 Application-Only is forbidden for this endpoint. Supported authentication types are [OAuth 1.0a User Context, OAuth 2.0 User Context].",
					"type":    "https://api.twitter.com/2/problems/unsupported-authentication",
					"status":  403,
				}
			}
		}
	}
	
	// If no auth provided but required
	// By default, enforce auth like the real API
	if detectedAuth == AuthNone {
		for _, auth := range requiredAuth {
			if auth != AuthNone {
				return false, map[string]interface{}{
					"title":   "Unauthorized",
					"detail":  "Unauthorized",
					"type":    "about:blank",
					"status":  401,
				}
			}
		}
	}
	
	// If we have auth but it doesn't match requirements, reject it
	// This ensures we still validate auth when it IS provided
	if len(requiredAuth) > 0 {
		hasMatch := false
		for _, auth := range requiredAuth {
			if auth == AuthAny || auth == detectedAuth {
				hasMatch = true
				break
			}
		}
		if !hasMatch {
			// Auth provided but doesn't match requirements
			if detectedAuth == AuthBearerToken {
				for _, auth := range requiredAuth {
					if auth == AuthOAuth1a || auth == AuthOAuth2User {
						// Return 403 error matching real X API format
						return false, map[string]interface{}{
							"title":   "Unsupported Authentication",
							"detail":  "Authenticating with OAuth 2.0 Application-Only is forbidden for this endpoint. Supported authentication types are [OAuth 1.0a User Context, OAuth 2.0 User Context].",
							"type":    "https://api.twitter.com/2/problems/unsupported-authentication",
							"status":  403,
						}
					}
				}
			}
			// Other auth mismatches - reject with 401 format
			return false, map[string]interface{}{
				"title":   "Unauthorized",
				"detail":  "Unauthorized",
				"type":    "about:blank",
				"status":  401,
			}
		}
	}
	
	// Default: allow (for backwards compatibility and endpoints not in our map)
	return true, nil
}

// WriteAuthError writes an authentication error response
func WriteAuthError(w http.ResponseWriter, errorResponse map[string]interface{}) {
	statusCode := 403
	if status, ok := errorResponse["status"].(int); ok {
		statusCode = status
	}
	
	// Headers (including rate limits) should already be set by caller
	// Only set Content-Type if not already set
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(statusCode)
	
	// For 401 Unauthorized, X API returns a simple object format (not errors array)
	if statusCode == 401 {
		response := map[string]interface{}{
			"title":  errorResponse["title"],
			"type":   errorResponse["type"],
			"status": statusCode,
			"detail": errorResponse["detail"],
		}
		WriteJSONSafe(w, statusCode, response)
		return
	}
	
	// For other auth errors (403, etc.), use errors array format
	response := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"title":  errorResponse["title"],
				"detail": errorResponse["detail"],
				"type":   errorResponse["type"],
			},
		},
	}
	
	if statusCode == 403 {
		// Add status field for 403 errors
		response["errors"].([]map[string]interface{})[0]["status"] = statusCode
	}
	
	WriteJSONSafe(w, statusCode, response)
}

