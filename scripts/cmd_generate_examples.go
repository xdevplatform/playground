//go:build ignore
// +build ignore

// Package main provides a standalone tool to generate example responses.
//
// This is a development tool (not compiled into the playground binary) that
// generates example JSON response files from the OpenAPI specification.
// Run with: go run scripts/cmd_generate_examples.go
package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/xdevplatform/playground/internal/playground"
)

// This is a standalone tool to generate example responses from the OpenAPI spec.
// Run with: go run scripts/cmd_generate_examples.go
func main() {
	spec, err := playground.LoadOpenAPISpec()
	if err != nil {
		log.Fatalf("Failed to load OpenAPI spec: %v", err)
	}

	fmt.Printf("✅ Loaded OpenAPI spec (version: %v)\n", spec.Info["version"])

	outputDir := filepath.Join("examples")

	if err := playground.GenerateAllExamples(spec, outputDir); err != nil {
		log.Fatalf("Failed to generate examples: %v", err)
	}

	fmt.Printf("\n✅ Successfully generated example responses in %s/\n", outputDir)
	fmt.Println("💡 Review and update the generated examples with real API response data as needed.")
}

