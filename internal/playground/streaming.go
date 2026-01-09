// Package playground handles Server-Sent Events (SSE) streaming endpoints.
//
// This file implements streaming support for real-time endpoints like sample
// stream, search stream, and firehose stream. It manages SSE connections,
// streams tweet data, handles client disconnections, and supports filtering
// and rule-based streaming.
package playground

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	mathrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleStreamingEndpoint handles Server-Sent Events (SSE) streaming endpoints
// Returns true if handled, false otherwise
func handleStreamingEndpoint(w http.ResponseWriter, op *EndpointOperation, path, method string, r *http.Request, state *State, spec *OpenAPISpec, queryParams *QueryParams, creditTracker *CreditTracker) bool {
	log.Printf("DEBUG handleStreamingEndpoint: called with path='%s', method='%s'", path, method)
	// Exclude rule management endpoints - these are NOT streaming endpoints
	if strings.Contains(path, "/stream/rules") {
		log.Printf("DEBUG handleStreamingEndpoint: path contains /stream/rules, returning false")
		return false
	}
	
	// Check if this is a known streaming path pattern
	// Actual streaming endpoints end with /stream or have /stream followed by /lang/
	isStreamingPath := strings.HasSuffix(path, "/stream") ||
	                  strings.Contains(path, "/sample/stream") ||
	                  strings.Contains(path, "/sample10/stream") ||
	                  strings.Contains(path, "/search/stream") ||
	                  strings.Contains(path, "/firehose/stream") ||
	                  strings.Contains(path, "/compliance/stream")
	
	// Also check OpenAPI spec if available
	isStreamingInSpec := op != nil && op.Operation != nil && op.Operation.IsStreamingEndpoint()
	
	// If neither indicates streaming, don't handle
	if !isStreamingPath && !isStreamingInSpec {
		return false
	}

	// Validate required query parameters BEFORE setting up stream
	// Language-specific firehose streams and sample10 streams require partition parameter
	requiresPartition := (strings.Contains(path, "/firehose/stream") && strings.Contains(path, "/lang/")) ||
	                     strings.Contains(path, "/sample10/stream")
	
	if requiresPartition {
		partition := r.URL.Query().Get("partition")
		if partition == "" {
			// Return validation error in X API format matching expected structure
			errorResp := map[string]interface{}{
				"errors": []map[string]interface{}{
					{
						"parameters": map[string]interface{}{
							"partition": []string{},
						},
						"message": "The `partition` query parameter can not be empty",
					},
				},
				"title":  "Invalid Request",
				"detail": "One or more parameters to your request was invalid.",
				"type":   "https://api.twitter.com/2/problems/invalid-request",
			}
			AddXAPIHeaders(w)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(errorResp)
			return true
		}
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.Header().Set("Transfer-Encoding", "chunked")
	
	// Add comprehensive X API headers for streaming
	AddStreamingHeaders(w, r, state)
	
	// WriteHeader should be called once
	// The responseTimeWriter wrapper will prevent duplicate WriteHeader calls
	w.WriteHeader(http.StatusOK)

	// Ensure we can flush - check both responseTimeWriter and underlying writer
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	}
	
	if flusher == nil {
		log.Printf("Warning: ResponseWriter does not support flushing")
		return false
	}

	// Flush headers immediately
	flusher.Flush()

	// Get authenticated user ID for connection registration
	authenticatedUserID := getAuthenticatedUserID(r, state)
	// Get developer account ID for credit tracking (matches real X API behavior)
	developerAccountID := getDeveloperAccountID(r, state)
	
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel() // Ensure cleanup on exit
	
	// Register the connection so it can be closed by DELETE /2/connections/all
	connectionID, unregister := state.RegisterStreamConnection(authenticatedUserID, cancel)
	defer unregister() // Unregister when connection ends
	
	log.Printf("Registered stream connection %s for user %s (dev account %s) at path %s", connectionID, authenticatedUserID, developerAccountID, path)
	
	// Create a new request with the cancellable context
	r = r.WithContext(ctx)

	// Determine what to stream based on endpoint
	if strings.Contains(path, "/tweets/sample/stream") || strings.Contains(path, "/tweets/sample10/stream") {
		log.Printf("Streaming sample tweets from: %s", path)
		streamSampleTweets(w, r, state, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}

	if strings.Contains(path, "/tweets/search/stream") {
		log.Printf("Streaming search tweets from: %s", path)
		streamSearchTweets(w, r, state, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}

	if strings.Contains(path, "/tweets/firehose/stream") {
		log.Printf("Streaming firehose tweets from: %s", path)
		// Check for language-specific firehose streams
		if strings.Contains(path, "/lang/") {
			lang := extractLanguageFromPath(path)
			log.Printf("Detected language-specific firehose stream for language: %s (path: %s)", lang, path)
			streamFirehoseTweetsByLanguage(w, r, state, queryParams, lang, creditTracker, developerAccountID, path, method)
		} else {
			log.Printf("Streaming regular firehose (no language filter)")
			streamFirehoseTweets(w, r, state, queryParams, creditTracker, developerAccountID, path, method)
		}
		return true
	}

	if strings.Contains(path, "/likes/firehose/stream") || strings.Contains(path, "/likes/sample10/stream") {
		log.Printf("Streaming likes firehose from: %s", path)
		// Firehose streams simulate continuous data - stream all tweets like sample stream
		streamLikesFirehose(w, r, state, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}
	
	if strings.Contains(path, "/likes/compliance/stream") {
		log.Printf("Streaming likes compliance from: %s", path)
		streamComplianceLikes(w, r, op, state, spec, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}

	if strings.Contains(path, "/tweets/compliance/stream") {
		log.Printf("Streaming compliance tweets from: %s", path)
		streamComplianceTweets(w, r, op, state, spec, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}

	if strings.Contains(path, "/users/compliance/stream") {
		log.Printf("Streaming compliance users from: %s", path)
		streamComplianceUsers(w, r, op, state, spec, queryParams, creditTracker, developerAccountID, path, method)
		return true
	}

	// Activity stream endpoint removed - not ready to support yet

	// Generic streaming - use example or generate from schema
	log.Printf("Streaming generic response from: %s", path)
	streamGeneric(w, op, state, spec, queryParams)
	return true
}

// streamSampleTweets streams sample tweets continuously until client disconnects
func streamSampleTweets(w http.ResponseWriter, r *http.Request, state *State, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Get flusher - responseTimeWriter implements http.Flusher if underlying writer supports it
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Error: ResponseWriter does not support flushing for streaming")
		return
	}
	
	ctx := r.Context()
	
	// Parse delay parameter (in milliseconds, default 200ms for faster streaming)
	// Enforce minimum delay of 10ms to prevent excessive CPU usage
	const MinStreamingDelayMs = 10
	delayMs := DefaultStreamingDelayMs
	if delayStr := r.URL.Query().Get("delay_ms"); delayStr != "" {
		if parsed, err := strconv.Atoi(delayStr); err == nil && parsed >= MinStreamingDelayMs && parsed <= MaxStreamingDelayMs {
			delayMs = parsed
		} else if parsed > 0 && parsed < MinStreamingDelayMs {
			// If delay is too small, use minimum
			delayMs = MinStreamingDelayMs
		}
	}
	ticker := time.NewTicker(time.Duration(delayMs) * time.Millisecond)
	defer ticker.Stop()

	// Get all tweets upfront and shuffle them to avoid duplicates
	state.mu.RLock()
	tweetList := make([]*Tweet, 0, len(state.tweets))
	for _, t := range state.tweets {
		tweetList = append(tweetList, t)
	}
	state.mu.RUnlock()

	if len(tweetList) == 0 {
		log.Printf("Warning: No tweets available for streaming")
		return
	}

	// Shuffle the tweet list to randomize order
	shuffled := make([]*Tweet, len(tweetList))
	copy(shuffled, tweetList)
	mathrand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Track recently sent tweet IDs to avoid immediate duplicates
	recentSent := make(map[string]bool)
	recentWindow := StreamingRecentWindow // Don't repeat within last N tweets
	recentQueue := make([]string, 0, recentWindow)
	
	count := 0

	// Stream continuously until client disconnects
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("Client disconnected from stream")
			return
		case <-ticker.C:
			// Find a tweet that hasn't been sent recently
			var tweet *Tweet
			attempts := 0
			maxAttempts := len(shuffled) * 2 // Try to find a unique tweet
			
			for attempts < maxAttempts {
				candidate := shuffled[count%len(shuffled)]
				if !recentSent[candidate.ID] {
					tweet = candidate
					break
				}
				count++
				attempts++
			}
			
			// If we couldn't find a unique one, just use the next one
			if tweet == nil {
				tweet = shuffled[count%len(shuffled)]
			}
			
			// Update recent sent tracking
			recentSent[tweet.ID] = true
			recentQueue = append(recentQueue, tweet.ID)
			if len(recentQueue) > recentWindow {
				// Remove oldest from tracking
				oldest := recentQueue[0]
				delete(recentSent, oldest)
				recentQueue = recentQueue[1:]
			}

			if tweet != nil {
				tweetMap := FormatTweet(tweet)
				// Apply field filtering
				if queryParams != nil && len(queryParams.TweetFields) > 0 {
					tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
				} else {
					tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
				}

				// Add expansion fields if requested
				if queryParams != nil && len(queryParams.Expansions) > 0 {
					addExpansionFieldsToTweet(tweetMap, tweet, queryParams.Expansions)
				}

				// Format as SSE event
				event := map[string]interface{}{
					"data": tweetMap,
				}

				if queryParams != nil && len(queryParams.Expansions) > 0 {
					includes := buildExpansions([]*Tweet{tweet}, queryParams.Expansions, state, nil, queryParams)
					if len(includes) > 0 {
						event["includes"] = includes
					}
				}

				eventJSON, _ := json.Marshal(event)
				_, err := fmt.Fprintf(w, "%s\n", eventJSON)
				if err != nil {
					// Client disconnected or connection error
					log.Printf("Error writing to stream: %v", err)
					return
				}
				flusher.Flush()
				
				// Track credit usage for each streamed tweet
				if creditTracker != nil {
					creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
				}
				
				count++
			}
		}
	}
}

// streamSearchTweets streams search results continuously
// Only streams tweets created AFTER the stream connection is established
func streamSearchTweets(w http.ResponseWriter, r *http.Request, state *State, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	ctx := r.Context()

	// Get flusher - responseTimeWriter implements http.Flusher if underlying writer supports it
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Error: ResponseWriter does not support flushing for streaming")
		return
	}

	// Parse delay parameter (in milliseconds, default 200ms)
	// Enforce minimum delay of 10ms to prevent excessive CPU usage
	const MinStreamingDelayMs = 10
	delayMs := DefaultStreamingDelayMs
	if delayStr := r.URL.Query().Get("delay_ms"); delayStr != "" {
		if parsed, err := strconv.Atoi(delayStr); err == nil && parsed >= MinStreamingDelayMs && parsed <= MaxStreamingDelayMs {
			delayMs = parsed
		} else if parsed > 0 && parsed < MinStreamingDelayMs {
			// If delay is too small, use minimum
			delayMs = MinStreamingDelayMs
		}
	}

	// Record stream start time - only stream tweets created AFTER this time
	streamStartTime := time.Now()

	// Get active search stream rules
	rules := state.GetSearchStreamRules()
	ruleMatcher := NewRuleMatcher(rules)

	// Filter tweets that match rules and track which rules matched
	type tweetWithRules struct {
		tweet        *Tweet
		matchedRules []*SearchStreamRule
	}
	
	// Track ALL tweets that have been sent (to prevent duplicates)
	sentTweetIDs := make(map[string]bool)
	
	// Helper function to get NEW matching tweets (created after stream started)
	// This allows us to refresh matching tweets periodically as rules or tweets change
	getNewMatchingTweets := func() []tweetWithRules {
		matchingTweetsWithRules := make([]tweetWithRules, 0)
		state.mu.RLock()
		allTweets := make([]*Tweet, 0, len(state.tweets))
		for _, t := range state.tweets {
			// Only include tweets created AFTER stream started
			if t.CreatedAt.After(streamStartTime) {
				allTweets = append(allTweets, t)
			}
		}
		state.mu.RUnlock()
		
		for _, tweet := range allTweets {
			// Skip tweets that have already been sent
			if sentTweetIDs[tweet.ID] {
				continue
			}
			
			matchedRules := make([]*SearchStreamRule, 0)
			for _, rule := range rules {
				if ruleMatcher.MatchRule(tweet, rule.Value, state) {
					matchedRules = append(matchedRules, rule)
				}
			}
			if len(matchedRules) > 0 {
				matchingTweetsWithRules = append(matchingTweetsWithRules, tweetWithRules{
					tweet:        tweet,
					matchedRules: matchedRules,
				})
			}
		}
		return matchingTweetsWithRules
	}

	ticker := time.NewTicker(time.Duration(delayMs) * time.Millisecond)
	defer ticker.Stop()
	lastRuleCheck := time.Now()
	ruleCheckInterval := 5 * time.Second // Re-check rules every 5 seconds

	// Stream continuously until client disconnects
	for {
		select {
		case <-ctx.Done():
			log.Printf("Client disconnected from search stream")
			return
		case <-ticker.C:
			// Periodically refresh rules in case rules changed
			if time.Since(lastRuleCheck) > ruleCheckInterval {
				rules = state.GetSearchStreamRules()
				ruleMatcher = NewRuleMatcher(rules)
				lastRuleCheck = time.Now()
			}
			
			// Get only NEW matching tweets (created after stream started and not yet sent)
			newMatchingTweets := getNewMatchingTweets()
			
			// If no new matching tweets, send keep-alive comment and continue
			if len(newMatchingTweets) == 0 {
				// Send keep-alive as empty line to keep connection open
				_, err := fmt.Fprintf(w, "\n")
				if err != nil {
					log.Printf("Error writing keep-alive to stream: %v", err)
					return
				}
				flusher.Flush()
				continue
			}
			
			// Send each new matching tweet exactly once
			for _, tweetWithRules := range newMatchingTweets {
				// Mark as sent immediately to prevent duplicates
				sentTweetIDs[tweetWithRules.tweet.ID] = true
				
				tweet := tweetWithRules.tweet

				tweetMap := FormatTweet(tweet)
				// Apply field filtering
				if queryParams != nil && len(queryParams.TweetFields) > 0 {
					tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
				} else {
					tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
				}

				// Add expansion fields if requested
				if queryParams != nil && len(queryParams.Expansions) > 0 {
					addExpansionFieldsToTweet(tweetMap, tweet, queryParams.Expansions)
				}

				event := map[string]interface{}{
					"data": tweetMap,
				}

				// Add matching rule IDs and tags
				matchingRules := make([]map[string]interface{}, len(tweetWithRules.matchedRules))
				for i, rule := range tweetWithRules.matchedRules {
					ruleInfo := map[string]interface{}{
						"id": rule.ID,
					}
					if rule.Tag != "" {
						ruleInfo["tag"] = rule.Tag
					}
					matchingRules[i] = ruleInfo
				}
				event["matching_rules"] = matchingRules

				if queryParams != nil && len(queryParams.Expansions) > 0 {
					includes := buildExpansions([]*Tweet{tweet}, queryParams.Expansions, state, nil, queryParams)
					// Always add includes object (even if empty) when expansions are requested
					// This matches real API behavior where includes is always present when requested
					event["includes"] = includes
				}

				eventJSON, _ := json.Marshal(event)
				_, err := fmt.Fprintf(w, "%s\n", eventJSON)
				if err != nil {
					log.Printf("Error writing to search stream: %v", err)
					return
				}
				flusher.Flush()
				
				// Track credit usage for each streamed tweet
				if creditTracker != nil {
					creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
				}
			}
		}
	}
}

// streamFirehoseTweets streams firehose tweets
func streamFirehoseTweets(w http.ResponseWriter, r *http.Request, state *State, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Similar to sample but potentially more tweets
	streamSampleTweets(w, r, state, queryParams, creditTracker, accountID, path, method)
}

// streamFirehoseTweetsByLanguage streams firehose tweets filtered by language
func streamFirehoseTweetsByLanguage(w http.ResponseWriter, r *http.Request, state *State, queryParams *QueryParams, lang string, creditTracker *CreditTracker, accountID, path, method string) {
	log.Printf("streamFirehoseTweetsByLanguage called for language: %s", lang)
	// Get flusher - responseTimeWriter implements http.Flusher if underlying writer supports it
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Error: ResponseWriter does not support flushing for streaming")
		return
	}
	log.Printf("streamFirehoseTweetsByLanguage: flusher obtained, starting stream loop")
	
	ctx := r.Context()
	
	// Parse delay parameter
	// Enforce minimum delay of 10ms to prevent excessive CPU usage
	const MinStreamingDelayMs = 10
	delayMs := DefaultStreamingDelayMs
	if delayStr := r.URL.Query().Get("delay_ms"); delayStr != "" {
		if parsed, err := strconv.Atoi(delayStr); err == nil && parsed >= MinStreamingDelayMs && parsed <= MaxStreamingDelayMs {
			delayMs = parsed
		} else if parsed > 0 && parsed < MinStreamingDelayMs {
			// If delay is too small, use minimum
			delayMs = MinStreamingDelayMs
		}
	}
	ticker := time.NewTicker(time.Duration(delayMs) * time.Millisecond)
	defer ticker.Stop()

	// Helper function to get filtered tweets by language
	getFilteredTweets := func() []*Tweet {
		state.mu.RLock()
		defer state.mu.RUnlock()
		tweetList := make([]*Tweet, 0)
		for _, t := range state.tweets {
			if t.Lang == lang {
				tweetList = append(tweetList, t)
			}
		}
		return tweetList
	}

	// Get initial tweet list
	tweetList := getFilteredTweets()
	log.Printf("Streaming firehose for language '%s': found %d tweets", lang, len(tweetList))
	
	// Shuffle and track recent
	shuffled := make([]*Tweet, 0)
	if len(tweetList) > 0 {
		shuffled = make([]*Tweet, len(tweetList))
		copy(shuffled, tweetList)
		mathrand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
	}

	recentSent := make(map[string]bool)
	recentWindow := 10
	recentQueue := make([]string, 0, recentWindow)
	count := 0
	refreshCounter := 0
	const refreshInterval = 50        // Refresh tweet list every 50 ticks when we have tweets
	const refreshIntervalEmpty = 10   // Refresh more frequently when we have no tweets

	// Send initial keepalive to establish connection immediately
	if len(shuffled) == 0 {
		_, err := fmt.Fprintf(w, "\n")
		if err != nil {
			log.Printf("streamFirehoseTweetsByLanguage: Error sending initial keepalive for language '%s': %v", lang, err)
			return
		}
		flusher.Flush()
	}

	log.Printf("streamFirehoseTweetsByLanguage: Entering main loop for language '%s'", lang)
	loopCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("streamFirehoseTweetsByLanguage: Context cancelled for language '%s'", lang)
			return
		case <-ticker.C:
			loopCount++
			if loopCount%10 == 0 {
				log.Printf("streamFirehoseTweetsByLanguage: Still streaming for language '%s', loop iteration %d, tweets available: %d", lang, loopCount, len(shuffled))
			}
			// Periodically refresh the tweet list to pick up new tweets
			refreshCounter++
			shouldRefresh := false
			if len(shuffled) == 0 {
				// Refresh more frequently when we have no tweets
				shouldRefresh = refreshCounter >= refreshIntervalEmpty
			} else {
				// Refresh less frequently when we have tweets
				shouldRefresh = refreshCounter >= refreshInterval
			}
			
			if shouldRefresh {
				refreshCounter = 0
				newTweetList := getFilteredTweets()
				if len(newTweetList) > 0 {
					// Update if we have tweets (either from empty to some, or if count changed)
					if len(shuffled) == 0 || len(newTweetList) != len(tweetList) {
						tweetList = newTweetList
						shuffled = make([]*Tweet, len(tweetList))
						copy(shuffled, tweetList)
						mathrand.Shuffle(len(shuffled), func(i, j int) {
							shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
						})
						// Reset count when updating tweet list
						count = 0
						log.Printf("Updated tweet list for language '%s': now have %d tweets", lang, len(shuffled))
					}
				}
			}

			// If no tweets available yet, send keepalive to maintain connection
			if len(shuffled) == 0 {
				// Send keepalive as empty line to keep connection alive
				_, err := fmt.Fprintf(w, "\n")
				if err != nil {
					return
				}
				flusher.Flush()
				continue
			}

			var tweet *Tweet
			attempts := 0
			maxAttempts := len(shuffled) * 2
			
			for attempts < maxAttempts {
				candidate := shuffled[count%len(shuffled)]
				if !recentSent[candidate.ID] {
					tweet = candidate
					break
				}
				count++
				attempts++
			}
			
			if tweet == nil {
				tweet = shuffled[count%len(shuffled)]
			}

			recentSent[tweet.ID] = true
			recentQueue = append(recentQueue, tweet.ID)
			if len(recentQueue) > recentWindow {
				oldest := recentQueue[0]
				delete(recentSent, oldest)
				recentQueue = recentQueue[1:]
			}

			tweetMap := FormatTweet(tweet)
			if queryParams != nil && len(queryParams.TweetFields) > 0 {
				tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
			} else {
				tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
			}

			if queryParams != nil && len(queryParams.Expansions) > 0 {
				addExpansionFieldsToTweet(tweetMap, tweet, queryParams.Expansions)
			}

			event := map[string]interface{}{
				"data": tweetMap,
			}

			if queryParams != nil && len(queryParams.Expansions) > 0 {
				includes := buildExpansions([]*Tweet{tweet}, queryParams.Expansions, state, nil, queryParams)
				// Always add includes object (even if empty) when expansions are requested
				// This matches real API behavior where includes is always present when requested
				event["includes"] = includes
			}

			eventJSON, _ := json.Marshal(event)
			_, err := fmt.Fprintf(w, "%s\n", eventJSON)
			if err != nil {
				return
			}
			flusher.Flush()
			
			// Track credit usage for each streamed tweet
			if creditTracker != nil {
				creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
			}
			
			count++
		}
	}
}

// generateLikeEventID generates a unique hex ID for a like event (32 chars)
func generateLikeEventID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// streamLikesFirehose streams likes firehose - simulates continuous stream of like events
// This streams like events (not tweets) matching the real API format
func streamLikesFirehose(w http.ResponseWriter, r *http.Request, state *State, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Get flusher - responseTimeWriter implements http.Flusher if underlying writer supports it
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Error: ResponseWriter does not support flushing for streaming")
		return
	}
	
	ctx := r.Context()
	
	// Parse delay parameter (in milliseconds, default 200ms for faster streaming)
	// Enforce minimum delay of 10ms to prevent excessive CPU usage
	const MinStreamingDelayMs = 10
	delayMs := DefaultStreamingDelayMs
	if delayStr := r.URL.Query().Get("delay_ms"); delayStr != "" {
		if parsed, err := strconv.Atoi(delayStr); err == nil && parsed >= MinStreamingDelayMs && parsed <= MaxStreamingDelayMs {
			delayMs = parsed
		} else if parsed > 0 && parsed < MinStreamingDelayMs {
			// If delay is too small, use minimum
			delayMs = MinStreamingDelayMs
		}
	}
	ticker := time.NewTicker(time.Duration(delayMs) * time.Millisecond)
	defer ticker.Stop()

	// Build list of like events from existing like relationships
	// Each like relationship (tweet.LikedBy) represents a like event
	type likeEvent struct {
		tweetID      string
		authorID    string
		likedAt     time.Time
	}
	
	state.mu.RLock()
	// Get all tweets and users for simulation
	tweetList := make([]*Tweet, 0, len(state.tweets))
	for _, t := range state.tweets {
		tweetList = append(tweetList, t)
	}
	userList := make([]*User, 0, len(state.users))
	for _, u := range state.users {
		userList = append(userList, u)
	}
	
	likeEvents := make([]likeEvent, 0)
	for _, tweet := range state.tweets {
		if len(tweet.LikedBy) > 0 {
			// For each user who liked this tweet, create a like event
			// Use tweet creation time as base, add some variation
			for range tweet.LikedBy {
				// Simulate like happening after tweet creation
				likedAt := tweet.CreatedAt.Add(time.Duration(mathrand.Intn(3600)) * time.Second)
				likeEvents = append(likeEvents, likeEvent{
					tweetID:   tweet.ID,
					authorID:  tweet.AuthorID,
					likedAt:   likedAt,
				})
			}
		}
	}
	state.mu.RUnlock()

	// If no like events exist but we have tweets and users, simulate like events
	if len(likeEvents) == 0 && len(tweetList) > 0 && len(userList) > 0 {
		log.Printf("No existing likes found, simulating like events for firehose stream")
		// Generate simulated like events from tweets and users
		// Create multiple like events per tweet for variety
		simulatedCount := len(tweetList) * 3 // 3 likes per tweet on average
		if simulatedCount > 100 {
			simulatedCount = 100 // Cap at 100 for performance
		}
		
		state.mu.RLock()
		for i := 0; i < simulatedCount && i < len(tweetList)*3; i++ {
			tweet := tweetList[mathrand.Intn(len(tweetList))]
			// Simulate like happening after tweet creation (within last hour)
			likedAt := tweet.CreatedAt.Add(time.Duration(mathrand.Intn(3600)) * time.Second)
			// Ensure likedAt is not in the future
			now := time.Now()
			if likedAt.After(now) {
				likedAt = now.Add(-time.Duration(mathrand.Intn(3600)) * time.Second)
			}
			likeEvents = append(likeEvents, likeEvent{
				tweetID:   tweet.ID,
				authorID:  tweet.AuthorID,
				likedAt:   likedAt,
			})
		}
		state.mu.RUnlock()
	}

	if len(likeEvents) == 0 {
		// If no tweets/users exist, send keep-alive messages to keep connection open
		log.Printf("Warning: No tweets or users available for likes firehose streaming, sending keep-alive")
		for {
			select {
			case <-ctx.Done():
				log.Printf("Client disconnected from likes firehose stream")
				return
			case <-ticker.C:
				// Send keep-alive as empty line to keep connection open
				_, err := fmt.Fprintf(w, "\n")
				if err != nil {
					log.Printf("Error writing keep-alive to likes firehose stream: %v", err)
					return
				}
				flusher.Flush()
			}
		}
	}

	// Shuffle the like events to randomize order
	shuffled := make([]likeEvent, len(likeEvents))
	copy(shuffled, likeEvents)
	mathrand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Track recently sent like event IDs to avoid immediate duplicates
	recentSent := make(map[string]bool)
	recentWindow := StreamingRecentWindow
	recentQueue := make([]string, 0, recentWindow)
	
	count := 0

	// Stream continuously until client disconnects
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("Client disconnected from likes firehose stream")
			return
		case <-ticker.C:
			// Pick a like event
			likeEvt := shuffled[count%len(shuffled)]
			count++

			// Generate unique like event ID
			likeEventID := generateLikeEventID()
			
			// Ensure we haven't sent this exact like event recently
			eventKey := fmt.Sprintf("%s-%s", likeEvt.tweetID, likeEvt.authorID)
			if recentSent[eventKey] {
				// Skip if we've sent this recently
				continue
			}
			
			// Update recent sent tracking
			recentSent[eventKey] = true
			recentQueue = append(recentQueue, eventKey)
			if len(recentQueue) > recentWindow {
				// Remove oldest from tracking
				oldest := recentQueue[0]
				delete(recentSent, oldest)
				recentQueue = recentQueue[1:]
			}

			// Format timestamp
			timestampMs := likeEvt.likedAt.UnixMilli()
			createdAt := likeEvt.likedAt.Format(time.RFC3339Nano)
			// Remove nanoseconds and add milliseconds precision
			if len(createdAt) > 19 {
				createdAt = createdAt[:19] + ".000Z"
			}

			// Build like event data matching real API format
			likeData := map[string]interface{}{
				"id":                  likeEventID,
				"created_at":          createdAt,
				"liked_tweet_id":      likeEvt.tweetID,
				"liked_tweet_author_id": likeEvt.authorID,
				"timestamp_ms":        fmt.Sprintf("%d", timestampMs),
			}

			// Format as SSE event - only include includes if expansions are requested
			event := map[string]interface{}{
				"data": likeData,
			}

			// Add includes only if expansions are requested
			if queryParams != nil && len(queryParams.Expansions) > 0 {
				// Check if user expansion is requested
				hasUserExpansion := false
				for _, exp := range queryParams.Expansions {
					if exp == "author_id" || strings.Contains(exp, "author_id") || strings.Contains(exp, "user") {
						hasUserExpansion = true
						break
					}
				}

				if hasUserExpansion {
					// Get the author user for includes
					state.mu.RLock()
					author := state.users[likeEvt.authorID]
					state.mu.RUnlock()

					if author != nil {
						// Build includes with the author of the liked tweet
						includes := map[string]interface{}{
							"users": []map[string]interface{}{
								{
									"id":       author.ID,
									"name":     author.Name,
									"username": author.Username,
								},
							},
						}
						event["includes"] = includes
					}
				}
			}

			eventJSON, _ := json.Marshal(event)
			_, err := fmt.Fprintf(w, "%s\n", eventJSON)
			if err != nil {
				// Client disconnected or connection error
				log.Printf("Error writing to likes firehose stream: %v", err)
				return
			}
			flusher.Flush()
			
			// Track credit usage for each streamed like event
			if creditTracker != nil {
				creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
			}
		}
	}
}

// streamComplianceTweets streams tweets for compliance using OpenAPI spec
func streamComplianceTweets(w http.ResponseWriter, r *http.Request, op *EndpointOperation, state *State, spec *OpenAPISpec, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Use OpenAPI spec to generate compliance events
	if op != nil && op.Operation != nil {
		streamComplianceFromSchema(w, r, op, state, spec, queryParams, creditTracker, accountID, path, method)
	} else {
		// Fallback to generic streaming
		streamGeneric(w, op, state, spec, queryParams)
	}
}

// streamComplianceLikes streams likes for compliance using OpenAPI spec
func streamComplianceLikes(w http.ResponseWriter, r *http.Request, op *EndpointOperation, state *State, spec *OpenAPISpec, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Use OpenAPI spec to generate compliance events
	// streamComplianceFromSchema handles nil op by building structure manually
	streamComplianceFromSchema(w, r, op, state, spec, queryParams, creditTracker, accountID, path, method)
}

// streamComplianceUsers streams user compliance events using OpenAPI spec
func streamComplianceUsers(w http.ResponseWriter, r *http.Request, op *EndpointOperation, state *State, spec *OpenAPISpec, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Use OpenAPI spec to generate compliance events
	if op != nil && op.Operation != nil {
		streamComplianceFromSchema(w, r, op, state, spec, queryParams, creditTracker, accountID, path, method)
	} else {
		// Fallback to generic streaming
		streamGeneric(w, op, state, spec, queryParams)
	}
}

// extractLanguageFromPath extracts language code from path like /2/tweets/firehose/stream/lang/en
func extractLanguageFromPath(path string) string {
	parts := strings.Split(path, "/lang/")
	if len(parts) > 1 {
		lang := parts[1]
		// Remove any trailing path segments
		if idx := strings.Index(lang, "/"); idx != -1 {
			lang = lang[:idx]
		}
		// Remove query parameters if present
		if idx := strings.Index(lang, "?"); idx != -1 {
			lang = lang[:idx]
		}
		log.Printf("extractLanguageFromPath: extracted language '%s' from path '%s'", lang, path)
		return lang
	}
	log.Printf("extractLanguageFromPath: no language found in path '%s', defaulting to 'en'", path)
	return "en" // default
}

// streamComplianceFromSchema streams compliance events using OpenAPI spec response schema
func streamComplianceFromSchema(w http.ResponseWriter, r *http.Request, op *EndpointOperation, state *State, spec *OpenAPISpec, queryParams *QueryParams, creditTracker *CreditTracker, accountID, path, method string) {
	// Get flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("Error: ResponseWriter does not support flushing for streaming")
		return
	}
	
	ctx := r.Context()
	
	// Parse delay parameter
	const MinStreamingDelayMs = 10
	delayMs := DefaultStreamingDelayMs
	if delayStr := r.URL.Query().Get("delay_ms"); delayStr != "" {
		if parsed, err := strconv.Atoi(delayStr); err == nil && parsed >= MinStreamingDelayMs && parsed <= MaxStreamingDelayMs {
			delayMs = parsed
		} else if parsed > 0 && parsed < MinStreamingDelayMs {
			delayMs = MinStreamingDelayMs
		}
	}
	ticker := time.NewTicker(time.Duration(delayMs) * time.Millisecond)
	defer ticker.Stop()

	// Get response schema from OpenAPI spec
	var responseSchema map[string]interface{}
	if op != nil && op.Operation != nil {
		responseSchema = op.Operation.GetResponseSchema("200")
		if responseSchema == nil {
			responseSchema = op.Operation.GetResponseSchema("default")
		}
	}

	if responseSchema == nil {
		log.Printf("Warning: No response schema found for compliance stream, using default structure")
		// Continue with default structure - we'll build it manually
	}

	// Track compliance event counts (incrementing over time)
	eventCounts := map[string]int{
		"user_profile_modification": 14,
		"user_delete":                3,
		"user_suspend":               14,
		"user_undelete":              2,
	}

	// Additional event types that may appear later
	additionalEvents := []string{"user_unprotect", "user_protect"}

	count := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate compliance event from schema (if available)
			var baseResponse interface{}
			if responseSchema != nil {
				baseResponse = GenerateMockResponse(responseSchema, spec)
			}
			
			// For user compliance, replace with event counts format
			// The real API returns dictionaries with event type counts
			if strings.Contains(r.URL.Path, "/users/compliance/stream") {
				// Randomly increment one of the counts
				eventTypes := make([]string, 0, len(eventCounts))
				for k := range eventCounts {
					eventTypes = append(eventTypes, k)
				}
				if len(additionalEvents) > 0 && count > 0 && count%30 == 0 {
					// Occasionally add new event types
					newEvent := additionalEvents[mathrand.Intn(len(additionalEvents))]
					if _, exists := eventCounts[newEvent]; !exists {
						eventCounts[newEvent] = 1
					}
				}
				
				// Increment a random event count
				if len(eventTypes) > 0 {
					eventToIncrement := eventTypes[mathrand.Intn(len(eventTypes))]
					eventCounts[eventToIncrement]++
				}
				
				// Create response matching real API format
				response := map[string]interface{}{
					"data": eventCounts,
				}
				
				eventJSON, err := json.Marshal(response)
				if err != nil {
					log.Printf("Error marshaling compliance event: %v", err)
					continue
				}
				
				_, err = fmt.Fprintf(w, "%s\n", eventJSON)
				if err != nil {
					return
				}
				flusher.Flush()
				
				// Track credit usage for each streamed compliance event
				if creditTracker != nil {
					creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
				}
			} else if strings.Contains(r.URL.Path, "/tweets/compliance/stream") {
				// For tweet compliance, generate compliance events using schema structure
				// Get tweets from state to generate compliance events
				state.mu.RLock()
				tweetList := make([]*Tweet, 0, len(state.tweets))
				for _, t := range state.tweets {
					tweetList = append(tweetList, t)
				}
				state.mu.RUnlock()

				if len(tweetList) == 0 {
					// Send keep-alive if no tweets
					fmt.Fprintf(w, "\n")
					flusher.Flush()
					count++
					continue
				}

				// Pick a random tweet for the compliance event
				tweet := tweetList[mathrand.Intn(len(tweetList))]
				
				// Use schema to determine structure, but populate with actual tweet data
				// The real API returns: {"data": {"delete": {"tweet": {"id": "...", "author_id": "..."}, "event_at": "..."}}}
				// Try to extract structure from schema, or build manually
				var complianceEvent map[string]interface{}
				
				// Check if baseResponse has the right structure
				if baseRespMap, ok := baseResponse.(map[string]interface{}); ok {
					// Try to use schema structure
					complianceEvent = baseRespMap
					
					// Ensure data.delete structure exists
					dataObj, ok := complianceEvent["data"].(map[string]interface{})
					if !ok {
						dataObj = make(map[string]interface{})
						complianceEvent["data"] = dataObj
					}
					
					// Ensure delete structure exists
					deleteObj, ok := dataObj["delete"].(map[string]interface{})
					if !ok {
						deleteObj = make(map[string]interface{})
						dataObj["delete"] = deleteObj
					}
					
					// Ensure tweet structure exists
					tweetObj, ok := deleteObj["tweet"].(map[string]interface{})
					if !ok {
						tweetObj = make(map[string]interface{})
						deleteObj["tweet"] = tweetObj
					}
					
					// Populate with actual tweet data
					tweetObj["id"] = tweet.ID
					tweetObj["author_id"] = tweet.AuthorID
					
					// Set event_at timestamp (current time in RFC3339 format)
					now := time.Now().UTC()
					formatted := now.Format(time.RFC3339Nano)
					deleteObj["event_at"] = formatted[:len(formatted)-4] + "Z"
					
					// Optionally include quote_tweet_id if tweet is a quote
					for _, ref := range tweet.ReferencedTweets {
						if ref.Type == "quoted" {
							deleteObj["quote_tweet_id"] = ref.ID
							break
						}
					}
				} else {
					// Fallback: build structure manually matching real API
					now := time.Now().UTC()
					formatted := now.Format(time.RFC3339Nano)
					complianceEvent = map[string]interface{}{
						"data": map[string]interface{}{
							"delete": map[string]interface{}{
								"tweet": map[string]interface{}{
									"id":        tweet.ID,
									"author_id": tweet.AuthorID,
								},
								"event_at": formatted[:len(formatted)-4] + "Z",
							},
						},
					}
					// Optionally include quote_tweet_id if tweet is a quote
					for _, ref := range tweet.ReferencedTweets {
						if ref.Type == "quoted" {
							if deleteObj, ok := complianceEvent["data"].(map[string]interface{})["delete"].(map[string]interface{}); ok {
								deleteObj["quote_tweet_id"] = ref.ID
							}
							break
						}
					}
				}
				
				eventJSON, err := json.Marshal(complianceEvent)
				if err != nil {
					log.Printf("Error marshaling compliance event: %v", err)
					continue
				}
				_, err = fmt.Fprintf(w, "%s\n", eventJSON)
				if err != nil {
					return
				}
				flusher.Flush()
				
				// Track credit usage for each streamed compliance tweet event
				if creditTracker != nil {
					creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
				}
			} else if strings.Contains(r.URL.Path, "/likes/compliance/stream") {
				// For likes compliance, generate compliance events using schema structure
				// Get tweets and users from state to generate like compliance events
				state.mu.RLock()
				tweetList := make([]*Tweet, 0, len(state.tweets))
				for _, t := range state.tweets {
					tweetList = append(tweetList, t)
				}
				userList := make([]*User, 0, len(state.users))
				for _, u := range state.users {
					userList = append(userList, u)
				}
				state.mu.RUnlock()

				if len(tweetList) == 0 || len(userList) == 0 {
					// Send keep-alive if no data
					fmt.Fprintf(w, "\n")
					flusher.Flush()
					count++
					continue
				}

				// Pick a random tweet for the like compliance event
				tweet := tweetList[mathrand.Intn(len(tweetList))]
				
				// Generate a like event ID (similar to likes firehose)
				likeEventIDBytes := make([]byte, 16)
				rand.Read(likeEventIDBytes)
				likeEventID := hex.EncodeToString(likeEventIDBytes)
				
				// Use schema to determine structure, but populate with actual data
				// The real API returns: {"data": {"delete": {"like": {"id": "...", "liked_tweet_id": "...", "liked_tweet_author_id": "..."}, "event_at": "..."}}}
				var complianceEvent map[string]interface{}
				
				// Check if baseResponse has the right structure
				if baseRespMap, ok := baseResponse.(map[string]interface{}); ok {
					// Try to use schema structure
					complianceEvent = baseRespMap
					
					// Ensure data.delete structure exists
					dataObj, ok := complianceEvent["data"].(map[string]interface{})
					if !ok {
						dataObj = make(map[string]interface{})
						complianceEvent["data"] = dataObj
					}
					
					// Ensure delete structure exists
					deleteObj, ok := dataObj["delete"].(map[string]interface{})
					if !ok {
						deleteObj = make(map[string]interface{})
						dataObj["delete"] = deleteObj
					}
					
					// Ensure like structure exists
					likeObj, ok := deleteObj["like"].(map[string]interface{})
					if !ok {
						likeObj = make(map[string]interface{})
						deleteObj["like"] = likeObj
					}
					
					// Populate with actual like data
					likeObj["id"] = likeEventID
					likeObj["liked_tweet_id"] = tweet.ID
					likeObj["liked_tweet_author_id"] = tweet.AuthorID
					
					// Set event_at timestamp (current time in RFC3339 format)
					now := time.Now().UTC()
					formatted := now.Format(time.RFC3339Nano)
					deleteObj["event_at"] = formatted[:len(formatted)-4] + "Z"
				} else {
					// Fallback: build structure manually matching real API
					now := time.Now().UTC()
					formatted := now.Format(time.RFC3339Nano)
					complianceEvent = map[string]interface{}{
						"data": map[string]interface{}{
							"delete": map[string]interface{}{
								"like": map[string]interface{}{
									"id":                  likeEventID,
									"liked_tweet_id":      tweet.ID,
									"liked_tweet_author_id": tweet.AuthorID,
								},
								"event_at": formatted[:len(formatted)-4] + "Z",
							},
					},
				}
			}
			
			eventJSON, err := json.Marshal(complianceEvent)
			if err != nil {
				log.Printf("Error marshaling compliance event: %v", err)
				continue
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", eventJSON)
			if err != nil {
				return
			}
			flusher.Flush()
			
			// Track credit usage for each streamed compliance like event
			if creditTracker != nil {
				creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
			}
		} else {
			// For other compliance streams, use schema-generated response
				if baseResponse == nil {
					// If no schema response, send keep-alive
					fmt.Fprintf(w, "\n")
					flusher.Flush()
					count++
					continue
				}
				eventJSON, err := json.Marshal(baseResponse)
				if err != nil {
					log.Printf("Error marshaling compliance event: %v", err)
					continue
				}
				_, err = fmt.Fprintf(w, "%s\n", eventJSON)
				if err != nil {
					return
				}
				flusher.Flush()
				
				// Track credit usage for each streamed compliance event
				if creditTracker != nil {
					creditTracker.TrackUsage(accountID, method, path, eventJSON, http.StatusOK)
				}
			}
			
			flusher.Flush()
			count++
		}
	}
}

// streamGeneric streams a generic response from schema
func streamGeneric(w http.ResponseWriter, op *EndpointOperation, state *State, spec *OpenAPISpec, queryParams *QueryParams) {
	// Log stack trace to see where this is being called from
	log.Printf("streamGeneric called (op=%v) - WARNING: This should not be called for firehose endpoints!", op != nil)
	if op != nil && op.Operation != nil {
		log.Printf("streamGeneric: operation path: %s", op.Path)
	}
	// Get response schema
	if op == nil || op.Operation == nil {
		log.Printf("streamGeneric: op or op.Operation is nil, returning early")
		return
	}
	
	responseSchema := op.Operation.GetResponseSchema("200")
	if responseSchema == nil {
		responseSchema = op.Operation.GetResponseSchema("default")
	}

	if responseSchema != nil {
		// Generate a response
		response := GenerateMockResponse(responseSchema, spec)
		eventJSON, err := json.Marshal(response)
		if err != nil {
			log.Printf("Error marshaling stream response: %v", err)
			return
		}
		_, err = fmt.Fprintf(w, "%s\n", eventJSON)
		if err != nil {
			// Client disconnected or connection error
			log.Printf("Error writing to stream: %v", err)
			return
		}
		
		// Get flusher - responseTimeWriter implements http.Flusher if underlying writer supports it
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}


