// Package playground manages the in-memory state of the playground server.
//
// This file defines the State type and all data structures (User, Tweet, List, etc.)
// that represent the playground's stateful data. It provides thread-safe operations
// for creating, reading, updating, and deleting entities, as well as managing
// relationships between entities (following, likes, retweets, etc.).
//
// The State type uses RWMutex for concurrent access and maintains maps of all
// entity types. All operations are thread-safe and designed to match X API v2
// behavior patterns.
package playground

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SearchStreamRule represents a search stream rule.
type SearchStreamRule struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Tag   string `json:"tag,omitempty"`
}

// SearchWebhook represents a search webhook.
type SearchWebhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// State manages the in-memory state of the playground server
type State struct {
	mu      sync.RWMutex
	importMu sync.Mutex // Mutex to prevent concurrent imports
	users   map[string]*User
	tweets  map[string]*Tweet
	media   map[string]*Media
	lists   map[string]*List
	spaces  map[string]*Space
	polls   map[string]*Poll
	places  map[string]*Place
	topics  map[string]*Topic
	nextID  int64
	config  *PlaygroundConfig // Store config for access in handlers
	// Search stream rules and webhooks
	searchStreamRules map[string]*SearchStreamRule
	searchWebhooks   map[string]*SearchWebhook
	// DMs
	dmConversations map[string]*DMConversation
	dmEvents        map[string]*DMEvent
	// Compliance, Communities, News, Notes
	complianceJobs map[string]*ComplianceJob
	communities    map[string]*Community
	news           map[string]*News
	notes          map[string]*Note
	// Activity subscriptions
	activitySubscriptions map[string]*ActivitySubscription
	// Personalized trends
	personalizedTrends []*PersonalizedTrend
	// Streaming connections - tracks active connections per user
	// Key: userID, Value: map of connectionID -> cancelFunc
	streamConnections map[string]map[string]context.CancelFunc
	streamConnMu      sync.RWMutex // Separate mutex for stream connections to avoid deadlocks
}

// User represents a user in the playground.
// Matches the X API v2 User object structure with additional internal relationship tracking.
type User struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Username        string    `json:"username"`
	CreatedAt       time.Time `json:"created_at"`
	Description     string    `json:"description,omitempty"`
	Location        string    `json:"location,omitempty"`
	URL             string    `json:"url,omitempty"`
	Verified        bool      `json:"verified,omitempty"`
	VerifiedType    string    `json:"verified_type,omitempty"` // "blue", "business", "government", "none"
	Protected       bool      `json:"protected,omitempty"`
	PublicMetrics   UserMetrics `json:"public_metrics"`
	ProfileImageURL string    `json:"profile_image_url,omitempty"`
	PinnedTweetID   string    `json:"pinned_tweet_id,omitempty"`
	Entities        *UserEntities `json:"entities,omitempty"`
	Withheld        *UserWithheld `json:"withheld,omitempty"` // Withheld information
	// Relationships
	Tweets          []string  `json:"-"` // Tweet IDs (owned tweets)
	LikedTweets     []string  `json:"-"` // Tweet IDs (liked)
	RetweetedTweets []string  `json:"-"` // Tweet IDs (retweeted)
	Following       []string  `json:"-"` // User IDs (following)
	Followers       []string  `json:"-"` // User IDs (followers - reverse relationship)
	Lists           []string  `json:"-"` // List IDs (owned lists)
	ListMemberships []string  `json:"-"` // List IDs (member of)
	Spaces          []string  `json:"-"` // Space IDs (hosted spaces)
	MutedUsers      []string  `json:"-"` // User IDs (muted)
	BlockedUsers    []string  `json:"-"` // User IDs (blocked)
	BookmarkedTweets []string  `json:"-"` // Tweet IDs (bookmarked)
	FollowedLists   []string  `json:"-"` // List IDs (followed lists)
	PinnedLists     []string  `json:"-"` // List IDs (pinned lists)
}

// UserWithheld represents withheld information for a user.
type UserWithheld struct {
	CountryCodes []string `json:"country_codes,omitempty"`
	Scope        string   `json:"scope,omitempty"`
}

// UserEntities represents entities in user profile.
type UserEntities struct {
	URL         *UserURLEntity `json:"url,omitempty"`
	Description *UserDescriptionEntity `json:"description,omitempty"`
}

// UserURLEntity represents URL entity in user profile.
type UserURLEntity struct {
	URLs []EntityURL `json:"urls,omitempty"`
}

// UserDescriptionEntity represents description entity in user profile.
type UserDescriptionEntity struct {
	URLs     []EntityURL     `json:"urls,omitempty"`
	Hashtags []EntityHashtag `json:"hashtags,omitempty"`
	Mentions []EntityMention `json:"mentions,omitempty"`
	Cashtags []EntityCashtag `json:"cashtags,omitempty"`
}

// UserMetrics represents user public metrics.
// Includes follower count, following count, tweet count, and other engagement metrics.
type UserMetrics struct {
	FollowersCount int `json:"followers_count"`
	FollowingCount int `json:"following_count"`
	TweetCount     int `json:"tweet_count"`
	ListedCount    int `json:"listed_count,omitempty"`
	LikeCount      int `json:"like_count,omitempty"`    // Total likes on user's tweets
	MediaCount     int `json:"media_count,omitempty"`   // Total media items in user's tweets
}

// Tweet represents a tweet in the playground.
// Matches the X API v2 Tweet object structure with additional internal relationship tracking.
type Tweet struct {
	ID              string    `json:"id"`
	Text            string    `json:"text"`
	AuthorID        string    `json:"author_id"`
	CreatedAt       time.Time `json:"created_at"`
	EditHistoryTweetIDs []string `json:"edit_history_tweet_ids,omitempty"` // Array of tweet IDs in edit history
	ConversationID  string    `json:"conversation_id,omitempty"`
	InReplyToID     string    `json:"in_reply_to_user_id,omitempty"`
	InReplyToTweetID string   `json:"in_reply_to_tweet_id,omitempty"`
	ReferencedTweets []ReferencedTweet `json:"referenced_tweets,omitempty"`
	PublicMetrics   TweetMetrics `json:"public_metrics"`
	Entities        *TweetEntities `json:"entities,omitempty"`
	Attachments     *TweetAttachments `json:"attachments,omitempty"`
	Source          string    `json:"source,omitempty"`
	Lang            string    `json:"lang,omitempty"`
	PossiblySensitive bool    `json:"possibly_sensitive,omitempty"`
	Hidden          bool      `json:"hidden,omitempty"` // Hidden reply
	// Relationships
	LikedBy         []string  `json:"-"` // User IDs (users who liked)
	RetweetedBy     []string  `json:"-"` // User IDs (users who retweeted)
	Replies         []string  `json:"-"` // Tweet IDs (reply tweets)
	Quotes          []string  `json:"-"` // Tweet IDs (tweets quoting this)
	Media           []string  `json:"-"` // Media IDs
	PollID          string    `json:"-"` // Poll ID (if poll tweet)
	PlaceID         string    `json:"-"` // Place ID (if geo-tagged)
	SpaceID         string    `json:"-"` // Space ID (if tweet is associated with a space)
}

// ReferencedTweet represents a referenced tweet.
// Used in tweet.referenced_tweets field to link to related tweets.
type ReferencedTweet struct {
	Type string `json:"type"` // "replied_to", "quoted", "retweeted"
	ID   string `json:"id"`
}

// TweetEntities represents entities extracted from tweet text.
// Contains hashtags, mentions, URLs, and cashtags found in the tweet.
type TweetEntities struct {
	Hashtags []EntityHashtag `json:"hashtags,omitempty"`
	Mentions []EntityMention `json:"mentions,omitempty"`
	URLs     []EntityURL     `json:"urls,omitempty"`
	Cashtags []EntityCashtag `json:"cashtags,omitempty"`
}

// EntityHashtag represents a hashtag entity.
// Contains start/end positions and the tag text.
type EntityHashtag struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Tag   string `json:"tag"`
}

// EntityMention represents a mention entity.
// Contains start/end positions, username, and optional user ID.
type EntityMention struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Username string `json:"username"`
	ID       string `json:"id,omitempty"`
}

// EntityURL represents a URL entity.
// Contains start/end positions, URL, expanded URL, and optional metadata.
type EntityURL struct {
	Start       int      `json:"start"`
	End         int      `json:"end"`
	URL         string   `json:"url"`
	ExpandedURL string   `json:"expanded_url,omitempty"`
	DisplayURL  string   `json:"display_url,omitempty"`
	Images      []URLImage `json:"images,omitempty"`
	Status      int      `json:"status,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	UnwoundURL  string   `json:"unwound_url,omitempty"`
}

// URLImage represents an image in a URL entity.
type URLImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// EntityCashtag represents a cashtag entity.
// Contains start/end positions and the cashtag text (e.g., "$AAPL").
type EntityCashtag struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Tag   string `json:"tag"`
}

// TweetAttachments represents tweet attachments.
// Contains media keys and poll IDs attached to a tweet.
type TweetAttachments struct {
	MediaKeys []string `json:"media_keys,omitempty"`
	PollIDs   []string `json:"poll_ids,omitempty"`
}

// TweetMetrics represents tweet public metrics.
// Includes retweet count, like count, reply count, quote count, and engagement metrics.
type TweetMetrics struct {
	RetweetCount   int `json:"retweet_count"`
	LikeCount      int `json:"like_count"`
	ReplyCount     int `json:"reply_count"`
	QuoteCount     int `json:"quote_count"`
	BookmarkCount  int `json:"bookmark_count"`  // Always include (even if 0) to match real API
	ImpressionCount int `json:"impression_count"` // Always include (even if 0) to match real API
}

// List represents a list in the playground.
// Matches the X API v2 List object structure.
type List struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	OwnerID       string    `json:"owner_id"`
	Private       bool      `json:"private,omitempty"`
	MemberCount   int       `json:"member_count"`
	FollowerCount int      `json:"follower_count"`
	// Relationships
	Members       []string  `json:"-"` // User IDs
	Followers     []string  `json:"-"` // User IDs (list followers)
}

// Space represents a space in the playground.
// Matches the X API v2 Space object structure.
type Space struct {
	ID              string    `json:"id"`
	State           string    `json:"state"` // scheduled, live, ended
	Title           string    `json:"title,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	ScheduledStart  time.Time `json:"scheduled_start,omitempty"`
	CreatorID       string    `json:"creator_id,omitempty"`
	HostIDs         []string  `json:"host_ids,omitempty"`
	SpeakerIDs      []string  `json:"speaker_ids,omitempty"`
	InvitedUserIDs  []string  `json:"invited_user_ids,omitempty"`
	SubscriberCount int       `json:"subscriber_count,omitempty"`
	ParticipantCount int      `json:"participant_count,omitempty"`
	IsTicketed      bool      `json:"is_ticketed,omitempty"`
	Lang            string    `json:"lang,omitempty"`
	TopicIDs        []string  `json:"-"` // Topic IDs (for relationships)
	BuyerIDs        []string  `json:"-"` // User IDs (users who bought tickets)
}

// Poll represents a poll in the playground.
// Matches the X API v2 Poll object structure.
type Poll struct {
	ID              string    `json:"id"`
	Options         []PollOption `json:"options"`
	DurationMinutes int       `json:"duration_minutes"`
	EndDatetime     time.Time `json:"end_datetime,omitempty"`
	VotingStatus    string    `json:"voting_status,omitempty"` // open, closed
}

// PollOption represents a poll option.
type PollOption struct {
	Position int    `json:"position"`
	Label    string `json:"label"`
	Votes    int    `json:"votes"`
}

// Place represents a place/location in the playground.
// Matches the X API v2 Place object structure.
type Place struct {
	ID          string    `json:"id"`
	FullName    string    `json:"full_name"`
	Name        string    `json:"name"`
	Country     string    `json:"country"`
	CountryCode string    `json:"country_code"`
	PlaceType   string    `json:"place_type"` // city, admin, country, poi
	Geo         PlaceGeo  `json:"geo,omitempty"`
}

// PlaceGeo represents geographic coordinates
type PlaceGeo struct {
	Type        string    `json:"type"` // Point
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude]
}

// Topic represents a topic in the playground.
// Matches the X API v2 Topic object structure.
type Topic struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
}

// PersonalizedTrend represents a personalized trending topic.
// Matches the X API v2 PersonalizedTrend object structure.
type PersonalizedTrend struct {
	Category      string `json:"category"`
	PostCount     string `json:"post_count"`
	TrendName     string `json:"trend_name"`
	TrendingSince string `json:"trending_since"`
}

// DMConversation represents a direct message conversation
type DMConversation struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	ParticipantIDs []string  `json:"participant_ids"`
}

// DMEvent represents a direct message event
type DMEvent struct {
	ID               string    `json:"id"`
	Text             string    `json:"text,omitempty"`
	SenderID         string    `json:"sender_id"`
	CreatedAt        time.Time `json:"created_at"`
	DMConversationID string    `json:"dm_conversation_id"`
	EventType        string    `json:"event_type"` // "MessageCreate", "Read", etc.
	ParticipantIDs   []string  `json:"participant_ids"`
	// Optional fields similar to Tweet
	Attachments      *TweetAttachments `json:"attachments,omitempty"`
	Entities         *TweetEntities    `json:"entities,omitempty"`
	ReferencedTweets []ReferencedTweet `json:"referenced_tweets,omitempty"`
}

// ComplianceJob represents a compliance job.
// Matches the X API v2 Compliance Job object structure.
type ComplianceJob struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Type              string    `json:"type"` // "tweets", "users"
	Status            string    `json:"status"` // "in_progress", "complete", "failed"
	CreatedAt         time.Time `json:"created_at"`
	UploadURL         string    `json:"upload_url,omitempty"`
	UploadExpiresAt   time.Time `json:"upload_expires_at,omitempty"`
	DownloadURL       string    `json:"download_url,omitempty"`
	DownloadExpiresAt time.Time `json:"download_expires_at,omitempty"`
}

// Community represents a community.
// Matches the X API v2 Community object structure.
type Community struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	MemberCount int       `json:"member_count,omitempty"`
	Access      string    `json:"access,omitempty"` // e.g., "public", "private", "restricted"
}

// News represents a news article.
// Matches the X API v2 News object structure.
type News struct {
	ID              string                 `json:"id"`
	RestID          string                 `json:"rest_id,omitempty"`
	Name            string                 `json:"name"`
	Summary         string                 `json:"summary"`
	Hook            string                 `json:"hook"`
	Category        string                 `json:"category"`
	UpdatedAt       time.Time              `json:"updated_at"`
	LastUpdatedAtMs string                 `json:"last_updated_at_ms,omitempty"`
	Disclaimer      string                 `json:"disclaimer"`
	Contexts        *NewsContexts          `json:"contexts,omitempty"`
	Keywords        []string               `json:"keywords,omitempty"`
	ClusterPostsResults []map[string]interface{} `json:"cluster_posts_results,omitempty"`
}

// NewsContexts represents the contexts object in news articles
type NewsContexts struct {
	Topics      []string               `json:"topics,omitempty"`
	Entities    *NewsEntities          `json:"entities,omitempty"`
	Finance     *NewsFinance           `json:"finance,omitempty"`
	Sports      *NewsSports            `json:"sports,omitempty"`
}

// NewsEntities represents entities in news contexts
type NewsEntities struct {
	Events        []string `json:"events,omitempty"`
	Organizations []string `json:"organizations,omitempty"`
	People        []string `json:"people,omitempty"`
	Places        []string `json:"places,omitempty"`
	Products      []string `json:"products,omitempty"`
}

// NewsFinance represents finance context in news
type NewsFinance struct {
	Tickers []string `json:"tickers,omitempty"`
}

// NewsSports represents sports context in news
type NewsSports struct {
	Teams []string `json:"teams,omitempty"`
}

// Note represents a note.
// Matches the X API v2 Note object structure.
type Note struct {
	ID          string    `json:"id"`
	Text        string    `json:"text"`
	AuthorID    string    `json:"author_id"`
	CreatedAt   time.Time `json:"created_at"`
	PostID      string    `json:"post_id,omitempty"` // Tweet ID this note is about
}

// ActivitySubscription represents an activity subscription.
// Matches the X API v2 Activity Subscription object structure.
type ActivitySubscription struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Active      bool      `json:"active"`
}

// Media represents a media upload in the playground.
// Matches the X API v2 Media object structure.
type Media struct {
	ID               string    `json:"id"`
	MediaKey         string    `json:"media_key"`
	Type             string    `json:"type,omitempty"` // photo, video, animated_gif
	State            string    `json:"state"` // initialized, processing, succeeded, failed
	ProcessingInfo   *ProcessingInfo `json:"processing_info,omitempty"`
	ExpiresAfterSecs int      `json:"expires_after_secs"`
	CreatedAt        time.Time `json:"created_at"`
	URL              string    `json:"url,omitempty"`
	Width            int       `json:"width,omitempty"`
	Height           int       `json:"height,omitempty"`
	DurationMs      int       `json:"duration_ms,omitempty"` // for video
	PreviewImageURL string    `json:"preview_image_url,omitempty"`
	AltText          string    `json:"alt_text,omitempty"`
}

// ProcessingInfo represents media processing information
type ProcessingInfo struct {
	State           string `json:"state"`
	CheckAfterSecs  int    `json:"check_after_secs"`
	ProgressPercent int    `json:"progress_percent"`
}

// NewState creates a new State instance with default data
func NewState() *State {
	return NewStateWithConfig(nil)
}

// UpdateConfig updates the state's configuration reference
// This allows runtime configuration changes without recreating state
func (s *State) UpdateConfig(config *PlaygroundConfig) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// NewStateWithConfig creates a new State instance with optional config
// If persistence is enabled, it will try to load state from file first
func NewStateWithConfig(config *PlaygroundConfig) *State {
	state := &State{
		users:  make(map[string]*User),
		tweets: make(map[string]*Tweet),
		media:  make(map[string]*Media),
		lists:  make(map[string]*List),
		spaces: make(map[string]*Space),
		polls:  make(map[string]*Poll),
		places: make(map[string]*Place),
		topics: make(map[string]*Topic),
		nextID: 1, // Start at 1 (0 is reserved for playground user)
		config: config, // Store config for access in handlers
		searchStreamRules: make(map[string]*SearchStreamRule),
		searchWebhooks:   make(map[string]*SearchWebhook),
		dmConversations:  make(map[string]*DMConversation),
		dmEvents:          make(map[string]*DMEvent),
		complianceJobs:    make(map[string]*ComplianceJob),
		communities:       make(map[string]*Community),
		news:              make(map[string]*News),
		notes:             make(map[string]*Note),
		activitySubscriptions: make(map[string]*ActivitySubscription),
		personalizedTrends: make([]*PersonalizedTrend, 0),
		streamConnections: make(map[string]map[string]context.CancelFunc),
	}

	// Try to load persisted state if enabled
	if config != nil {
		persistenceConfig := config.GetPersistenceConfig()
		if persistenceConfig != nil && persistenceConfig.Enabled {
			if export, err := LoadStateFromFile(persistenceConfig); err == nil && export != nil {
				// Load state from file
				log.Printf("Loading persisted state from file")
				ImportStateFromFile(state, export)
				// Check if trends are missing and seed them if needed
				state.mu.RLock()
				needsTrends := state.personalizedTrends == nil || len(state.personalizedTrends) == 0
				state.mu.RUnlock()
				if needsTrends {
					log.Printf("Personalized trends missing from persisted state, seeding trends")
					// Create a temporary seeder just for trends
					var seedingConfig *SeedingConfig
					if config != nil {
						seedingConfig = config.GetSeedingConfig()
					}
					seeder := &Seeder{
						state:         state,
						config:        config,
						seedingConfig: seedingConfig,
					}
					seeder.seedPersonalizedTrends()
				}
				log.Printf("Loaded persisted state")
				return state
			}
			log.Printf("Persistence enabled but no saved state found")
		}
	}

	// No persisted state found or persistence disabled - seed realistic data
	log.Printf("Seeding realistic data")
	seedRealisticData(state, config)
	log.Printf("Data seeding complete")
	
	// Ensure default user exists (in case seeding didn't create it)
	ensureDefaultUser(state)

	return state
}

// generateIDUnlocked generates an ID without acquiring a lock.
// Caller must hold s.mu.
// Uses int64 which can overflow after ~292 years at 1 ID/ms.
// If overflow occurs, wraps around to 1 (skipping 0 which is reserved).
func (s *State) generateIDUnlocked() string {
	id := s.nextID
	s.nextID++
	
	// Handle overflow: if nextID becomes negative or wraps, reset to 1
	// This prevents issues while maintaining uniqueness within a session
	if s.nextID <= 0 {
		s.nextID = 1
		// Note: This means IDs may not be globally unique after overflow,
		// but this is acceptable for a playground/testing environment
	}
	
	return fmt.Sprintf("%d", id)
}

// generateID generates a new unique ID (snowflake-like).
// Thread-safe wrapper around generateIDUnlocked.
func (s *State) generateID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generateIDUnlocked()
}

// generateSpaceIDUnlocked generates a space ID in the format used by the real X API.
// Format: 13 characters, alphanumeric (base62), e.g., "1ypJdqvkgerxW"
// Caller must hold s.mu.
func (s *State) generateSpaceIDUnlocked() string {
	// Base62 characters: 0-9, a-z, A-Z
	const charset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const length = 13
	
	// Use nextID as seed for determinism
	seed := s.nextID
	s.nextID++
	
	// Generate a random-looking ID but deterministic based on seed
	id := make([]byte, length)
	r := rand.New(rand.NewSource(int64(seed)))
	
	for i := range id {
		id[i] = charset[r.Intn(len(charset))]
	}
	
	return string(id)
}

// generateSpaceID generates a space ID in the format used by the real X API.
// Format: 13 characters, alphanumeric (base62), e.g., "1ypJdqvkgerxW"
// Thread-safe wrapper around generateSpaceIDUnlocked.
func (s *State) generateSpaceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generateSpaceIDUnlocked()
}

// getNextID returns the current nextID value (for state import atomicity)
// Caller must hold s.mu
func (s *State) getNextID() int64 {
	return s.nextID
}

// GetUserByID gets a user by ID
// Returns a pointer to the user, which is safe to read from but should not be modified.
// The pointer remains valid even if the user is deleted from the map, but may point to stale data.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetUserByID(id string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[id]
}

// GetUserByUsername gets a user by username
// Returns a pointer to the user, which is safe to read from but should not be modified.
// The pointer remains valid even if the user is deleted from the map, but may point to stale data.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetUserByUsername(username string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[username]
}

// GetDefaultUser gets the default playground user (ID "0")
// Returns a pointer to the default user, which is safe to read from but should not be modified.
// The default user is never deleted, so the pointer is always valid.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetDefaultUser() *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Playground user always has ID "0"
	return s.users["0"]
}

// extractEntities extracts hashtags, mentions, URLs, and cashtags from tweet text
func extractEntities(text string) *TweetEntities {
	entities := &TweetEntities{
		Hashtags: make([]EntityHashtag, 0),
		Mentions: make([]EntityMention, 0),
		URLs:     make([]EntityURL, 0),
		Cashtags: make([]EntityCashtag, 0),
	}

	// Extract hashtags (#hashtag)
	hashtagRegex := regexp.MustCompile(`#([a-zA-Z0-9_]+)`)
	hashtagMatches := hashtagRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range hashtagMatches {
		start := match[0]
		end := match[1]
		tag := text[match[2]:match[3]] // Extract the tag without #
		entities.Hashtags = append(entities.Hashtags, EntityHashtag{
			Start: start,
			End:   end,
			Tag:   tag,
		})
	}

	// Extract mentions (@username)
	mentionRegex := regexp.MustCompile(`@([a-zA-Z0-9_]+)`)
	mentionMatches := mentionRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range mentionMatches {
		start := match[0]
		end := match[1]
		username := text[match[2]:match[3]] // Extract the username without @
		entities.Mentions = append(entities.Mentions, EntityMention{
			Start:    start,
			End:      end,
			Username: username,
		})
	}

	// Extract URLs (http://, https://, www.)
	urlRegex := regexp.MustCompile(`(https?://[^\s]+|www\.[^\s]+)`)
	urlMatches := urlRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range urlMatches {
		start := match[0]
		end := match[1]
		url := text[start:end]
		entities.URLs = append(entities.URLs, EntityURL{
			Start: start,
			End:   end,
			URL:   url,
		})
	}

	// Extract cashtags ($TICKER)
	cashtagRegex := regexp.MustCompile(`\$([A-Z]{1,5})`)
	cashtagMatches := cashtagRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range cashtagMatches {
		start := match[0]
		end := match[1]
		tag := text[match[2]:match[3]] // Extract the tag without $
		entities.Cashtags = append(entities.Cashtags, EntityCashtag{
			Start: start,
			End:   end,
			Tag:   tag,
		})
	}

	// Only return entities if at least one entity type has items
	if len(entities.Hashtags) == 0 && len(entities.Mentions) == 0 && len(entities.URLs) == 0 && len(entities.Cashtags) == 0 {
		return nil
	}

	return entities
}

// CreateTweet creates a new tweet
func (s *State) CreateTweet(text string, authorID string) *Tweet {
	s.mu.Lock()
	defer s.mu.Unlock()

	tweet := &Tweet{
		ID:              s.generateIDUnlocked(),
		Text:            text,
		AuthorID:        authorID,
		CreatedAt:       time.Now(),
		ConversationID:  "",
		LikedBy:         make([]string, 0),
		RetweetedBy:     make([]string, 0),
		Replies:         make([]string, 0),
		Quotes:          make([]string, 0),
		Media:           make([]string, 0),
		Source:          "Twitter Web App",
		Lang:            "en",
		PossiblySensitive: false,
	}

	// Extract entities (hashtags, mentions, URLs, cashtags) from text
	tweet.Entities = extractEntities(text)

	// Set conversation ID (same as tweet ID for new tweets)
	tweet.ConversationID = tweet.ID

	s.tweets[tweet.ID] = tweet

	// Update user tweet list and count
	if user := s.users[authorID]; user != nil {
		user.Tweets = append(user.Tweets, tweet.ID)
		user.PublicMetrics.TweetCount = len(user.Tweets)
	}

	return tweet
}

// GetTweet gets a tweet by ID
// Returns a pointer to the tweet, which is safe to read from but should not be modified.
// The pointer remains valid even if the tweet is deleted from the map, but may point to stale data.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetTweet(id string) *Tweet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tweets[id]
}

// DeleteTweet deletes a tweet
func (s *State) DeleteTweet(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tweet, exists := s.tweets[id]; exists {
		delete(s.tweets, id)
		// Update user tweet list and count
		if user := s.users[tweet.AuthorID]; user != nil {
			// Remove from user's tweets list
			for i, tweetID := range user.Tweets {
				if tweetID == id {
					user.Tweets = append(user.Tweets[:i], user.Tweets[i+1:]...)
					break
				}
			}
			user.PublicMetrics.TweetCount = len(user.Tweets)
		}
		return true
	}
	return false
}

// SearchTweets searches for tweets with optional time filtering
// ctx is used to check for cancellation during long-running searches
// If ctx is nil, cancellation checks are skipped
func (s *State) SearchTweets(ctx context.Context, query string, limit int, sinceID, untilID string, startTime, endTime *time.Time) []*Tweet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Tweet
	count := 0
	iterationCount := 0
	
	for _, tweet := range s.tweets {
		// Check for context cancellation periodically to avoid overhead
		if ctx != nil && iterationCount%ContextCheckIntervalMedium == 0 {
			select {
			case <-ctx.Done():
				// Client disconnected, return partial results
				return results
			default:
			}
		}
		iterationCount++
		
		if count >= limit {
			break
		}
		
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
		
		// Simple text matching - if query is empty, return all tweets (up to limit)
		// Otherwise, match if query appears in tweet text OR author name/username (case-insensitive)
		matches := false
		if query == "" {
			matches = true
		} else {
			queryLower := strings.ToLower(query)
			// Check tweet text first
			if strings.Contains(strings.ToLower(tweet.Text), queryLower) {
				matches = true
			}
			
			// Also check author name and username (OR condition - matches if text OR author matches)
			if !matches {
				author := s.users[tweet.AuthorID]
				if author != nil {
					if strings.Contains(strings.ToLower(author.Name), queryLower) ||
						strings.Contains(strings.ToLower(author.Username), queryLower) {
						matches = true
					}
				}
			}
		}
		
		if matches {
			results = append(results, tweet)
			count++
		}
	}
	return results
}

// CreateMedia creates a new media upload
func (s *State) CreateMedia(mediaKey string, expiresAfterSecs int) *Media {
	s.mu.Lock()
	defer s.mu.Unlock()

	media := &Media{
		ID:               s.generateIDUnlocked(),
		MediaKey:         mediaKey,
		State:            "initialized",
		ExpiresAfterSecs: expiresAfterSecs,
		CreatedAt:        time.Now(),
	}

	s.media[media.ID] = media
	return media
}

// GetMedia gets media by ID
func (s *State) GetMedia(id string) *Media {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.media[id]
}

// GetMediaByKey gets media by media_key
func (s *State) GetMediaByKey(mediaKey string) *Media {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, media := range s.media {
		if media.MediaKey == mediaKey {
			return media
		}
	}
	return nil
}

// GetAllMedia returns all media
func (s *State) GetAllMedia() []*Media {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mediaList := make([]*Media, 0, len(s.media))
	for _, media := range s.media {
		mediaList = append(mediaList, media)
	}
	return mediaList
}

// UpdateMediaMetadata updates media metadata (alt text, etc.)
func (s *State) UpdateMediaMetadata(mediaID, altText string, metadata map[string]interface{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if media, exists := s.media[mediaID]; exists {
		if altText != "" {
			media.AltText = altText
		}
		// Metadata could include other fields, but for now we just update alt_text
		return true
	}
	return false
}

// UpdateMediaState updates the state of a media upload
func (s *State) UpdateMediaState(id string, state string, processingInfo *ProcessingInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if media, exists := s.media[id]; exists {
		media.State = state
		media.ProcessingInfo = processingInfo
	}
}

// GetTweets gets multiple tweets by IDs
// Returns pointers to tweets, which are safe to read from but should not be modified.
// Pointers remain valid even if tweets are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetTweets(ids []string) []*Tweet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tweets []*Tweet
	for _, id := range ids {
		if tweet := s.tweets[id]; tweet != nil {
			tweets = append(tweets, tweet)
		}
	}
	return tweets
}

// GetAllTweets returns all tweets (for counts, analytics, etc.)
// Returns pointers to tweets, which are safe to read from but should not be modified.
// Pointers remain valid even if tweets are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetAllTweets() []*Tweet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tweets := make([]*Tweet, 0, len(s.tweets))
	for _, tweet := range s.tweets {
		tweets = append(tweets, tweet)
	}
	return tweets
}

// GetSearchStreamRules returns all search stream rules
func (s *State) GetSearchStreamRules() []*SearchStreamRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]*SearchStreamRule, 0, len(s.searchStreamRules))
	for _, rule := range s.searchStreamRules {
		rules = append(rules, rule)
	}
	return rules
}

// FindSearchStreamRuleByValue finds an existing rule by its value (for duplicate detection)
func (s *State) FindSearchStreamRuleByValue(value string) *SearchStreamRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, rule := range s.searchStreamRules {
		if rule.Value == value {
			return rule
		}
	}
	return nil
}

// CreateSearchStreamRule creates a new search stream rule
func (s *State) CreateSearchStreamRule(value, tag string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	ruleID := s.generateIDUnlocked()
	rule := &SearchStreamRule{
		ID:    ruleID,
		Value: value,
		Tag:   tag,
	}
	s.searchStreamRules[ruleID] = rule
	return ruleID
}

// GetSearchWebhooks returns all search webhooks
func (s *State) GetSearchWebhooks() []*SearchWebhook {
	s.mu.RLock()
	defer s.mu.RUnlock()

	webhooks := make([]*SearchWebhook, 0, len(s.searchWebhooks))
	for _, webhook := range s.searchWebhooks {
		webhooks = append(webhooks, webhook)
	}
	return webhooks
}

// GetSearchWebhook gets a search webhook by ID
func (s *State) GetSearchWebhook(webhookID string) *SearchWebhook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchWebhooks[webhookID]
}

// CreateSearchWebhook creates a new search webhook
func (s *State) CreateSearchWebhook(webhookID, url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	webhook := &SearchWebhook{
		ID:        webhookID,
		URL:       url,
		CreatedAt: time.Now(),
	}
	s.searchWebhooks[webhookID] = webhook
	return true
}

// DeleteSearchWebhook deletes a search webhook
func (s *State) DeleteSearchWebhook(webhookID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.searchWebhooks[webhookID]; exists {
		delete(s.searchWebhooks, webhookID)
		return true
	}
	return false
}

// DM Conversation methods

// CreateDMConversation creates a new DM conversation
func (s *State) CreateDMConversation(participantIDs []string) *DMConversation {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversationID := s.generateIDUnlocked()
	conversation := &DMConversation{
		ID:             conversationID,
		CreatedAt:      time.Now(),
		ParticipantIDs: participantIDs,
	}
	s.dmConversations[conversationID] = conversation
	return conversation
}

// GetDMConversation gets a DM conversation by ID
func (s *State) GetDMConversation(id string) *DMConversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dmConversations[id]
}

// GetDMConversations gets all DM conversations for a user (by participant ID)
func (s *State) GetDMConversations(userID string) []*DMConversation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var conversations []*DMConversation
	for _, conv := range s.dmConversations {
		// Check if user is a participant
		for _, pid := range conv.ParticipantIDs {
			if pid == userID {
				conversations = append(conversations, conv)
				break
			}
		}
	}
	return conversations
}

// GetDMConversationByParticipants gets a conversation between specific participants
func (s *State) GetDMConversationByParticipants(participantIDs []string) *DMConversation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a set for quick lookup
	participantSet := make(map[string]bool)
	for _, pid := range participantIDs {
		participantSet[pid] = true
	}

	for _, conv := range s.dmConversations {
		if len(conv.ParticipantIDs) != len(participantIDs) {
			continue
		}
		// Check if all participants match
		allMatch := true
		for _, pid := range conv.ParticipantIDs {
			if !participantSet[pid] {
				allMatch = false
				break
			}
		}
		if allMatch {
			return conv
		}
	}
	return nil
}

// DM Event methods

// CreateDMEvent creates a new DM event
func (s *State) CreateDMEvent(conversationID, senderID, eventType, text string, participantIDs []string) *DMEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventID := s.generateIDUnlocked()
	event := &DMEvent{
		ID:                eventID,
		Text:              text,
		SenderID:          senderID,
		CreatedAt:         time.Now(),
		DMConversationID:  conversationID,
		EventType:         eventType,
		ParticipantIDs:    participantIDs,
	}
	s.dmEvents[eventID] = event
	return event
}

// GetDMEvent gets a DM event by ID
func (s *State) GetDMEvent(id string) *DMEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dmEvents[id]
}

// GetDMEvents gets all DM events (optionally filtered)
func (s *State) GetDMEvents(conversationID, participantID string, limit int) []*DMEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var events []*DMEvent
	for _, event := range s.dmEvents {
		// Filter by conversation if specified
		if conversationID != "" && event.DMConversationID != conversationID {
			continue
		}
		// Filter by participant if specified
		if participantID != "" {
			found := false
			for _, pid := range event.ParticipantIDs {
				if pid == participantID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		events = append(events, event)
	}
	
	// Sort by CreatedAt descending (newest first)
	// Simple sort - could be improved
	if len(events) > limit && limit > 0 {
		// For now, just return up to limit
		return events[:limit]
	}
	return events
}

// GetDMEventsByConversation gets all events for a conversation
func (s *State) GetDMEventsByConversation(conversationID string, limit int) []*DMEvent {
	return s.GetDMEvents(conversationID, "", limit)
}

// GetDMEventsByParticipant gets all events involving a participant
func (s *State) GetDMEventsByParticipant(participantID string, limit int) []*DMEvent {
	return s.GetDMEvents("", participantID, limit)
}

// DeleteDMEvent deletes a DM event
func (s *State) DeleteDMEvent(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.dmEvents[eventID]; exists {
		delete(s.dmEvents, eventID)
		return true
	}
	return false
}

// Compliance Job methods

// CreateComplianceJob creates a new compliance job
func (s *State) CreateComplianceJob(name, jobType string) *ComplianceJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobID := s.generateIDUnlocked()
	job := &ComplianceJob{
		ID:        jobID,
		Name:      name,
		Type:      jobType,
		Status:    "in_progress",
		CreatedAt: time.Now(),
		UploadURL: fmt.Sprintf("https://api.twitter.com/2/compliance/jobs/%s/upload", jobID),
		UploadExpiresAt: time.Now().Add(24 * time.Hour),
	}
	s.complianceJobs[jobID] = job
	return job
}

// GetComplianceJob gets a compliance job by ID
func (s *State) GetComplianceJob(id string) *ComplianceJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.complianceJobs[id]
}

// GetComplianceJobs gets all compliance jobs
func (s *State) GetComplianceJobs(jobType string) []*ComplianceJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var jobs []*ComplianceJob
	for _, job := range s.complianceJobs {
		if jobType == "" || job.Type == jobType {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// UpdateComplianceJobStatus updates a compliance job status
func (s *State) UpdateComplianceJobStatus(jobID, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, exists := s.complianceJobs[jobID]; exists {
		job.Status = status
		if status == "complete" {
			job.DownloadURL = fmt.Sprintf("https://api.twitter.com/2/compliance/jobs/%s/download", jobID)
			job.DownloadExpiresAt = time.Now().Add(7 * 24 * time.Hour)
		}
		return true
	}
	return false
}

// Community methods

// CreateCommunity creates a new community.
func (s *State) CreateCommunity(name, description string) *Community {
	s.mu.Lock()
	defer s.mu.Unlock()

	communityID := s.generateIDUnlocked()
	community := &Community{
		ID:          communityID,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		MemberCount: 0,
	}
	s.communities[communityID] = community
	return community
}

// GetCommunity gets a community by ID
func (s *State) GetCommunity(id string) *Community {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.communities[id]
}

// SearchCommunities searches communities by name or description
func (s *State) SearchCommunities(query string, limit int) []*Community {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Community
	queryLower := strings.ToLower(query)
	for _, community := range s.communities {
		if len(results) >= limit {
			break
		}
		if query == "" ||
			strings.Contains(strings.ToLower(community.Name), queryLower) ||
			strings.Contains(strings.ToLower(community.Description), queryLower) {
			results = append(results, community)
		}
	}
	return results
}

// News methods.

// CreateNews creates a new news article.
func (s *State) CreateNews(name, summary, hook, category, disclaimer string, contexts *NewsContexts) *News {
	s.mu.Lock()
	defer s.mu.Unlock()

	newsID := s.generateIDUnlocked()
	news := &News{
		ID:         newsID,
		RestID:     newsID,
		Name:       name,
		Summary:    summary,
		Hook:       hook,
		Category:   category,
		UpdatedAt:  time.Now(),
		Disclaimer: disclaimer,
		Contexts:   contexts,
	}
	s.news[newsID] = news
	return news
}

// GetNews gets a news article by ID
func (s *State) GetNews(id string) *News {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.news[id]
}

// SearchNews searches news by name, summary, or hook
func (s *State) SearchNews(query string, limit int) []*News {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*News
	queryLower := strings.ToLower(query)
	for _, article := range s.news {
		if len(results) >= limit {
			break
		}
		if query == "" ||
			strings.Contains(strings.ToLower(article.Name), queryLower) ||
			strings.Contains(strings.ToLower(article.Summary), queryLower) ||
			strings.Contains(strings.ToLower(article.Hook), queryLower) {
			results = append(results, article)
		}
	}
	return results
}

// Note methods.

// CreateNote creates a new note.
func (s *State) CreateNote(text, authorID, postID string) *Note {
	s.mu.Lock()
	defer s.mu.Unlock()

	noteID := s.generateIDUnlocked()
	note := &Note{
		ID:        noteID,
		Text:      text,
		AuthorID:  authorID,
		CreatedAt: time.Now(),
		PostID:    postID,
	}
	s.notes[noteID] = note
	return note
}

// GetNote gets a note by ID
func (s *State) GetNote(id string) *Note {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notes[id]
}

// SearchNotesWritten searches notes written by a user
func (s *State) SearchNotesWritten(authorID string, limit int) []*Note {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Note
	for _, note := range s.notes {
		if len(results) >= limit {
			break
		}
		if note.AuthorID == authorID {
			results = append(results, note)
		}
	}
	return results
}

// SearchPostsEligibleForNotes searches tweets that are eligible for notes
func (s *State) SearchPostsEligibleForNotes(limit int) []*Tweet {
	// Return tweets that don't have notes yet
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Tweet
	notedPostIDs := make(map[string]bool)
	for _, note := range s.notes {
		if note.PostID != "" {
			notedPostIDs[note.PostID] = true
		}
	}

	for _, tweet := range s.tweets {
		if len(results) >= limit {
			break
		}
		if !notedPostIDs[tweet.ID] {
			results = append(results, tweet)
		}
	}
	return results
}

// DeleteNote deletes a note
func (s *State) DeleteNote(noteID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.notes[noteID]; exists {
		delete(s.notes, noteID)
		return true
	}
	return false
}

// Activity Subscription methods

// CreateActivitySubscription creates a new activity subscription
func (s *State) CreateActivitySubscription(userID string) *ActivitySubscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriptionID := s.generateIDUnlocked()
	subscription := &ActivitySubscription{
		ID:        subscriptionID,
		UserID:    userID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Active:    true,
	}
	s.activitySubscriptions[subscriptionID] = subscription
	return subscription
}

// GetActivitySubscription gets an activity subscription by ID
func (s *State) GetActivitySubscription(id string) *ActivitySubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activitySubscriptions[id]
}

// GetActivitySubscriptions gets all activity subscriptions
func (s *State) GetActivitySubscriptions(userID string) []*ActivitySubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var subscriptions []*ActivitySubscription
	for _, sub := range s.activitySubscriptions {
		if userID == "" || sub.UserID == userID {
			subscriptions = append(subscriptions, sub)
		}
	}
	return subscriptions
}

// UpdateActivitySubscription updates an activity subscription
func (s *State) UpdateActivitySubscription(subscriptionID string, active bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if subscription, exists := s.activitySubscriptions[subscriptionID]; exists {
		subscription.Active = active
		subscription.UpdatedAt = time.Now()
		return true
	}
	return false
}

// DeleteActivitySubscription deletes an activity subscription
func (s *State) DeleteActivitySubscription(subscriptionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.activitySubscriptions[subscriptionID]; exists {
		delete(s.activitySubscriptions, subscriptionID)
		return true
	}
	return false
}

// GetUsers gets multiple users by IDs
// Returns pointers to users, which are safe to read from but should not be modified.
// Pointers remain valid even if users are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetUsers(ids []string) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var users []*User
	for _, id := range ids {
		if user := s.users[id]; user != nil {
			users = append(users, user)
		}
	}
	return users
}

// GetAllUsers returns all users (for search)
// Returns pointers to users, which are safe to read from but should not be modified.
// Pointers remain valid even if users are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetAllUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users
}

// GetList gets a list by ID
// Returns a pointer to the list, which is safe to read from but should not be modified.
// The pointer remains valid even if the list is deleted from the map, but may point to stale data.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetList(id string) *List {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lists[id]
}

// GetLists gets multiple lists by IDs
// Returns pointers to lists, which are safe to read from but should not be modified.
// Pointers remain valid even if lists are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetLists(ids []string) []*List {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lists []*List
	for _, id := range ids {
		if list := s.lists[id]; list != nil {
			lists = append(lists, list)
		}
	}
	return lists
}

// GetSpace gets a space by ID
// Returns a pointer to the space, which is safe to read from but should not be modified.
// The pointer remains valid even if the space is deleted from the map, but may point to stale data.
// Callers should use the returned pointer immediately and only for reading.
func (s *State) GetSpace(id string) *Space {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spaces[id]
}

// GetSpaces gets multiple spaces by IDs
// Returns pointers to spaces, which are safe to read from but should not be modified.
// Pointers remain valid even if spaces are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetSpaces(ids []string) []*Space {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var spaces []*Space
	for _, id := range ids {
		if space := s.spaces[id]; space != nil {
			spaces = append(spaces, space)
		}
	}
	return spaces
}

// GetSpacesByCreator gets spaces created by a user
// Returns pointers to spaces, which are safe to read from but should not be modified.
// Pointers remain valid even if spaces are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetSpacesByCreator(creatorID string) []*Space {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var spaces []*Space
	for _, space := range s.spaces {
		if space.CreatorID == creatorID || stringSliceContains(space.HostIDs, creatorID) {
			spaces = append(spaces, space)
		}
	}
	return spaces
}

// GetSpaceTweets gets tweets from a space (tweets associated with the space)
func (s *State) GetSpaceTweets(spaceID string) []*Tweet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tweets []*Tweet
	for _, tweet := range s.tweets {
		if tweet.SpaceID == spaceID {
			tweets = append(tweets, tweet)
		}
	}
	return tweets
}

// GetSpaceBuyers gets users who bought tickets for a ticketed space
func (s *State) GetSpaceBuyers(spaceID string) []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	space := s.spaces[spaceID]
	if space == nil || len(space.BuyerIDs) == 0 {
		return []*User{}
	}

	var buyers []*User
	for _, buyerID := range space.BuyerIDs {
		if user := s.users[buyerID]; user != nil {
			buyers = append(buyers, user)
		}
	}
	return buyers
}

// GetPersonalizedTrends returns all personalized trends
func (s *State) GetPersonalizedTrends() []*PersonalizedTrend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Handle nil slice (backward compatibility)
	if s.personalizedTrends == nil {
		return []*PersonalizedTrend{}
	}
	// Return a copy to prevent external modification
	result := make([]*PersonalizedTrend, len(s.personalizedTrends))
	copy(result, s.personalizedTrends)
	return result
}

// Helper function to check if slice contains string
func stringSliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetPoll gets a poll by ID
func (s *State) GetPoll(id string) *Poll {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.polls[id]
}

// GetPlace gets a place by ID
func (s *State) GetPlace(id string) *Place {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.places[id]
}

// CreateList creates a new list
func (s *State) CreateList(name, description, ownerID string, private bool) *List {
	s.mu.Lock()
	defer s.mu.Unlock()

	listID := s.generateIDUnlocked()
	list := &List{
		ID:          listID,
		Name:        name,
		Description: description,
		OwnerID:     ownerID,
		Private:     private,
		CreatedAt:   time.Now(),
		Members:     make([]string, 0),
		Followers:   make([]string, 0),
	}

	s.lists[listID] = list

	// Add to owner's lists
	if owner := s.users[ownerID]; owner != nil {
		owner.Lists = append(owner.Lists, listID)
	}

	return list
}

// UpdateList updates a list
func (s *State) UpdateList(listID, name, description string, private *bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	if list == nil {
		return false
	}

	if name != "" {
		list.Name = name
	}
	if description != "" {
		list.Description = description
	}
	if private != nil {
		list.Private = *private
	}

	return true
}

// DeleteList deletes a list
func (s *State) DeleteList(listID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	if list == nil {
		return false
	}

	// Remove from owner's lists
	if owner := s.users[list.OwnerID]; owner != nil {
		for i, id := range owner.Lists {
			if id == listID {
				owner.Lists = append(owner.Lists[:i], owner.Lists[i+1:]...)
				break
			}
		}
	}

	delete(s.lists, listID)
	return true
}

// LikeTweet adds a like relationship
func (s *State) LikeTweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Check if already liked
	for _, id := range user.LikedTweets {
		if id == tweetID {
			return true // Already liked
		}
	}

	user.LikedTweets = append(user.LikedTweets, tweetID)
	tweet.LikedBy = append(tweet.LikedBy, userID)
	tweet.PublicMetrics.LikeCount++

	return true
}

// UnlikeTweet removes a like relationship
func (s *State) UnlikeTweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Remove from user's liked tweets
	for i, id := range user.LikedTweets {
		if id == tweetID {
			user.LikedTweets = append(user.LikedTweets[:i], user.LikedTweets[i+1:]...)
			break
		}
	}

	// Remove from tweet's liked by
	for i, id := range tweet.LikedBy {
		if id == userID {
			tweet.LikedBy = append(tweet.LikedBy[:i], tweet.LikedBy[i+1:]...)
			if tweet.PublicMetrics.LikeCount > 0 {
				tweet.PublicMetrics.LikeCount--
			}
			return true
		}
	}

	return false
}

// Retweet adds a retweet relationship
func (s *State) Retweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Check if already retweeted
	for _, id := range user.RetweetedTweets {
		if id == tweetID {
			return true // Already retweeted
		}
	}

	user.RetweetedTweets = append(user.RetweetedTweets, tweetID)
	tweet.RetweetedBy = append(tweet.RetweetedBy, userID)
	tweet.PublicMetrics.RetweetCount++

	return true
}

// Unretweet removes a retweet relationship
func (s *State) Unretweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Remove from user's retweeted tweets
	for i, id := range user.RetweetedTweets {
		if id == tweetID {
			user.RetweetedTweets = append(user.RetweetedTweets[:i], user.RetweetedTweets[i+1:]...)
			break
		}
	}

	// Remove from tweet's retweeted by
	for i, id := range tweet.RetweetedBy {
		if id == userID {
			tweet.RetweetedBy = append(tweet.RetweetedBy[:i], tweet.RetweetedBy[i+1:]...)
			if tweet.PublicMetrics.RetweetCount > 0 {
				tweet.PublicMetrics.RetweetCount--
			}
			return true
		}
	}

	return false
}

// FollowUser adds a follow relationship
func (s *State) FollowUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	target := s.users[targetUserID]
	if source == nil || target == nil || sourceUserID == targetUserID {
		return false
	}

	// Check if already following
	for _, id := range source.Following {
		if id == targetUserID {
			return true // Already following
		}
	}

	source.Following = append(source.Following, targetUserID)
	target.Followers = append(target.Followers, sourceUserID)
	source.PublicMetrics.FollowingCount++
	target.PublicMetrics.FollowersCount++

	return true
}

// UnfollowUser removes a follow relationship
func (s *State) UnfollowUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	target := s.users[targetUserID]
	if source == nil || target == nil {
		return false
	}

	// Remove from source's following
	for i, id := range source.Following {
		if id == targetUserID {
			source.Following = append(source.Following[:i], source.Following[i+1:]...)
			if source.PublicMetrics.FollowingCount > 0 {
				source.PublicMetrics.FollowingCount--
			}
			break
		}
	}

	// Remove from target's followers
	for i, id := range target.Followers {
		if id == sourceUserID {
			target.Followers = append(target.Followers[:i], target.Followers[i+1:]...)
			if target.PublicMetrics.FollowersCount > 0 {
				target.PublicMetrics.FollowersCount--
			}
			return true
		}
	}

	return false
}

// unfollowUserUnlocked performs the unfollow operation without acquiring a lock
// Caller must hold s.mu
func (s *State) unfollowUserUnlocked(sourceUserID, targetUserID string) bool {
	source := s.users[sourceUserID]
	target := s.users[targetUserID]
	if source == nil || target == nil {
		return false
	}

	// Remove from source's following
	for i, id := range source.Following {
		if id == targetUserID {
			source.Following = append(source.Following[:i], source.Following[i+1:]...)
			if source.PublicMetrics.FollowingCount > 0 {
				source.PublicMetrics.FollowingCount--
			}
			break
		}
	}

	// Remove from target's followers
	for i, id := range target.Followers {
		if id == sourceUserID {
			target.Followers = append(target.Followers[:i], target.Followers[i+1:]...)
			if target.PublicMetrics.FollowersCount > 0 {
				target.PublicMetrics.FollowersCount--
			}
			return true
		}
	}

	return false
}

// BlockUser adds a block relationship
func (s *State) BlockUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	target := s.users[targetUserID]
	if source == nil || target == nil || sourceUserID == targetUserID {
		return false
	}

	// Check if already blocked
	for _, id := range source.BlockedUsers {
		if id == targetUserID {
			return true // Already blocked
		}
	}

	source.BlockedUsers = append(source.BlockedUsers, targetUserID)

	// If following, also unfollow (use unlocked version since we already have the lock)
	s.unfollowUserUnlocked(sourceUserID, targetUserID)

	return true
}

// UnblockUser removes a block relationship
func (s *State) UnblockUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	if source == nil {
		return false
	}

	for i, id := range source.BlockedUsers {
		if id == targetUserID {
			source.BlockedUsers = append(source.BlockedUsers[:i], source.BlockedUsers[i+1:]...)
			return true
		}
	}

	return false
}

// MuteUser adds a mute relationship
func (s *State) MuteUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	target := s.users[targetUserID]
	if source == nil || target == nil || sourceUserID == targetUserID {
		return false
	}

	// Check if already muted
	for _, id := range source.MutedUsers {
		if id == targetUserID {
			return true // Already muted
		}
	}

	source.MutedUsers = append(source.MutedUsers, targetUserID)
	return true
}

// UnmuteUser removes a mute relationship
func (s *State) UnmuteUser(sourceUserID, targetUserID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	source := s.users[sourceUserID]
	if source == nil {
		return false
	}

	for i, id := range source.MutedUsers {
		if id == targetUserID {
			source.MutedUsers = append(source.MutedUsers[:i], source.MutedUsers[i+1:]...)
			return true
		}
	}

	return false
}

// BookmarkTweet adds a bookmark
func (s *State) BookmarkTweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Check if already bookmarked
	for _, id := range user.BookmarkedTweets {
		if id == tweetID {
			return true // Already bookmarked
		}
	}

	user.BookmarkedTweets = append(user.BookmarkedTweets, tweetID)
	tweet.PublicMetrics.BookmarkCount++
	return true
}

// UnbookmarkTweet removes a bookmark
func (s *State) UnbookmarkTweet(userID, tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	tweet := s.tweets[tweetID]
	if user == nil || tweet == nil {
		return false
	}

	// Remove from user's bookmarked tweets
	for i, id := range user.BookmarkedTweets {
		if id == tweetID {
			user.BookmarkedTweets = append(user.BookmarkedTweets[:i], user.BookmarkedTweets[i+1:]...)
			if tweet.PublicMetrics.BookmarkCount > 0 {
				tweet.PublicMetrics.BookmarkCount--
			}
			return true
		}
	}

	return false
}

// CreateSpace creates a new space
func (s *State) CreateSpace(title, creatorID string, scheduledStart time.Time) *Space {
	s.mu.Lock()
	defer s.mu.Unlock()

	spaceID := s.generateSpaceIDUnlocked()
	space := &Space{
		ID:             spaceID,
		State:          "scheduled",
		Title:          title,
		CreatorID:      creatorID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		ScheduledStart: scheduledStart,
		HostIDs:        []string{creatorID},
		TopicIDs:       make([]string, 0),
	}

	s.spaces[spaceID] = space

	// Add to creator's spaces
	if creator := s.users[creatorID]; creator != nil {
		creator.Spaces = append(creator.Spaces, spaceID)
	}

	return space
}

// UpdateSpace updates a space
func (s *State) UpdateSpace(spaceID, title, state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	space := s.spaces[spaceID]
	if space == nil {
		return false
	}

	if title != "" {
		space.Title = title
	}
	if state != "" {
		space.State = state
		if state == "live" && space.StartedAt.IsZero() {
			space.StartedAt = time.Now()
		}
		if state == "ended" && space.EndedAt.IsZero() {
			space.EndedAt = time.Now()
		}
	}
	space.UpdatedAt = time.Now()

	return true
}

// AddListMember adds a user to a list
func (s *State) AddListMember(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	user := s.users[userID]
	if list == nil || user == nil {
		return false
	}

	// Check if already a member
	for _, id := range list.Members {
		if id == userID {
			return true // Already a member
		}
	}

	list.Members = append(list.Members, userID)
	list.MemberCount++
	
	// Add to user's list memberships
	user.ListMemberships = append(user.ListMemberships, listID)

	return true
}

// RemoveListMember removes a user from a list
func (s *State) RemoveListMember(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	user := s.users[userID]
	if list == nil || user == nil {
		return false
	}

	// Remove from list members
	for i, id := range list.Members {
		if id == userID {
			list.Members = append(list.Members[:i], list.Members[i+1:]...)
			if list.MemberCount > 0 {
				list.MemberCount--
			}
			break
		}
	}

	// Remove from user's list memberships
	for i, id := range user.ListMemberships {
		if id == listID {
			user.ListMemberships = append(user.ListMemberships[:i], user.ListMemberships[i+1:]...)
			return true
		}
	}

	return false
}

// FollowList adds a user as a follower of a list
func (s *State) FollowList(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	user := s.users[userID]
	if list == nil || user == nil {
		return false
	}

	// Check if already following
	for _, id := range list.Followers {
		if id == userID {
			return true // Already following
		}
	}

	list.Followers = append(list.Followers, userID)
	list.FollowerCount++
	
	// Add to user's followed lists
	user.FollowedLists = append(user.FollowedLists, listID)

	return true
}

// UnfollowList removes a user as a follower of a list
func (s *State) UnfollowList(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	user := s.users[userID]
	if list == nil || user == nil {
		return false
	}

	// Remove from list followers
	for i, id := range list.Followers {
		if id == userID {
			list.Followers = append(list.Followers[:i], list.Followers[i+1:]...)
			if list.FollowerCount > 0 {
				list.FollowerCount--
			}
			break
		}
	}

	// Remove from user's followed lists
	for i, id := range user.FollowedLists {
		if id == listID {
			user.FollowedLists = append(user.FollowedLists[:i], user.FollowedLists[i+1:]...)
			return true
		}
	}

	return false
}

// HideReply hides a reply tweet
func (s *State) HideReply(tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tweet := s.tweets[tweetID]
	if tweet == nil {
		return false
	}

	// In the real API, this would hide the reply from conversation view
	// For playground, we'll just mark it
	tweet.Hidden = true
	return true
}

// UnhideReply unhides a reply tweet
func (s *State) UnhideReply(tweetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tweet := s.tweets[tweetID]
	if tweet == nil {
		return false
	}

	tweet.Hidden = false
	return true
}

// PinList pins a list for a user
func (s *State) PinList(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.lists[listID]
	user := s.users[userID]
	if list == nil || user == nil {
		return false
	}

	// Check if already pinned
	for _, id := range user.PinnedLists {
		if id == listID {
			return true // Already pinned
		}
	}

	user.PinnedLists = append(user.PinnedLists, listID)
	return true
}

// UnpinList unpins a list for a user
func (s *State) UnpinList(listID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := s.users[userID]
	if user == nil {
		return false
	}

	// Remove from pinned lists
	for i, id := range user.PinnedLists {
		if id == listID {
			user.PinnedLists = append(user.PinnedLists[:i], user.PinnedLists[i+1:]...)
			return true
		}
	}

	return false
}

// GetPlaces gets multiple places by IDs
// Returns pointers to places, which are safe to read from but should not be modified.
// Pointers remain valid even if places are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetPlaces(ids []string) []*Place {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var places []*Place
	for _, id := range ids {
		if place := s.places[id]; place != nil {
			places = append(places, place)
		}
	}
	return places
}

// GetAllPlaces returns all places (for search)
// Returns pointers to places, which are safe to read from but should not be modified.
// Pointers remain valid even if places are deleted from the map, but may point to stale data.
// Callers should use the returned pointers immediately and only for reading.
func (s *State) GetAllPlaces() []*Place {
	s.mu.RLock()
	defer s.mu.RUnlock()

	places := make([]*Place, 0, len(s.places))
	for _, place := range s.places {
		places = append(places, place)
	}
	return places
}

// SearchPlaces searches places by query string
func (s *State) SearchPlaces(query string, limit int) []*Place {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Place
	queryLower := strings.ToLower(query)
	
	for _, place := range s.places {
		if len(results) >= limit {
			break
		}
		
		// Search in name, full_name, country
		if query == "" ||
		   strings.Contains(strings.ToLower(place.Name), queryLower) ||
		   strings.Contains(strings.ToLower(place.FullName), queryLower) ||
		   strings.Contains(strings.ToLower(place.Country), queryLower) {
			results = append(results, place)
		}
	}
	return results
}

// GetTopic gets a topic by ID
func (s *State) GetTopic(id string) *Topic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.topics[id]
}

// GetTopics gets multiple topics by IDs
func (s *State) GetTopics(ids []string) []*Topic {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var topics []*Topic
	for _, id := range ids {
		if topic := s.topics[id]; topic != nil {
			topics = append(topics, topic)
		}
	}
	return topics
}

// GetAllTopics returns all topics
func (s *State) GetAllTopics() []*Topic {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topics := make([]*Topic, 0, len(s.topics))
	for _, topic := range s.topics {
		topics = append(topics, topic)
	}
	return topics
}

// SearchSpaces searches spaces by title
func (s *State) SearchSpaces(query string, limit int) []*Space {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Space
	queryLower := strings.ToLower(query)
	
	for _, space := range s.spaces {
		if len(results) >= limit {
			break
		}
		
		// Search in title
		if query == "" || strings.Contains(strings.ToLower(space.Title), queryLower) {
			results = append(results, space)
		}
	}
	return results
}

// RegisterStreamConnection registers a streaming connection for a user
// Returns a connection ID and a cleanup function to unregister when done
func (s *State) RegisterStreamConnection(userID string, cancelFunc context.CancelFunc) (string, func()) {
	if s == nil {
		return "", func() {}
	}
	
	// Generate a unique connection ID
	connectionID := fmt.Sprintf("%s-%d", userID, time.Now().UnixNano())
	
	s.streamConnMu.Lock()
	defer s.streamConnMu.Unlock()
	
	// Initialize user's connection map if needed
	if s.streamConnections[userID] == nil {
		s.streamConnections[userID] = make(map[string]context.CancelFunc)
	}
	
	// Register the connection
	s.streamConnections[userID][connectionID] = cancelFunc
	
	log.Printf("Registered stream connection %s for user %s (total connections for user: %d)", 
		connectionID, userID, len(s.streamConnections[userID]))
	
	// Return connection ID and cleanup function
	return connectionID, func() {
		s.UnregisterStreamConnection(userID, connectionID)
	}
}

// UnregisterStreamConnection removes a streaming connection for a user
func (s *State) UnregisterStreamConnection(userID, connectionID string) {
	if s == nil {
		return
	}
	
	s.streamConnMu.Lock()
	defer s.streamConnMu.Unlock()
	
	if userConnections, exists := s.streamConnections[userID]; exists {
		if _, hasConn := userConnections[connectionID]; hasConn {
			delete(userConnections, connectionID)
			log.Printf("Unregistered stream connection %s for user %s (remaining connections: %d)", 
				connectionID, userID, len(userConnections))
			
			// Clean up empty user map
			if len(userConnections) == 0 {
				delete(s.streamConnections, userID)
			}
		}
	}
}

// CloseAllStreamConnectionsForUser closes all streaming connections for a specific user
// This simulates the real API behavior where DELETE /2/connections/all closes all streams
func (s *State) CloseAllStreamConnectionsForUser(userID string) int {
	if s == nil {
		return 0
	}
	
	s.streamConnMu.Lock()
	defer s.streamConnMu.Unlock()
	
	userConnections, exists := s.streamConnections[userID]
	if !exists || len(userConnections) == 0 {
		log.Printf("No active stream connections found for user %s", userID)
		return 0
	}
	
	// Cancel all connections for this user
	count := 0
	for connectionID, cancelFunc := range userConnections {
		if cancelFunc != nil {
			cancelFunc()
			count++
			log.Printf("Closed stream connection %s for user %s", connectionID, userID)
		}
	}
	
	// Clear all connections for this user
	delete(s.streamConnections, userID)
	
	log.Printf("Closed %d stream connection(s) for user %s", count, userID)
	return count
}

