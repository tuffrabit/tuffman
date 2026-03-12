# tuffman Implementation Plan

> From indexer validation to full orchestrator

**Version:** 0.1  
**Date:** 2026-03-09  
**Status:** Draft

---

## Overview

This plan implements tuffman in phases, starting with the riskiest component first: the codebase indexer. By validating the indexer architecture early (Phase 0.5), we de-risk the entire project. If we can't efficiently index and query codebases with deterministic accuracy, the orchestrator built on top will be compromised.

---

## Phase 0.5: Codebase Indexer Test Implementation

**Goal:** Validate the core indexing architecture works across multiple languages and real-world codebases.

**Duration:** 2-3 weeks

---

### Phase 0.5.0: Foundation & Skeleton

**Goal:** Core project structure and storage layer.

**Duration:** 1 session

**Deliverables:**
1. **Project Structure** — Phase 1 layout with `internal/`, `cmd/`, `pkg/` directories
2. **SQLite Storage Layer** — Database connection, migrations, basic CRUD operations
3. **Schema Implementation** — `files` and `symbols` tables (references table deferred)
4. **Basic CLI Framework** — `index` and `stats` commands only

**Success Criteria:**
- [ ] Database creates and migrates on first run
- [ ] Can insert and query file records
- [ ] Can insert and query symbol records
- [ ] `tuffman stats` returns accurate counts

---

### Phase 0.5.1: Go Parser & Indexing

**Goal:** Single-language parsing with tree-sitter.

**Duration:** 1-2 sessions

**Deliverables:**
1. **Tree-sitter Integration** — CGO bindings, Go parser loading
2. **AST Walker** — Extract functions, methods, structs, interfaces from Go code
3. **File Scanner** — Walk directory, filter by extension, detect Git root
4. **Batch Indexing** — `tuffman index [path]` command working end-to-end

**Success Criteria:**
- [ ] Indexes tuffman itself correctly
- [ ] Extracts function names, signatures, line numbers
- [ ] Handles malformed Go code gracefully
- [ ] Can query symbols by name with `tuffman symbols <query>`

---

### Phase 0.5.2: Incremental Updates & Watching

**Goal:** File system watching and incremental re-indexing.

**Duration:** 1-2 sessions

**Deliverables:**
1. **fsnotify Integration** — Watch source directories for changes
2. **Debounced Re-indexing** — 500ms delay, cancel in-flight operations
3. **Incremental Updates** — Delete old symbols for changed files, re-parse, insert new
4. **Git Branch Detection** — Monitor `.git/HEAD` for branch switches
5. **`tuffman watch` Command** — Continuous indexing mode

**Success Criteria:**
- [ ] File change reflected in index within 1 second of save
- [ ] Git branch switch triggers re-index of changed files
- [ ] Graceful shutdown on interrupt (Ctrl-C)

---

### Phase 0.5.3: Multi-Language Support

**Goal:** Python and JavaScript/TypeScript parsing.

**Duration:** 2-3 sessions

**Deliverables:**
1. **Python Parser** — tree-sitter-python integration, AST walker
2. **JavaScript/TypeScript Parser** — tree-sitter-javascript, tree-sitter-typescript
3. **Language Detection** — File extension and content-based detection
4. **Unified Symbol Extraction** — Common interface across languages

**Success Criteria:**
- [ ] Index a 50K LOC Go codebase in < 10 seconds
- [ ] Index a 50K LOC Python codebase in < 10 seconds
- [ ] Index a 50K LOC JS/TS codebase in < 10 seconds
- [ ] Symbol coverage > 80% for standard code patterns in all three languages

---

### Phase 0.5.4: Query Interface & Reference Tracking

**Goal:** Complete CLI toolset and basic reference resolution.

**Duration:** 2 sessions

**Deliverables:**
1. **`tuffman map`** — Display repository structure
2. **`tuffman inspect <symbol_id>`** — Show symbol details
3. **`tuffman refs`** — Show incoming/outgoing references (basic heuristic matching)
4. **`references` Table** — Store call expressions and imports
5. **Call Chain Tracing** — Basic name-based resolution

**Success Criteria:**
- [ ] Query latency for `symbols` and `inspect` < 50ms
- [ ] Can trace call chains 3+ levels deep (heuristic-based)
- [ ] All Phase 0.5 CLI commands functional

---

### Phase 0.5 Summary

**Core Architecture:**

```
┌─────────────────────────────────────────────────────────────┐
│                    tuffman (Go binary)                      │
│  ┌─────────────┐  ┌─────────────────────────────────────┐   │
│  │  CLI/Tool   │  │        Indexer Goroutine            │   │
│  │   API Layer │  │  ┌──────────┐  ┌─────────────────┐  │   │
│  │             │  │  │  FS Watcher    │  │ Tree-sitter     │  │   │
│  │  SQLite     │◄─┼──┤  (fsnotify)    │  │ Parsers (Go,    │  │   │
│  │  Queries    │  │  │                │  │ Py, JS/TS...)   │  │   │
│  └─────────────┘  │  └──────────┘  └─────────────────┘  │   │
│                   │              │                        │   │
│                   │         ┌────▼────┐                   │   │
│                   │         │ SQLite  │                   │   │
│                   │         │ index.db│                   │   │
│                   │         └─────────┘                   │   │
│                   └─────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

**Deliverables:**

1. **Background Indexer Service**
   - Goroutine that runs on `tuffman index` or `tuffman watch`
   - File system watching via `fsnotify`
   - Debounced re-indexing on file changes (500ms delay)
   - Git branch change detection (via `.git/HEAD` monitoring)
   - Graceful shutdown on interrupt

2. **Multi-Language Parsing**
   - Tree-sitter integration via `github.com/tree-sitter/go-tree-sitter`
   - Go parser (using tree-sitter-go)
   - Python parser (using tree-sitter-python)
   - JavaScript/TypeScript parser (using tree-sitter-javascript, tree-sitter-typescript)
   - Manual AST walkers extracting:
     - Function/method declarations (name, signature, receiver, line)
     - Class/struct/interface declarations
     - Call expressions (for reference tracking)

3. **SQLite Storage Layer**
   - Database: `.tuffman/index.db`
   - Library: `github.com/mattn/go-sqlite3` (CGO already required)
   - WAL mode for concurrent reads
   - Schema:
     ```sql
     CREATE TABLE files (
         id TEXT PRIMARY KEY,        -- relative path
         absolute_path TEXT NOT NULL,
         language TEXT,
         size_bytes INTEGER,
         mtime INTEGER,              -- modification time
         indexed_at INTEGER,
         git_sha TEXT                -- for branch change detection
     );

     CREATE TABLE symbols (
         id TEXT PRIMARY KEY,        -- file_path#name#line
         file_id TEXT NOT NULL,
         language TEXT,
         kind TEXT,                  -- function, method, class, struct, interface
         name TEXT NOT NULL,
         signature TEXT,             -- parameters, return type
         doc TEXT,                   -- docstring/comment
         line_start INTEGER,
         line_end INTEGER,
         receiver TEXT,              -- for methods
         FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
     );

     CREATE INDEX idx_symbols_name ON symbols(name);
     CREATE INDEX idx_symbols_file ON symbols(file_id);
     CREATE INDEX idx_symbols_kind ON symbols(kind);

     CREATE TABLE references (
         id INTEGER PRIMARY KEY AUTOINCREMENT,
         source_id TEXT NOT NULL,    -- symbol doing the referencing
         target_name TEXT NOT NULL,  -- unresolved target name
         target_id TEXT,             -- resolved symbol id (nullable)
         kind TEXT,                  -- call, import, inherit
         line INTEGER,
         FOREIGN KEY (source_id) REFERENCES symbols(id) ON DELETE CASCADE,
         FOREIGN KEY (target_id) REFERENCES symbols(id) ON DELETE SET NULL
     );

     CREATE INDEX idx_refs_source ON references(source_id);
     CREATE INDEX idx_refs_target_name ON references(target_name);
     CREATE INDEX idx_refs_target_id ON references(target_id);
     ```

4. **Query Interface (CLI Tools)**
   - `tuffman index [path]` — One-time indexing
   - `tuffman watch [path]` — Continuous indexing with file watching
   - `tuffman map [--depth N]` — Display repository map
   - `tuffman symbols <query> [--kind function]` — Search symbols
   - `tuffman inspect <symbol_id>` — Show symbol details + relationships
   - `tuffman refs <symbol_id> [--direction in|out]` — Show references
   - `tuffman stats` — Index statistics (files, symbols, coverage)

5. **Incremental Update Strategy**
   - On file change: delete old symbols/refs for that file, re-parse, insert new
   - On git branch change: compare file MTIMES, re-index changed files
   - SQLite transaction per file for atomicity
   - Batch deletes/inserts for performance

**Technical Decisions to Resolve:**

| Question | Approach to Validate |
|----------|---------------------|
| Call resolution | Heuristic: match by name within same file first, then global. Log unresolved refs for analysis. |
| Multi-language files | Treat as separate regions (e.g., Vue SFC → template region + script region). Start with script only. |
| Index invalidation | File watcher primary, manual `index --force` as escape hatch. Git branch detection as secondary trigger. |
| Symbol signatures | Include type annotations when available (Go, TS), parameter names always. Review token count impact. |
| Tree-sitter binding | Validate `go-tree-sitter` handles malformed code gracefully and performance meets targets. |

**Phase 0.5 Success Criteria (Cumulative):**

| Criterion | Target Phase | Metric |
|-----------|--------------|--------|
| Database & Schema | 0.5.0 | Functional migrations, < 50ms query latency |
| Go Indexing | 0.5.1 | Index tuffman in < 5 seconds, 80% symbol coverage |
| File Watching | 0.5.2 | < 1 second reflection time, graceful shutdown |
| Multi-Language | 0.5.3 | 50K LOC in < 10 seconds per language |
| Query Interface | 0.5.4 | < 50ms query latency, 3-level call chains |

**Test Codebases:**

1. **Go:** tuffman itself (dogfooding)
2. **Python:** A medium Django/Flask project
3. **JS/TS:** A Vue.js or React project

**Phase Gate:** 
If the indexer cannot meet success criteria or the architecture proves unworkable, we back up and re-evaluate tree-sitter approach before proceeding.

**Integration Path:**
Upon completion of Phase 0.5.4, the indexer will be refactored into:
- `internal/indexer/` — Core indexing engine (library)
- `internal/tools/search_symbols.go` — Agent tool implementation
- `internal/tools/read_file.go` — File content tool (uses index for validation)

---

## Phase 1: Foundation (CLI Skeleton)

**Goal:** Build the minimal CLI framework that will house the orchestrator.

**Duration:** 1 week

**Deliverables:**

1. **Project Structure**
   ```
   tuffman/
   ├── cmd/
   │   └── tuffman/
   │       └── main.go
   ├── internal/
   │   ├── cli/           # Command definitions (cobra or similar)
   │   ├── config/        # Config loading and validation
   │   ├── logging/       # Structured logging (slog)
   │   └── indexer/       # Phase 0.5 indexer (moved here)
   ├── pkg/
   │   └── api/           # Public API (if any)
   └── go.mod
   ```

2. **CLI Framework**
   - Use `spf13/cobra` or `urfave/cli`
   - Global flags: `--config`, `--verbose`, `--project`
   - Subcommands: `init`, `config`, `index`, `watch`, `map`, `symbols`, `inspect`
   - Help text with examples

3. **Configuration System**
   - JSON-based (as per PRD)
   - Layering: system → user → project
   - Environment variable interpolation: `"${OPENAI_API_KEY}"`
   - Schema validation on load
   - Default config created on `tuffman init`
   - Example `.tuffman/config.json`:
     ```json
     {
       "version": "1",
       "providers": {},
       "indexer": {
         "exclude_patterns": ["*.log", "node_modules/**", ".git/**"],
         "watch_debounce_ms": 500
       },
       "logging": {
         "level": "info",
         "format": "text"
       }
     }
     ```

4. **Logging**
   - Structured logging via `log/slog`
   - Levels: debug, info, warn, error
   - Text format for terminal, JSON for piping
   - Debug flag enables verbose indexer logging

5. **Project Initialization**
   - `tuffman init` creates `.tuffman/` directory
   - Creates default `config.json`
   - Creates empty `index.db` and `conversations.db`

**Success Criteria:**

- [ ] Binary builds and runs on macOS and Linux
- [ ] `tuffman init` creates proper project structure
- [ ] Config layering works correctly
- [ ] All Phase 0.5 commands work through new CLI
- [ ] Debug logging shows indexer internals

---

## Phase 2: Provider Client & Basic Chat

**Goal:** Add LLM provider support and a basic conversation loop.

**Duration:** 1-2 weeks

**Deliverables:**

1. **Provider Interface**
   ```go
   type Provider interface {
       Name() string
       Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
       StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
       ProbeCapabilities(ctx context.Context) (Capabilities, error)
   }
   ```

2. **Provider Implementations**
   - OpenAI-compatible (covers OpenAI, LM Studio, Ollama)
   - Configuration: base URL, API key, default model
   - Capability probing: context length, tool support, JSON mode

3. **Basic Conversation Loop**
   - `tuffman chat` starts interactive REPL
   - System prompt support
   - Message history in memory only (no persistence yet)
   - Streaming responses to terminal
   - Token usage display

4. **Simple Tool Support**
   - Tool interface and registry
   - Two built-in tools:
     - `read_file` — Read file contents
     - `search_symbols` — Query the codebase index
   - Basic tool execution loop

**Success Criteria:**

- [ ] Can chat with local Ollama instance
- [ ] Can chat with OpenAI API
- [ ] Tool calls work (file read, symbol search)
- [ ] Streaming responses display correctly
- [ ] Token usage shown after each response

---

## Phase 3: Context Engine

**Goal:** Implement intelligent context management for limited windows.

**Duration:** 2 weeks

**Deliverables:**

1. **Token Accounting**
   - Tiktoken for OpenAI models
   - Estimation fallback for other providers
   - Real-time token counting on every operation
   - Hard/soft limit enforcement

2. **Context Tiers**
   - **Hot**: Active conversation messages
   - **Warm**: Summarized older conversation
   - **Cold**: Searchable via tool calls (index already exists)

3. **Sliding Window**
   - Preserve system prompt and critical tool schemas
   - Evict oldest messages when approaching limit
   - Summarize evicted content into warm context

4. **Conversation Persistence**
   - SQLite storage: `.tuffman/conversations.db`
   - Save/resume conversation state
   - `tuffman resume` continues last session
   - `@checkpoint` command in chat

**Success Criteria:**

- [ ] 32K context feels usable with 8K worth of active conversation
- [ ] Never exceed context limit (hard enforcement)
- [ ] Can resume interrupted conversation
- [ ] Token budget display: `3,247 / 8,192 tokens (40%)`

---

## Phase 4: Tool Discovery System

**Goal:** Implement lazy tool loading to minimize context bloat.

**Duration:** 1 week

**Deliverables:**

1. **Discovery Tools**
   - `tool_search` — Find tools by keyword/category
   - `tool_describe` — Get full schema for specific tools

2. **Tool Registry**
   - Register tools with metadata (name, category, description)
   - Load only discovery tools into initial context
   - On-demand schema loading when `tool_describe` called

3. **Expanded Tool Set**
   - `file_write`, `file_append`
   - `shell_exec` (with timeout and allowlist)
   - `search_grep` (text search)
   - `web_fetch`

**Success Criteria:**

- [ ] Initial context contains only 2 discovery tools regardless of total tool count
- [ ] Model can discover and load tools as needed
- [ ] 50 tools don't blow up context window

---

## Phase 5: Agent Orchestration

**Goal:** Multi-agent spawning and coordination.

**Duration:** 2 weeks

**Deliverables:**

1. **Agent Registry**
   - Agent templates in config (system prompts, tool sets)
   - Built-in agents: coder, reviewer, explorer, tester

2. **Agent Spawning**
   - Parent agent spawns child agents for subtasks
   - Context inheritance (relevant slices, not everything)
   - Agent lifecycle management

3. **Parallel Execution**
   - Fan-out: Spawn multiple agents for independent tasks
   - Fan-in: Collect and aggregate results
   - Timeout and cancellation propagation

4. **Structured Handoffs**
   - Protocol for agent-to-agent communication
   - Result formatting for parent consumption
   - Progress reporting from children

**Success Criteria:**

- [ ] Can spawn specialized agents for subtasks
- [ ] Parallel agents execute concurrently
- [ ] Results properly aggregated
- [ ] Parent maintains oversight of children

---

## Phase 6: Hardening & Polish

**Goal:** Production readiness and user experience.

**Duration:** 2 weeks

**Deliverables:**

1. **Indexer Hardening**
   - SCM file loader for custom extractors
   - Additional language support (Rust, Java)
   - Performance profiling and optimization
   - Coverage gap analysis and fixes

2. **Configuration Wizard**
   - `tuffman setup` interactive config
   - Provider testing (verify API keys work)
   - Model capability auto-detection

3. **Error Handling**
   - Graceful degradation when providers fail
   - Retry logic with exponential backoff
   - Clear error messages with suggestions

4. **Documentation**
   - README with quickstart
   - Agent template cookbook
   - Architecture decision records (ADRs)

**Success Criteria:**

- [ ] New user can go from zero to working in < 5 minutes
- [ ] All error cases handled gracefully
- [ ] Documentation complete and accurate

---

## Future Phases (P2)

- **TUI Mode:** Rich terminal interface with panels
- **Self-Modification:** Agents can improve tuffman's codebase
- **Voice Interface:** Local STT/TTS
- **MCP Integration:** Full Model Context Protocol support

---

## Appendix: Dependencies

### Phase 0.5
- `github.com/tree-sitter/go-tree-sitter`
- `github.com/tree-sitter/tree-sitter-go`
- `github.com/tree-sitter/tree-sitter-python`
- `github.com/tree-sitter/tree-sitter-javascript`
- `github.com/tree-sitter/tree-sitter-typescript`
- `github.com/mattn/go-sqlite3`
- `github.com/fsnotify/fsnotify`
- `github.com/spf13/cobra` (or alternative)

### Phase 2+
- `github.com/pkoukk/tiktoken-go` (token counting)
- Standard library: `log/slog`, `net/http`, `encoding/json`, etc.

---

## Appendix: Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Tree-sitter performance unacceptable | Spike early in Phase 0.5; fallback to regex-based parsing for MVP |
| CGO cross-compilation issues | Limit to Linux/macOS initially; document Windows limitations |
| Index too large for SQLite | Implement pruning strategies; measure early and often |
| Local LLM too slow for tool loop | Add aggressive caching; allow user to set timeout thresholds |
| Multi-agent coordination too complex | Start with sequential spawning; add parallelism in later iteration |
