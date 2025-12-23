// Package playground parses and validates query parameters from API requests.
//
// This file handles parsing of field selection (user.fields, tweet.fields, etc.),
// expansions, pagination parameters (max_results, pagination_token), time-based
// filtering (since_id, until_id, start_time, end_time), and other query parameters.
// It validates parameters against OpenAPI specifications.
package playground

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// QueryParams represents parsed query parameters from a request
type QueryParams struct {
	UserFields     []string
	TweetFields    []string
	ListFields     []string
	SpaceFields    []string
	CommunityFields []string
	MediaFields    []string
	PollFields     []string
	PlaceFields    []string
	TrendFields    []string
	Expansions     []string
	
	// Pagination parameters
	MaxResults      int
	PaginationToken string
	
	// Time-based filtering parameters
	SinceID   string
	UntilID   string
	StartTime *time.Time // ISO 8601 datetime
	EndTime   *time.Time // ISO 8601 datetime
	
	// Sorting parameters
	SortOrder string // "recency" or "relevancy"
	
	// Other parameters
	Granularity string // For counts endpoints
}

// ParseQueryParams parses query parameters from an HTTP request
// If op, spec, and pathItem are provided, uses OpenAPI spec values for defaults and limits
func ParseQueryParams(r *http.Request, op *Operation, spec *OpenAPISpec, pathItem *PathItem) *QueryParams {
	params := &QueryParams{}
	query := r.URL.Query()

	// Parse field parameters
	if userFields := query.Get("user.fields"); userFields != "" {
		params.UserFields = strings.Split(userFields, ",")
	}
	if tweetFields := query.Get("tweet.fields"); tweetFields != "" {
		params.TweetFields = strings.Split(tweetFields, ",")
	}
	if listFields := query.Get("list.fields"); listFields != "" {
		params.ListFields = strings.Split(listFields, ",")
	}
	if spaceFields := query.Get("space.fields"); spaceFields != "" {
		params.SpaceFields = strings.Split(spaceFields, ",")
	}
	if communityFields := query.Get("community.fields"); communityFields != "" {
		params.CommunityFields = strings.Split(communityFields, ",")
	}
	if mediaFields := query.Get("media.fields"); mediaFields != "" {
		params.MediaFields = strings.Split(mediaFields, ",")
	}
	if pollFields := query.Get("poll.fields"); pollFields != "" {
		params.PollFields = strings.Split(pollFields, ",")
	}
	if placeFields := query.Get("place.fields"); placeFields != "" {
		params.PlaceFields = strings.Split(placeFields, ",")
	}
	if trendFields := query.Get("trend.fields"); trendFields != "" {
		params.TrendFields = strings.Split(trendFields, ",")
	}
	if personalizedTrendFields := query.Get("personalized_trends.fields"); personalizedTrendFields != "" {
		// personalized_trends.fields maps to trend fields (same object type)
		params.TrendFields = strings.Split(personalizedTrendFields, ",")
	}

	// Parse expansions
	if expansions := query.Get("expansions"); expansions != "" {
		params.Expansions = strings.Split(expansions, ",")
	}
	
	// Parse pagination parameters
	maxResultsStr := query.Get("max_results")
	if maxResultsStr != "" {
		if val, err := strconv.Atoi(maxResultsStr); err == nil {
			// Get limits from OpenAPI spec if available
			min, max, _, found := GetMaxResultsLimits(op, spec, pathItem)
			
			if found {
				// Use spec maximum if available
				if max > 0 && val > max {
					val = max
				}
				// Use spec minimum if available
				if min > 0 && val < min {
					val = min
				}
			} else {
				// Fallback to default limits
				if val > MaxPaginationResults {
					val = MaxPaginationResults
				}
				if val < 1 {
					val = 1
				}
			}
			params.MaxResults = val
		}
	} else {
		// If max_results not provided, use default from spec if available
		_, _, defaultValue, found := GetMaxResultsLimits(op, spec, pathItem)
		if found && defaultValue > 0 {
			params.MaxResults = defaultValue
		}
	}
	params.PaginationToken = query.Get("pagination_token")
	
	// Parse time-based filtering parameters
	params.SinceID = query.Get("since_id")
	params.UntilID = query.Get("until_id")
	
	if startTimeStr := query.Get("start_time"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			params.StartTime = &t
		}
	}
	if endTimeStr := query.Get("end_time"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			params.EndTime = &t
		}
	}
	
	// Parse sorting parameters
	params.SortOrder = query.Get("sort_order")
	
	// Parse other parameters
	params.Granularity = query.Get("granularity")

	return params
}

// GetRequestedFields returns the requested fields for a given field type
func (qp *QueryParams) GetRequestedFields(fieldType string) []string {
	switch fieldType {
	case "user":
		return qp.UserFields
	case "tweet":
		return qp.TweetFields
	case "list":
		return qp.ListFields
	case "space":
		return qp.SpaceFields
	case "media":
		return qp.MediaFields
	case "poll":
		return qp.PollFields
	case "place":
		return qp.PlaceFields
	case "trend":
		return qp.TrendFields
	case "community":
		return qp.CommunityFields
	default:
		return nil
	}
}

// ValidateFields validates that requested fields are valid against OpenAPI spec
// Returns validation errors if invalid fields are found, nil if valid
// pathItem can be nil - if provided, path-level parameters will also be checked
func (qp *QueryParams) ValidateFields(op *Operation, spec *OpenAPISpec, pathItem *PathItem) []*ValidationError {
	var errors []*ValidationError

	// Get response schema to extract available fields
	responseSchema := op.GetResponseSchema("200")
	if responseSchema == nil {
		responseSchema = op.GetResponseSchema("default")
	}
	if responseSchema == nil {
		// No schema to validate against - skip validation
		return nil
	}

	// Resolve $ref if present
	if ref, ok := responseSchema["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			responseSchema = resolved
		}
	}

	// Extract available fields from query parameter definitions (primary source)
	// This gets the enum values from tweet.fields, user.fields, etc. parameters
	// This is more reliable than extracting from response schema
	availableFields := make(map[string][]string)
	
	// Get query parameters from both operation and path level, resolving $ref references
	var queryParams []Parameter
	if pathItem != nil {
		queryParams = op.GetQueryParametersWithPathParams(pathItem, spec)
	} else {
		queryParams = op.GetQueryParametersWithSpec(spec)
	}
		for _, param := range queryParams {
		if param.Name == "tweet.fields" || param.Name == "user.fields" || param.Name == "list.fields" || 
		   param.Name == "space.fields" || param.Name == "community.fields" || 
		   param.Name == "media.fields" || param.Name == "poll.fields" || 
		   param.Name == "place.fields" {
			fieldType := strings.TrimSuffix(param.Name, ".fields")
			if param.Schema != nil {
				fields := extractEnumFromSchema(param.Schema, spec)
				if len(fields) > 0 {
					availableFields[fieldType] = fields
				}
			}
		}
	}
	
	// Fallback: try to extract from response schema if query params didn't have enum
	// This is less reliable but can help for endpoints where query params don't define enums
	if len(availableFields) == 0 && responseSchema != nil {
		schemaFields := extractAvailableFieldsFromSchema(responseSchema, spec)
		for k, v := range schemaFields {
			if _, exists := availableFields[k]; !exists && len(v) > 0 {
				availableFields[k] = v
			}
		}
	}

	// Validate user.fields
	if len(qp.UserFields) > 0 {
		userFields, ok := availableFields["user"]
		if !ok || len(userFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			userFields = []string{
				"created_at", "description", "entities", "id", "location", "name",
				"pinned_tweet_id", "profile_image_url", "protected", "public_metrics",
				"url", "username", "verified", "withheld",
			}
		}
		invalidFields := validateFieldList(qp.UserFields, userFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(userFields))
			copy(validFieldsList, userFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "user.fields",
				Message:   fmt.Sprintf("The `user.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate tweet.fields
	if len(qp.TweetFields) > 0 {
		tweetFields, ok := availableFields["tweet"]
		if !ok || len(tweetFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			tweetFields = []string{
				"article", "attachments", "author_id", "card_uri", "community_id", "context_annotations",
				"conversation_id", "created_at", "display_text_range", "edit_controls", "edit_history_tweet_ids",
				"entities", "geo", "id", "in_reply_to_user_id", "lang", "media_metadata", "non_public_metrics",
				"note_tweet", "organic_metrics", "possibly_sensitive", "promoted_metrics", "public_metrics",
				"referenced_tweets", "reply_settings", "scopes", "source", "suggested_source_links",
				"suggested_source_links_with_counts", "text", "withheld",
			}
		}
		invalidFields := validateFieldList(qp.TweetFields, tweetFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(tweetFields))
			copy(validFieldsList, tweetFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "tweet.fields",
				Message:   fmt.Sprintf("The `tweet.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate list.fields
	if len(qp.ListFields) > 0 {
		listFields, ok := availableFields["list"]
		if !ok || len(listFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			listFields = []string{
				"created_at", "description", "follower_count", "id", "member_count",
				"name", "owner_id", "private",
			}
		}
		invalidFields := validateFieldList(qp.ListFields, listFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(listFields))
			copy(validFieldsList, listFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "list.fields",
				Message:   fmt.Sprintf("The `list.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate space.fields
	if len(qp.SpaceFields) > 0 {
		spaceFields, ok := availableFields["space"]
		if !ok || len(spaceFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			spaceFields = []string{
				"created_at", "creator_id", "ended_at", "host_ids", "id",
				"invited_user_ids", "is_ticketed", "lang", "participant_count",
				"scheduled_start", "speaker_ids", "started_at", "state", "title",
				"topic_ids", "updated_at",
			}
		}
		invalidFields := validateFieldList(qp.SpaceFields, spaceFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(spaceFields))
			copy(validFieldsList, spaceFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "space.fields",
				Message:   fmt.Sprintf("The `space.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate community.fields
	if len(qp.CommunityFields) > 0 {
		communityFields, ok := availableFields["community"]
		if !ok || len(communityFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			communityFields = []string{
				"id", "name", "description", "created_at", "member_count", "access",
			}
		}
		invalidFields := validateFieldList(qp.CommunityFields, communityFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(communityFields))
			copy(validFieldsList, communityFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "community.fields",
				Message:   fmt.Sprintf("The `community.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate media.fields
	if len(qp.MediaFields) > 0 {
		mediaFields, ok := availableFields["media"]
		if !ok || len(mediaFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			mediaFields = []string{
				"duration_ms", "height", "media_key", "non_public_metrics",
				"organic_metrics", "preview_image_url", "promoted_metrics",
				"public_metrics", "type", "url", "width",
			}
		}
		invalidFields := validateFieldList(qp.MediaFields, mediaFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(mediaFields))
			copy(validFieldsList, mediaFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "media.fields",
				Message:   fmt.Sprintf("The `media.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate poll.fields
	if len(qp.PollFields) > 0 {
		pollFields, ok := availableFields["poll"]
		if !ok || len(pollFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			pollFields = []string{
				"duration_minutes", "end_datetime", "id", "options", "voting_status",
			}
		}
		invalidFields := validateFieldList(qp.PollFields, pollFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(pollFields))
			copy(validFieldsList, pollFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "poll.fields",
				Message:   fmt.Sprintf("The `poll.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	// Validate place.fields
	if len(qp.PlaceFields) > 0 {
		placeFields, ok := availableFields["place"]
		if !ok || len(placeFields) == 0 {
			// Fallback: use hardcoded list only if spec doesn't have it
			placeFields = []string{
				"contained_within", "country", "country_code", "full_name",
				"geo", "id", "name", "place_type",
			}
		}
		invalidFields := validateFieldList(qp.PlaceFields, placeFields)
		if len(invalidFields) > 0 {
			// Format valid fields list for error message (sorted for consistency)
			validFieldsList := make([]string, len(placeFields))
			copy(validFieldsList, placeFields)
			sort.Strings(validFieldsList)
			errors = append(errors, &ValidationError{
				Parameter: "place.fields",
				Message:   fmt.Sprintf("The `place.fields` query parameter value [%s] is not one of [%s]", invalidFields[0], strings.Join(validFieldsList, ",")),
				Value:     invalidFields[0],
			})
		}
	}

	return errors
}

// extractFieldsFromSchema extracts field names from a schema
func extractFieldsFromSchema(s map[string]interface{}, spec *OpenAPISpec) []string {
	var fields []string

	// Resolve $ref if present
	if ref, ok := s["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			s = resolved
		}
	}

	// Handle allOf
	if allOf, ok := s["allOf"].([]interface{}); ok {
		for _, item := range allOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				fields = append(fields, extractFieldsFromSchema(itemMap, spec)...)
			}
		}
		return fields
	}

	// Extract properties
	if properties, ok := s["properties"]; ok && properties != nil {
		if propertiesMap, ok := properties.(map[string]interface{}); ok {
			for fieldName := range propertiesMap {
				fields = append(fields, fieldName)
			}
		}
	}

	return fields
}

// ValidateExpansions validates that requested expansions are valid against OpenAPI spec
// Returns validation errors if invalid expansions are found, nil if valid
func (qp *QueryParams) ValidateExpansions(op *Operation, spec *OpenAPISpec, path string) []*ValidationError {
	var errors []*ValidationError

	if len(qp.Expansions) == 0 {
		return nil // No expansions requested
	}

	// Get available expansions from OpenAPI operation query parameters (primary source)
	availableExpansions := extractAvailableExpansions(op, spec)

	// Fallback: use endpoint-specific hardcoded list only if spec doesn't have it
	if len(availableExpansions) == 0 {
		availableExpansions = getDefaultExpansionsForEndpoint(path)
	}

	// If we still have no available expansions, we can't validate
	// Skip validation for unknown endpoints (this shouldn't happen for standard endpoints)
	if len(availableExpansions) == 0 {
		return nil
	}

	// Validate each requested expansion
	invalidExpansions := validateExpansionList(qp.Expansions, availableExpansions)
	if len(invalidExpansions) > 0 {
		// Format valid expansions list for error message (sorted for consistency)
		validExpansionsList := make([]string, len(availableExpansions))
		copy(validExpansionsList, availableExpansions)
		sort.Strings(validExpansionsList)
		errors = append(errors, &ValidationError{
			Parameter: "expansions",
			Message:   fmt.Sprintf("The `expansions` query parameter value [%s] is not one of [%s]", invalidExpansions[0], strings.Join(validExpansionsList, ",")),
			Value:     invalidExpansions[0],
		})
	}

	return errors
}

// getDefaultExpansionsForEndpoint returns default available expansions for known endpoints
// This is used as a fallback when the OpenAPI spec doesn't define expansions
func getDefaultExpansionsForEndpoint(path string) []string {
	// Remove query parameters and trailing slashes for matching
	pathWithoutQuery := path
	if idx := strings.Index(path, "?"); idx >= 0 {
		pathWithoutQuery = path[:idx]
	}
	pathWithoutQuery = strings.TrimSuffix(pathWithoutQuery, "/")

	// Common expansions for tweet endpoints
	if strings.HasPrefix(pathWithoutQuery, "/2/tweets") {
		return []string{
			"article.cover_media",
			"article.media_entities",
			"attachments.media_keys",
			"attachments.media_source_tweet",
			"attachments.poll_ids",
			"author_id",
			"edit_history_tweet_ids",
			"entities.mentions.username",
			"entities.note.mentions.username",
			"geo.place_id",
			"in_reply_to_user_id",
			"referenced_tweets.id",
			"referenced_tweets.id.attachments.media_keys",
			"referenced_tweets.id.author_id",
		}
	}

	// Common expansions for user endpoints
	if strings.HasPrefix(pathWithoutQuery, "/2/users") {
		return []string{
			"pinned_tweet_id",
		}
	}

	// Common expansions for list endpoints
	if strings.HasPrefix(pathWithoutQuery, "/2/lists") {
		return []string{
			"owner_id",
		}
	}

	// Common expansions for space endpoints
	if strings.HasPrefix(pathWithoutQuery, "/2/spaces") {
		return []string{
			"creator_id",
			"host_ids",
			"invited_user_ids",
			"speaker_ids",
			"topic_ids",
		}
	}

	return nil
}

// extractAvailableExpansions extracts available expansions from OpenAPI operation parameters
func extractAvailableExpansions(op *Operation, spec *OpenAPISpec) []string {
	// Get query parameters
	queryParams := op.GetQueryParameters()

	// Find the "expansions" parameter
	for _, param := range queryParams {
		if param.Name == "expansions" {
			if param.Schema != nil {
				expansions := extractEnumFromSchema(param.Schema, spec)
				if len(expansions) > 0 {
					return expansions
				}
			}
			break
		}
	}

	return nil
}

// extractEnumFromSchema extracts enum values from a schema, handling various schema structures
// This handles: direct enum, array items enum, $ref resolution, allOf, oneOf, anyOf
func extractEnumFromSchema(schema map[string]interface{}, spec *OpenAPISpec) []string {
	if schema == nil {
		return nil
	}

	// Resolve $ref if present
	if ref, ok := schema["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			schema = resolved
		} else {
			return nil
		}
	}

	// Handle allOf - check all schemas in allOf
	if allOf, ok := schema["allOf"].([]interface{}); ok {
		for _, item := range allOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if ref, ok := itemMap["$ref"].(string); ok {
					resolved := spec.ResolveRef(ref)
					if resolved != nil {
						itemMap = resolved
					}
				}
				if enum := extractEnumFromSchema(itemMap, spec); len(enum) > 0 {
					return enum
				}
			}
		}
	}

	// Handle oneOf - check first schema with enum
	if oneOf, ok := schema["oneOf"].([]interface{}); ok {
		for _, item := range oneOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if ref, ok := itemMap["$ref"].(string); ok {
					resolved := spec.ResolveRef(ref)
					if resolved != nil {
						itemMap = resolved
					}
				}
				if enum := extractEnumFromSchema(itemMap, spec); len(enum) > 0 {
					return enum
				}
			}
		}
	}

	// Handle anyOf - check first schema with enum
	if anyOf, ok := schema["anyOf"].([]interface{}); ok {
		for _, item := range anyOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if ref, ok := itemMap["$ref"].(string); ok {
					resolved := spec.ResolveRef(ref)
					if resolved != nil {
						itemMap = resolved
					}
				}
				if enum := extractEnumFromSchema(itemMap, spec); len(enum) > 0 {
					return enum
				}
			}
		}
	}

	// Check for direct enum (for string type parameters)
	if enum, ok := schema["enum"].([]interface{}); ok {
		values := make([]string, 0, len(enum))
		for _, e := range enum {
			if eStr, ok := e.(string); ok {
				values = append(values, eStr)
			}
		}
		if len(values) > 0 {
			return values
		}
	}

	// Check for array with items enum (most common for field/expansion parameters)
	if items, ok := schema["items"].(map[string]interface{}); ok {
		// Resolve $ref in items if present
		if ref, ok := items["$ref"].(string); ok {
			resolved := spec.ResolveRef(ref)
			if resolved != nil {
				items = resolved
			}
		}
		// Recursively check items schema
		if enum := extractEnumFromSchema(items, spec); len(enum) > 0 {
			return enum
		}
		// Direct enum in items
		if enum, ok := items["enum"].([]interface{}); ok {
			values := make([]string, 0, len(enum))
			for _, e := range enum {
				if eStr, ok := e.(string); ok {
					values = append(values, eStr)
				}
			}
			if len(values) > 0 {
				return values
			}
		}
	}

	return nil
}

// validateExpansionList checks if requested expansions are in the available expansions list
func validateExpansionList(requested []string, available []string) []string {
	availableMap := make(map[string]bool)
	for _, expansion := range available {
		availableMap[strings.TrimSpace(expansion)] = true
	}

	var invalidExpansions []string
	for _, expansion := range requested {
		expansion = strings.TrimSpace(expansion)
		// Handle expansion paths like "attachments.media_keys"
		expansionBase := expansion
		if idx := strings.Index(expansion, "."); idx > 0 {
			expansionBase = expansion[:idx]
		}
		if !availableMap[expansion] && !availableMap[expansionBase] {
			invalidExpansions = append(invalidExpansions, expansion)
		}
	}

	return invalidExpansions
}

// extractAvailableFieldsFromSchema extracts available fields for each object type from the response schema
func extractAvailableFieldsFromSchema(schema map[string]interface{}, spec *OpenAPISpec) map[string][]string {
	availableFields := make(map[string][]string)

	// Check if schema has a "properties" field and it's not nil
	properties, hasProperties := schema["properties"]
	if !hasProperties || properties == nil {
		return availableFields
	}
	
	propertiesMap, ok := properties.(map[string]interface{})
	if !ok {
		return availableFields
	}

	// Check if schema has a "data" property (common X API pattern)
	if dataSchema, ok := propertiesMap["data"]; ok && dataSchema != nil {
		if dataSchemaMap, ok := dataSchema.(map[string]interface{}); ok {
			// Check if data is an object or array
			if dataType, ok := dataSchemaMap["type"].(string); ok {
				if dataType == "object" {
					// Single object - extract fields
					fields := extractFieldsFromSchema(dataSchemaMap, spec)
					// Try to infer type from schema name or structure
					// For now, we'll check common patterns
					if len(fields) > 0 {
					// Check if it looks like a user, tweet, etc. based on common fields
					if containsString(fields, "username") || containsString(fields, "name") {
						availableFields["user"] = fields
					}
					if containsString(fields, "text") || containsString(fields, "author_id") {
						availableFields["tweet"] = fields
					}
					if containsString(fields, "name") && containsString(fields, "member_count") {
						availableFields["list"] = fields
					}
					}
				} else if dataType == "array" {
					// Array of objects - check items schema
					if items, ok := dataSchemaMap["items"].(map[string]interface{}); ok {
						fields := extractFieldsFromSchema(items, spec)
						if len(fields) > 0 {
							if containsString(fields, "username") || containsString(fields, "name") {
								availableFields["user"] = fields
							}
							if containsString(fields, "text") || containsString(fields, "author_id") {
								availableFields["tweet"] = fields
							}
							if containsString(fields, "name") && containsString(fields, "member_count") {
								availableFields["list"] = fields
							}
						}
					}
				}
			}
		}
	}

	// Also check includes (for expansions)
	if includesSchema, ok := propertiesMap["includes"]; ok && includesSchema != nil {
		if includesMap, ok := includesSchema.(map[string]interface{}); ok {
			if includesProps, ok := includesMap["properties"].(map[string]interface{}); ok {
				// Check for users, tweets, etc. in includes
				if usersSchema, ok := includesProps["users"]; ok {
					if usersMap, ok := usersSchema.(map[string]interface{}); ok {
						if items, ok := usersMap["items"].(map[string]interface{}); ok {
							fields := extractFieldsFromSchema(items, spec)
							if len(fields) > 0 {
								availableFields["user"] = fields
							}
						}
					}
				}
				if tweetsSchema, ok := includesProps["tweets"]; ok {
					if tweetsMap, ok := tweetsSchema.(map[string]interface{}); ok {
						if items, ok := tweetsMap["items"].(map[string]interface{}); ok {
							fields := extractFieldsFromSchema(items, spec)
							if len(fields) > 0 {
								availableFields["tweet"] = fields
							}
						}
					}
				}
			}
		}
	}

	return availableFields
}

// validateFieldList checks if requested fields are in the available fields list
func validateFieldList(requested []string, available []string) []string {
	availableMap := make(map[string]bool)
	for _, field := range available {
		availableMap[strings.TrimSpace(field)] = true
	}

	var invalidFields []string
	for _, field := range requested {
		field = strings.TrimSpace(field)
		if !availableMap[field] {
			invalidFields = append(invalidFields, field)
		}
	}

	return invalidFields
}

// containsString checks if a string slice contains a value
func containsString(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// GetMaxResultsLimits extracts min, max, and default values for max_results from OpenAPI spec
// Returns (min, max, default, found) where found indicates if the parameter exists in spec
// Also checks path-level parameters if pathItem is provided
func GetMaxResultsLimits(op *Operation, spec *OpenAPISpec, pathItem *PathItem) (min, max, defaultValue int, found bool) {
	if op == nil || spec == nil {
		return 0, 0, 0, false
	}

	// Get all query parameters for this operation (including path-level ones)
	var queryParams []Parameter
	if pathItem != nil {
		queryParams = op.GetQueryParametersWithPathParams(pathItem, spec)
	} else {
		queryParams = op.GetQueryParametersWithSpec(spec)
	}
	
	// Find max_results parameter
	for _, param := range queryParams {
		if param.Name == "max_results" {
			found = true
			
			// Get schema (resolve $ref if needed)
			schema := param.Schema
			if schema == nil {
				return 0, 0, 0, false
			}
			
			// Resolve $ref if present
			if ref, ok := schema["$ref"].(string); ok {
				resolved := spec.ResolveRef(ref)
				if resolved != nil {
					schema = resolved
				}
			}
			
			// Extract minimum
			if minVal, ok := schema["minimum"].(float64); ok {
				min = int(minVal)
			}
			
			// Extract maximum
			if maxVal, ok := schema["maximum"].(float64); ok {
				max = int(maxVal)
			}
			
			// Extract default
			if defVal, ok := schema["default"].(float64); ok {
				defaultValue = int(defVal)
			}
			
			return min, max, defaultValue, true
		}
	}
	
	return 0, 0, 0, false
}

// ValidateMaxResults validates the max_results query parameter
// If op, spec, and pathItem are provided, uses limits from OpenAPI spec; otherwise uses fallback defaults
// Returns error if invalid, nil if valid
func ValidateMaxResults(r *http.Request, op *Operation, spec *OpenAPISpec, pathItem *PathItem) error {
	maxResultsStr := r.URL.Query().Get("max_results")
	if maxResultsStr == "" {
		return nil // Optional parameter, no validation needed if not present
	}

	maxResults, err := strconv.Atoi(maxResultsStr)
	if err != nil {
		return fmt.Errorf("max_results must be an integer")
	}

	// Try to get limits from OpenAPI spec
	min, max, _, found := GetMaxResultsLimits(op, spec, pathItem)
	
	if found {
		// Use spec values
		if min > 0 && maxResults < min {
			return fmt.Errorf("max_results must be at least %d", min)
		}
		if max > 0 && maxResults > max {
			return fmt.Errorf("max_results must be at most %d", max)
		}
	} else {
		// Fallback to default limits if not in spec
		if maxResults < 5 {
			return fmt.Errorf("max_results must be at least 5")
		}
		if maxResults > MaxPaginationResults {
			return fmt.Errorf("max_results must be at most %d", MaxPaginationResults)
		}
	}

	return nil
}

// ValidatePaginationToken validates the pagination_token format
// Returns error if invalid format, nil if valid (or empty)
func ValidatePaginationToken(token string) error {
	if token == "" {
		return nil // Optional parameter
	}

	// Basic validation - should be base64-like (alphanumeric, +, /, =)
	// We'll do a simple check for reasonable length and characters
	if len(token) < 4 {
		return fmt.Errorf("pagination_token must be at least 4 characters")
	}

	if len(token) > 500 {
		return fmt.Errorf("pagination_token must be at most 500 characters")
	}

	return nil
}
