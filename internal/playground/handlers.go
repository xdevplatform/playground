// Package playground sets up route handlers for the playground server.
//
// This file initializes all HTTP route handlers using the OpenAPI specification
// for endpoint discovery. It sets up the unified handler for all /2/* endpoints
// and provides fallback handlers if the OpenAPI spec is unavailable.
package playground

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// SetupHandlers sets up all route handlers using OpenAPI spec.
// All endpoints are discovered from OpenAPI, with state integration for stateful operations.
// Falls back to basic handlers if OpenAPI spec is not available.
func SetupHandlers(mux *http.ServeMux, state *State, spec *OpenAPISpec, examples *ExampleStore, server *Server) {
	if spec == nil {
		log.Printf("Warning: No OpenAPI spec available, using fallback handlers")
		setupFallbackHandlers(mux, state)
		return
	}

	// Use unified OpenAPI handler for all endpoints
	mux.HandleFunc("/2/", createUnifiedOpenAPIHandler(spec, state, examples, server))
	
	// Special case: OAuth token endpoint (needs custom handling)
	mux.HandleFunc("/2/oauth2/token", handleOAuthToken(state))
}

// handleGetMe handles GET /2/users/me
func handleGetMe(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		user := state.GetDefaultUser()
		if user == nil {
			WriteError(w, http.StatusNotFound, "User not found", 404)
			return
		}

		WriteJSONSafe(w, http.StatusOK, GenerateUserResponse(user))
	}
}

// handleGetUser handles GET /2/users/:id
func handleGetUser(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/2/users/")
		if path == "" || path == "me" {
			handleGetMe(state)(w, r)
			return
		}

		// Remove query parameters
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}

		user := state.GetUserByID(path)
		if user == nil {
			WriteError(w, http.StatusNotFound, "User not found", 404)
			return
		}

		WriteJSONSafe(w, http.StatusOK, GenerateUserResponse(user))
	}
}

// handleGetUserByUsername handles GET /2/users/by/username/:username
func handleGetUserByUsername(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/2/users/by/username/")
		if path == "" {
			WriteError(w, http.StatusBadRequest, "Username required", 400)
			return
		}

		// Remove query parameters
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}

		user := state.GetUserByUsername(path)
		if user == nil {
			WriteError(w, http.StatusNotFound, "User not found", 404)
			return
		}

		WriteJSONSafe(w, http.StatusOK, GenerateUserResponse(user))
	}
}

// handleTweets handles POST /2/tweets (create tweet)
func handleTweets(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Text string `json:"text"`
			}

			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				WriteError(w, http.StatusBadRequest, "Invalid request body", 400)
				return
			}

			if req.Text == "" {
				WriteError(w, http.StatusBadRequest, "Text is required", 400)
				return
			}

			user := state.GetDefaultUser()
			if user == nil {
				WriteError(w, http.StatusInternalServerError, "Default user not found", 500)
				return
			}

			tweet := state.CreateTweet(req.Text, user.ID)
			WriteJSONSafe(w, http.StatusCreated, GenerateTweetResponse(tweet))
			return
		}

		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
	}
}

// handleTweetByID handles GET /2/tweets/:id and DELETE /2/tweets/:id
func handleTweetByID(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/2/tweets/")
		if path == "" {
			WriteError(w, http.StatusBadRequest, "Tweet ID required", 400)
			return
		}

		// Remove query parameters
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}

		switch r.Method {
		case http.MethodGet:
			tweet := state.GetTweet(path)
			if tweet == nil {
				WriteError(w, http.StatusNotFound, "Tweet not found", 404)
				return
			}
			WriteJSONSafe(w, http.StatusOK, GenerateTweetResponse(tweet))

		case http.MethodDelete:
			if state.DeleteTweet(path) {
				w.WriteHeader(http.StatusOK)
				WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
					"data": map[string]bool{"deleted": true},
				})
			} else {
				WriteError(w, http.StatusNotFound, "Tweet not found", 404)
			}

		default:
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
		}
	}
}

// handleSearchTweets handles GET /2/tweets/search/recent
func handleSearchTweets(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		query := r.URL.Query().Get("query")
		limit := 10
		if limitStr := r.URL.Query().Get("max_results"); limitStr != "" {
			fmt.Sscanf(limitStr, "%d", &limit)
		}
		
		// Parse time filtering parameters
		sinceID := r.URL.Query().Get("since_id")
		untilID := r.URL.Query().Get("until_id")
		var startTime, endTime *time.Time
		if startTimeStr := r.URL.Query().Get("start_time"); startTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
				startTime = &t
			}
		}
		if endTimeStr := r.URL.Query().Get("end_time"); endTimeStr != "" {
			if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
				endTime = &t
			}
		}

		tweets := state.SearchTweets(r.Context(), query, limit, sinceID, untilID, startTime, endTime)
		WriteJSONSafe(w, http.StatusOK, GenerateTweetsResponse(tweets))
	}
}

// handleMediaInitialize handles POST /2/media/upload/initialize
func handleMediaInitialize(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		var req struct {
			TotalBytes    int64  `json:"total_bytes"`
			MediaType     string `json:"media_type"`
			MediaCategory string `json:"media_category"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body", 400)
			return
		}

		mediaKey := fmt.Sprintf("playground_media_%d", time.Now().Unix())
		media := state.CreateMedia(mediaKey, 3600)
		WriteJSONSafe(w, http.StatusOK, GenerateMediaInitResponse(media))
	}
}

// handleMediaUpload handles POST /2/media/upload/:id/append and POST /2/media/upload/:id/finalize
func handleMediaUpload(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/2/media/upload/")
		if path == "" || path == "initialize" {
			return // Handled by handleMediaInitialize
		}

		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			WriteError(w, http.StatusBadRequest, "Invalid media upload path", 400)
			return
		}

		mediaID := parts[0]
		action := parts[1]

		media := state.GetMedia(mediaID)
		if media == nil {
			WriteError(w, http.StatusNotFound, "Media not found", 404)
			return
		}

		switch action {
		case "append":
			if r.Method != http.MethodPost {
				WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
				return
			}
			// In playground, we just accept the append
			w.WriteHeader(http.StatusNoContent)

		case "finalize":
			if r.Method != http.MethodPost {
				WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
				return
			}
			// Set to processing state
			processingInfo := &ProcessingInfo{
				State:           "processing",
				CheckAfterSecs:  1,
				ProgressPercent: 50,
			}
			state.UpdateMediaState(mediaID, "processing", processingInfo)
			WriteJSONSafe(w, http.StatusOK, GenerateMediaStatusResponse(media))

		default:
			WriteError(w, http.StatusBadRequest, "Invalid action", 400)
		}
	}
}

// handleMediaStatus handles GET /2/media/upload?command=STATUS&media_id=:id
func handleMediaStatus(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		mediaID := r.URL.Query().Get("media_id")
		if mediaID == "" {
			WriteError(w, http.StatusBadRequest, "media_id required", 400)
			return
		}

		media := state.GetMedia(mediaID)
		if media == nil {
			WriteError(w, http.StatusNotFound, "Media not found", 404)
			return
		}

		// Simulate processing progression
		if media.State == "processing" && media.ProcessingInfo != nil {
			// After a few checks, mark as succeeded
			if media.ProcessingInfo.ProgressPercent >= 100 {
				state.UpdateMediaState(mediaID, "succeeded", nil)
			} else {
				media.ProcessingInfo.ProgressPercent += 25
				if media.ProcessingInfo.ProgressPercent > 100 {
					media.ProcessingInfo.ProgressPercent = 100
				}
			}
		}

		WriteJSONSafe(w, http.StatusOK, GenerateMediaStatusResponse(media))
	}
}

// handleOAuthToken handles POST /2/oauth2/token
func handleOAuthToken(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}

		// In playground mode, we accept any token exchange request
		WriteJSONSafe(w, http.StatusOK, GenerateOAuthTokenResponse())
	}
}

