// Package playground generates mock responses from OpenAPI schemas.
//
// This file provides fallback response generation when example responses
// are not available. It traverses OpenAPI schemas and generates realistic
// mock data based on schema types, constraints, and examples defined in
// the OpenAPI specification.
package playground

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"
)


func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateMockResponse generates a mock response from an OpenAPI schema
func GenerateMockResponse(schema map[string]interface{}, spec *OpenAPISpec) interface{} {
	return GenerateMockResponseWithState(schema, spec, nil)
}

// GenerateMockResponseWithState generates a mock response from an OpenAPI schema, using state when available
func GenerateMockResponseWithState(schema map[string]interface{}, spec *OpenAPISpec, state *State) interface{} {
	if schema == nil {
		return map[string]interface{}{}
	}

	return generateValueWithState(schema, spec, "", state)
}

// generateValue generates a value based on a schema (without property context)
func generateValue(schema map[string]interface{}, spec *OpenAPISpec) interface{} {
	return generateValueWithState(schema, spec, "", nil)
}

// generateValueWithState generates a value based on a schema with state support
func generateValueWithState(schema map[string]interface{}, spec *OpenAPISpec, propertyName string, state *State) interface{} {
	return generateValueWithContext(schema, spec, propertyName, state)
}

// generateValueWithContext generates a value based on a schema with property name context
func generateValueWithContext(schema map[string]interface{}, spec *OpenAPISpec, propertyName string, state *State) interface{} {
	// Handle $ref - resolve the reference FIRST
	if ref, ok := schema["$ref"].(string); ok {
		if spec != nil {
			resolved := spec.ResolveRef(ref)
			if resolved == nil {
				if SchemaDebug {
					log.Printf("DEBUG: Failed to resolve $ref: %s", ref)
				}
			} else {
				if SchemaDebug && strings.Contains(ref, "ListCreateResponse") {
					schemaJSON, err := json.MarshalIndent(resolved, "", "  ")
					if err != nil {
						log.Printf("DEBUG: [ListCreateResponse] Error marshaling resolved schema for %s: %v", ref, err)
					} else {
						log.Printf("DEBUG: [ListCreateResponse] Resolved schema for %s:\n%s", ref, string(schemaJSON))
					}
					
					// Check what properties exist
					if props, ok := resolved["properties"].(map[string]interface{}); ok {
						log.Printf("DEBUG: [ListCreateResponse] Has %d properties: %v", len(props), getKeys(props))
					}
					if allOf, ok := resolved["allOf"].([]interface{}); ok {
						log.Printf("DEBUG: [ListCreateResponse] Has allOf with %d items", len(allOf))
						for i, item := range allOf {
							if itemMap, ok := item.(map[string]interface{}); ok {
								if props, ok := itemMap["properties"].(map[string]interface{}); ok {
									log.Printf("DEBUG: [ListCreateResponse] allOf[%d] has %d properties: %v", i, len(props), getKeys(props))
								}
							}
						}
					}
				}
				
				// Check if resolved schema has allOf - handle it
				if allOf, ok := resolved["allOf"].([]interface{}); ok && len(allOf) > 0 {
					// Merge all schemas in allOf
					merged := make(map[string]interface{})
					for _, item := range allOf {
						if itemMap, ok := item.(map[string]interface{}); ok {
							generated := generateValueWithState(itemMap, spec, "", state)
							if genMap, ok := generated.(map[string]interface{}); ok {
								for k, v := range genMap {
									merged[k] = v
								}
							}
						}
					}
					return merged
				}
				
				// Check if resolved schema has properties directly
				if properties, ok := resolved["properties"].(map[string]interface{}); ok && len(properties) > 0 {
					// Has properties - generate object from it
					// Make sure we use the full resolved schema (not just properties)
					return generateObject(resolved, spec)
				}
				
				// Check if resolved schema might have properties nested in allOf
				// (we already handled allOf above, but check again after resolution)
				if allOf, ok := resolved["allOf"].([]interface{}); ok && len(allOf) > 0 {
					// Extract properties from allOf items
					allProperties := make(map[string]interface{})
					for _, item := range allOf {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if props, ok := itemMap["properties"].(map[string]interface{}); ok {
								for k, v := range props {
									allProperties[k] = v
								}
							}
						}
					}
					if len(allProperties) > 0 {
						// Create a new schema with merged properties
						mergedSchema := make(map[string]interface{})
						for k, v := range resolved {
							mergedSchema[k] = v
						}
						mergedSchema["properties"] = allProperties
						return generateObject(mergedSchema, spec)
					}
				}
				
				// Check if resolved schema has a type
				if _, ok := resolved["type"].(string); ok {
					// Has explicit type - use it
					return generateValueWithState(resolved, spec, propertyName, state)
				}
				
				// No type, no properties - might be an empty schema or reference to another schema
				// Try to generate as object anyway
				return generateObjectWithState(resolved, spec, propertyName, state)
			}
		}
		// If we can't resolve, try to generate a reasonable object
		return map[string]interface{}{
			"id": generateID(),
			"created_at": time.Now().Format(time.RFC3339),
		}
	}

	// Handle allOf - merge all schemas
	if allOf, ok := schema["allOf"].([]interface{}); ok && len(allOf) > 0 {
		merged := make(map[string]interface{})
		for _, item := range allOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				generated := generateValueWithState(itemMap, spec, propertyName, state)
				if genMap, ok := generated.(map[string]interface{}); ok {
					for k, v := range genMap {
						merged[k] = v
					}
				}
			}
		}
		return merged
	}

	// Handle oneOf - use first schema
	if oneOf, ok := schema["oneOf"].([]interface{}); ok && len(oneOf) > 0 {
		if first, ok := oneOf[0].(map[string]interface{}); ok {
			return generateValueWithState(first, spec, propertyName, state)
		}
	}

	// Handle anyOf - use first schema
	if anyOf, ok := schema["anyOf"].([]interface{}); ok && len(anyOf) > 0 {
		if first, ok := anyOf[0].(map[string]interface{}); ok {
			return generateValueWithState(first, spec, propertyName, state)
		}
	}

	// Handle type
	schemaType, hasType := schema["type"].(string)
	
	// If no explicit type, try to infer from structure
	if !hasType {
		if properties, ok := schema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
			schemaType = "object"
		} else if _, hasItems := schema["items"]; hasItems {
			schemaType = "array"
		} else if len(schema) > 0 {
			// If schema has content but no explicit type, default to object
			// (many OpenAPI schemas don't have explicit type if they have properties)
			schemaType = "object"
		}
	}

	switch schemaType {
	case "object":
		return generateObjectWithState(schema, spec, propertyName, state)
	case "array":
		return generateArrayWithState(schema, spec, propertyName, state)
	case "string":
		return generateString(schema, propertyName)
	case "integer", "number":
		return generateNumber(schema, propertyName)
	case "boolean":
		return generateBoolean()
	case "null":
		return nil
	default:
		// If no type, check for oneOf, anyOf, allOf
		if oneOf, ok := schema["oneOf"].([]interface{}); ok && len(oneOf) > 0 {
			if first, ok := oneOf[0].(map[string]interface{}); ok {
				return generateValueWithState(first, spec, propertyName, state)
			}
		}
		// If schema has properties or any structure, try to generate as object
		if properties, ok := schema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
			return generateObjectWithState(schema, spec, propertyName, state)
		}
		// Last resort: return empty object
		return map[string]interface{}{}
	}
}

// generateObject generates an object from a schema (backward compatibility)
func generateObject(schema map[string]interface{}, spec *OpenAPISpec) map[string]interface{} {
	return generateObjectWithState(schema, spec, "", nil)
}

// generateObjectWithState generates an object from a schema with state support
func generateObjectWithState(schema map[string]interface{}, spec *OpenAPISpec, propertyName string, state *State) map[string]interface{} {
	// Check if we can use real data from state
	if state != nil {
		// Try to detect if this is a User, Tweet, or other entity type
		if userData := tryGetUserFromState(schema, state); userData != nil {
			return userData
		}
		if tweetData := tryGetTweetFromState(schema, state); tweetData != nil {
			return tweetData
		}
		if listData := tryGetListFromState(schema, state); listData != nil {
			return listData
		}
	}

	result := make(map[string]interface{})

	// Handle properties
	properties, hasProperties := schema["properties"].(map[string]interface{})
	
	if hasProperties && len(properties) > 0 {
		// Normal properties handling
		for key, prop := range properties {
			var propMap map[string]interface{}
			var ok bool
			
			// Property might be a direct map or need conversion
			if propMap, ok = prop.(map[string]interface{}); !ok {
				// Try to convert if it's not already a map
				if SchemaDebug && strings.Contains(key, "data") {
					log.Printf("DEBUG: [generateObject] Property '%s' is not a map, skipping", key)
				}
				continue
			}
			
			// Debug logging for data property
			if SchemaDebug && key == "data" {
				propJSON, err := json.MarshalIndent(propMap, "", "  ")
				if err != nil {
					log.Printf("DEBUG: [generateObject] Error marshaling property 'data': %v", err)
				} else {
					log.Printf("DEBUG: [generateObject] Generating value for property 'data':\n%s", string(propJSON))
				}
			}
			
			// Generate value for this property (pass property name for context)
			// This will resolve any $ref in the property schema
			// Use state to get real data when possible (e.g., user IDs)
			generatedValue := generateValueWithState(propMap, spec, key, state)
			
			// If this is an ID field and we have state, try to use a real ID
			if state != nil && (strings.Contains(strings.ToLower(key), "id") || strings.Contains(strings.ToLower(key), "_id")) {
				if idStr, ok := generatedValue.(string); ok && idStr != "" {
					// Check if this ID exists in state - if not, use a real one
					if !isRealIDInState(idStr, key, state) {
						realID := getRealIDFromState(key, state)
						if realID != "" {
							generatedValue = realID
						}
					}
				}
			}
			
			if SchemaDebug && key == "data" {
				if generatedValue == nil {
					log.Printf("DEBUG: [generateObject] Generated value for 'data' is nil")
				} else {
					valJSON, err := json.MarshalIndent(generatedValue, "", "  ")
					if err != nil {
						log.Printf("DEBUG: [generateObject] Error marshaling generated value for 'data': %v", err)
					} else {
						log.Printf("DEBUG: [generateObject] Generated value for 'data':\n%s", string(valJSON))
					}
				}
			}
			
			// Always add the generated value (even if empty, it might be valid)
			// The generateValue function should return appropriate defaults
			if generatedValue != nil {
				result[key] = generatedValue
			} else {
				// If nil, generate a default based on property name
				result[key] = generateDefaultForProperty(key)
			}
		}
		
		// If we didn't generate any properties, add some defaults
		if len(result) == 0 {
			if SchemaDebug {
				log.Printf("DEBUG: [generateObject] No properties generated, adding defaults")
			}
			result["id"] = generateID()
			result["created_at"] = time.Now().Format(time.RFC3339)
		}
	} else {
		// No explicit properties - generate common fields based on context
		// This is a fallback for schemas without explicit properties
		if SchemaDebug {
			log.Printf("DEBUG: [generateObject] No properties in schema, adding defaults")
		}
		result["id"] = generateID()
		result["created_at"] = time.Now().Format(time.RFC3339)
	}

	// Handle required fields
	if required, ok := schema["required"].([]interface{}); ok {
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				if _, exists := result[reqStr]; !exists {
					// Generate a default value for required fields
					result[reqStr] = generateDefaultValue()
				}
			}
		}
	}

	// Don't use hasPattern check - it causes issues with nested data properties
	// The schema should be generated as-is, with data as a normal property

	return result
}

// generateArray generates an array from a schema (backward compatibility)
func generateArray(schema map[string]interface{}, spec *OpenAPISpec) []interface{} {
	return generateArrayWithState(schema, spec, "", nil)
}

// generateArrayWithState generates an array from a schema with state support
func generateArrayWithState(schema map[string]interface{}, spec *OpenAPISpec, propertyName string, state *State) []interface{} {
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		return []interface{}{}
	}

	// Check for minItems - if specified, generate at least that many
	minItems := 0
	if min, ok := schema["minItems"].(float64); ok {
		minItems = int(min)
	}
	
	// Generate items (at least minItems, up to 3)
	count := minItems
	if count == 0 {
		count = 1 // Default to at least 1 item
	}
	maxCount := count + 2 // Allow up to minItems + 2
	if maxCount > 3 {
		maxCount = 3
	}
	if count < maxCount {
		count = rand.Intn(maxCount-count+1) + count
	}
	
	result := make([]interface{}, count)
	for i := 0; i < count; i++ {
		result[i] = generateValueWithState(items, spec, "", state)
	}

	return result
}

// generateString generates a string from a schema with property name context
func generateString(schema map[string]interface{}, propertyName string) string {
	// Check for enum
	if enum, ok := schema["enum"].([]interface{}); ok && len(enum) > 0 {
		if str, ok := enum[rand.Intn(len(enum))].(string); ok {
			return str
		}
	}

	// Check for format
	format, _ := schema["format"].(string)
	switch format {
	case "date-time":
		return time.Now().Format(time.RFC3339)
	case "date":
		return time.Now().Format("2006-01-02")
	case "uri":
		return "https://example.com/resource"
	case "email":
		return "user@example.com"
	case "uuid":
		return generateUUID()
	default:
		// Check for example
		if example, ok := schema["example"].(string); ok {
			return example
		}
		// Generate based on property name patterns
		if propertyName != "" {
			return generateStringFromName(propertyName)
		}
		// Fallback: try to get name from schema
		if name, ok := schema["name"].(string); ok {
			return generateStringFromName(name)
		}
		return "mock_string_value"
	}
}

// generateNumber generates a number from a schema with property name context
func generateNumber(schema map[string]interface{}, propertyName string) interface{} {
	schemaType, _ := schema["type"].(string)
	
	// Use property name to generate more realistic numbers
	if propertyName != "" {
		nameLower := strings.ToLower(propertyName)
		if strings.Contains(nameLower, "count") || strings.Contains(nameLower, "total") {
			if schemaType == "integer" {
				return int64(rand.Intn(1000) + 1)
			}
			return rand.Float64()*1000 + 1
		}
		if strings.Contains(nameLower, "status") || strings.Contains(nameLower, "code") {
			// HTTP status codes
			statusCodes := []int64{200, 201, 400, 401, 403, 404, 429, 500, 503}
			return statusCodes[rand.Intn(len(statusCodes))]
		}
		if strings.Contains(nameLower, "percent") || strings.Contains(nameLower, "progress") {
			return rand.Float64() * 100
		}
	}
	
	if minimum, ok := schema["minimum"].(float64); ok {
		maximum, _ := schema["maximum"].(float64)
		if maximum == 0 {
			maximum = minimum + 100
		}
		value := minimum + rand.Float64()*(maximum-minimum)
		if schemaType == "integer" {
			return int64(value)
		}
		return value
	}

	if schemaType == "integer" {
		return rand.Int63n(1000)
	}
	return rand.Float64() * 1000
}

// generateBoolean generates a boolean value
func generateBoolean() bool {
	return rand.Intn(2) == 1
}

// generateDefaultValue generates a default value
func generateDefaultValue() interface{} {
	types := []string{"string", "integer", "boolean"}
	selected := types[rand.Intn(len(types))]
	
	switch selected {
	case "string":
		return "default_value"
	case "integer":
		return rand.Int63n(100)
	case "boolean":
		return false
	default:
		return nil
	}
}

// generateDefaultForProperty generates a default value based on property name
func generateDefaultForProperty(name string) interface{} {
	nameLower := strings.ToLower(name)
	
	switch {
	case strings.Contains(nameLower, "id"):
		return generateID()
	case strings.Contains(nameLower, "name"):
		return "Mock " + strings.Title(name)
	case strings.Contains(nameLower, "created_at"), strings.Contains(nameLower, "updated_at"):
		return time.Now().Format(time.RFC3339)
	case strings.Contains(nameLower, "description"):
		return "Mock description"
	case strings.Contains(nameLower, "count"), strings.Contains(nameLower, "total"):
		return rand.Intn(100)
	case strings.Contains(nameLower, "url"):
		return "https://example.com/resource"
	default:
		return "mock_value"
	}
}

// generateStringFromName generates a string based on property name patterns
// This creates more realistic and distinct values for different field types
func generateStringFromName(name string) string {
	nameLower := strings.ToLower(name)
	
	// Error/problem fields
	if strings.Contains(nameLower, "type") && (strings.Contains(nameLower, "error") || strings.Contains(nameLower, "problem")) {
		errorTypes := []string{
			"https://api.twitter.com/2/problems/invalid-request",
			"https://api.twitter.com/2/problems/resource-not-found",
			"https://api.twitter.com/2/problems/unauthorized",
			"about:blank",
		}
		return errorTypes[rand.Intn(len(errorTypes))]
	}
	if strings.Contains(nameLower, "title") && (strings.Contains(nameLower, "error") || strings.Contains(nameLower, "problem")) {
		titles := []string{
			"Invalid Request",
			"Resource Not Found",
			"Unauthorized",
			"Bad Request",
		}
		return titles[rand.Intn(len(titles))]
	}
	if strings.Contains(nameLower, "detail") {
		details := []string{
			"The request is invalid",
			"The specified resource was not found",
			"Authentication credentials were missing or incorrect",
			"Rate limit exceeded",
		}
		return details[rand.Intn(len(details))]
	}
	
	// ID fields - try to use real IDs from state when possible
	if strings.Contains(nameLower, "id") || strings.Contains(nameLower, "_id") {
		// If we have state, try to get a real ID based on context
		// This will be handled by the caller if state is available
		return generateID()
	}
	
	// URL/URI fields
	if strings.Contains(nameLower, "url") || strings.Contains(nameLower, "uri") {
		urls := []string{
			"https://example.com/resource",
			"https://pbs.twimg.com/profile_images/example.jpg",
			"https://abs.twimg.com/icons/apple-touch-icon.png",
		}
		return urls[rand.Intn(len(urls))]
	}
	
	// Username
	if strings.Contains(nameLower, "username") {
		usernames := []string{"playground_user", "example_user", "test_user", "demo_user"}
		return usernames[rand.Intn(len(usernames))]
	}
	
	// Name fields
	if strings.Contains(nameLower, "name") {
		names := []string{
			"Playground User",
			"Example Account",
			"Test User",
			"Demo Account",
			"Sample List",
			"My Playground List",
		}
		return names[rand.Intn(len(names))]
	}
	
	// Text/content fields
	if strings.Contains(nameLower, "text") {
		texts := []string{
			"Just setting up my playground! This is a sample tweet.",
			"Testing the xurl playground with a realistic tweet example.",
			"Hello from the playground! 🎮",
			"This is an example tweet generated by the playground.",
		}
		return texts[rand.Intn(len(texts))]
	}
	
	// Description
	if strings.Contains(nameLower, "description") {
		descriptions := []string{
			"A sample description for the playground",
			"Example profile description",
			"Mock description for testing",
			"Playground test description",
		}
		return descriptions[rand.Intn(len(descriptions))]
	}
	
	// Location
	if strings.Contains(nameLower, "location") {
		locations := []string{
			"San Francisco, CA",
			"New York, NY",
			"London, UK",
			"Tokyo, Japan",
		}
		return locations[rand.Intn(len(locations))]
	}
	
	// Language
	if strings.Contains(nameLower, "lang") {
		languages := []string{"en", "es", "fr", "ja", "de", "pt"}
		return languages[rand.Intn(len(languages))]
	}
	
	// Source
	if strings.Contains(nameLower, "source") {
		sources := []string{
			"Twitter Web App",
			"Twitter for iPhone",
			"Twitter for Android",
			"xurl playground",
		}
		return sources[rand.Intn(len(sources))]
	}
	
	// State/status
	if strings.Contains(nameLower, "state") {
		states := []string{"active", "inactive", "pending", "processing", "succeeded", "failed"}
		return states[rand.Intn(len(states))]
	}
	
	// Type fields
	if strings.Contains(nameLower, "type") {
		// Media type
		if strings.Contains(nameLower, "media") {
			mediaTypes := []string{"photo", "video", "animated_gif"}
			return mediaTypes[rand.Intn(len(mediaTypes))]
		}
		// Generic type
		types := []string{"user", "tweet", "list", "media"}
		return types[rand.Intn(len(types))]
	}
	
	// Category
	if strings.Contains(nameLower, "category") {
		categories := []string{"tweet", "tweet_image", "tweet_video", "amplify_video"}
		return categories[rand.Intn(len(categories))]
	}
	
	// Format
	if strings.Contains(nameLower, "format") {
		formats := []string{"json", "xml", "csv"}
		return formats[rand.Intn(len(formats))]
	}
	
	// Key
	if strings.Contains(nameLower, "key") {
		return generateID() // Use ID generator for keys
	}
	
	// Token
	if strings.Contains(nameLower, "token") {
		return "mock_token_" + generateID()
	}
	
	// Secret
	if strings.Contains(nameLower, "secret") {
		return "mock_secret_" + generateID()
	}
	
	// Email
	if strings.Contains(nameLower, "email") {
		return "user@example.com"
	}
	
	// Phone
	if strings.Contains(nameLower, "phone") {
		return "+1234567890"
	}
	
	// Timestamp fields
	if strings.Contains(nameLower, "created_at") || strings.Contains(nameLower, "updated_at") ||
		strings.Contains(nameLower, "started_at") || strings.Contains(nameLower, "ended_at") ||
		strings.Contains(nameLower, "scheduled_start") {
		return time.Now().Format(time.RFC3339)
	}
	
	// Title
	if strings.Contains(nameLower, "title") {
		titles := []string{
			"Example Title",
			"Sample Title",
			"Playground Title",
			"Test Title",
		}
		return titles[rand.Intn(len(titles))]
	}
	
	// Message
	if strings.Contains(nameLower, "message") {
		messages := []string{
			"Operation completed successfully",
			"Request processed",
			"Action performed",
		}
		return messages[rand.Intn(len(messages))]
	}
	
	// Default fallback - use a more descriptive value
	return fmt.Sprintf("mock_%s_value", nameLower)
}

// generateID generates a mock ID (snowflake-like)
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))
}

// generateUUID generates a mock UUID
func generateUUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		rand.Uint32(),
		rand.Uint32()&0xffff,
		rand.Uint32()&0xffff,
		rand.Uint32()&0xffff,
		rand.Uint64()&0xffffffffffff)
}

// getKeys returns the keys of a map as a slice of strings
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// hasPattern checks if a schema has a specific pattern
func hasPattern(schema map[string]interface{}, pattern string) bool {
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		_, exists := properties[pattern]
		return exists
	}
	return false
}

// GenerateResponseFromSchema generates a response from an OpenAPI schema with special handling for X API patterns
func GenerateResponseFromSchema(schema map[string]interface{}, spec *OpenAPISpec, queryParams *QueryParams) ([]byte, error) {
	return GenerateResponseFromSchemaWithState(schema, spec, queryParams, nil)
}

// GenerateResponseFromSchemaWithState generates a response from an OpenAPI schema with state support
func GenerateResponseFromSchemaWithState(schema map[string]interface{}, spec *OpenAPISpec, queryParams *QueryParams, state *State) ([]byte, error) {
	if SchemaDebug {
		log.Printf("DEBUG: [GenerateResponseFromSchema] Starting generation")
	}
	
	response := GenerateMockResponseWithState(schema, spec, state)
	
	if SchemaDebug {
		if response == nil {
			log.Printf("DEBUG: [GenerateResponseFromSchema] GenerateMockResponse returned nil")
		} else {
			respJSON, _ := json.MarshalIndent(response, "", "  ")
			log.Printf("DEBUG: [GenerateResponseFromSchema] Generated response:\n%s", string(respJSON))
		}
	}
	
	// If the response is a map and doesn't have "data", wrap it
	// BUT: Only if the top-level schema doesn't already define a "data" property
	if respMap, ok := response.(map[string]interface{}); ok {
		// Apply field filtering if queryParams provided
		if queryParams != nil {
			respMap = applyFieldFilteringToResponse(respMap, queryParams, schema)
			
			// Handle expansions if requested
			if len(queryParams.Expansions) > 0 && state != nil {
				respMap = applyExpansionsToResponse(respMap, queryParams, state, spec)
			}
		}

		// Check if the original schema has a "data" property
		schemaHasData := false
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			if _, exists := props["data"]; exists {
				schemaHasData = true
			}
		}
		
		// Only wrap if schema doesn't already define "data" and response doesn't have it
		if !schemaHasData {
			if _, hasData := respMap["data"]; !hasData {
				// Check if this looks like it should be wrapped
				if len(respMap) > 0 {
					response = map[string]interface{}{
						"data": respMap,
					}
					if SchemaDebug {
						log.Printf("DEBUG: [GenerateResponseFromSchema] Wrapped response in 'data'")
					}
				}
			}
		} else if SchemaDebug {
			log.Printf("DEBUG: [GenerateResponseFromSchema] Schema already defines 'data' property, not wrapping")
		}
	}
	
	result, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		if SchemaDebug {
			log.Printf("DEBUG: [GenerateResponseFromSchema] Error marshaling JSON: %v", err)
		}
		return nil, fmt.Errorf("failed to marshal response from schema: %w", err)
	}
	
	if SchemaDebug {
		maxLen := 500
		if len(result) < maxLen {
			maxLen = len(result)
		}
		log.Printf("DEBUG: [GenerateResponseFromSchema] Final JSON (first 500 chars): %s", string(result[:maxLen]))
	}
	
	return result, nil
}

// applyFieldFilteringToResponse applies field filtering to a response based on query parameters
func applyFieldFilteringToResponse(respMap map[string]interface{}, queryParams *QueryParams, schema map[string]interface{}) map[string]interface{} {
	// If response has a "data" field, filter it
	if data, ok := respMap["data"].(map[string]interface{}); ok {
		// Try to infer the type from the schema or data structure
		fieldType := inferFieldTypeFromData(data, schema)
		if fieldType != "" {
			requestedFields := queryParams.GetRequestedFields(fieldType)
			if len(requestedFields) > 0 {
				respMap["data"] = filterFieldsByType(data, requestedFields, fieldType)
			}
		}
	} else if dataArray, ok := respMap["data"].([]interface{}); ok {
		// Handle array of objects
		if len(dataArray) > 0 {
			if firstItem, ok := dataArray[0].(map[string]interface{}); ok {
				fieldType := inferFieldTypeFromData(firstItem, schema)
				if fieldType != "" {
					requestedFields := queryParams.GetRequestedFields(fieldType)
					if len(requestedFields) > 0 {
						filteredArray := make([]interface{}, 0, len(dataArray))
						for _, item := range dataArray {
							if itemMap, ok := item.(map[string]interface{}); ok {
								filteredArray = append(filteredArray, filterFieldsByType(itemMap, requestedFields, fieldType))
							} else {
								filteredArray = append(filteredArray, item)
							}
						}
						respMap["data"] = filteredArray
					}
				}
			}
		}
	}

	return respMap
}

// inferFieldTypeFromData tries to infer the field type from data structure
func inferFieldTypeFromData(data map[string]interface{}, schema map[string]interface{}) string {
	// Check for common indicators
	if _, hasText := data["text"]; hasText {
		return "tweet"
	}
	if _, hasUsername := data["username"]; hasUsername {
		return "user"
	}
	if _, hasMemberCount := data["member_count"]; hasMemberCount {
		return "list"
	}
	// Could add more type detection here
	return ""
}

// filterFieldsByType filters fields based on type
func filterFieldsByType(data map[string]interface{}, requestedFields []string, fieldType string) map[string]interface{} {
	switch fieldType {
	case "user":
		return filterUserFields(data, requestedFields)
	case "tweet":
		return filterTweetFields(data, requestedFields)
	case "list":
		return filterListFields(data, requestedFields)
	default:
		// Generic filtering
		filtered := make(map[string]interface{})
		fieldMap := make(map[string]bool)
		for _, f := range requestedFields {
			fieldMap[strings.TrimSpace(f)] = true
		}
		for key, value := range data {
			if fieldMap[key] {
				filtered[key] = value
			}
		}
		return filtered
	}
}

// applyExpansionsToResponse applies expansions to a schema-generated response
func applyExpansionsToResponse(respMap map[string]interface{}, queryParams *QueryParams, state *State, spec *OpenAPISpec) map[string]interface{} {
	// Check if response has data field
	if data, ok := respMap["data"].(map[string]interface{}); ok {
		// Single object - check if it's a tweet
		if _, hasText := data["text"]; hasText {
			// It's a tweet - try to get the real tweet from state
			if tweetID, ok := data["id"].(string); ok {
				if tweet := state.GetTweet(tweetID); tweet != nil {
					// Use real tweet data
					tweetMap := FormatTweet(tweet)
					// Apply field filtering
					if len(queryParams.TweetFields) > 0 {
						tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
					}
					// Add expansion fields
					addExpansionFieldsToTweet(tweetMap, tweet, queryParams.Expansions)
					respMap["data"] = tweetMap
					
					// Build expansions
					includes := buildExpansions([]*Tweet{tweet}, queryParams.Expansions, state, spec, queryParams)
					if len(includes) > 0 {
						respMap["includes"] = includes
					}
				}
			}
		} else if _, hasUsername := data["username"]; hasUsername {
			// It's a user - try to get the real user from state
			if userID, ok := data["id"].(string); ok {
				if user := state.GetUserByID(userID); user != nil {
					// Use real user data
					userMap := FormatUser(user)
					// Apply field filtering
					if len(queryParams.UserFields) > 0 {
						userMap = filterUserFields(userMap, queryParams.UserFields)
					}
					// Add expansion fields
					addExpansionFieldsToUser(userMap, user, queryParams.Expansions)
					respMap["data"] = userMap
					
					// Handle pinned_tweet_id expansion
					for _, exp := range queryParams.Expansions {
						if exp == "pinned_tweet_id" && user.PinnedTweetID != "" {
							if pinnedTweet := state.GetTweet(user.PinnedTweetID); pinnedTweet != nil {
								tweetMap := FormatTweet(pinnedTweet)
								if len(queryParams.TweetFields) > 0 {
									tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
								}
								if includes, ok := respMap["includes"].(map[string]interface{}); ok {
									if tweets, ok := includes["tweets"].([]map[string]interface{}); ok {
										tweets = append(tweets, tweetMap)
										includes["tweets"] = tweets
									} else {
										includes["tweets"] = []map[string]interface{}{tweetMap}
									}
								} else {
									respMap["includes"] = map[string]interface{}{
										"tweets": []map[string]interface{}{tweetMap},
									}
								}
							}
						}
					}
				}
			}
		}
	} else if dataArray, ok := respMap["data"].([]interface{}); ok {
		// Array of objects - handle each item
		allTweets := []*Tweet{}
		for _, item := range dataArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if _, hasText := itemMap["text"]; hasText {
					// It's a tweet
					if tweetID, ok := itemMap["id"].(string); ok {
						if tweet := state.GetTweet(tweetID); tweet != nil {
							allTweets = append(allTweets, tweet)
						}
					}
				}
			}
		}
		
		// If we found tweets, build expansions for all of them
		if len(allTweets) > 0 && len(queryParams.Expansions) > 0 {
			includes := buildExpansions(allTweets, queryParams.Expansions, state, spec, queryParams)
			if len(includes) > 0 {
				respMap["includes"] = includes
			}
		}
	}

	return respMap
}

// tryGetUserFromState attempts to get a real user from state if the schema matches User type
func tryGetUserFromState(schema map[string]interface{}, state *State) map[string]interface{} {
	// Check if schema has user-like properties
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		// Check if it's a $ref to a User schema
		if ref, ok := schema["$ref"].(string); ok && strings.Contains(strings.ToLower(ref), "user") {
			// This might be a user reference
		} else {
			return nil
		}
	}

	// Check for user indicators
	hasUsername := false
	hasName := false
	if properties != nil {
		if _, ok := properties["username"]; ok {
			hasUsername = true
		}
		if _, ok := properties["name"]; ok {
			hasName = true
		}
	}

	// If it looks like a user schema, get a real user
	if hasUsername || hasName {
		state.mu.RLock()
		defer state.mu.RUnlock()
		
		// Get a random user
		for _, user := range state.users {
			userMap := FormatUser(user)
			return userMap
		}
	}

	return nil
}

// tryGetTweetFromState attempts to get a real tweet from state if the schema matches Tweet type
func tryGetTweetFromState(schema map[string]interface{}, state *State) map[string]interface{} {
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		if ref, ok := schema["$ref"].(string); ok && strings.Contains(strings.ToLower(ref), "tweet") {
			// This might be a tweet reference
		} else {
			return nil
		}
	}

	// Check for tweet indicators
	hasText := false
	hasAuthorID := false
	if properties != nil {
		if _, ok := properties["text"]; ok {
			hasText = true
		}
		if _, ok := properties["author_id"]; ok {
			hasAuthorID = true
		}
	}

	// If it looks like a tweet schema, get a real tweet
	if hasText || hasAuthorID {
		state.mu.RLock()
		defer state.mu.RUnlock()
		
		// Get a random tweet
		for _, tweet := range state.tweets {
			tweetMap := FormatTweet(tweet)
			return tweetMap
		}
	}

	return nil
}

// tryGetListFromState attempts to get a real list from state if the schema matches List type
func tryGetListFromState(schema map[string]interface{}, state *State) map[string]interface{} {
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Check for list indicators
	hasMemberCount := false
	hasFollowerCount := false
	if properties != nil {
		if _, ok := properties["member_count"]; ok {
			hasMemberCount = true
		}
		if _, ok := properties["follower_count"]; ok {
			hasFollowerCount = true
		}
	}

	// If it looks like a list schema, get a real list
	if hasMemberCount || hasFollowerCount {
		state.mu.RLock()
		defer state.mu.RUnlock()
		
		// Get a random list
		for _, list := range state.lists {
			listMap := formatList(list)
			return listMap
		}
	}

	return nil
}



// isRealIDInState checks if an ID exists in state
func isRealIDInState(id, fieldName string, state *State) bool {
	if state == nil {
		return false
	}
	
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	fieldLower := strings.ToLower(fieldName)
	if strings.Contains(fieldLower, "user") || strings.Contains(fieldLower, "author") {
		_, exists := state.users[id]
		return exists
	}
	if strings.Contains(fieldLower, "tweet") {
		_, exists := state.tweets[id]
		return exists
	}
	if strings.Contains(fieldLower, "list") {
		_, exists := state.lists[id]
		return exists
	}
	return false
}

// getRealIDFromState gets a real ID from state based on field name
func getRealIDFromState(fieldName string, state *State) string {
	if state == nil {
		return ""
	}
	
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	fieldLower := strings.ToLower(fieldName)
	if strings.Contains(fieldLower, "user") || strings.Contains(fieldLower, "author") {
		for id := range state.users {
			return id
		}
	}
	if strings.Contains(fieldLower, "tweet") {
		for id := range state.tweets {
			return id
		}
	}
	if strings.Contains(fieldLower, "list") {
		for id := range state.lists {
			return id
		}
	}
	return ""
}
