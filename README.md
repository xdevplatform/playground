# X API Playground

A standalone local HTTP server that simulates the X (Twitter) API v2 for testing and development without consuming API credits.

## Features

- Complete API compatibility with X API v2 endpoints
- Stateful operations with in-memory state management
- Optional file-based state persistence across server restarts
- Interactive web UI for exploring and testing endpoints
- Request validation against OpenAPI specifications
- Error responses matching real API formats
- Configurable rate limiting simulation
- Server-Sent Events (SSE) streaming support
- CORS support for web applications
- OpenAPI-driven endpoint discovery and handling

## Prerequisites

- **Go 1.21 or later** - [Download Go](https://go.dev/dl/)

## Installation

### Option 1: Install from Source (Recommended)

```bash
# Clone the repository
git clone https://github.com/xdevplatform/playground.git
cd playground

# Build the binary
go build -o playground ./cmd/playground

# Make it executable (optional)
chmod +x playground

# Move to PATH (optional, for global access)
sudo mv playground /usr/local/bin/
```

### Option 2: Install via go install

```bash
go install github.com/xdevplatform/playground/cmd/playground@latest
```

This will install the `playground` binary to `$GOPATH/bin` (or `$HOME/go/bin` by default).

Make sure `$GOPATH/bin` is in your `PATH`:
```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### Option 3: Download Pre-built Binary

Check the [Releases](https://github.com/xdevplatform/playground/releases) page for pre-built binaries for your platform.

### Verify Installation

```bash
playground --help
```

You should see the playground command help output.

## Quick Start

1. **Start the server:**
   ```bash
   playground start
   ```

2. **Access the Web UI:**
   Open http://localhost:8080/playground in your browser

3. **Make API requests:**
   ```bash
   curl -H "Authorization: Bearer test_token" http://localhost:8080/2/users/me
   ```

## Usage

### Commands

- `playground start` - Start the playground server
  - Flags: 
    - `--port` / `-p` (default: 8080) - Port to run the server on
    - `--host` (default: localhost) - Host to bind the server to
    - `--refresh` - Force refresh of OpenAPI spec cache
- `playground status` - Check if a server is running
- `playground refresh` - Refresh OpenAPI spec cache

### Examples

Start on custom port:
```bash
playground start --port 3000 --host 0.0.0.0
```

Start with OpenAPI cache refresh:
```bash
playground start --refresh
```

### Configuration

Configuration files are stored in `~/.playground/`:
- `config.json` - Playground configuration (optional, uses embedded defaults if not present)
- `state.json` - Persistent state (if persistence enabled)
- `examples/` - Custom example responses (optional)
- `.playground-openapi-cache.json` - Cached OpenAPI spec

### Management Endpoints

The playground provides several management endpoints (not part of X API):

- `GET /health` - Server health and statistics
- `GET /rate-limits` - Rate limit configuration and status
- `GET /config` - View current configuration
- `PUT /config/update` - Update configuration (temporary, lost on restart)
- `POST /config/save` - Save configuration to file
- `POST /state/reset` - Reset state to initial seed data
- `GET /state/export` - Export state as JSON
- `POST /state/import` - Import state from JSON
- `POST /state/save` - Manually save state (if persistence enabled)
- `DELETE /state` - Delete all state
- `GET /endpoints` - List all available endpoints

## Documentation

For detailed documentation, see [DOCUMENTATION.md](DOCUMENTATION.md).

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT

## Links

- **GitHub**: https://github.com/xdevplatform/playground
- **Issues**: https://github.com/xdevplatform/playground/issues

