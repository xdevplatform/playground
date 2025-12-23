// Package playground seeds the playground with realistic initial data.
//
// This file generates interconnected test data including users, tweets,
// relationships (following, likes, retweets), lists, and other entities.
// It creates realistic data patterns with proper relationships and metadata
// to provide a useful starting point for testing and development.
package playground

import (
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

// Pre-compiled regex patterns for entity extraction
var (
	hashtagRegex = regexp.MustCompile(`#(\w+)`)
	mentionRegex = regexp.MustCompile(`@(\w+)`)
	urlRegex     = regexp.MustCompile(`https?://[^\s]+`)
	cashtagRegex = regexp.MustCompile(`\$([A-Z]{1,5})`)
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// seedRealisticData seeds the playground with realistic, interconnected data
func seedRealisticData(state *State, config *PlaygroundConfig) {
	seeder := &Seeder{
		state: state,
		config: config,
		seedingConfig: config.GetSeedingConfig(),
	}
	seeder.Seed()
}

// Seeder creates and seeds realistic mock data
type Seeder struct {
	state          *State
	config         *PlaygroundConfig
	seedingConfig  *SeedingConfig
	users          []*User
	tweets         []*Tweet
	lists          []*List
	spaces         []*Space
	media          []*Media
	polls          []*Poll
	places         []*Place
	playgroundUser *User
	tweetIDCounter int64 // Separate counter for tweet IDs starting at 0
	listIDCounter  int64 // Separate counter for list IDs starting at 0
}

// Seed seeds all data
func (s *Seeder) Seed() {
	s.seedPlaces()
	s.seedTopics()
	s.seedUsers()
	s.seedTweets()
	s.seedLists()
	s.seedSpaces()
	s.seedCommunities()
	s.seedMedia()
	s.seedPolls()
	s.seedNews()
	s.seedPersonalizedTrends()
	s.seedRelationships()
	s.seedPlaygroundUserRetweets()
	s.seedDMConversations()
	s.updateMetrics()
}

// seedPlaces seeds geographic places
func (s *Seeder) seedPlaces() {
	// Check if config provides custom places
	var places []struct {
		fullName    string
		name        string
		country     string
		countryCode string
		placeType   string
		lat, lon    float64
	}

	if s.config != nil {
		configPlaces := s.config.GetPlaces()
		if len(configPlaces) > 0 {
			// Use config places
			for _, cp := range configPlaces {
				places = append(places, struct {
					fullName    string
					name        string
					country     string
					countryCode string
					placeType   string
					lat, lon    float64
				}{
					cp.FullName, cp.Name, cp.Country, cp.CountryCode, cp.PlaceType, cp.Latitude, cp.Longitude,
				})
			}
		}
	}

	// Use defaults if no config places
	if len(places) == 0 {
		places = []struct {
			fullName    string
			name        string
			country     string
			countryCode string
			placeType   string
			lat, lon    float64
		}{
			{"San Francisco, CA", "San Francisco", "United States", "US", "city", 37.7749, -122.4194},
			{"New York, NY", "New York", "United States", "US", "city", 40.7128, -74.0060},
			{"London, England", "London", "United Kingdom", "GB", "city", 51.5074, -0.1278},
			{"Tokyo, Japan", "Tokyo", "Japan", "JP", "city", 35.6762, 139.6503},
		}
	}

	for _, p := range places {
		place := &Place{
			ID:          s.state.generateID(),
			FullName:    p.fullName,
			Name:        p.name,
			Country:     p.country,
			CountryCode: p.countryCode,
			PlaceType:   p.placeType,
			Geo: PlaceGeo{
				Type:        "Point",
				Coordinates: []float64{p.lon, p.lat},
			},
		}
		s.places = append(s.places, place)
		s.state.places[place.ID] = place
	}
}

// seedTopics seeds topics
func (s *Seeder) seedTopics() {
	// Check if config provides custom topics
	var topics []struct {
		name        string
		description string
	}

	if s.config != nil {
		configTopics := s.config.GetTopics()
		if len(configTopics) > 0 {
			// Use config topics
			for _, ct := range configTopics {
				topics = append(topics, struct {
					name        string
					description string
				}{
					ct.Name, ct.Description,
				})
			}
		}
	}

	// Use defaults if no config topics
	if len(topics) == 0 {
		topics = []struct {
			name        string
			description string
		}{
			{"Technology", "Discussions about technology and innovation"},
			{"Programming", "Software development and coding"},
			{"AI", "Artificial intelligence and machine learning"},
			{"News", "Current events and breaking news"},
			{"Sports", "Sports news and updates"},
		}
	}

	for _, t := range topics {
		topic := &Topic{
			ID:          s.state.generateID(),
			Name:        t.name,
			Description: t.description,
		}
		s.state.topics[topic.ID] = topic
	}
}

// seedUsers seeds diverse users
func (s *Seeder) seedUsers() {
	// Check if config provides custom users
	var userDefs []struct {
		name        string
		username    string
		description string
		location    string
		verified    bool
		protected   bool
		tweetCount  int
		url         string
	}

	if s.config != nil {
		configUsers := s.config.GetUserProfiles()
		if len(configUsers) > 0 {
			// Use config users (with default tweet counts)
			for _, cu := range configUsers {
				tweetCount := 50 // Default tweet count
				userDefs = append(userDefs, struct {
					name        string
					username    string
					description string
					location    string
					verified    bool
					protected   bool
					tweetCount  int
					url         string
				}{
					cu.Name, cu.Username, cu.Description, cu.Location, cu.Verified, cu.Protected, tweetCount, cu.URL,
				})
			}
		}
	}

	// Use defaults if no config users
	if len(userDefs) == 0 {
		userDefs = []struct {
			name        string
			username    string
			description string
			location    string
			verified    bool
			protected   bool
			tweetCount  int
			url         string
		}{
			{"Playground User", "playground_user", "Default playground user for testing API endpoints.", "San Francisco, CA", false, false, 50, ""},
			{"Terry Aki", "CleverFox", "Tech, tacos, and interstellar travel. Part-time philosopher, full-time meme curator.", "San Francisco, CA", false, false, 50, ""},
			{"Sue Flay", "DolphinTech", "Sharing the latest in tech and innovation. Verified tech enthusiast.", "San Francisco, CA", true, false, 120, ""},
			{"Paige Turner", "CuriousCat", "Breaking news and current events. Verified news source.", "New York, NY", true, false, 200, ""},
			{"Justin Thyme", "EagerEagle", "Helping developers build amazing things with APIs. Open source enthusiast.", "London, England", false, false, 80, ""},
			{"Carry Okey", "BrilliantBear", "Researching the future of AI. Papers, thoughts, and experiments.", "San Francisco, CA", true, false, 90, ""},
			{"Bob Frapples", "CreativeCobra", "UI/UX designer sharing design inspiration and tips.", "New York, NY", false, false, 60, ""},
			{"Brock Lee", "BoldBadger", "Building the next big thing. Sharing the journey.", "San Francisco, CA", false, false, 70, ""},
			{"Ella Vader", "SwiftSwan", "Maintaining open source projects. Contributor to various ecosystems.", "London, England", false, false, 40, ""},
			{"Ray Ning", "MysticMoose", "This account is protected.", "", false, true, 20, ""},
			{"Will Power", "WiseWolf", "Curating lists of interesting accounts and topics.", "New York, NY", false, false, 30, ""},
			{"Jim Shorts", "VibrantVulture", "Hosting regular spaces on tech and innovation.", "San Francisco, CA", false, false, 25, ""},
			{"Mike Raffone", "AdventureAnt", "Sharing photos and videos from travels and daily life.", "Tokyo, Japan", false, false, 100, ""},
		}
	}

	// Generate additional users if configured
	userMin, userMax := s.seedingConfig.GetUsersSeeding()
	targetUserCount := userMin
	if userMax > userMin {
		targetUserCount = userMin + rand.Intn(userMax-userMin+1)
	}
	
	// Generate additional users beyond the base set if needed
	if targetUserCount > len(userDefs) {
		// Use shorter adjectives and animals to ensure usernames are <= 15 characters
		adjectives := []string{"Bold", "Swift", "Bright", "Calm", "Eager", "Wise", "Vibrant", "Mystic", "Creative", "Brilliant", "Curious", "Fierce", "Gentle", "Noble", "Radiant", "Serene", "Dynamic", "Graceful", "Clever", "Brave"}
		animals := []string{"Eagle", "Bear", "Wolf", "Swan", "Fox", "Lion", "Tiger", "Hawk", "Owl", "Deer", "Rabbit", "Falcon", "Lynx", "Raven", "Dragon", "Cat", "Dog", "Bird", "Fish", "Bee"}
		firstNames := []string{"Alex", "Jordan", "Taylor", "Casey", "Morgan", "Riley", "Avery", "Quinn", "Sage", "River", "Phoenix", "Skyler", "Blake", "Cameron", "Dakota", "Emery", "Finley", "Harper", "Indigo", "Jules"}
		lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin", "Lee"}
		
		for i := len(userDefs); i < targetUserCount; i++ {
			// Generate username ensuring it's <= 15 characters
			var username string
			maxAttempts := 20
			for attempt := 0; attempt < maxAttempts; attempt++ {
				adj := adjectives[rand.Intn(len(adjectives))]
				animal := animals[rand.Intn(len(animals))]
				candidate := adj + animal
				if len(candidate) <= 15 {
					username = candidate
					break
				}
			}
			// Fallback: use shorter format if still too long
			if len(username) > 15 || username == "" {
				shortAdjs := []string{"Bold", "Swift", "Bright", "Calm", "Eager", "Wise", "Fierce", "Noble", "Clever", "Brave"}
				shortAnimals := []string{"Fox", "Cat", "Dog", "Owl", "Hawk", "Bee", "Lynx", "Deer", "Bear", "Wolf"}
				adj := shortAdjs[rand.Intn(len(shortAdjs))]
				animal := shortAnimals[rand.Intn(len(shortAnimals))]
				username = adj + animal
				// Truncate if still too long (shouldn't happen, but safety check)
				if len(username) > 15 {
					username = username[:15]
				}
			}
			firstName := firstNames[rand.Intn(len(firstNames))]
			lastName := lastNames[rand.Intn(len(lastNames))]
			name := firstName + " " + lastName
			
			locations := []string{"San Francisco, CA", "New York, NY", "London, England", "Tokyo, Japan", "Los Angeles, CA", "Chicago, IL", "Boston, MA", "Seattle, WA", "Austin, TX", "Denver, CO"}
			location := locations[rand.Intn(len(locations))]
			
			descriptions := []string{
				"Building cool things with code.",
				"Tech enthusiast and lifelong learner.",
				"Sharing thoughts on software and life.",
				"Developer by day, dreamer by night.",
				"Exploring the intersection of tech and creativity.",
				"Making the web a better place, one commit at a time.",
				"Passionate about open source and community.",
				"Always learning, always building.",
			}
			description := descriptions[rand.Intn(len(descriptions))]
			
			userDefs = append(userDefs, struct {
				name        string
				username    string
				description string
				location    string
				verified    bool
				protected   bool
				tweetCount  int
				url         string
			}{
				name, username, description, location, false, false, 20 + rand.Intn(60), "",
			})
		}
	}

	baseTime := time.Now().AddDate(-2, 0, 0) // 2 years ago

	for i, def := range userDefs {
		// Validate username format (must match ^[A-Za-z0-9_]{1,15}$)
		if err := ValidateUsername(def.username); err != nil {
			log.Printf("Warning: Invalid username '%s' for user '%s': %v. Skipping user.", def.username, def.name, err)
			continue
		}
		
		var userID string
		// Assign ID "0" to playground user, others get sequential IDs starting from 1
		if def.username == "playground_user" {
			userID = "0"
		} else {
			userID = s.state.generateID()
		}
		
		// Set verified_type based on verified status
		verifiedType := "none"
		if def.verified {
			// Assign different verified types for variety
			verifiedTypes := []string{"blue", "business", "government"}
			verifiedType = verifiedTypes[i%len(verifiedTypes)]
		}
		
		user := &User{
			ID:          userID,
			Name:        def.name,
			Username:    def.username,
			Description: def.description,
			Location:    def.location,
			Verified:    def.verified,
			VerifiedType: verifiedType,
			Protected:   def.protected,
			URL:         def.url,
			CreatedAt:   baseTime.AddDate(0, 0, i*30), // Stagger creation dates
			ProfileImageURL: fmt.Sprintf("https://abs.twimg.com/sticky/default_profile_images/default_profile_%d_normal.png", (i%7)+1), // Vary profile images
			Tweets:      make([]string, 0),
			LikedTweets: make([]string, 0),
			RetweetedTweets: make([]string, 0),
			Following:   make([]string, 0),
			Followers:   make([]string, 0),
			Lists:       make([]string, 0),
			PinnedLists: make([]string, 0),
			ListMemberships: make([]string, 0),
			Spaces:      make([]string, 0),
			MutedUsers:  make([]string, 0),
			BlockedUsers: make([]string, 0),
			Withheld:    nil, // No withheld by default
		}

		s.users = append(s.users, user)
		s.state.users[user.ID] = user
		s.state.users[user.Username] = user // Index by username

		if user.Username == "playground_user" {
			s.playgroundUser = user
		}
	}
}

// seedTweets seeds tweets with realistic content
func (s *Seeder) seedTweets() {
	if len(s.users) == 0 {
		return
	}

	// Get tweet texts from config (or use defaults)
	tweetTexts := GetDefaultTweetTexts()
	if s.config != nil {
		tweetTexts = s.config.GetTweetTexts()
	}

	baseTime := time.Now().AddDate(0, -3, 0) // 3 months ago
	tweetIdx := 0

	// Get post seeding config
	postMin, postMax := s.seedingConfig.GetPostsSeeding()
	
	// Get language distribution config
	langConfig := s.seedingConfig.GetLanguageDistribution()
	supportedLanguages := langConfig.SupportedLanguages
	englishPercentage := langConfig.EnglishPercentage
	minPerLanguage := langConfig.MinPerLanguage
	
	// Track language counts to ensure minimum per language
	langCounts := make(map[string]int)
	for _, lang := range supportedLanguages {
		langCounts[lang] = 0
	}
	
	// First pass: create tweets for each user with language distribution
	allTweets := make([]*Tweet, 0)
	
	for _, user := range s.users {
		// Determine tweet count based on user type and config
		tweetCount := 0
		if user.Username == "playground_user" {
			// Ensure playground user always has at least 5 tweets
			tweetCount = postMin + rand.Intn(postMax-postMin+1)
			if tweetCount < 5 {
				tweetCount = 5
			}
		} else if user.Verified {
			// Verified users get more posts
			tweetCount = postMin*2 + rand.Intn((postMax*2)-(postMin*2)+1)
		} else {
			tweetCount = postMin + rand.Intn(postMax-postMin+1)
		}

		for i := 0; i < tweetCount && tweetIdx < len(tweetTexts)*10; i++ {
			// Assign language based on distribution config
			lang := "en" // Default to English
			randVal := rand.Float64() * 100.0
			if randVal < englishPercentage {
				lang = "en"
			} else {
				// Distribute remaining percentage evenly among other languages
				otherLangs := make([]string, 0)
				for _, l := range supportedLanguages {
					if l != "en" {
						otherLangs = append(otherLangs, l)
					}
				}
				if len(otherLangs) > 0 {
					lang = otherLangs[rand.Intn(len(otherLangs))]
				}
			}
			
			// Get language-appropriate tweet text
			text := getTweetTextForLanguage(lang, tweetTexts, tweetIdx)

			// Assign sequential tweet IDs starting from "0"
			tweetID := fmt.Sprintf("%d", s.tweetIDCounter)
			s.tweetIDCounter++
			
			tweet := &Tweet{
				ID:        tweetID,
				Text:      text,
				AuthorID:  user.ID,
				CreatedAt: baseTime.Add(time.Duration(tweetIdx) * time.Hour),
				EditHistoryTweetIDs: []string{tweetID}, // Initialize with tweet ID (unedited tweet)
				ConversationID: "",
				LikedBy:       make([]string, 0),
				RetweetedBy:   make([]string, 0),
				Replies:       make([]string, 0),
				Quotes:        make([]string, 0),
				Media:         make([]string, 0),
				Source:        generateSource(),
				Lang:          lang,
				PossiblySensitive: rand.Float32() < 0.1, // 10% chance
			}

			// Set conversation ID (same as tweet ID for now, can be updated for replies)
			tweet.ConversationID = tweet.ID

			// Generate entities from text
			tweet.Entities = generateEntities(text, s.users)
			
			// Add attachments if media exists
			if len(tweet.Media) > 0 {
				mediaKeys := make([]string, 0, len(tweet.Media))
				for _, mediaID := range tweet.Media {
					media := s.state.GetMedia(mediaID)
					if media != nil {
						mediaKeys = append(mediaKeys, media.MediaKey)
					}
				}
				if len(mediaKeys) > 0 {
					tweet.Attachments = &TweetAttachments{
						MediaKeys: mediaKeys,
					}
				}
			}

			allTweets = append(allTweets, tweet)
			langCounts[lang]++
			tweetIdx++
		}
	}
	
	// Second pass: ensure minimum tweets per language
	// If any language has fewer than minPerLanguage tweets, add more
	for lang, count := range langCounts {
		if count < minPerLanguage {
			needed := minPerLanguage - count
			for i := 0; i < needed; i++ {
				// Pick a random user
				user := s.users[rand.Intn(len(s.users))]
				
				// Get language-appropriate tweet text
				text := getTweetTextForLanguage(lang, tweetTexts, tweetIdx)
				tweetID := fmt.Sprintf("%d", s.tweetIDCounter)
				s.tweetIDCounter++
				
				tweet := &Tweet{
					ID:        tweetID,
					Text:      text,
					AuthorID:  user.ID,
					CreatedAt: baseTime.Add(time.Duration(tweetIdx) * time.Hour),
					EditHistoryTweetIDs: []string{tweetID},
					ConversationID: "",
					LikedBy:       make([]string, 0),
					RetweetedBy:   make([]string, 0),
					Replies:       make([]string, 0),
					Quotes:        make([]string, 0),
					Media:         make([]string, 0),
					Source:        generateSource(),
					Lang:          lang,
					PossiblySensitive: rand.Float32() < 0.1,
				}
				
				tweet.ConversationID = tweet.ID
				tweet.Entities = generateEntities(text, s.users)
				
				allTweets = append(allTweets, tweet)
				langCounts[lang]++
				tweetIdx++
			}
		}
	}
	
	// Add all tweets to state
	for _, tweet := range allTweets {
		s.tweets = append(s.tweets, tweet)
		s.state.tweets[tweet.ID] = tweet
		// Find user and add tweet to their list
		for _, user := range s.users {
			if user.ID == tweet.AuthorID {
				user.Tweets = append(user.Tweets, tweet.ID)
				break
			}
		}
	}
}

// seedLists seeds lists with members
func (s *Seeder) seedLists() {
	// Debug logging for list seeding
	if HandlerDebug {
		log.Printf("seedLists: Starting, users count: %d", len(s.users))
	}
	if len(s.users) == 0 {
		if HandlerDebug {
			log.Printf("seedLists: No users, returning early")
		}
		return
	}

	// Ensure we have users to assign as list owners
	if len(s.users) == 0 {
		if HandlerDebug {
			log.Printf("seedLists: No users available, returning early")
		}
		return
	}
	if HandlerDebug {
		log.Printf("seedLists: Will distribute list ownership across %d users", len(s.users))
	}

	// Get list seeding config
	var numLists int
	var listMin, listMax int
	if s.seedingConfig == nil {
		listMin, listMax = 5, 10
		numLists = listMin
		if listMax > listMin {
			numLists = listMin + rand.Intn(listMax-listMin+1)
		}
		if HandlerDebug {
			log.Printf("seedLists: Will create %d lists (default)", numLists)
		}
	} else {
		listMin, listMax = s.seedingConfig.GetListsSeeding()
		numLists = listMin
		if listMax > listMin {
			numLists = listMin + rand.Intn(listMax-listMin+1)
		}
		if HandlerDebug {
			log.Printf("seedLists: Config loaded, will create %d lists (min: %d, max: %d)", numLists, listMin, listMax)
		}
	}
	
	listDefs := []struct {
		name        string
		description string
		private     bool
		memberCount int
	}{
		{"Tech Leaders", "Influential people in technology", false, 5},
		{"API Developers", "Developers working with APIs", false, 4},
		{"Design Inspiration", "Accounts sharing great design", false, 3},
		{"Private List", "My private curated list", true, 2},
		{"News Sources", "Verified news accounts", false, 3},
	}
	// Generate additional lists if needed
	listNames := []string{"Tech Innovators", "Startup Founders", "Open Source Heroes", "Design Masters", "AI Researchers", "Developer Community", "Tech News", "Code Reviewers", "API Enthusiasts", "Crypto Builders"}
	listDescriptions := []string{"Curated list of amazing people", "People to follow", "Best accounts in tech", "Worth following", "Top contributors", "Community leaders", "Thought leaders", "Industry experts"}
	
	iterations := 0
	maxIterations := 1000 // Safety limit to prevent infinite loop
	for len(listDefs) < numLists && iterations < maxIterations {
		iterations++
		if HandlerDebug && iterations%100 == 0 {
			log.Printf("seedLists: Still generating lists... iteration %d, have %d/%d", iterations, len(listDefs), numLists)
		}
		name := listNames[rand.Intn(len(listNames))]
		desc := listDescriptions[rand.Intn(len(listDescriptions))]
		// Avoid duplicates
		exists := false
		for _, def := range listDefs {
			if def.name == name {
				exists = true
				break
			}
		}
		if !exists {
			listDefs = append(listDefs, struct {
				name        string
				description string
				private     bool
				memberCount int
			}{
				name, desc, rand.Float32() < 0.2, 3 + rand.Intn(5),
			})
		}
	}
	if iterations >= maxIterations {
		log.Printf("seedLists: Hit max iterations (%d), stopping list generation", maxIterations)
	}
	
	listIdx := 0
	for _, def := range listDefs {
		listIdx++
		if HandlerDebug && listIdx%10 == 0 {
			log.Printf("seedLists: Creating list %d/%d: %s", listIdx, len(listDefs), def.name)
		}
		
		// Randomly assign ownership to different users
		// Distribute ownership: ~40% to playground user, ~60% to other users
		var owner *User
		if s.playgroundUser != nil && rand.Float32() < 0.4 {
			owner = s.playgroundUser
		} else {
			// Pick a random user from all users
			owner = s.users[rand.Intn(len(s.users))]
		}
		
		listID := fmt.Sprintf("%d", s.listIDCounter)
		s.listIDCounter++
		list := &List{
			ID:            listID,
			Name:          def.name,
			Description:   def.description,
			OwnerID:       owner.ID,
			Private:       def.private,
			CreatedAt:     time.Now().AddDate(0, -1, 0),
			Members:       make([]string, 0),
			Followers:     make([]string, 0),
		}

		// Add members (excluding list owner)
		memberCount := 0
		for _, user := range s.users {
			if user.ID != owner.ID && memberCount < def.memberCount {
				list.Members = append(list.Members, user.ID)
				user.ListMemberships = append(user.ListMemberships, list.ID)
				memberCount++
			}
		}
		list.MemberCount = len(list.Members)

		// Add followers to lists (2-6 followers per list)
		followerCount := 2 + rand.Intn(5)
		for i := 0; i < followerCount && i < len(s.users); i++ {
			followerIdx := rand.Intn(len(s.users))
			follower := s.users[followerIdx]
			if follower.ID != owner.ID && !contains(list.Followers, follower.ID) {
				list.Followers = append(list.Followers, follower.ID)
			}
		}

		s.lists = append(s.lists, list)
		s.state.lists[list.ID] = list
		owner.Lists = append(owner.Lists, list.ID)
	}
	if HandlerDebug {
		log.Printf("seedLists: All %d lists created and added to state", len(listDefs))
	}
}

// seedSpaces seeds spaces
func (s *Seeder) seedSpaces() {
	if len(s.users) == 0 {
		return
	}

	// Use a user that has "VibrantVulture" username (for hosting spaces) or playground_user or first available user
	spaceHost := s.findUserByUsername("VibrantVulture")
	if spaceHost == nil {
		spaceHost = s.playgroundUser
	}
	if spaceHost == nil && len(s.users) > 0 {
		spaceHost = s.users[0]
	}
	if spaceHost == nil {
		return
	}

	// Get space seeding config
	spaceMin, spaceMax := s.seedingConfig.GetSpacesSeeding()
	numSpaces := spaceMin
	if spaceMax > spaceMin {
		numSpaces = spaceMin + rand.Intn(spaceMax-spaceMin+1)
	}
	
		spaceDefs := []struct {
		title  string
		state  string
		hoursAgo int
		isTicketed bool
	}{
		{"Weekly Tech Discussion", "ended", 24, false},
		{"API Design Best Practices", "scheduled", -48, true}, // Future, ticketed
		{"Open Source Community Chat", "live", 0, false},
		{"Premium Tech Conference", "scheduled", -24, true}, // Ticketed space
		{"Exclusive Developer Meetup", "live", -1, true}, // Live ticketed space
	}
	
	// Generate additional spaces if needed
	spaceTitles := []string{"Tech Talk", "Developer Q&A", "Startup Stories", "Code Review Session", "API Deep Dive", "Design Critique", "AI Discussion", "Open Source Hour", "Tech News Roundup", "Community Meetup"}
	states := []string{"ended", "scheduled", "live"}
	
	for len(spaceDefs) < numSpaces {
		title := spaceTitles[rand.Intn(len(spaceTitles))]
		state := states[rand.Intn(len(states))]
		hoursAgo := rand.Intn(72) - 24 // -24 to 48 hours
		if state == "scheduled" {
			hoursAgo = -(24 + rand.Intn(48)) // Future
		}
		isTicketed := rand.Intn(4) == 0 // 25% chance of being ticketed
		
		spaceDefs = append(spaceDefs, struct {
			title  string
			state  string
			hoursAgo int
			isTicketed bool
		}{
			title, state, hoursAgo, isTicketed,
		})
	}

	for _, def := range spaceDefs {
		space := &Space{
			ID:             s.state.generateSpaceID(),
			Title:          def.title,
			State:          def.state,
			CreatedAt:      time.Now().Add(time.Duration(-def.hoursAgo) * time.Hour),
			UpdatedAt:      time.Now().Add(time.Duration(-def.hoursAgo) * time.Hour),
			CreatorID:      spaceHost.ID,
			HostIDs:        []string{spaceHost.ID},
			SpeakerIDs:     make([]string, 0),
			InvitedUserIDs: make([]string, 0),
			IsTicketed:     def.isTicketed,
			Lang:           "en",
			BuyerIDs:       make([]string, 0),
		}

		if def.state == "scheduled" {
			space.ScheduledStart = time.Now().Add(time.Duration(-def.hoursAgo) * time.Hour)
		} else if def.state == "live" {
			space.StartedAt = time.Now().Add(time.Duration(-def.hoursAgo) * time.Hour)
		} else if def.state == "ended" {
			space.StartedAt = time.Now().Add(time.Duration(-def.hoursAgo-2) * time.Hour)
			space.EndedAt = time.Now().Add(time.Duration(-def.hoursAgo) * time.Hour)
		}

		// Add some speakers
		for i, user := range s.users {
			if i < 3 && user.ID != spaceHost.ID {
				space.SpeakerIDs = append(space.SpeakerIDs, user.ID)
			}
		}

		// Add participant and subscriber counts
		if def.state == "live" || def.state == "ended" {
			space.ParticipantCount = 50 + rand.Intn(200)
			space.SubscriberCount = 100 + rand.Intn(500)
		}

		// Add some topic IDs
		topicIDs := make([]string, 0)
		for topicID := range s.state.topics {
			if len(topicIDs) < 2 {
				topicIDs = append(topicIDs, topicID)
			}
		}
		space.TopicIDs = topicIDs

		// Add buyers for ticketed spaces
		if def.isTicketed && len(s.users) > 1 {
			numBuyers := 2 + rand.Intn(5) // 2-6 buyers
			buyerIDs := make([]string, 0)
			usedIndices := make(map[int]bool)
			for len(buyerIDs) < numBuyers && len(buyerIDs) < len(s.users)-1 {
				idx := rand.Intn(len(s.users))
				if !usedIndices[idx] && s.users[idx].ID != spaceHost.ID {
					buyerIDs = append(buyerIDs, s.users[idx].ID)
					usedIndices[idx] = true
				}
			}
			space.BuyerIDs = buyerIDs
		}

		s.spaces = append(s.spaces, space)
		s.state.spaces[space.ID] = space
		spaceHost.Spaces = append(spaceHost.Spaces, space.ID)
	}

	// Link some tweets to spaces (tweets about or promoting spaces)
	// Link 2-5 tweets per space
	for _, space := range s.spaces {
		if len(s.tweets) == 0 {
			break
		}
		numTweetsToLink := 2 + rand.Intn(4) // 2-5 tweets
		usedTweetIndices := make(map[int]bool)
		linkedCount := 0
		
		for linkedCount < numTweetsToLink && linkedCount < len(s.tweets) {
			idx := rand.Intn(len(s.tweets))
			if !usedTweetIndices[idx] {
				s.tweets[idx].SpaceID = space.ID
				usedTweetIndices[idx] = true
				linkedCount++
			}
		}
	}
}

// seedCommunities seeds communities
func (s *Seeder) seedCommunities() {
	if len(s.users) == 0 {
		return
	}

	// Get community seeding config (default to 5-10 communities if not configured)
	communityMin, communityMax := s.seedingConfig.GetCommunitiesSeeding()
	numCommunities := communityMin
	if communityMax > communityMin {
		numCommunities = communityMin + rand.Intn(communityMax-communityMin+1)
	}
	
	communityDefs := []struct {
		name        string
		description string
		memberCount int
		access      string
	}{
		{"Tech Innovators", "A community for sharing innovative tech ideas and projects", 25, "public"},
		{"Open Source Contributors", "Connect with open source maintainers and contributors", 30, "public"},
		{"API Developers", "Discussion and resources for API development", 20, "public"},
		{"Startup Founders", "Network and share experiences with fellow founders", 15, "restricted"},
		{"Design Community", "Showcase designs and get feedback from peers", 18, "public"},
		{"AI & Machine Learning", "Latest trends and discussions in AI/ML", 22, "public"},
		{"Web Development", "Resources and discussions for web developers", 28, "public"},
		{"Mobile Developers", "iOS, Android, and cross-platform development", 16, "restricted"},
		{"DevOps Engineers", "Infrastructure, CI/CD, and deployment strategies", 12, "private"},
		{"Data Science", "Data analysis, visualization, and insights", 14, "public"},
	}
	
	// Use only the number of communities requested
	if numCommunities < len(communityDefs) {
		communityDefs = communityDefs[:numCommunities]
	} else if numCommunities > len(communityDefs) {
		// Generate additional communities if needed
		names := []string{"Cloud Computing", "Cybersecurity", "Blockchain", "Game Development", "IoT Developers", "Backend Engineers", "Frontend Masters", "Full Stack", "QA & Testing", "Product Management"}
		descriptions := []string{
			"Cloud infrastructure and services",
			"Security best practices and threats",
			"Blockchain and cryptocurrency development",
			"Game design and development",
			"Internet of Things projects",
			"Server-side development",
			"Client-side development",
			"End-to-end development",
			"Quality assurance and testing",
			"Product strategy and management",
		}
		accessTypes := []string{"public", "restricted", "private"}
		for len(communityDefs) < numCommunities {
			idx := rand.Intn(len(names))
			communityDefs = append(communityDefs, struct {
				name        string
				description string
				memberCount int
				access      string
			}{
				names[idx],
				descriptions[idx],
				10 + rand.Intn(20),
				accessTypes[rand.Intn(len(accessTypes))],
			})
		}
	}

	baseTime := time.Now().AddDate(-1, 0, 0) // 1 year ago

	for i, def := range communityDefs {
		community := s.state.CreateCommunity(def.name, def.description)
		if community != nil {
			// Set creation time
			community.CreatedAt = baseTime.AddDate(0, 0, i*10) // Stagger creation dates
			
			// Add members (random selection from users)
			memberCount := def.memberCount
			if memberCount > len(s.users) {
				memberCount = len(s.users)
			}
			
			// Randomly select members
			selectedMembers := make(map[string]bool)
			for len(selectedMembers) < memberCount {
				userIdx := rand.Intn(len(s.users))
				user := s.users[userIdx]
				if !selectedMembers[user.ID] {
					selectedMembers[user.ID] = true
				}
			}
			
			// Update member count
			community.MemberCount = len(selectedMembers)
			
			// Set access field
			community.Access = def.access
		}
	}
	
	if HandlerDebug {
		log.Printf("seedCommunities: Created %d communities", len(communityDefs))
	}
}

// seedMedia seeds media items
func (s *Seeder) seedMedia() {
	// Get media seeding config
	mediaMin, mediaMax := s.seedingConfig.GetMediaSeeding()
	numMedia := mediaMin
	if mediaMax > mediaMin {
		numMedia = mediaMin + rand.Intn(mediaMax-mediaMin+1)
	}
	
	// Create some media items that can be attached to tweets
	// Media keys must match pattern: ^([0-9]+)_([0-9]+)$ (e.g., "123_456")
	mediaTypes := []string{"photo", "video", "animated_gif"}
	for i := 0; i < numMedia; i++ {
		mediaType := mediaTypes[i%len(mediaTypes)]
		// Generate media key in correct format: two numbers separated by underscore
		// Use a base number and index to create unique keys like "1000_1", "1000_2", etc.
		mediaKey := fmt.Sprintf("%d_%d", 1000+i/100, i%100+1)
		media := &Media{
			ID:               s.state.generateID(),
			MediaKey:         mediaKey,
			Type:             mediaType,
			State:            "succeeded",
			ExpiresAfterSecs: 3600,
			CreatedAt:        time.Now().Add(time.Duration(-i) * time.Hour),
			URL:              fmt.Sprintf("https://pbs.twimg.com/media/example_%d.jpg", i),
			Width:            1200 + (i*50)%500,
			Height:           800 + (i*30)%400,
		}

		if mediaType == "video" {
			media.DurationMs = 30000 + (i*1000)%20000
			media.PreviewImageURL = fmt.Sprintf("https://pbs.twimg.com/media/preview_%d.jpg", i)
		}

		s.media = append(s.media, media)
		s.state.media[media.ID] = media

		// Attach some media to tweets
		if i < len(s.tweets) {
			tweet := s.tweets[i]
			tweet.Media = append(tweet.Media, media.ID)
		}
	}
}

// seedPolls seeds polls
func (s *Seeder) seedPolls() {
	pollDefs := []struct {
		options []string
		minutes int
	}{
		{[]string{"Option A", "Option B", "Option C"}, 60},
		{[]string{"Yes", "No"}, 30},
		{[]string{"Python", "JavaScript", "Go", "Rust"}, 120},
	}

	for _, def := range pollDefs {
		poll := &Poll{
			ID:              s.state.generateID(),
			DurationMinutes: def.minutes,
			EndDatetime:     time.Now().Add(time.Duration(def.minutes) * time.Minute),
			VotingStatus:    "open",
			Options:         make([]PollOption, len(def.options)),
		}

		for i, label := range def.options {
			poll.Options[i] = PollOption{
				Position: i,
				Label:    label,
				Votes:    rand.Intn(100),
			}
		}

		s.polls = append(s.polls, poll)
		s.state.polls[poll.ID] = poll

		// Attach to a tweet
		if len(s.tweets) > 0 {
			// For the first poll, attach to tweet "0" for easy testing
			var tweet *Tweet
			if len(s.polls) == 1 && len(s.tweets) > 0 {
				tweet = s.tweets[0] // Attach first poll to tweet "0"
			} else {
				tweet = s.tweets[rand.Intn(len(s.tweets))]
			}
			tweet.PollID = poll.ID
			// Also add to Attachments.PollIDs to match API structure
			if tweet.Attachments == nil {
				tweet.Attachments = &TweetAttachments{
					PollIDs: []string{poll.ID},
				}
			} else {
				tweet.Attachments.PollIDs = append(tweet.Attachments.PollIDs, poll.ID)
			}
		}
	}
}

// seedNews seeds news articles
func (s *Seeder) seedNews() {
	// Create news articles matching the real API structure
	newsDefs := []struct {
		name        string
		summary     string
		hook        string
		category    string
		disclaimer  string
		topics      []string
		restID      string
	}{
		{
			name: "Developers Experiment with Reverse Test-Driven Development Approach",
			summary: "A growing number of developers are trying a new workflow where they write code first, then use automated tools to generate comprehensive test suites. This approach challenges traditional test-driven development by allowing developers to focus on implementation first. Proponents argue it speeds up initial development while still achieving good test coverage. Critics note that tests generated after the fact may miss edge cases that upfront thinking would catch. The debate continues as more teams experiment with this methodology.",
			hook: "Some developers are flipping the script on traditional testing, writing features first and generating tests afterward using modern tooling.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Technology", "Software Development"},
			restID: "2002885094042693899",
		},
		{
			name: "Language Learning App Users Share Struggles with Advanced Vocabulary",
			summary: "Users of a popular language learning platform are discussing the challenges of mastering advanced vocabulary. Many report feeling overwhelmed when transitioning from conversational basics to academic-level terms. The conversation highlights the gap between everyday language skills and the specialized vocabulary needed for professional or academic contexts. Some users share strategies for building vocabulary gradually, while others debate whether such advanced terms are necessary for most learners.",
			hook: "Language learners are opening up about the difficulty of moving beyond basic conversation to mastering complex academic vocabulary.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Education"},
			restID: "2000440656242581813",
		},
		{
			name: "New Type Checking Tool Promises Significant Performance Improvements",
			summary: "A recently released type checking utility claims to analyze codebases much faster than existing solutions. Early adopters report dramatic speed improvements when working with large projects. The tool offers advanced type system features and provides detailed error messages to help developers fix issues quickly. While still in early stages, the project has generated significant interest among developers working with statically typed languages.",
			hook: "Developers are excited about a new tool that promises to make type checking large codebases nearly instantaneous.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Technology", "Software Development"},
			restID: "2001090507334955221",
		},
		{
			name: "Database Project Maintains Extensive Testing Methodology",
			summary: "An open-source database project has gained recognition for its comprehensive testing approach. The project maintains a test suite that far exceeds the size of its core codebase, covering millions of scenarios and edge cases. Developers use multiple testing strategies including simulated failures, memory constraints, and automated fuzzing. This rigorous approach has earned the project a reputation for reliability and stability in production environments.",
			hook: "One database project demonstrates that extensive testing can lead to exceptional reliability and stability.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Technology", "Software Development"},
			restID: "2001572980581896396",
		},
		{
			name: "Reality Show Episode Features Unconventional Challenge Format",
			summary: "A recent episode of a popular reality competition show introduced a new challenge format that has viewers divided. The episode featured contestants participating in an unusual activity that some found entertaining while others criticized as inappropriate. Online discussions have been mixed, with some viewers praising the show's willingness to try new formats and others expressing discomfort with the content. The show's producers have not commented on the episode's reception.",
			hook: "A reality show's latest episode has sparked debate among viewers about the boundaries of entertainment content.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Movies & TV"},
			restID: "2002951373285957826",
		},
		{
			name: "Music Group Member Discusses Future Solo Projects",
			summary: "During a recent fan event, a member of a popular music group shared plans for upcoming solo work. The artist expressed excitement about exploring individual creative projects while maintaining commitment to group activities. Fans responded enthusiastically to the announcement, sharing support and anticipation for the new material. The discussion also touched on balancing personal projects with group commitments.",
			hook: "Fans are excited after a group member revealed plans for solo work during a recent fan interaction.",
			category: "Entertainment",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Music"},
			restID: "2001298644209643527",
		},
		{
			name: "University Rejects Student's Internship Report for Non-Compliance",
			summary: "A student's internship report was rejected by university faculty for not meeting program requirements. The student had completed training with an organization that the university determined did not align with the program's educational objectives. The department requires students to gain experience in specific professional settings, and this particular placement was deemed insufficient. The student will need to complete a new internship to fulfill graduation requirements.",
			hook: "A student's internship report was quickly dismissed by faculty for failing to meet program standards.",
			category: "News",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Education"},
			restID: "2001404258999316640",
		},
		{
			name: "Social Media Trend Features Creative Self-Expression in Public Spaces",
			summary: "A new social media trend has emerged where people share creative photos taken in everyday public locations. Participants use simple backgrounds like elevators or lobbies to showcase personal style and creativity. The trend emphasizes authenticity and has resonated with many users who appreciate the accessible, low-barrier approach to content creation. Some posts have gained significant engagement, demonstrating the appeal of relatable, everyday creativity.",
			hook: "People are finding creative ways to express themselves using ordinary public spaces as backdrops.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Social Media"},
			restID: "2003171984100737318",
		},
		{
			name: "Personality Assessment Tool Gains Popularity Through Social Sharing",
			summary: "A new personality assessment application has become popular as users share their results online. The tool uses a series of questions to assign users various characteristics and provides visual representations of results. Many users find the assessments entertaining and enjoy comparing results with friends. The trend has spread quickly as people post their outcomes and invite others to try the tool.",
			hook: "A personality quiz app is going viral as users share their colorful results across social platforms.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Social Media"},
			restID: "2002847397546799587",
		},
		{
			name: "Fan Community Creates Daily Celebration Challenge",
			summary: "Fans of a popular music group have organized a daily challenge where they celebrate different aspects of the group each day. Participants share memories, favorite moments, and creative content themed around daily prompts. The challenge has brought the community together and created a sense of shared celebration. Many participants appreciate the opportunity to reflect on what they enjoy about the group and connect with other fans.",
			hook: "A fan community has launched a daily challenge celebrating their favorite group, one letter at a time.",
			category: "Other",
			disclaimer: "This story is a summary of posts on X and may evolve over time. Grok can make mistakes, verify its outputs.",
			topics: []string{"Music"},
			restID: "2000687516429541862",
		},
	}
	
	baseTime := time.Now().AddDate(0, 0, -7) // 7 days ago
	
	for i, def := range newsDefs {
		// Create contexts with topics
		contexts := &NewsContexts{
			Topics: def.topics,
		}
		
		// Create news article with all fields set
		news := &News{
			ID:         def.restID,
			RestID:     def.restID,
			Name:       def.name,
			Summary:    def.summary,
			Hook:       def.hook,
			Category:   def.category,
			UpdatedAt:  baseTime.Add(time.Duration(i) * time.Hour),
			Disclaimer: def.disclaimer,
			Contexts:   contexts,
		}
		
		// Add to state directly
		s.state.mu.Lock()
		s.state.news[news.ID] = news
		s.state.mu.Unlock()
	}
}

// seedPersonalizedTrends seeds personalized trending topics
func (s *Seeder) seedPersonalizedTrends() {
	trendDefs := []struct {
		category      string
		postCount     string
		trendName     string
		trendingSince string
	}{
		{
			category:      "Technology",
			postCount:     "2.3K posts",
			trendName:     "New Programming Language Gains Traction Among Developers",
			trendingSince: "Trending now",
		},
		{
			category:      "Technology",
			postCount:     "1.8K posts",
			trendName:     "Open Source Framework Reaches Major Milestone",
			trendingSince: "1 hour ago",
		},
		{
			category:      "Entertainment",
			postCount:     "15K posts",
			trendName:     "Streaming Series Finale Sparks Fan Reactions",
			trendingSince: "4 hours ago",
		},
		{
			category:      "Technology",
			postCount:     "1.5K posts",
			trendName:     "Annual Developer Conference Announces Keynote Speakers",
			trendingSince: "2 hours ago",
		},
		{
			category:      "Technology",
			postCount:     "892 posts",
			trendName:     "New Code Editor Feature Draws Developer Interest",
			trendingSince: "Trending now",
		},
		{
			category:      "Technology",
			postCount:     "647 posts",
			trendName:     "Developers Share Tips for Modern Web Development",
			trendingSince: "Trending now",
		},
		{
			category:      "Technology",
			postCount:     "324 posts",
			trendName:     "Cryptographic Protocol Update Raises Security Questions",
			trendingSince: "Trending now",
		},
		{
			category:      "Sports",
			postCount:     "5.2K posts",
			trendName:     "Championship Finals Set New Viewership Records",
			trendingSince: "3 hours ago",
		},
		{
			category:      "News",
			postCount:     "8.7K posts",
			trendName:     "Industry Leaders Announce Major Partnership",
			trendingSince: "5 hours ago",
		},
		{
			category:      "Entertainment",
			postCount:     "3.1K posts",
			trendName:     "New Film Release Generates Buzz Online",
			trendingSince: "6 hours ago",
		},
	}

	trends := make([]*PersonalizedTrend, 0, len(trendDefs))
	for _, def := range trendDefs {
		trend := &PersonalizedTrend{
			Category:      def.category,
			PostCount:     def.postCount,
			TrendName:     def.trendName,
			TrendingSince: def.trendingSince,
		}
		trends = append(trends, trend)
	}

	s.state.mu.Lock()
	s.state.personalizedTrends = trends
	s.state.mu.Unlock()
}

// seedRelationships seeds relationships between entities
func (s *Seeder) seedRelationships() {
	relConfig := s.seedingConfig.GetRelationshipSeeding()
	if len(s.users) < 2 {
		return
	}

	// Follow relationships - create a more diverse network
	// Playground user follows several users (use actual seeded usernames)
	if s.playgroundUser != nil && len(s.users) > 1 {
		// Follow some of the default seeded users
		usersToFollow := []string{"DolphinTech", "CuriousCat", "EagerEagle", "BrilliantBear", "CreativeCobra"}
		for _, username := range usersToFollow {
			user := s.findUserByUsername(username)
			if user != nil {
				s.addFollow(s.playgroundUser, user)
			}
		}
		// Also follow a few random users if there are enough
		if len(s.users) > 6 {
			followCount := 3 + rand.Intn(3) // Follow 3-5 additional random users
			attempts := 0
			for len(s.playgroundUser.Following) < followCount && attempts < len(s.users)*2 {
				randomUser := s.users[rand.Intn(len(s.users))]
				if randomUser.ID != s.playgroundUser.ID && !contains(s.playgroundUser.Following, randomUser.ID) {
					s.addFollow(s.playgroundUser, randomUser)
				}
				attempts++
			}
		}
	}

	// Create a diverse follow network
	// Popular users (verified users) get more followers
	popularUsers := make([]*User, 0)
	for _, user := range s.users {
		if user.Verified {
			popularUsers = append(popularUsers, user)
		}
	}
	// If no verified users, pick first few users as popular
	if len(popularUsers) == 0 && len(s.users) > 0 {
		maxPopular := 3
		if len(s.users) < maxPopular {
			maxPopular = len(s.users)
		}
		popularUsers = s.users[:maxPopular]
	}
	for _, user := range popularUsers {
		// Popular users get 5-8 followers
		followerCount := 5 + rand.Intn(4)
		for i := 0; i < followerCount && i < len(s.users); i++ {
			followerIdx := rand.Intn(len(s.users))
			follower := s.users[followerIdx]
			if follower.ID != user.ID && !contains(follower.Following, user.ID) {
				s.addFollow(follower, user)
			}
		}
	}

	// Regular users follow each other more randomly
	for _, user := range s.users {
		// Each user follows configurable amount
		followingCount := relConfig.FollowsPerUserMin + rand.Intn(relConfig.FollowsPerUserMax-relConfig.FollowsPerUserMin+1)
		for i := 0; i < followingCount; i++ {
			targetIdx := rand.Intn(len(s.users))
			target := s.users[targetIdx]
			if target.ID != user.ID && !contains(user.Following, target.ID) {
				s.addFollow(user, target)
			}
		}
	}

	// Create some mutual follows (20% chance for any follow to be mutual)
	for _, user := range s.users {
		for _, followingID := range user.Following {
			if rand.Float32() < 0.2 { // 20% chance
				followingUser := s.state.GetUserByID(followingID)
				if followingUser != nil && !contains(followingUser.Following, user.ID) {
					s.addFollow(followingUser, user)
				}
			}
		}
	}

	// Like relationships - create more varied engagement
	// Some tweets become popular (viral), others get minimal engagement
	popularTweetIndices := make(map[int]bool)
	// Mark 10-15% of tweets as "popular"
	numPopularTweets := len(s.tweets) / 7 // ~14%
	if numPopularTweets < 5 {
		numPopularTweets = 5
	}
	for i := 0; i < numPopularTweets; i++ {
		popularTweetIndices[rand.Intn(len(s.tweets))] = true
	}

	for _, user := range s.users {
		// Each user likes configurable amount of tweets
		likeCount := relConfig.LikesPerPostMin + rand.Intn(relConfig.LikesPerPostMax-relConfig.LikesPerPostMin+1)
		likedTweets := make(map[string]bool) // Track to avoid duplicates
		for i := 0; i < likeCount && len(likedTweets) < len(s.tweets); i++ {
			tweetIdx := rand.Intn(len(s.tweets))
			tweet := s.tweets[tweetIdx]
			if tweet.AuthorID != user.ID && !likedTweets[tweet.ID] {
				s.addLike(user, tweet)
				likedTweets[tweet.ID] = true
				
				// Popular tweets get additional random likes
				if popularTweetIndices[tweetIdx] && rand.Float32() < 0.3 {
					// 30% chance of another user also liking this popular tweet
					extraLikerIdx := rand.Intn(len(s.users))
					extraLiker := s.users[extraLikerIdx]
					if extraLiker.ID != user.ID && extraLiker.ID != tweet.AuthorID && !contains(extraLiker.LikedTweets, tweet.ID) {
						s.addLike(extraLiker, tweet)
					}
				}
			}
		}
	}

	// Retweet relationships - more varied
	for _, user := range s.users {
		// Each user retweets 3-8 tweets (increased from 2-5)
		retweetCount := relConfig.RetweetsPerPostMin + rand.Intn(relConfig.RetweetsPerPostMax-relConfig.RetweetsPerPostMin+1)
		retweetedTweets := make(map[string]bool)
		for i := 0; i < retweetCount && len(retweetedTweets) < len(s.tweets); i++ {
			tweetIdx := rand.Intn(len(s.tweets))
			tweet := s.tweets[tweetIdx]
			if tweet.AuthorID != user.ID && !retweetedTweets[tweet.ID] {
				s.addRetweet(user, tweet)
				retweetedTweets[tweet.ID] = true
			}
		}
	}

	// Create more reply threads (5-8 threads)
	numThreads := 5 + rand.Intn(4)
	for i := 0; i < numThreads && i*3 < len(s.tweets); i++ {
		originalIdx := i * 3
		if originalIdx >= len(s.tweets) {
			break
		}
		original := s.tweets[originalIdx]
		
		// Add 1-3 replies to each thread
		numReplies := 1 + rand.Intn(3)
		for j := 0; j < numReplies && (originalIdx+j+1) < len(s.tweets); j++ {
			replyIdx := originalIdx + j + 1
			if replyIdx >= len(s.tweets) {
				break
			}
			reply := s.tweets[replyIdx]
			reply.InReplyToID = original.AuthorID
			reply.InReplyToTweetID = original.ID
			reply.ConversationID = original.ConversationID
			original.Replies = append(original.Replies, reply.ID)
			// Add referenced_tweet relationship
			if reply.ReferencedTweets == nil {
				reply.ReferencedTweets = make([]ReferencedTweet, 0)
			}
			reply.ReferencedTweets = append(reply.ReferencedTweets, ReferencedTweet{
				Type: "replied_to",
				ID:   original.ID,
			})
		}
	}

	// Add some quote tweets (5-10 quotes)
	numQuotes := 5 + rand.Intn(6)
	for i := 0; i < numQuotes && i < len(s.tweets); i++ {
		// Find a tweet to quote (not the first few)
		if i+10 >= len(s.tweets) {
			break
		}
		quotedIdx := rand.Intn(len(s.tweets)-10) + 10
		quoted := s.tweets[quotedIdx]
		
		// Find a later tweet to be the quote
		if quotedIdx+1 < len(s.tweets) {
			quoteIdx := quotedIdx + 1 + rand.Intn(min(5, len(s.tweets)-quotedIdx-1))
			if quoteIdx < len(s.tweets) {
				quote := s.tweets[quoteIdx]
				quoted.Quotes = append(quoted.Quotes, quote.ID)
				// Add referenced_tweet relationship
				if quote.ReferencedTweets == nil {
					quote.ReferencedTweets = make([]ReferencedTweet, 0)
				}
				quote.ReferencedTweets = append(quote.ReferencedTweets, ReferencedTweet{
					Type: "quoted",
					ID:   quoted.ID,
				})
			}
		}
	}

	// Bookmark relationships - users bookmark some tweets
	for _, user := range s.users {
		// Each user bookmarks 2-5 tweets
		bookmarkCount := 2 + rand.Intn(4)
		bookmarkedTweets := make(map[string]bool)
		for i := 0; i < bookmarkCount && len(bookmarkedTweets) < len(s.tweets); i++ {
			tweetIdx := rand.Intn(len(s.tweets))
			tweet := s.tweets[tweetIdx]
			if tweet.AuthorID != user.ID && !bookmarkedTweets[tweet.ID] {
				s.state.BookmarkTweet(user.ID, tweet.ID)
				bookmarkedTweets[tweet.ID] = true
			}
		}
	}

	// Mute relationships - some users mute others
	for _, user := range s.users {
		// 20-30% of users mute 1-3 other users
		if rand.Float32() < 0.25 {
			muteCount := 1 + rand.Intn(3)
			mutedUsers := make(map[string]bool)
			for i := 0; i < muteCount && len(mutedUsers) < len(s.users)-1; i++ {
				targetIdx := rand.Intn(len(s.users))
				target := s.users[targetIdx]
				if target.ID != user.ID && !mutedUsers[target.ID] {
					s.state.MuteUser(user.ID, target.ID)
					mutedUsers[target.ID] = true
				}
			}
		}
	}

	// Block relationships - some users block others
	for _, user := range s.users {
		// 10-15% of users block 1-2 other users
		if rand.Float32() < 0.12 {
			blockCount := 1 + rand.Intn(2)
			blockedUsers := make(map[string]bool)
			for i := 0; i < blockCount && len(blockedUsers) < len(s.users)-1; i++ {
				targetIdx := rand.Intn(len(s.users))
				target := s.users[targetIdx]
				if target.ID != user.ID && !blockedUsers[target.ID] {
					s.state.BlockUser(user.ID, target.ID)
					blockedUsers[target.ID] = true
				}
			}
		}
	}

	// Follow List relationships - users follow some lists
	if len(s.lists) > 0 {
		for _, user := range s.users {
			// Each user follows 1-3 lists (if lists exist)
			followListCount := 1 + rand.Intn(3)
			followedLists := make(map[string]bool)
			for i := 0; i < followListCount && len(followedLists) < len(s.lists); i++ {
				listIdx := rand.Intn(len(s.lists))
				list := s.lists[listIdx]
				if list.OwnerID != user.ID && !followedLists[list.ID] {
					s.state.FollowList(list.ID, user.ID)
					followedLists[list.ID] = true
				}
			}
		}
	}

	// Pin List relationships - some users pin lists they follow
	if len(s.lists) > 0 {
		for _, user := range s.users {
			// 30-40% of users pin 1-2 lists they follow
			if rand.Float32() < 0.35 && len(user.FollowedLists) > 0 {
				pinCount := 1 + rand.Intn(2)
				if pinCount > len(user.FollowedLists) {
					pinCount = len(user.FollowedLists)
				}
				pinnedLists := make(map[string]bool)
				for i := 0; i < pinCount; i++ {
					listID := user.FollowedLists[rand.Intn(len(user.FollowedLists))]
					if !pinnedLists[listID] {
						s.state.PinList(listID, user.ID)
						pinnedLists[listID] = true
					}
				}
			}
		}
	}
}

// Helper function to check if slice contains value
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// seedDMConversations seeds DM conversations and messages
func (s *Seeder) seedDMConversations() {
	if len(s.users) < 2 {
		return
	}

	// Get DM conversation seeding config
	dmMin, dmMax := s.seedingConfig.GetDMConversationsSeeding()
	numConversations := dmMin
	if dmMax > dmMin {
		numConversations = dmMin + rand.Intn(dmMax-dmMin+1)
	}
	conversationPairs := make(map[string]bool) // Track to avoid duplicates

	for i := 0; i < numConversations && i < len(s.users)*(len(s.users)-1)/2; i++ {
		// Pick two random users
		user1Idx := rand.Intn(len(s.users))
		user2Idx := rand.Intn(len(s.users))
		
		// Make sure they're different
		for user2Idx == user1Idx {
			user2Idx = rand.Intn(len(s.users))
		}
		
		user1 := s.users[user1Idx]
		user2 := s.users[user2Idx]
		
		// Create a unique key for this pair (sorted to avoid duplicates)
		key1 := user1.ID + "-" + user2.ID
		key2 := user2.ID + "-" + user1.ID
		if conversationPairs[key1] || conversationPairs[key2] {
			continue
		}
		conversationPairs[key1] = true
		
		// Create conversation
		conversation := s.state.CreateDMConversation([]string{user1.ID, user2.ID})
		
		// Create 4-10 messages in each conversation
		numMessages := 4 + rand.Intn(7)
		baseTime := time.Now().AddDate(0, 0, -rand.Intn(30)) // Random time in last 30 days
		
		// Create conversation templates for more realistic conversations
		conversationTemplates := [][]string{
			// Tech discussion
			{
				"Hey! I saw your post about the new API features. Really interesting stuff!",
				"Thanks! Yeah, I've been experimenting with it. Have you tried it out yet?",
				"Not yet, but I'm planning to this weekend. Any tips?",
				"Definitely start with the authentication flow - it's much smoother now.",
				"Awesome, I'll check that out first. Thanks for the heads up!",
				"No problem! Let me know how it goes.",
			},
			// Project collaboration
			{
				"Hi! Are you still working on that open source project?",
				"Yeah, we're making good progress. We just merged a big PR.",
				"That's great! I'd love to contribute if you need help.",
				"Absolutely! We have some good first issues if you want to check them out.",
				"Perfect, I'll take a look. Thanks!",
			},
			// Meeting up
			{
				"Hey! Are you going to the tech meetup next week?",
				"I'm planning to! Are you?",
				"Yeah, I'll be there. Want to grab coffee before?",
				"Sure, that sounds great! How about 6pm?",
				"Perfect, see you then!",
			},
			// Sharing resources
			{
				"Hey! I found this really useful article and thought you'd like it.",
				"Oh cool, what's it about?",
				"It's about API design patterns. I know you've been working on that.",
				"Thanks for thinking of me! I'll check it out tonight.",
				"No problem! Let me know what you think.",
			},
			// Casual check-in
			{
				"Hey! How's everything going?",
				"Pretty good! Just busy with work. How about you?",
				"Same here, but can't complain. Working on some exciting projects.",
				"That's awesome! Would love to hear about them sometime.",
				"For sure! Let's catch up soon.",
			},
			// Question/help
			{
				"Hi! Quick question - have you used the new endpoint yet?",
				"Yeah, I have! What do you need help with?",
				"I'm getting a 400 error and not sure why.",
				"Did you check the required fields? That's usually the issue.",
				"Ah, that might be it. Let me check. Thanks!",
				"No problem! Let me know if you need anything else.",
			},
		}
		
		// Pick a random conversation template
		template := conversationTemplates[rand.Intn(len(conversationTemplates))]
		
		// Use template messages, but limit to numMessages
		for j := 0; j < numMessages && j < len(template); j++ {
			// Alternate between users
			sender := user1
			if j%2 == 1 {
				sender = user2
			}
			
			messageText := template[j]
			
			// Create DM event
			dmEvent := &DMEvent{
				ID:               s.state.generateID(),
				Text:             messageText,
				SenderID:         sender.ID,
				CreatedAt:        baseTime.Add(time.Duration(j*15+rand.Intn(10)) * time.Minute), // Stagger messages with some randomness
				DMConversationID: conversation.ID,
				EventType:        "MessageCreate",
				ParticipantIDs:    []string{user1.ID, user2.ID},
			}
			
			// Add to state (need to use the state's method or direct access)
			s.state.mu.Lock()
			s.state.dmEvents[dmEvent.ID] = dmEvent
			s.state.mu.Unlock()
		}
		
		// If we need more messages than the template provides, add some generic ones
		if numMessages > len(template) {
			genericMessages := []string{
				"Sounds good!",
				"Thanks!",
				"Perfect!",
				"Awesome!",
				"Got it, thanks!",
				"Will do!",
			}
			
			for j := len(template); j < numMessages; j++ {
				sender := user1
				if j%2 == 1 {
					sender = user2
				}
				
				messageText := genericMessages[rand.Intn(len(genericMessages))]
				
				dmEvent := &DMEvent{
					ID:               s.state.generateID(),
					Text:             messageText,
					SenderID:         sender.ID,
					CreatedAt:        baseTime.Add(time.Duration(j*15+rand.Intn(10)) * time.Minute),
					DMConversationID: conversation.ID,
					EventType:        "MessageCreate",
					ParticipantIDs:    []string{user1.ID, user2.ID},
				}
				
				s.state.mu.Lock()
				s.state.dmEvents[dmEvent.ID] = dmEvent
				s.state.mu.Unlock()
			}
		}
	}
}

// updateMetrics updates all metrics based on relationships
func (s *Seeder) updateMetrics() {
	// Update user metrics
	for _, user := range s.users {
		user.PublicMetrics.TweetCount = len(user.Tweets)
		user.PublicMetrics.FollowingCount = len(user.Following)
		user.PublicMetrics.FollowersCount = len(user.Followers)
		user.PublicMetrics.ListedCount = len(user.ListMemberships)
		
		// Calculate like_count (total likes on user's tweets) and media_count
		likeCount := 0
		mediaCount := 0
		for _, tweetID := range user.Tweets {
			// Find tweet in slice by ID
			for _, tweet := range s.tweets {
				if tweet.ID == tweetID {
					likeCount += len(tweet.LikedBy)
					mediaCount += len(tweet.Media)
					break
				}
			}
		}
		user.PublicMetrics.LikeCount = likeCount
		user.PublicMetrics.MediaCount = mediaCount
	}

	// Update tweet metrics
	for _, tweet := range s.tweets {
		tweet.PublicMetrics.LikeCount = len(tweet.LikedBy)
		tweet.PublicMetrics.RetweetCount = len(tweet.RetweetedBy)
		tweet.PublicMetrics.ReplyCount = len(tweet.Replies)
		tweet.PublicMetrics.QuoteCount = len(tweet.Quotes)
	}

	// Update list metrics
	for _, list := range s.lists {
		list.MemberCount = len(list.Members)
		list.FollowerCount = len(list.Followers)
	}
}

// Helper methods

func (s *Seeder) findUserByUsername(username string) *User {
	return s.state.GetUserByUsername(username)
}

func (s *Seeder) addFollow(follower, followee *User) {
	// Add to follower's following list
	follower.Following = append(follower.Following, followee.ID)
	// Add to followee's followers list
	followee.Followers = append(followee.Followers, follower.ID)
}

func (s *Seeder) addLike(user *User, tweet *Tweet) {
	user.LikedTweets = append(user.LikedTweets, tweet.ID)
	tweet.LikedBy = append(tweet.LikedBy, user.ID)
}

func (s *Seeder) addRetweet(user *User, tweet *Tweet) {
	user.RetweetedTweets = append(user.RetweetedTweets, tweet.ID)
	tweet.RetweetedBy = append(tweet.RetweetedBy, user.ID)
	// Note: In real API, retweets create new tweet objects with referenced_tweets
	// For simplicity, we just track the relationship here
}

// createRetweetTweet creates an actual retweet tweet object (not just a relationship)
func (s *Seeder) createRetweetTweet(retweeter *User, originalTweet *Tweet) *Tweet {
	retweetID := s.state.generateID()
	retweet := &Tweet{
		ID:        retweetID,
		Text:      originalTweet.Text, // Retweets typically preserve the original text
		AuthorID:  retweeter.ID,
		CreatedAt: time.Now().Add(-time.Duration(rand.Intn(30)) * 24 * time.Hour), // Random time within last 30 days
		EditHistoryTweetIDs: []string{retweetID},
		ReferencedTweets: []ReferencedTweet{
			{
				Type: "retweeted",
				ID:   originalTweet.ID,
			},
		},
		PublicMetrics: TweetMetrics{
			LikeCount:    rand.Intn(50),
			RetweetCount: 0,
			ReplyCount:   0,
			QuoteCount:   0,
		},
		Source: generateSource(),
		Lang:   originalTweet.Lang,
		LikedBy:         make([]string, 0),
		RetweetedBy:     make([]string, 0),
		Replies:         make([]string, 0),
		Quotes:          make([]string, 0),
		Media:           make([]string, 0),
	}
	
	// Add to retweeter's tweets
	retweeter.Tweets = append(retweeter.Tweets, retweetID)
	
	// Track relationship
	s.addRetweet(retweeter, originalTweet)
	
	// Store the retweet tweet
	s.tweets = append(s.tweets, retweet)
	s.state.tweets[retweetID] = retweet
	
	return retweet
}

// seedPlaygroundUserRetweets creates retweet tweet objects for the playground user's posts
func (s *Seeder) seedPlaygroundUserRetweets() {
	if s.playgroundUser == nil || len(s.users) < 2 {
		return
	}
	
	// Get all tweets by the playground user
	playgroundTweets := s.state.GetTweets(s.playgroundUser.Tweets)
	if len(playgroundTweets) == 0 {
		return
	}
	
	// Create 3-8 retweets for each of the playground user's tweets
	for _, originalTweet := range playgroundTweets {
		retweetCount := 3 + rand.Intn(6) // 3-8 retweets per tweet
		
		for i := 0; i < retweetCount && i < len(s.users)-1; i++ {
			// Pick a random user (not the playground user)
			retweeterIdx := rand.Intn(len(s.users))
			retweeter := s.users[retweeterIdx]
			
			// Make sure we don't pick the playground user
			attempts := 0
			for retweeter.ID == s.playgroundUser.ID && attempts < len(s.users)*2 {
				retweeterIdx = rand.Intn(len(s.users))
				retweeter = s.users[retweeterIdx]
				attempts++
			}
			
			if retweeter.ID != s.playgroundUser.ID {
				// Check if this user already retweeted this tweet
				alreadyRetweeted := false
				for _, rtID := range retweeter.RetweetedTweets {
					if rtID == originalTweet.ID {
						alreadyRetweeted = true
						break
					}
				}
				
				if !alreadyRetweeted {
					s.createRetweetTweet(retweeter, originalTweet)
				}
			}
		}
	}
}

// getTweetTextForLanguage returns a tweet text in the specified language
func getTweetTextForLanguage(lang string, defaultTexts []string, index int) string {
	// Get language-specific texts
	langTexts := getMultilingualTweetTexts(lang)
	if len(langTexts) > 0 {
		return langTexts[index%len(langTexts)]
	}
	// Fallback to default texts if language not supported
	return defaultTexts[index%len(defaultTexts)]
}

// getMultilingualTweetTexts returns tweet texts in the specified language
func getMultilingualTweetTexts(lang string) []string {
	switch lang {
	case "es": // Spanish
		return []string{
			"¡Acabo de lanzar una nueva función! 🚀 Estoy emocionado de ver qué piensa la comunidad. #programación #desarrollo",
			"Leyendo sobre los últimos avances en #IA. El futuro es fascinante.",
			"Tuve una gran conversación sobre diseño de API hoy. REST vs GraphQL - ¿qué opinas? #desarrolloweb",
			"El código limpio es poesía para desarrolladores. Cada línea cuenta una historia.",
			"Construyendo algo increíble con código. La tecnología nunca deja de sorprenderme.",
			"Compartiendo pensamientos sobre software y vida. La programación es un arte.",
			"Explorando la intersección de tecnología y creatividad.",
			"Haciendo del mundo web un lugar mejor, un commit a la vez.",
			"Apasionado por el código abierto y la comunidad.",
			"Siempre aprendiendo, siempre construyendo.",
		}
	case "fr": // French
		return []string{
			"Je viens de publier une nouvelle fonctionnalité ! 🚀 J'ai hâte de voir ce que la communauté en pense. #codage #dev",
			"Lecture sur les derniers développements en #IA. L'avenir est fascinant.",
			"J'ai eu une excellente conversation sur la conception d'API aujourd'hui. REST vs GraphQL - qu'en pensez-vous ? #webdev",
			"Le code propre est de la poésie pour les développeurs. Chaque ligne raconte une histoire.",
			"Construire quelque chose d'incroyable avec du code. La technologie ne cesse de me surprendre.",
			"Partager des réflexions sur le logiciel et la vie. La programmation est un art.",
			"Explorer l'intersection de la technologie et de la créativité.",
			"Rendre le web meilleur, un commit à la fois.",
			"Passionné par l'open source et la communauté.",
			"Toujours apprendre, toujours construire.",
		}
	case "ja": // Japanese
		return []string{
			"新機能をリリースしました！🚀 コミュニティの反応が楽しみです。 #コーディング #開発",
			"#AIの最新動向について読んでいます。未来は魅力的です。",
			"今日はAPI設計について素晴らしい会話をしました。REST vs GraphQL - どう思いますか？ #ウェブ開発",
			"クリーンなコードは開発者にとって詩です。各行が物語を語ります。",
			"コードで素晴らしいものを構築しています。テクノロジーは常に驚きを与えてくれます。",
			"ソフトウェアと人生について考えを共有しています。プログラミングは芸術です。",
			"テクノロジーと創造性の交差点を探求しています。",
			"一度に1つのコミットで、ウェブをより良い場所にしています。",
			"オープンソースとコミュニティに情熱を注いでいます。",
			"常に学び、常に構築しています。",
		}
	case "de": // German
		return []string{
			"Ich habe gerade ein neues Feature veröffentlicht! 🚀 Ich bin gespannt, was die Community denkt. #Programmierung #Entwicklung",
			"Lese über die neuesten Entwicklungen in #KI. Die Zukunft ist faszinierend.",
			"Hatte heute ein großartiges Gespräch über API-Design. REST vs GraphQL - was denkt ihr? #Webentwicklung",
			"Sauberer Code ist Poesie für Entwickler. Jede Zeile erzählt eine Geschichte.",
			"Baue etwas Erstaunliches mit Code. Technologie hört nie auf zu überraschen.",
			"Teile Gedanken über Software und Leben. Programmierung ist eine Kunst.",
			"Erkunde die Schnittstelle zwischen Technologie und Kreativität.",
			"Mache das Web zu einem besseren Ort, einen Commit nach dem anderen.",
			"Leidenschaftlich für Open Source und Community.",
			"Immer lernen, immer bauen.",
		}
	case "pt": // Portuguese
		return []string{
			"Acabei de lançar um novo recurso! 🚀 Estou animado para ver o que a comunidade acha. #programação #desenvolvimento",
			"Lendo sobre os últimos desenvolvimentos em #IA. O futuro é fascinante.",
			"Tive uma ótima conversa sobre design de API hoje. REST vs GraphQL - o que você acha? #desenvolvimentoweb",
			"Código limpo é poesia para desenvolvedores. Cada linha conta uma história.",
			"Construindo algo incrível com código. A tecnologia nunca para de surpreender.",
			"Compartilhando pensamentos sobre software e vida. Programação é uma arte.",
			"Explorando a interseção de tecnologia e criatividade.",
			"Tornando a web um lugar melhor, um commit por vez.",
			"Paixão por código aberto e comunidade.",
			"Sempre aprendendo, sempre construindo.",
		}
	case "ko": // Korean
		return []string{
			"새로운 기능을 출시했습니다! 🚀 커뮤니티의 반응이 기대됩니다. #코딩 #개발",
			"#AI의 최신 동향을 읽고 있습니다. 미래는 매력적입니다.",
			"오늘 API 설계에 대해 좋은 대화를 나눴습니다. REST vs GraphQL - 어떻게 생각하시나요? #웹개발",
			"깨끗한 코드는 개발자에게 시입니다. 각 줄이 이야기를 전합니다.",
			"코드로 놀라운 것을 만들고 있습니다. 기술은 항상 놀라움을 줍니다.",
			"소프트웨어와 삶에 대한 생각을 공유합니다. 프로그래밍은 예술입니다.",
			"기술과 창의성의 교차점을 탐구합니다.",
			"한 번에 하나의 커밋으로 웹을 더 나은 곳으로 만들고 있습니다.",
			"오픈소스와 커뮤니티에 열정적입니다.",
			"항상 배우고, 항상 구축합니다.",
		}
	case "ar": // Arabic
		return []string{
			"لقد أطلقت للتو ميزة جديدة! 🚀 أنا متحمس لرؤية ما يعتقده المجتمع. #البرمجة #التطوير",
			"أقرأ عن أحدث التطورات في #الذكاء_الاصطناعي. المستقبل رائع.",
			"كان لدي محادثة رائعة حول تصميم API اليوم. REST مقابل GraphQL - ما رأيك؟ #تطوير_الويب",
			"الكود النظيف هو شعر للمطورين. كل سطر يروي قصة.",
			"أبني شيئًا مذهلاً بالكود. التكنولوجيا لا تتوقف عن الإدهاش.",
			"أشارك أفكارًا حول البرمجيات والحياة. البرمجة فن.",
			"أستكشف تقاطع التكنولوجيا والإبداع.",
			"أجعل الويب مكانًا أفضل، commit واحد في كل مرة.",
			"شغوف بالمصادر المفتوحة والمجتمع.",
			"أتعلم دائمًا، أبني دائمًا.",
		}
	case "hi": // Hindi
		return []string{
			"मैंने अभी एक नई सुविधा जारी की है! 🚀 मैं समुदाय की प्रतिक्रिया देखने के लिए उत्साहित हूं। #कोडिंग #विकास",
			"#AI में नवीनतम विकास के बारे में पढ़ रहा हूं। भविष्य आकर्षक है।",
			"आज API डिज़ाइन पर एक शानदार बातचीत हुई। REST बनाम GraphQL - आप क्या सोचते हैं? #वेब_विकास",
			"साफ कोड डेवलपर्स के लिए कविता है। हर पंक्ति एक कहानी कहती है।",
			"कोड के साथ कुछ अद्भुत बना रहा हूं। प्रौद्योगिकी कभी आश्चर्य करना बंद नहीं करती।",
			"सॉफ्टवेयर और जीवन के बारे में विचार साझा कर रहा हूं। प्रोग्रामिंग एक कला है।",
			"प्रौद्योगिकी और रचनात्मकता के चौराहे का अन्वेषण कर रहा हूं।",
			"एक समय में एक commit के साथ वेब को बेहतर जगह बना रहा हूं।",
			"ओपन सोर्स और समुदाय के लिए भावुक हूं।",
			"हमेशा सीख रहा हूं, हमेशा बना रहा हूं।",
		}
	case "zh": // Chinese
		return []string{
			"我刚刚发布了一个新功能！🚀 我很想看看社区的想法。 #编程 #开发",
			"正在阅读#AI的最新发展。未来令人着迷。",
			"今天进行了一次关于API设计的精彩对话。REST vs GraphQL - 你怎么看？ #Web开发",
			"干净的代码对开发者来说是诗歌。每一行都讲述一个故事。",
			"正在用代码构建令人惊叹的东西。技术永远不会停止令人惊讶。",
			"分享关于软件和生活的想法。编程是一门艺术。",
			"探索技术与创造力的交汇点。",
			"一次一个提交，让网络变得更好。",
			"对开源和社区充满热情。",
			"一直在学习，一直在构建。",
		}
	default: // English (fallback)
		return []string{
			"Just shipped a new feature! 🚀 Excited to see what the community thinks. #coding #dev",
			"Reading about the latest developments in #AI. The future is fascinating.",
			"Had a great conversation about API design today. REST vs GraphQL - what are your thoughts? #webdev",
			"Clean code is poetry for developers. Every line tells a story.",
			"Building something amazing with code. Technology never stops surprising.",
			"Sharing thoughts on software and life. Programming is an art.",
			"Exploring the intersection of technology and creativity.",
			"Making the web a better place, one commit at a time.",
			"Passionate about open source and community.",
			"Always learning, always building.",
		}
	}
}

// generateSource generates a realistic source string
func generateSource() string {
	sources := []string{
		"Twitter Web App",
		"Twitter for iPhone",
		"Twitter for Android",
		"Twitter for iPad",
		"TweetDeck",
		"Twitter API",
	}
	return sources[rand.Intn(len(sources))]
}

// generateEntities extracts and generates entities from tweet text
func generateEntities(text string, users []*User) *TweetEntities {
	entities := &TweetEntities{}
	
	// Extract hashtags
	hashtagMatches := hashtagRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range hashtagMatches {
		if len(match) >= 4 {
			entities.Hashtags = append(entities.Hashtags, EntityHashtag{
				Start: match[0],
				End:   match[1],
				Tag:   strings.TrimPrefix(text[match[0]:match[1]], "#"),
			})
		}
	}
	
	// Extract mentions
	mentionMatches := mentionRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range mentionMatches {
		if len(match) >= 4 {
			username := strings.TrimPrefix(text[match[0]:match[1]], "@")
			// Find user by username
			var userID string
			for _, user := range users {
				if user.Username == username {
					userID = user.ID
					break
				}
			}
			entities.Mentions = append(entities.Mentions, EntityMention{
				Start:    match[0],
				End:      match[1],
				Username: username,
				ID:       userID,
			})
		}
	}
	
	// Extract URLs (simple pattern)
	urlMatches := urlRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range urlMatches {
		if len(match) >= 2 {
			url := text[match[0]:match[1]]
			entities.URLs = append(entities.URLs, EntityURL{
				Start:       match[0],
				End:         match[1],
				URL:         url,
				ExpandedURL: url,
				DisplayURL:   url,
				Status:      200,
			})
		}
	}
	
	// Extract cashtags
	cashtagMatches := cashtagRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range cashtagMatches {
		if len(match) >= 4 {
			entities.Cashtags = append(entities.Cashtags, EntityCashtag{
				Start: match[0],
				End:   match[1],
				Tag:   strings.TrimPrefix(text[match[0]:match[1]], "$"),
			})
		}
	}
	
	// Only return if we have at least one entity type
	if len(entities.Hashtags) > 0 || len(entities.Mentions) > 0 || len(entities.URLs) > 0 || len(entities.Cashtags) > 0 {
		return entities
	}
	return nil
}

