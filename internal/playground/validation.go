// Package playground provides request validation functions.
//
// This file validates request parameters, IDs, usernames, and input lengths
// against X API v2 constraints. It includes validation for snowflake IDs,
// usernames, tweet text, and other input fields, with proper error formatting
// matching the X API error response format.
package playground

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Input length limits matching X API constraints
const (
	MaxTweetLength       = 280  // X API tweet character limit
	MaxUsernameLength    = 15   // X API username character limit (enforced by regex)
	MaxDescriptionLength = 160  // X API user description character limit
	MaxListNameLength    = 25   // X API list name character limit
	MaxListDescriptionLength = 100 // X API list description character limit
	MaxSpaceTitleLength  = 70   // X API space title character limit
)

// State import limits to prevent memory exhaustion
const (
	MaxImportedUsers              = 10000
	MaxImportedTweets             = 100000
	MaxImportedLists              = 1000
	MaxImportedSpaces             = 1000
	MaxImportedMedia              = 10000
	MaxImportedPolls              = 1000
	MaxImportedPlaces             = 1000
	MaxImportedTopics             = 1000
	MaxImportedSearchStreamRules  = 1000
	MaxImportedSearchWebhooks     = 100
	MaxImportedDMConversations    = 1000
	MaxImportedDMEvents           = 10000
	MaxImportedComplianceJobs     = 100
	MaxImportedCommunities        = 100
	MaxImportedNews               = 1000
	MaxImportedNotes               = 1000
	MaxImportedActivitySubscriptions = 100
)

// Pre-compiled regex patterns for validation
var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	numericIDRegex = regexp.MustCompile(`^\d+$`)
	usernameRegex  = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
)

// ValidationError represents a validation error.
// It includes the parameter name, error message, and optional resource information.
type ValidationError struct {
	Parameter    string
	Message      string
	Value        interface{}
	ResourceID   string // Optional: resource ID (e.g., user ID, tweet ID)
	ResourceType string // Optional: resource type (e.g., "user", "tweet")
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for parameter '%s': %s", e.Parameter, e.Message)
}

// ValidateRequest validates a request against the OpenAPI operation spec
func ValidateRequest(r *http.Request, op *Operation, specPath string, spec *OpenAPISpec) []*ValidationError {
	return ValidateRequestWithPathItem(r, op, specPath, spec, nil)
}

// ValidateRequestWithPathItem validates a request against the OpenAPI operation spec, including path-level parameters
func ValidateRequestWithPathItem(r *http.Request, op *Operation, specPath string, spec *OpenAPISpec, pathItem *PathItem) []*ValidationError {
	var errors []*ValidationError

	// Validate path parameters
	pathParams := op.GetPathParameters()
	for _, param := range pathParams {
		value := extractPathParameter(r.URL.Path, param.Name, specPath)
		if param.Required && value == "" {
			errors = append(errors, &ValidationError{
				Parameter: param.Name,
				Message:   "required path parameter is missing",
				Value:     nil,
			})
		}
		if value != "" {
			if err := validateParameterValue(value, param, spec); err != nil {
				errors = append(errors, err)
			}
		}
	}

	// Validate query parameters - use path-level parameters if available
	var queryParams []Parameter
	if pathItem != nil && spec != nil {
		queryParams = op.GetQueryParametersWithPathParams(pathItem, spec)
	} else {
		queryParams = op.GetQueryParameters()
	}
	query := r.URL.Query()
	
	// Build map of valid parameter names for checking unknown parameters
	validParamNames := make(map[string]bool)
	for _, param := range queryParams {
		validParamNames[param.Name] = true
	}
	
	// Check for unknown query parameters (not in spec)
	for paramName := range query {
		// Skip field selection parameters and expansions - these are always valid
		// Check if it's a field parameter (ends with .fields) or expansions
		// This matches the X API pattern where any {object}.fields is valid
		if strings.HasSuffix(paramName, ".fields") || paramName == "expansions" {
			continue
		}
		
		if !validParamNames[paramName] {
			errors = append(errors, &ValidationError{
				Parameter: paramName,
				Message:   fmt.Sprintf("The query parameter '%s' is not valid for this endpoint", paramName),
				Value:     query.Get(paramName),
			})
		}
	}
	
	// Validate each query parameter from spec
	for _, param := range queryParams {
		value := query.Get(param.Name)
		if param.Required && value == "" {
			errors = append(errors, &ValidationError{
				Parameter: param.Name,
				Message:   "required query parameter is missing",
				Value:     nil,
			})
		}
		if value != "" {
			// Enhanced validation for array parameters
			if err := validateQueryParameterValue(value, param, spec); err != nil {
				errors = append(errors, err)
			}
		}
	}

	return errors
}

// extractPathParameter extracts a path parameter value from the request path
func extractPathParameter(requestPath, paramName, specPath string) string {
	// Remove query parameters
	requestPath = strings.Split(requestPath, "?")[0]

	specParts := strings.Split(specPath, "/")
	requestParts := strings.Split(requestPath, "/")

	if len(specParts) != len(requestParts) {
		return ""
	}

	for i := 0; i < len(specParts); i++ {
		specPart := specParts[i]
		// Check if this is the parameter we're looking for
		if strings.HasPrefix(specPart, "{") && strings.HasSuffix(specPart, "}") {
			paramNameInSpec := strings.Trim(specPart, "{}")
			if paramNameInSpec == paramName {
				return requestParts[i]
			}
		}
	}

	return ""
}

// validateQueryParameterValue validates a query parameter value against its schema
// Enhanced version that handles arrays and special formats
func validateQueryParameterValue(value string, param Parameter, spec *OpenAPISpec) *ValidationError {
	// Check if parameter is an array type (comma-separated)
	if param.Schema != nil {
		schema := param.Schema
		
		// Resolve $ref if present
		if ref, ok := schema["$ref"].(string); ok {
			resolved := spec.ResolveRef(ref)
			if resolved != nil {
				schema = resolved
			}
		}
		
		schemaType, _ := schema["type"].(string)
		
		// Handle array parameters (comma-separated values)
		if schemaType == "array" {
			return validateArrayParameter(value, param, schema, spec)
		}
	}
	
	// Fall back to standard validation
	return validateParameterValue(value, param, spec)
}

// validateArrayParameter validates a comma-separated array parameter
func validateArrayParameter(value string, param Parameter, schema map[string]interface{}, spec *OpenAPISpec) *ValidationError {
	// Split comma-separated values
	values := strings.Split(value, ",")
	
	// Validate minItems
	if minItems, ok := schema["minItems"].(float64); ok {
		if len(values) < int(minItems) {
			return &ValidationError{
				Parameter: param.Name,
				Message:   fmt.Sprintf("array must have at least %d items", int(minItems)),
				Value:     value,
			}
		}
	}
	
	// Validate maxItems
	if maxItems, ok := schema["maxItems"].(float64); ok {
		if len(values) > int(maxItems) {
			return &ValidationError{
				Parameter: param.Name,
				Message:   fmt.Sprintf("array must have at most %d items", int(maxItems)),
				Value:     value,
			}
		}
	}
	
	// Validate each item
	if items, ok := schema["items"].(map[string]interface{}); ok {
		for i, itemValue := range values {
			itemValue = strings.TrimSpace(itemValue)
			if itemValue == "" {
				continue // Skip empty values
			}
			
			// Create a temporary parameter for item validation
			itemParam := Parameter{
				Name:   fmt.Sprintf("%s[%d]", param.Name, i),
				Schema: items,
			}
			
			if err := validateParameterValue(itemValue, itemParam, spec); err != nil {
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("invalid value '%s' at index %d: %s", itemValue, i, err.Message),
					Value:     value,
				}
			}
		}
	}
	
	return nil
}

// validateParameterValue validates a parameter value against its schema
func validateParameterValue(value string, param Parameter, spec *OpenAPISpec) *ValidationError {
	if param.Schema == nil {
		return nil // No schema to validate against
	}

	schema := param.Schema

	// Resolve $ref if present
	if ref, ok := schema["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			schema = resolved
		}
	}

	// Get type from schema
	schemaType, _ := schema["type"].(string)

	switch schemaType {
	case "string":
		// Validate string constraints
		if minLength, ok := schema["minLength"].(float64); ok {
			if len(value) < int(minLength) {
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("string length must be at least %d", int(minLength)),
					Value:     value,
				}
			}
		}
		if maxLength, ok := schema["maxLength"].(float64); ok {
			if len(value) > int(maxLength) {
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("string length must be at most %d", int(maxLength)),
					Value:     value,
				}
			}
		}
		if pattern, ok := schema["pattern"].(string); ok {
			// Enhanced pattern validation with regex
			if pattern != "" {
				matched, err := regexp.MatchString(pattern, value)
				if err == nil && !matched {
					return &ValidationError{
						Parameter: param.Name,
						Message:   fmt.Sprintf("The `%s` query parameter value [%s] does not match %s", param.Name, value, pattern),
						Value:     value,
					}
				}
			}
		}
		
		// Validate format (date-time, email, etc.)
		if format, ok := schema["format"].(string); ok {
			if err := validateFormat(value, format); err != nil {
				return &ValidationError{
					Parameter: param.Name,
					Message:   err.Error(),
					Value:     value,
				}
			}
		}
		if enum, ok := schema["enum"].([]interface{}); ok {
			valid := false
			for _, e := range enum {
				if eStr, ok := e.(string); ok && eStr == value {
					valid = true
					break
				}
			}
			if !valid {
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("value must be one of: %v", enum),
					Value:     value,
				}
			}
		}

	case "integer", "number":
		numValue, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return &ValidationError{
				Parameter: param.Name,
				Message:   fmt.Sprintf("value must be a %s", schemaType),
				Value:     value,
			}
		}
		if minimum, ok := schema["minimum"].(float64); ok {
			if numValue < minimum {
				// Format minimum as integer if it's a whole number
				minStr := fmt.Sprintf("%.0f", minimum)
				if minimum == float64(int64(minimum)) {
					return &ValidationError{
						Parameter: param.Name,
						Message:   fmt.Sprintf("value must be at least %s", minStr),
						Value:     value,
					}
				}
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("value must be at least %v", minimum),
					Value:     value,
				}
			}
		}
		if maximum, ok := schema["maximum"].(float64); ok {
			if numValue > maximum {
				// Format maximum as integer if it's a whole number
				maxStr := fmt.Sprintf("%.0f", maximum)
				if maximum == float64(int64(maximum)) {
					return &ValidationError{
						Parameter: param.Name,
						Message:   fmt.Sprintf("value must be at most %s", maxStr),
						Value:     value,
					}
				}
				return &ValidationError{
					Parameter: param.Name,
					Message:   fmt.Sprintf("value must be at most %v", maximum),
					Value:     value,
				}
			}
		}

	case "boolean":
		if value != "true" && value != "false" {
			return &ValidationError{
				Parameter: param.Name,
				Message:   "value must be 'true' or 'false'",
				Value:     value,
			}
		}
	}

	return nil
}

// validateFormat validates a value against a format constraint
func validateFormat(value, format string) error {
	switch format {
	case "date-time":
		// ISO 8601 date-time format
		_, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return fmt.Errorf("value must be a valid ISO 8601 date-time (RFC3339)")
		}
	case "date":
		// ISO 8601 date format (YYYY-MM-DD)
		_, err := time.Parse("2006-01-02", value)
		if err != nil {
			return fmt.Errorf("value must be a valid date (YYYY-MM-DD)")
		}
	case "email":
		// Basic email validation
		if !emailRegex.MatchString(value) {
			return fmt.Errorf("value must be a valid email address")
		}
	case "uri":
		// Basic URI validation
		if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
			return fmt.Errorf("value must be a valid URI")
		}
	}
	return nil
}

// ValidateSnowflakeID validates a snowflake ID format
// X API uses Twitter snowflake IDs: 19 digits, typically between 0 and 2^63-1
func ValidateSnowflakeID(id string) *ValidationError {
	if id == "" {
		return &ValidationError{
			Parameter: "id",
			Message:   "ID cannot be empty",
			Value:     id,
		}
	}

	// Check if it's all digits
	if !numericIDRegex.MatchString(id) {
		return &ValidationError{
			Parameter: "id",
			Message:   fmt.Sprintf("The `id` query parameter value [%s] is not valid", id),
			Value:     id,
		}
	}

	// Check length (snowflake IDs are typically 19 digits, but can be shorter)
	// X API accepts IDs from 1 to 19 digits
	if len(id) > 19 {
		return &ValidationError{
			Parameter: "id",
			Message:   fmt.Sprintf("The `id` query parameter value [%s] is not valid", id),
			Value:     id,
		}
	}

	// Try to parse as int64 to check if it's within valid range
	if num, err := strconv.ParseInt(id, 10, 64); err != nil || num < 0 {
		return &ValidationError{
			Parameter: "id",
			Message:   fmt.Sprintf("The `id` query parameter value [%s] is not valid", id),
			Value:     id,
		}
	}

	return nil
}

// ValidateUsername validates a username format
// X API username format: ^[A-Za-z0-9_]{1,15}$
func ValidateUsername(username string) *ValidationError {
	if username == "" {
		return &ValidationError{
			Parameter: "username",
			Message:   "Username cannot be empty",
			Value:     username,
		}
	}

	// X API username regex: ^[A-Za-z0-9_]{1,15}$
	if !usernameRegex.MatchString(username) {
		return &ValidationError{
			Parameter: "username",
			Message:   fmt.Sprintf("The `username` query parameter value [%s] does not match ^[A-Za-z0-9_]{1,15}$", username),
			Value:     username,
		}
	}

	return nil
}

// FormatValidationErrors formats validation errors into X API error response format
// Matches real X API error format exactly
func FormatValidationErrors(errors []*ValidationError) map[string]interface{} {
	if len(errors) == 0 {
		return nil
	}

	// Special handling for ids parameter errors - group them together
	// Real API returns a single error with parameters.id array containing all invalid IDs
	idErrors := make([]*ValidationError, 0)
	fieldErrors := make([]*ValidationError, 0) // Field validation errors (tweet.fields, user.fields, etc.)
	expansionErrors := make([]*ValidationError, 0) // Expansion validation errors
	partitionErrors := make([]*ValidationError, 0) // Partition parameter errors
	otherErrors := make([]*ValidationError, 0)
	
	for _, err := range errors {
		// Check if this is an id/ids parameter error (could be "id" or "ids" from OpenAPI validation)
		if err.Parameter == "id" || err.Parameter == "ids" {
			idErrors = append(idErrors, err)
		} else if strings.HasSuffix(err.Parameter, ".fields") {
			// Field validation errors (tweet.fields, user.fields, etc.)
			fieldErrors = append(fieldErrors, err)
		} else if err.Parameter == "expansions" {
			// Expansion validation errors
			expansionErrors = append(expansionErrors, err)
		} else if err.Parameter == "partition" {
			// Partition parameter errors (for streaming endpoints)
			partitionErrors = append(partitionErrors, err)
		} else {
			otherErrors = append(otherErrors, err)
		}
	}
	
	errorList := make([]map[string]interface{}, 0)
	
	// Handle ids parameter errors specially
	if len(idErrors) > 0 {
		invalidIds := make([]string, 0, len(idErrors))
		for _, err := range idErrors {
			if err.Value != nil {
				invalidIds = append(invalidIds, fmt.Sprintf("%v", err.Value))
			}
		}
		if len(invalidIds) > 0 {
			// Real API uses "id" (singular) in parameters map even though query param is "ids"
			errorObj := map[string]interface{}{
				"parameters": map[string]interface{}{
					"id": invalidIds,
				},
				"message": fmt.Sprintf("The `id` query parameter value [%s] is not valid", invalidIds[0]),
			}
			errorList = append(errorList, errorObj)
		}
	}
	
	// Handle field validation errors specially (tweet.fields, user.fields, etc.)
	// Real API format: parameters.tweet.fields with message
	for _, err := range fieldErrors {
		var valueStr string
		if err.Value != nil {
			valueStr = fmt.Sprintf("%v", err.Value)
		}
		errorObj := map[string]interface{}{
			"parameters": map[string]interface{}{
				err.Parameter: []string{valueStr},
			},
			"message": err.Message,
		}
		errorList = append(errorList, errorObj)
	}
	
	// Handle expansion validation errors specially (expansions)
	// Real API format: parameters.expansions with message
	for _, err := range expansionErrors {
		var valueStr string
		if err.Value != nil {
			valueStr = fmt.Sprintf("%v", err.Value)
		}
		errorObj := map[string]interface{}{
			"parameters": map[string]interface{}{
				err.Parameter: []string{valueStr},
			},
			"message": err.Message,
		}
		errorList = append(errorList, errorObj)
	}
	
	// Handle partition parameter errors specially (for streaming endpoints)
	// Real API format: parameters.partition with empty array and message
	for _, err := range partitionErrors {
		// Update message to match expected format
		message := err.Message
		if message == "required query parameter is missing" {
			message = "The `partition` query parameter can not be empty"
		}
		errorObj := map[string]interface{}{
			"parameters": map[string]interface{}{
				"partition": []string{},
			},
			"message": message,
		}
		errorList = append(errorList, errorObj)
	}
	
	// Handle other errors (pattern validation, type validation, etc.)
	// Real API format for these errors uses only "parameters" and "message" fields
	for _, err := range otherErrors {
		var valueStr string
		if err.Value != nil {
			valueStr = fmt.Sprintf("%v", err.Value)
		}
		
		// Check if this is a required parameter missing error
		// Real API format: empty array in parameters, message "The `{param}` query parameter can not be empty"
		if err.Message == "required query parameter is missing" {
			errorObj := map[string]interface{}{
				"parameters": map[string]interface{}{
					err.Parameter: []string{},
				},
				"message": fmt.Sprintf("The `%s` query parameter can not be empty", err.Parameter),
			}
			errorList = append(errorList, errorObj)
			continue
		}
		
		// Check if this is a pattern validation error or similar validation error
		// Real API uses simplified format: only "parameters" and "message"
		isPatternError := strings.Contains(err.Message, "does not match")
		isTypeError := strings.Contains(err.Message, "must be") || strings.Contains(err.Message, "must have")
		isFormatError := strings.Contains(err.Message, "must be a valid")
		
		if isPatternError || isTypeError || isFormatError {
			// Use simplified format matching real API
			errorObj := map[string]interface{}{
				"parameters": map[string]interface{}{
					err.Parameter: []string{valueStr},
				},
				"message": err.Message,
			}
			errorList = append(errorList, errorObj)
		} else {
			// For other errors (like resource not found), use full format with resource_id/resource_type
			errorObj := map[string]interface{}{
				"parameters": map[string]interface{}{
					err.Parameter: []string{valueStr},
				},
				"message": err.Message,
			}
			
			// Add resource_id and resource_type if provided
			if err.ResourceID != "" {
				errorObj["resource_id"] = err.ResourceID
			}
			if err.ResourceType != "" {
				errorObj["resource_type"] = err.ResourceType
			}
			
			errorList = append(errorList, errorObj)
		}
	}

	return map[string]interface{}{
		"errors": errorList,
		"title":  "Invalid Request",
		"detail": "One or more parameters to your request was invalid.",
		"type":   "https://api.twitter.com/2/problems/invalid-request",
	}
}

// FormatSingleValidationError formats a single validation error into X API error response format
// Matches real X API error format exactly
func FormatSingleValidationError(err *ValidationError) map[string]interface{} {
	if err == nil {
		return nil
	}

	var valueStr string
	if err.Value != nil {
		valueStr = fmt.Sprintf("%v", err.Value)
	}

	errorObj := map[string]interface{}{
		"parameter": err.Parameter,
		"value":     valueStr,
		"detail":    err.Message,
		"title":     "Invalid Request",
		"type":      "https://api.twitter.com/2/problems/invalid-request",
	}
	
	// Add resource_id and resource_type if provided
	if err.ResourceID != "" {
		errorObj["resource_id"] = err.ResourceID
	}
	if err.ResourceType != "" {
		errorObj["resource_type"] = err.ResourceType
	}
	
	// Also include parameters map for compatibility
	errorObj["parameters"] = map[string]interface{}{
		err.Parameter: []string{valueStr},
	}
	
	return map[string]interface{}{
		"errors": []map[string]interface{}{errorObj},
		"title":  "Invalid Request",
		"detail": "One or more parameters to your request was invalid.",
		"type":   "https://api.twitter.com/2/problems/invalid-request",
	}
}

// ValidateRequestBody validates a request body against the OpenAPI requestBody schema
func ValidateRequestBody(body []byte, op *Operation, spec *OpenAPISpec) []*ValidationError {
	var errors []*ValidationError

	// Check if operation requires a request body
	if op.RequestBody == nil {
		// No request body expected
		if len(body) > 0 {
			// Body provided but not expected - this is usually OK, but we could warn
			return nil
		}
		return nil
	}

	// Get request body schema early to check if it has properties
	schema := op.GetRequestBodySchema()
	hasProperties := false
	if schema != nil {
		// Resolve $ref if present to check the actual schema
		resolvedSchema := schema
		if ref, ok := schema["$ref"].(string); ok && spec != nil {
			if resolved := spec.ResolveRef(ref); resolved != nil {
				resolvedSchema = resolved
			}
		}
		
		// Check if resolved schema has properties
		if properties, ok := resolvedSchema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
			hasProperties = true
		}
		// Also check if there are required fields in the schema
		if requiredFields, ok := resolvedSchema["required"].([]interface{}); ok && len(requiredFields) > 0 {
			hasProperties = true
		}
		// Check for allOf/oneOf/anyOf that might contain properties
		if allOf, ok := resolvedSchema["allOf"].([]interface{}); ok {
			for _, item := range allOf {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if props, ok := itemMap["properties"].(map[string]interface{}); ok && len(props) > 0 {
						hasProperties = true
						break
					}
				}
			}
		}
	}
	
	// Check if request body is required but missing
	// However, if the schema is empty (no properties), allow empty body even if required
	// Only require body if it's marked as required AND the schema has properties/required fields
	if op.RequestBody.Required && len(body) == 0 && hasProperties {
		errors = append(errors, &ValidationError{
			Parameter: "requestBody",
			Message:   "request body is required",
			Value:     nil,
		})
		return errors
	}

	// If no body and not required (or schema is empty), that's fine
	if len(body) == 0 {
		return nil
	}

	// Parse JSON body
	var bodyData interface{}
	if err := json.Unmarshal(body, &bodyData); err != nil {
		errors = append(errors, &ValidationError{
			Parameter: "requestBody",
			Message:   fmt.Sprintf("invalid JSON: %v", err),
			Value:     string(body),
		})
		return errors
	}
	if schema == nil {
		// No schema to validate against - that's OK
		return nil
	}

	// Resolve $ref if present
	if ref, ok := schema["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			schema = resolved
		}
	}

	// Validate body against schema
	bodyErrors := validateValueAgainstSchema(bodyData, schema, spec, "requestBody")
	errors = append(errors, bodyErrors...)

	return errors
}

// validateValueAgainstSchema validates a value against an OpenAPI schema
func validateValueAgainstSchema(value interface{}, schema map[string]interface{}, spec *OpenAPISpec, path string) []*ValidationError {
	var errors []*ValidationError

	// Resolve $ref if present
	if ref, ok := schema["$ref"].(string); ok {
		resolved := spec.ResolveRef(ref)
		if resolved != nil {
			schema = resolved
		}
	}

	// Handle allOf, oneOf, anyOf
	if allOf, ok := schema["allOf"].([]interface{}); ok {
		// Validate against all schemas in allOf
		for _, item := range allOf {
			if itemSchema, ok := item.(map[string]interface{}); ok {
				itemErrors := validateValueAgainstSchema(value, itemSchema, spec, path)
				errors = append(errors, itemErrors...)
			}
		}
		return errors
	}

	// Get schema type
	schemaType, _ := schema["type"].(string)

	switch schemaType {
	case "object":
		// Validate object
		valueMap, ok := value.(map[string]interface{})
		if !ok {
			errors = append(errors, &ValidationError{
				Parameter: path,
				Message:   "value must be an object",
				Value:     value,
			})
			return errors
		}

		// Check required fields
		if required, ok := schema["required"].([]interface{}); ok {
			for _, reqField := range required {
				if reqFieldStr, ok := reqField.(string); ok {
					if _, exists := valueMap[reqFieldStr]; !exists {
						errors = append(errors, &ValidationError{
							Parameter: fmt.Sprintf("%s.%s", path, reqFieldStr),
							Message:   fmt.Sprintf("required field '%s' is missing", reqFieldStr),
							Value:     nil,
						})
					}
				}
			}
		}

		// Validate properties
		if properties, ok := schema["properties"].(map[string]interface{}); ok {
			for propName, propSchema := range properties {
				if propSchemaMap, ok := propSchema.(map[string]interface{}); ok {
					if propValue, exists := valueMap[propName]; exists {
						propPath := fmt.Sprintf("%s.%s", path, propName)
						propErrors := validateValueAgainstSchema(propValue, propSchemaMap, spec, propPath)
						errors = append(errors, propErrors...)
					}
				}
			}
		}

	case "array":
		// Validate array
		valueArray, ok := value.([]interface{})
		if !ok {
			errors = append(errors, &ValidationError{
				Parameter: path,
				Message:   "value must be an array",
				Value:     value,
			})
			return errors
		}

		// Validate minItems
		if minItems, ok := schema["minItems"].(float64); ok {
			if len(valueArray) < int(minItems) {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("array must have at least %d items", int(minItems)),
					Value:     valueArray,
				})
			}
		}

		// Validate maxItems
		if maxItems, ok := schema["maxItems"].(float64); ok {
			if len(valueArray) > int(maxItems) {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("array must have at most %d items", int(maxItems)),
					Value:     valueArray,
				})
			}
		}

		// Validate items
		if items, ok := schema["items"].(map[string]interface{}); ok {
			for i, itemValue := range valueArray {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				itemErrors := validateValueAgainstSchema(itemValue, items, spec, itemPath)
				errors = append(errors, itemErrors...)
			}
		}

	case "string":
		// Validate string
		valueStr, ok := value.(string)
		if !ok {
			errors = append(errors, &ValidationError{
				Parameter: path,
				Message:   "value must be a string",
				Value:     value,
			})
			return errors
		}

		// Validate minLength
		if minLength, ok := schema["minLength"].(float64); ok {
			if len(valueStr) < int(minLength) {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("string length must be at least %d", int(minLength)),
					Value:     valueStr,
				})
			}
		}

		// Validate maxLength
		if maxLength, ok := schema["maxLength"].(float64); ok {
			if len(valueStr) > int(maxLength) {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("string length must be at most %d", int(maxLength)),
					Value:     valueStr,
				})
			}
		}

		// Validate enum
		if enum, ok := schema["enum"].([]interface{}); ok {
			valid := false
			for _, e := range enum {
				if eStr, ok := e.(string); ok && eStr == valueStr {
					valid = true
					break
				}
			}
			if !valid {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("value must be one of: %v", enum),
					Value:     valueStr,
				})
			}
		}

	case "integer", "number":
		// Validate number
		var numValue float64
		switch v := value.(type) {
		case float64:
			numValue = v
		case int:
			numValue = float64(v)
		case int64:
			numValue = float64(v)
		default:
			errors = append(errors, &ValidationError{
				Parameter: path,
				Message:   fmt.Sprintf("value must be a %s", schemaType),
				Value:     value,
			})
			return errors
		}

		// Validate minimum
		if minimum, ok := schema["minimum"].(float64); ok {
			if numValue < minimum {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("value must be at least %v", minimum),
					Value:     numValue,
				})
			}
		}

		// Validate maximum
		if maximum, ok := schema["maximum"].(float64); ok {
			if numValue > maximum {
				errors = append(errors, &ValidationError{
					Parameter: path,
					Message:   fmt.Sprintf("value must be at most %v", maximum),
					Value:     numValue,
				})
			}
		}

	case "boolean":
		// Validate boolean
		_, ok := value.(bool)
		if !ok {
			errors = append(errors, &ValidationError{
				Parameter: path,
				Message:   "value must be a boolean",
				Value:     value,
			})
		}
	}

	return errors
}

// ValidateStateImport validates imported state data for security and integrity
func ValidateStateImport(importData *StateExport) []*ValidationError {
	var errors []*ValidationError

	if importData == nil {
		return errors
	}

	// Validate entity counts to prevent memory exhaustion
	if importData.Users != nil && len(importData.Users) > MaxImportedUsers {
		errors = append(errors, &ValidationError{
			Parameter: "users",
			Message:   fmt.Sprintf("Too many users: %d (maximum: %d)", len(importData.Users), MaxImportedUsers),
			Value:     len(importData.Users),
		})
	}

	if importData.Tweets != nil && len(importData.Tweets) > MaxImportedTweets {
		errors = append(errors, &ValidationError{
			Parameter: "tweets",
			Message:   fmt.Sprintf("Too many tweets: %d (maximum: %d)", len(importData.Tweets), MaxImportedTweets),
			Value:     len(importData.Tweets),
		})
	}

	if importData.Lists != nil && len(importData.Lists) > MaxImportedLists {
		errors = append(errors, &ValidationError{
			Parameter: "lists",
			Message:   fmt.Sprintf("Too many lists: %d (maximum: %d)", len(importData.Lists), MaxImportedLists),
			Value:     len(importData.Lists),
		})
	}

	if importData.Spaces != nil && len(importData.Spaces) > MaxImportedSpaces {
		errors = append(errors, &ValidationError{
			Parameter: "spaces",
			Message:   fmt.Sprintf("Too many spaces: %d (maximum: %d)", len(importData.Spaces), MaxImportedSpaces),
			Value:     len(importData.Spaces),
		})
	}

	if importData.Media != nil && len(importData.Media) > MaxImportedMedia {
		errors = append(errors, &ValidationError{
			Parameter: "media",
			Message:   fmt.Sprintf("Too many media: %d (maximum: %d)", len(importData.Media), MaxImportedMedia),
			Value:     len(importData.Media),
		})
	}

	if importData.Polls != nil && len(importData.Polls) > MaxImportedPolls {
		errors = append(errors, &ValidationError{
			Parameter: "polls",
			Message:   fmt.Sprintf("Too many polls: %d (maximum: %d)", len(importData.Polls), MaxImportedPolls),
			Value:     len(importData.Polls),
		})
	}

	if importData.Places != nil && len(importData.Places) > MaxImportedPlaces {
		errors = append(errors, &ValidationError{
			Parameter: "places",
			Message:   fmt.Sprintf("Too many places: %d (maximum: %d)", len(importData.Places), MaxImportedPlaces),
			Value:     len(importData.Places),
		})
	}

	if importData.Topics != nil && len(importData.Topics) > MaxImportedTopics {
		errors = append(errors, &ValidationError{
			Parameter: "topics",
			Message:   fmt.Sprintf("Too many topics: %d (maximum: %d)", len(importData.Topics), MaxImportedTopics),
			Value:     len(importData.Topics),
		})
	}

	if importData.SearchStreamRules != nil && len(importData.SearchStreamRules) > MaxImportedSearchStreamRules {
		errors = append(errors, &ValidationError{
			Parameter: "search_stream_rules",
			Message:   fmt.Sprintf("Too many search stream rules: %d (maximum: %d)", len(importData.SearchStreamRules), MaxImportedSearchStreamRules),
			Value:     len(importData.SearchStreamRules),
		})
	}

	if importData.SearchWebhooks != nil && len(importData.SearchWebhooks) > MaxImportedSearchWebhooks {
		errors = append(errors, &ValidationError{
			Parameter: "search_webhooks",
			Message:   fmt.Sprintf("Too many search webhooks: %d (maximum: %d)", len(importData.SearchWebhooks), MaxImportedSearchWebhooks),
			Value:     len(importData.SearchWebhooks),
		})
	}

	if importData.DMConversations != nil && len(importData.DMConversations) > MaxImportedDMConversations {
		errors = append(errors, &ValidationError{
			Parameter: "dm_conversations",
			Message:   fmt.Sprintf("Too many DM conversations: %d (maximum: %d)", len(importData.DMConversations), MaxImportedDMConversations),
			Value:     len(importData.DMConversations),
		})
	}

	if importData.DMEvents != nil && len(importData.DMEvents) > MaxImportedDMEvents {
		errors = append(errors, &ValidationError{
			Parameter: "dm_events",
			Message:   fmt.Sprintf("Too many DM events: %d (maximum: %d)", len(importData.DMEvents), MaxImportedDMEvents),
			Value:     len(importData.DMEvents),
		})
	}

	if importData.ComplianceJobs != nil && len(importData.ComplianceJobs) > MaxImportedComplianceJobs {
		errors = append(errors, &ValidationError{
			Parameter: "compliance_jobs",
			Message:   fmt.Sprintf("Too many compliance jobs: %d (maximum: %d)", len(importData.ComplianceJobs), MaxImportedComplianceJobs),
			Value:     len(importData.ComplianceJobs),
		})
	}

	if importData.Communities != nil && len(importData.Communities) > MaxImportedCommunities {
		errors = append(errors, &ValidationError{
			Parameter: "communities",
			Message:   fmt.Sprintf("Too many communities: %d (maximum: %d)", len(importData.Communities), MaxImportedCommunities),
			Value:     len(importData.Communities),
		})
	}

	if importData.News != nil && len(importData.News) > MaxImportedNews {
		errors = append(errors, &ValidationError{
			Parameter: "news",
			Message:   fmt.Sprintf("Too many news: %d (maximum: %d)", len(importData.News), MaxImportedNews),
			Value:     len(importData.News),
		})
	}

	if importData.Notes != nil && len(importData.Notes) > MaxImportedNotes {
		errors = append(errors, &ValidationError{
			Parameter: "notes",
			Message:   fmt.Sprintf("Too many notes: %d (maximum: %d)", len(importData.Notes), MaxImportedNotes),
			Value:     len(importData.Notes),
		})
	}

	if importData.ActivitySubscriptions != nil && len(importData.ActivitySubscriptions) > MaxImportedActivitySubscriptions {
		errors = append(errors, &ValidationError{
			Parameter: "activity_subscriptions",
			Message:   fmt.Sprintf("Too many activity subscriptions: %d (maximum: %d)", len(importData.ActivitySubscriptions), MaxImportedActivitySubscriptions),
			Value:     len(importData.ActivitySubscriptions),
		})
	}

	// Validate total entity count to prevent memory exhaustion from combination of entities
	// This is a safety check in addition to individual entity limits
	const MaxTotalImportedEntities = 200000 // Total limit across all entity types
	totalEntities := 0
	if importData.Users != nil {
		totalEntities += len(importData.Users)
	}
	if importData.Tweets != nil {
		totalEntities += len(importData.Tweets)
	}
	if importData.Lists != nil {
		totalEntities += len(importData.Lists)
	}
	if importData.Spaces != nil {
		totalEntities += len(importData.Spaces)
	}
	if importData.Media != nil {
		totalEntities += len(importData.Media)
	}
	if importData.Polls != nil {
		totalEntities += len(importData.Polls)
	}
	if importData.Places != nil {
		totalEntities += len(importData.Places)
	}
	if importData.Topics != nil {
		totalEntities += len(importData.Topics)
	}
	if importData.SearchStreamRules != nil {
		totalEntities += len(importData.SearchStreamRules)
	}
	if importData.SearchWebhooks != nil {
		totalEntities += len(importData.SearchWebhooks)
	}
	if importData.DMConversations != nil {
		totalEntities += len(importData.DMConversations)
	}
	if importData.DMEvents != nil {
		totalEntities += len(importData.DMEvents)
	}
	if importData.ComplianceJobs != nil {
		totalEntities += len(importData.ComplianceJobs)
	}
	if importData.Communities != nil {
		totalEntities += len(importData.Communities)
	}
	if importData.News != nil {
		totalEntities += len(importData.News)
	}
	if importData.Notes != nil {
		totalEntities += len(importData.Notes)
	}
	if importData.ActivitySubscriptions != nil {
		totalEntities += len(importData.ActivitySubscriptions)
	}
	
	if totalEntities > MaxTotalImportedEntities {
		errors = append(errors, &ValidationError{
			Parameter: "total_entities",
			Message:   fmt.Sprintf("Total entity count too large: %d (maximum: %d). This import would exceed memory limits.", totalEntities, MaxTotalImportedEntities),
			Value:     totalEntities,
		})
	}

	// Validate ID formats for users and tweets
	if importData.Users != nil {
		for id, user := range importData.Users {
			if user == nil {
				continue
			}
			// Validate user ID format (should be numeric)
			if err := ValidateSnowflakeID(id); err != nil {
				errors = append(errors, &ValidationError{
					Parameter:    "users",
					Message:      fmt.Sprintf("Invalid user ID format: %s", id),
					Value:        id,
					ResourceID:   id,
					ResourceType: "user",
				})
			}
			// Validate username if present
			if user.Username != "" {
				if err := ValidateUsername(user.Username); err != nil {
					errors = append(errors, &ValidationError{
						Parameter:    "username",
						Message:      err.Message,
						Value:        user.Username,
						ResourceID:   id,
						ResourceType: "user",
					})
				}
			}
		}
	}

	if importData.Tweets != nil {
		for id := range importData.Tweets {
			// Validate tweet ID format (should be numeric)
			if err := ValidateSnowflakeID(id); err != nil {
				errors = append(errors, &ValidationError{
					Parameter:    "tweets",
					Message:      fmt.Sprintf("Invalid tweet ID format: %s", id),
					Value:        id,
					ResourceID:   id,
					ResourceType: "tweet",
				})
			}
		}
	}

	return errors
}

// SanitizeInput sanitizes user input to prevent XSS and control character issues
// - HTML escapes special characters
// - Filters control characters (except newlines and tabs)
// - Returns sanitized string
func SanitizeInput(input string) string {
	// HTML escape to prevent XSS
	sanitized := html.EscapeString(input)
	
	// Filter control characters (keep newlines \n and tabs \t)
	var result strings.Builder
	result.Grow(len(sanitized))
	
	for _, r := range sanitized {
		// Allow printable characters, newlines, and tabs
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
		// All other control characters are filtered out
	}
	
	return result.String()
}

// SanitizeInputForDisplay sanitizes input specifically for HTML display
// This is more aggressive - removes all control characters including newlines/tabs
func SanitizeInputForDisplay(input string) string {
	// HTML escape
	sanitized := html.EscapeString(input)
	
	// Filter all control characters
	var result strings.Builder
	result.Grow(len(sanitized))
	
	for _, r := range sanitized {
		if unicode.IsPrint(r) {
			result.WriteRune(r)
		}
	}
	
	return result.String()
}
