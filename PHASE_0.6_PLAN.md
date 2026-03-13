# Phase 0.6: MCP Server Mode

> Hybrid architecture: Standalone CLI + MCP protocol integration

**Version:** 0.1  
**Date:** 2026-03-13  
**Status:** Draft

---

## Overview

Phase 0.6 transforms tuffman from a CLI-only tool into a hybrid: **standalone binary that can also run as an MCP server**. This maintains portability while enabling seamless integration with any MCP-compatible orchestrator (Claude Desktop, Claude Code, Kimi CLI, etc.).

**Core Philosophy:** Be the best codebase intelligence tool — available via CLI for humans, via MCP for agents.

---

## Goals

1. **MCP Protocol Compliance:** Implement Model Context Protocol for tool discovery and execution
2. **Bidirectional Flexibility:** Same functionality via CLI and MCP
3. **Zero Breaking Changes:** All existing commands continue working
4. **Performance:** MCP server keeps index hot (in-memory/watched), eliminating reindexing overhead
5. **Discoverability:** Rich tool descriptions so agents understand when to use each capability

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      tuffman binary                             │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │   CLI Mode   │  │  MCP Server  │  │    Indexer Core      │   │
│  │   (existing) │  │    (new)     │  │  ┌────────┐ ┌─────┐  │   │
│  │              │  │              │  │  │Parsers │ │ DB  │  │   │
│  │  index       │  │  /tools/list │  │  └────────┘ └─────┘  │   │
│  │  watch       │  │  /tools/call │  │  ┌────────┐          │   │
│  │  symbols     │  │  /resources  │  │  │Watcher │          │   │
│  │  inspect     │  │  (optional)  │  │  └────────┘          │   │
│  │  ...         │  │              │  │                      │   │
│  └──────────────┘  └──────────────┘  └──────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Phase 0.6.0: MCP Foundation

**Goal:** Core MCP protocol implementation with stdio transport

**Duration:** 1-2 sessions

### Deliverables

1. **MCP Protocol Types**
   - JSON-RPC 2.0 message structures
   - Tool definition schemas
   - Request/response types per MCP 2024-11-05 spec

2. **Server Lifecycle**
   - `tuffman server` command
   - Stdio transport (primary)
   - HTTP/SSE transport (optional, for remote use)
   - Graceful shutdown handling

3. **Basic Capability**
   - Initialize handshake
   - `tools/list` endpoint
   - `tools/call` endpoint with routing
   - Error handling per MCP spec

### Success Criteria
- [ ] Server starts and responds to MCP initialize
- [ ] `tools/list` returns available tools
- [ ] `tools/call` routes to correct handler
- [ ] Stdio transport works with Claude Desktop

---

## Phase 0.6.1: Tool Implementation

**Goal:** Map all tuffman capabilities to MCP tools

**Duration:** 2-3 sessions

### Tool Inventory

| Tool | Description | Args | Returns |
|------|-------------|------|---------|
| `get_repository_map` | High-level codebase structure | `path`, `depth` | Tree structure |
| `search_symbols` | Find symbols by name pattern | `query`, `kind`, `language` | Symbol list |
| `inspect_symbol` | Detailed symbol info + relationships | `symbol_id` | Symbol details, refs |
| `get_references` | Incoming/outgoing references | `symbol_id`, `direction` | Reference graph |
| `read_source` | Read file contents with line range | `file_path`, `start_line`, `end_line` | Source code |
| `get_index_status` | Index health and coverage | `path` | Stats, last indexed |

### Design Decisions

**1. Arguments as Objects**
```json
{
  "name": "search_symbols",
  "arguments": {
    "query": "Handler",
    "kind": "function",
    "language": "go"
  }
}
```

**2. Structured Results**
```json
{
  "content": [
    {
      "type": "text",
      "text": "Found 3 symbols matching 'Handler'..."
    },
    {
      "type": "json",
      "json": {
        "symbols": [...]
      }
    }
  ]
}
```

**3. Resource Exposure (Optional)**
Consider exposing the index as a read-only resource for MCP clients that support resources:
- `codebase://{project}/symbols/{symbol_id}`
- `codebase://{project}/files/{file_path}`

### Success Criteria
- [ ] All 6 tools return correct results
- [ ] Results include both human-readable text and structured data
- [ ] Error messages are actionable
- [ ] Tool descriptions are clear enough for LLM tool selection

---

## Phase 0.6.2: Hot Index & Watch Mode

**Goal:** MCP server keeps index live and responsive

**Duration:** 1-2 sessions

### Behavior

**On Server Start:**
1. Auto-detect git root from working directory
2. Load or create index at `.tuffman/index.db`
3. Start filesystem watcher (fsnotify)
4. Background incremental reindexing on changes

**On Tool Call:**
- Queries use current index (always fresh)
- No reindexing delay on first query

**Configuration:**
```bash
# Auto-watch current directory
tuffman server

# Watch specific path
tuffman server --path ./src

# Disable auto-watch (query-only mode)
tuffman server --no-watch
```

### Success Criteria
- [ ] Server starts with index ready in < 2 seconds
- [ ] File changes reflected in queries within 1 second
- [ ] Graceful handling of large repositories (100K+ LOC)
- [ ] CPU usage minimal when idle

---

## Phase 0.6.3: JSON Output Mode & CLI Improvements

**Goal:** Make CLI output machine-parseable for scripting

**Duration:** 1 session

### Global Flag
```bash
tuffman symbols "Handler" --format json
tuffman map --format json
tuffman inspect "main.go#main#1" --format json
```

### JSON Schema (per command)

**`symbols --format json`:**
```json
{
  "query": "Handler",
  "count": 3,
  "symbols": [
    {
      "id": "internal/cli/root.go#Execute#16",
      "name": "Execute",
      "kind": "function",
      "language": "go",
      "file": "internal/cli/root.go",
      "line_start": 16,
      "line_end": 44,
      "signature": "func Execute(ctx context.Context) error",
      "receiver": ""
    }
  ]
}
```

**`map --format json`:**
```json
{
  "root": ".",
  "languages": {"go": 12, "python": 5},
  "directories": {...},
  "total_files": 17,
  "total_symbols": 247
}
```

### Success Criteria
- [ ] All commands support `--format json`
- [ ] JSON output is stable (versioned schema)
- [ ] Errors returned as JSON when format is json

---

## Phase 0.6.4: Configuration & Integration

**Goal:** Production-ready configuration and documentation

**Duration:** 1 session

### Configuration File Support

**`.tuffman/config.json`:**
```json
{
  "version": "1",
  "indexer": {
    "exclude_patterns": ["*.log", "node_modules/**", ".git/**"],
    "watch_debounce_ms": 500,
    "auto_index_on_start": true
  },
  "mcp": {
    "transport": "stdio",
    "http_port": 8080,
    "cors_origins": ["http://localhost:3000"]
  },
  "logging": {
    "level": "info",
    "format": "text"
  }
}
```

### Claude Desktop Integration Example

**`claude_desktop_config.json`:**
```json
{
  "mcpServers": {
    "tuffman": {
      "command": "tuffman",
      "args": ["server"],
      "env": {
        "TUFFMAN_PATH": "/path/to/project"
      }
    }
  }
}
```

### Documentation
- README section: "Using with Claude Desktop"
- README section: "Using with Kimi CLI"
- Example: programmatic usage with JSON output

### Success Criteria
- [ ] Config file loaded from `.tuffman/config.json`
- [ ] Claude Desktop integration documented
- [ ] JSON schema documented
- [ ] Example scripts for common workflows

---

## Phase 0.6 Summary

### New Commands

```bash
# MCP server mode (primary addition)
tuffman server [flags]
  --path PATH          # Root path to index
  --transport TYPE     # stdio (default) or http
  --port PORT          # HTTP port (default 8080)
  --no-watch           # Disable auto-watch
  --config PATH        # Config file path

# Enhanced existing commands
tuffman <command> --format json  # Machine-readable output
```

### New Internal Structure

```
internal/
├── cli/                    # Existing CLI commands
├── indexer/                # Existing indexing logic
├── storage/                # Existing database layer
├── watcher/                # Existing file watcher
└── mcp/                    # NEW: MCP server implementation
    ├── server.go           # Server lifecycle, transports
    ├── protocol.go         # MCP JSON-RPC types
    ├── tools.go            # Tool definitions and routing
    ├── handlers.go         # Tool implementation handlers
    └── resources.go        # Optional resource support
```

### Dependencies to Add

```go
// Already have (from Phase 0.5)
// - github.com/mattn/go-sqlite3
// - github.com/fsnotify/fsnotify
// - tree-sitter bindings

// May add for HTTP transport
// - github.com/go-chi/chi/v5 (lightweight router, optional)

// Prefer stdlib for stdio transport
// - encoding/json
// - bufio.Scanner for line-delimited JSON
```

---

## Phase 0.6 Success Criteria

| Criterion | Target | Validation |
|-----------|--------|------------|
| MCP Initialize | < 100ms | Manual test with Claude Desktop |
| Tool Call Latency | < 50ms | Query existing index |
| Index Refresh | < 1s after file change | Edit file, query immediately |
| Memory Footprint | < 200MB for 100K LOC | Profile with pprof |
| Protocol Compliance | Passes MCP inspector | Use MCP inspector tool |

---

## Integration Path (Future)

**Post-Phase 0.6 Options:**

1. **Keep as standalone** (recommended)
   - Distribute via Homebrew, go install
   - Users add to their MCP client config
   - Focus on adding languages, improving accuracy

2. **Add agent subcommand** (optional expansion)
   ```bash
   tuffman agent --model ollama:codellama
   # Uses internal indexer, adds chat loop
   ```

3. **Library mode** (if others want to embed)
   ```go
   import "github.com/tuffrabit/tuffman/pkg/indexer"
   idx := indexer.New(db, config)
   ```

---

## Open Questions

| Question | Default Decision | Notes |
|----------|------------------|-------|
| HTTP transport in 0.6? | Defer to 0.6.1 or later | stdio covers 90% of use cases |
| Resource support? | Optional in 0.6.1 | Nice-to-have, not critical |
| Progress notifications? | No | Complex; can add later |
| Multiple roots? | No | One server per project |
| Prompts support? | No | Out of scope for code oracle |

---

## Appendix: MCP Protocol Reference

**Key MCP Concepts:**

- **Tools:** Functions the LLM can call (our query interface)
- **Resources:** Read-only data URIs (optional: expose files/symbols)
- **Prompts:** Pre-defined templates (out of scope)
- **Transports:** stdio (line-delimited JSON-RPC) or HTTP/SSE

**MCP Lifecycle:**
1. Client starts server process (stdio) or connects (HTTP)
2. Client sends `initialize` request with capabilities
3. Server responds with its capabilities
4. Client sends `initialized` notification
5. Normal operation: `tools/list`, `tools/call`, etc.
6. Cleanup: client closes connection, server exits gracefully
