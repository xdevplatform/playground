// Package playground filters API responses based on query parameters.
//
// This file implements field filtering for responses, removing fields that
// weren't requested via query parameters (e.g., user.fields, tweet.fields).
// It ensures responses match what clients request, reducing payload size
// and matching real X API behavior.
package playground

import (
	"encoding/json"
	"log"
	"strings"
)

// filterResponseByQueryParams filters a response based on query parameters
// When no query params are provided, applies default field filtering
func filterResponseByQueryParams(response map[string]interface{}, queryParams *QueryParams, op *EndpointOperation) map[string]interface{} {
	// Deep copy the response to avoid modifying the original
	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error marshaling response for filtering: %v", err)
		return response // Return original if marshaling fails
	}
	var filtered map[string]interface{}
	if err := json.Unmarshal(responseJSON, &filtered); err != nil {
		log.Printf("Error unmarshaling response for filtering: %v", err)
		return response // Return original if unmarshaling fails
	}

	// Filter based on field selection parameters
	if data, ok := filtered["data"].(map[string]interface{}); ok {
		filtered["data"] = filterObjectFields(data, queryParams, op)
	} else if dataArray, ok := filtered["data"].([]interface{}); ok {
		// Handle array of objects
		filteredArray := make([]interface{}, 0, len(dataArray))
		for _, item := range dataArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				filteredArray = append(filteredArray, filterObjectFields(itemMap, queryParams, op))
			} else {
				filteredArray = append(filteredArray, item)
			}
		}
		filtered["data"] = filteredArray
	}

	// Handle expansions - remove includes if no expansions requested
	if queryParams == nil || len(queryParams.Expansions) == 0 {
		// Remove includes when no expansions are requested
		delete(filtered, "includes")
	} else if includes, ok := filtered["includes"].(map[string]interface{}); ok {
		filtered["includes"] = filterExpansions(includes, queryParams)
	}

	return filtered
}

// filterObjectFields filters fields from an object based on query parameters
func filterObjectFields(obj map[string]interface{}, queryParams *QueryParams, op *EndpointOperation) map[string]interface{} {
	// Determine field type from operation or object structure
	fieldType := inferFieldType(obj, op)
	
	// Get default fields for this type (always included)
	defaultFields := getDefaultFields(fieldType)
	
	// Get requested fields for this type
	var requestedFields []string
	if queryParams != nil {
		requestedFields = queryParams.GetRequestedFields(fieldType)
	}
	
	// Create a map of requested field names for quick lookup
	// Always include default fields, then add requested fields
	requestedMap := make(map[string]bool)
	for _, field := range defaultFields {
		requestedMap[strings.TrimSpace(field)] = true
	}
	for _, field := range requestedFields {
		requestedMap[strings.TrimSpace(field)] = true
	}

	// Filter the object
	filtered := make(map[string]interface{})
	
	// Always preserve edit_history_tweet_ids for tweets (X API always includes this field)
	var editHistoryTweetIDs interface{}
	hasEditHistory := false
	if fieldType == "tweet" {
		if value, ok := obj["edit_history_tweet_ids"]; ok {
			editHistoryTweetIDs = value
			hasEditHistory = true
		}
	}
	
	for key, value := range obj {
		if key == "edit_history_tweet_ids" {
			// Skip for now, will add at end if needed
			continue
		}
		if requestedMap[key] {
			filtered[key] = value
		}
	}
	
	// Always include edit_history_tweet_ids for tweets if it exists
	if fieldType == "tweet" && hasEditHistory {
		filtered["edit_history_tweet_ids"] = editHistoryTweetIDs
	}
	
	// If no fields matched and we have default fields, return empty object
	// Otherwise return filtered result
	if len(filtered) == 0 && len(requestedFields) > 0 {
		// This shouldn't happen, but return empty object if it does
		return make(map[string]interface{})
	}

	return filtered
}

// filterExpansions filters expansion objects based on query parameters
func filterExpansions(includes map[string]interface{}, queryParams *QueryParams) map[string]interface{} {
	if len(queryParams.Expansions) == 0 {
		return includes
	}

	expansionMap := make(map[string]bool)
	for _, exp := range queryParams.Expansions {
		expansionMap[strings.TrimSpace(exp)] = true
	}

	filtered := make(map[string]interface{})
	for key, value := range includes {
		if expansionMap[key] {
			filtered[key] = value
		}
	}

	return filtered
}

// getDefaultFields returns the default fields for a given field type
// These are the fields returned by the X API when no field parameters are specified
func getDefaultFields(fieldType string) []string {
	switch fieldType {
	case "user":
		return []string{"id", "name", "username"} // Matching X API default
	case "tweet":
		return []string{"id", "text", "edit_history_tweet_ids"} // Matching X API default
	case "list":
		return []string{"id", "name"} // Matching X API default
	case "space":
		return []string{"id", "state"} // Matching X API default
	case "media":
		return []string{"media_key", "type"}
	case "poll":
		return []string{"id", "options"} // options includes position, label, votes
	case "place":
		return []string{"id", "full_name"} // Matching X API default
	case "trend":
		// Default fields depend on endpoint type
		// For /2/trends/by/woeid/{woeid}: trend_name, tweet_count
		// For /2/users/personalized_trends: trend_name, category, post_count, trending_since
		// Return all common fields - filtering will handle endpoint-specific ones
		return []string{"trend_name", "tweet_count", "category", "post_count", "trending_since"}
	default:
		// For unknown types, return empty to include all fields (fallback)
		return nil
	}
}

// inferFieldType tries to infer the field type from an object or operation
func inferFieldType(obj map[string]interface{}, op *EndpointOperation) string {
	// Check for common type indicators in the object
	if _, hasTweetID := obj["id"]; hasTweetID {
		if _, hasText := obj["text"]; hasText {
			return "tweet"
		}
		if _, hasName := obj["name"]; hasName {
			if _, hasMemberCount := obj["member_count"]; hasMemberCount {
				return "list"
			}
			if _, hasUsername := obj["username"]; hasUsername {
				return "user"
			}
		}
	}

	// Try to infer from operation ID or path
	if op != nil && op.Operation != nil {
		opID := strings.ToLower(op.Operation.OperationID)
		if strings.Contains(opID, "user") {
			return "user"
		}
		if strings.Contains(opID, "tweet") {
			return "tweet"
		}
		if strings.Contains(opID, "list") {
			return "list"
		}
		if strings.Contains(opID, "trend") {
			return "trend"
		}
	}
	
	// Check for trend-specific fields
	if _, hasTrendName := obj["trend_name"]; hasTrendName {
		if _, hasTweetCount := obj["tweet_count"]; hasTweetCount {
			return "trend"
		}
		// Check for personalized_trends fields (has trend_name but different structure)
		if _, hasCategory := obj["category"]; hasCategory {
			if _, hasPostCount := obj["post_count"]; hasPostCount {
				return "trend" // Use same field type for filtering
			}
		}
	}

	return ""
}

