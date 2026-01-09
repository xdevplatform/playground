// Package playground provides credit usage tracking for API requests.
//
// This file implements credit tracking and pricing estimation based on the X API
// pricing model. It tracks usage by event type (resources) and request type (requests)
// and calculates costs based on the pricing configuration.
package playground

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pricingURL     = "https://console.x.com/api/credits/pricing"
	pricingCacheFileName = ".playground-pricing-cache.json"
	pricingCacheMaxAge   = 24 * time.Hour // Cache pricing for 24 hours
)

// PricingConfig represents the pricing configuration for API endpoints.
type PricingConfig struct {
	EventTypePricing  map[string]float64 `json:"eventTypePricing"`
	RequestTypePricing map[string]float64 `json:"requestTypePricing"`
	DefaultCost        float64             `json:"defaultCost"`
}

// UsageDataPoint represents a single usage data point with timestamp and value.
type UsageDataPoint struct {
	Timestamp string `json:"timestamp"` // Unix timestamp in milliseconds
	Value     string `json:"value"`      // Usage count as string
}

// UsageGroup represents usage grouped by event type or request type.
type UsageGroup struct {
	Usage         string          `json:"usage"`         // Total usage as string
	PercentOfCap  float64         `json:"percentOfCap"` // Percentage of cap used
	UsageDataPoints []UsageDataPoint `json:"usageDataPoints"` // Time series data
}

// AccountUsage represents usage for a specific account.
type AccountUsage struct {
	UsageUnit string                  `json:"usageUnit"` // "resources" or "requests"
	Cap       string                  `json:"cap"`       // Cap limit as string
	Groups    map[string]*UsageGroup  `json:"groups"`    // Usage by group type
	Total     *UsageGroup             `json:"_total"`   // Total usage
}

// UsageResponse represents the usage API response.
type UsageResponse struct {
	Bucket   string                        `json:"bucket"`   // e.g., "30days"
	FromDate string                        `json:"fromDate"` // ISO 8601 date
	ToDate   string                        `json:"toDate"`   // ISO 8601 date
	Grouping string                        `json:"grouping"`  // "eventType" or "requestType"
	Usage    map[string]*AccountUsage      `json:"usage"`    // Usage by account ID
}

// ResourceAccess tracks when a resource was accessed for de-duplication
type ResourceAccess struct {
	ResourceID string
	EventType  string
	Timestamp  time.Time
}

// CreditTracker tracks API usage and calculates costs.
type CreditTracker struct {
	pricing        *PricingConfig
	usage          map[string]map[string]*AccountUsage // accountID -> grouping -> AccountUsage
	endpointMapping map[string]string                  // endpoint path -> pricing type
	resourceAccess map[string]map[string]time.Time     // accountID -> resourceKey (eventType:resourceID) -> timestamp
	firstRequestTime map[string]time.Time              // accountID -> first request timestamp (for billing cycle calculation)
	mu             sync.RWMutex
}

// LoadPricingConfig loads the pricing configuration from API or cache.
// Falls back to hardcoded defaults if API fetch fails.
func LoadPricingConfig() *PricingConfig {
	return LoadPricingConfigWithRefresh(false)
}

// LoadPricingConfigWithRefresh loads the pricing configuration, optionally forcing a refresh.
// If forceRefresh is true, ignores cache and fetches fresh pricing from API.
func LoadPricingConfigWithRefresh(forceRefresh bool) *PricingConfig {
	// If not forcing refresh, try to load from cache first
	if !forceRefresh {
		if pricing := loadPricingFromCache(); pricing != nil {
			if cacheInfo := getPricingCacheInfo(); cacheInfo != nil {
				age := time.Since(cacheInfo.ModTime)
				log.Printf("Using cached pricing config (age: %s, location: %s)", formatPricingDuration(age), cacheInfo.Path)
			}
			return pricing
		}
		log.Printf("No valid pricing cache found, fetching from API")
	} else {
		log.Printf("Clearing pricing cache and forcing refresh")
		clearPricingCache()
	}

	// Try to fetch from API
	log.Printf("Fetching pricing config from %s (timeout: 10s)", pricingURL)
	pricing, err := fetchPricingFromAPI()
	if err != nil {
		log.Printf("Failed to fetch pricing from API: %v, using hardcoded defaults", err)
		return defaultPricingConfigHardcoded()
	}
	log.Printf("Successfully fetched pricing config from API")

	// Save to cache
	if err := savePricingToCache(pricing); err != nil {
		log.Printf("Warning: failed to cache pricing config: %v", err)
	} else {
		log.Printf("Cached pricing config to %s", getPricingCachePath())
	}

	return pricing
}

// defaultPricingConfigHardcoded returns hardcoded pricing as fallback.
// This is used when API fetch fails or cache is unavailable.
func defaultPricingConfigHardcoded() *PricingConfig {
	return &PricingConfig{
		EventTypePricing: map[string]float64{
			"ProfileUpdate":  0.005,
			"Like":           0.001,
			"Follow":          0.01,
			"Community":       0.005,
			"Space":           0.005,
			"Post":            0.005,
			"DirectMessage":   0.01,
			"Mute":            0.001,
			"User":            0.01,
			"List":             0.005,
			"Block":            0.001,
			"News":             0.005,
		},
		RequestTypePricing: map[string]float64{
			"DmInteractionCreate":  0.015,
			"InteractionDelete":    0.01,
			"MediaMetadata":         0.005,
			"PrivacyUpdate":         0.01,
			"ListManage":             0.005,
			"ContentManage":          0.005,
			"Bookmark":               0.005,
			"CountsRecent":           0.005,
			"Write":                  0.01,
			"Trends":                 0.01,
			"CountsAll":              0.01,
			"ContentCreate":          0.01,
			"UserInteractionCreate": 0.015,
			"MuteDelete":             0.005,
			"ListCreate":             0.01,
		},
		DefaultCost: 0,
	}
}

// PricingCacheInfo contains information about the cached pricing config
type PricingCacheInfo struct {
	Path    string
	ModTime time.Time
	Age     time.Duration
	Exists  bool
}

// getPricingCacheInfo returns information about the pricing cache file
func getPricingCacheInfo() *PricingCacheInfo {
	cachePath := getPricingCachePath()
	if cachePath == "" {
		return &PricingCacheInfo{
			Path:   "",
			Exists: false,
		}
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		return &PricingCacheInfo{
			Path:   cachePath,
			Exists: false,
		}
	}

	return &PricingCacheInfo{
		Path:    cachePath,
		ModTime: info.ModTime(),
		Age:     time.Since(info.ModTime()),
		Exists:  true,
	}
}

// getPricingCachePath returns the path to the pricing cache file
func getPricingCachePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".playground", pricingCacheFileName)
}

// clearPricingCache removes the cached pricing config
func clearPricingCache() error {
	cachePath := getPricingCachePath()
	if cachePath == "" {
		return fmt.Errorf("could not determine pricing cache path")
	}

	if err := os.Remove(cachePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to clear pricing cache: %w", err)
	}

	return nil
}

// formatPricingDuration formats a duration in a human-readable way
func formatPricingDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1f minutes", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}

// fetchPricingFromAPI fetches the pricing configuration from the API
func fetchPricingFromAPI() (*PricingConfig, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(pricingURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pricing from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read pricing response body: %w", err)
	}

	var pricing PricingConfig
	if err := json.Unmarshal(body, &pricing); err != nil {
		return nil, fmt.Errorf("failed to parse pricing config: %w", err)
	}

	// Validate that we got some pricing data
	if len(pricing.EventTypePricing) == 0 && len(pricing.RequestTypePricing) == 0 {
		return nil, fmt.Errorf("pricing config appears empty")
	}

	return &pricing, nil
}

// loadPricingFromCache loads the pricing config from cache if it exists and is fresh
func loadPricingFromCache() *PricingConfig {
	cachePath := getPricingCachePath()
	if cachePath == "" {
		return nil
	}

	// Check if cache file exists
	info, err := os.Stat(cachePath)
	if err != nil {
		return nil
	}

	// Check if cache is still fresh
	if time.Since(info.ModTime()) > pricingCacheMaxAge {
		return nil
	}

	// Read and parse cache
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}

	var pricing PricingConfig
	if err := json.Unmarshal(data, &pricing); err != nil {
		return nil
	}

	// Validate cache has pricing data
	if len(pricing.EventTypePricing) == 0 && len(pricing.RequestTypePricing) == 0 {
		return nil
	}

	return &pricing
}

// savePricingToCache saves the pricing config to cache
func savePricingToCache(pricing *PricingConfig) error {
	cachePath := getPricingCachePath()
	if cachePath == "" {
		return fmt.Errorf("failed to determine pricing cache path")
	}

	// Ensure directory exists
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.MarshalIndent(pricing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal pricing config for cache: %w", err)
	}

	return os.WriteFile(cachePath, data, 0644)
}

// DefaultPricingConfig returns the default pricing configuration.
// DEPRECATED: Use LoadPricingConfig() instead, which fetches from API.
// This function is kept for backward compatibility but now delegates to LoadPricingConfig.
func DefaultPricingConfig() *PricingConfig {
	return LoadPricingConfig()
}

// NewCreditTracker creates a new credit tracker with pricing from API.
// Loads pricing from https://console.x.com/api/credits/pricing (cached for 24 hours),
// falls back to hardcoded defaults if API fetch fails.
// Uses hardcoded endpoint-to-pricing-type mapping.
func NewCreditTracker() *CreditTracker {
	return &CreditTracker{
		pricing:         LoadPricingConfig(),
		usage:           make(map[string]map[string]*AccountUsage),
		endpointMapping: buildEndpointMapping(),
		resourceAccess:  make(map[string]map[string]time.Time),
		firstRequestTime: make(map[string]time.Time),
	}
}

// ReloadPricing reloads the pricing configuration from API or cache.
// This allows updating pricing without restarting the server.
func (ct *CreditTracker) ReloadPricing(forceRefresh bool) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	newPricing := LoadPricingConfigWithRefresh(forceRefresh)
	ct.pricing = newPricing
	
	log.Printf("Reloaded pricing configuration")
	return nil
}

// buildEndpointMapping builds a mapping from endpoint paths to pricing types.
// This maps API endpoints to their corresponding eventTypePricing or requestTypePricing.
// Returns a map where keys are "METHOD:/path" and values are pricing type names.
func buildEndpointMapping() map[string]string {
	mapping := make(map[string]string)
	
	// EventTypePricing mappings (charged per resource)
	// These endpoints charge based on the number of resources returned/fetched
	
	// Post endpoints - charge per post resource
	mapping["GET:/2/tweets"] = "Post"
	mapping["GET:/2/tweets/{id}"] = "Post"
	mapping["GET:/2/users/{id}/tweets"] = "Post"
	mapping["GET:/2/users/{id}/mentions"] = "Post"
	mapping["GET:/2/tweets/search/recent"] = "Post"
	mapping["GET:/2/tweets/search/all"] = "Post"
	mapping["GET:/2/users/{id}/timelines/reverse_chronological"] = "Post"
	mapping["GET:/2/tweets/{id}/retweets"] = "Post"
	mapping["GET:/2/users/reposts_of_me"] = "Post"
	mapping["GET:/2/tweets/{id}/quote_tweets"] = "Post"
	mapping["GET:/2/spaces/{id}/tweets"] = "Post"
	mapping["GET:/2/lists/{id}/tweets"] = "Post"
	// Streaming endpoints that stream posts/tweets
	mapping["GET:/2/tweets/firehose/stream"] = "Post"
	mapping["GET:/2/tweets/firehose/stream/lang/en"] = "Post"
	mapping["GET:/2/tweets/firehose/stream/lang/ja"] = "Post"
	mapping["GET:/2/tweets/firehose/stream/lang/ko"] = "Post"
	mapping["GET:/2/tweets/firehose/stream/lang/pt"] = "Post"
	mapping["GET:/2/tweets/sample/stream"] = "Post"
	mapping["GET:/2/tweets/sample10/stream"] = "Post"
	mapping["GET:/2/tweets/search/stream"] = "Post"
	mapping["GET:/2/tweets/compliance/stream"] = "Post"
	
	// User endpoints - charge per user resource
	mapping["GET:/2/users"] = "User"
	mapping["GET:/2/users/{id}"] = "User"
	mapping["GET:/2/users/by"] = "User"
	mapping["GET:/2/users/by/username/{username}"] = "User"
	mapping["GET:/2/users/{id}/followers"] = "User"
	mapping["GET:/2/users/{id}/following"] = "User"
	mapping["GET:/2/users/{id}/liked_tweets"] = "User"
	mapping["GET:/2/users/{id}/bookmarks"] = "User"
	mapping["GET:/2/users/me"] = "User"
	mapping["GET:/2/users/search"] = "User"
	mapping["GET:/2/users/{id}/muting"] = "User"
	mapping["GET:/2/users/{id}/blocking"] = "User"
	mapping["GET:/2/tweets/{id}/retweeted_by"] = "User"
	mapping["GET:/2/tweets/{id}/liking_users"] = "User"
	mapping["GET:/2/spaces/{id}/buyers"] = "User"
	// Streaming endpoints that stream users
	mapping["GET:/2/users/compliance/stream"] = "User"
	
	// Note: GET:/2/tweets/{id}/liking_users is mapped to "User" since it returns user resources
	
	// Like endpoints - charge per like event (including streams)
	mapping["GET:/2/likes/firehose/stream"] = "Like"
	mapping["GET:/2/likes/sample10/stream"] = "Like"
	mapping["GET:/2/likes/compliance/stream"] = "Like"
	
	// List endpoints - charge per list resource
	mapping["GET:/2/lists"] = "List"
	mapping["GET:/2/lists/{id}"] = "List"
	mapping["GET:/2/users/{id}/owned_lists"] = "List"
	mapping["GET:/2/users/{id}/list_memberships"] = "List"
	mapping["GET:/2/lists/{id}/members"] = "List"
	mapping["GET:/2/lists/{id}/followers"] = "List"
	mapping["GET:/2/lists/{id}/tweets"] = "List"
	mapping["GET:/2/users/{id}/followed_lists"] = "List"
	mapping["GET:/2/users/{id}/pinned_lists"] = "List"
	
	// Space endpoints - charge per space resource
	mapping["GET:/2/spaces"] = "Space"
	mapping["GET:/2/spaces/{id}"] = "Space"
	mapping["GET:/2/spaces/by/creator_ids"] = "Space"
	mapping["GET:/2/spaces/search"] = "Space"
	
	// Community endpoints - charge per community resource
	mapping["GET:/2/communities/{id}"] = "Community"
	mapping["GET:/2/communities/search"] = "Community"
	
	// DirectMessage endpoints - charge per DM event
	mapping["GET:/2/dm_conversations"] = "DirectMessage"
	mapping["GET:/2/dm_conversations/{dm_conversation_id}"] = "DirectMessage"
	mapping["GET:/2/dm_conversations/{id}"] = "DirectMessage" // Alternative parameter name
	mapping["GET:/2/dm_conversations/{dm_conversation_id}/dm_events"] = "DirectMessage"
	mapping["GET:/2/dm_conversations/{id}/dm_events"] = "DirectMessage" // Alternative parameter name
	mapping["GET:/2/dm_conversations/with/{participant_id}/dm_events"] = "DirectMessage"
	mapping["GET:/2/dm_events"] = "DirectMessage"
	mapping["GET:/2/dm_events/{event_id}"] = "DirectMessage"
	
	// RequestTypePricing mappings (charged per request)
	// These endpoints charge a fixed cost per request
	// Mapping based on official X API pricing documentation
	
	// DM_INTERACTION_CREATE ($0.015) - creating DM interactions
	mapping["POST:/2/dm_conversations"] = "DmInteractionCreate"
	mapping["POST:/2/dm_conversations/with/{participant_id}/messages"] = "DmInteractionCreate"
	mapping["POST:/2/dm_conversations/{dm_conversation_id}/messages"] = "DmInteractionCreate"
	
	// USER_INTERACTION_CREATE ($0.015) - creating user interactions
	mapping["POST:/2/users/{id}/retweets"] = "UserInteractionCreate"
	mapping["POST:/2/users/{id}/following"] = "UserInteractionCreate"
	mapping["POST:/2/users/{id}/likes"] = "UserInteractionCreate"
	
	// INTERACTION_DELETE ($0.010) - deleting interactions
	mapping["DELETE:/2/users/{id}/likes/{tweet_id}"] = "InteractionDelete"
	mapping["DELETE:/2/users/{source_user_id}/following/{target_user_id}"] = "InteractionDelete"
	mapping["DELETE:/2/dm_events/{event_id}"] = "InteractionDelete"
	
	// CONTENT_CREATE ($0.010) - creating posts/tweets and media
	mapping["POST:/2/tweets"] = "ContentCreate"
	mapping["POST:/2/media/upload"] = "ContentCreate"
	mapping["POST:/2/media/upload/initialize"] = "ContentCreate"
	mapping["POST:/2/media/upload/{id}/append"] = "ContentCreate"
	mapping["POST:/2/media/upload/{id}/finalize"] = "ContentCreate"
	
	// CONTENT_MANAGE ($0.005) - managing content
	mapping["DELETE:/2/tweets/{id}"] = "ContentManage"
	mapping["PUT:/2/tweets/{tweet_id}/hidden"] = "ContentManage"
	mapping["DELETE:/2/users/{id}/retweets/{source_tweet_id}"] = "ContentManage"
	
	// LIST_CREATE ($0.010) - creating lists and list operations
	mapping["POST:/2/lists"] = "ListCreate"
	mapping["POST:/2/users/{id}/followed_lists"] = "ListCreate"
	mapping["POST:/2/lists/{id}/members"] = "ListCreate"
	mapping["POST:/2/users/{id}/pinned_lists"] = "ListCreate"
	
	// LIST_MANAGE ($0.005) - managing lists
	mapping["DELETE:/2/lists/{id}"] = "ListManage"
	mapping["DELETE:/2/users/{id}/followed_lists/{list_id}"] = "ListManage"
	mapping["DELETE:/2/lists/{id}/members/{user_id}"] = "ListManage"
	mapping["DELETE:/2/users/{id}/pinned_lists/{list_id}"] = "ListManage"
	mapping["PUT:/2/lists/{id}"] = "ListManage"
	
	// BOOKMARK ($0.005) - bookmark operations
	mapping["POST:/2/users/{id}/bookmarks"] = "Bookmark"
	mapping["DELETE:/2/users/{id}/bookmarks/{tweet_id}"] = "Bookmark"
	
	// MEDIA_METADATA ($0.005) - media metadata operations
	mapping["POST:/2/media/metadata"] = "MediaMetadata"
	mapping["POST:/2/media/subtitles"] = "MediaMetadata"
	mapping["DELETE:/2/media/subtitles"] = "MediaMetadata"
	
	// PRIVACY_UPDATE ($0.010) - privacy settings updates
	mapping["POST:/2/users/{id}/muting"] = "PrivacyUpdate"
	mapping["POST:/2/users/{id}/dm/block"] = "PrivacyUpdate"
	mapping["POST:/2/users/{id}/dm/unblock"] = "PrivacyUpdate"
	
	// MUTE_DELETE ($0.005) - unmuting
	mapping["DELETE:/2/users/{source_user_id}/muting/{target_user_id}"] = "MuteDelete"
	
	// COUNTS_RECENT ($0.005) - recent counts endpoints
	mapping["GET:/2/tweets/counts/recent"] = "CountsRecent"
	
	// COUNTS_ALL ($0.010) - all counts endpoints
	mapping["GET:/2/tweets/counts/all"] = "CountsAll"
	
	// TRENDS ($0.010) - trends endpoints
	mapping["GET:/2/trends/by/woeid/{woeid}"] = "Trends"
	mapping["GET:/2/users/personalized_trends"] = "Trends"
	
	return mapping
}

// getPricingType returns the pricing type for an endpoint.
// Returns the eventType or requestType, and a boolean indicating if it's an eventType.
func (ct *CreditTracker) getPricingType(method, path string) (string, bool) {
	key := fmt.Sprintf("%s:%s", method, path)
	
	// Try exact match first
	if pricingType, exists := ct.endpointMapping[key]; exists {
		// Check if it's an eventType
		if _, isEventType := ct.pricing.EventTypePricing[pricingType]; isEventType {
			return pricingType, true
		}
		// Otherwise it's a requestType
		return pricingType, false
	}
	
	// Try pattern matching for parameterized paths
	pathParts := strings.Split(path, "/")
	for endpointKey, pricingType := range ct.endpointMapping {
		parts := strings.Split(endpointKey, ":")
		if len(parts) != 2 {
			continue
		}
		endpointMethod := parts[0]
		endpointPath := parts[1]
		
		if endpointMethod != method {
			continue
		}
		
		endpointParts := strings.Split(endpointPath, "/")
		if len(endpointParts) != len(pathParts) {
			continue
		}
		
		matches := true
		for i, part := range endpointParts {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				// Parameter placeholder, skip
				continue
			}
			if part != pathParts[i] {
				matches = false
				break
			}
		}
		
		if matches {
			// Check if it's an eventType
			if _, isEventType := ct.pricing.EventTypePricing[pricingType]; isEventType {
				return pricingType, true
			}
			// Otherwise it's a requestType
			return pricingType, false
		}
	}
	
	return "", false
}

// extractResourceIDs extracts resource IDs from a JSON response based on event type.
// Returns a map of eventType -> []resourceIDs for de-duplication tracking.
func extractResourceIDs(responseData []byte, eventType string) []string {
	if len(responseData) == 0 {
		return nil
	}
	
	var response map[string]interface{}
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil
	}
	
	var resourceIDs []string
	
	// Extract IDs from data array or object
	if data, exists := response["data"]; exists {
		switch v := data.(type) {
		case []interface{}:
			// Array of resources
			for _, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := extractIDFromResource(itemMap); ok {
						resourceIDs = append(resourceIDs, id)
					}
				}
			}
		case map[string]interface{}:
			// Single resource object
			if id, ok := extractIDFromResource(v); ok {
				resourceIDs = append(resourceIDs, id)
			}
		}
	}
	
	// Extract IDs from includes (expansions) based on event type
	if includes, exists := response["includes"]; exists {
		if includesMap, ok := includes.(map[string]interface{}); ok {
			// Map event types to include keys
			includeKey := getIncludeKeyForEventType(eventType)
			if includeKey != "" {
				if items, exists := includesMap[includeKey]; exists {
					if itemsArray, ok := items.([]interface{}); ok {
						for _, item := range itemsArray {
							if itemMap, ok := item.(map[string]interface{}); ok {
								if id, ok := extractIDFromResource(itemMap); ok {
									resourceIDs = append(resourceIDs, id)
								}
							}
						}
					}
				}
			}
		}
	}
	
	return resourceIDs
}

// extractIDFromResource extracts the ID field from a resource object.
func extractIDFromResource(resource map[string]interface{}) (string, bool) {
	if idVal, exists := resource["id"]; exists {
		return idValueToString(idVal)
	}
	return "", false
}

// getIncludeKeyForEventType maps event types to their corresponding include keys in API responses.
func getIncludeKeyForEventType(eventType string) string {
	switch eventType {
	case "Post":
		return "tweets"
	case "User":
		return "users"
	case "List":
		return "lists"
	case "Space":
		return "spaces"
	case "Community":
		return "communities"
	case "DirectMessage":
		return "messages"
	default:
		return ""
	}
}

// TrackUsage records usage for an API request.
// accountID: The developer account ID making the request (not the user ID).
//            In the real X API, this is extracted from the OAuth token/app credentials.
//            In the playground, this is derived from the authentication token using
//            getDeveloperAccountID(), which extracts or derives the developer account ID
//            from Bearer tokens, OAuth 1.0a consumer keys, or OAuth 2.0 tokens.
// method: HTTP method (GET, POST, etc.)
// path: API endpoint path
// responseData: The response data (for extracting resource IDs for de-duplication)
// statusCode: HTTP status code (only track successful requests)
func (ct *CreditTracker) TrackUsage(accountID, method, path string, responseData []byte, statusCode int) {
	// Only track successful requests (2xx status codes)
	if statusCode < 200 || statusCode >= 300 {
		return
	}
	
	pricingType, isEventType := ct.getPricingType(method, path)
	if pricingType == "" {
		// Unknown endpoint, use default cost or skip
		return
	}
	
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	// Track first request time for billing cycle calculation
	now := time.Now()
	if _, exists := ct.firstRequestTime[accountID]; !exists {
		ct.firstRequestTime[accountID] = now
	}
	
	// Initialize account usage if needed
	if ct.usage[accountID] == nil {
		ct.usage[accountID] = make(map[string]*AccountUsage)
	}
	
	// Determine grouping type
	grouping := "requestType"
	if isEventType {
		grouping = "eventType"
	}
	
	// Initialize account usage for this grouping if needed
	if ct.usage[accountID][grouping] == nil {
		ct.usage[accountID][grouping] = &AccountUsage{
			UsageUnit: grouping,
			Cap:       "0", // No cap in playground
			Groups:    make(map[string]*UsageGroup),
			Total: &UsageGroup{
				Usage:          "0",
				PercentOfCap:   0.0,
				UsageDataPoints: make([]UsageDataPoint, 0),
			},
		}
	}
	
	accountUsage := ct.usage[accountID][grouping]
	
	// Initialize group if needed
	if accountUsage.Groups[pricingType] == nil {
		accountUsage.Groups[pricingType] = &UsageGroup{
			Usage:          "0",
			PercentOfCap:   0.0,
			UsageDataPoints: make([]UsageDataPoint, 0),
		}
	}
	
	group := accountUsage.Groups[pricingType]
	
	// Calculate usage amount with de-duplication for event types
	var usageAmount int
	deDupWindow := 24 * time.Hour
	
	if isEventType {
		// For eventTypePricing, extract resource IDs and apply de-duplication
		resourceIDs := extractResourceIDs(responseData, pricingType)
		
		// Initialize resource access tracking for this account if needed
		if ct.resourceAccess[accountID] == nil {
			ct.resourceAccess[accountID] = make(map[string]time.Time)
		}
		
		// Count unique resources (not accessed within 24 hours)
		uniqueCount := 0
		for _, resourceID := range resourceIDs {
			resourceKey := fmt.Sprintf("%s:%s", pricingType, resourceID)
			lastAccess, exists := ct.resourceAccess[accountID][resourceKey]
			
			if !exists || now.Sub(lastAccess) >= deDupWindow {
				// Resource not accessed before, or last access was more than 24 hours ago
				// Track this access and count it
				ct.resourceAccess[accountID][resourceKey] = now
				uniqueCount++
			}
			// If resource was accessed within 24 hours, skip it (don't charge again)
		}
		
		// Clean up old entries (older than 24 hours) to prevent memory bloat
		// Only clean up a limited number per request to avoid performance issues
		cleanupCount := 0
		maxCleanupPerRequest := 100
		for key, accessTime := range ct.resourceAccess[accountID] {
			if now.Sub(accessTime) >= deDupWindow {
				delete(ct.resourceAccess[accountID], key)
				cleanupCount++
				if cleanupCount >= maxCleanupPerRequest {
					break
				}
			}
		}
		
		usageAmount = uniqueCount
		if usageAmount == 0 && len(resourceIDs) == 0 {
			// If no resources extracted, assume 1 (at least one resource was accessed)
			// But don't track it for de-duplication since we don't have an ID
			usageAmount = 1
		}
	} else {
		// For requestTypePricing, usage is 1 request
		usageAmount = 1
	}
	
	// Update usage
	currentUsage, _ := strconv.Atoi(group.Usage)
	group.Usage = strconv.Itoa(currentUsage + usageAmount)
	
	// Update total usage
	currentTotal, _ := strconv.Atoi(accountUsage.Total.Usage)
	accountUsage.Total.Usage = strconv.Itoa(currentTotal + usageAmount)
	
	// Add data point
	timestamp := now.UnixMilli()
	dataPoint := UsageDataPoint{
		Timestamp: strconv.FormatInt(timestamp, 10),
		Value:     strconv.Itoa(usageAmount),
	}
	group.UsageDataPoints = append(group.UsageDataPoints, dataPoint)
	accountUsage.Total.UsageDataPoints = append(accountUsage.Total.UsageDataPoints, dataPoint)
}

// GetPricing returns the current pricing configuration.
func (ct *CreditTracker) GetPricing() *PricingConfig {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	// Return a copy to prevent external modification
	pricingCopy := &PricingConfig{
		EventTypePricing:  make(map[string]float64),
		RequestTypePricing: make(map[string]float64),
		DefaultCost:        ct.pricing.DefaultCost,
	}
	
	for k, v := range ct.pricing.EventTypePricing {
		pricingCopy.EventTypePricing[k] = v
	}
	for k, v := range ct.pricing.RequestTypePricing {
		pricingCopy.RequestTypePricing[k] = v
	}
	
	return pricingCopy
}

// GetUsage returns usage data for an account.
// accountID: The account ID
// interval: Time interval (e.g., "30days", "7days")
// groupBy: Grouping type ("eventType" or "requestType")
func (ct *CreditTracker) GetUsage(accountID, interval, groupBy string) *UsageResponse {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	// Calculate date range based on interval
	now := time.Now()
	var fromDate time.Time
	var bucket string
	
	switch interval {
	case "7days":
		fromDate = now.AddDate(0, 0, -7)
		bucket = "7days"
	case "30days":
		fromDate = now.AddDate(0, 0, -30)
		bucket = "30days"
	case "90days":
		fromDate = now.AddDate(0, 0, -90)
		bucket = "90days"
	default:
		// Default to 30 days
		fromDate = now.AddDate(0, 0, -30)
		bucket = "30days"
	}
	
	// Get account usage for the specified grouping
	var accountUsage *AccountUsage
	if ct.usage[accountID] != nil {
		accountUsage = ct.usage[accountID][groupBy]
	}
	
	// If no usage exists, return empty structure
	if accountUsage == nil {
		accountUsage = &AccountUsage{
			UsageUnit: groupBy,
			Cap:       "0",
			Groups:    make(map[string]*UsageGroup),
			Total: &UsageGroup{
				Usage:          "0",
				PercentOfCap:   0.0,
				UsageDataPoints: make([]UsageDataPoint, 0),
			},
		}
	}
	
	// Generate daily data points for the interval
	// This matches the format of the real API
	days := int(now.Sub(fromDate).Hours() / 24)
	if days < 1 {
		days = 1
	}
	
	// Create data points for each day (work on copies to avoid modifying stored data)
	generateDataPoints := func(group *UsageGroup) []UsageDataPoint {
		if len(group.UsageDataPoints) == 0 {
			// Initialize with zero data points for each day
			result := make([]UsageDataPoint, days)
			for i := 0; i < days; i++ {
				dayTime := fromDate.AddDate(0, 0, i)
				result[i] = UsageDataPoint{
					Timestamp: strconv.FormatInt(dayTime.UnixMilli(), 10),
					Value:     "0",
				}
			}
			return result
		} else {
			// Aggregate existing data points by day
			dailyValues := make(map[int64]int) // timestamp (day start) -> count
			for _, dp := range group.UsageDataPoints {
				timestamp, _ := strconv.ParseInt(dp.Timestamp, 10, 64)
				dayStart := time.UnixMilli(timestamp).Truncate(24 * time.Hour).UnixMilli()
				value, _ := strconv.Atoi(dp.Value)
				dailyValues[dayStart] += value
			}
			
			// Create data points for each day in the interval
			result := make([]UsageDataPoint, days)
			for i := 0; i < days; i++ {
				dayTime := fromDate.AddDate(0, 0, i).Truncate(24 * time.Hour)
				timestamp := dayTime.UnixMilli()
				value := dailyValues[timestamp]
				result[i] = UsageDataPoint{
					Timestamp: strconv.FormatInt(timestamp, 10),
					Value:     strconv.Itoa(value),
				}
			}
			return result
		}
	}
	
	// Create a copy of accountUsage to avoid modifying the stored data
	accountUsageCopy := &AccountUsage{
		UsageUnit: accountUsage.UsageUnit,
		Cap:       accountUsage.Cap,
		Groups:    make(map[string]*UsageGroup),
		Total: &UsageGroup{
			Usage:          accountUsage.Total.Usage,
			PercentOfCap:   accountUsage.Total.PercentOfCap,
			UsageDataPoints: generateDataPoints(accountUsage.Total),
		},
	}
	
	// Generate data points for all groups (on copies)
	for pricingType, group := range accountUsage.Groups {
		accountUsageCopy.Groups[pricingType] = &UsageGroup{
			Usage:          group.Usage,
			PercentOfCap:   group.PercentOfCap,
			UsageDataPoints: generateDataPoints(group),
		}
	}
	
	return &UsageResponse{
		Bucket:   bucket,
		FromDate: fromDate.Format(time.RFC3339),
		ToDate:   now.Format(time.RFC3339),
		Grouping: groupBy,
		Usage: map[string]*AccountUsage{
			accountID: accountUsageCopy,
		},
	}
}

// ResetUsage resets usage for an account (or all accounts if accountID is empty).
func (ct *CreditTracker) ResetUsage(accountID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	if accountID == "" {
		// Reset all accounts
		ct.usage = make(map[string]map[string]*AccountUsage)
	} else {
		// Reset specific account
		delete(ct.usage, accountID)
	}
}

// CostBreakdown represents a cost breakdown for a single pricing type.
type CostBreakdown struct {
	Type      string  `json:"type"`      // Event type or request type name
	Usage     int     `json:"usage"`      // Number of resources/requests used
	Price     float64 `json:"price"`      // Price per unit
	TotalCost float64 `json:"totalCost"`  // Total cost for this type
}

// BillingCycleDataPoint represents cost data for a single day in a billing cycle.
type BillingCycleDataPoint struct {
	Date      string             `json:"date"`      // Date in YYYY-MM-DD format
	Timestamp int64              `json:"timestamp"` // Unix timestamp (milliseconds)
	Costs     map[string]float64 `json:"costs"`    // type -> cost for that day
}

// CostBreakdownResponse represents the cost breakdown response.
type CostBreakdownResponse struct {
	AccountID            string                 `json:"accountId"`
	TotalCost            float64                `json:"totalCost"`
	EventTypeCosts       []CostBreakdown        `json:"eventTypeCosts"`
	RequestTypeCosts     []CostBreakdown        `json:"requestTypeCosts"`
	BillingCycleStart    string                 `json:"billingCycleStart"`    // ISO 8601 date
	CurrentBillingCycle int                    `json:"currentBillingCycle"`  // Cycle number (1, 2, 3, ...)
	EventTypeTimeSeries  []BillingCycleDataPoint `json:"eventTypeTimeSeries"` // Daily costs over billing cycle
	RequestTypeTimeSeries []BillingCycleDataPoint `json:"requestTypeTimeSeries"` // Daily costs over billing cycle
}

// truncateToLocalMidnight truncates a time to midnight in the local timezone.
func truncateToLocalMidnight(t time.Time) time.Time {
	local := t.Local()
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, local.Location())
}

// CalculateCost calculates the total cost for an account based on usage.
func (ct *CreditTracker) CalculateCost(accountID string) float64 {
	breakdown := ct.GetCostBreakdown(accountID)
	return breakdown.TotalCost
}

// GetCostBreakdown returns a detailed cost breakdown for an account.
func (ct *CreditTracker) GetCostBreakdown(accountID string) *CostBreakdownResponse {
	ct.mu.RLock()
	
	// Ensure pricing is loaded
	if ct.pricing == nil {
		ct.mu.RUnlock()
		ct.mu.Lock()
		if ct.pricing == nil {
			ct.pricing = LoadPricingConfig()
		}
		ct.mu.Unlock()
		ct.mu.RLock()
	}
	defer ct.mu.RUnlock()
	
	now := time.Now()
	
	// Get first request time (default to now if not set)
	firstRequestTime, exists := ct.firstRequestTime[accountID]
	if !exists {
		firstRequestTime = now
	}
	
	// Calculate current billing cycle (30-day periods from first request)
	daysSinceFirstRequest := int(now.Sub(firstRequestTime).Hours() / 24)
	if daysSinceFirstRequest < 0 {
		daysSinceFirstRequest = 0
	}
	currentBillingCycle := (daysSinceFirstRequest / 30) + 1
	billingCycleStart := firstRequestTime.AddDate(0, 0, (currentBillingCycle-1)*30)
	billingCycleDayStart := truncateToLocalMidnight(billingCycleStart)
	
	// Calculate days into current billing cycle
	nowLocalMidnight := truncateToLocalMidnight(now)
	daysIntoCycle := int(nowLocalMidnight.Sub(billingCycleDayStart).Hours() / 24)
	if daysIntoCycle < 0 {
		daysIntoCycle = 0
	}
	if daysIntoCycle > 30 {
		daysIntoCycle = 30
	}
	
	response := &CostBreakdownResponse{
		AccountID:            accountID,
		TotalCost:            0.0,
		EventTypeCosts:       make([]CostBreakdown, 0),
		RequestTypeCosts:     make([]CostBreakdown, 0),
		BillingCycleStart:     billingCycleDayStart.Format(time.RFC3339),
		CurrentBillingCycle:  currentBillingCycle,
		EventTypeTimeSeries:   make([]BillingCycleDataPoint, 0),
		RequestTypeTimeSeries: make([]BillingCycleDataPoint, 0),
	}
	
	if ct.usage == nil || ct.usage[accountID] == nil {
		// Initialize empty time series for the billing cycle
		for i := 0; i <= daysIntoCycle; i++ {
			dayTime := truncateToLocalMidnight(billingCycleDayStart.AddDate(0, 0, i))
			response.EventTypeTimeSeries = append(response.EventTypeTimeSeries, BillingCycleDataPoint{
				Date:      dayTime.Format("2006-01-02"),
				Timestamp: dayTime.UnixMilli(),
				Costs:     make(map[string]float64),
			})
			response.RequestTypeTimeSeries = append(response.RequestTypeTimeSeries, BillingCycleDataPoint{
				Date:      dayTime.Format("2006-01-02"),
				Timestamp: dayTime.UnixMilli(),
				Costs:     make(map[string]float64),
			})
		}
		return response
	}
	
	// Initialize time series maps for daily aggregation
	eventTypeDailyCosts := make(map[string]map[string]float64) // date -> type -> cost
	requestTypeDailyCosts := make(map[string]map[string]float64) // date -> type -> cost
	
	// Initialize time series for all days in current billing cycle
	for i := 0; i <= daysIntoCycle; i++ {
		dayTime := truncateToLocalMidnight(billingCycleDayStart.AddDate(0, 0, i))
		dateKey := dayTime.Format("2006-01-02")
		eventTypeDailyCosts[dateKey] = make(map[string]float64)
		requestTypeDailyCosts[dateKey] = make(map[string]float64)
	}
	
	// Calculate cost for eventTypePricing (resources) and aggregate by day
	if eventUsage := ct.usage[accountID]["eventType"]; eventUsage != nil && ct.pricing != nil {
		for eventType, group := range eventUsage.Groups {
			if group == nil {
				continue
			}
			if price, exists := ct.pricing.EventTypePricing[eventType]; exists {
				usage, _ := strconv.Atoi(group.Usage)
				totalCost := float64(usage) * price
				response.EventTypeCosts = append(response.EventTypeCosts, CostBreakdown{
					Type:      eventType,
					Usage:     usage,
					Price:     price,
					TotalCost: totalCost,
				})
				response.TotalCost += totalCost
				
				// Aggregate usage data points by day within current billing cycle
				hasDataPoints := false
				if group.UsageDataPoints != nil {
					for _, dp := range group.UsageDataPoints {
						if dp.Timestamp == "" {
							continue
						}
						timestamp, err := strconv.ParseInt(dp.Timestamp, 10, 64)
						if err != nil {
							continue
						}
						dpTime := time.UnixMilli(timestamp)
						
						// Truncate to local midnight for comparison (use the already-calculated billingCycleDayStart)
						dpDayStart := truncateToLocalMidnight(dpTime)
						
						// Only include data points within current billing cycle
						if dpDayStart.Before(billingCycleDayStart) || dpDayStart.After(nowLocalMidnight) {
							continue
						}
						
						// Calculate days since cycle start - use date key directly from dpDayStart
						// This ensures we use the exact same date that the data point represents
						dateKey := dpDayStart.Format("2006-01-02")
						
						// Ensure the date key exists in the map
						if eventTypeDailyCosts[dateKey] == nil {
							eventTypeDailyCosts[dateKey] = make(map[string]float64)
						}
						
						value, _ := strconv.Atoi(dp.Value)
						if value > 0 {
							dailyCost := float64(value) * price
							eventTypeDailyCosts[dateKey][eventType] += dailyCost
							hasDataPoints = true
						}
					}
				}
				
				// If we have usage but no data points in the billing cycle, distribute it to today
				if usage > 0 && !hasDataPoints && daysIntoCycle >= 0 {
					todayKey := nowLocalMidnight.Format("2006-01-02")
					if eventTypeDailyCosts[todayKey] == nil {
						eventTypeDailyCosts[todayKey] = make(map[string]float64)
					}
					eventTypeDailyCosts[todayKey][eventType] += totalCost
				}
			}
		}
	}
	
	// Calculate cost for requestTypePricing (requests) and aggregate by day
	if requestUsage := ct.usage[accountID]["requestType"]; requestUsage != nil && ct.pricing != nil {
		for requestType, group := range requestUsage.Groups {
			if group == nil {
				continue
			}
			if price, exists := ct.pricing.RequestTypePricing[requestType]; exists {
				usage, _ := strconv.Atoi(group.Usage)
				totalCost := float64(usage) * price
				response.RequestTypeCosts = append(response.RequestTypeCosts, CostBreakdown{
					Type:      requestType,
					Usage:     usage,
					Price:     price,
					TotalCost: totalCost,
				})
				response.TotalCost += totalCost
				
				// Aggregate usage data points by day within current billing cycle
				hasDataPoints := false
				if group.UsageDataPoints != nil {
					for _, dp := range group.UsageDataPoints {
						if dp.Timestamp == "" {
							continue
						}
						timestamp, err := strconv.ParseInt(dp.Timestamp, 10, 64)
						if err != nil {
							continue
						}
						dpTime := time.UnixMilli(timestamp)
						
						// Truncate to local midnight for comparison (use the already-calculated billingCycleDayStart)
						dpDayStart := truncateToLocalMidnight(dpTime)
						
						// Only include data points within current billing cycle
						if dpDayStart.Before(billingCycleDayStart) || dpDayStart.After(nowLocalMidnight) {
							continue
						}
						
						// Calculate days since cycle start - use date key directly from dpDayStart
						// This ensures we use the exact same date that the data point represents
						dateKey := dpDayStart.Format("2006-01-02")
						
						// Ensure the date key exists in the map
						if requestTypeDailyCosts[dateKey] == nil {
							requestTypeDailyCosts[dateKey] = make(map[string]float64)
						}
						
						value, _ := strconv.Atoi(dp.Value)
						if value > 0 {
							dailyCost := float64(value) * price
							requestTypeDailyCosts[dateKey][requestType] += dailyCost
							hasDataPoints = true
						}
					}
				}
				
				// If we have usage but no data points in the billing cycle, distribute it to today
				if usage > 0 && !hasDataPoints && daysIntoCycle >= 0 {
					todayKey := nowLocalMidnight.Format("2006-01-02")
					if requestTypeDailyCosts[todayKey] == nil {
						requestTypeDailyCosts[todayKey] = make(map[string]float64)
					}
					requestTypeDailyCosts[todayKey][requestType] += totalCost
				}
			}
		}
	}
	
	// Build time series arrays
	for i := 0; i <= daysIntoCycle; i++ {
		dayTime := truncateToLocalMidnight(billingCycleDayStart.AddDate(0, 0, i))
		dateKey := dayTime.Format("2006-01-02")
		
		// Ensure costs map exists (even if empty)
		eventCosts := eventTypeDailyCosts[dateKey]
		if eventCosts == nil {
			eventCosts = make(map[string]float64)
		}
		
		requestCosts := requestTypeDailyCosts[dateKey]
		if requestCosts == nil {
			requestCosts = make(map[string]float64)
		}
		
		response.EventTypeTimeSeries = append(response.EventTypeTimeSeries, BillingCycleDataPoint{
			Date:      dateKey,
			Timestamp: dayTime.UnixMilli(),
			Costs:     eventCosts,
		})
		
		response.RequestTypeTimeSeries = append(response.RequestTypeTimeSeries, BillingCycleDataPoint{
			Date:      dateKey,
			Timestamp: dayTime.UnixMilli(),
			Costs:     requestCosts,
		})
	}
	
	return response
}

// LoadPricingFromJSON loads pricing configuration from JSON data.
func (ct *CreditTracker) LoadPricingFromJSON(data []byte) error {
	var config PricingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal pricing config: %w", err)
	}
	
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	ct.pricing = &config
	log.Printf("Loaded pricing configuration")
	return nil
}

// ExportUsage exports the usage data for persistence.
// Returns a copy of the usage map.
func (ct *CreditTracker) ExportUsage() map[string]map[string]*AccountUsage {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	export := make(map[string]map[string]*AccountUsage)
	for accountID, groupings := range ct.usage {
		export[accountID] = make(map[string]*AccountUsage)
		for grouping, accountUsage := range groupings {
			// Deep copy the AccountUsage
			accountUsageCopy := &AccountUsage{
				UsageUnit: accountUsage.UsageUnit,
				Cap:       accountUsage.Cap,
				Groups:    make(map[string]*UsageGroup),
				Total: &UsageGroup{
					Usage:          accountUsage.Total.Usage,
					PercentOfCap:    accountUsage.Total.PercentOfCap,
					UsageDataPoints: make([]UsageDataPoint, len(accountUsage.Total.UsageDataPoints)),
				},
			}
			copy(accountUsageCopy.Total.UsageDataPoints, accountUsage.Total.UsageDataPoints)
			
			for groupType, group := range accountUsage.Groups {
				accountUsageCopy.Groups[groupType] = &UsageGroup{
					Usage:          group.Usage,
					PercentOfCap:   group.PercentOfCap,
					UsageDataPoints: make([]UsageDataPoint, len(group.UsageDataPoints)),
				}
				copy(accountUsageCopy.Groups[groupType].UsageDataPoints, group.UsageDataPoints)
			}
			
			export[accountID][grouping] = accountUsageCopy
		}
	}
	
	return export
}

// ExportResourceAccess exports the resource access data for persistence.
// Returns a map where timestamps are serialized as ISO 8601 strings.
func (ct *CreditTracker) ExportResourceAccess() map[string]map[string]string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	export := make(map[string]map[string]string)
	for accountID, resources := range ct.resourceAccess {
		export[accountID] = make(map[string]string)
		for resourceKey, timestamp := range resources {
			export[accountID][resourceKey] = timestamp.Format(time.RFC3339)
		}
	}
	
	return export
}

// ExportFirstRequestTime exports the first request time for each account.
// Returns a map where timestamps are serialized as ISO 8601 strings.
func (ct *CreditTracker) ExportFirstRequestTime() map[string]string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	export := make(map[string]string)
	for accountID, timestamp := range ct.firstRequestTime {
		export[accountID] = timestamp.Format(time.RFC3339)
	}
	
	return export
}

// ImportUsage imports usage data from persistence.
func (ct *CreditTracker) ImportUsage(usage map[string]map[string]*AccountUsage) {
	if usage == nil {
		return
	}
	
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	ct.usage = usage
	log.Printf("Imported credit usage data for %d account(s)", len(usage))
}

// ImportResourceAccess imports resource access data from persistence.
// Expects timestamps as ISO 8601 strings.
func (ct *CreditTracker) ImportResourceAccess(resourceAccess map[string]map[string]string) {
	if resourceAccess == nil {
		return
	}
	
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	ct.resourceAccess = make(map[string]map[string]time.Time)
	for accountID, resources := range resourceAccess {
		ct.resourceAccess[accountID] = make(map[string]time.Time)
		for resourceKey, timestampStr := range resources {
			if timestamp, err := time.Parse(time.RFC3339, timestampStr); err == nil {
				ct.resourceAccess[accountID][resourceKey] = timestamp
			} else {
				log.Printf("Warning: Failed to parse resource access timestamp for %s:%s: %v", accountID, resourceKey, err)
			}
		}
	}
	log.Printf("Imported resource access data for %d account(s)", len(resourceAccess))
}

// ImportFirstRequestTime imports first request time data from persistence.
// Expects timestamps as ISO 8601 strings.
func (ct *CreditTracker) ImportFirstRequestTime(firstRequestTime map[string]string) {
	if firstRequestTime == nil {
		return
	}
	
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	ct.firstRequestTime = make(map[string]time.Time)
	for accountID, timestampStr := range firstRequestTime {
		if timestamp, err := time.Parse(time.RFC3339, timestampStr); err == nil {
			ct.firstRequestTime[accountID] = timestamp
		} else {
			log.Printf("Warning: Failed to parse first request time for %s: %v", accountID, err)
		}
	}
	log.Printf("Imported first request time for %d account(s)", len(firstRequestTime))
}

// Reset clears all credit tracking data (usage and resource access).
func (ct *CreditTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	ct.usage = make(map[string]map[string]*AccountUsage)
	ct.resourceAccess = make(map[string]map[string]time.Time)
	ct.firstRequestTime = make(map[string]time.Time)
	log.Printf("Credit tracking data reset")
}
