// Package playground handles loading and parsing OpenAPI specifications.
//
// This file manages fetching the OpenAPI spec from X API, caching it locally,
// and providing access to schema information for validation and response generation.
// It includes functions to resolve schema references, extract parameter definitions,
// and access response schemas for any endpoint.
package playground

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	openAPISpecURL = "https://api.x.com/2/openapi.json"
	cacheFileName  = ".playground-openapi-cache.json"
	cacheMaxAge    = 24 * time.Hour
)

// OpenAPISpec represents the OpenAPI specification.
// Contains all paths, operations, components, and schemas from the X API specification.
type OpenAPISpec struct {
	OpenAPI    string                 `json:"openapi"`
	Info       map[string]interface{} `json:"info"`
	Paths      map[string]PathItem    `json:"paths"`
	Components map[string]interface{} `json:"components"`
}

// PathItem represents an OpenAPI path item.
// Contains operations (GET, POST, etc.) and path-level parameters.
type PathItem struct {
	Parameters []Parameter  `json:"parameters,omitempty"` // Parameters defined at path level (apply to all operations)
	Get        *Operation   `json:"get,omitempty"`
	Post       *Operation   `json:"post,omitempty"`
	Put        *Operation   `json:"put,omitempty"`
	Patch      *Operation   `json:"patch,omitempty"`
	Delete     *Operation   `json:"delete,omitempty"`
}

// Operation represents an OpenAPI operation.
// Contains request/response schemas, parameters, and security requirements.
type Operation struct {
	OperationID string                 `json:"operationId"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Parameters  []Parameter            `json:"parameters,omitempty"`
	RequestBody *RequestBody           `json:"requestBody,omitempty"`
	Responses   map[string]Response    `json:"responses"`
	Security    []map[string]interface{} `json:"security,omitempty"` // Security requirements
}

// Parameter represents an OpenAPI parameter.
// Can be a path, query, header, or cookie parameter.
type Parameter struct {
	Ref         string                 `json:"$ref,omitempty"` // Reference to shared parameter definition
	Name        string                 `json:"name"`
	In          string                 `json:"in"` // path, query, header, cookie
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Schema      map[string]interface{} `json:"schema,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for Parameter.
// This handles the case where a parameter is just a $ref reference.
func (p *Parameter) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as a map to check for $ref
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to unmarshal parameter as map: %w", err)
	}
	
	// If it's just a $ref (most common case for shared parameters), capture it and return
	if ref, ok := raw["$ref"].(string); ok {
		if len(raw) == 1 {
			// This is a $ref-only parameter - just store the ref
			p.Ref = ref
			return nil
		}
		// $ref mixed with other fields - store ref but continue unmarshaling
		p.Ref = ref
	}
	
	// Unmarshal normally into the struct (handles all other fields)
	// Use a temporary type to avoid infinite recursion
	type parameterAlias Parameter
	var alias parameterAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("failed to unmarshal parameter struct: %w", err)
	}
	
	*p = Parameter(alias)
	
	// Ensure $ref is set if it was in the raw data
	if ref, ok := raw["$ref"].(string); ok {
		p.Ref = ref
	}
	
	return nil
}

// RequestBody represents an OpenAPI request body.
type RequestBody struct {
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Content     map[string]interface{} `json:"content"`
}

// Response represents an OpenAPI response.
type Response struct {
	Description string                 `json:"description"`
	Content     map[string]interface{} `json:"content,omitempty"`
}

// LoadOpenAPISpec loads the OpenAPI spec from URL or cache.
func LoadOpenAPISpec() (*OpenAPISpec, error) {
	return LoadOpenAPISpecWithRefresh(false)
}

// LoadOpenAPISpecWithRefresh loads the OpenAPI spec, optionally forcing a refresh.
// If forceRefresh is true, ignores cache and fetches fresh spec from URL.
func LoadOpenAPISpecWithRefresh(forceRefresh bool) (*OpenAPISpec, error) {
	// If not forcing refresh, try to load from cache first
	if !forceRefresh {
		if spec := loadFromCache(); spec != nil {
			// Log cache info
			if cacheInfo := getCacheInfo(); cacheInfo != nil {
				age := time.Since(cacheInfo.ModTime)
				log.Printf("Using cached OpenAPI spec (age: %s, location: %s)", formatDuration(age), cacheInfo.Path)
			}
			return spec, nil
		}
		log.Printf("No valid cache found, fetching from URL")
	} else {
		// Force refresh - clear cache first
		log.Printf("Clearing cache and forcing refresh of OpenAPI spec")
		ClearCache()
	}

	// Fetch from URL
	log.Printf("Fetching OpenAPI spec from URL (timeout: 10s)")
	spec, err := fetchFromURL()
	if err != nil {
		log.Printf("Failed to fetch OpenAPI spec: %v", err)
		return nil, fmt.Errorf("failed to fetch OpenAPI spec: %w", err)
	}
	log.Printf("Successfully fetched OpenAPI spec from URL")

	// Save to cache
	if err := saveToCache(spec); err != nil {
		// Log but don't fail if cache save fails
		log.Printf("Warning: failed to cache OpenAPI spec: %v", err)
	} else {
		log.Printf("Cached OpenAPI spec to %s", getCachePath())
	}

	return spec, nil
}

// CacheInfo contains information about the cached OpenAPI spec
type CacheInfo struct {
	Path    string
	ModTime time.Time
	Age     time.Duration
	Exists  bool
}

// GetCacheInfo returns information about the cache file (exported for CLI)
func GetCacheInfo() *CacheInfo {
	return getCacheInfo()
}

// getCacheInfo returns information about the cache file
func getCacheInfo() *CacheInfo {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	cachePath := filepath.Join(homeDir, cacheFileName)
	info, err := os.Stat(cachePath)
	if err != nil {
		return &CacheInfo{
			Path:   cachePath,
			Exists:  false,
		}
	}

	return &CacheInfo{
		Path:    cachePath,
		ModTime:  info.ModTime(),
		Age:     time.Since(info.ModTime()),
		Exists:  true,
	}
}

// getCachePath returns the path to the cache file
func getCachePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, cacheFileName)
}

// ClearCache removes the cached OpenAPI spec
func ClearCache() error {
	cachePath := getCachePath()
	if cachePath == "" {
		return fmt.Errorf("could not determine cache path")
	}

	if err := os.Remove(cachePath); err != nil {
		if os.IsNotExist(err) {
			// Cache doesn't exist, that's fine
			return nil
		}
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	return nil
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
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

// fetchFromURL fetches the OpenAPI spec from the URL
func fetchFromURL() (*OpenAPISpec, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(openAPISpecURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAPI spec from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenAPI spec response body: %w", err)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	return &spec, nil
}

// loadFromCache loads the OpenAPI spec from cache if it exists and is fresh
func loadFromCache() *OpenAPISpec {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	cachePath := filepath.Join(homeDir, cacheFileName)

	// Check if cache file exists
	info, err := os.Stat(cachePath)
	if err != nil {
		return nil
	}

	// Check if cache is still fresh
	if time.Since(info.ModTime()) > cacheMaxAge {
		return nil
	}

	// Read and parse cache
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil
	}

	// If Components is nil, the cache might be from an old version - invalidate it
	if spec.Components == nil {
		if SchemaDebug {
			log.Printf("DEBUG: Cache has no Components, invalidating cache")
		}
		os.Remove(cachePath)
		return nil
	}

	return &spec
}

// saveToCache saves the OpenAPI spec to cache
func saveToCache(spec *OpenAPISpec) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory for cache: %w", err)
	}

	cachePath := filepath.Join(homeDir, cacheFileName)

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal OpenAPI spec for cache: %w", err)
	}

	return os.WriteFile(cachePath, data, 0644)
}

// GetEndpointOperations returns all operations for a given path pattern
func (s *OpenAPISpec) GetEndpointOperations(path string) []EndpointOperation {
	var operations []EndpointOperation
	var patternMatches []EndpointOperation

	// Try exact match first - prioritize exact paths over parameterized ones
	if pathItem, exists := s.Paths[path]; exists {
		operations = append(operations, extractOperations(path, pathItem)...)
		return operations
	}

	// Try pattern matching for parameterized paths
	// Collect all matches, but prioritize exact matches (already returned above)
	// CRITICAL: Filter out bad pattern matches (e.g., /2/users/{username} matching /2/users/personalized_trends)
	pathParts := strings.Split(path, "/")
	lastSegment := ""
	if len(pathParts) > 0 {
		lastSegment = pathParts[len(pathParts)-1]
	}
	hasUnderscoreInLastSegment := strings.Contains(lastSegment, "_")
	
	for specPath, pathItem := range s.Paths {
		if matchesPath(specPath, path) {
			// Filter out bad matches: if last segment has underscore and spec path has parameter in last position
			if hasUnderscoreInLastSegment {
				specParts := strings.Split(specPath, "/")
				if len(specParts) == len(pathParts) && len(specParts) > 0 {
					specLastPart := specParts[len(specParts)-1]
					if strings.HasPrefix(specLastPart, "{") && strings.HasSuffix(specLastPart, "}") {
						// Skip this pattern match - literal endpoint with underscore shouldn't match parameterized path
						continue
					}
				}
			}
			patternMatches = append(patternMatches, extractOperations(specPath, pathItem)...)
		}
	}

	// Return pattern matches only if no exact match was found
	return patternMatches
}

// EndpointOperation represents an operation for a specific endpoint
type EndpointOperation struct {
	Path      string
	Method    string
	Operation *Operation
}

// extractOperations extracts all operations from a path item
func extractOperations(path string, pathItem PathItem) []EndpointOperation {
	var operations []EndpointOperation

	if pathItem.Get != nil {
		operations = append(operations, EndpointOperation{
			Path:      path,
			Method:    "GET",
			Operation: pathItem.Get,
		})
	}
	if pathItem.Post != nil {
		operations = append(operations, EndpointOperation{
			Path:      path,
			Method:    "POST",
			Operation: pathItem.Post,
		})
	}
	if pathItem.Put != nil {
		operations = append(operations, EndpointOperation{
			Path:      path,
			Method:    "PUT",
			Operation: pathItem.Put,
		})
	}
	if pathItem.Patch != nil {
		operations = append(operations, EndpointOperation{
			Path:      path,
			Method:    "PATCH",
			Operation: pathItem.Patch,
		})
	}
	if pathItem.Delete != nil {
		operations = append(operations, EndpointOperation{
			Path:      path,
			Method:    "DELETE",
			Operation: pathItem.Delete,
		})
	}

	return operations
}

// matchesPath checks if a request path matches an OpenAPI path pattern
func matchesPath(specPath, requestPath string) bool {
	// Remove query parameters
	requestPath = strings.Split(requestPath, "?")[0]

	specParts := strings.Split(specPath, "/")
	requestParts := strings.Split(requestPath, "/")

	if len(specParts) != len(requestParts) {
		return false
	}

	for i := 0; i < len(specParts); i++ {
		specPart := specParts[i]
		requestPart := requestParts[i]

		// If spec part is a parameter (starts with { and ends with }), it matches anything
		if strings.HasPrefix(specPart, "{") && strings.HasSuffix(specPart, "}") {
			continue
		}

		// Otherwise, parts must match exactly
		if specPart != requestPart {
			return false
		}
	}

	return true
}

// GetResponseSchema extracts the response schema from an operation
func (op *Operation) GetResponseSchema(statusCode string) map[string]interface{} {
	response, exists := op.Responses[statusCode]
	if !exists {
		// Try default response
		response, exists = op.Responses["default"]
		if !exists {
			return nil
		}
	}

	// Try to get application/json content
	if content, ok := response.Content["application/json"].(map[string]interface{}); ok {
		if schema, ok := content["schema"].(map[string]interface{}); ok {
			return schema
		}
	}

	return nil
}

// GetResponseContentType returns the content type for a response status code
// Returns the first content type found (prefers application/json, then text/event-stream)
func (op *Operation) GetResponseContentType(statusCode string) string {
	response, exists := op.Responses[statusCode]
	if !exists {
		response, exists = op.Responses["default"]
		if !exists {
			return "application/json" // Default
		}
	}

	if response.Content == nil {
		return "application/json"
	}

	// Prefer application/json
	if content, ok := response.Content["application/json"]; ok && content != nil {
		return "application/json"
	}

	// Check for text/event-stream (streaming)
	if content, ok := response.Content["text/event-stream"]; ok && content != nil {
		return "text/event-stream"
	}

	// Return first available content type
	for contentType := range response.Content {
		return contentType
	}

	return "application/json"
}

// IsStreamingEndpoint checks if an operation returns a streaming response
func (op *Operation) IsStreamingEndpoint() bool {
	// Check all response status codes
	for statusCode := range op.Responses {
		if op.GetResponseContentType(statusCode) == "text/event-stream" {
			return true
		}
	}
	return false
}

// GetResponseExample extracts an example from the response schema
func (op *Operation) GetResponseExample(statusCode string) interface{} {
	response, exists := op.Responses[statusCode]
	if !exists {
		response, exists = op.Responses["default"]
		if !exists {
			return nil
		}
	}

	// Try to get application/json content
	if content, ok := response.Content["application/json"].(map[string]interface{}); ok {
		// Check for example
		if example, ok := content["example"]; ok {
			return example
		}
		// Check for examples (plural)
		if examples, ok := content["examples"].(map[string]interface{}); ok {
			// Return first example
			for _, ex := range examples {
				if exMap, ok := ex.(map[string]interface{}); ok {
					if value, ok := exMap["value"]; ok {
						return value
					}
				}
			}
		}
	}

	return nil
}

// GetRequestBodySchema extracts the request body schema from an operation
func (op *Operation) GetRequestBodySchema() map[string]interface{} {
	if op.RequestBody == nil {
		return nil
	}

	// Try to get application/json content
	if content, ok := op.RequestBody.Content["application/json"].(map[string]interface{}); ok {
		if schema, ok := content["schema"].(map[string]interface{}); ok {
			return schema
		}
	}

	return nil
}

// HasRequestBody checks if the operation requires a request body
func (op *Operation) HasRequestBody() bool {
	return op.RequestBody != nil && op.RequestBody.Required
}

// GetPathParameters returns all path parameters for an operation
func (op *Operation) GetPathParameters() []Parameter {
	var pathParams []Parameter
	for _, param := range op.Parameters {
		if param.In == "path" {
			pathParams = append(pathParams, param)
		}
	}
	return pathParams
}

// resolveParameter resolves a parameter, handling $ref references
func (op *Operation) resolveParameter(param Parameter, spec *OpenAPISpec) Parameter {
	// If parameter has $ref, resolve it
	if param.Ref != "" && spec != nil {
		resolved := spec.ResolveParameterRef(param.Ref)
		if resolved != nil {
			return *resolved
		}
	}
	return param
}

// GetQueryParameters returns all query parameters for an operation
func (op *Operation) GetQueryParameters() []Parameter {
	return op.GetQueryParametersWithSpec(nil)
}

// GetQueryParametersWithSpec returns all query parameters for an operation, resolving $ref references
func (op *Operation) GetQueryParametersWithSpec(spec *OpenAPISpec) []Parameter {
	var queryParams []Parameter
	for _, param := range op.Parameters {
		// Resolve $ref if present
		resolvedParam := param
		if spec != nil {
			resolvedParam = op.resolveParameter(param, spec)
		}
		if resolvedParam.In == "query" {
			queryParams = append(queryParams, resolvedParam)
		}
	}
	return queryParams
}

// GetQueryParametersWithPathParams returns all query parameters for an operation,
// including parameters defined at the path level, resolving $ref references
func (op *Operation) GetQueryParametersWithPathParams(pathItem *PathItem, spec *OpenAPISpec) []Parameter {
	var queryParams []Parameter
	
	// First, add path-level parameters (these apply to all operations)
	if pathItem != nil {
		for _, param := range pathItem.Parameters {
			// Resolve $ref if present
			resolvedParam := param
			if spec != nil {
				if param.Ref != "" {
					resolved := spec.ResolveParameterRef(param.Ref)
					if resolved != nil {
						resolvedParam = *resolved
					}
				}
			}
			if resolvedParam.In == "query" {
				queryParams = append(queryParams, resolvedParam)
			}
		}
	}
	
	// Then, add operation-level parameters (these override path-level ones with same name)
	for _, param := range op.Parameters {
		// Resolve $ref if present
		resolvedParam := op.resolveParameter(param, spec)
		if resolvedParam.In == "query" {
			// Check if this parameter already exists (from path level)
			found := false
			for i, existingParam := range queryParams {
				if existingParam.Name == resolvedParam.Name {
					// Override with operation-level parameter
					queryParams[i] = resolvedParam
					found = true
					break
				}
			}
			if !found {
				queryParams = append(queryParams, resolvedParam)
			}
		}
	}
	
	return queryParams
}

// GetRequiredParameters returns all required parameters (path, query, header)
func (op *Operation) GetRequiredParameters() []Parameter {
	var required []Parameter
	for _, param := range op.Parameters {
		if param.Required {
			required = append(required, param)
		}
	}
	return required
}

// ResolveRef resolves a $ref reference to the actual schema
func (s *OpenAPISpec) ResolveRef(ref string) map[string]interface{} {
	// $ref format: #/components/schemas/ListCreateResponse
	if !strings.HasPrefix(ref, "#/components/schemas/") {
		if SchemaDebug {
			log.Printf("DEBUG: Ref doesn't match pattern: %s", ref)
		}
		return nil
	}

	schemaName := strings.TrimPrefix(ref, "#/components/schemas/")
	
	if s.Components == nil {
		if SchemaDebug {
			log.Printf("DEBUG: Components is nil")
		}
		return nil
	}

	schemas, ok := s.Components["schemas"].(map[string]interface{})
	if !ok {
		if SchemaDebug {
			log.Printf("DEBUG: Could not get schemas from components")
		}
		return nil
	}

	schema, ok := schemas[schemaName].(map[string]interface{})
	if !ok {
		if SchemaDebug {
			log.Printf("DEBUG: Schema '%s' not found in schemas", schemaName)
			// List available schemas for debugging
			if strings.Contains(ref, "ListCreateResponse") {
				log.Printf("DEBUG: Available schemas (first 20):")
				count := 0
				for name := range schemas {
					if count < 20 {
						log.Printf("DEBUG:   - %s", name)
						count++
					}
				}
			}
		}
		return nil
	}

	// Always log ListCreateResponse resolution
	if SchemaDebug && strings.Contains(ref, "ListCreateResponse") {
		schemaJSON, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			log.Printf("DEBUG: [ListCreateResponse] Error marshaling schema for %s: %v", ref, err)
		} else {
			log.Printf("DEBUG: [ListCreateResponse] Successfully resolved %s:\n%s", ref, string(schemaJSON))
		}
	} else if SchemaDebug {
		// Only log other schemas if they're not too verbose
		// (reduce noise by not logging Problem, ListId repeatedly)
		if !strings.Contains(ref, "Problem") && !strings.Contains(ref, "ListId") {
			schemaJSON, err := json.MarshalIndent(schema, "", "  ")
			if err != nil {
				log.Printf("DEBUG: Error marshaling schema for %s: %v", ref, err)
			} else {
				log.Printf("DEBUG: Resolved schema for %s:\n%s", ref, string(schemaJSON))
			}
		}
	}

	// Return a copy to avoid modifying the original
	result := make(map[string]interface{})
	for k, v := range schema {
		result[k] = v
	}

	return result
}

// ResolveParameterRef resolves a $ref reference to a shared parameter definition
func (s *OpenAPISpec) ResolveParameterRef(ref string) *Parameter {
	// $ref format: #/components/parameters/TweetFieldsParameter
	if !strings.HasPrefix(ref, "#/components/parameters/") {
		return nil
	}

	paramName := strings.TrimPrefix(ref, "#/components/parameters/")
	
	if s.Components == nil {
		return nil
	}

	parameters, ok := s.Components["parameters"].(map[string]interface{})
	if !ok {
		return nil
	}

	paramData, ok := parameters[paramName].(map[string]interface{})
	if !ok {
		return nil
	}

	// Convert map to JSON and unmarshal into Parameter struct
	// This ensures our custom UnmarshalJSON is used
	paramJSON, err := json.Marshal(paramData)
	if err != nil {
		return nil
	}

	var param Parameter
	if err := json.Unmarshal(paramJSON, &param); err != nil {
		return nil
	}

	return &param
}

