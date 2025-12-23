package playground

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEndpointRateLimit(t *testing.T) {
	tests := []struct {
		name           string
		method          string
		path            string
		expectedLimit   int
		expectedWindow  int
		shouldMatch     bool
		expectedEndpoint string
	}{
		{
			name:            "Exact match - /2/users/me",
			method:          "GET",
			path:            "/2/users/me",
			expectedLimit:   75,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/users/me",
		},
		{
			name:            "Exact match - /2/users/search",
			method:          "GET",
			path:            "/2/users/search",
			expectedLimit:   60,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/users/search",
		},
		{
			name:            "Prefix match - /2/users/{id}",
			method:          "GET",
			path:            "/2/users/123456789",
			expectedLimit:   75,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/users",
		},
		{
			name:            "Prefix match - /2/users with query params",
			method:          "GET",
			path:            "/2/users?ids=0,1,2",
			expectedLimit:   75,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/users",
		},
		{
			name:            "Tweet search recent",
			method:          "GET",
			path:            "/2/tweets/search/recent",
			expectedLimit:   180,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/tweets/search/recent",
		},
		{
			name:            "Tweet search all",
			method:          "GET",
			path:            "/2/tweets/search/all",
			expectedLimit:   300,
			expectedWindow:  10800,
			shouldMatch:     true,
			expectedEndpoint: "/2/tweets/search/all",
		},
		{
			name:            "Individual tweet lookup",
			method:          "GET",
			path:            "/2/tweets/123456789",
			expectedLimit:   300,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/tweets",
		},
		{
			name:            "POST tweet",
			method:          "POST",
			path:            "/2/tweets",
			expectedLimit:   300,
			expectedWindow:  10800,
			shouldMatch:     true,
			expectedEndpoint: "/2/tweets",
		},
		{
			name:            "DELETE tweet",
			method:          "DELETE",
			path:            "/2/tweets/123456789",
			expectedLimit:   50,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/tweets",
		},
		{
			name:            "Lists endpoint",
			method:          "GET",
			path:            "/2/lists",
			expectedLimit:   75,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/lists",
		},
		{
			name:            "Media upload",
			method:          "POST",
			path:            "/2/media/upload",
			expectedLimit:   1000,
			expectedWindow:  86400,
			shouldMatch:     true,
			expectedEndpoint: "/2/media/upload",
		},
		{
			name:            "HEAD request uses GET limits",
			method:          "HEAD",
			path:            "/2/users/me",
			expectedLimit:   75,
			expectedWindow:  900,
			shouldMatch:     true,
			expectedEndpoint: "/2/users/me",
			// Note: Method will be "GET" in result because HEAD is normalized to GET internally
		},
		{
			name:        "Unknown endpoint returns nil",
			method:      "GET",
			path:        "/2/unknown/endpoint",
			shouldMatch: false,
		},
		{
			name:        "Method mismatch",
			method:      "POST",
			path:        "/2/users/me",
			shouldMatch: false, // /2/users/me only matches GET
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEndpointRateLimit(tt.method, tt.path, nil)
			
			if !tt.shouldMatch {
				assert.Nil(t, result, "Expected no match for %s %s", tt.method, tt.path)
				return
			}
			
			require.NotNil(t, result, "Expected match for %s %s", tt.method, tt.path)
			assert.Equal(t, tt.expectedLimit, result.Limit, "Limit mismatch")
			assert.Equal(t, tt.expectedWindow, result.WindowSec, "Window mismatch")
			// HEAD requests are normalized to GET, so check accordingly
			expectedMethod := tt.method
			if tt.method == "HEAD" {
				expectedMethod = "GET"
			}
			assert.Equal(t, expectedMethod, result.Method, "Method mismatch")
			if tt.expectedEndpoint != "" {
				assert.Equal(t, tt.expectedEndpoint, result.Endpoint, "Endpoint mismatch")
			}
		})
	}
}

func TestGetEndpointRateLimit_Specificity(t *testing.T) {
	// Test that more specific endpoints take precedence
	t.Run("More specific endpoint wins", func(t *testing.T) {
		// /2/users/search should match /2/users/search (60) not /2/users (75)
		result := GetEndpointRateLimit("GET", "/2/users/search", nil)
		require.NotNil(t, result)
		assert.Equal(t, 60, result.Limit, "Should match /2/users/search with limit 60")
		assert.Equal(t, "/2/users/search", result.Endpoint)
	})
	
	t.Run("Exact match takes precedence over prefix", func(t *testing.T) {
		// /2/users/me should match exactly, not as prefix of /2/users
		result := GetEndpointRateLimit("GET", "/2/users/me", nil)
		require.NotNil(t, result)
		assert.Equal(t, 75, result.Limit)
		assert.Equal(t, "/2/users/me", result.Endpoint)
	})
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Path with query params",
			input:    "/2/users?ids=0,1,2",
			expected: "/2/users",
		},
		{
			name:     "Path with trailing slash",
			input:    "/2/users/",
			expected: "/2/users",
		},
		{
			name:     "Path with both",
			input:    "/2/users/?ids=0",
			expected: "/2/users",
		},
		{
			name:     "Root path",
			input:    "/",
			expected: "/",
		},
		{
			name:     "Simple path",
			input:    "/2/users",
			expected: "/2/users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRateLimiter_CheckRateLimit(t *testing.T) {
	t.Run("Rate limit enabled - within limit", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     5,
			WindowSec: 60,
			Enabled:   true,
		}
		limiter := NewRateLimiter(config)
		credentials := "test_key"

		// Make 3 requests
		for i := 0; i < 3; i++ {
			allowed, remaining, resetTime := limiter.CheckRateLimit(credentials, "/2/users/me")
			assert.True(t, allowed, "Request %d should be allowed", i+1)
			assert.Equal(t, 5-(i+1), remaining, "Remaining should decrease")
			assert.True(t, resetTime.After(time.Now()), "Reset time should be in future")
		}
	})

	t.Run("Rate limit enabled - exceeds limit", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     3,
			WindowSec: 60,
			Enabled:   true,
		}
		limiter := NewRateLimiter(config)
		credentials := "test_key"

		// Make 3 requests (at limit)
		for i := 0; i < 3; i++ {
			allowed, _, _ := limiter.CheckRateLimit(credentials, "/2/users/me")
			assert.True(t, allowed, "Request %d should be allowed", i+1)
		}

		// 4th request should be denied
		allowed, remaining, resetTime := limiter.CheckRateLimit(credentials, "/2/users/me")
		assert.False(t, allowed, "4th request should be denied")
		assert.Equal(t, 0, remaining, "Remaining should be 0")
		assert.True(t, resetTime.After(time.Now()), "Reset time should be in future")
	})

	t.Run("Rate limit disabled", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     5,
			WindowSec: 60,
			Enabled:   false,
		}
		limiter := NewRateLimiter(config)
		credentials := "test_key"

		// Make many requests - all should be allowed
		for i := 0; i < 10; i++ {
			allowed, remaining, resetTime := limiter.CheckRateLimit(credentials, "/2/users/me")
			assert.True(t, allowed, "Request %d should be allowed when disabled", i+1)
			assert.Equal(t, config.Limit, remaining, "Remaining should equal limit")
			assert.True(t, resetTime.After(time.Now()), "Reset time should be in future")
		}
	})

	t.Run("Different credentials have separate limits", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     2,
			WindowSec: 60,
			Enabled:   true,
		}
		limiter := NewRateLimiter(config)

		// Use up limit for key1
		limiter.CheckRateLimit("key1", "/2/users/me")
		limiter.CheckRateLimit("key1", "/2/users/me")
		allowed1, _, _ := limiter.CheckRateLimit("key1", "/2/users/me")
		assert.False(t, allowed1, "key1 should be rate limited")

		// key2 should still have requests available
		allowed2, remaining2, resetTime2 := limiter.CheckRateLimit("key2", "/2/users/me")
		assert.True(t, allowed2, "key2 should be allowed")
		assert.Equal(t, 1, remaining2, "key2 should have 1 remaining")
		assert.True(t, resetTime2.After(time.Now()), "Reset time should be in future")
	})

	t.Run("Different endpoints have separate limits", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     2,
			WindowSec: 60,
			Enabled:   true,
		}
		limiter := NewRateLimiter(config)
		credentials := "test_key"

		// Use up limit for endpoint1
		limiter.CheckRateLimit(credentials, "/2/users/me")
		limiter.CheckRateLimit(credentials, "/2/users/me")
		allowed1, _, _ := limiter.CheckRateLimit(credentials, "/2/users/me")
		assert.False(t, allowed1, "/2/users/me should be rate limited")

		// endpoint2 should still have requests available (same credentials, different endpoint)
		allowed2, remaining2, resetTime2 := limiter.CheckRateLimit(credentials, "/2/tweets")
		assert.True(t, allowed2, "/2/tweets should be allowed")
		assert.Equal(t, 1, remaining2, "/2/tweets should have 1 remaining")
		assert.True(t, resetTime2.After(time.Now()), "Reset time should be in future")
	})

	t.Run("Old requests expire", func(t *testing.T) {
		config := &RateLimitConfig{
			Limit:     2,
			WindowSec: 1, // 1 second window
			Enabled:   true,
		}
		limiter := NewRateLimiter(config)
		credentials := "test_key"

		// Make 2 requests
		limiter.CheckRateLimit(credentials, "/2/users/me")
		limiter.CheckRateLimit(credentials, "/2/users/me")

		// Should be rate limited
		allowed, remaining, resetTime := limiter.CheckRateLimit(credentials, "/2/users/me")
		assert.False(t, allowed, "Should be rate limited")
		assert.Equal(t, 0, remaining, "Remaining should be 0 when rate limited")
		assert.True(t, resetTime.After(time.Now()), "Reset time should be in future")

		// Wait for window to expire
		time.Sleep(1100 * time.Millisecond)

		// Should be allowed again
		allowed2, remaining2, resetTime2 := limiter.CheckRateLimit(credentials, "/2/users/me")
		assert.True(t, allowed2, "Should be allowed after window expires")
		assert.Equal(t, 1, remaining2, "Should have 1 remaining")
		assert.True(t, resetTime2.After(time.Now()), "Reset time should be in future")
	})
}

func TestGetDefaultRateLimit(t *testing.T) {
	config := GetDefaultRateLimit()
	require.NotNil(t, config)
	assert.Equal(t, 15, config.Limit, "Default limit should be 15")
	assert.Equal(t, 900, config.WindowSec, "Default window should be 900 seconds (15 min)")
}

func TestRateLimiter_NilConfig(t *testing.T) {
	limiter := NewRateLimiter(nil)
	require.NotNil(t, limiter)
	
	// Should not panic and should allow requests (disabled)
	allowed, remaining, resetTime := limiter.CheckRateLimit("test_key", "/2/users/me")
	assert.True(t, allowed, "Should allow requests when config is nil")
	assert.Equal(t, 0, remaining, "Remaining should be 0 when disabled")
	assert.False(t, resetTime.IsZero(), "Reset time should be set")
	assert.True(t, resetTime.After(time.Now().Add(-time.Second)) || resetTime.Equal(time.Now()), "Reset time should be current time or future")
}
