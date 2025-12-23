// Package playground provides rule matching for search stream rules.
//
// This file implements rule matching for X API search stream rules, supporting
// all operators documented in the X API filtered stream documentation.
package playground

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// RuleMatcher matches tweets against search stream rules
type RuleMatcher struct {
	rules []*SearchStreamRule
}

// NewRuleMatcher creates a new rule matcher with the given rules
func NewRuleMatcher(rules []*SearchStreamRule) *RuleMatcher {
	return &RuleMatcher{
		rules: rules,
	}
}

// MatchTweet checks if a tweet matches any of the active rules
// Returns true if the tweet matches at least one rule, false otherwise
func (rm *RuleMatcher) MatchTweet(tweet *Tweet, state *State) bool {
	if len(rm.rules) == 0 {
		// If no rules, don't match anything (real API behavior)
		return false
	}

	// A tweet matches if it matches ANY rule (OR logic between rules)
	for _, rule := range rm.rules {
		if rm.MatchRule(tweet, rule.Value, state) {
			return true
		}
	}

	return false
}

// MatchRule checks if a tweet matches a single rule value
// Rules can contain boolean operators: OR, AND, NOT, parentheses
func (rm *RuleMatcher) MatchRule(tweet *Tweet, ruleValue string, state *State) bool {
	// Normalize rule value
	ruleValue = strings.TrimSpace(ruleValue)
	if ruleValue == "" {
		return false
	}

	// Handle parentheses for grouping
	if strings.Contains(ruleValue, "(") {
		return rm.matchComplexRule(tweet, ruleValue, state)
	}

	// Handle OR operator (takes precedence)
	if strings.Contains(strings.ToUpper(ruleValue), " OR ") {
		parts := splitOnOperator(ruleValue, " OR ")
		for _, part := range parts {
			if rm.MatchRule(tweet, strings.TrimSpace(part), state) {
				return true
			}
		}
		return false
	}

	// Handle AND operator (explicit " AND ")
	if strings.Contains(strings.ToUpper(ruleValue), " AND ") {
		parts := splitOnOperator(ruleValue, " AND ")
		for _, part := range parts {
			if !rm.MatchRule(tweet, strings.TrimSpace(part), state) {
				return false
			}
		}
		return true
	}

	// Handle implicit AND: multiple space-separated conditions (when not quoted)
	// Split on spaces, but respect quoted strings
	// Example: "has:hashtags #API" should be treated as "has:hashtags AND #API"
	parts := splitOnSpacesRespectingQuotes(ruleValue)
	if len(parts) > 1 {
		// Multiple parts found - treat as AND
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// Handle negation
			if strings.HasPrefix(part, "-") {
				if rm.MatchRule(tweet, part[1:], state) {
					return false // Negated condition matched, so AND fails
				}
				continue
			}
			if !rm.MatchRule(tweet, part, state) {
				return false
			}
		}
		return true
	}

	// Handle NOT operator (also supports - prefix)
	if strings.HasPrefix(strings.ToUpper(ruleValue), "NOT ") {
		notPart := strings.TrimPrefix(ruleValue, "NOT ")
		notPart = strings.TrimPrefix(notPart, "not ")
		return !rm.MatchRule(tweet, strings.TrimSpace(notPart), state)
	}

	// Handle negation prefix (-)
	if strings.HasPrefix(ruleValue, "-") {
		return !rm.MatchRule(tweet, ruleValue[1:], state)
	}

	// Match single condition
	return rm.matchCondition(tweet, ruleValue, state)
}

// matchComplexRule handles rules with parentheses
func (rm *RuleMatcher) matchComplexRule(tweet *Tweet, ruleValue string, state *State) bool {
	// Simple approach: evaluate innermost parentheses first
	// Find innermost parentheses
	re := regexp.MustCompile(`\(([^()]+)\)`)
	for {
		match := re.FindStringSubmatch(ruleValue)
		if match == nil {
			break
		}
		// Evaluate the inner expression
		innerResult := rm.MatchRule(tweet, match[1], state)
		// Replace with result
		replacement := "TRUE"
		if !innerResult {
			replacement = "FALSE"
		}
		ruleValue = strings.Replace(ruleValue, match[0], replacement, 1)
	}

	// Now evaluate the remaining expression, treating TRUE/FALSE as keywords
	ruleValue = strings.ReplaceAll(ruleValue, "TRUE", "true")
	ruleValue = strings.ReplaceAll(ruleValue, "FALSE", "false")

	// Handle boolean operators with TRUE/FALSE
	if strings.Contains(strings.ToUpper(ruleValue), " OR ") {
		parts := splitOnOperator(ruleValue, " OR ")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "true" || part == "TRUE" {
				return true
			}
			if part == "false" || part == "FALSE" {
				continue
			}
			if rm.MatchRule(tweet, part, state) {
				return true
			}
		}
		return false
	}

	if strings.Contains(strings.ToUpper(ruleValue), " AND ") {
		parts := splitOnOperator(ruleValue, " AND ")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "false" || part == "FALSE" {
				return false
			}
			if part != "true" && part != "TRUE" {
				if !rm.MatchRule(tweet, part, state) {
					return false
				}
			}
		}
		return true
	}

	// Single value
	if ruleValue == "true" || ruleValue == "TRUE" {
		return true
	}
	if ruleValue == "false" || ruleValue == "FALSE" {
		return false
	}
	return rm.MatchRule(tweet, ruleValue, state)
}

// tokenizeText tokenizes text by splitting on punctuation, symbols, and Unicode separators
// This matches X API behavior where keywords are tokenized
func tokenizeText(text string) []string {
	// Split on Unicode separators, punctuation, and symbols
	var tokens []string
	var current strings.Builder
	
	for _, r := range text {
		// Check if rune is a separator, punctuation, or symbol
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if current.Len() > 0 {
				tokens = append(tokens, strings.ToLower(current.String()))
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	
	if current.Len() > 0 {
		tokens = append(tokens, strings.ToLower(current.String()))
	}
	
	return tokens
}

// matchCondition matches a single condition (no boolean operators)
func (rm *RuleMatcher) matchCondition(tweet *Tweet, condition string, state *State) bool {
	condition = strings.TrimSpace(condition)

	// Handle proximity operator (~N) - must check before other operators
	if strings.Contains(condition, "~") {
		return rm.matchProximityOperator(tweet, condition)
	}

	// Handle operators with colons (must check before standalone operators)
	if strings.Contains(condition, ":") {
		if strings.HasPrefix(condition, "has:") {
			return rm.matchHasOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "lang:") {
			return rm.matchLangOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "from:") {
			return rm.matchFromOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "to:") {
			return rm.matchToOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "url:") {
			return rm.matchURLOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "retweets_of:") || strings.HasPrefix(condition, "retweets_of_user:") {
			return rm.matchRetweetsOfOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "context:") {
			return rm.matchContextOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "entity:") {
			return rm.matchEntityOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "conversation_id:") {
			return rm.matchConversationIDOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "bio:") || strings.HasPrefix(condition, "user_bio:") {
			return rm.matchBioOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "bio_name:") {
			return rm.matchBioNameOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "bio_location:") || strings.HasPrefix(condition, "user_bio_location:") {
			return rm.matchBioLocationOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "place:") {
			return rm.matchPlaceOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "place_country:") {
			return rm.matchPlaceCountryOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "point_radius:") {
			return rm.matchPointRadiusOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "bounding_box:") || strings.HasPrefix(condition, "geo_bounding_box:") {
			return rm.matchBoundingBoxOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "is:") {
			return rm.matchIsOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "sample:") {
			// Sample operator is handled at rule level, not tweet level
			// For now, always match (sampling would be applied upstream)
			return true
		}
		if strings.HasPrefix(condition, "followers_count:") {
			return rm.matchFollowersCountOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "tweets_count:") || strings.HasPrefix(condition, "statuses_count:") {
			return rm.matchTweetsCountOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "following_count:") || strings.HasPrefix(condition, "friends_count:") {
			return rm.matchFollowingCountOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "listed_count:") || strings.HasPrefix(condition, "user_in_lists_count:") {
			return rm.matchListedCountOperator(tweet, condition, state)
		}
		if strings.HasPrefix(condition, "url_title:") || strings.HasPrefix(condition, "within_url_title:") {
			return rm.matchURLTitleOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "url_description:") || strings.HasPrefix(condition, "within_url_description:") {
			return rm.matchURLDescriptionOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "url_contains:") {
			return rm.matchURLContainsOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "source:") {
			return rm.matchSourceOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "in_reply_to_tweet_id:") || strings.HasPrefix(condition, "in_reply_to_status_id:") {
			return rm.matchInReplyToTweetIDOperator(tweet, condition)
		}
		if strings.HasPrefix(condition, "retweets_of_tweet_id:") || strings.HasPrefix(condition, "retweets_of_status_id:") {
			return rm.matchRetweetsOfTweetIDOperator(tweet, condition, state)
		}
	}

	// Handle standalone operators
	if strings.HasPrefix(condition, "@") {
		return rm.matchMention(tweet, condition, state)
	}

	if strings.HasPrefix(condition, "#") {
		return rm.matchHashtag(tweet, condition)
	}

	if strings.HasPrefix(condition, "$") {
		return rm.matchCashtag(tweet, condition)
	}

	// Handle quoted exact phrase
	if strings.HasPrefix(condition, `"`) && strings.HasSuffix(condition, `"`) {
		phrase := strings.Trim(condition, `"`)
		return strings.Contains(strings.ToLower(tweet.Text), strings.ToLower(phrase))
	}

	// Check for emoji (Unicode emoji characters)
	if containsEmoji(condition) {
		return rm.matchEmoji(tweet, condition)
	}

	// Default: tokenized keyword matching
	return rm.matchTokenizedKeyword(tweet, condition)
}

// matchTokenizedKeyword performs tokenized keyword matching
// Keywords are matched against tokenized text (split on punctuation/symbols)
func (rm *RuleMatcher) matchTokenizedKeyword(tweet *Tweet, keyword string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	tokens := tokenizeText(tweet.Text)
	
	// Check if any token matches the keyword
	for _, token := range tokens {
		if token == keyword {
			return true
		}
	}
	
	return false
}

// containsEmoji checks if a string contains emoji characters
func containsEmoji(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.So) || (r >= 0x1F300 && r <= 0x1F9FF) || (r >= 0x2600 && r <= 0x26FF) || (r >= 0x2700 && r <= 0x27BF) {
			return true
		}
	}
	return false
}

// matchEmoji matches emoji in tweet text (tokenized match)
func (rm *RuleMatcher) matchEmoji(tweet *Tweet, emojiCondition string) bool {
	// Extract emoji from condition (may have variants, need to handle quotes)
	emoji := strings.TrimSpace(emojiCondition)
	if strings.HasPrefix(emoji, `"`) && strings.HasSuffix(emoji, `"`) {
		emoji = strings.Trim(emoji, `"`)
	}
	
	// Tokenize tweet text and check for emoji
	tokens := tokenizeText(tweet.Text)
	for _, token := range tokens {
		// Check if token contains the emoji
		if strings.Contains(token, emoji) || strings.Contains(emoji, token) {
			return true
		}
	}
	
	// Also check raw text for emoji
	return strings.Contains(tweet.Text, emoji)
}

// matchProximityOperator matches proximity operator: "keyword1 keyword2"~N
func (rm *RuleMatcher) matchProximityOperator(tweet *Tweet, condition string) bool {
	// Parse: "keyword1 keyword2"~N
	// Find the ~N part
	tildeIndex := strings.LastIndex(condition, "~")
	if tildeIndex == -1 {
		return false
	}
	
	phrasePart := strings.TrimSpace(condition[:tildeIndex])
	distancePart := strings.TrimSpace(condition[tildeIndex+1:])
	
	// Remove quotes if present
	if strings.HasPrefix(phrasePart, `"`) && strings.HasSuffix(phrasePart, `"`) {
		phrasePart = strings.Trim(phrasePart, `"`)
	}
	
	// Parse distance N (must be <= 6)
	distance, err := strconv.Atoi(distancePart)
	if err != nil || distance < 1 || distance > 6 {
		return false
	}
	
	// Split phrase into keywords
	keywords := strings.Fields(strings.ToLower(phrasePart))
	if len(keywords) < 2 {
		return false
	}
	
	// Tokenize tweet text
	tokens := tokenizeText(tweet.Text)
	
	// Find positions of keywords in tokens
	keywordPositions := make(map[string][]int)
	for i, token := range tokens {
		for _, keyword := range keywords {
			if token == keyword {
				keywordPositions[keyword] = append(keywordPositions[keyword], i)
			}
		}
	}
	
	// Check if all keywords are found
	if len(keywordPositions) < len(keywords) {
		return false
	}
	
	// Check if keywords are within N tokens of each other
	// Try all combinations of positions
	for _, pos1 := range keywordPositions[keywords[0]] {
		for _, pos2 := range keywordPositions[keywords[1]] {
			dist := pos1 - pos2
			if dist < 0 {
				dist = -dist
			}
			// For reverse order, distance can be N-2
			if dist <= distance || (dist <= distance-2 && pos2 < pos1) {
				return true
			}
		}
	}
	
	return false
}

// matchHasOperator matches has: operators
func (rm *RuleMatcher) matchHasOperator(tweet *Tweet, condition string) bool {
	condition = strings.ToLower(condition)

	switch condition {
	case "has:hashtags":
		return tweet.Entities != nil && len(tweet.Entities.Hashtags) > 0
	case "has:links", "has:urls":
		return tweet.Entities != nil && len(tweet.Entities.URLs) > 0
	case "has:mentions":
		return tweet.Entities != nil && len(tweet.Entities.Mentions) > 0
	case "has:media", "has:media_link":
		return (tweet.Attachments != nil && len(tweet.Attachments.MediaKeys) > 0) || len(tweet.Media) > 0
	case "has:images":
		// Check if tweet has image media
		if tweet.Attachments != nil && len(tweet.Attachments.MediaKeys) > 0 {
			// In a real implementation, we'd check media types
			return true
		}
		return false
	case "has:videos", "has:video_link":
		// Check if tweet has video media
		// Would need to check media types in real implementation
		return false
	case "has:cashtags":
		return tweet.Entities != nil && len(tweet.Entities.Cashtags) > 0
	case "has:geo":
		// Check if tweet has geo location (place or coordinates)
		// This would require PlaceID or geo coordinates in tweet
		return false
	}

	return false
}

// matchIsOperator matches is: operators
func (rm *RuleMatcher) matchIsOperator(tweet *Tweet, condition string, state *State) bool {
	condition = strings.ToLower(condition)
	
	switch condition {
	case "is:retweet":
		// Check if tweet is a retweet (has referenced_tweets with type "retweeted")
		if tweet.ReferencedTweets != nil {
			for _, ref := range tweet.ReferencedTweets {
				if ref.Type == "retweeted" {
					return true
				}
			}
		}
		return false
	case "is:reply":
		// Check if tweet is a reply
		return tweet.InReplyToTweetID != "" || tweet.InReplyToID != ""
	case "is:quote":
		// Check if tweet is a quote tweet
		if tweet.ReferencedTweets != nil {
			for _, ref := range tweet.ReferencedTweets {
				if ref.Type == "quoted" {
					return true
				}
			}
		}
		return false
	case "is:verified":
		// Check if author is verified
		if state != nil {
			author := state.GetUserByID(tweet.AuthorID)
			if author != nil {
				return author.Verified
			}
		}
		return false
	case "is:nullcast":
		// Check if tweet source indicates nullcast
		return strings.Contains(strings.ToLower(tweet.Source), "advertisers")
	}
	
	return false
}

// matchLangOperator matches lang: operator
func (rm *RuleMatcher) matchLangOperator(tweet *Tweet, condition string) bool {
	lang := strings.TrimPrefix(condition, "lang:")
	lang = strings.TrimPrefix(lang, "LANG:")
	lang = strings.ToLower(strings.TrimSpace(lang))
	return strings.ToLower(tweet.Lang) == lang
}

// matchFromOperator matches from: operator
func (rm *RuleMatcher) matchFromOperator(tweet *Tweet, condition string, state *State) bool {
	username := strings.TrimPrefix(condition, "from:")
	username = strings.TrimPrefix(username, "FROM:")
	username = strings.TrimSpace(username)

	// Get author user
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}

	// Can match username or user ID
	return strings.EqualFold(author.Username, username) || author.ID == username
}

// matchToOperator matches to: operator (reply to user)
func (rm *RuleMatcher) matchToOperator(tweet *Tweet, condition string, state *State) bool {
	username := strings.TrimPrefix(condition, "to:")
	username = strings.TrimPrefix(username, "TO:")
	username = strings.TrimSpace(username)

	// Check if tweet is in reply to this user
	// Can match by username or user ID
	if tweet.InReplyToID != "" {
		user := state.GetUserByID(tweet.InReplyToID)
		if user != nil {
			return strings.EqualFold(user.Username, username) || user.ID == username
		}
		// Also check if InReplyToID directly matches username (if username is a user ID)
		if tweet.InReplyToID == username {
			return true
		}
	}

	return false
}

// matchURLOperator matches url: operator (tokenized match on URLs)
func (rm *RuleMatcher) matchURLOperator(tweet *Tweet, condition string) bool {
	urlPattern := strings.TrimPrefix(condition, "url:")
	urlPattern = strings.TrimPrefix(urlPattern, "URL:")
	urlPattern = strings.TrimSpace(urlPattern)
	
	// Remove quotes if present
	if strings.HasPrefix(urlPattern, `"`) && strings.HasSuffix(urlPattern, `"`) {
		urlPattern = strings.Trim(urlPattern, `"`)
	}
	
	urlPattern = strings.ToLower(urlPattern)
	
	if tweet.Entities != nil && tweet.Entities.URLs != nil {
		for _, urlEntity := range tweet.Entities.URLs {
			// Tokenize both URL and expanded URL
			urlTokens := tokenizeText(urlEntity.URL)
			expandedTokens := tokenizeText(urlEntity.ExpandedURL)
			
			// Check if pattern tokens match URL tokens
			patternTokens := tokenizeText(urlPattern)
			for _, patternToken := range patternTokens {
				for _, token := range urlTokens {
					if token == patternToken {
						return true
					}
				}
				for _, token := range expandedTokens {
					if token == patternToken {
						return true
					}
				}
			}
		}
	}
	
	return false
}

// matchRetweetsOfOperator matches retweets_of: operator
func (rm *RuleMatcher) matchRetweetsOfOperator(tweet *Tweet, condition string, state *State) bool {
	username := strings.TrimPrefix(condition, "retweets_of:")
	username = strings.TrimPrefix(username, "retweets_of_user:")
	username = strings.TrimSpace(username)
	
	// Check if tweet is a retweet
	if tweet.ReferencedTweets != nil {
		for _, ref := range tweet.ReferencedTweets {
			if ref.Type == "retweeted" {
				// Get the original tweet
				originalTweet := state.GetTweet(ref.ID)
				if originalTweet != nil {
					// Get the original tweet's author
					author := state.GetUserByID(originalTweet.AuthorID)
					if author != nil {
						return strings.EqualFold(author.Username, username) || author.ID == username
					}
				}
			}
		}
	}
	
	return false
}

// matchContextOperator matches context: operator (domain.entity)
func (rm *RuleMatcher) matchContextOperator(tweet *Tweet, condition string) bool {
	// context:domain_id.entity_id or context:domain_id.* or context:*.entity_id
	// For now, return false as we don't track context annotations
	return false
}

// matchEntityOperator matches entity: operator
func (rm *RuleMatcher) matchEntityOperator(tweet *Tweet, condition string) bool {
	// entity:"string declaration of entity/place"
	// For now, return false as we don't track entity annotations
	return false
}

// matchConversationIDOperator matches conversation_id: operator
func (rm *RuleMatcher) matchConversationIDOperator(tweet *Tweet, condition string) bool {
	convID := strings.TrimPrefix(condition, "conversation_id:")
	convID = strings.TrimSpace(convID)
	return tweet.ConversationID == convID
}

// matchBioOperator matches bio: operator (user bio keyword)
func (rm *RuleMatcher) matchBioOperator(tweet *Tweet, condition string, state *State) bool {
	keyword := strings.TrimPrefix(condition, "bio:")
	keyword = strings.TrimPrefix(keyword, "user_bio:")
	keyword = strings.TrimSpace(keyword)
	
	// Remove quotes if present
	if strings.HasPrefix(keyword, `"`) && strings.HasSuffix(keyword, `"`) {
		keyword = strings.Trim(keyword, `"`)
	}
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	// Tokenized match on user bio
	bioTokens := tokenizeText(author.Description)
	keywordLower := strings.ToLower(keyword)
	for _, token := range bioTokens {
		if token == keywordLower {
			return true
		}
	}
	
	return false
}

// matchBioNameOperator matches bio_name: operator
func (rm *RuleMatcher) matchBioNameOperator(tweet *Tweet, condition string, state *State) bool {
	keyword := strings.TrimPrefix(condition, "bio_name:")
	keyword = strings.TrimSpace(keyword)
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	// Tokenized match on user name
	nameTokens := tokenizeText(author.Name)
	keywordLower := strings.ToLower(keyword)
	for _, token := range nameTokens {
		if token == keywordLower {
			return true
		}
	}
	
	return false
}

// matchBioLocationOperator matches bio_location: operator
func (rm *RuleMatcher) matchBioLocationOperator(tweet *Tweet, condition string, state *State) bool {
	keyword := strings.TrimPrefix(condition, "bio_location:")
	keyword = strings.TrimPrefix(keyword, "user_bio_location:")
	keyword = strings.TrimSpace(keyword)
	
	// Remove quotes if present
	if strings.HasPrefix(keyword, `"`) && strings.HasSuffix(keyword, `"`) {
		keyword = strings.Trim(keyword, `"`)
	}
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	// Tokenized match on user location
	locationTokens := tokenizeText(author.Location)
	keywordLower := strings.ToLower(keyword)
	for _, token := range locationTokens {
		if token == keywordLower {
			return true
		}
	}
	
	return false
}

// matchPlaceOperator matches place: operator
func (rm *RuleMatcher) matchPlaceOperator(tweet *Tweet, condition string) bool {
	// place:"new york city" or place:place_id
	// For now, return false as we don't track place data on tweets
	return false
}

// matchPlaceCountryOperator matches place_country: operator
func (rm *RuleMatcher) matchPlaceCountryOperator(tweet *Tweet, condition string) bool {
	// place_country:US (ISO alpha-2 code)
	// For now, return false as we don't track place data
	return false
}

// matchPointRadiusOperator matches point_radius: operator
func (rm *RuleMatcher) matchPointRadiusOperator(tweet *Tweet, condition string) bool {
	// point_radius:[longitude latitude radius]
	// For now, return false as we don't track geo coordinates
	return false
}

// matchBoundingBoxOperator matches bounding_box: operator
func (rm *RuleMatcher) matchBoundingBoxOperator(tweet *Tweet, condition string) bool {
	// bounding_box:[west_long south_lat east_long north_lat]
	// For now, return false as we don't track geo coordinates
	return false
}

// matchFollowersCountOperator matches followers_count: operator
func (rm *RuleMatcher) matchFollowersCountOperator(tweet *Tweet, condition string, state *State) bool {
	rangeStr := strings.TrimPrefix(condition, "followers_count:")
	rangeStr = strings.TrimSpace(rangeStr)
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	return matchRange(rangeStr, author.PublicMetrics.FollowersCount)
}

// matchTweetsCountOperator matches tweets_count: operator
func (rm *RuleMatcher) matchTweetsCountOperator(tweet *Tweet, condition string, state *State) bool {
	rangeStr := strings.TrimPrefix(condition, "tweets_count:")
	rangeStr = strings.TrimPrefix(rangeStr, "statuses_count:")
	rangeStr = strings.TrimSpace(rangeStr)
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	return matchRange(rangeStr, author.PublicMetrics.TweetCount)
}

// matchFollowingCountOperator matches following_count: operator
func (rm *RuleMatcher) matchFollowingCountOperator(tweet *Tweet, condition string, state *State) bool {
	rangeStr := strings.TrimPrefix(condition, "following_count:")
	rangeStr = strings.TrimPrefix(rangeStr, "friends_count:")
	rangeStr = strings.TrimSpace(rangeStr)
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	return matchRange(rangeStr, author.PublicMetrics.FollowingCount)
}

// matchListedCountOperator matches listed_count: operator
func (rm *RuleMatcher) matchListedCountOperator(tweet *Tweet, condition string, state *State) bool {
	rangeStr := strings.TrimPrefix(condition, "listed_count:")
	rangeStr = strings.TrimPrefix(rangeStr, "user_in_lists_count:")
	rangeStr = strings.TrimSpace(rangeStr)
	
	author := state.GetUserByID(tweet.AuthorID)
	if author == nil {
		return false
	}
	
	return matchRange(rangeStr, author.PublicMetrics.ListedCount)
}

// matchRange matches a range specification (e.g., "1000" or "1000..10000")
func matchRange(rangeStr string, value int) bool {
	if strings.Contains(rangeStr, "..") {
		parts := strings.Split(rangeStr, "..")
		if len(parts) != 2 {
			return false
		}
		min, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		max, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return false
		}
		return value >= min && value <= max
	}
	
	// Single number: >= that number
	min, err := strconv.Atoi(strings.TrimSpace(rangeStr))
	if err != nil {
		return false
	}
	return value >= min
}

// matchURLTitleOperator matches url_title: operator
func (rm *RuleMatcher) matchURLTitleOperator(tweet *Tweet, condition string) bool {
	keyword := strings.TrimPrefix(condition, "url_title:")
	keyword = strings.TrimPrefix(keyword, "within_url_title:")
	keyword = strings.TrimSpace(keyword)
	
	// Remove quotes if present
	if strings.HasPrefix(keyword, `"`) && strings.HasSuffix(keyword, `"`) {
		keyword = strings.Trim(keyword, `"`)
	}
	
	// For now, return false as we don't track URL metadata
	return false
}

// matchURLDescriptionOperator matches url_description: operator
func (rm *RuleMatcher) matchURLDescriptionOperator(tweet *Tweet, condition string) bool {
	keyword := strings.TrimPrefix(condition, "url_description:")
	keyword = strings.TrimPrefix(keyword, "within_url_description:")
	keyword = strings.TrimSpace(keyword)
	
	// Remove quotes if present
	if strings.HasPrefix(keyword, `"`) && strings.HasSuffix(keyword, `"`) {
		keyword = strings.Trim(keyword, `"`)
	}
	
	// For now, return false as we don't track URL metadata
	return false
}

// matchURLContainsOperator matches url_contains: operator
func (rm *RuleMatcher) matchURLContainsOperator(tweet *Tweet, condition string) bool {
	phrase := strings.TrimPrefix(condition, "url_contains:")
	phrase = strings.TrimSpace(phrase)
	
	// Remove quotes if present
	if strings.HasPrefix(phrase, `"`) && strings.HasSuffix(phrase, `"`) {
		phrase = strings.Trim(phrase, `"`)
	}
	
	phraseLower := strings.ToLower(phrase)
	
	if tweet.Entities != nil && tweet.Entities.URLs != nil {
		for _, urlEntity := range tweet.Entities.URLs {
			if strings.Contains(strings.ToLower(urlEntity.URL), phraseLower) ||
				strings.Contains(strings.ToLower(urlEntity.ExpandedURL), phraseLower) {
				return true
			}
		}
	}
	
	return false
}

// matchSourceOperator matches source: operator
func (rm *RuleMatcher) matchSourceOperator(tweet *Tweet, condition string) bool {
	sourcePattern := strings.TrimPrefix(condition, "source:")
	sourcePattern = strings.TrimSpace(sourcePattern)
	
	// Remove quotes if present
	if strings.HasPrefix(sourcePattern, `"`) && strings.HasSuffix(sourcePattern, `"`) {
		sourcePattern = strings.Trim(sourcePattern, `"`)
	}
	
	return strings.Contains(strings.ToLower(tweet.Source), strings.ToLower(sourcePattern))
}

// matchInReplyToTweetIDOperator matches in_reply_to_tweet_id: operator
func (rm *RuleMatcher) matchInReplyToTweetIDOperator(tweet *Tweet, condition string) bool {
	tweetID := strings.TrimPrefix(condition, "in_reply_to_tweet_id:")
	tweetID = strings.TrimPrefix(tweetID, "in_reply_to_status_id:")
	tweetID = strings.TrimSpace(tweetID)
	return tweet.InReplyToTweetID == tweetID
}

// matchRetweetsOfTweetIDOperator matches retweets_of_tweet_id: operator
func (rm *RuleMatcher) matchRetweetsOfTweetIDOperator(tweet *Tweet, condition string, state *State) bool {
	tweetID := strings.TrimPrefix(condition, "retweets_of_tweet_id:")
	tweetID = strings.TrimPrefix(tweetID, "retweets_of_status_id:")
	tweetID = strings.TrimSpace(tweetID)
	
	// Check if tweet is a retweet of the specified tweet ID
	if tweet.ReferencedTweets != nil {
		for _, ref := range tweet.ReferencedTweets {
			if ref.Type == "retweeted" && ref.ID == tweetID {
				return true
			}
		}
	}
	
	return false
}

// matchMention matches @username mentions
func (rm *RuleMatcher) matchMention(tweet *Tweet, condition string, state *State) bool {
	username := strings.TrimPrefix(condition, "@")
	username = strings.TrimSpace(username)

	if tweet.Entities != nil {
		for _, mention := range tweet.Entities.Mentions {
			if strings.EqualFold(mention.Username, username) {
				return true
			}
		}
	}

	return false
}

// matchHashtag matches #hashtag (exact match, not tokenized)
func (rm *RuleMatcher) matchHashtag(tweet *Tweet, condition string) bool {
	hashtag := strings.TrimPrefix(condition, "#")
	hashtag = strings.ToLower(strings.TrimSpace(hashtag))

	if tweet.Entities != nil {
		for _, tag := range tweet.Entities.Hashtags {
			if strings.EqualFold(tag.Tag, hashtag) {
				return true
			}
		}
	}

	return false
}

// matchCashtag matches $cashtag
func (rm *RuleMatcher) matchCashtag(tweet *Tweet, condition string) bool {
	cashtag := strings.TrimPrefix(condition, "$")
	cashtag = strings.ToUpper(strings.TrimSpace(cashtag))

	if tweet.Entities != nil {
		for _, tag := range tweet.Entities.Cashtags {
			if strings.EqualFold(tag.Tag, cashtag) {
				return true
			}
		}
	}

	return false
}

// splitOnSpacesRespectingQuotes splits a string on spaces, but respects quoted strings
func splitOnSpacesRespectingQuotes(s string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	
	for i := 0; i < len(s); i++ {
		char := s[i]
		
		if char == '"' {
			inQuotes = !inQuotes
			current.WriteByte(char)
			continue
		}
		
		if !inQuotes && char == ' ' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}
		
		current.WriteByte(char)
	}
	
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	
	return parts
}

// splitOnOperator splits a string on an operator, handling quoted strings
func splitOnOperator(s, operator string) []string {
	operatorUpper := strings.ToUpper(operator)
	sUpper := strings.ToUpper(s)

	var parts []string
	var current strings.Builder
	inQuotes := false
	i := 0

	for i < len(s) {
		char := s[i]

		if char == '"' {
			inQuotes = !inQuotes
			current.WriteByte(char)
			i++
			continue
		}

		if !inQuotes && i+len(operator) <= len(s) {
			substr := sUpper[i : i+len(operator)]
			if substr == operatorUpper {
				// Check if it's actually the operator (surrounded by spaces or at boundaries)
				beforeOK := i == 0 || s[i-1] == ' '
				afterOK := i+len(operator) >= len(s) || s[i+len(operator)] == ' '

				if beforeOK && afterOK {
					parts = append(parts, current.String())
					current.Reset()
					i += len(operator)
					continue
				}
			}
		}

		current.WriteByte(char)
		i++
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}
