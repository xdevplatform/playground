// Package playground serves the embedded web UI for the playground.
//
// This file handles serving the interactive web UI at /playground that allows
// users to explore and test API endpoints through a browser interface. The UI
// files (HTML, CSS, JavaScript) are embedded in the binary using go:embed.
package playground

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed ui/*
var uiFiles embed.FS

// HandleUI serves the playground UI.
func HandleUI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Remove /playground prefix
	path = strings.TrimPrefix(path, "/playground")
	if path == "" || path == "/" {
		path = "/index.html"
	}
	
	// Remove leading slash for embed.FS
	path = strings.TrimPrefix(path, "/")
	
	// Build the full path within the embedded filesystem
	filePath := "ui/" + path
	
	// Read file from embedded filesystem
	data, err := uiFiles.ReadFile(filePath)
	if err != nil {
		// If file not found and it's not index.html, try serving index.html (for SPA routing)
		if path != "index.html" && !strings.Contains(path, ".") {
			// Likely a route, serve index.html
			data, err = uiFiles.ReadFile("ui/index.html")
		} else if path != "index.html" {
			// File not found
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	
	// Set content type based on file extension
	ext := filepath.Ext(path)
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	
	if _, err := w.Write(data); err != nil {
		log.Printf("Error writing UI file %s: %v", path, err)
	}
}

// ServeUI creates a filesystem handler for the UI
func ServeUI() (http.Handler, error) {
	fsys, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return nil, fmt.Errorf("failed to create UI filesystem: %w", err)
	}
	return http.FileServer(http.FS(fsys)), nil
}
