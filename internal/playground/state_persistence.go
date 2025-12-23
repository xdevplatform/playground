// Package playground handles saving and loading playground state to/from disk.
//
// This file manages state persistence with automatic saving, retry logic for
// failed saves, and loading state on server startup. It supports both manual
// save operations and automatic periodic saves based on configuration.
package playground

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Constants for state persistence retry logic
const (
	// StateSaveMaxRetries is the maximum number of retry attempts for saving state
	StateSaveMaxRetries = 3
	// StateSaveInitialBackoff is the initial backoff delay for retry attempts
	StateSaveInitialBackoff = 100 * time.Millisecond
)

// StatePersistence handles saving and loading state to/from disk
type StatePersistence struct {
	config            *PersistenceConfig
	state             *State
	mu                sync.Mutex
	lastSave          time.Time
	saveTicker        *time.Ticker
	stopChan          chan bool
	stopOnce          sync.Once // Ensure stopChan is only closed once
	consecutiveFailures int // Track consecutive save failures
	maxFailures        int  // Maximum consecutive failures before disabling auto-save
}

// NewStatePersistence creates a new state persistence manager.
// Returns nil if persistence is disabled in the configuration.
// Starts auto-save goroutine if enabled in configuration.
func NewStatePersistence(state *State, config *PersistenceConfig) *StatePersistence {
	if config == nil || !config.Enabled {
		return nil // Persistence disabled
	}

	sp := &StatePersistence{
		config:        config,
		state:         state,
		stopChan:      make(chan bool),
		maxFailures:   5, // Disable auto-save after 5 consecutive failures
	}

	// Start auto-save if enabled
	if config.AutoSave && config.SaveInterval > 0 {
		sp.startAutoSave()
	}

	return sp
}

// LoadStateFromFile loads state from file if it exists.
// Returns nil, nil if the file doesn't exist (not an error).
// Returns an error if the file exists but cannot be read or parsed.
func LoadStateFromFile(config *PersistenceConfig) (*StateExport, error) {
	if config == nil || !config.Enabled {
		return nil, nil // Persistence disabled
	}

	filePath := config.FilePath
	if filePath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		filePath = filepath.Join(homeDir, ".playground", "state.json")
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil // File doesn't exist, return nil (not an error)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// Parse JSON
	var export StateExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &export, nil
}

// SaveStateWithRetry saves the current state to file with retry logic
func (sp *StatePersistence) SaveStateWithRetry() error {
	var lastErr error
	for attempt := 0; attempt < StateSaveMaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms
			backoff := StateSaveInitialBackoff * time.Duration(1<<uint(attempt-1))
			time.Sleep(backoff)
		}
		
		err := sp.SaveState()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	
	return fmt.Errorf("failed after %d attempts: %w", StateSaveMaxRetries, lastErr)
}

// SaveState saves the current state to file
func (sp *StatePersistence) SaveState() error {
	if sp == nil || sp.config == nil || !sp.config.Enabled {
		return nil // Persistence disabled
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Export current state
	sp.state.mu.RLock()
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
		PersonalizedTrends:   make([]*PersonalizedTrend, 0),
		ExportedAt:           time.Now(),
	}

	// Copy all data
	// For users, only export entries keyed by ID (not by username)
	// Username keys are used for lookup but shouldn't be exported as separate entries
	for k, v := range sp.state.users {
		// Only include entries where the key matches the user's ID
		// This filters out username-indexed entries
		if v != nil && v.ID == k {
			export.Users[k] = v
		}
	}
	for k, v := range sp.state.tweets {
		export.Tweets[k] = v
	}
	for k, v := range sp.state.media {
		export.Media[k] = v
	}
	for k, v := range sp.state.lists {
		export.Lists[k] = v
	}
	for k, v := range sp.state.spaces {
		export.Spaces[k] = v
	}
	for k, v := range sp.state.polls {
		export.Polls[k] = v
	}
	for k, v := range sp.state.places {
		export.Places[k] = v
	}
	for k, v := range sp.state.topics {
		export.Topics[k] = v
	}
	for k, v := range sp.state.searchStreamRules {
		export.SearchStreamRules[k] = v
	}
	for k, v := range sp.state.searchWebhooks {
		export.SearchWebhooks[k] = v
	}
	for k, v := range sp.state.dmConversations {
		export.DMConversations[k] = v
	}
	for k, v := range sp.state.dmEvents {
		export.DMEvents[k] = v
	}
	for k, v := range sp.state.complianceJobs {
		export.ComplianceJobs[k] = v
	}
	for k, v := range sp.state.communities {
		export.Communities[k] = v
	}
	for k, v := range sp.state.news {
		export.News[k] = v
	}
	for k, v := range sp.state.notes {
		export.Notes[k] = v
	}
	for k, v := range sp.state.activitySubscriptions {
		export.ActivitySubscriptions[k] = v
	}
	// Copy personalized trends (slice, not map)
	export.PersonalizedTrends = make([]*PersonalizedTrend, len(sp.state.personalizedTrends))
	copy(export.PersonalizedTrends, sp.state.personalizedTrends)
	sp.state.mu.RUnlock()

	// Marshal to JSON
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(sp.config.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to file atomically (write to temp file, then rename)
	tempFile := sp.config.FilePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	if err := os.Rename(tempFile, sp.config.FilePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	sp.lastSave = time.Now()
	return nil
}

// startAutoSave starts the auto-save ticker
func (sp *StatePersistence) startAutoSave() {
	if sp.config.SaveInterval <= 0 {
		return
	}

	sp.saveTicker = time.NewTicker(time.Duration(sp.config.SaveInterval) * time.Second)
	go func() {
		defer func() {
			// Ensure ticker is stopped when goroutine exits
			if sp.saveTicker != nil {
				sp.saveTicker.Stop()
			}
		}()
		for {
			select {
			case <-sp.saveTicker.C:
				if err := sp.SaveStateWithRetry(); err != nil {
					sp.mu.Lock()
					sp.consecutiveFailures++
					failures := sp.consecutiveFailures
					maxFailures := sp.maxFailures
					sp.mu.Unlock()
					
					if failures >= maxFailures {
						log.Printf("Error: Auto-save failed %d consecutive times. Disabling auto-save to prevent repeated errors.", failures)
						// Ticker will be stopped by defer
						return
					} else {
						log.Printf("Warning: Auto-save failed (attempt %d/%d): %v", failures, maxFailures, err)
					}
				} else {
					// Reset failure count on success
					sp.mu.Lock()
					if sp.consecutiveFailures > 0 {
						sp.consecutiveFailures = 0
					}
					sp.mu.Unlock()
				}
			case <-sp.stopChan:
				// Ticker will be stopped by defer
				return
			}
		}
	}()
}

// Stop stops the auto-save ticker and performs a final save
func (sp *StatePersistence) Stop() error {
	if sp == nil {
		return nil
	}

	if sp.saveTicker != nil {
		sp.saveTicker.Stop()
		// Use sync.Once to ensure channel is only closed once
		sp.stopOnce.Do(func() {
			if sp.stopChan != nil {
				close(sp.stopChan)
			}
		})
	}

	// Final save
	return sp.SaveState()
}

// UpdateConfig updates the persistence configuration and restarts auto-save if needed
// This allows runtime configuration changes without server restart
func (sp *StatePersistence) UpdateConfig(newConfig *PersistenceConfig) error {
	if sp == nil {
		return fmt.Errorf("persistence is nil")
	}
	if newConfig == nil {
		return fmt.Errorf("config is nil")
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Stop existing auto-save if running
	if sp.saveTicker != nil {
		sp.saveTicker.Stop()
		sp.stopOnce.Do(func() {
			if sp.stopChan != nil {
				close(sp.stopChan)
			}
		})
		// Create new stop channel for restarted auto-save
		sp.stopChan = make(chan bool)
		sp.stopOnce = sync.Once{}
	}

	// Update config
	sp.config = newConfig

	// Restart auto-save if enabled
	if newConfig.Enabled && newConfig.AutoSave && newConfig.SaveInterval > 0 {
		sp.startAutoSave()
		log.Printf("Persistence config updated: %s (auto-save: %v, interval: %ds)", 
			newConfig.FilePath, newConfig.AutoSave, newConfig.SaveInterval)
	} else if !newConfig.Enabled {
		log.Printf("Persistence disabled")
	}

	return nil
}

// ImportStateFromFile imports state from the persistence file into the state object
func ImportStateFromFile(state *State, export *StateExport) {
	if export == nil {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if export.Users != nil {
		state.users = export.Users
		// Rebuild username index after import and populate missing fields
		for _, user := range state.users {
			if user != nil {
				// Rebuild username index
				if user.Username != "" {
					state.users[user.Username] = user
				}
				// Populate missing fields for backward compatibility
				if user.VerifiedType == "" {
					if user.Verified {
						user.VerifiedType = "blue" // Default verified type for verified users
					} else {
						user.VerifiedType = "none"
					}
				}
				// Initialize empty slices if nil (for backward compatibility)
				if user.Tweets == nil {
					user.Tweets = make([]string, 0)
				}
				if user.LikedTweets == nil {
					user.LikedTweets = make([]string, 0)
				}
				if user.RetweetedTweets == nil {
					user.RetweetedTweets = make([]string, 0)
				}
				if user.Following == nil {
					user.Following = make([]string, 0)
				}
				if user.Followers == nil {
					user.Followers = make([]string, 0)
				}
				if user.Lists == nil {
					user.Lists = make([]string, 0)
				}
				if user.PinnedLists == nil {
					user.PinnedLists = make([]string, 0)
				}
				if user.ListMemberships == nil {
					user.ListMemberships = make([]string, 0)
				}
				if user.Spaces == nil {
					user.Spaces = make([]string, 0)
				}
				if user.MutedUsers == nil {
					user.MutedUsers = make([]string, 0)
				}
				if user.BlockedUsers == nil {
					user.BlockedUsers = make([]string, 0)
				}
				if user.BookmarkedTweets == nil {
					user.BookmarkedTweets = make([]string, 0)
				}
				if user.FollowedLists == nil {
					user.FollowedLists = make([]string, 0)
				}
			}
		}
	}
	if export.Tweets != nil {
		state.tweets = export.Tweets
	}
	if export.Media != nil {
		state.media = export.Media
	}
	if export.Lists != nil {
		state.lists = export.Lists
	}
	if export.Spaces != nil {
		state.spaces = export.Spaces
	}
	if export.Polls != nil {
		state.polls = export.Polls
	}
	if export.Places != nil {
		state.places = export.Places
	}
	if export.Topics != nil {
		state.topics = export.Topics
	}
	if export.SearchStreamRules != nil {
		state.searchStreamRules = export.SearchStreamRules
	}
	if export.SearchWebhooks != nil {
		state.searchWebhooks = export.SearchWebhooks
	}
	if export.DMConversations != nil {
		state.dmConversations = export.DMConversations
	}
	if export.DMEvents != nil {
		state.dmEvents = export.DMEvents
	}
	if export.ComplianceJobs != nil {
		state.complianceJobs = export.ComplianceJobs
	}
	if export.Communities != nil {
		state.communities = export.Communities
	}
	if export.News != nil {
		state.news = export.News
	}
	if export.Notes != nil {
		state.notes = export.Notes
	}
	if export.ActivitySubscriptions != nil {
		state.activitySubscriptions = export.ActivitySubscriptions
	}
	if export.PersonalizedTrends != nil && len(export.PersonalizedTrends) > 0 {
		state.personalizedTrends = make([]*PersonalizedTrend, len(export.PersonalizedTrends))
		copy(state.personalizedTrends, export.PersonalizedTrends)
	} else {
		// If no trends in persisted state, leave as nil (will be checked and seeded if needed)
		state.personalizedTrends = nil
	}

	// Update nextID based on highest ID found
	maxID := int64(0)
	for _, user := range state.users {
		if id, err := parseInt64(user.ID); err == nil && id > maxID {
			maxID = id
		}
	}
	for _, tweet := range state.tweets {
		if id, err := parseInt64(tweet.ID); err == nil && id > maxID {
			maxID = id
		}
	}
	if maxID > 0 {
		state.nextID = maxID + 1
	}
	
	// Ensure default user (ID "0") always exists
	// Note: Lock is already held, so use the unlocked version
	ensureDefaultUserUnlocked(state)
}

// parseInt64 parses a string ID to int64
func parseInt64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// ensureDefaultUserUnlocked ensures the default playground user (ID "0") exists
// This version assumes the lock is already held - use ensureDefaultUser() if you need locking
func ensureDefaultUserUnlocked(state *State) {
	// Check if default user already exists
	if state.users["0"] != nil {
		return
	}
	
	// Create default user if it doesn't exist
	defaultUser := &User{
		ID:            "0",
		Name:          "Playground User",
		Username:      "playground_user",
		Description:   "Default playground user for testing",
		CreatedAt:     time.Now().AddDate(-2, 0, 0), // 2 years ago
		Verified:      false,
		VerifiedType:  "none",
		Protected:     false,
		Tweets:        make([]string, 0),
		LikedTweets:   make([]string, 0),
		RetweetedTweets: make([]string, 0),
		Following:     make([]string, 0),
		Followers:     make([]string, 0),
		Lists:         make([]string, 0),
		PinnedLists:   make([]string, 0),
		ListMemberships: make([]string, 0),
		Spaces:        make([]string, 0),
		MutedUsers:    make([]string, 0),
		BlockedUsers:  make([]string, 0),
		BookmarkedTweets: make([]string, 0),
		FollowedLists: make([]string, 0),
	}

	state.users["0"] = defaultUser
	state.users["playground_user"] = defaultUser // Index by username
}

// ensureDefaultUser ensures the default playground user (ID "0") exists
// This is called after importing state to ensure the default user is always available
// This version acquires its own lock - use ensureDefaultUserUnlocked() if lock is already held
func ensureDefaultUser(state *State) {
	state.mu.Lock()
	defer state.mu.Unlock()
	ensureDefaultUserUnlocked(state)
}
