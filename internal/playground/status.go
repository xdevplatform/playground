// Package playground provides health check and server statistics endpoints.
//
// This file implements /health and /rate-limits endpoints for monitoring
// server status, tracking request statistics (total requests, success/error
// counts, average response times), and viewing rate limit status.
package playground

import (
	"net/http"
	"sync/atomic"
	"time"
)

// ServerStats tracks server statistics.
// All counters are atomic for thread-safe access.
type ServerStats struct {
	RequestsTotal      int64   `json:"requests_total"`
	RequestsSuccess    int64   `json:"requests_success"`
	RequestsError      int64   `json:"requests_error"`
	ResponseTimeTotal  int64   `json:"response_time_total_ms"` // Total response time in milliseconds
	ResponseTimeCount  int64   `json:"response_time_count"`    // Number of responses timed
	StartTime          time.Time `json:"start_time"`
}

// GetAverageResponseTime returns average response time in milliseconds
func (s *ServerStats) GetAverageResponseTime() float64 {
	if s.ResponseTimeCount == 0 {
		return 0
	}
	return float64(s.ResponseTimeTotal) / float64(s.ResponseTimeCount)
}

var serverStats = &ServerStats{
	StartTime: time.Now(),
}

// IncrementRequestsTotal increments the total request counter
func IncrementRequestsTotal() {
	atomic.AddInt64(&serverStats.RequestsTotal, 1)
}

// IncrementRequestsSuccess increments the success request counter.
// Thread-safe atomic operation.
func IncrementRequestsSuccess() {
	atomic.AddInt64(&serverStats.RequestsSuccess, 1)
}

// IncrementRequestsError increments the error request counter.
// Thread-safe atomic operation.
func IncrementRequestsError() {
	atomic.AddInt64(&serverStats.RequestsError, 1)
}

// RecordResponseTime records a response time measurement.
// Thread-safe atomic operation. Used for calculating average response times.
func RecordResponseTime(responseTimeMs int64) {
	atomic.AddInt64(&serverStats.ResponseTimeTotal, responseTimeMs)
	atomic.AddInt64(&serverStats.ResponseTimeCount, 1)
}

// GetServerStats returns current server statistics.
// Returns a snapshot of all statistics with atomic values loaded safely.
func GetServerStats() *ServerStats {
	return &ServerStats{
		RequestsTotal:     atomic.LoadInt64(&serverStats.RequestsTotal),
		RequestsSuccess:   atomic.LoadInt64(&serverStats.RequestsSuccess),
		RequestsError:     atomic.LoadInt64(&serverStats.RequestsError),
		ResponseTimeTotal: atomic.LoadInt64(&serverStats.ResponseTimeTotal),
		ResponseTimeCount: atomic.LoadInt64(&serverStats.ResponseTimeCount),
		StartTime:         serverStats.StartTime,
	}
}

// HandleHealth returns a comprehensive health check response.
// Includes server status, uptime, and request statistics.
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	stats := GetServerStats()
	uptime := time.Since(stats.StartTime)
	
	WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "xurl-playground",
		"version": "1.0.0",
		"uptime_seconds": int64(uptime.Seconds()),
		"stats": map[string]interface{}{
			"requests_total":        stats.RequestsTotal,
			"requests_success":      stats.RequestsSuccess,
			"requests_error":        stats.RequestsError,
			"response_time_avg_ms":  stats.GetAverageResponseTime(),
			"response_time_count":   stats.ResponseTimeCount,
		},
	})
}

// HandleRateLimitStatus returns current rate limit configuration status.
// Shows configured endpoint limits, defaults, and user overrides.
func HandleRateLimitStatus(w http.ResponseWriter, r *http.Request) {
	endpointCount := len(endpointRateLimits)
	
	// Get default limit (from config if available, otherwise hardcoded default)
	defaultLimit := 15
	defaultWindowSec := 900
	
	// Try to get configurable default from global config
	globalConfig := GetGlobalConfig()
	var rateLimitConfig *RateLimitConfig
	if globalConfig != nil {
		rateLimitConfig = globalConfig.GetRateLimitConfig()
		if rateLimitConfig != nil {
			defaultLimit = rateLimitConfig.Limit
			defaultWindowSec = rateLimitConfig.WindowSec
		}
	}
	
	response := map[string]interface{}{
		"endpoint_limits_configured": endpointCount,
		"default_limit": map[string]interface{}{
			"limit":      defaultLimit,
			"window_sec":  defaultWindowSec,
		},
		"endpoints": make([]map[string]interface{}, 0, endpointCount),
		"endpoint_overrides": make(map[string]interface{}),
	}
	
	// Include all endpoint configurations with specific limits (hardcoded defaults)
	endpoints := make([]map[string]interface{}, 0, endpointCount)
	for _, limit := range endpointRateLimits {
		endpoints = append(endpoints, map[string]interface{}{
			"endpoint":   limit.Endpoint,
			"method":     limit.Method,
			"limit":      limit.Limit,
			"window_sec": limit.WindowSec,
			"source":     "default", // Hardcoded default matching X API
		})
	}
	response["endpoints"] = endpoints
	
	// Include user-configured overrides if any
	if rateLimitConfig != nil && rateLimitConfig.EndpointOverrides != nil {
		overrides := make(map[string]interface{})
		for key, override := range rateLimitConfig.EndpointOverrides {
			overrides[key] = map[string]interface{}{
				"limit":      override.Limit,
				"window_sec": override.WindowSec,
			}
		}
		response["endpoint_overrides"] = overrides
	}
	
	response["note"] = "Hardcoded defaults match real X API limits. Users can override any endpoint via Settings."
	response["default_limit_note"] = "The default limit applies to endpoints without hardcoded defaults. You can override any endpoint's rate limit in Settings."
	
	WriteJSONSafe(w, http.StatusOK, response)
}

