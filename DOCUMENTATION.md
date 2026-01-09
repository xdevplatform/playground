# X API Playground - Complete User Manual

**Version:** Latest  
**Last Updated:** January 2025  
**Repository:** https://github.com/xdevplatform/playground


## Table of Contents

1. [Introduction](#introduction)
2. [Installation](#installation)
3. [Quick Start](#quick-start)
4. [CLI Commands](#cli-commands)
5. [Configuration](#configuration)
6. [API Endpoints](#api-endpoints)
7. [State Management](#state-management)
8. [Authentication](#authentication)
9. [Rate Limiting](#rate-limiting)
10. [Query Parameters & Field Selection](#query-parameters--field-selection)
11. [Expansions](#expansions)
12. [Request Validation](#request-validation)
13. [Error Handling](#error-handling)
14. [Examples and Use Cases](#examples-and-use-cases)
15. [Advanced Features](#advanced-features)
16. [Performance & Best Practices](#performance--best-practices)
17. [Integration Patterns](#integration-patterns)
18. [Troubleshooting](#troubleshooting)
19. [API Reference](#api-reference)

---

## Introduction

### What is the Playground?

The X URL Playground is a **local HTTP server** that provides a complete simulation of the X (Twitter) API v2 for testing and development. It runs entirely on your local machine and requires no internet connection (after initial setup).

### Key Features

- ✅ **Complete API Compatibility**: Supports all endpoints from the X API OpenAPI specification
- ✅ **Stateful Operations**: Maintains in-memory state for realistic testing workflows
- ✅ **State Persistence**: Optional file-based persistence across server restarts
- ✅ **Request Validation**: Validates request bodies, query parameters, and field selections
- ✅ **Error Responses**: Matches real API error formats exactly
- ✅ **Rate Limiting**: Configurable rate limit simulation
- ✅ **OpenAPI-Driven**: Automatically supports new endpoints as they're added to the spec
- ✅ **No API Credits**: Test without consuming your X API quota
- ✅ **Offline Development**: Work without internet connectivity
- ✅ **CORS Support**: Works with web applications
- ✅ **Interactive Web UI**: Data Explorer with relationships view, search operators, and pagination
- ✅ **Relationship Management**: View and search user relationships (likes, follows, bookmarks, etc.)

### Use Cases

- **Development**: Test API integrations locally without hitting rate limits
- **CI/CD**: Run automated tests against a predictable API
- **Learning**: Explore X API endpoints without API keys
- **Prototyping**: Build and test features before deploying
- **Debugging**: Reproduce issues in a controlled environment

### Architecture

```
┌─────────────────┐
│  playground CLI │
│   (standalone)  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  HTTP Server    │
│  (localhost)    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐      ┌──────────────┐
│  OpenAPI Spec   │◄─────│  X API Spec │
│     Cache       │      │  (api.x.com)│
└────────┬────────┘      └──────────────┘
         │
         ▼
┌─────────────────┐      ┌──────────────┐
│  State Manager  │◄─────│  Persistence │
│   (In-Memory)   │      │    (File)     │
└─────────────────┘      └──────────────┘
```

---

## Installation

### Prerequisites

- **Go 1.21 or later**: Required to build the playground
- **Internet Connection**: Required for initial OpenAPI spec download (optional after)
- **4GB RAM**: Recommended for optimal performance
- **100MB Disk Space**: For OpenAPI cache and state files

### Install Playground

#### Option 1: Build from Source

```bash
# Clone the repository
git clone https://github.com/xdevplatform/playground.git
cd playground

# Build the binary
go build -o playground ./cmd/playground

# Make executable (optional)
chmod +x playground

# Test installation
./playground --help

# Move to PATH (optional, for global access)
sudo mv playground /usr/local/bin/
```

#### Option 2: Install via go install (Recommended)

```bash
# Install latest version
go install github.com/xdevplatform/playground/cmd/playground@latest

# Or install a specific version
go install github.com/xdevplatform/playground/cmd/playground@v1.0.0
```

This will install the `playground` binary to `$GOPATH/bin` (or `$HOME/go/bin` by default).

Make sure `$GOPATH/bin` is in your `PATH`:
```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

#### Option 3: Download Pre-built Binary

```bash
# Download from releases
# Visit https://github.com/xdevplatform/playground/releases/latest
# Download the binary for your platform

# Or use wget/curl (example for Linux amd64)
wget https://github.com/xdevplatform/playground/releases/latest/download/playground-linux-amd64 -O playground
chmod +x playground
sudo mv playground /usr/local/bin/
```

### Verify Installation

```bash
# Check help
playground --help

# Expected output:
# X API Playground - Local X API v2 simulator
# 
# Usage:
#   playground [command]
# 
# Available Commands:
#   refresh   Refresh OpenAPI spec cache
#   start     Start the playground API server
#   status    Check playground server status
```

### Post-Installation Setup

1. **Create Configuration Directory** (optional):
```bash
mkdir -p ~/.playground
```

2. **Create Configuration File** (optional):
```bash
cat > ~/.playground/config.json <<EOF
{
  "persistence": {
    "enabled": true,
    "auto_save": true,
    "save_interval": 60
  }
}
EOF
```

---

## Quick Start

### 1. Start the Playground Server

```bash
playground start
```

**Expected Output:**
```
Loaded OpenAPI spec (version: 3.1.0)
Playground server starting on http://localhost:8080
Supported endpoints: All X API v2 endpoints from OpenAPI spec
Management endpoints: /health, /rate-limits, /config, /state
State persistence: ENABLED (file: ~/.playground/state.json, auto-save: true, interval: 60s)
Web UI: http://localhost:8080/playground
Set API_BASE_URL=http://localhost:8080 to use the playground with your API client
```

### 2. Make Your First Request

**In a new terminal:**

```bash
# Get the authenticated user
curl http://localhost:8080/2/users/me \
  -H "Authorization: Bearer test"

# Expected Response:
# {
#   "data": {
#     "id": "0",
#     "name": "Playground User",
#     "username": "playground_user",
#     "created_at": "2025-01-01T00:00:00Z"
#   }
# }
```

### 3. Create a Tweet

```bash
curl -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from the playground!"}'

# Expected Response:
# {
#   "data": {
#     "id": "1",
#     "text": "Hello from the playground!"
#   }
# }
```

### 4. Use with API Clients

You can use any HTTP client with the playground:

**Using curl:**
```bash
# Get authenticated user
curl -H "Authorization: Bearer test_token" http://localhost:8080/2/users/me

# Create a tweet
curl -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test_token" \
  -H "Content-Type: application/json" \
  -d '{"text": "Testing!"}'
```

**Using environment variable:**
```bash
# Set playground as API base URL
export API_BASE_URL=http://localhost:8080

# Now your API client can use the playground
```

**Using the Web UI:**
Open http://localhost:8080/playground in your browser to interactively explore and test endpoints.

The Web UI includes:
- **Data Explorer**: Browse and search users, posts, lists, relationships, and other entities
- **Search Operators**: Use advanced search operators like `user:username`, `type:bookmark`, `post:id` to filter relationships
- **Relationship Viewing**: Explore user relationships including likes, follows, bookmarks, reposts, muting, blocking, and list memberships
- **State Statistics**: View counts and statistics for all entity types
- **API Testing**: Build and test API requests interactively

### 5. Stop the Server

Press `Ctrl+C` in the terminal where the server is running. The server will:
- Save state (if persistence enabled)
- Shut down gracefully
- Display: `✅ Playground server stopped gracefully`

---

## CLI Commands

### `playground start`

Start the playground API server.

#### Syntax

```bash
playground start [flags]
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8080` | Port to run the playground server on |
| `--host` | | `localhost` | Host to bind the playground server to |
| `--refresh` | | `false` | Force refresh of OpenAPI spec cache |

#### Examples

```bash
# Start on default port (8080)
playground start

# Start on custom port
playground start --port 3000

# Start on all interfaces (0.0.0.0)
playground start --host 0.0.0.0

# Start and refresh OpenAPI spec
playground start --refresh

# Start on custom port and refresh
playground start --port 9000 --refresh
```

#### Behavior

- Server runs until interrupted (`Ctrl+C`)
- State is saved on shutdown (if persistence enabled)
- OpenAPI spec is cached locally (refreshed if older than 24 hours)
- Logs are written to stdout

#### Exit Codes

- `0`: Normal shutdown
- `1`: Error starting server
- `130`: Interrupted by user (Ctrl+C)

---

### `playground status`

Check if the playground server is running.

#### Syntax

```bash
playground status
```

#### Examples

```bash
# Check if server is running
playground status

# Expected Output (if running):
# ✅ Playground server is running on port 8080

# Expected Output (if not running):
# ❌ No playground server found
```

#### Behavior

- Attempts to connect to `http://localhost:8080/2/users/me`
- Timeout: 2 seconds
- Returns exit code `1` if server is not running

---

### `playground refresh`

Force refresh the cached OpenAPI specification.

#### Syntax

```bash
playground refresh
```

#### Examples

```bash
# Refresh OpenAPI spec cache
playground refresh

# Expected Output:
# ✅ OpenAPI spec cache refreshed
```

#### Behavior

- Fetches latest spec from `https://api.x.com/2/openapi.json`
- Updates local cache file
- Displays cache location and age

#### When to Use

- After X API adds new endpoints
- If you suspect cache is stale
- Before important testing sessions

---

### Cache Management

The OpenAPI spec cache is automatically managed. The cache file is located at `~/.playground/.playground-openapi-cache.json`.

**Cache Behavior:**
- Cache is automatically refreshed if older than 24 hours
- Use `playground start --refresh` to force refresh on startup
- Use `playground refresh` to refresh the cache without starting the server
- Cache is automatically created on first server start

---

## Configuration

### Configuration File Location

The playground reads configuration from:

1. **User Config**: `~/.playground/config.json` (takes precedence)
2. **Default Config**: Embedded default (used if user config doesn't exist)

### Configuration File Format

The configuration file is JSON format:

```json
{
  "tweets": { ... },
  "users": { ... },
  "places": { ... },
  "topics": { ... },
  "streaming": { ... },
  "rate_limit": { ... },
  "errors": { ... },
  "auth": { ... },
  "persistence": { ... }
}
```

### Configuration Options

#### Tweet Configuration

**Purpose**: Customize tweet texts for seeding.

**Structure:**
```json
{
  "tweets": {
    "texts": [
      "Custom tweet text 1",
      "Custom tweet text 2",
      "Another tweet with #hashtags and @mentions"
    ]
  }
}
```

**Fields:**
- `texts` (array of strings): Custom tweet texts used when creating initial tweets

**Example:**
```json
{
  "tweets": {
    "texts": [
      "Just shipped a new feature! 🚀",
      "Reading about #AI developments",
      "Great conversation about API design today"
    ]
  }
}
```

**Default**: Uses embedded default texts if not specified.

---

#### User Configuration

**Purpose**: Define custom user profiles for seeding.

**Structure:**
```json
{
  "users": {
    "profiles": [
      {
        "username": "username",
        "name": "Display Name",
        "description": "Bio text",
        "location": "Location",
        "verified": false,
        "protected": false,
        "url": "https://example.com"
      }
    ]
  }
}
```

**Fields:**
- `profiles` (array of objects): Custom user profiles
  - `username` (string, required): Username (without @)
  - `name` (string, required): Display name
  - `description` (string, optional): Bio/description
  - `location` (string, optional): Location
  - `verified` (boolean, optional): Verified badge
  - `protected` (boolean, optional): Protected account
  - `url` (string, optional): Profile URL

**Example:**
```json
{
  "users": {
    "profiles": [
      {
        "username": "developer",
        "name": "Developer",
        "description": "Software developer",
        "location": "San Francisco, CA",
        "verified": true,
        "protected": false
      },
      {
        "username": "designer",
        "name": "Designer",
        "description": "UI/UX Designer",
        "verified": false,
        "protected": true
      }
    ]
  }
}
```

**Default**: Creates default "Playground User" if not specified.

---

#### Places Configuration

**Purpose**: Define custom places/locations.

**Structure:**
```json
{
  "places": {
    "places": [
      {
        "full_name": "San Francisco, CA",
        "name": "San Francisco",
        "country": "United States",
        "country_code": "US",
        "place_type": "city",
        "latitude": 37.7749,
        "longitude": -122.4194
      }
    ]
  }
}
```

**Fields:**
- `places` (array of objects): Custom places
  - `full_name` (string, required): Full place name
  - `name` (string, required): Place name
  - `country` (string, required): Country name
  - `country_code` (string, required): ISO country code
  - `place_type` (string, required): Type (city, admin, country, poi)
  - `latitude` (number, required): Latitude
  - `longitude` (number, required): Longitude

---

#### Topics Configuration

**Purpose**: Define custom topics.

**Structure:**
```json
{
  "topics": {
    "topics": [
      {
        "name": "Technology",
        "description": "Technology news and discussions"
      }
    ]
  }
}
```

**Fields:**
- `topics` (array of objects): Custom topics
  - `name` (string, required): Topic name
  - `description` (string, optional): Topic description

---

#### Streaming Configuration

**Purpose**: Configure streaming endpoint behavior.

**Structure:**
```json
{
  "streaming": {
    "default_delay_ms": 200
  }
}
```

**Fields:**
- `default_delay_ms` (integer, optional): Default delay between streamed tweets in milliseconds (default: 200)

**Example:**
```json
{
  "streaming": {
    "default_delay_ms": 500
  }
}
```

---

#### Rate Limit Configuration

**Purpose**: Configure rate limiting simulation.

**Structure:**
```json
{
  "rate_limit": {
    "enabled": true,
    "limit": 15,
    "window_sec": 900
  }
}
```

**Fields:**
- `enabled` (boolean, optional): Enable rate limiting (default: false)
- `limit` (integer, optional): Requests per window (default: 15)
- `window_sec` (integer, optional): Window size in seconds (default: 900 = 15 minutes)

**Example:**
```json
{
  "rate_limit": {
    "enabled": true,
    "limit": 15,
    "window_sec": 900
  }
}
```

**Behavior:**
- When enabled, tracks requests per window
- Returns `429 Too Many Requests` when limit exceeded
- Includes rate limit headers in responses
- Resets after window expires

**Rate Limit Headers:**
- `x-rate-limit-limit`: Request limit per window
- `x-rate-limit-remaining`: Remaining requests
- `x-rate-limit-reset`: Reset time (Unix timestamp)

---

#### Error Configuration

**Purpose**: Configure error simulation (for testing error handling).

**Structure:**
```json
{
  "errors": {
    "enabled": false,
    "error_rate": 0.0,
    "error_type": "rate_limit",
    "status_code": 429
  }
}
```

**Fields:**
- `enabled` (boolean, optional): Enable error simulation (default: false)
- `error_rate` (float, optional): Probability of error (0.0-1.0, default: 0.0)
- `error_type` (string, optional): Type of error: "rate_limit", "server_error", "not_found" (default: "rate_limit")
- `status_code` (integer, optional): HTTP status code (default: 429 for rate_limit)

**Example:**
```json
{
  "errors": {
    "enabled": true,
    "error_rate": 0.1,
    "error_type": "server_error",
    "status_code": 500
  }
}
```

**Use Case**: Test error handling in your application.

---

#### Authentication Configuration

**Purpose**: Configure authentication validation.

**Structure:**
```json
{
  "auth": {
    "disable_validation": false
  }
}
```

**Fields:**
- `disable_validation` (boolean, optional): If `true`, allows requests without authentication (default: false)

**Example (Testing Only):**
```json
{
  "auth": {
    "disable_validation": true
  }
}
```

**Warning**: Only disable for testing. The real X API always requires authentication.

**Behavior:**
- `false` (default): Enforces authentication like real API
- `true`: Allows requests without `Authorization` header

---

#### Persistence Configuration

**Purpose**: Configure state persistence to disk.

**Structure:**
```json
{
  "persistence": {
    "enabled": true,
    "file_path": "~/.playground/state.json",
    "auto_save": true,
    "save_interval": 60
  }
}
```

**Fields:**
- `enabled` (boolean, optional): Enable state persistence (default: false)
- `file_path` (string, optional): Path to state file (default: `~/.playground/state.json`)
- `auto_save` (boolean, optional): Auto-save on state changes (default: true if enabled)
- `save_interval` (integer, optional): Auto-save interval in seconds (default: 60)

**Example:**
```json
{
  "persistence": {
    "enabled": true,
    "file_path": "~/.playground/state.json",
    "auto_save": true,
    "save_interval": 60
  }
}
```

**Behavior:**
- When enabled, state is saved to disk
- Auto-save runs periodically (every `save_interval` seconds)
- State is saved on server shutdown
- State is loaded on server startup

**File Format**: JSON

**File Location**: Defaults to `~/.playground/state.json`

---

### Complete Configuration Example

```json
{
  "tweets": {
    "texts": [
      "Just shipped a new feature! 🚀",
      "Reading about #AI developments",
      "Great conversation about API design"
    ]
  },
  "users": {
    "profiles": [
      {
        "username": "developer",
        "name": "Developer",
        "description": "Software developer",
        "verified": true
      }
    ]
  },
  "rate_limit": {
    "enabled": true,
    "limit": 15,
    "window_sec": 900
  },
  "auth": {
    "disable_validation": false
  },
  "persistence": {
    "enabled": true,
    "file_path": "~/.playground/state.json",
    "auto_save": true,
    "save_interval": 60
  }
}
```

### Updating Configuration

#### Method 1: Edit Configuration File

1. Edit `~/.playground/config.json`
2. Restart the server: `playground start`

#### Method 2: Via API (Temporary)

```bash
# Update config (temporary - lost on restart)
curl -X PUT http://localhost:8080/config/update \
  -H "Content-Type: application/json" \
  -d '{
    "rate_limit": {
      "enabled": true,
      "limit": 20
    }
  }'
```

**Note**: API updates are temporary and lost on server restart.

---

## API Endpoints

The playground supports **all endpoints** from the X API OpenAPI specification. Endpoints are organized into categories below.

### Base URL

All endpoints are prefixed with `/2/`:
```
http://localhost:8080/2/{endpoint}
```

### Management Endpoints

These endpoints are specific to the playground (not part of X API):

#### `GET /health`

Server health and statistics.

**Authentication**: Not required

**Response:**
```json
{
  "status": "healthy",
  "uptime_seconds": 3600,
  "requests_total": 150,
  "requests_success": 145,
  "requests_error": 5,
  "avg_response_time_ms": 12
}
```

**Fields:**
- `status` (string): Server status ("healthy")
- `uptime_seconds` (integer): Server uptime in seconds
- `requests_total` (integer): Total requests processed
- `requests_success` (integer): Successful requests (2xx)
- `requests_error` (integer): Error requests (4xx, 5xx)
- `avg_response_time_ms` (integer): Average response time in milliseconds

**Example:**
```bash
curl http://localhost:8080/health
```

---

#### `GET /rate-limits`

Rate limit configuration and status.

**Authentication**: Not required

**Response:**
```json
{
  "enabled": true,
  "limit": 15,
  "window_sec": 900,
  "remaining": 10,
  "reset_at": "2025-12-15T10:30:00Z"
}
```

**Fields:**
- `enabled` (boolean): Whether rate limiting is enabled
- `limit` (integer): Requests per window
- `window_sec` (integer): Window size in seconds
- `remaining` (integer): Remaining requests in current window
- `reset_at` (string): ISO 8601 timestamp when window resets

**Example:**
```bash
curl http://localhost:8080/rate-limits
```

---

#### `GET /config`

View current configuration.

**Authentication**: Not required

**Response:**
```json
{
  "tweets": { ... },
  "users": { ... },
  "rate_limit": { ... },
  "auth": { ... },
  "persistence": { ... }
}
```

**Example:**
```bash
curl http://localhost:8080/config | jq
```

---

#### `PUT /config/update`

Update configuration (temporary - lost on restart).

**Authentication**: Not required

**Request Body:**
```json
{
  "rate_limit": {
    "enabled": true,
    "limit": 20
  }
}
```

**Response:**
```json
{
  "message": "Configuration updated",
  "config": { ... }
}
```

**Example:**
```bash
curl -X PUT http://localhost:8080/config/update \
  -H "Content-Type: application/json" \
  -d '{"rate_limit": {"enabled": true}}'
```

**Note**: Changes are temporary and lost on server restart.

---

#### `POST /state/reset`

Reset state to initial seed data.

**Authentication**: Not required

**Response:**
```json
{
  "message": "State reset successfully"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/state/reset
```

**Warning**: This deletes all current state (tweets, users, relationships, etc.).

---

#### `GET /state/export`

Export current state as JSON.

**Authentication**: Not required

**Response:**
```json
{
  "users": { ... },
  "tweets": { ... },
  "media": { ... },
  "lists": { ... },
  "relationships": [
    {
      "id": "like-0-1",
      "type": "like",
      "user_id": "0",
      "target_tweet_id": "1"
    },
    {
      "id": "following-0-2",
      "type": "following",
      "user_id": "0",
      "target_user_id": "2"
    }
  ],
  ...
}
```

**Response Fields:**
- `users`: Map of user objects keyed by user ID
- `tweets`: Map of tweet/post objects keyed by tweet ID
- `lists`: Map of list objects keyed by list ID
- `relationships`: Array of relationship objects representing user interactions (likes, follows, bookmarks, etc.)
  - `id`: Unique relationship identifier
  - `type`: Relationship type (`bookmark`, `like`, `following`, `follower`, `retweet`, `mute`, `block`, `list_member`, `followed_list`, `pinned_list`)
  - `user_id`: ID of the user who initiated the relationship
  - `target_user_id`: ID of the target user (for user-to-user relationships)
  - `target_tweet_id`: ID of the target tweet/post (for user-to-tweet relationships)
  - `target_list_id`: ID of the target list (for user-to-list relationships)

**Example:**
```bash
# Export to file
curl http://localhost:8080/state/export > my-state.json

# Pretty print
curl http://localhost:8080/state/export | jq > my-state.json
```

**Use Case**: Backup state, share state between environments, debug state issues.

---

#### `POST /state/import`

Import state from JSON.

**Authentication**: Not required

**Request Body:**
```json
{
  "users": { ... },
  "tweets": { ... },
  ...
}
```

**Response:**
```json
{
  "message": "State imported successfully",
  "users_count": 10,
  "tweets_count": 50
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/state/import \
  -H "Content-Type: application/json" \
  -d @my-state.json
```

**Use Case**: Restore from backup, load test data, share state.

---

#### `POST /state/save`

Manually save state (if persistence enabled).

**Authentication**: Not required

**Response:**
```json
{
  "message": "State saved successfully",
  "file_path": "~/.playground/state.json"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/state/save
```

**Use Case**: Force save before important operations, ensure state is persisted.

---

## Usage & Cost Tracking API

The playground provides API endpoints to programmatically access the same usage and cost data shown in the Usage tab of the web UI. These endpoints track API usage at the developer account level and provide detailed cost breakdowns.

**Quick Start:**
```bash
# Get complete cost breakdown (same as Usage tab)
curl http://localhost:8080/api/accounts/0/cost | jq

# Get usage breakdown by event type
curl "http://localhost:8080/api/accounts/0/usage?interval=30days&groupBy=eventType" | jq

# Get usage breakdown by request type
curl "http://localhost:8080/api/accounts/0/usage?interval=30days&groupBy=requestType" | jq
```

**Note:** The `account_id` parameter is your developer account ID, automatically derived from your authentication token. The default token "test" maps to account "0". Different tokens map to different developer accounts.

---

#### `GET /api/credits/pricing`

Get the current pricing configuration for all event types and request types.

**Authentication**: Not required

**Query Parameters:**
- `refresh` (boolean, optional): If `true`, forces a refresh of pricing from the API. Default: `false`

**Response:**
```json
{
  "eventTypePricing": {
    "Post": 0.005,
    "User": 0.01,
    "Like": 0.001,
    "Follow": 0.01,
    ...
  },
  "requestTypePricing": {
    "Write": 0.01,
    "ContentCreate": 0.01,
    "UserInteractionCreate": 0.015,
    ...
  },
  "defaultCost": 0
}
```

**Fields:**
- `eventTypePricing` (object): Map of event type names to their cost per resource
- `requestTypePricing` (object): Map of request type names to their cost per request
- `defaultCost` (number): Default cost for unpriced endpoints

**Example:**
```bash
# Get current pricing
curl http://localhost:8080/api/credits/pricing | jq

# Force refresh pricing from API
curl "http://localhost:8080/api/credits/pricing?refresh=true" | jq
```

---

#### `GET /api/accounts/{account_id}/usage`

Get usage data for a specific account, grouped by event type or request type.

**Authentication**: Not required

**Path Parameters:**
- `account_id` (string, required): The developer account ID.
  
  **Important:** In the real X API, `account_id` refers to the **developer account ID** (the account that owns the API keys/apps), **not** the user ID. All usage across all apps and users under a developer account is aggregated together.
  
  In the playground, the developer account ID is automatically derived from the authentication token:
  - **Bearer tokens**: Developer account ID is derived from the token (same token = same account)
  - **OAuth 1.0a**: Developer account ID is derived from the consumer key
  - **OAuth 2.0**: Developer account ID would be extracted from token claims (in production)
  - **Default**: Falls back to authenticated user ID for simple tokens (typically "0")
  
  To simulate multiple developer accounts, use different Bearer tokens or OAuth consumer keys - each will map to a different developer account ID.

**Query Parameters:**
- `interval` (string, optional): Time interval for usage data. Options: `"7days"`, `"30days"`, `"90days"`. Default: `"30days"`
- `groupBy` (string, required): Grouping type. Must be either `"eventType"` or `"requestType"`

**Response:**
```json
{
  "accountID": "0",
  "interval": "30days",
  "groupBy": "eventType",
  "groups": {
    "Post": {
      "type": "Post",
      "usage": "150",
      "price": "0.005",
      "totalCost": 0.75,
      "usageDataPoints": [
        {
          "timestamp": "1704067200000",
          "value": "10"
        },
        ...
      ]
    },
    ...
  },
  "total": {
    "usage": "500",
    "totalCost": 2.5,
    "usageDataPoints": [...]
  }
}
```

**Fields:**
- `accountID` (string): The account ID
- `interval` (string): The time interval used
- `groupBy` (string): The grouping type used
- `groups` (object): Map of type names to usage data
  - Each group contains:
    - `type` (string): The type name
    - `usage` (string): Total usage count as string
    - `price` (string): Price per item as string
    - `totalCost` (number): Total cost for this type
    - `usageDataPoints` (array): Time series data points
- `total` (object): Aggregate totals across all types

**Example:**
```bash
# Get event type usage for account 0
curl "http://localhost:8080/api/accounts/0/usage?interval=30days&groupBy=eventType" | jq

# Get request type usage for account 0
curl "http://localhost:8080/api/accounts/0/usage?interval=30days&groupBy=requestType" | jq

# Get 7-day usage
curl "http://localhost:8080/api/accounts/0/usage?interval=7days&groupBy=eventType" | jq
```

---

#### `GET /api/accounts/{account_id}/cost`

Get detailed cost breakdown for a specific account, including billing cycle information and time series data.

**Authentication**: Not required

**Path Parameters:**
- `account_id` (string, required): The developer account ID.
  
  **Note:** In the real X API, this is the developer account ID (the account that owns the API keys/apps), not the user ID. In the playground, this is automatically derived from your authentication token. Use different tokens/keys to simulate different developer accounts.

**Response:**
```json
{
  "accountID": "0",
  "totalCost": 2.5,
  "eventTypeCosts": [
    {
      "type": "Post",
      "usage": 150,
      "price": 0.005,
      "totalCost": 0.75
    },
    ...
  ],
  "requestTypeCosts": [
    {
      "type": "Write",
      "usage": 50,
      "price": 0.01,
      "totalCost": 0.5
    },
    ...
  ],
  "billingCycleStart": "2026-01-08T00:00:00Z",
  "currentBillingCycle": 1,
  "eventTypeTimeSeries": [
    {
      "date": "2026-01-08",
      "timestamp": 1704067200000,
      "costs": {
        "Post": 0.05,
        "User": 0.1
      }
    },
    ...
  ],
  "requestTypeTimeSeries": [
    {
      "date": "2026-01-08",
      "timestamp": 1704067200000,
      "costs": {
        "Write": 0.1,
        "ContentCreate": 0.05
      }
    },
    ...
  ]
}
```

**Fields:**
- `accountID` (string): The account ID
- `totalCost` (number): Total estimated cost for the current billing cycle
- `eventTypeCosts` (array): Cost breakdown by event type
  - `type` (string): Event type name
  - `usage` (integer): Number of resources used
  - `price` (number): Price per resource
  - `totalCost` (number): Total cost for this type
- `requestTypeCosts` (array): Cost breakdown by request type (same structure as eventTypeCosts)
- `billingCycleStart` (string): ISO 8601 timestamp of the billing cycle start date
- `currentBillingCycle` (integer): Current billing cycle number (1-based, resets monthly)
- `eventTypeTimeSeries` (array): Daily cost data for event types over the billing cycle
  - `date` (string): Date in YYYY-MM-DD format
  - `timestamp` (integer): Unix timestamp in milliseconds
  - `costs` (object): Map of event type names to daily costs
- `requestTypeTimeSeries` (array): Daily cost data for request types over the billing cycle (same structure as eventTypeTimeSeries)

**Example:**
```bash
# Get cost breakdown for account 0
curl http://localhost:8080/api/accounts/0/cost | jq

# Pretty print with jq
curl http://localhost:8080/api/accounts/0/cost | jq '.totalCost, .eventTypeCosts, .requestTypeCosts'

# Get time series data only
curl http://localhost:8080/api/accounts/0/cost | jq '.eventTypeTimeSeries'
```

**Note:** 
- The billing cycle is a 30-day period starting from the first request made by the account. The cycle resets monthly.
- **Developer Account vs User ID:** In the real X API, `account_id` refers to the **developer account ID** (the account that owns the API keys/apps), **not** the user ID. All usage across all apps and users under a developer account is aggregated together. In the playground, the developer account ID is automatically derived from your authentication token (Bearer token, OAuth consumer key, etc.), matching the real API behavior.

**Getting the Same Data as the Usage Tab:**

The `/api/accounts/{account_id}/cost` endpoint returns all the data displayed in the Usage tab:
- Total cost and billing cycle information
- Cost breakdowns by event type and request type  
- Daily time series data for the charts (`eventTypeTimeSeries` and `requestTypeTimeSeries`)
- Usage statistics

To replicate what the Usage tab shows programmatically:

```bash
# Get the complete cost breakdown (includes all Usage tab data)
curl http://localhost:8080/api/accounts/0/cost | jq

# Extract specific fields
curl -s http://localhost:8080/api/accounts/0/cost | jq '.totalCost'                    # Total cost
curl -s http://localhost:8080/api/accounts/0/cost | jq '.eventTypeCosts'               # Event type breakdown
curl -s http://localhost:8080/api/accounts/0/cost | jq '.requestTypeCosts'             # Request type breakdown
curl -s http://localhost:8080/api/accounts/0/cost | jq '.eventTypeTimeSeries'         # Chart data for event types
curl -s http://localhost:8080/api/accounts/0/cost | jq '.requestTypeTimeSeries'       # Chart data for request types
curl -s http://localhost:8080/api/accounts/0/cost | jq '.billingCycleStart'           # Billing cycle start
curl -s http://localhost:8080/api/accounts/0/cost | jq '.currentBillingCycle'         # Current cycle number
```

The Usage tab also fetches additional usage details from `/api/accounts/{account_id}/usage` endpoints, but the `/cost` endpoint provides the primary data structure.

---

## API Endpoints (Continued)

Due to the extensive number of endpoints, I'll continue with detailed documentation in the next section. The playground supports all X API v2 endpoints. Here are the main categories:

### User Endpoints (20+ endpoints)
### Tweet Endpoints (15+ endpoints)
### List Endpoints (15+ endpoints)
### Media Endpoints (10+ endpoints)
### Space Endpoints (10+ endpoints)
### DM Endpoints (10+ endpoints)
### OAuth Endpoints
### Compliance Endpoints
### Search Stream Endpoints
### Activity Subscription Endpoints
### Notes Endpoints
### Trends & Insights Endpoints

---

## Query Parameters & Field Selection

### Field Selection Overview

The X API uses field selection to control which fields are returned in responses. This reduces payload size and improves performance.

### Field Selection Syntax

Use `{object}.fields` query parameters to specify which fields to return:

```
?user.fields=id,name,username
?tweet.fields=id,text,created_at
?list.fields=id,name,description
```

### Available Fields by Object Type

#### User Fields (`user.fields`)

**Default Fields** (returned if no `user.fields` specified):
- `id`
- `name`
- `username`

**All Available Fields**:
- `id` - User ID (Snowflake)
- `name` - Display name
- `username` - Username (without @)
- `created_at` - Account creation date (ISO 8601)
- `description` - Bio/description
- `entities` - Entities extracted from description/URL
- `location` - Location
- `pinned_tweet_id` - ID of pinned tweet
- `profile_image_url` - Profile image URL
- `protected` - Protected account (boolean)
- `public_metrics` - Public metrics object
  - `followers_count`
  - `following_count`
  - `tweet_count`
  - `listed_count`
  - `like_count`
  - `media_count`
- `url` - Profile URL
- `verified` - Verified badge (boolean)
- `verified_type` - Verification type
- `withheld` - Withheld information

**Example:**
```bash
# Get only id and name
curl "http://localhost:8080/2/users/0?user.fields=id,name" \
  -H "Authorization: Bearer test"

# Get all fields
curl "http://localhost:8080/2/users/0?user.fields=id,name,username,created_at,description,location,url,verified,protected,profile_image_url,pinned_tweet_id,public_metrics,entities,verified_type,withheld" \
  -H "Authorization: Bearer test"
```

#### Tweet Fields (`tweet.fields`)

**Default Fields** (returned if no `tweet.fields` specified):
- `id`
- `text`

**All Available Fields**:
- `id` - Tweet ID (Snowflake)
- `text` - Tweet text
- `attachments` - Attachments object
  - `media_keys` - Array of media keys
  - `poll_ids` - Array of poll IDs
- `author_id` - Author user ID
- `context_annotations` - Context annotations
- `conversation_id` - Conversation ID
- `created_at` - Creation date (ISO 8601)
- `edit_controls` - Edit controls
- `edit_history_tweet_ids` - Array of tweet IDs in edit history (always included)
- `entities` - Entities extracted from text
  - `hashtags` - Array of hashtag entities
  - `mentions` - Array of mention entities
  - `urls` - Array of URL entities
  - `cashtags` - Array of cashtag entities
- `geo` - Geo information
- `in_reply_to_user_id` - User ID being replied to
- `lang` - Language code
- `non_public_metrics` - Non-public metrics (requires elevated access)
- `organic_metrics` - Organic metrics (requires elevated access)
- `possibly_sensitive` - Possibly sensitive content (boolean)
- `promoted_metrics` - Promoted metrics (requires elevated access)
- `public_metrics` - Public metrics object
  - `retweet_count`
  - `reply_count`
  - `like_count`
  - `quote_count`
  - `bookmark_count`
  - `impression_count`
- `referenced_tweets` - Array of referenced tweets
- `reply_settings` - Reply settings
- `source` - Source client
- `withheld` - Withheld information

**Example:**
```bash
# Get only id and text
curl "http://localhost:8080/2/tweets/0?tweet.fields=id,text" \
  -H "Authorization: Bearer test"

# Get tweet with metrics
curl "http://localhost:8080/2/tweets/0?tweet.fields=id,text,created_at,public_metrics,author_id" \
  -H "Authorization: Bearer test"
```

#### List Fields (`list.fields`)

**Default Fields**:
- `id`
- `name`

**All Available Fields**:
- `id` - List ID
- `name` - List name
- `created_at` - Creation date
- `description` - List description
- `follower_count` - Follower count
- `member_count` - Member count
- `owner_id` - Owner user ID
- `private` - Private list (boolean)

**Example:**
```bash
curl "http://localhost:8080/2/lists/0?list.fields=id,name,description,member_count,follower_count" \
  -H "Authorization: Bearer test"
```

#### Media Fields (`media.fields`)

**Default Fields**:
- `media_key`
- `type`

**All Available Fields**:
- `media_key` - Media key
- `type` - Media type (photo, video, animated_gif)
- `url` - Media URL
- `duration_ms` - Duration in milliseconds (video)
- `height` - Height in pixels
- `width` - Width in pixels
- `preview_image_url` - Preview image URL
- `public_metrics` - Public metrics
- `alt_text` - Alt text
- `variants` - Video variants

**Example:**
```bash
curl "http://localhost:8080/2/media/upload?command=STATUS&media_id=123&media.fields=media_key,type,url,width,height" \
  -H "Authorization: Bearer test"
```

#### Space Fields (`space.fields`)

**Default Fields**:
- `id`
- `title`
- `state`
- `created_at`

**All Available Fields**:
- `id` - Space ID
- `title` - Space title
- `state` - Space state (scheduled, live, ended)
- `created_at` - Creation date
- `updated_at` - Last update date
- `started_at` - Start time
- `ended_at` - End time
- `scheduled_start` - Scheduled start time
- `creator_id` - Creator user ID
- `host_ids` - Array of host user IDs
- `speaker_ids` - Array of speaker user IDs
- `invited_user_ids` - Array of invited user IDs
- `subscriber_count` - Subscriber count
- `participant_count` - Participant count
- `is_ticketed` - Ticketed space (boolean)
- `lang` - Language code

#### Poll Fields (`poll.fields`)

**Default Fields**:
- `id`
- `options`
- `voting_status`

**All Available Fields**:
- `id` - Poll ID
- `options` - Array of poll options
  - `position` - Option position
  - `label` - Option label
  - `votes` - Vote count
- `duration_minutes` - Duration in minutes
- `end_datetime` - End datetime
- `voting_status` - Voting status (open, closed)

#### Place Fields (`place.fields`)

**Default Fields**:
- `id`
- `full_name`
- `name`

**All Available Fields**:
- `id` - Place ID
- `full_name` - Full place name
- `name` - Place name
- `country` - Country name
- `country_code` - ISO country code
- `place_type` - Place type (city, admin, country, poi)
- `geo` - Geo information
  - `type` - Geo type (Point)
  - `bbox` - Bounding box
  - `properties` - Geo properties

### Field Selection Best Practices

1. **Request Only Needed Fields**: Reduces payload size and improves performance
2. **Use Defaults When Possible**: Default fields are optimized for common use cases
3. **Combine with Expansions**: Request fields for expanded objects too
4. **Validate Fields**: Invalid fields return validation errors

**Example:**
```bash
# Efficient: Only request needed fields
curl "http://localhost:8080/2/users/0?user.fields=id,name,username" \
  -H "Authorization: Bearer test"

# Less efficient: Request all fields
curl "http://localhost:8080/2/users/0?user.fields=id,name,username,created_at,description,location,url,verified,protected,profile_image_url,pinned_tweet_id,public_metrics,entities" \
  -H "Authorization: Bearer test"
```

---

## Expansions

### Expansion Overview

Expansions allow you to request related objects in the same response, reducing the number of API calls needed.

### Expansion Syntax

Use `expansions` query parameter with comma-separated expansion names:

```
?expansions=author_id
?expansions=author_id,referenced_tweets.id
```

### Available Expansions

#### Common Expansions

- `author_id` - Expand tweet author (returns user object)
- `referenced_tweets.id` - Expand referenced tweets
- `referenced_tweets.id.author_id` - Expand referenced tweet authors
- `in_reply_to_user_id` - Expand user being replied to
- `attachments.media_keys` - Expand media attachments
- `attachments.poll_ids` - Expand poll attachments
- `geo.place_id` - Expand place information
- `entities.mentions.username` - Expand mentioned users
- `referenced_tweets.id.author_id` - Nested expansion

#### User-Related Expansions

- `pinned_tweet_id` - Expand pinned tweet
- `pinned_tweet_id.author_id` - Expand pinned tweet author

#### List-Related Expansions

- `owner_id` - Expand list owner
- `list_id` - Expand list information

#### Space-Related Expansions

- `creator_id` - Expand space creator
- `host_ids` - Expand space hosts
- `speaker_ids` - Expand space speakers
- `invited_user_ids` - Expand invited users

### Expansion Response Format

Expanded objects are returned in the `includes` object:

```json
{
  "data": {
    "id": "123",
    "text": "Hello!",
    "author_id": "456"
  },
  "includes": {
    "users": [
      {
        "id": "456",
        "name": "User",
        "username": "user"
      }
    ]
  }
}
```

### Expansion Examples

#### Expand Tweet Author

```bash
curl "http://localhost:8080/2/tweets/0?expansions=author_id&user.fields=id,name,username" \
  -H "Authorization: Bearer test"
```

**Response:**
```json
{
  "data": {
    "id": "0",
    "text": "Hello!",
    "author_id": "0"
  },
  "includes": {
    "users": [
      {
        "id": "0",
        "name": "Playground User",
        "username": "playground_user"
      }
    ]
  }
}
```

#### Expand Referenced Tweets

```bash
curl "http://localhost:8080/2/tweets/0?expansions=referenced_tweets.id&tweet.fields=id,text" \
  -H "Authorization: Bearer test"
```

#### Expand Media Attachments

```bash
curl "http://localhost:8080/2/tweets/0?expansions=attachments.media_keys&media.fields=media_key,type,url" \
  -H "Authorization: Bearer test"
```

#### Multiple Expansions

```bash
curl "http://localhost:8080/2/tweets/0?expansions=author_id,referenced_tweets.id.author_id&user.fields=id,name,username&tweet.fields=id,text" \
  -H "Authorization: Bearer test"
```

### Expansion Validation

Invalid expansions return validation errors:

```json
{
  "errors": [
    {
      "parameter": "expansions",
      "value": "invalid_expansion",
      "detail": "The following expansions are not valid for this endpoint: invalid_expansion"
    }
  ]
}
```

---

## Request Validation

### Validation Overview

The playground validates requests against the OpenAPI specification to match real API behavior.

### What Gets Validated

1. **Request Bodies**: Validates JSON structure, required fields, types, constraints
2. **Query Parameters**: Validates types, formats, constraints, bounds
3. **Path Parameters**: Validates format (e.g., Snowflake IDs)
4. **Field Selection**: Validates requested fields exist
5. **Expansions**: Validates requested expansions exist
6. **Unknown Parameters**: Rejects parameters not in OpenAPI spec

### Request Body Validation

#### Required Fields

Missing required fields return validation errors:

**Request:**
```json
{}
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "text",
      "value": "",
      "detail": "text field is required"
    }
  ]
}
```

#### Type Validation

Invalid types return validation errors:

**Request:**
```json
{
  "text": 123
}
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "text",
      "value": 123,
      "detail": "text must be a string"
    }
  ]
}
```

#### Constraint Validation

Values outside constraints return validation errors:

**Request:**
```json
{
  "text": ""
}
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "text",
      "value": "",
      "detail": "text field is required"
    }
  ]
}
```

#### Nested Object Validation

Nested objects are validated recursively:

**Request:**
```json
{
  "text": "Hello",
  "media": {
    "media_ids": ["invalid"]
  }
}
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "media.media_ids[0]",
      "value": "invalid",
      "detail": "media ID must be a valid Snowflake ID"
    }
  ]
}
```

### Query Parameter Validation

#### Type Validation

**Example:**
```bash
# Invalid: max_results must be integer
curl "http://localhost:8080/2/tweets?max_results=abc" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "max_results",
      "value": "abc",
      "detail": "max_results must be an integer"
    }
  ]
}
```

#### Constraint Validation

**Example:**
```bash
# Invalid: max_results must be between 5-100
curl "http://localhost:8080/2/tweets?max_results=200" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "max_results",
      "value": "200",
      "detail": "max_results must be at most 100"
    }
  ]
}
```

#### Format Validation

**Example:**
```bash
# Invalid: start_time must be ISO 8601 format
curl "http://localhost:8080/2/tweets/search/recent?query=hello&start_time=invalid" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "start_time",
      "value": "invalid",
      "detail": "start_time must be a valid ISO 8601 datetime"
    }
  ]
}
```

#### Unknown Parameter Rejection

**Example:**
```bash
# Invalid: unknown_param is not a valid parameter
curl "http://localhost:8080/2/users/me?unknown_param=test" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "unknown_param",
      "value": "test",
      "detail": "The query parameter 'unknown_param' is not valid for this endpoint"
    }
  ]
}
```

### Field Selection Validation

Invalid fields return validation errors:

**Example:**
```bash
curl "http://localhost:8080/2/users/me?user.fields=id,name,invalid_field" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "user.fields",
      "value": "invalid_field",
      "detail": "The following fields are not valid for user objects: invalid_field"
    }
  ]
}
```

### Expansion Validation

Invalid expansions return validation errors:

**Example:**
```bash
curl "http://localhost:8080/2/tweets/0?expansions=invalid_expansion" \
  -H "Authorization: Bearer test"
```

**Response (400 Bad Request):**
```json
{
  "errors": [
    {
      "parameter": "expansions",
      "value": "invalid_expansion",
      "detail": "The following expansions are not valid for this endpoint: invalid_expansion"
    }
  ]
}
```

---

## Error Handling

### Error Response Format

All errors follow the X API error format:

```json
{
  "errors": [
    {
      "parameter": "parameter_name",
      "value": "invalid_value",
      "detail": "Error message",
      "title": "Error Title",
      "type": "https://api.twitter.com/2/problems/error-type"
    }
  ],
  "title": "Error Title",
  "detail": "Error detail message",
  "type": "https://api.twitter.com/2/problems/error-type"
}
```

### Error Types

#### Validation Errors (`invalid-request`)

**Status Code**: `400 Bad Request`

**Type**: `https://api.twitter.com/2/problems/invalid-request`

**Example:**
```json
{
  "errors": [
    {
      "parameter": "max_results",
      "value": "200",
      "detail": "max_results must be at most 100",
      "title": "Invalid Request",
      "type": "https://api.twitter.com/2/problems/invalid-request"
    }
  ],
  "title": "Invalid Request",
  "detail": "One or more parameters to your request was invalid.",
  "type": "https://api.twitter.com/2/problems/invalid-request"
}
```

#### Resource Not Found (`resource-not-found`)

**Status Code**: `404 Not Found`

**Type**: `https://api.twitter.com/2/problems/resource-not-found`

**Example:**
```json
{
  "errors": [
    {
      "parameter": "id",
      "value": "999999",
      "detail": "User not found",
      "title": "Not Found Error",
      "type": "https://api.twitter.com/2/problems/resource-not-found",
      "resource_id": "999999",
      "resource_type": "user"
    }
  ],
  "title": "Not Found Error",
  "detail": "User not found",
  "type": "https://api.twitter.com/2/problems/resource-not-found"
}
```

#### Unauthorized (`not-authorized-for-resource`)

**Status Code**: `401 Unauthorized`

**Type**: `https://api.twitter.com/2/problems/not-authorized-for-resource`

**Example:**
```json
{
  "errors": [
    {
      "detail": "Unauthorized",
      "title": "Unauthorized",
      "type": "https://api.twitter.com/2/problems/not-authorized-for-resource"
    }
  ],
  "title": "Unauthorized",
  "detail": "Unauthorized",
  "type": "https://api.twitter.com/2/problems/not-authorized-for-resource"
}
```

#### Forbidden (`about:blank`)

**Status Code**: `403 Forbidden`

**Type**: `about:blank`

The playground returns `403 Forbidden` errors when authenticated users attempt to perform actions they don't have permission for, matching the real X API behavior.

**Examples:**

**Delete List (Not Owner):**
```json
{
  "detail": "You are not allowed to delete this List.",
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403
}
```

**Add List Member (Not Owner):**
```json
{
  "detail": "You are not allowed to add members to this List.",
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403
}
```

**Remove List Member (Not Owner):**
```json
{
  "detail": "You are not allowed to delete members from this List.",
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403
}
```

**Delete Post/Tweet (Not Author):**
```json
{
  "detail": "You are not authorized to delete this Tweet.",
  "type": "about:blank",
  "title": "Forbidden",
  "status": 403
}
```

**When These Errors Occur:**
- `DELETE /2/lists/{id}` - When the authenticated user is not the list owner
- `POST /2/lists/{id}/members` - When the authenticated user is not the list owner
- `DELETE /2/lists/{id}/members/{user_id}` - When the authenticated user is not the list owner
- `DELETE /2/tweets/{id}` - When the authenticated user is not the post author

#### Rate Limit Exceeded (`rate-limit-exceeded`)

**Status Code**: `429 Too Many Requests`

**Type**: `https://api.twitter.com/2/problems/rate-limit-exceeded`

**Example:**
```json
{
  "errors": [
    {
      "detail": "Rate limit exceeded",
      "title": "Rate Limit Exceeded",
      "type": "https://api.twitter.com/2/problems/rate-limit-exceeded"
    }
  ],
  "title": "Rate Limit Exceeded",
  "detail": "Rate limit exceeded",
  "type": "https://api.twitter.com/2/problems/rate-limit-exceeded"
}
```

#### Server Error (`server-error`)

**Status Code**: `500 Internal Server Error`

**Type**: `https://api.twitter.com/2/problems/server-error`

### Error Handling Best Practices

1. **Check Status Code**: Always check HTTP status code first
2. **Parse Error Object**: Parse `errors` array for details
3. **Handle Multiple Errors**: `errors` array can contain multiple errors
4. **Check Error Type**: Use `type` field to determine error category
5. **Log Error Details**: Log `parameter`, `value`, and `detail` for debugging

**Example Error Handling (JavaScript):**
```javascript
try {
  const response = await fetch('http://localhost:8080/2/tweets', {
    method: 'POST',
    headers: {
      'Authorization': 'Bearer test',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ text: '' })
  });
  
  if (!response.ok) {
    const error = await response.json();
    if (error.errors) {
      error.errors.forEach(err => {
        console.error(`Error in ${err.parameter}: ${err.detail}`);
      });
    }
  }
} catch (error) {
  console.error('Request failed:', error);
}
```

---

## Examples and Use Cases

### Complete Workflow Examples

#### Example 1: Create Tweet and View Timeline

```bash
# 1. Create a tweet
TWEET_ID=$(curl -s -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from the playground!"}' | jq -r '.data.id')

echo "Created tweet: $TWEET_ID"

# 2. Get the tweet
curl -s "http://localhost:8080/2/tweets/$TWEET_ID?tweet.fields=id,text,created_at,public_metrics" \
  -H "Authorization: Bearer test" | jq

# 3. Get user's tweets (timeline)
curl -s "http://localhost:8080/2/users/0/tweets?max_results=10&tweet.fields=id,text,created_at" \
  -H "Authorization: Bearer test" | jq
```

#### Example 2: Follow User and View Following List

```bash
# 1. Create a second user (via state import or use existing)
# For this example, assume user ID "1" exists

# 2. Follow the user
curl -X POST http://localhost:8080/2/users/0/following \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"target_user_id": "1"}'

# 3. Get following list
curl -s "http://localhost:8080/2/users/0/following?max_results=10&user.fields=id,name,username" \
  -H "Authorization: Bearer test" | jq

# 4. Get followers of user 1 (should include user 0)
curl -s "http://localhost:8080/2/users/1/followers?max_results=10&user.fields=id,name,username" \
  -H "Authorization: Bearer test" | jq
```

#### Example 3: Like Post and View Likes

```bash
# 1. Create a post
POST_ID=$(curl -s -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"text": "Like this post!"}' | jq -r '.data.id')

# 2. Like the post
curl -X POST http://localhost:8080/2/users/0/likes \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d "{\"tweet_id\": \"$POST_ID\"}"

# 3. Get user's likes
curl -s "http://localhost:8080/2/users/0/likes?max_results=10&tweet.fields=id,text" \
  -H "Authorization: Bearer test" | jq

# 4. Get users who liked the post
curl -s "http://localhost:8080/2/tweets/$POST_ID/liking_users?max_results=10&user.fields=id,name,username" \
  -H "Authorization: Bearer test" | jq
```

**Note**: Relationships created via API calls (likes, follows, bookmarks, etc.) are automatically included in the state export and visible in the Data Explorer.

#### Example 4: Create List and Add Members

```bash
# 1. Create a list
LIST_ID=$(curl -s -X POST http://localhost:8080/2/lists \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"name": "Developers", "description": "List of developers"}' | jq -r '.data.id')

echo "Created list: $LIST_ID"

# 2. Add members (assuming user IDs 1, 2, 3 exist)
for USER_ID in 1 2 3; do
  curl -X POST "http://localhost:8080/2/lists/$LIST_ID/members" \
    -H "Authorization: Bearer test" \
    -H "Content-Type: application/json" \
    -d "{\"user_id\": \"$USER_ID\"}"
done

# 3. Get list members
curl -s "http://localhost:8080/2/lists/$LIST_ID/members?max_results=10&user.fields=id,name,username" \
  -H "Authorization: Bearer test" | jq

# 4. Get tweets from list members
curl -s "http://localhost:8080/2/lists/$LIST_ID/tweets?max_results=10&tweet.fields=id,text,created_at" \
  -H "Authorization: Bearer test" | jq
```

#### Example 5: Search Tweets with Filters

```bash
# 1. Create some tweets with specific content
curl -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello world #testing"}'

curl -X POST http://localhost:8080/2/tweets \
  -H "Authorization: Bearer test" \
  -H "Content-Type: application/json" \
  -d '{"text": "Another hello #testing"}'

# 2. Search for tweets
curl -s "http://localhost:8080/2/tweets/search/recent?query=hello&max_results=10&tweet.fields=id,text,created_at" \
  -H "Authorization: Bearer test" | jq

# 3. Search with time filter
START_TIME=$(date -u -v-1H +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "1 hour ago" +"%Y-%m-%dT%H:%M:%SZ")
curl -s "http://localhost:8080/2/tweets/search/recent?query=hello&start_time=$START_TIME&max_results=10" \
  -H "Authorization: Bearer test" | jq
```

### Integration Examples

#### Python Example

```python
import requests

BASE_URL = "http://localhost:8080"
HEADERS = {
    "Authorization": "Bearer test",
    "Content-Type": "application/json"
}

# Create a tweet
response = requests.post(
    f"{BASE_URL}/2/tweets",
    headers=HEADERS,
    json={"text": "Hello from Python!"}
)
tweet = response.json()["data"]
print(f"Created tweet: {tweet['id']}")

# Get user tweets
response = requests.get(
    f"{BASE_URL}/2/users/0/tweets",
    headers=HEADERS,
    params={"max_results": 10, "tweet.fields": "id,text,created_at"}
)
tweets = response.json()["data"]
print(f"Found {len(tweets)} tweets")
```

#### Node.js Example

```javascript
const fetch = require('node-fetch');

const BASE_URL = 'http://localhost:8080';
const HEADERS = {
  'Authorization': 'Bearer test',
  'Content-Type': 'application/json'
};

// Create a tweet
async function createTweet(text) {
  const response = await fetch(`${BASE_URL}/2/tweets`, {
    method: 'POST',
    headers: HEADERS,
    body: JSON.stringify({ text })
  });
  return response.json();
}

// Get user tweets
async function getUserTweets(userId) {
  const response = await fetch(
    `${BASE_URL}/2/users/${userId}/tweets?max_results=10&tweet.fields=id,text,created_at`,
    { headers: { 'Authorization': 'Bearer test' } }
  );
  return response.json();
}

// Usage
createTweet('Hello from Node.js!').then(data => {
  console.log('Created tweet:', data.data.id);
  return getUserTweets('0');
}).then(data => {
  console.log('User tweets:', data.data);
});
```

---

## Web UI & Data Explorer

The playground includes a comprehensive web interface accessible at `http://localhost:8080/playground` that provides interactive exploration and testing capabilities.

### Data Explorer

The Data Explorer allows you to browse and search all entities in your playground state:

#### Available Entity Types

- **Users**: Browse all users with their profiles, metrics, and details
- **Posts** (formerly Tweets): View all posts with text, author information, and metrics
- **Lists**: Explore lists with members and descriptions
- **Relationships**: View user interactions including:
  - Bookmarks
  - Likes
  - Following/Followers
  - Reposts (formerly Retweets)
  - Muting
  - Blocking
  - List Memberships
  - Followed Lists
  - Pinned Lists
- **Spaces**: Browse spaces and their details
- **Media**: View media attachments
- **DM Conversations**: Explore direct message conversations
- **Stream Rules**: View search stream rules
- **Communities**: Browse communities

#### Search Operators

The Data Explorer supports advanced search operators for filtering relationships and other entities:

**For Relationships:**
- `user:username` or `username:username` - Filter by user (username or name)
- `post:id` or `tweet:id` - Filter by post/tweet ID
- `list:id` - Filter by list ID
- `type:bookmark` - Filter by relationship type (bookmark, like, following, follower, retweet, mute, block, list_member, followed_list, pinned_list)
- `id:value` - Filter by relationship ID

**For Posts/Tweets:**
- `user:username` or `username:username` - Filter by author (username or name)
- `post:id` or `tweet:id` - Filter by post/tweet ID
- `id:value` - Filter by tweet ID

**For Lists:**
- `list:id` - Filter by list ID
- `id:value` - Filter by list ID

**General:**
- `id:value` - Filter by entity ID (works for all entity types)

You can combine multiple operators and add free text search. For example:
- `user:john type:like` - Find all likes by user "john"
- `post:123` - Find relationships involving post ID "123"
- `type:bookmark user:alice` - Find bookmarks by user "alice"

#### Relationship Viewing

The Relationships view displays user interactions in a clear, readable format:
- Each relationship shows the initiating user and the target entity (user, post, or list)
- Relationships are displayed with action verbs (e.g., "User A followed User B", "User A liked Post 123")
- You can filter by relationship type using the dropdown
- Search operators allow precise filtering of relationships

#### Pagination

All entity views support pagination:
- Navigate through results using Previous/Next buttons
- Pagination works correctly with search filters
- Page information shows current page and total pages

---

## Performance & Best Practices

### Performance Tips

1. **Use Field Selection**: Request only needed fields to reduce payload size
2. **Use Expansions**: Reduce API calls by requesting related objects
3. **Use Pagination**: Request reasonable page sizes (10-100 items)
4. **Cache Responses**: Cache responses when appropriate
5. **Batch Requests**: Use batch endpoints when available (e.g., `/2/users?ids=...`)

### Best Practices

1. **Always Check Status Codes**: Don't assume requests succeed
2. **Handle Errors Gracefully**: Parse error responses and handle appropriately
3. **Validate Input**: Validate data before sending requests
4. **Use Appropriate Field Selection**: Request only needed fields
5. **Respect Rate Limits**: Implement rate limit handling in production
6. **Log Requests**: Log requests and responses for debugging
7. **Use HTTPS in Production**: Always use HTTPS for real API calls
8. **Store Credentials Securely**: Never commit API keys to version control

### Rate Limit Handling

```javascript
async function makeRequest(url, options) {
  const response = await fetch(url, options);
  
  if (response.status === 429) {
    // Rate limited - wait for reset
    const resetTime = parseInt(response.headers.get('x-rate-limit-reset'));
    const waitTime = (resetTime * 1000) - Date.now();
    await new Promise(resolve => setTimeout(resolve, waitTime));
    return makeRequest(url, options); // Retry
  }
  
  return response;
}
```

---

## Troubleshooting

### Common Issues and Solutions

#### Server Won't Start

**Problem**: Port already in use

**Solution**:
```bash
# Find process using port 8080
lsof -ti:8080

# Kill the process
kill $(lsof -ti:8080)

# Or use a different port
playground start --port 3000
```

#### OpenAPI Spec Not Loading

**Problem**: Failed to load OpenAPI spec

**Solution**:
```bash
# Refresh the cache
playground refresh

# Or restart with refresh
playground start --refresh
```

#### State Not Persisting

**Problem**: State resets on restart

**Checklist**:
1. Verify persistence is enabled in `~/.playground/config.json`
2. Check file path is writable: `touch ~/.playground/state.json`
3. Check server logs for persistence errors
4. Verify `auto_save` is enabled

**Solution**:
```json
{
  "persistence": {
    "enabled": true,
    "file_path": "~/.playground/state.json",
    "auto_save": true,
    "save_interval": 60
  }
}
```

#### Authentication Errors

**Problem**: Getting `401 Unauthorized`

**Solution**:
1. Add `Authorization` header: `Authorization: Bearer test`
2. Or disable validation in config (testing only):
```json
{
  "auth": {
    "disable_validation": true
  }
}
```

#### Rate Limit Errors

**Problem**: Getting `429 Too Many Requests`

**Solution**:
1. Wait for rate limit window to reset
2. Check rate limit status: `curl http://localhost:8080/rate-limits`
3. Disable rate limiting in config (testing only):
```json
{
  "rate_limit": {
    "enabled": false
  }
}
```

#### Endpoint Not Found

**Problem**: Getting `404 Not Found`

**Checklist**:
1. Verify endpoint path is correct
2. Check server logs for OpenAPI spec loading
3. Refresh OpenAPI spec: `playground refresh`
4. Verify endpoint exists in X API v2

#### State Corruption

**Problem**: State file is corrupted

**Solution**:
```bash
# Reset state
curl -X POST http://localhost:8080/state/reset

# Or delete state file and restart
rm ~/.playground/state.json
playground start
```

#### Validation Errors

**Problem**: Getting `400 Bad Request` with validation errors

**Solution**:
1. Check error response for specific parameter issues
2. Verify request body matches OpenAPI schema
3. Check query parameters are valid
4. Verify field names are correct
5. Check expansion names are valid

#### Slow Performance

**Problem**: Requests are slow

**Solutions**:
1. Use field selection to reduce payload size
2. Reduce `max_results` if requesting large datasets
3. Check server logs for errors
4. Verify OpenAPI cache is not stale
5. Check system resources (CPU, memory)

---

## Summary

The X URL Playground provides a complete local simulation of the X API for testing and development. Key features:

- ✅ **Complete API Coverage**: All X API v2 endpoints supported
- ✅ **Stateful Operations**: Realistic state management
- ✅ **State Persistence**: Optional file-based persistence
- ✅ **Request Validation**: Validates requests like the real API
- ✅ **Error Formatting**: Matches real API error formats
- ✅ **Rate Limiting**: Configurable rate limit simulation
- ✅ **OpenAPI-Driven**: Uses official X API OpenAPI spec

### Quick Reference

**Start Server**: `playground start`  
**Check Status**: `playground status`  
**Refresh Spec**: `playground refresh`  
**Base URL**: `http://localhost:8080`  
**Config File**: `~/.playground/config.json`  
**State File**: `~/.playground/state.json`  
**Repository**: https://github.com/xdevplatform/playground  
**Install Latest**: `go install github.com/xdevplatform/playground/cmd/playground@latest`

### Getting Help

- Check server logs for detailed error messages
- Export state for debugging: `curl http://localhost:8080/state/export`
- Check health: `curl http://localhost:8080/health`
- View endpoints: `curl http://localhost:8080/endpoints`
- Report issues: https://github.com/xdevplatform/playground/issues
- View releases: https://github.com/xdevplatform/playground/releases

Happy testing! 🚀
