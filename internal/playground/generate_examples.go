// Package playground generates example response files from OpenAPI specifications.
//
// This file provides functions to automatically generate example JSON response
// files for all endpoints in the OpenAPI spec. These generated examples can
// then be reviewed and updated with real API response data. Used by the
// cmd_generate_examples.go development tool.
package playground

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// GenerateAllExamples generates example responses for all endpoints in the OpenAPI spec.
// Writes JSON files to the specified output directory, one file per endpoint pattern.
func GenerateAllExamples(spec *OpenAPISpec, outputDir string) error {
	if spec == nil {
		return fmt.Errorf("OpenAPI spec is nil")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	examplesByEndpoint := make(map[string][]ExampleResponse)

	for path, pathItem := range spec.Paths {
		operations := []struct {
			method    string
			operation *Operation
		}{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"PATCH", pathItem.Patch},
			{"DELETE", pathItem.Delete},
		}

		for _, opData := range operations {
			if opData.operation == nil {
				continue
			}

			// Get response schema (prefer 200, fallback to default)
			responseSchema := opData.operation.GetResponseSchema("200")
			if responseSchema == nil {
				responseSchema = opData.operation.GetResponseSchema("201")
			}
			if responseSchema == nil {
				responseSchema = opData.operation.GetResponseSchema("default")
			}

			if responseSchema == nil {
				log.Printf("No response schema found for %s %s", opData.method, path)
				continue
			}

			// Generate example response (pass nil for queryParams to get all fields)
			exampleResponse := GenerateMockResponse(responseSchema, spec)

			// Wrap in data if needed
			var responseData map[string]interface{}
			if respMap, ok := exampleResponse.(map[string]interface{}); ok {
				// Check if it already has a "data" field at top level
				if _, hasData := respMap["data"]; hasData {
					responseData = respMap
				} else {
					// Check if the schema defines a "data" property
					if props, ok := responseSchema["properties"].(map[string]interface{}); ok {
						if _, schemaHasData := props["data"]; schemaHasData {
							// Schema expects data at top level, use as-is
							responseData = respMap
						} else {
							// Wrap in data
							responseData = map[string]interface{}{
								"data": respMap,
							}
						}
					} else {
						// No properties defined, wrap in data
						responseData = map[string]interface{}{
							"data": respMap,
						}
					}
				}
			} else {
				responseData = map[string]interface{}{
					"data": exampleResponse,
				}
			}

			// Infer field types from the endpoint
			fields := inferFieldsFromEndpoint(path, opData.operation)

			example := ExampleResponse{
				Endpoint: path,
				Method:   opData.method,
				Response: responseData,
				Fields:   fields,
			}

			// Group by endpoint pattern (normalize path parameters)
			endpointKey := normalizeEndpointPath(path)
			examplesByEndpoint[endpointKey] = append(examplesByEndpoint[endpointKey], example)
		}
	}

	// Write examples to files
	fileCount := 0
	totalExamples := 0
	for endpointKey, examples := range examplesByEndpoint {
		if len(examples) == 0 {
			continue
		}

		// Generate filename from endpoint
		filename := generateFilename(endpointKey)
		filePath := filepath.Join(outputDir, filename)

		// Convert to JSON
		jsonData, err := json.MarshalIndent(examples, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal examples for %s: %v", endpointKey, err)
			continue
		}

		// Write to file
		if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
			log.Printf("Failed to write %s: %v", filePath, err)
			continue
		}

		log.Printf("Generated %s with %d example(s) for endpoint: %s", filename, len(examples), endpointKey)
		fileCount++
		totalExamples += len(examples)
	}

	log.Printf("Summary: Generated %d files with %d total examples", fileCount, totalExamples)

	return nil
}

// normalizeEndpointPath normalizes a path for grouping examples
// This is used to group examples by endpoint pattern, not for matching
func normalizeEndpointPath(path string) string {
	// Keep the path as-is since OpenAPI spec already uses {id} patterns
	// This function is mainly for grouping, so we can use the path directly
	return path
}

// generateFilename generates a filename from an endpoint path
func generateFilename(endpoint string) string {
	// Remove leading /2/
	endpoint = strings.TrimPrefix(endpoint, "/2/")
	
	// Handle special case for /2/users/me
	if endpoint == "users/me" {
		return "users_me.json"
	}
	
	// Replace path parameters with descriptive names
	endpoint = strings.ReplaceAll(endpoint, "{id}", "id")
	endpoint = strings.ReplaceAll(endpoint, "{username}", "username")
	endpoint = strings.ReplaceAll(endpoint, "{list_id}", "list_id")
	endpoint = strings.ReplaceAll(endpoint, "{tweet_id}", "tweet_id")
	endpoint = strings.ReplaceAll(endpoint, "{user_id}", "user_id")
	endpoint = strings.ReplaceAll(endpoint, "{space_id}", "space_id")
	
	// Replace slashes and special chars with underscores
	endpoint = strings.ReplaceAll(endpoint, "/", "_")
	endpoint = strings.ReplaceAll(endpoint, "-", "_")
	
	// Clean up multiple underscores
	for strings.Contains(endpoint, "__") {
		endpoint = strings.ReplaceAll(endpoint, "__", "_")
	}
	
	// Remove trailing underscores
	endpoint = strings.TrimSuffix(endpoint, "_")
	
	// If empty or too short, use a default
	if endpoint == "" || len(endpoint) < 2 {
		endpoint = "endpoint"
	}
	
	return endpoint + ".json"
}

// inferFieldsFromEndpoint infers field types from endpoint and operation
func inferFieldsFromEndpoint(endpoint string, op *Operation) map[string][]string {
	fields := make(map[string][]string)

	// Infer field types from endpoint path
	if strings.Contains(endpoint, "/users/") {
		fields["user.fields"] = []string{"id", "name", "username", "description", "created_at", "public_metrics"}
	}
	if strings.Contains(endpoint, "/tweets/") || strings.Contains(endpoint, "/tweets") {
		fields["tweet.fields"] = []string{"id", "text", "created_at", "author_id", "public_metrics"}
	}
	if strings.Contains(endpoint, "/lists/") || strings.Contains(endpoint, "/lists") {
		fields["list.fields"] = []string{"id", "name", "description", "created_at", "follower_count", "member_count"}
	}
	if strings.Contains(endpoint, "/spaces/") {
		fields["space.fields"] = []string{"id", "title", "state", "created_at"}
	}
	if strings.Contains(endpoint, "/media/") {
		fields["media.fields"] = []string{"media_key", "type", "url"}
	}

	// Infer from operation ID
	if op != nil && op.OperationID != "" {
		opID := strings.ToLower(op.OperationID)
		if strings.Contains(opID, "user") && fields["user.fields"] == nil {
			fields["user.fields"] = []string{"id", "name", "username", "description", "created_at"}
		}
		if strings.Contains(opID, "tweet") && fields["tweet.fields"] == nil {
			fields["tweet.fields"] = []string{"id", "text", "created_at", "author_id"}
		}
		if strings.Contains(opID, "list") && fields["list.fields"] == nil {
			fields["list.fields"] = []string{"id", "name", "description", "created_at"}
		}
	}

	return fields
}

