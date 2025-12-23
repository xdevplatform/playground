// Package playground provides a local HTTP server that simulates the X (Twitter) API v2
// for testing and development purposes. It runs entirely on the local machine and requires
// no internet connection after initial setup.
//
// Key Features:
//   - Complete API compatibility with X API v2 endpoints
//   - Stateful operations with in-memory state management
//   - Optional file-based state persistence across server restarts
//   - Request validation against OpenAPI specifications
//   - Error responses matching real API formats
//   - Configurable rate limiting simulation
//   - OpenAPI-driven endpoint discovery and handling
//   - Server-Sent Events (SSE) streaming support
//   - CORS support for web applications
//
// Architecture:
//   The playground uses a unified OpenAPI handler that processes all API requests,
//   validates them against the OpenAPI specification, and generates appropriate responses.
//   Stateful operations (creating tweets, following users, etc.) are handled by the State
//   type, which maintains in-memory data structures. The playground can optionally
//   persist state to disk for restoration across restarts.
//
// Usage:
//   Start the playground server:
//     server := playground.NewServer(8080, "localhost")
//     server.Start()
//
//   Make API requests:
//     curl -H "Authorization: Bearer test_token" http://localhost:8080/2/users/me
//
// Conventions:
//   - Exported functions use PascalCase (e.g., GenerateUserResponse)
//   - Internal functions use camelCase (e.g., generateRequestID)
//   - Constants use PascalCase for exported, camelCase for internal
//   - All exported functions and types must have godoc comments
package playground

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Server represents the playground API server.
// It manages HTTP server lifecycle, state, examples, and persistence.
type Server struct {
	httpServer   *http.Server
	state        *State
	examples     *ExampleStore
	persistence  *StatePersistence
	port         int
	host         string
	activeReqs   int64 // Track active requests (atomic)
}

// NewServer creates a new playground server.
// Uses default configuration and does not refresh the OpenAPI cache.
func NewServer(port int, host string) *Server {
	return NewServerWithRefresh(port, host, false)
}

// NewServerWithRefresh creates a new playground server with optional cache refresh.
// If refreshCache is true, forces a refresh of the OpenAPI specification cache.
func NewServerWithRefresh(port int, host string, refreshCache bool) *Server {
	// Load playground configuration
	config, err := LoadPlaygroundConfig()
	if err != nil {
		log.Printf("Warning: Failed to load playground config: %v", err)
		config = nil
	}

	state := NewStateWithConfig(config)
	if state == nil {
		log.Fatal("CRITICAL: Failed to initialize state - this should never happen")
	}
	
	// Initialize persistence if enabled
	var persistence *StatePersistence
	if config != nil {
		persistenceConfig := config.GetPersistenceConfig()
		if persistenceConfig != nil && persistenceConfig.Enabled {
			persistence = NewStatePersistence(state, persistenceConfig)
			if persistence != nil {
				if export, err := LoadStateFromFile(persistenceConfig); err == nil && export != nil {
					log.Printf("Loaded persisted state from %s", persistenceConfig.FilePath)
				} else {
					log.Printf("State persistence enabled: %s (auto-save: %v, interval: %ds)", 
						persistenceConfig.FilePath, persistenceConfig.AutoSave, persistenceConfig.SaveInterval)
				}
			}
		}
	}

	examples := NewExampleStore()
	mux := http.NewServeMux()

	// Load OpenAPI spec
	spec, err := LoadOpenAPISpecWithRefresh(refreshCache)
	if err != nil {
		log.Printf("Warning: Failed to load OpenAPI spec: %v (using hardcoded endpoints only)", err)
		spec = nil
	} else {
		version := "unknown"
		if v, ok := spec.Info["version"]; ok {
			version = fmt.Sprintf("%v", v)
		}
		log.Printf("Loaded OpenAPI spec (version: %s)", version)
	}

	// Load embedded examples
	if err := examples.LoadEmbeddedExamples(); err != nil {
		log.Printf("Warning: Failed to load embedded examples: %v", err)
	}

	// Load user-provided examples (takes precedence)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		examplesDir := filepath.Join(homeDir, ".playground", "examples")
		if err := examples.LoadExamplesFromDir(examplesDir); err == nil {
			log.Printf("Loaded user examples from %s", examplesDir)
		}
	}

	SetGlobalConfig(config)
	
	addr := fmt.Sprintf("%s:%d", host, port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      nil, // Will be set after mux is configured
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disable write timeout to support streaming endpoints
		IdleTimeout:  60 * time.Second,
	}
	
	// Create server instance first (with nil handler, will be set later)
	server := &Server{
		httpServer:  httpServer,
		state:       state,
		examples:    examples,
		persistence: persistence,
		port:        port,
		host:        host,
		activeReqs:  0,
	}
	
	// Setup HTTP handlers
	mux.HandleFunc("/playground", HandleUI)
	mux.HandleFunc("/playground/", HandleUI)
	
	// Add health check endpoint (before other handlers)
	mux.HandleFunc("/health", HandleHealth)
	
	// Add rate limit status endpoint (before other handlers)
	mux.HandleFunc("/rate-limits", HandleRateLimitStatus)
	
	// Add endpoints list endpoint
	mux.HandleFunc("/endpoints", HandleEndpointsList(spec))
	
	// Add configuration management endpoints
	mux.HandleFunc("/config", HandleConfigGet)
	mux.HandleFunc("/config/update", HandleConfigUpdate)
	mux.HandleFunc("/config/save", HandleConfigSave)
	
	// Add state management endpoints
	mux.HandleFunc("/state/reset", HandleStateReset(state, persistence))
	mux.HandleFunc("/state", HandleStateDelete(state, persistence))
	mux.HandleFunc("/state/export", HandleStateExport(state))
	mux.HandleFunc("/state/import", HandleStateImport(state, persistence))
	mux.HandleFunc("/state/save", HandleStateSave(persistence))
	
	// Setup handlers (includes CORS handling in unified handler)
	SetupHandlers(mux, state, spec, examples, server)

	// Set global server instance for config handlers (after server is fully initialized)
	SetGlobalServer(server)

	// Set the mux as the handler now that it's fully configured
	httpServer.Handler = mux

	return server
}

// Start starts the playground server.
// Blocks until the server is stopped. Returns an error if the server fails to start.
func (s *Server) Start() error {
	addr := fmt.Sprintf("http://%s:%d", s.host, s.port)
	log.Printf("Playground server starting on %s", addr)
	log.Printf("Supported endpoints: All X API v2 endpoints from OpenAPI spec")
	log.Printf("Management endpoints: /health, /rate-limits, /config, /state")
	
	if s.persistence != nil {
		log.Printf("State persistence: ENABLED (file: %s, auto-save: %v, interval: %ds)", 
			s.persistence.config.FilePath, s.persistence.config.AutoSave, s.persistence.config.SaveInterval)
	} else {
		log.Printf("State persistence: DISABLED")
	}
	
	log.Printf("Web UI: %s/playground", addr)
	log.Printf("Set API_BASE_URL=%s to use the playground", addr)

	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the playground server.
// Waits for active requests to complete (up to 30 seconds) before shutting down.
// Returns an error if shutdown fails.
func (s *Server) Stop(ctx context.Context) error {
	log.Println("Stopping playground server...")
	
	// Wait for active requests to complete (with timeout)
	// Check active requests every 100ms
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)
	
	forceShutdown := false
	for atomic.LoadInt64(&s.activeReqs) > 0 && time.Now().Before(deadline) && !forceShutdown {
		select {
		case <-ctx.Done():
			log.Printf("Shutdown context cancelled, forcing shutdown")
			forceShutdown = true
		case <-ticker.C:
			active := atomic.LoadInt64(&s.activeReqs)
			if active > 0 {
				log.Printf("Waiting for %d active request(s) to complete...", active)
			}
		}
	}
	
	if atomic.LoadInt64(&s.activeReqs) > 0 {
		log.Printf("Warning: %d active request(s) still in progress, proceeding with shutdown", atomic.LoadInt64(&s.activeReqs))
	}
	
	// Save state if persistence is enabled
	if s.persistence != nil {
		if err := s.persistence.Stop(); err != nil {
			log.Printf("Warning: Failed to save state: %v", err)
		} else {
			log.Printf("State saved successfully")
		}
	}
	
	return s.httpServer.Shutdown(ctx)
}

// GetURL returns the server URL.
// Returns the full URL including protocol, host, and port.
func (s *Server) GetURL() string {
	return fmt.Sprintf("http://%s:%d", s.host, s.port)
}

// GetState returns the server state.
// Provides access to the in-memory state for testing or inspection.
func (s *Server) GetState() *State {
	return s.state
}

