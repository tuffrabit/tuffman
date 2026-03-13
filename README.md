# tuffman

tuffman is an AI agent orchestrator for codebases. It provides fast symbol indexing, code navigation, and MCP (Model Context Protocol) integration.

## Quick Start

### 1. Build

```bash
# Standard build
CGO_ENABLED=1 go build -o tuffman ./cmd/tuffman

# Optimized build (smaller binary)
CGO_ENABLED=1 go build -ldflags="-s -w" -o tuffman ./cmd/tuffman
```

### 2. Start the Daemon

For MCP integration, first start the daemon in your project directory:

```bash
# Start daemon with file watching
tuffman daemon

# Or run in background
tuffman daemon &

# One-time index without watching
tuffman daemon --no-watch
```

### 3. Use MCP Client

Run the MCP client for editor/IDE integration:

```bash
tuffman mcp
```

### 4. Check Status

```bash
tuffman status
```

## Architecture

tuffman uses a **daemon + client** architecture:

- **Daemon** (`tuffman daemon`): Runs continuously, indexes files, watches for changes
- **Client** (`tuffman mcp`): Read-only MCP server for queries
- **Database**: SQLite with WAL mode for concurrent access

This design allows:
- Multiple MCP clients to query the same index concurrently
- Long-running indexing without blocking queries
- Clear separation between write (daemon) and read (client) operations

## Commands

### Daemon Commands (Indexing)

| Command | Description |
|---------|-------------|
| `tuffman daemon [path]` | Run continuous indexer/watcher |
| `tuffman index [path]` | One-time index |

### Query Commands (Read-Only)

| Command | Description |
|---------|-------------|
| `tuffman mcp` | Run MCP client server (stdio) |
| `tuffman status` | Check index status |
| `tuffman stats` | Show database statistics |
| `tuffman symbols <query>` | Search symbols by name |
| `tuffman map` | Show repository structure |
| `tuffman inspect <symbol_id>` | Show symbol details |
| `tuffman refs <symbol_id>` | Show references |
| `tuffman read <file>` | Read file contents |

## MCP Integration

For MCP clients (Claude Desktop, etc.), configure the **client** command:

```json
{
  "mcpServers": {
    "tuffman": {
      "command": "tuffman",
      "args": ["mcp"],
      "env": {
        "PATH": "/usr/local/bin:/usr/bin"
      }
    }
  }
}
```

**Important**: Ensure `tuffman daemon` is running in your project directory before using MCP tools.

## Workflow

```bash
# Terminal 1: Start daemon
cd /path/to/project
tuffman daemon

# Terminal 2: Query with CLI
tuffman symbols "Handler"
tuffman map

# Or use MCP client for editor integration
tuffman mcp
```

## Configuration

Configuration files are loaded from (in order):
1. `--config <path>` flag
2. `.tuffman/config.json` (project-specific)
3. `~/.config/tuffman/config.json` (user-global)

See `config.example.json` for available options.

## Development

Run tests:
```bash
go test ./...
```

Build with race detection:
```bash
CGO_ENABLED=1 go build -race -o tuffman ./cmd/tuffman
```
