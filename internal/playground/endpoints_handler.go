// Package playground provides endpoint discovery functionality.
//
// This file implements the /playground/endpoints endpoint that returns a list
// of all available API endpoints with their methods, parameters, and descriptions.
// This is useful for API exploration and documentation purposes.
package playground

import (
	"net/http"
	"sort"
	"strings"
)

// EndpointInfo represents information about an API endpoint
type EndpointInfo struct {
	Method          string                 `json:"method"`
	Path            string                 `json:"path"`
	Summary         string                 `json:"summary"`
	Description     string                 `json:"description"`
	Parameters      []ParameterInfo        `json:"parameters"`
	HasBody         bool                   `json:"has_body"`
	RequestBodySchema interface{}          `json:"request_body_schema,omitempty"`
	Tags            []string               `json:"tags"`
	Security        []string               `json:"security,omitempty"` // Auth methods required: BearerToken, OAuth2UserToken, UserToken
}

// ParameterInfo represents a parameter for an endpoint
type ParameterInfo struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path, query, header
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
}

// HandleEndpointsList returns a list of all available endpoints
func HandleEndpointsList(spec *OpenAPISpec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if spec == nil {
			WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
				"endpoints": []EndpointInfo{},
				"note":      "OpenAPI spec not loaded",
			})
			return
		}

		var endpoints []EndpointInfo

		// Extract all endpoints from the spec
		for path, pathItem := range spec.Paths {
			// Check each HTTP method
			methods := []struct {
				method string
				op     *Operation
			}{
				{"GET", pathItem.Get},
				{"POST", pathItem.Post},
				{"PUT", pathItem.Put},
				{"DELETE", pathItem.Delete},
				{"PATCH", pathItem.Patch},
			}

			for _, m := range methods {
				if m.op != nil {
					// Skip activity and webhook endpoints (not ready to support yet)
					// Skip deprecated insights endpoints
					pathLower := strings.ToLower(path)
					if strings.Contains(pathLower, "/activity") || 
					   strings.Contains(pathLower, "/webhook") || 
					   strings.Contains(pathLower, "account_activity") ||
					   path == "/2/insights/28hr" ||
					   path == "/2/insights/historical" {
						continue
					}
					
					// Extract parameters
					params := make([]ParameterInfo, 0)
					for _, param := range m.op.Parameters {
						if param.In == "query" || param.In == "path" {
							paramType := "string"
							if param.Schema != nil {
								if t, ok := param.Schema["type"].(string); ok {
									paramType = t
								}
							}
							params = append(params, ParameterInfo{
								Name:        param.Name,
								In:          param.In,
								Description: param.Description,
								Required:    param.Required,
								Type:        paramType,
							})
						}
					}

					// Check if request body exists and has properties (not just empty schema)
					hasBody := false
					if m.op.RequestBody != nil {
						schema := m.op.GetRequestBodySchema()
						if schema != nil {
							// Resolve $ref if present to check the actual schema
							resolvedSchema := schema
							if ref, ok := schema["$ref"].(string); ok {
								if resolved := spec.ResolveRef(ref); resolved != nil {
									resolvedSchema = resolved
								}
							}
							
							// Check if resolved schema has properties
							if properties, ok := resolvedSchema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
								hasBody = true
							}
							// Also check if there are required fields in the schema
							if requiredFields, ok := resolvedSchema["required"].([]interface{}); ok && len(requiredFields) > 0 {
								hasBody = true
							}
							// Check for allOf/oneOf/anyOf that might contain properties
							if allOf, ok := resolvedSchema["allOf"].([]interface{}); ok {
								for _, item := range allOf {
									if itemMap, ok := item.(map[string]interface{}); ok {
										if props, ok := itemMap["properties"].(map[string]interface{}); ok && len(props) > 0 {
											hasBody = true
											break
										}
									}
								}
							}
						}
					}
					
					// Generate request body example if available
					var requestBodyExample interface{}
					if hasBody {
						// Special handling for specific endpoints to provide simpler, more useful examples
						if path == "/2/tweets/search/stream/rules" && m.method == "POST" {
							requestBodyExample = map[string]interface{}{
								"add": []map[string]interface{}{
									{
										"value": "has:hashtags #AI",
										"tag":   "ai-tweets",
									},
								},
							}
						} else if path == "/2/tweets" && m.method == "POST" {
							// Simplify POST /2/tweets to just show text field
							requestBodyExample = map[string]interface{}{
								"text": "Hello world!",
							}
						} else if path == "/2/lists" && m.method == "POST" {
							// Provide list-appropriate placeholder values for POST /2/lists
							requestBodyExample = map[string]interface{}{
								"name":        "My List",
								"description": "A sample list description",
								"private":     false,
							}
						} else if strings.HasPrefix(path, "/2/lists/") && m.method == "PUT" {
							// Provide list-appropriate placeholder values for PUT /2/lists/{id}
							requestBodyExample = map[string]interface{}{
								"name":        "My List",
								"description": "A sample list description",
								"private":     false,
							}
						} else {
							schema := m.op.GetRequestBodySchema()
							if schema != nil {
								requestBodyExample = GenerateMockResponse(schema, spec)
							}
						}
					}

					// Extract security requirements
					securityMethods := make([]string, 0)
					if m.op.Security != nil && len(m.op.Security) > 0 {
						securitySet := make(map[string]bool)
						for _, securityReq := range m.op.Security {
							for schemeName := range securityReq {
								if !securitySet[schemeName] {
									securityMethods = append(securityMethods, schemeName)
									securitySet[schemeName] = true
								}
							}
						}
					}

					endpoints = append(endpoints, EndpointInfo{
						Method:          m.method,
						Path:            path,
						Summary:         m.op.Summary,
						Description:     m.op.Description,
						Parameters:      params,
						HasBody:         hasBody,
						RequestBodySchema: requestBodyExample,
						Tags:            m.op.Tags,
						Security:        securityMethods,
					})
				}
			}
		}

		// Sort by path, then by method
		sort.Slice(endpoints, func(i, j int) bool {
			if endpoints[i].Path != endpoints[j].Path {
				return endpoints[i].Path < endpoints[j].Path
			}
			return endpoints[i].Method < endpoints[j].Method
		})

		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"endpoints": endpoints,
			"total":     len(endpoints),
		})
	}
}

// GetEndpointByPathAndMethod finds an endpoint by path and method
func GetEndpointByPathAndMethod(spec *OpenAPISpec, path, method string) *EndpointInfo {
	if spec == nil {
		return nil
	}

	pathItem, exists := spec.Paths[path]
	if !exists {
		return nil
	}

	var op *Operation
	switch strings.ToUpper(method) {
	case "GET":
		op = pathItem.Get
	case "POST":
		op = pathItem.Post
	case "PUT":
		op = pathItem.Put
	case "DELETE":
		op = pathItem.Delete
	case "PATCH":
		op = pathItem.Patch
	default:
		return nil
	}

	if op == nil {
		return nil
	}

	params := make([]ParameterInfo, 0)
	for _, param := range op.Parameters {
		if param.In == "query" || param.In == "path" {
			paramType := "string"
			if param.Schema != nil {
				if t, ok := param.Schema["type"].(string); ok {
					paramType = t
				}
			}
			params = append(params, ParameterInfo{
				Name:        param.Name,
				In:          param.In,
				Description: param.Description,
				Required:    param.Required,
				Type:        paramType,
			})
		}
	}

	// Check if request body exists and has properties (not just empty schema)
	hasBody := false
	if op.RequestBody != nil {
		schema := op.GetRequestBodySchema()
		if schema != nil {
			// Resolve $ref if present to check the actual schema
			resolvedSchema := schema
			if ref, ok := schema["$ref"].(string); ok {
				if resolved := spec.ResolveRef(ref); resolved != nil {
					resolvedSchema = resolved
				}
			}
			
			// Check if resolved schema has properties
			if properties, ok := resolvedSchema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
				hasBody = true
			}
			// Also check if there are required fields in the schema
			if requiredFields, ok := resolvedSchema["required"].([]interface{}); ok && len(requiredFields) > 0 {
				hasBody = true
			}
			// Check for allOf/oneOf/anyOf that might contain properties
			if allOf, ok := resolvedSchema["allOf"].([]interface{}); ok {
				for _, item := range allOf {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if props, ok := itemMap["properties"].(map[string]interface{}); ok && len(props) > 0 {
							hasBody = true
							break
						}
					}
				}
			}
		}
	}
	
	var requestBodyExample interface{}
	if hasBody {
		// Special handling for specific endpoints to provide simpler, more useful examples
		if path == "/2/tweets/search/stream/rules" && method == "POST" {
			requestBodyExample = map[string]interface{}{
				"add": []map[string]interface{}{
					{
						"value": "has:hashtags #AI",
						"tag":   "ai-tweets",
					},
				},
			}
		} else if path == "/2/tweets" && method == "POST" {
			// Simplify POST /2/tweets to just show text field
			requestBodyExample = map[string]interface{}{
				"text": "Hello world!",
			}
		} else if path == "/2/lists" && method == "POST" {
			// Provide list-appropriate placeholder values for POST /2/lists
			requestBodyExample = map[string]interface{}{
				"name":        "My List",
				"description": "A sample list description",
				"private":     false,
			}
		} else if strings.HasPrefix(path, "/2/lists/") && method == "PUT" {
			// Provide list-appropriate placeholder values for PUT /2/lists/{id}
			requestBodyExample = map[string]interface{}{
				"name":        "My List",
				"description": "A sample list description",
				"private":     false,
			}
		} else {
			schema := op.GetRequestBodySchema()
			if schema != nil {
				requestBodyExample = GenerateMockResponse(schema, spec)
			}
		}
	}

	// Extract security requirements
	securityMethods := make([]string, 0)
	if op.Security != nil && len(op.Security) > 0 {
		securitySet := make(map[string]bool)
		for _, securityReq := range op.Security {
			for schemeName := range securityReq {
				if !securitySet[schemeName] {
					securityMethods = append(securityMethods, schemeName)
					securitySet[schemeName] = true
				}
			}
		}
	}

	return &EndpointInfo{
		Method:          method,
		Path:            path,
		Summary:         op.Summary,
		Description:     op.Description,
		Parameters:      params,
		HasBody:         hasBody,
		RequestBodySchema: requestBodyExample,
		Tags:            op.Tags,
		Security:        securityMethods,
	}
}
