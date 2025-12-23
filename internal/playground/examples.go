// Package playground manages example API responses.
//
// This file loads example responses from JSON files (embedded or user-provided)
// and provides lookup by method and endpoint. Examples are used to provide
// realistic responses when available, falling back to schema-based generation
// when examples are not found.
package playground

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//go:embed examples/*.json
var embeddedExamples embed.FS

// ExampleResponse represents a template response from a real API call.
// Used to provide realistic example responses for endpoints.
type ExampleResponse struct {
	Endpoint string                 `json:"endpoint"` // e.g., "/2/users/me"
	Method   string                 `json:"method"`  // e.g., "GET"
	Response map[string]interface{}  `json:"response"`
	Fields   map[string][]string     `json:"fields"` // Maps field type to requested fields, e.g., {"user.fields": ["id", "name", "username"]}
}

// ExampleStore manages example responses.
// Provides lookup by method and endpoint for realistic API responses.
type ExampleStore struct {
	examples map[string]*ExampleResponse // Key: "METHOD /endpoint"
}

// NewExampleStore creates a new example store.
// Returns an empty store ready to load examples from files or embedded resources.
func NewExampleStore() *ExampleStore {
	return &ExampleStore{
		examples: make(map[string]*ExampleResponse),
	}
}

// LoadExamplesFromFile loads example responses from a JSON file.
// Parses the JSON and adds all examples to the store.
func (s *ExampleStore) LoadExamplesFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read examples file %s: %w", filePath, err)
	}

	var examples []ExampleResponse
	if err := json.Unmarshal(data, &examples); err != nil {
		return fmt.Errorf("failed to parse examples file %s: %w", filePath, err)
	}

	for _, example := range examples {
		key := s.makeKey(example.Method, example.Endpoint)
		s.examples[key] = &example
	}

	return nil
}

// LoadExamplesFromDir loads all JSON files from a directory.
// Logs warnings for individual file errors but continues processing other files.
func (s *ExampleStore) LoadExamplesFromDir(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read examples directory %s: %w", dirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		if err := s.LoadExamplesFromFile(filePath); err != nil {
			// Log but don't fail on individual file errors
			log.Printf("Warning: Failed to load example from %s: %v", filePath, err)
		}
	}

	return nil
}

// LoadEmbeddedExamples loads examples embedded in the binary.
// Logs warnings for individual file errors but continues processing other files.
func (s *ExampleStore) LoadEmbeddedExamples() error {
	entries, err := embeddedExamples.ReadDir("examples")
	if err != nil {
		return fmt.Errorf("failed to read embedded examples directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := "examples/" + entry.Name()
		data, err := embeddedExamples.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: Failed to read embedded example %s: %v", filePath, err)
			continue
		}

		var examples []ExampleResponse
		if err := json.Unmarshal(data, &examples); err != nil {
			log.Printf("Warning: Failed to parse embedded example %s: %v", filePath, err)
			continue
		}

		for _, example := range examples {
			key := s.makeKey(example.Method, example.Endpoint)
			// Only add if not already present (user examples take precedence)
			if s.examples[key] == nil {
				s.examples[key] = &example
			}
		}
	}

	return nil
}

// GetExample retrieves an example response for an endpoint.
// Returns nil if no example is found for the given method and endpoint.
func (s *ExampleStore) GetExample(method, endpoint string) *ExampleResponse {
	key := s.makeKey(method, endpoint)
	return s.examples[key]
}

// AddExample adds an example response.
// If an example already exists for the same method and endpoint, it is replaced.
func (s *ExampleStore) AddExample(example *ExampleResponse) {
	key := s.makeKey(example.Method, example.Endpoint)
	s.examples[key] = example
}

// makeKey creates a key from method and endpoint.
// Format: "METHOD /endpoint" (e.g., "GET /2/users/me").
func (s *ExampleStore) makeKey(method, endpoint string) string {
	return strings.ToUpper(method) + " " + endpoint
}

// FindBestMatch finds the best matching example for an endpoint
// (handles path parameters like /2/users/{id})
func (s *ExampleStore) FindBestMatch(method, endpoint string) *ExampleResponse {
	// Try exact match first
	if example := s.GetExample(method, endpoint); example != nil {
		return example
	}

	// Try to match by endpoint pattern (e.g., /2/users/{id} matches /2/users/123)
	for key, example := range s.examples {
		if strings.HasPrefix(key, method+" ") {
			examplePath := strings.TrimPrefix(key, method+" ")
			if matchesEndpointPattern(examplePath, endpoint) {
				return example
			}
		}
	}

	return nil
}

// matchesEndpointPattern checks if an endpoint pattern matches a path
func matchesEndpointPattern(pattern, path string) bool {
	patternParts := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	pathParts := strings.Split(strings.TrimPrefix(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return false
	}

	for i, patternPart := range patternParts {
		pathPart := pathParts[i]
		// If pattern part is {param}, it matches anything
		if strings.HasPrefix(patternPart, "{") && strings.HasSuffix(patternPart, "}") {
			continue
		}
		if patternPart != pathPart {
			return false
		}
	}

	return true
}

