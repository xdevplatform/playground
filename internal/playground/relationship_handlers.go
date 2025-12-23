// Package playground handles user relationship and timeline endpoints.
//
// This file processes endpoints for user relationships (following, blocking,
// muting, likes, retweets, bookmarks) and user timelines (tweets, mentions,
// reposts). It includes pagination support and proper state management for
// all relationship operations.
package playground

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)


// handleUserRelationshipEndpoints handles user relationship endpoints
func handleUserRelationshipEndpoints(path, method string, r *http.Request, state *State, spec *OpenAPISpec, queryParams *QueryParams, op *Operation, pathItem *PathItem) ([]byte, int) {
	// Normalize path: remove query parameters and trailing slashes
	normalizedPath := strings.Split(path, "?")[0]
	normalizedPath = strings.TrimSuffix(normalizedPath, "/")
	
	// GET /2/users/{id}/tweets
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && (strings.HasSuffix(normalizedPath, "/tweets") || normalizedPath == "/2/users/tweets") {
		userID := extractUserIDFromPath(normalizedPath, "/tweets")
		if userID == "" {
			// Malformed path - return error
			return formatResourceNotFoundError("user", "id", "unknown"), http.StatusOK
		}
		user := state.GetUserByID(userID)
		if user == nil {
			// Try username
			user = state.GetUserByUsername(userID)
		}
		if user != nil {
			tweets := state.GetTweets(user.Tweets)
			// Apply time filtering (since_id, until_id, start_time, end_time)
			tweets = filterTweetsByTime(tweets, r)
			// Sort by created_at descending (newest first)
			sortTweetsByCreatedAt(tweets, true)
			// Apply pagination
			tweets, nextToken, err := applyPagination(tweets, r, op, spec, pathItem)
			if err != nil {
				errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
				data, statusCode := MarshalJSONErrorResponse(errorResp)
				return data, statusCode
			}
			// Apply field filtering
			return formatTweetsResponse(tweets, queryParams, state, spec, nextToken), http.StatusOK
		} else {
			// User not found - return 404 Not Found error
			return formatResourceNotFoundError("user", "id", userID), http.StatusOK
		}
	}

	// GET /2/users/{id}/timelines/reverse_chronological
	// Returns home timeline (tweets from accounts user follows) in reverse chronological order (newest first)
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/timelines/reverse_chronological") {
		userID := extractUserIDFromPath(normalizedPath, "/timelines/reverse_chronological")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				// Home timeline includes tweets from accounts the user follows
				// Build set of user IDs to include (user themselves + accounts they follow)
				followingSet := make(map[string]bool)
				followingSet[user.ID] = true // Include user's own tweets
				for _, followingID := range user.Following {
					followingSet[followingID] = true
				}
				
				// Get all tweets and filter by author ID
				state.mu.RLock()
				timelineTweets := make([]*Tweet, 0)
				for _, tweet := range state.tweets {
					if followingSet[tweet.AuthorID] {
						timelineTweets = append(timelineTweets, tweet)
					}
				}
				state.mu.RUnlock()
				
				// Apply time filtering
				timelineTweets = filterTweetsByTime(timelineTweets, r)
				// Sort by created_at descending (newest first - reverse chronological)
				sortTweetsByCreatedAt(timelineTweets, true)
				// Apply pagination
				timelineTweets, nextToken, err := applyPagination(timelineTweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(timelineTweets, queryParams, state, spec, nextToken), http.StatusOK
			} else {
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/liked_tweets
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/liked_tweets") {
		userID := extractUserIDFromPath(normalizedPath, "/liked_tweets")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				tweets := state.GetTweets(user.LikedTweets)
				// Apply time filtering
				tweets = filterTweetsByTime(tweets, r)
				// Sort by created_at descending
				sortTweetsByCreatedAt(tweets, true)
				tweets, nextToken, err := applyPagination(tweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(tweets, queryParams, state, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/followers
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/followers") {
		userID := extractUserIDFromPath(normalizedPath, "/followers")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				users := state.GetUsers(user.Followers)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/following
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/following") {
		userID := extractUserIDFromPath(normalizedPath, "/following")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				users := state.GetUsers(user.Following)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{source_user_id}/following/{target_user_id}
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.Contains(normalizedPath, "/following/") {
		parts := strings.Split(normalizedPath, "/following/")
		if len(parts) == 2 {
			sourceID := extractUserIDFromPath(parts[0], "")
			targetID := parts[1]
			
			sourceUser := state.GetUserByID(sourceID)
			if sourceUser == nil {
				sourceUser = state.GetUserByUsername(sourceID)
			}
			
			if sourceUser != nil {
				following := false
				for _, id := range sourceUser.Following {
					if id == targetID {
						following = true
						break
					}
				}
				
				response := map[string]interface{}{
					"data": map[string]bool{
						"following": following,
					},
				}
				data, statusCode := MarshalJSONResponse(response)
				return data, statusCode
			}
		}
	}

	// GET /2/users/{id}/lists (owned lists)
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/lists") && !strings.Contains(normalizedPath, "/list_memberships") {
		userID := extractUserIDFromPath(normalizedPath, "/lists")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				lists := state.GetLists(user.Lists)
				return formatListsResponse(lists, queryParams, spec), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/list_memberships
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/list_memberships") {
		userID := extractUserIDFromPath(normalizedPath, "/list_memberships")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				lists := state.GetLists(user.ListMemberships)
				return formatListsResponse(lists, queryParams, spec), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/mentions
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/mentions") {
		userID := extractUserIDFromPath(normalizedPath, "/mentions")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				// Search for tweets mentioning this user
				tweets := findMentions(r.Context(), user.Username, state)
				// Apply time filtering
				tweets = filterTweetsByTime(tweets, r)
				// Sort by created_at descending
				sortTweetsByCreatedAt(tweets, true)
				tweets, nextToken, err := applyPagination(tweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(tweets, queryParams, state, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/bookmarks
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/bookmarks") {
		userID := extractUserIDFromPath(normalizedPath, "/bookmarks")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				tweets := state.GetTweets(user.BookmarkedTweets)
				// Apply time filtering
				tweets = filterTweetsByTime(tweets, r)
				// Sort by created_at descending
				sortTweetsByCreatedAt(tweets, true)
				tweets, nextToken, err := applyPagination(tweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(tweets, queryParams, state, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/followed_lists
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/followed_lists") {
		userID := extractUserIDFromPath(normalizedPath, "/followed_lists")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				lists := state.GetLists(user.FollowedLists)
				return formatListsResponse(lists, queryParams, spec), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/muting
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/muting") {
		userID := extractUserIDFromPath(normalizedPath, "/muting")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				users := state.GetUsers(user.MutedUsers)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	// GET /2/users/{id}/blocking
	if method == "GET" && strings.Contains(normalizedPath, "/users/") && strings.HasSuffix(normalizedPath, "/blocking") {
		userID := extractUserIDFromPath(normalizedPath, "/blocking")
		if userID != "" {
			user := state.GetUserByID(userID)
			if user == nil {
				user = state.GetUserByUsername(userID)
			}
			if user != nil {
				users := state.GetUsers(user.BlockedUsers)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			} else {
				// User not found - return proper error response (200 OK with errors, matching X API behavior)
				return formatResourceNotFoundError("user", "id", userID), http.StatusOK
			}
		}
	}

	return nil, 0
}

// formatResourceNotFoundError formats a resource not found error in X API format
func formatResourceNotFoundError(resourceType, parameter, resourceID string) []byte {
	errorResponse := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"value":         resourceID,
				"detail":       fmt.Sprintf("Could not find %s with %s: [%s].", resourceType, parameter, resourceID),
				"title":        "Not Found Error",
				"resource_type": resourceType,
				"parameter":     parameter,
				"resource_id":  resourceID,
				"type":         "https://api.twitter.com/2/problems/resource-not-found",
				"code":         50, // X API error code 50 = Not Found
			},
		},
	}
	
	data, _ := MarshalJSONErrorResponse(errorResponse)
	return data
}

// handleTweetRelationshipEndpoints handles tweet relationship endpoints
func handleTweetRelationshipEndpoints(path, method string, r *http.Request, state *State, spec *OpenAPISpec, queryParams *QueryParams, op *Operation, pathItem *PathItem) ([]byte, int) {
	// GET /2/tweets/{id}/liking_users
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/liking_users") {
		// Normalize path
		normalizedPath := strings.TrimSuffix(strings.Split(path, "?")[0], "/")
		// Extract tweet ID from path like /2/tweets/0/liking_users
		parts := strings.Split(normalizedPath, "/tweets/")
		if len(parts) == 2 {
			tweetID := strings.TrimSuffix(parts[1], "/liking_users")
			if tweetID != "" {
				tweet := state.GetTweet(tweetID)
				if tweet != nil {
				users := state.GetUsers(tweet.LikedBy)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
					if err != nil {
						errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
						data, _ := MarshalJSONErrorResponse(errorResp)
						return data, http.StatusBadRequest
					}
					return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
				}
			}
		}
	}

	// GET /2/tweets/{id}/retweeted_by
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/retweeted_by") {
		tweetID := extractPathParam(path, "/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/retweeted_by")
		if tweetID != "" {
			tweet := state.GetTweet(tweetID)
			if tweet != nil {
				users := state.GetUsers(tweet.RetweetedBy)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			}
		}
	}

	// GET /2/tweets/{id}/retweets
	// Returns users who retweeted (similar to retweeted_by, but this endpoint returns users in retweets format)
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/retweets") {
		tweetID := extractPathParam(path, "/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/retweets")
		if tweetID != "" {
			tweet := state.GetTweet(tweetID)
			if tweet != nil {
				users := state.GetUsers(tweet.RetweetedBy)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			}
		}
	}

	// GET /2/tweets/{id}/quote_tweets
	if method == "GET" && strings.Contains(path, "/tweets/") && strings.HasSuffix(path, "/quote_tweets") {
		tweetID := extractPathParam(path, "/tweets/")
		tweetID = strings.TrimSuffix(tweetID, "/quote_tweets")
		if tweetID != "" {
			tweet := state.GetTweet(tweetID)
			if tweet != nil {
				tweets := state.GetTweets(tweet.Quotes)
				// Apply time filtering
				tweets = filterTweetsByTime(tweets, r)
				// Sort by created_at descending
				sortTweetsByCreatedAt(tweets, true)
				tweets, nextToken, err := applyPagination(tweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(tweets, queryParams, state, spec, nextToken), http.StatusOK
			}
		}
	}

	return nil, 0
}

// handleListRelationshipEndpoints handles list relationship endpoints
func handleListRelationshipEndpoints(path, method string, r *http.Request, state *State, spec *OpenAPISpec, queryParams *QueryParams, op *Operation, pathItem *PathItem) ([]byte, int) {
	// GET /2/lists/{id}/members
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/members") {
		listID := extractPathParam(path, "/lists/")
		listID = strings.TrimSuffix(listID, "/members")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				users := state.GetUsers(list.Members)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			}
		}
	}

	// GET /2/lists/{id}/followers
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/followers") {
		listID := extractPathParam(path, "/lists/")
		listID = strings.TrimSuffix(listID, "/followers")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				users := state.GetUsers(list.Followers)
				users, nextToken, err := applyUserPagination(users, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatUsersResponse(users, queryParams, spec, nextToken), http.StatusOK
			}
		}
	}

	// GET /2/lists/{id}/tweets
	if method == "GET" && strings.Contains(path, "/lists/") && strings.HasSuffix(path, "/tweets") {
		listID := extractPathParam(path, "/lists/")
		listID = strings.TrimSuffix(listID, "/tweets")
		if listID != "" {
			list := state.GetList(listID)
			if list != nil {
				var allTweets []*Tweet
				for _, userID := range list.Members {
					user := state.GetUserByID(userID)
					if user != nil {
						userTweets := state.GetTweets(user.Tweets)
						allTweets = append(allTweets, userTweets...)
					}
				}
				// Apply time filtering
				allTweets = filterTweetsByTime(allTweets, r)
				// Sort by created_at descending
				sortTweetsByCreatedAt(allTweets, true)
				allTweets, nextToken, err := applyPagination(allTweets, r, op, spec, pathItem)
				if err != nil {
					errorResp := CreateValidationErrorResponse("max_results", r.URL.Query().Get("max_results"), err.Error())
					data, _ := MarshalJSONErrorResponse(errorResp)
					return data, http.StatusBadRequest
				}
				return formatTweetsResponse(allTweets, queryParams, state, spec, nextToken), http.StatusOK
			}
		}
	}

	return nil, 0
}

// Helper functions

func extractUserIDFromPath(path, suffix string) string {
	path = strings.TrimPrefix(path, "/2/users/")
	if suffix != "" {
		path = strings.TrimSuffix(path, suffix)
	}
	path = strings.TrimSuffix(path, "/")
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	// Handle /users/by/username/{username} case
	if strings.HasPrefix(path, "by/username/") {
		return strings.TrimPrefix(path, "by/username/")
	}
	return path
}

// applyPaginationGeneric is a generic pagination function that works with any slice type
// Checks context cancellation for long-running operations
func applyPaginationGeneric[T any](items []T, r *http.Request, op *Operation, spec *OpenAPISpec, pathItem *PathItem, emptySlice func() []T) ([]T, string, error) {
	ctx := r.Context()
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
	}
	
	_, max, defaultValue, found := GetMaxResultsLimits(op, spec, pathItem)
	
	// Determine default and max values
	defaultMaxResults := 10
	maxMaxResults := 100
	if found {
		if defaultValue > 0 {
			defaultMaxResults = defaultValue
		}
		if max > 0 {
			maxMaxResults = max
		}
	}
	
	maxResults := defaultMaxResults
	if maxStr := r.URL.Query().Get("max_results"); maxStr != "" {
		val, err := strconv.Atoi(maxStr)
		if err != nil {
			return nil, "", fmt.Errorf("max_results must be a number")
		}
		if val <= 0 {
			return nil, "", fmt.Errorf("max_results must be greater than 0")
		}
		if found && max > 0 {
			if val > max {
				return nil, "", fmt.Errorf("max_results [%d] is not between 1 and %d", val, max)
			}
		} else if val > maxMaxResults {
			return nil, "", fmt.Errorf("max_results [%d] is not between 1 and %d", val, maxMaxResults)
		}
		maxResults = val
	}

	// Handle pagination_token for continuing pagination
	paginationToken := r.URL.Query().Get("pagination_token")
	startIdx := 0
	if paginationToken != "" {
		// Validate token format and size
		if len(paginationToken) > 1000 {
			return nil, "", fmt.Errorf("pagination_token is too long")
		}
		
		// Try to parse as base64-encoded token first (realistic format)
		if idx, err := decodePaginationToken(paginationToken); err == nil {
			// Validate decoded index is non-negative
			if idx < 0 {
				return nil, "", fmt.Errorf("invalid pagination_token: negative index")
			}
			startIdx = idx
		} else {
			// Fallback to old NEXT_ format for backwards compatibility
			if strings.HasPrefix(paginationToken, "NEXT_") {
				if idx, err := strconv.Atoi(strings.TrimPrefix(paginationToken, "NEXT_")); err == nil {
					if idx < 0 {
						return nil, "", fmt.Errorf("invalid pagination_token: negative index")
					}
					startIdx = idx
				}
			}
		}
	}

	var nextToken string
	totalItems := len(items)
	
	// Bounds checking: ensure startIdx is within valid range
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= totalItems {
		// Past the end, return empty
		return emptySlice(), "", nil
	}

	// Calculate end index with bounds checking
	endIdx := startIdx + maxResults
	if endIdx > totalItems {
		endIdx = totalItems
	} else if endIdx < totalItems {
		// More items available - generate realistic base64 token
		nextToken = encodePaginationToken(endIdx)
	}

	// Final bounds check before slicing
	if startIdx < 0 || endIdx < startIdx || endIdx > totalItems {
		return nil, "", fmt.Errorf("invalid pagination parameters")
	}

	// Check context one more time before returning (in case of very large slices)
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
	}

	return items[startIdx:endIdx], nextToken, nil
}

func applyPagination(items []*Tweet, r *http.Request, op *Operation, spec *OpenAPISpec, pathItem *PathItem) ([]*Tweet, string, error) {
	return applyPaginationGeneric(items, r, op, spec, pathItem, func() []*Tweet { return []*Tweet{} })
}

func applyUserPagination(items []*User, r *http.Request, op *Operation, spec *OpenAPISpec, pathItem *PathItem) ([]*User, string, error) {
	return applyPaginationGeneric(items, r, op, spec, pathItem, func() []*User { return []*User{} })
}

// encodePaginationToken encodes an offset into a base64 pagination token (realistic format)
func encodePaginationToken(offset int) string {
	// Encode offset as 8 bytes (uint64) + timestamp for realism
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(offset))
	binary.BigEndian.PutUint64(buf[8:16], uint64(time.Now().Unix()))
	return base64.URLEncoding.EncodeToString(buf)
}

// decodePaginationToken decodes a base64 pagination token to extract the offset
func decodePaginationToken(token string) (int, error) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	if len(data) < 8 {
		return 0, fmt.Errorf("invalid token length")
	}
	offset := int(binary.BigEndian.Uint64(data[0:8]))
	return offset, nil
}

func findMentions(ctx context.Context, username string, state *State) []*Tweet {
	// Simple search for @username mentions in tweet text
	mention := "@" + username
	var results []*Tweet
	iterationCount := 0
	
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	for _, tweet := range state.tweets {
		// Check for context cancellation periodically
		if ctx != nil && iterationCount%ContextCheckIntervalMedium == 0 {
			select {
			case <-ctx.Done():
				// Client disconnected, return partial results
				return results
			default:
			}
		}
		iterationCount++
		
		if strings.Contains(strings.ToLower(tweet.Text), strings.ToLower(mention)) {
			results = append(results, tweet)
		}
	}
	
	return results
}

// filterTweetsByTime filters tweets by since_id, until_id, start_time, end_time
func filterTweetsByTime(tweets []*Tweet, r *http.Request) []*Tweet {
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
	
	if sinceID == "" && untilID == "" && startTime == nil && endTime == nil {
		return tweets // No filtering needed
	}
	
	var filtered []*Tweet
	for _, tweet := range tweets {
		// Filter by since_id (tweets after this ID)
		if sinceID != "" && tweet.ID <= sinceID {
			continue
		}
		
		// Filter by until_id (tweets before this ID)
		if untilID != "" && tweet.ID >= untilID {
			continue
		}
		
		// Filter by start_time
		if startTime != nil && tweet.CreatedAt.Before(*startTime) {
			continue
		}
		
		// Filter by end_time
		if endTime != nil && tweet.CreatedAt.After(*endTime) {
			continue
		}
		
		filtered = append(filtered, tweet)
	}
	
	return filtered
}

// sortTweetsByCreatedAt sorts tweets by created_at using efficient sorting
func sortTweetsByCreatedAt(tweets []*Tweet, descending bool) {
	sort.Slice(tweets, func(i, j int) bool {
		if descending {
			return tweets[i].CreatedAt.After(tweets[j].CreatedAt)
		}
		return tweets[i].CreatedAt.Before(tweets[j].CreatedAt)
	})
}

// formatTweetsResponse formats tweets with field filtering and expansions
func formatTweetsResponse(tweets []*Tweet, queryParams *QueryParams, state *State, spec *OpenAPISpec, nextToken string) []byte {
	response := map[string]interface{}{
		"data": make([]map[string]interface{}, 0),
	}

	// Format tweets
	tweetData := make([]map[string]interface{}, 0, len(tweets))
	for _, tweet := range tweets {
		tweetMap := FormatTweet(tweet)
		// Apply field filtering if specified
		// filterTweetFields always includes default fields (id, text), so we can pass requested fields directly
		if queryParams != nil && len(queryParams.TweetFields) > 0 {
			tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
		} else {
			// Default fields only (id, text are included automatically)
			tweetMap = filterTweetFields(tweetMap, []string{})
		}
		tweetData = append(tweetData, tweetMap)
	}
	response["data"] = tweetData

	// Add meta with pagination token if available
	meta := map[string]interface{}{
		"result_count": len(tweetData),
	}
	if nextToken != "" {
		meta["next_token"] = nextToken
	}
	response["meta"] = meta

	// Handle expansions - always include includes object when expansions are requested
	if queryParams != nil && len(queryParams.Expansions) > 0 && state != nil {
		includes := buildExpansions(tweets, queryParams.Expansions, state, spec, queryParams)
		// Always add includes object (even if empty) when expansions are requested
		// This matches real API behavior where includes is always present when requested
		response["includes"] = includes
	}

	data, _ := MarshalJSONResponse(response)
	return data
}

// formatUsersResponse formats users with field filtering
func formatUsersResponse(users []*User, queryParams *QueryParams, spec *OpenAPISpec, nextToken string) []byte {
	response := map[string]interface{}{
		"data": make([]map[string]interface{}, 0),
	}

	// Format users
	userData := make([]map[string]interface{}, 0, len(users))
	for _, user := range users {
		userMap := FormatUser(user)
		// Always apply field filtering - if no fields specified, returns only defaults (id, name, username)
		requestedFields := []string{}
		if queryParams != nil && len(queryParams.UserFields) > 0 {
			requestedFields = queryParams.UserFields
		}
		userMap = filterUserFields(userMap, requestedFields)
		userData = append(userData, userMap)
	}
	response["data"] = userData

	// Add meta with pagination token if available
	meta := map[string]interface{}{
		"result_count": len(userData),
	}
	if nextToken != "" {
		meta["next_token"] = nextToken
	}
	response["meta"] = meta

	data, _ := MarshalJSONResponse(response)
	return data
}

// formatListsResponse formats lists with field filtering
func formatListsResponse(lists []*List, queryParams *QueryParams, spec *OpenAPISpec) []byte {
	response := map[string]interface{}{
		"data": make([]map[string]interface{}, 0),
	}

	// Format lists
	listData := make([]map[string]interface{}, 0, len(lists))
	for _, list := range lists {
		listMap := formatList(list)
		// Always apply field filtering - if no fields specified, returns only defaults (id, name)
		requestedFields := []string{}
		if queryParams != nil && len(queryParams.ListFields) > 0 {
			requestedFields = queryParams.ListFields
		}
		listMap = filterListFields(listMap, requestedFields)
		listData = append(listData, listMap)
	}
	response["data"] = listData

	// Add meta
	response["meta"] = map[string]interface{}{
		"result_count": len(listData),
	}

	data, _ := MarshalJSONResponse(response)
	return data
}

// formatList formats a list for response
func formatList(list *List) map[string]interface{} {
	return map[string]interface{}{
		"id":             list.ID,
		"name":           list.Name,
		"description":   list.Description,
		"created_at":     list.CreatedAt.Format(time.RFC3339),
		"private":       list.Private,
		"follower_count": list.FollowerCount,
		"member_count":   list.MemberCount,
		"owner_id":       list.OwnerID,
	}
}

// formatSpace formats a space for response
func formatSpace(space *Space) map[string]interface{} {
	result := map[string]interface{}{
		"id":    space.ID,
		"state": space.State,
		"created_at": space.CreatedAt.Format(time.RFC3339),
	}
	
	if space.Title != "" {
		result["title"] = space.Title
	}
	if !space.UpdatedAt.IsZero() {
		result["updated_at"] = space.UpdatedAt.Format(time.RFC3339)
	}
	if !space.StartedAt.IsZero() {
		result["started_at"] = space.StartedAt.Format(time.RFC3339)
	}
	if !space.EndedAt.IsZero() {
		result["ended_at"] = space.EndedAt.Format(time.RFC3339)
	}
	if !space.ScheduledStart.IsZero() {
		result["scheduled_start"] = space.ScheduledStart.Format(time.RFC3339)
	}
	if space.CreatorID != "" {
		result["creator_id"] = space.CreatorID
	}
	if len(space.HostIDs) > 0 {
		result["host_ids"] = space.HostIDs
	}
	if len(space.SpeakerIDs) > 0 {
		result["speaker_ids"] = space.SpeakerIDs
	}
	if space.SubscriberCount > 0 {
		result["subscriber_count"] = space.SubscriberCount
	}
	if space.ParticipantCount > 0 {
		result["participant_count"] = space.ParticipantCount
	}
	if space.IsTicketed {
		result["is_ticketed"] = space.IsTicketed
	}
	if space.Lang != "" {
		result["lang"] = space.Lang
	}
	
	return result
}

// buildExpansions builds the includes object for expansions
func buildExpansions(tweets []*Tweet, expansions []string, state *State, spec *OpenAPISpec, queryParams *QueryParams) map[string]interface{} {
	includes := make(map[string]interface{})
	
	// Return empty includes if state is nil (prevents nil pointer dereference)
	if state == nil {
		return includes
	}
	
	for _, exp := range expansions {
		switch exp {
		case "author_id":
			users := make([]map[string]interface{}, 0)
			seenUsers := make(map[string]bool)
			for _, tweet := range tweets {
				user := state.GetUserByID(tweet.AuthorID)
				if user != nil && !seenUsers[user.ID] {
					userMap := FormatUser(user)
					// Apply field filtering if specified
					if queryParams != nil && len(queryParams.UserFields) > 0 {
						userMap = filterUserFields(userMap, queryParams.UserFields)
					} else {
						// Default fields for expanded users
						userMap = filterUserFields(userMap, []string{"id", "name", "username"})
					}
					users = append(users, userMap)
					seenUsers[user.ID] = true
				}
			}
			if len(users) > 0 {
				includes["users"] = users
			}
		case "media_keys", "attachments.media_keys":
			media := make([]map[string]interface{}, 0)
			seenMedia := make(map[string]bool)
			for _, tweet := range tweets {
				// Check attachments first (preferred)
				if tweet.Attachments != nil {
					for _, mediaKey := range tweet.Attachments.MediaKeys {
						if !seenMedia[mediaKey] {
							// Find media by media_key
							state.mu.RLock()
							for _, m := range state.media {
								if m.MediaKey == mediaKey {
									mediaMap := formatMediaForExpansion(m)
									// Apply field filtering if specified
									if queryParams != nil && len(queryParams.MediaFields) > 0 {
										mediaMap = filterMediaFields(mediaMap, queryParams.MediaFields)
									} else {
										// Default fields for expanded media
										mediaMap = filterMediaFields(mediaMap, []string{"media_key", "type"})
									}
									media = append(media, mediaMap)
									seenMedia[mediaKey] = true
									break
								}
							}
							state.mu.RUnlock()
						}
					}
				}
				// Fallback to Media IDs for backward compatibility
				for _, mediaID := range tweet.Media {
					if !seenMedia[mediaID] {
						m := state.GetMedia(mediaID)
						if m != nil {
							mediaMap := formatMediaForExpansion(m)
							// Apply field filtering if specified
							if queryParams != nil && len(queryParams.MediaFields) > 0 {
								mediaMap = filterMediaFields(mediaMap, queryParams.MediaFields)
							} else {
								// Default fields for expanded media
								mediaMap = filterMediaFields(mediaMap, []string{"media_key", "type"})
							}
							media = append(media, mediaMap)
							seenMedia[mediaID] = true
						}
					}
				}
			}
			if len(media) > 0 {
				includes["media"] = media
			}
		case "poll_ids", "attachments.poll_ids":
			polls := make([]map[string]interface{}, 0)
			seenPolls := make(map[string]bool)
			for _, tweet := range tweets {
				// Check attachments first
				if tweet.Attachments != nil {
					for _, pollID := range tweet.Attachments.PollIDs {
						if !seenPolls[pollID] {
							poll := state.GetPoll(pollID)
							if poll != nil {
								pollMap := formatPoll(poll)
								// Apply field filtering if specified
								if queryParams != nil && len(queryParams.PollFields) > 0 {
									pollMap = filterPollFields(pollMap, queryParams.PollFields)
								} else {
									// Default fields for expanded polls
									pollMap = filterPollFields(pollMap, []string{"id", "options", "voting_status"})
								}
								polls = append(polls, pollMap)
								seenPolls[pollID] = true
							}
						}
					}
				}
				// Fallback to direct PollID field
				if tweet.PollID != "" && !seenPolls[tweet.PollID] {
					poll := state.GetPoll(tweet.PollID)
					if poll != nil {
						pollMap := formatPoll(poll)
						// Apply field filtering if specified
						if queryParams != nil && len(queryParams.PollFields) > 0 {
							pollMap = filterPollFields(pollMap, queryParams.PollFields)
						} else {
							// Default fields for expanded polls
							pollMap = filterPollFields(pollMap, []string{"id", "options", "voting_status"})
						}
						polls = append(polls, pollMap)
						seenPolls[tweet.PollID] = true
					}
				}
			}
			if len(polls) > 0 {
				includes["polls"] = polls
			}
		case "place_id", "geo.place_id":
			places := make([]map[string]interface{}, 0)
			seenPlaces := make(map[string]bool)
			for _, tweet := range tweets {
				if tweet.PlaceID != "" && !seenPlaces[tweet.PlaceID] {
					place := state.GetPlace(tweet.PlaceID)
					if place != nil {
						placeMap := formatPlace(place)
						// Apply field filtering if specified
						if queryParams != nil && len(queryParams.PlaceFields) > 0 {
							placeMap = filterPlaceFields(placeMap, queryParams.PlaceFields)
						} else {
							// Default fields for expanded places
							placeMap = filterPlaceFields(placeMap, []string{"id", "full_name", "name"})
						}
						places = append(places, placeMap)
						seenPlaces[tweet.PlaceID] = true
					}
				}
			}
			if len(places) > 0 {
				includes["places"] = places
			}
		case "referenced_tweets.id":
			referencedTweets := make([]map[string]interface{}, 0)
			seenTweets := make(map[string]bool)
			for _, tweet := range tweets {
				for _, ref := range tweet.ReferencedTweets {
					if !seenTweets[ref.ID] {
						refTweet := state.GetTweet(ref.ID)
						if refTweet != nil {
							tweetMap := FormatTweet(refTweet)
							// Apply field filtering if specified
							if queryParams != nil && len(queryParams.TweetFields) > 0 {
								tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
							} else {
								// Default fields for expanded tweets
								tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
							}
							referencedTweets = append(referencedTweets, tweetMap)
							seenTweets[ref.ID] = true
						}
					}
				}
			}
			if len(referencedTweets) > 0 {
				includes["tweets"] = referencedTweets
			}
		case "in_reply_to_user_id":
			users := make([]map[string]interface{}, 0)
			seenUsers := make(map[string]bool)
			for _, tweet := range tweets {
				if tweet.InReplyToID != "" && !seenUsers[tweet.InReplyToID] {
					user := state.GetUserByID(tweet.InReplyToID)
					if user != nil {
						userMap := FormatUser(user)
						// Apply field filtering if specified
						if queryParams != nil && len(queryParams.UserFields) > 0 {
							userMap = filterUserFields(userMap, queryParams.UserFields)
						} else {
							// Default fields for expanded users
							userMap = filterUserFields(userMap, []string{"id", "name", "username"})
						}
						users = append(users, userMap)
						seenUsers[tweet.InReplyToID] = true
					}
				}
			}
			if len(users) > 0 {
				// Merge with existing users if any (from author_id expansion)
				if existingUsers, ok := includes["users"].([]map[string]interface{}); ok {
					// Avoid duplicates
					existingIDs := make(map[string]bool)
					for _, u := range existingUsers {
						if id, ok := u["id"].(string); ok {
							existingIDs[id] = true
						}
					}
					for _, u := range users {
						if id, ok := u["id"].(string); ok && !existingIDs[id] {
							existingUsers = append(existingUsers, u)
						}
					}
					includes["users"] = existingUsers
				} else {
					includes["users"] = users
				}
			}
		}
	}
	
	return includes
}

// buildUserExpansions builds the includes section for user endpoints
func buildUserExpansions(users []*User, expansions []string, state *State, spec *OpenAPISpec, queryParams *QueryParams) map[string]interface{} {
	includes := make(map[string]interface{})
	
	for _, exp := range expansions {
		switch exp {
		case "pinned_tweet_id":
			tweets := make([]map[string]interface{}, 0)
			seenTweets := make(map[string]bool)
			for _, user := range users {
				if user.PinnedTweetID != "" && !seenTweets[user.PinnedTweetID] {
					tweet := state.GetTweet(user.PinnedTweetID)
					if tweet != nil {
						tweetMap := FormatTweet(tweet)
						// Apply field filtering if specified
						if queryParams != nil && len(queryParams.TweetFields) > 0 {
							tweetMap = filterTweetFields(tweetMap, queryParams.TweetFields)
						} else {
							// Default fields for expanded tweets
							tweetMap = filterTweetFields(tweetMap, []string{"id", "text"})
						}
						tweets = append(tweets, tweetMap)
						seenTweets[user.PinnedTweetID] = true
					}
				}
			}
			if len(tweets) > 0 {
				includes["tweets"] = tweets
			}
		}
	}
	
	return includes
}

// Field filtering functions.

func filterTweetFields(tweet map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, text, edit_history_tweet_ids) even when tweet.fields is specified
	defaultFields := []string{"id", "text", "edit_history_tweet_ids"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range tweet {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterUserFields(user map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, name, username) even when user.fields is specified
	defaultFields := []string{"id", "name", "username"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range user {
		if fieldMap[key] {
			// Match X API behavior: omit null/empty values even when explicitly requested
			// This matches the real X API which omits null fields
			if value == nil {
				continue // Skip null values
			}
			if strVal, ok := value.(string); ok && strVal == "" {
				continue // Skip empty strings
			}
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterListFields(list map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, name) even when list.fields is specified
	defaultFields := []string{"id", "name"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range list {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

// formatCommunity formats a community for response
func formatCommunity(community *Community) map[string]interface{} {
	result := map[string]interface{}{
		"id":          community.ID,
		"name":        community.Name,
		"created_at":  community.CreatedAt.Format(time.RFC3339),
	}
	if community.Description != "" {
		result["description"] = community.Description
	}
	if community.MemberCount > 0 {
		result["member_count"] = community.MemberCount
	}
	if community.Access != "" {
		result["access"] = community.Access
	}
	return result
}

// filterCommunityFields filters a community map to only include requested fields
func filterCommunityFields(community map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, name) even when community.fields is specified
	defaultFields := []string{"id", "name"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range community {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterMediaFields(media map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (media_key, type) even when media.fields is specified
	defaultFields := []string{"media_key", "type"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range media {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterPollFields(poll map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, options) even when poll.fields is specified
	defaultFields := []string{"id", "options"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range poll {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterSpaceFields(space map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, state) even when space.fields is specified
	defaultFields := []string{"id", "state"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range space {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

func filterPlaceFields(place map[string]interface{}, fields []string) map[string]interface{} {
	filtered := make(map[string]interface{})
	fieldMap := make(map[string]bool)
	
	// X API always includes default fields (id, full_name) even when place.fields is specified
	defaultFields := []string{"id", "full_name"}
	for _, f := range defaultFields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	// Add requested fields
	for _, f := range fields {
		fieldMap[strings.TrimSpace(f)] = true
	}
	
	for key, value := range place {
		if fieldMap[key] {
			filtered[key] = value
		}
	}
	
	return filtered
}

// formatPoll formats a poll for response
func formatPoll(poll *Poll) map[string]interface{} {
	options := make([]map[string]interface{}, len(poll.Options))
	for i, opt := range poll.Options {
		options[i] = map[string]interface{}{
			"position": opt.Position,
			"label":    opt.Label,
			"votes":    opt.Votes,
		}
	}
	
	return map[string]interface{}{
		"id":               poll.ID,
		"options":          options,
		"duration_minutes": poll.DurationMinutes,
		"end_datetime":     poll.EndDatetime.Format(time.RFC3339),
		"voting_status":    poll.VotingStatus,
	}
}

// formatMedia formats media for response (uses existing function from handlers_unified.go)
func formatMediaForExpansion(media *Media) map[string]interface{} {
	return formatMedia(media)
}

// formatPlace formats a place for response
func formatPlace(place *Place) map[string]interface{} {
	result := map[string]interface{}{
		"id":          place.ID,
		"full_name":   place.FullName,
		"name":        place.Name,
		"country":     place.Country,
		"country_code": place.CountryCode,
		"place_type":  place.PlaceType,
	}
	
	if len(place.Geo.Coordinates) > 0 {
		result["geo"] = place.Geo
	}
	
	return result
}

