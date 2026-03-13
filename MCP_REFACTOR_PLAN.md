# Tuffman MCP Refactoring Plan

## Overview
Separate tuffman into two distinct operational modes:
1. **Daemon Mode** (background): Handles indexing and filesystem watching
2. **Client Mode** (query): Read-only access to the existing index

## Architecture Changes

### 1. Database Concurrency
- SQLite already supports concurrent reads via WAL mode
- Verify/enforce WAL mode for better read concurrency
- Ensure daemon and clients can access DB simultaneously

### 2. CLI Command Refactoring

#### New Command Structure:
```
tuffman daemon [path]    # Run continuous indexer/watcher (existing watch behavior)
tuffman index [path]     # One-time index (existing - for initial setup)
tuffman status           # Check if daemon is running and index freshness

# Query commands (existing, but enforce read-only):
tuffman map
tuffman symbols <query>
tuffman inspect <symbol_id>
tuffman refs <symbol_id>
tuffman read <file>
tuffman stats

# MCP mode (NEW - client-only):
tuffman mcp [--transport stdio]  # Run MCP server in read-only client mode
```

#### Remove/Deprecate:
- `tuffman server` (replaced by `tuffman daemon` and `tuffman mcp`)

### 3. MCP Server Changes (`internal/mcp/`)

#### Server Modes:
**Current**: `NewServer()` creates indexer + watcher + handler
**New**: Two separate constructors

```go
// DaemonServer - full server with indexing and watching (internal use)
func NewDaemonServer(cfg *ServerConfig) (*Server, error)

// ClientServer - read-only query server for MCP (what users run)
func NewClientServer(cfg *ClientConfig) (*ClientServer, error)
```

#### ClientServer behavior:
- No indexer instance
- No watcher instance
- Only database connection + handlers
- Returns error if DB doesn't exist
- Returns error if index is stale (configurable)

### 4. Configuration Updates

Add to config:
```json
{
  "daemon": {
    "enabled": true,
    "auto_start": false,
    "pid_file": ".tuffman/daemon.pid"
  },
  "mcp": {
    "check_index_freshness": true,
    "max_index_age": "24h"
  }
}
```

### 5. Implementation Steps

#### Phase 1: Database Concurrency
- [ ] Enable WAL mode in SQLite (if not already)
- [ ] Add connection pooling for concurrent reads
- [ ] Test concurrent daemon write + client reads

#### Phase 2: Separate Server Types
- [ ] Create `ClientServer` type in `internal/mcp/client_server.go`
- [ ] Refactor `Server` to `DaemonServer` in `internal/mcp/daemon_server.go`
- [ ] Extract common message handling to shared package
- [ ] Update handlers to work without indexer dependency

#### Phase 3: CLI Restructuring
- [ ] Add `daemon` command (rename from server + watch logic)
- [ ] Add `mcp` command (new client-only MCP server)
- [ ] Update existing query commands to be read-only
- [ ] Add `status` command to check daemon/index health
- [ ] Deprecate `server` command with warning

#### Phase 4: MCP Client Mode Features
- [ ] Add index existence check on startup
- [ ] Add index freshness check (optional)
- [ ] Return helpful error messages:
  - "No index found. Run 'tuffman daemon' or 'tuffman index' first."
  - "Index is 3 days old. Run 'tuffman index' to update."

#### Phase 5: Documentation
- [ ] Update README with new architecture
- [ ] Document daemon vs client modes
- [ ] Update MCP integration guide
- [ ] Add troubleshooting section

### 6. Usage Workflow

#### Setup (per project):
```bash
# Initial index
tuffman index

# Start daemon (in background, tmux, systemd, etc.)
tuffman daemon
```

#### Query (MCP or CLI):
```bash
# MCP mode - lightweight, read-only
tuffman mcp

# Or CLI queries
tuffman symbols "Handler"
tuffman map
```

### 7. Backward Compatibility

- `tuffman server` shows deprecation warning, redirects to `tuffman daemon`
- Existing scripts using `tuffman watch` continue to work (alias to daemon)

### 8. Testing Strategy

- [ ] Test concurrent DB access (daemon writing, client reading)
- [ ] Test client mode without existing index
- [ ] Test client mode with stale index
- [ ] Test daemon restart recovery

## Files to Modify

1. `internal/storage/db.go` - WAL mode, connection pooling
2. `internal/mcp/server.go` - Split into daemon/client servers
3. `internal/mcp/handlers.go` - Remove indexer dependencies
4. `internal/cli/root.go` - New command structure
5. `internal/config/config.go` - New config options
6. `README.md` - Documentation updates

## Future Enhancements (out of scope)

- Socket-based daemon communication for status checks
- Auto-discovery of running daemon
- Index versioning and migration
