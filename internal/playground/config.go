// Package playground defines configuration structures and loading logic.
//
// This file contains all configuration types (PlaygroundConfig, RateLimitConfig,
// AuthConfig, etc.) and functions to load configuration from files or embedded
// defaults. Configuration controls rate limiting, authentication, persistence,
// seeding, and other playground behaviors.
package playground

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

//go:embed configs/*.json
var embeddedConfigs embed.FS

// PlaygroundConfig represents the playground configuration
type PlaygroundConfig struct {
	Tweets    *TweetConfig    `json:"tweets,omitempty"`
	Users     *UserConfig     `json:"users,omitempty"`
	Places    *PlacesConfig   `json:"places,omitempty"`
	Topics    *TopicsConfig   `json:"topics,omitempty"`
	Streaming *StreamingConfig `json:"streaming,omitempty"`
	RateLimit *RateLimitConfig `json:"rate_limit,omitempty"`
	Errors    *ErrorConfig     `json:"errors,omitempty"`
	Auth      *AuthConfig      `json:"auth,omitempty"`
	Persistence *PersistenceConfig `json:"persistence,omitempty"`
	Seeding   *SeedingConfig   `json:"seeding,omitempty"`
}

// TweetConfig contains configuration for tweet seeding
type TweetConfig struct {
	Texts []string `json:"texts,omitempty"` // Custom tweet texts
}

// UserConfig contains configuration for user seeding
type UserConfig struct {
	Profiles []UserProfileConfig `json:"profiles,omitempty"` // Custom user profiles
}

// UserProfileConfig represents a custom user profile
type UserProfileConfig struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Verified    bool   `json:"verified,omitempty"`
	Protected   bool   `json:"protected,omitempty"`
	URL         string `json:"url,omitempty"`
}

// PlacesConfig contains configuration for place seeding
type PlacesConfig struct {
	Places []PlaceConfig `json:"places,omitempty"`
}

// PlaceConfig represents a custom place
type PlaceConfig struct {
	FullName    string  `json:"full_name"`
	Name        string  `json:"name"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	PlaceType   string  `json:"place_type"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// TopicsConfig contains configuration for topic seeding
type TopicsConfig struct {
	Topics []TopicConfig `json:"topics,omitempty"`
}

// TopicConfig represents a custom topic
type TopicConfig struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// StreamingConfig contains configuration for streaming endpoints
type StreamingConfig struct {
	DefaultDelayMs int `json:"default_delay_ms,omitempty"` // Default delay between streamed tweets (milliseconds)
}

// RateLimitConfig contains configuration for rate limiting simulation
type RateLimitConfig struct {
	Enabled   bool `json:"enabled,omitempty"`   // Enable rate limiting simulation
	Limit     int  `json:"limit,omitempty"`      // Requests per window (default: 15)
	WindowSec int  `json:"window_sec,omitempty"` // Window size in seconds (default: 900 = 15 minutes)
	// EndpointOverrides allows per-endpoint rate limit overrides
	// Key format: "METHOD:ENDPOINT" (e.g., "GET:/2/users/me") or "ENDPOINT" for all methods
	EndpointOverrides map[string]EndpointRateLimitOverride `json:"endpoint_overrides,omitempty"`
}

// EndpointRateLimitOverride represents a per-endpoint rate limit override
type EndpointRateLimitOverride struct {
	Limit     int `json:"limit"`      // Requests per window
	WindowSec int `json:"window_sec"` // Window size in seconds
}

// ErrorConfig contains configuration for error simulation
// StatusCode is automatically determined by ErrorType:
//   - "rate_limit" -> 429 (Too Many Requests)
//   - "server_error" -> 500 (Internal Server Error)  
//   - "unauthorized" -> 401 (Unauthorized)
// StatusCode field is kept for backward compatibility but ignored - error_type takes precedence
type ErrorConfig struct {
	Enabled     bool    `json:"enabled,omitempty"`      // Enable error simulation
	ErrorRate   float64 `json:"error_rate,omitempty"`  // Probability of error (0.0-1.0, default: 0.0)
	ErrorType   string  `json:"error_type,omitempty"`  // Type of error: "rate_limit", "server_error", "unauthorized" (default: "rate_limit")
	StatusCode  int     `json:"status_code,omitempty"` // DEPRECATED: Automatically set based on error_type
}

// AuthConfig contains configuration for authentication validation
type AuthConfig struct {
	DisableValidation bool `json:"disable_validation,omitempty"` // If true, allows requests without auth (for testing). Default: false (enforce auth like real API)
}

// PersistenceConfig contains configuration for state persistence
type PersistenceConfig struct {
	Enabled      bool   `json:"enabled,omitempty"`       // Enable state persistence (default: false)
	FilePath     string `json:"file_path,omitempty"`     // Path to state file (default: ~/.playground/state.json)
	AutoSave     bool   `json:"auto_save,omitempty"`     // Auto-save on state changes (default: true if enabled)
	SaveInterval int    `json:"save_interval,omitempty"` // Auto-save interval in seconds (default: 60)
}

// SeedingConfig contains configuration for data seeding amounts
type SeedingConfig struct {
	Users           *SeedingAmountConfig `json:"users,omitempty"`           // User seeding config
	Posts           *SeedingAmountConfig `json:"posts,omitempty"`           // Post/tweet seeding config
	Media           *SeedingAmountConfig `json:"media,omitempty"`           // Media seeding config
	Lists           *SeedingAmountConfig `json:"lists,omitempty"`           // List seeding config
	Spaces          *SeedingAmountConfig `json:"spaces,omitempty"`          // Space seeding config
	Communities     *SeedingAmountConfig `json:"communities,omitempty"`    // Community seeding config
	DMConversations *SeedingAmountConfig `json:"dm_conversations,omitempty"` // DM conversation seeding config
	Relationships   *RelationshipSeedingConfig `json:"relationships,omitempty"` // Relationship seeding config
	LanguageDistribution *LanguageDistributionConfig `json:"language_distribution,omitempty"` // Language distribution config
}

// LanguageDistributionConfig configures how tweets are distributed across languages
type LanguageDistributionConfig struct {
	// SupportedLanguages is the list of language codes to seed (default: ["en", "es", "fr", "ja", "de", "pt", "ko", "ar", "hi", "zh"])
	SupportedLanguages []string `json:"supported_languages,omitempty"`
	// EnglishPercentage is the percentage of tweets that should be English (default: 60)
	// Remaining percentage is distributed evenly among other languages
	EnglishPercentage float64 `json:"english_percentage,omitempty"`
	// MinPerLanguage ensures at least this many tweets per language (default: 5)
	MinPerLanguage int `json:"min_per_language,omitempty"`
}

// SeedingAmountConfig configures the amount of a specific data type to seed
type SeedingAmountConfig struct {
	Min int `json:"min,omitempty"` // Minimum amount (default varies by type)
	Max int `json:"max,omitempty"` // Maximum amount (default varies by type)
	// If both min and max are 0 or not set, uses default amounts
}

// RelationshipSeedingConfig configures relationship seeding
type RelationshipSeedingConfig struct {
	FollowsPerUserMin int `json:"follows_per_user_min,omitempty"` // Min follows per user (default: 3)
	FollowsPerUserMax int `json:"follows_per_user_max,omitempty"` // Max follows per user (default: 15)
	LikesPerPostMin   int `json:"likes_per_post_min,omitempty"`   // Min likes per post (default: 0)
	LikesPerPostMax   int `json:"likes_per_post_max,omitempty"`   // Max likes per post (default: 50)
	RetweetsPerPostMin int `json:"retweets_per_post_min,omitempty"` // Min retweets per post (default: 0)
	RetweetsPerPostMax int `json:"retweets_per_post_max,omitempty"` // Max retweets per post (default: 20)
}

// LoadPlaygroundConfig loads the playground configuration
// First tries to load from ~/.playground/config.json (user config)
// If that doesn't exist, loads the embedded default config
// Returns nil only if both fail
func LoadPlaygroundConfig() (*PlaygroundConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".playground", "config.json")
	
	// Check if user config file exists
	if _, err := os.Stat(configPath); err == nil {
		// User config exists - load it
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read playground config: %w", err)
		}

		var config PlaygroundConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse playground config: %w", err)
		}

		log.Printf("Loaded playground configuration from %s", configPath)
		return &config, nil
	}

	// No user config - load embedded default
	return LoadDefaultPlaygroundConfig()
}

// validateConfig validates configuration values
func validateConfig(config *PlaygroundConfig) error {
	if config.Streaming != nil {
		if config.Streaming.DefaultDelayMs < 0 {
			return fmt.Errorf("streaming.default_delay_ms must be >= 0")
		}
		if config.Streaming.DefaultDelayMs > MaxStreamingDelayMs {
			return fmt.Errorf("streaming.default_delay_ms must be <= %d", MaxStreamingDelayMs)
		}
	}
	if config.RateLimit != nil {
		if config.RateLimit.Limit < 0 {
			return fmt.Errorf("rate_limit.limit must be >= 0")
		}
		if config.RateLimit.WindowSec < 0 {
			return fmt.Errorf("rate_limit.window_sec must be >= 0")
		}
		for key, override := range config.RateLimit.EndpointOverrides {
			if override.Limit < 0 {
				return fmt.Errorf("rate_limit.endpoint_overrides[%s].limit must be >= 0", key)
			}
			if override.WindowSec < 0 {
				return fmt.Errorf("rate_limit.endpoint_overrides[%s].window_sec must be >= 0", key)
			}
		}
	}
	if config.Errors != nil {
		if config.Errors.ErrorRate < 0 || config.Errors.ErrorRate > 1 {
			return fmt.Errorf("errors.error_rate must be between 0 and 1")
		}
	}
	if config.Persistence != nil {
		if config.Persistence.SaveInterval < 0 {
			return fmt.Errorf("persistence.save_interval must be >= 0")
		}
	}
	return nil
}

// LoadDefaultPlaygroundConfig loads the embedded default configuration
func LoadDefaultPlaygroundConfig() (*PlaygroundConfig, error) {
	data, err := embeddedConfigs.ReadFile("configs/default.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded default config: %w", err)
	}

	var config PlaygroundConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse embedded default config: %w", err)
	}

	log.Printf("Using default playground configuration (embedded)")
	return &config, nil
}

// GetSeedingConfig returns the seeding configuration with defaults applied
func (c *PlaygroundConfig) GetSeedingConfig() *SeedingConfig {
	if c.Seeding != nil {
		return c.Seeding
	}
	return &SeedingConfig{} // Return empty config, seeder will use defaults
}

// GetUsersSeeding returns user seeding config with defaults
func (sc *SeedingConfig) GetUsersSeeding() (min, max int) {
	if sc.Users != nil && (sc.Users.Min > 0 || sc.Users.Max > 0) {
		if sc.Users.Min > 0 {
			min = sc.Users.Min
		} else {
			min = 12 // Default min
		}
		if sc.Users.Max > 0 {
			max = sc.Users.Max
		} else {
			max = min // If only min is set, use it as max too
		}
		return
	}
	return 25, 75 // Default: 25-75 users (increased)
}

// GetPostsSeeding returns post seeding config with defaults
func (sc *SeedingConfig) GetPostsSeeding() (min, max int) {
	if sc.Posts != nil && (sc.Posts.Min > 0 || sc.Posts.Max > 0) {
		if sc.Posts.Min > 0 {
			min = sc.Posts.Min
		} else {
			min = 5 // Default min per user
		}
		if sc.Posts.Max > 0 {
			max = sc.Posts.Max
		} else {
			max = min
		}
		return
	}
	return 10, 50 // Default: 10-50 posts per user (increased)
}

// GetMediaSeeding returns media seeding config with defaults
func (sc *SeedingConfig) GetMediaSeeding() (min, max int) {
	if sc.Media != nil && (sc.Media.Min > 0 || sc.Media.Max > 0) {
		if sc.Media.Min > 0 {
			min = sc.Media.Min
		} else {
			min = 10 // Default min
		}
		if sc.Media.Max > 0 {
			max = sc.Media.Max
		} else {
			max = min
		}
		return
	}
	return 20, 100 // Default: 20-100 media items (increased)
}

// GetListsSeeding returns list seeding config with defaults
func (sc *SeedingConfig) GetListsSeeding() (min, max int) {
	if sc.Lists != nil && (sc.Lists.Min > 0 || sc.Lists.Max > 0) {
		if sc.Lists.Min > 0 {
			min = sc.Lists.Min
		} else {
			min = 5 // Default min
		}
		if sc.Lists.Max > 0 {
			max = sc.Lists.Max
		} else {
			max = min
		}
		return
	}
	return 10, 30 // Default: 10-30 lists (increased)
}

// GetSpacesSeeding returns space seeding config with defaults
func (sc *SeedingConfig) GetSpacesSeeding() (min, max int) {
	if sc.Spaces != nil && (sc.Spaces.Min > 0 || sc.Spaces.Max > 0) {
		if sc.Spaces.Min > 0 {
			min = sc.Spaces.Min
		} else {
			min = 3 // Default min
		}
		if sc.Spaces.Max > 0 {
			max = sc.Spaces.Max
		} else {
			max = min
		}
		return
	}
	return 5, 25 // Default: 5-25 spaces (increased)
}

// GetCommunitiesSeeding returns community seeding config with defaults
func (sc *SeedingConfig) GetCommunitiesSeeding() (min, max int) {
	if sc.Communities != nil && (sc.Communities.Min > 0 || sc.Communities.Max > 0) {
		if sc.Communities.Min > 0 {
			min = sc.Communities.Min
		} else {
			min = 5 // Default min
		}
		if sc.Communities.Max > 0 {
			max = sc.Communities.Max
		} else {
			max = min
		}
		return
	}
	return 5, 10 // Default: 5-10 communities
}

// GetDMConversationsSeeding returns DM conversation seeding config with defaults
func (sc *SeedingConfig) GetDMConversationsSeeding() (min, max int) {
	if sc.DMConversations != nil && (sc.DMConversations.Min > 0 || sc.DMConversations.Max > 0) {
		if sc.DMConversations.Min > 0 {
			min = sc.DMConversations.Min
		} else {
			min = 10 // Default min
		}
		if sc.DMConversations.Max > 0 {
			max = sc.DMConversations.Max
		} else {
			max = min
		}
		return
	}
	return 15, 50 // Default: 15-50 DM conversations (increased)
}

// GetRelationshipSeeding returns relationship seeding config with defaults
func (sc *SeedingConfig) GetRelationshipSeeding() *RelationshipSeedingConfig {
	if sc.Relationships != nil {
		cfg := sc.Relationships
		// Apply defaults for unset values
		if cfg.FollowsPerUserMin == 0 {
			cfg.FollowsPerUserMin = 5
		}
		if cfg.FollowsPerUserMax == 0 {
			cfg.FollowsPerUserMax = 25
		}
		if cfg.LikesPerPostMin == 0 {
			cfg.LikesPerPostMin = 2
		}
		if cfg.LikesPerPostMax == 0 {
			cfg.LikesPerPostMax = 100
		}
		if cfg.RetweetsPerPostMin == 0 {
			cfg.RetweetsPerPostMin = 1
		}
		if cfg.RetweetsPerPostMax == 0 {
			cfg.RetweetsPerPostMax = 50
		}
		return cfg
	}
	return &RelationshipSeedingConfig{
		FollowsPerUserMin: 5,
		FollowsPerUserMax: 25,
		LikesPerPostMin:   2,
		LikesPerPostMax:   100,
		RetweetsPerPostMin: 1,
		RetweetsPerPostMax: 50,
	}
}

// GetLanguageDistribution returns language distribution config with defaults
func (sc *SeedingConfig) GetLanguageDistribution() *LanguageDistributionConfig {
	if sc.LanguageDistribution != nil {
		cfg := sc.LanguageDistribution
		// Apply defaults for unset values
		if len(cfg.SupportedLanguages) == 0 {
			cfg.SupportedLanguages = []string{"en", "es", "fr", "ja", "de", "pt", "ko", "ar", "hi", "zh"}
		}
		if cfg.EnglishPercentage == 0 {
			cfg.EnglishPercentage = 60.0
		}
		if cfg.MinPerLanguage == 0 {
			cfg.MinPerLanguage = 5
		}
		return cfg
	}
	return &LanguageDistributionConfig{
		SupportedLanguages: []string{"en", "es", "fr", "ja", "de", "pt", "ko", "ar", "hi", "zh"},
		EnglishPercentage:  60.0,
		MinPerLanguage:     5,
	}
}

// GetDefaultTweetTexts returns the default embedded tweet texts
// This is a fallback if config loading fails
func GetDefaultTweetTexts() []string {
	// Try to load from embedded default config
	if config, err := LoadDefaultPlaygroundConfig(); err == nil {
		if config.Tweets != nil && len(config.Tweets.Texts) > 0 {
			return config.Tweets.Texts
		}
	}
	
	// Fallback to hardcoded defaults (shouldn't normally be reached)
	return []string{
		"Just shipped a new feature! 🚀 Excited to see what the community thinks. #coding #dev",
		"Reading about the latest developments in #AI. The future is fascinating. @tech_influencer",
		"Had a great conversation about API design today. REST vs GraphQL - what are your thoughts? #webdev",
	}
}

// GetTweetTexts returns tweet texts from config if available, otherwise defaults
func (c *PlaygroundConfig) GetTweetTexts() []string {
	if c != nil && c.Tweets != nil && len(c.Tweets.Texts) > 0 {
		return c.Tweets.Texts
	}
	return GetDefaultTweetTexts()
}

// GetUserProfiles returns user profiles from config if available, otherwise nil
func (c *PlaygroundConfig) GetUserProfiles() []UserProfileConfig {
	if c != nil && c.Users != nil && len(c.Users.Profiles) > 0 {
		return c.Users.Profiles
	}
	return nil
}

// GetPlaces returns places from config if available, otherwise nil
func (c *PlaygroundConfig) GetPlaces() []PlaceConfig {
	if c != nil && c.Places != nil && len(c.Places.Places) > 0 {
		return c.Places.Places
	}
	return nil
}

// GetTopics returns topics from config if available, otherwise nil
func (c *PlaygroundConfig) GetTopics() []TopicConfig {
	if c != nil && c.Topics != nil && len(c.Topics.Topics) > 0 {
		return c.Topics.Topics
	}
	return nil
}

// GetStreamingDelayMs returns the default streaming delay from config, or default value
func (c *PlaygroundConfig) GetStreamingDelayMs() int {
	if c != nil && c.Streaming != nil && c.Streaming.DefaultDelayMs > 0 {
		return c.Streaming.DefaultDelayMs
	}
	return 100 // Default 100ms
}

// GetRateLimitConfig returns rate limit configuration with defaults
func (c *PlaygroundConfig) GetRateLimitConfig() *RateLimitConfig {
	if c != nil && c.RateLimit != nil {
		config := *c.RateLimit
		if config.Limit <= 0 {
			config.Limit = 15
		}
		if config.WindowSec <= 0 {
			config.WindowSec = 900 // 15 minutes
		}
		return &config
	}
	return &RateLimitConfig{
		Enabled:   true, // Default: enabled for realistic API simulation
		Limit:     15,
		WindowSec: 900,
	}
}

// GetErrorConfig returns error configuration with defaults
// StatusCode is automatically determined by ErrorType - status_code field is ignored
func (c *PlaygroundConfig) GetErrorConfig() *ErrorConfig {
	if c != nil && c.Errors != nil {
		config := *c.Errors
		if config.ErrorRate < 0 {
			config.ErrorRate = 0
		}
		if config.ErrorRate > 1 {
			config.ErrorRate = 1
		}
		if config.ErrorType == "" {
			config.ErrorType = "rate_limit"
		}
		// CRITICAL: Status code is ALWAYS determined by error type, not user input
		// This prevents invalid combinations like rate_limit with 500 status code
			switch config.ErrorType {
			case "rate_limit":
				config.StatusCode = 429
			case "server_error":
				config.StatusCode = 500
			case "unauthorized":
				config.StatusCode = 401
			case "not_found":
				// Legacy support: convert not_found to rate_limit (not_found doesn't apply to all endpoints)
				config.ErrorType = "rate_limit"
				config.StatusCode = 429
			default:
				// Default to rate_limit if unknown type
				config.ErrorType = "rate_limit"
				config.StatusCode = 429
			}
		return &config
	}
	return &ErrorConfig{
		Enabled:    false,
		ErrorRate:  0,
		ErrorType:  "rate_limit",
		StatusCode: 429,
	}
}

// GetAuthConfig returns auth configuration with defaults
func (c *PlaygroundConfig) GetAuthConfig() *AuthConfig {
	if c != nil && c.Auth != nil {
		return c.Auth
	}
	return &AuthConfig{
		DisableValidation: false, // Default: enforce auth like real API
	}
}

// GetPersistenceConfig returns persistence configuration with defaults
func (c *PlaygroundConfig) GetPersistenceConfig() *PersistenceConfig {
	if c != nil && c.Persistence != nil {
		config := *c.Persistence
		if config.FilePath == "" {
			// Default to ~/.playground/state.json
			homeDir, err := os.UserHomeDir()
			if err == nil {
				config.FilePath = filepath.Join(homeDir, ".playground", "state.json")
			} else {
				config.FilePath = "playground-state.json" // Fallback
			}
		}
		if config.SaveInterval <= 0 {
			config.SaveInterval = 60 // Default 60 seconds
		}
		if config.Enabled && !config.AutoSave {
			// If enabled but auto_save not explicitly set, default to true
			config.AutoSave = true
		}
		return &config
	}
	return &PersistenceConfig{
		Enabled:      true, // Default: enabled to preserve state across restarts
		AutoSave:     true,
		SaveInterval: 60,
	}
}

