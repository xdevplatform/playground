// Package playground provides HTTP handlers for configuration management.
//
// This file implements the /playground/config/* endpoints for getting, updating,
// and saving playground configuration at runtime. Configuration changes are
// applied immediately but must be saved to persist across server restarts.
package playground

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

var (
	globalConfig     *PlaygroundConfig
	globalConfigLock sync.RWMutex
	globalServer     *Server // Store server instance for config reloading
	globalServerLock sync.RWMutex
)

// SetGlobalServer sets the global server instance (called at server startup)
func SetGlobalServer(server *Server) {
	globalServerLock.Lock()
	defer globalServerLock.Unlock()
	globalServer = server
}

// GetGlobalServer returns the current global server instance
func GetGlobalServer() *Server {
	globalServerLock.RLock()
	defer globalServerLock.RUnlock()
	return globalServer
}

// SetGlobalConfig sets the global configuration (called at server startup)
func SetGlobalConfig(config *PlaygroundConfig) {
	globalConfigLock.Lock()
	defer globalConfigLock.Unlock()
	globalConfig = config
}

// GetGlobalConfig returns the current global configuration
func GetGlobalConfig() *PlaygroundConfig {
	globalConfigLock.RLock()
	defer globalConfigLock.RUnlock()
	return globalConfig
}

// HandleConfigGet returns the current playground configuration
func HandleConfigGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config := GetGlobalConfig()
	if config == nil {
		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"error": "Configuration not available",
		})
		return
	}

	WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
		"config": config,
		"note":   "Runtime configuration changes are not persisted. Edit ~/.playground/config.json and restart server to persist changes.",
	})
}

// HandleConfigUpdate updates the playground configuration at runtime
func HandleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Enforce request size limit
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestSize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Validate request body size
	if len(body) > MaxRequestSize {
		WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Request body too large",
			"detail": fmt.Sprintf("Maximum size is %d bytes", MaxRequestSize),
		})
		return
	}

	var newConfig PlaygroundConfig
	if err := json.Unmarshal(body, &newConfig); err != nil {
		WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid JSON",
			"detail": err.Error(),
		})
		return
	}

	// Validate config values
	if err := validateConfig(&newConfig); err != nil {
		WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid configuration",
			"detail": err.Error(),
		})
		return
	}

	// Update global config
	globalConfigLock.Lock()
	globalConfig = &newConfig
	globalConfigLock.Unlock()

	// Update state.config to point to new config (so handlers see updated config)
	server := GetGlobalServer()
	if server != nil && server.GetState() != nil {
		server.GetState().UpdateConfig(&newConfig)
	}

	// Reload persistence config if server is available
	if server != nil && server.persistence != nil && newConfig.Persistence != nil {
		persistenceConfig := newConfig.GetPersistenceConfig()
		if err := server.persistence.UpdateConfig(persistenceConfig); err != nil {
			log.Printf("Warning: Failed to update persistence config: %v", err)
		}
	}

	WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
		"status": "Configuration updated",
		"config": newConfig,
		"note":   "Changes take effect immediately. Edit ~/.playground/config.json to persist changes.",
	})
}

// HandleConfigSave saves the configuration to the file
func HandleConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var newConfig PlaygroundConfig
	if err := json.Unmarshal(body, &newConfig); err != nil {
		WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid JSON",
			"detail": err.Error(),
		})
		return
	}

	// Save to file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Failed to get home directory",
			"detail": err.Error(),
		})
		return
	}

	configPath := filepath.Join(homeDir, ".playground", "config.json")
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Failed to create config directory",
			"detail": err.Error(),
		})
		return
	}

	// Write config with pretty formatting
	configJSON, err := json.MarshalIndent(newConfig, "", "  ")
	if err != nil {
		WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Failed to marshal config",
			"detail": err.Error(),
		})
		return
	}

	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
			"error": "Failed to write config file",
			"detail": err.Error(),
		})
		return
	}

	// Update global config
	globalConfigLock.Lock()
	globalConfig = &newConfig
	globalConfigLock.Unlock()

	// Update state.config to point to new config (so handlers see updated config)
	server := GetGlobalServer()
	if server != nil && server.GetState() != nil {
		server.GetState().UpdateConfig(&newConfig)
	}

	// Reload persistence config if server is available
	if server != nil && server.persistence != nil && newConfig.Persistence != nil {
		persistenceConfig := newConfig.GetPersistenceConfig()
		if err := server.persistence.UpdateConfig(persistenceConfig); err != nil {
			log.Printf("Warning: Failed to update persistence config: %v", err)
		}
	}

	WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
		"status": "Configuration saved successfully",
		"config": newConfig,
		"file_path": configPath,
		"note": "Configuration has been saved to file. Changes take effect immediately.",
	})
}
