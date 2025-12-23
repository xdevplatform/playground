// Package playground provides response templates matching the X API v2 format.
//
// This file defines Go struct types that mirror the JSON response structures
// returned by the X API. These templates are used to ensure consistent response
// formatting across all endpoints, including proper nesting of data objects,
// meta information, and error structures.
package playground

import (
	"encoding/json"
	"fmt"
	"time"
)

// UserResponse represents a user response in X API format
type UserResponse struct {
	Data *User `json:"data"`
}

// UsersResponse represents multiple users response
type UsersResponse struct {
	Data []*User `json:"data"`
}

// TweetResponse represents a tweet response in X API format
type TweetResponse struct {
	Data *Tweet `json:"data"`
}

// TweetsResponse represents multiple tweets response
type TweetsResponse struct {
	Data []*Tweet `json:"data"`
	Meta struct {
		ResultCount int `json:"result_count"`
	} `json:"meta"`
}

// MediaInitResponse represents media initialization response
type MediaInitResponse struct {
	Data struct {
		ID               string `json:"id"`
		ExpiresAfterSecs int    `json:"expires_after_secs"`
		MediaKey         string `json:"media_key"`
	} `json:"data"`
}

// MediaStatusResponse represents media status response
type MediaStatusResponse struct {
	Data struct {
		ID               string          `json:"id"`
		MediaKey         string          `json:"media_key"`
		ProcessingInfo   *ProcessingInfo `json:"processing_info,omitempty"`
		ExpiresAfterSecs int             `json:"expires_after_secs"`
	} `json:"data"`
}

// ErrorResponse represents an error response matching real X API format
type ErrorResponse struct {
	Errors []ErrorDetail `json:"errors"`
	Title  string        `json:"title,omitempty"`
	Detail string        `json:"detail,omitempty"`
	Type   string        `json:"type,omitempty"`
}

// ErrorDetail represents a single error in the errors array
// Matches real X API error format
type ErrorDetail struct {
	Message     string `json:"message,omitempty"`
	Code        int    `json:"code,omitempty"`
	Parameter   string `json:"parameter,omitempty"`
	Value       string `json:"value,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Title       string `json:"title,omitempty"`
	Type        string `json:"type,omitempty"`
	ResourceID  string `json:"resource_id,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Status      int    `json:"status,omitempty"`
}

// OAuthTokenResponse represents OAuth2 token response
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// CreateErrorResponse creates an error response matching real X API format
// For general errors, use a simple format. For validation errors, use CreateValidationErrorResponse
func CreateErrorResponse(message string, code int) *ErrorResponse {
	// Map common error codes to X API error types
	errorType := "https://api.twitter.com/2/problems/unknown"
	title := "An error occurred"
	detail := message
	
	switch code {
	case 404:
		errorType = "https://api.twitter.com/2/problems/resource-not-found"
		title = "Not Found Error"
		detail = message
	case 401:
		errorType = "https://api.twitter.com/2/problems/not-authorized-for-resource"
		title = "Unauthorized"
		detail = message
	case 403:
		errorType = "https://api.twitter.com/2/problems/forbidden"
		title = "Forbidden"
		detail = message
	case 405:
		errorType = "https://api.twitter.com/2/problems/not-allowed"
		title = "Method Not Allowed"
		detail = message
	case 429:
		errorType = "https://api.twitter.com/2/problems/rate-limit-exceeded"
		title = "Rate Limit Exceeded"
		detail = message
	case 500, 502, 503:
		errorType = "https://api.twitter.com/2/problems/server-error"
		title = "Server Error"
		detail = message
	}
	
	// Real X API format: errors array contains error objects with detail/title/type
	// Top-level detail/title/type are also included
	return &ErrorResponse{
		Errors: []ErrorDetail{
			{
				Detail: detail,
				Title:  title,
				Type:   errorType,
				Code:   code,
			},
		},
		Title:  title,
		Detail: detail,
		Type:   errorType,
	}
}

// CreateValidationErrorResponse creates a validation error response matching X API format
// Matches real X API error format exactly
func CreateValidationErrorResponse(parameter string, value interface{}, message string) map[string]interface{} {
	var valueStr string
	if value != nil {
		if str, ok := value.(string); ok {
			valueStr = str
		} else {
			valueStr = fmt.Sprintf("%v", value)
		}
	}
	
	errorObj := map[string]interface{}{
		"parameter": parameter,
		"value":     valueStr,
		"detail":    message,
		"title":     "Invalid Request",
		"type":      "https://api.twitter.com/2/problems/invalid-request",
		// Also include parameters map for compatibility
		"parameters": map[string]interface{}{
			parameter: []string{valueStr},
		},
	}
	return map[string]interface{}{
		"errors": []map[string]interface{}{errorObj},
		"title":  "Invalid Request",
		"detail": "One or more parameters to your request was invalid.",
		"type":   "https://api.twitter.com/2/problems/invalid-request",
	}
}

// CreateMutuallyExclusiveErrorResponse creates an error response for mutually exclusive parameters
// Matches the real X API format for "You can only provide one of..." errors
// Real API format uses "parameters" map with arrays and "message" field (not "detail")
func CreateMutuallyExclusiveErrorResponse(parameters map[string]interface{}, message string) map[string]interface{} {
	// Format parameters as arrays (matching real API format)
	// The real API uses arrays directly in the parameters map
	paramsFormatted := make(map[string]interface{})
	for key, val := range parameters {
		if val == nil {
			paramsFormatted[key] = []string{}
			continue
		}
		
		// If val is already a []string or []interface{}, use it directly
		if strSlice, ok := val.([]string); ok {
			paramsFormatted[key] = strSlice
		} else if ifaceSlice, ok := val.([]interface{}); ok {
			// Convert []interface{} to []string
			strSlice := make([]string, 0, len(ifaceSlice))
			for _, item := range ifaceSlice {
				if str, ok := item.(string); ok {
					strSlice = append(strSlice, str)
				} else {
					strSlice = append(strSlice, fmt.Sprintf("%v", item))
				}
			}
			paramsFormatted[key] = strSlice
		} else if str, ok := val.(string); ok {
			// Single string becomes array with one element
			paramsFormatted[key] = []string{str}
		} else {
			// For complex objects (poll, media), format as a string representation
			// Real API shows: "{options: [...], duration_minutes: ...}" format
			if jsonBytes, err := json.Marshal(val); err == nil {
				paramsFormatted[key] = []string{string(jsonBytes)}
			} else {
				paramsFormatted[key] = []string{fmt.Sprintf("%v", val)}
			}
		}
	}
	
	errorObj := map[string]interface{}{
		"parameters": paramsFormatted,
		"message":    message, // Real API uses "message" not "detail" for this error type
	}
	
	return map[string]interface{}{
		"errors": []map[string]interface{}{errorObj},
		"title":  "Invalid Request",
		"detail": "One or more parameters to your request was invalid.",
		"type":   "https://api.twitter.com/2/problems/invalid-request",
	}
}

// FormatUser formats a user for API response
func FormatUser(user *User) map[string]interface{} {
	// Format date in UTC with Z suffix (matching real API format)
	// Use time.Time methods to ensure time package is recognized
	var _ time.Time // Ensure time package is used
	createdAtUTC := user.CreatedAt.UTC()
	createdAtStr := createdAtUTC.Format("2006-01-02T15:04:05.000Z")

	result := map[string]interface{}{
		"id":             user.ID,
		"name":           user.Name,
		"username":       user.Username,
		"created_at":     createdAtStr,
		"description":    user.Description,
		"public_metrics": user.PublicMetrics,
	}

	// Add optional fields
	// Note: Boolean fields (verified, protected) are always included if they exist
	// The field filtering will handle whether to include them based on user.fields parameter
	if user.Location != "" {
		result["location"] = user.Location
	}
	if user.URL != "" {
		result["url"] = user.URL
	}
	// Always include verified and protected (even if false) - field filtering will handle inclusion
	result["verified"] = user.Verified
	result["protected"] = user.Protected
	if user.ProfileImageURL != "" {
		result["profile_image_url"] = user.ProfileImageURL
	}
	if user.PinnedTweetID != "" {
		result["pinned_tweet_id"] = user.PinnedTweetID
	}
	if user.Entities != nil {
		result["entities"] = user.Entities
	}
	// Always include verified_type (with defaults) - X API includes this even if "none"
	if user.VerifiedType != "" {
		result["verified_type"] = user.VerifiedType
	} else if user.Verified {
		result["verified_type"] = "blue" // Default verified type for verified users
	} else {
		result["verified_type"] = "none"
	}
	// Only include withheld if present (X API omits null fields)
	if user.Withheld != nil {
		result["withheld"] = user.Withheld
	}

	return result
}

// FormatTweet formats a tweet for API response
func FormatTweet(tweet *Tweet) map[string]interface{} {
	// Format date in UTC with Z suffix (matching real API format)
	createdAtUTC := tweet.CreatedAt.UTC()
	createdAtStr := createdAtUTC.Format("2006-01-02T15:04:05.000Z")

	result := map[string]interface{}{
		"id":             tweet.ID,
		"text":           tweet.Text,
		"author_id":      tweet.AuthorID,
		"created_at":     createdAtStr,
		"public_metrics": tweet.PublicMetrics,
	}

	// Always include edit_history_tweet_ids (array with at least the tweet ID)
	// This matches real API behavior - even unedited tweets have this field
	if len(tweet.EditHistoryTweetIDs) > 0 {
		result["edit_history_tweet_ids"] = tweet.EditHistoryTweetIDs
	} else {
		// Default: include the tweet ID itself (unedited tweet)
		result["edit_history_tweet_ids"] = []string{tweet.ID}
	}

	// Add optional fields if present
	if tweet.ConversationID != "" {
		result["conversation_id"] = tweet.ConversationID
	}
	if tweet.InReplyToID != "" {
		result["in_reply_to_user_id"] = tweet.InReplyToID
	}
	if tweet.InReplyToTweetID != "" {
		result["in_reply_to_tweet_id"] = tweet.InReplyToTweetID
	}
	if len(tweet.ReferencedTweets) > 0 {
		result["referenced_tweets"] = tweet.ReferencedTweets
	}
	if tweet.Entities != nil {
		result["entities"] = tweet.Entities
	}
	if tweet.Attachments != nil {
		result["attachments"] = tweet.Attachments
	}
	if tweet.Source != "" {
		result["source"] = tweet.Source
	}
	if tweet.Lang != "" {
		result["lang"] = tweet.Lang
	}
	if tweet.PossiblySensitive {
		result["possibly_sensitive"] = tweet.PossiblySensitive
	}

	return result
}

// ToJSON converts a response to JSON bytes
func ToJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
