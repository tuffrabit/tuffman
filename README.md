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

### 2. Start Watch Mode

For MCP integration, first start watch mode in your project directory:

```bash
# Start watching and indexing
tuffman watch

# Or run in background
tuffman watch &
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

tuffman uses a **watch + client** architecture:

- **Watch** (`tuffman watch`): Runs continuously, indexes files, watches for changes
- **Client** (`tuffman mcp`): Read-only MCP server for queries
- **Database**: SQLite with WAL mode for concurrent access

This design allows:
- Multiple MCP clients to query the same index concurrently
- Long-running indexing without blocking queries
- Clear separation between write (watcher) and read (client) operations

## Commands

### Indexing Commands

| Command | Description |
|---------|-------------|
| `tuffman watch [path]` | Run continuous indexer/watcher |
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

**Important**: Ensure `tuffman watch` is running in your project directory before using MCP tools.

## Workflow

```bash
# Terminal 1: Start watch mode
cd /path/to/project
tuffman watch &

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
