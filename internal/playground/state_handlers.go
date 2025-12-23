// Package playground provides HTTP handlers for state management endpoints.
//
// This file handles the /playground/state/* endpoints for exporting, importing,
// resetting, and deleting playground state. It supports atomic state imports
// to prevent corruption and includes validation of imported data.
package playground

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// RelationshipExport represents a single relationship for export
type RelationshipExport struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	UserID        string `json:"user_id"`
	TargetUserID  string `json:"target_user_id,omitempty"`
	TargetTweetID string `json:"target_tweet_id,omitempty"`
	TargetListID  string `json:"target_list_id,omitempty"`
}

// StateExport represents the exported state data.
// Used for serializing playground state to JSON for persistence or backup.
type StateExport struct {
	Users              map[string]*User              `json:"users"`
	Tweets             map[string]*Tweet             `json:"tweets"`
	Media              map[string]*Media             `json:"media"`
	Lists              map[string]*List              `json:"lists"`
	Spaces             map[string]*Space             `json:"spaces"`
	Polls              map[string]*Poll              `json:"polls"`
	Places             map[string]*Place             `json:"places"`
	Topics             map[string]*Topic              `json:"topics"`
	SearchStreamRules  map[string]*SearchStreamRule   `json:"search_stream_rules,omitempty"`
	SearchWebhooks     map[string]*SearchWebhook      `json:"search_webhooks,omitempty"`
	DMConversations    map[string]*DMConversation     `json:"dm_conversations,omitempty"`
	DMEvents           map[string]*DMEvent            `json:"dm_events,omitempty"`
	ComplianceJobs     map[string]*ComplianceJob      `json:"compliance_jobs,omitempty"`
	Communities        map[string]*Community          `json:"communities,omitempty"`
	News               map[string]*News               `json:"news,omitempty"`
	Notes              map[string]*Note               `json:"notes,omitempty"`
	ActivitySubscriptions map[string]*ActivitySubscription `json:"activity_subscriptions,omitempty"`
	PersonalizedTrends []*PersonalizedTrend          `json:"personalized_trends,omitempty"`
	Relationships      []RelationshipExport          `json:"relationships,omitempty"`
	ExportedAt         time.Time                      `json:"exported_at"`
}

// HandleStateReset resets the playground state to initial seeded state
func HandleStateReset(state *State, persistence *StatePersistence) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get current config
		config := GetGlobalConfig()

		// Reset state
		state.mu.Lock()
		// Clear all data
		state.users = make(map[string]*User)
		state.tweets = make(map[string]*Tweet)
		state.media = make(map[string]*Media)
		state.lists = make(map[string]*List)
		state.spaces = make(map[string]*Space)
		state.polls = make(map[string]*Poll)
		state.places = make(map[string]*Place)
		state.topics = make(map[string]*Topic)
		state.searchStreamRules = make(map[string]*SearchStreamRule)
		state.searchWebhooks = make(map[string]*SearchWebhook)
		state.dmConversations = make(map[string]*DMConversation)
		state.dmEvents = make(map[string]*DMEvent)
		state.complianceJobs = make(map[string]*ComplianceJob)
		state.communities = make(map[string]*Community)
		state.news = make(map[string]*News)
		state.notes = make(map[string]*Note)
		state.activitySubscriptions = make(map[string]*ActivitySubscription)
		state.nextID = 1
		state.mu.Unlock()

		// Reseed with config
		if config != nil {
			seedRealisticData(state, config)
		} else {
			// Fallback to default seeding
			seedRealisticData(state, nil)
		}

		// Save state if persistence is enabled
		if persistence != nil {
			if err := persistence.SaveState(); err != nil {
				WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
					"status": "State reset successfully",
					"message": "All data has been cleared and reseeded",
					"warning": fmt.Sprintf("Failed to save state: %v", err),
				})
				return
			}
		}

		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"status": "State reset successfully",
			"message": "All data has been cleared and reseeded",
		})
	}
}

// HandleStateDelete deletes all playground state (clears everything without reseeding).
// Clears all entities but does not re-seed. Use HandleStateReset to reset and re-seed.
func HandleStateDelete(state *State, persistence *StatePersistence) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Clear all state
		state.mu.Lock()
		state.users = make(map[string]*User)
		state.tweets = make(map[string]*Tweet)
		state.media = make(map[string]*Media)
		state.lists = make(map[string]*List)
		state.spaces = make(map[string]*Space)
		state.polls = make(map[string]*Poll)
		state.places = make(map[string]*Place)
		state.topics = make(map[string]*Topic)
		state.searchStreamRules = make(map[string]*SearchStreamRule)
		state.searchWebhooks = make(map[string]*SearchWebhook)
		state.dmConversations = make(map[string]*DMConversation)
		state.dmEvents = make(map[string]*DMEvent)
		state.complianceJobs = make(map[string]*ComplianceJob)
		state.communities = make(map[string]*Community)
		state.news = make(map[string]*News)
		state.notes = make(map[string]*Note)
		state.activitySubscriptions = make(map[string]*ActivitySubscription)
		state.nextID = 1
		state.mu.Unlock()

		// Save state if persistence is enabled
		if persistence != nil {
			if err := persistence.SaveState(); err != nil {
				WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
					"status": "State deleted successfully",
					"message": "All data has been cleared",
					"warning": fmt.Sprintf("Failed to save state: %v", err),
				})
				return
			}
		}

		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"status": "State deleted successfully",
			"message": "All data has been cleared",
		})
	}
}

// HandleStateExport exports the current playground state as JSON.
// Returns all entities in the playground state for backup or transfer.
func HandleStateExport(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state.mu.RLock()
		export := StateExport{
			Users:                make(map[string]*User),
			Tweets:               make(map[string]*Tweet),
			Media:                make(map[string]*Media),
			Lists:                make(map[string]*List),
			Spaces:               make(map[string]*Space),
			Polls:                make(map[string]*Poll),
			Places:               make(map[string]*Place),
			Topics:               make(map[string]*Topic),
			SearchStreamRules:    make(map[string]*SearchStreamRule),
			SearchWebhooks:       make(map[string]*SearchWebhook),
			DMConversations:      make(map[string]*DMConversation),
			DMEvents:             make(map[string]*DMEvent),
			ComplianceJobs:       make(map[string]*ComplianceJob),
			Communities:          make(map[string]*Community),
			News:                 make(map[string]*News),
			Notes:                make(map[string]*Note),
			ActivitySubscriptions: make(map[string]*ActivitySubscription),
			ExportedAt:          time.Now(),
		}

		// Copy all data
		// For users, only export entries keyed by ID (not by username)
		// Username keys are used for lookup but shouldn't be exported as separate entries
		for k, v := range state.users {
			// Only include entries where the key matches the user's ID
			// This filters out username-indexed entries
			if v != nil && v.ID == k {
				export.Users[k] = v
			}
		}
		for k, v := range state.tweets {
			export.Tweets[k] = v
		}
		for k, v := range state.media {
			export.Media[k] = v
		}
		for k, v := range state.lists {
			export.Lists[k] = v
		}
		for k, v := range state.spaces {
			export.Spaces[k] = v
		}
		for k, v := range state.polls {
			export.Polls[k] = v
		}
		for k, v := range state.places {
			export.Places[k] = v
		}
		for k, v := range state.topics {
			export.Topics[k] = v
		}
		for k, v := range state.searchStreamRules {
			export.SearchStreamRules[k] = v
		}
		for k, v := range state.searchWebhooks {
			export.SearchWebhooks[k] = v
		}
		for k, v := range state.dmConversations {
			export.DMConversations[k] = v
		}
		for k, v := range state.dmEvents {
			export.DMEvents[k] = v
		}
		for k, v := range state.complianceJobs {
			export.ComplianceJobs[k] = v
		}
		for k, v := range state.communities {
			export.Communities[k] = v
		}
		for k, v := range state.news {
			export.News[k] = v
		}
		for k, v := range state.notes {
			export.Notes[k] = v
		}
		for k, v := range state.activitySubscriptions {
			export.ActivitySubscriptions[k] = v
		}
		
		// Extract relationships from users
		// Read directly from state.users to ensure we get the latest relationship data
		// This ensures that relationships created via API calls are included in the export
		relationships := []RelationshipExport{}
		for k, user := range state.users {
			if user == nil {
				continue
			}
			// Only process users keyed by ID (not by username)
			// Username keys are used for lookup but shouldn't be processed for relationships
			if user.ID != k {
				continue
			}
			
			// Bookmarks
			for _, tweetID := range user.BookmarkedTweets {
				relationships = append(relationships, RelationshipExport{
					ID:            fmt.Sprintf("bookmark-%s-%s", user.ID, tweetID),
					Type:          "bookmark",
					UserID:        user.ID,
					TargetTweetID: tweetID,
				})
			}
			
			// Likes
			for _, tweetID := range user.LikedTweets {
				relationships = append(relationships, RelationshipExport{
					ID:            fmt.Sprintf("like-%s-%s", user.ID, tweetID),
					Type:          "like",
					UserID:        user.ID,
					TargetTweetID: tweetID,
				})
			}
			
			// Following
			for _, targetUserID := range user.Following {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("following-%s-%s", user.ID, targetUserID),
					Type:         "following",
					UserID:       user.ID,
					TargetUserID: targetUserID,
				})
			}
			
			// Followers (reverse relationship)
			for _, followerID := range user.Followers {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("follower-%s-%s", followerID, user.ID),
					Type:         "follower",
					UserID:       followerID,
					TargetUserID: user.ID,
				})
			}
			
			// Retweets
			for _, tweetID := range user.RetweetedTweets {
				relationships = append(relationships, RelationshipExport{
					ID:            fmt.Sprintf("retweet-%s-%s", user.ID, tweetID),
					Type:          "retweet",
					UserID:        user.ID,
					TargetTweetID: tweetID,
				})
			}
			
			// Muting
			for _, targetUserID := range user.MutedUsers {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("mute-%s-%s", user.ID, targetUserID),
					Type:         "mute",
					UserID:       user.ID,
					TargetUserID: targetUserID,
				})
			}
			
			// Blocking
			for _, targetUserID := range user.BlockedUsers {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("block-%s-%s", user.ID, targetUserID),
					Type:         "block",
					UserID:       user.ID,
					TargetUserID: targetUserID,
				})
			}
			
			// List Memberships
			for _, listID := range user.ListMemberships {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("list_member-%s-%s", user.ID, listID),
					Type:         "list_member",
					UserID:       user.ID,
					TargetListID: listID,
				})
			}
			
			// Followed Lists
			for _, listID := range user.FollowedLists {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("followed_list-%s-%s", user.ID, listID),
					Type:         "followed_list",
					UserID:       user.ID,
					TargetListID: listID,
				})
			}
			
			// Pinned Lists
			for _, listID := range user.PinnedLists {
				relationships = append(relationships, RelationshipExport{
					ID:           fmt.Sprintf("pinned_list-%s-%s", user.ID, listID),
					Type:         "pinned_list",
					UserID:       user.ID,
					TargetListID: listID,
				})
			}
		}
		export.Relationships = relationships
		
		state.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=playground-state.json")
		if err := json.NewEncoder(w).Encode(export); err != nil {
			log.Printf("Error encoding state export: %v", err)
			http.Error(w, "Failed to encode state export", http.StatusInternalServerError)
			return
		}
	}
}

// findMaxIDFromImport finds the maximum numeric ID from all imported entities
// This is used to set nextID appropriately to prevent ID collisions
func findMaxIDFromImport(importData *StateExport) int64 {
	maxID := int64(0)
	
	// Helper to parse numeric ID from string
	parseID := func(idStr string) int64 {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			return id
		}
		return 0
	}
	
	// Check all entity types
	if importData.Users != nil {
		for id := range importData.Users {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Tweets != nil {
		for id := range importData.Tweets {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Lists != nil {
		for id := range importData.Lists {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Spaces != nil {
		for id := range importData.Spaces {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Media != nil {
		for id := range importData.Media {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Polls != nil {
		for id := range importData.Polls {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Places != nil {
		for id := range importData.Places {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Topics != nil {
		for id := range importData.Topics {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.SearchStreamRules != nil {
		for id := range importData.SearchStreamRules {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.SearchWebhooks != nil {
		for id := range importData.SearchWebhooks {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.DMConversations != nil {
		for id := range importData.DMConversations {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.DMEvents != nil {
		for id := range importData.DMEvents {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.ComplianceJobs != nil {
		for id := range importData.ComplianceJobs {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Communities != nil {
		for id := range importData.Communities {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.News != nil {
		for id := range importData.News {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.Notes != nil {
		for id := range importData.Notes {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	if importData.ActivitySubscriptions != nil {
		for id := range importData.ActivitySubscriptions {
			if idNum := parseID(id); idNum > maxID {
				maxID = idNum
			}
		}
	}
	
	return maxID
}

// HandleStateImport imports state from JSON atomically.
// Uses a temporary state object and atomically swaps it with the main state upon success.
// Validates import limits to prevent memory exhaustion.
func HandleStateImport(state *State, persistence *StatePersistence) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Prevent concurrent imports
		state.importMu.Lock()
		defer state.importMu.Unlock()

		// Enforce request size limit
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestSize)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
				"error": "Failed to read request body",
				"detail": err.Error(),
			})
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

		var importData StateExport
		if err := json.Unmarshal(body, &importData); err != nil {
			WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
				"error": "Invalid JSON",
				"detail": err.Error(),
			})
			return
		}

		// Validate imported data before importing
		if validationErrors := ValidateStateImport(&importData); len(validationErrors) > 0 {
			errorResponse := FormatValidationErrors(validationErrors)
			WriteJSONSafe(w, http.StatusBadRequest, errorResponse)
			return
		}

		// Get current nextID and calculate max ID from import
		state.mu.RLock()
		currentNextID := state.getNextID()
		state.mu.RUnlock()
		
		maxImportedID := findMaxIDFromImport(&importData)
		// Set nextID to max(currentNextID, maxImportedID + 1) to prevent collisions
		newNextID := currentNextID
		if maxImportedID+1 > newNextID {
			newNextID = maxImportedID + 1
		}

		// Create temporary state for atomic import
		tempState := NewStateWithConfig(state.config)
		if tempState == nil {
			WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
				"error": "Failed to create temporary state",
			})
			return
		}

		ctx := r.Context()
		checkInterval := ContextCheckIntervalLarge
		state.mu.RLock()
		if importData.Users == nil {
			tempState.users = make(map[string]*User)
			count := 0
			for k, v := range state.users {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.users[k] = v
				count++
			}
		}
		if importData.Tweets == nil {
			tempState.tweets = make(map[string]*Tweet)
			count := 0
			for k, v := range state.tweets {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.tweets[k] = v
				count++
			}
		}
		if importData.Media == nil {
			tempState.media = make(map[string]*Media)
			count := 0
			for k, v := range state.media {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.media[k] = v
				count++
			}
		}
		if importData.Lists == nil {
			tempState.lists = make(map[string]*List)
			count := 0
			for k, v := range state.lists {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.lists[k] = v
				count++
			}
		}
		if importData.Spaces == nil {
			tempState.spaces = make(map[string]*Space)
			count := 0
			for k, v := range state.spaces {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.spaces[k] = v
				count++
			}
		}
		if importData.Polls == nil {
			tempState.polls = make(map[string]*Poll)
			count := 0
			for k, v := range state.polls {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.polls[k] = v
				count++
			}
		}
		if importData.Places == nil {
			tempState.places = make(map[string]*Place)
			count := 0
			for k, v := range state.places {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.places[k] = v
				count++
			}
		}
		if importData.Topics == nil {
			tempState.topics = make(map[string]*Topic)
			count := 0
			for k, v := range state.topics {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.topics[k] = v
				count++
			}
		}
		if importData.SearchStreamRules == nil {
			tempState.searchStreamRules = make(map[string]*SearchStreamRule)
			count := 0
			for k, v := range state.searchStreamRules {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.searchStreamRules[k] = v
				count++
			}
		}
		if importData.SearchWebhooks == nil {
			tempState.searchWebhooks = make(map[string]*SearchWebhook)
			count := 0
			for k, v := range state.searchWebhooks {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.searchWebhooks[k] = v
				count++
			}
		}
		if importData.DMConversations == nil {
			tempState.dmConversations = make(map[string]*DMConversation)
			count := 0
			for k, v := range state.dmConversations {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.dmConversations[k] = v
				count++
			}
		}
		if importData.DMEvents == nil {
			tempState.dmEvents = make(map[string]*DMEvent)
			count := 0
			for k, v := range state.dmEvents {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.dmEvents[k] = v
				count++
			}
		}
		if importData.ComplianceJobs == nil {
			tempState.complianceJobs = make(map[string]*ComplianceJob)
			count := 0
			for k, v := range state.complianceJobs {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.complianceJobs[k] = v
				count++
			}
		}
		if importData.Communities == nil {
			tempState.communities = make(map[string]*Community)
			count := 0
			for k, v := range state.communities {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.communities[k] = v
				count++
			}
		}
		if importData.News == nil {
			tempState.news = make(map[string]*News)
			count := 0
			for k, v := range state.news {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.news[k] = v
				count++
			}
		}
		if importData.Notes == nil {
			tempState.notes = make(map[string]*Note)
			count := 0
			for k, v := range state.notes {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.notes[k] = v
				count++
			}
		}
		if importData.ActivitySubscriptions == nil {
			tempState.activitySubscriptions = make(map[string]*ActivitySubscription)
			count := 0
			for k, v := range state.activitySubscriptions {
				if count%checkInterval == 0 {
					select {
					case <-ctx.Done():
						state.mu.RUnlock()
						WriteJSONSafe(w, http.StatusRequestTimeout, map[string]interface{}{
							"error": "Request cancelled during state import",
						})
						return
					default:
					}
				}
				tempState.activitySubscriptions[k] = v
				count++
			}
		}
		state.mu.RUnlock()

		// Import data into temporary state
		tempState.mu.Lock()
		if importData.Users != nil {
			tempState.users = importData.Users
			// Rebuild username index
			for _, user := range tempState.users {
				if user != nil && user.Username != "" {
					tempState.users[user.Username] = user
				}
			}
		}
		if importData.Tweets != nil {
			tempState.tweets = importData.Tweets
		}
		if importData.Media != nil {
			tempState.media = importData.Media
		}
		if importData.Lists != nil {
			tempState.lists = importData.Lists
		}
		if importData.Spaces != nil {
			tempState.spaces = importData.Spaces
		}
		if importData.Polls != nil {
			tempState.polls = importData.Polls
		}
		if importData.Places != nil {
			tempState.places = importData.Places
		}
		if importData.Topics != nil {
			tempState.topics = importData.Topics
		}
		if importData.SearchStreamRules != nil {
			tempState.searchStreamRules = importData.SearchStreamRules
		}
		if importData.SearchWebhooks != nil {
			tempState.searchWebhooks = importData.SearchWebhooks
		}
		if importData.DMConversations != nil {
			tempState.dmConversations = importData.DMConversations
		}
		if importData.DMEvents != nil {
			tempState.dmEvents = importData.DMEvents
		}
		if importData.ComplianceJobs != nil {
			tempState.complianceJobs = importData.ComplianceJobs
		}
		if importData.Communities != nil {
			tempState.communities = importData.Communities
		}
		if importData.News != nil {
			tempState.news = importData.News
		}
		if importData.Notes != nil {
			tempState.notes = importData.Notes
		}
		if importData.ActivitySubscriptions != nil {
			tempState.activitySubscriptions = importData.ActivitySubscriptions
		}
		// Set nextID to prevent collisions
		tempState.nextID = newNextID
		tempState.mu.Unlock()

		// Atomically swap the state
		state.mu.Lock()
		// Swap all maps atomically
		state.users = tempState.users
		state.tweets = tempState.tweets
		state.media = tempState.media
		state.lists = tempState.lists
		state.spaces = tempState.spaces
		state.polls = tempState.polls
		state.places = tempState.places
		state.topics = tempState.topics
		state.searchStreamRules = tempState.searchStreamRules
		state.searchWebhooks = tempState.searchWebhooks
		state.dmConversations = tempState.dmConversations
		state.dmEvents = tempState.dmEvents
		state.complianceJobs = tempState.complianceJobs
		state.communities = tempState.communities
		state.news = tempState.news
		state.notes = tempState.notes
		state.activitySubscriptions = tempState.activitySubscriptions
		state.nextID = tempState.nextID
		state.mu.Unlock()

		// Calculate import metrics
		importDuration := time.Since(startTime)
		entityCounts := map[string]int{
			"users":                len(importData.Users),
			"tweets":               len(importData.Tweets),
			"lists":                len(importData.Lists),
			"spaces":               len(importData.Spaces),
			"media":                len(importData.Media),
			"polls":                len(importData.Polls),
			"places":               len(importData.Places),
			"topics":               len(importData.Topics),
			"search_stream_rules":   len(importData.SearchStreamRules),
			"search_webhooks":      len(importData.SearchWebhooks),
			"dm_conversations":     len(importData.DMConversations),
			"dm_events":            len(importData.DMEvents),
			"compliance_jobs":      len(importData.ComplianceJobs),
			"communities":          len(importData.Communities),
			"news":                 len(importData.News),
			"notes":                 len(importData.Notes),
			"activity_subscriptions": len(importData.ActivitySubscriptions),
		}
		totalEntities := 0
		for _, count := range entityCounts {
			totalEntities += count
		}
		
		log.Printf("State import completed: %d total entities in %v", totalEntities, importDuration)

		// Save state if persistence is enabled
		if persistence != nil {
			if err := persistence.SaveState(); err != nil {
				WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
					"status": "State imported successfully",
					"message": "State data has been loaded",
					"exported_at": importData.ExportedAt,
					"warning": fmt.Sprintf("Failed to save state: %v", err),
					"import_metrics": map[string]interface{}{
						"duration_ms": importDuration.Milliseconds(),
						"total_entities": totalEntities,
						"entities": entityCounts,
					},
				})
				return
			}
		}

		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"status": "State imported successfully",
			"message": "State data has been loaded",
			"exported_at": importData.ExportedAt,
			"import_metrics": map[string]interface{}{
				"duration_ms": importDuration.Milliseconds(),
				"total_entities": totalEntities,
				"entities": entityCounts,
			},
		})
	}
}

// HandleStateSave manually saves the current state (if persistence is enabled).
// Forces an immediate save to disk, bypassing the auto-save interval.
func HandleStateSave(persistence *StatePersistence) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if persistence == nil {
			WriteJSONSafe(w, http.StatusBadRequest, map[string]interface{}{
				"error": "State persistence is not enabled",
				"message": "Enable persistence in configuration to use this endpoint",
			})
			return
		}

		if err := persistence.SaveState(); err != nil {
			WriteJSONSafe(w, http.StatusInternalServerError, map[string]interface{}{
				"error": "Failed to save state",
				"detail": err.Error(),
			})
			return
		}

		WriteJSONSafe(w, http.StatusOK, map[string]interface{}{
			"status": "State saved successfully",
			"file_path": persistence.config.FilePath,
		})
	}
}
