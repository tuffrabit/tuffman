# tuffman PRD

> AI Agent Orchestrator — Built Local-First

**Version:** 0.2  
**Date:** 2026-03-07  
**Status:** Draft — Ready for Implementation Planning

---

## 1. Vision

tuffman is a CLI-first AI agent orchestrator optimized for local LLM inference. It manages multiple AI agents across programming tasks while respecting hardware constraints — turning limited context windows and VRAM into a design constraint, not a limitation.

**Core Philosophy:** Context is currency. Every token is precious.

---

## 2. Goals

### 2.1 Primary Goals (P0)

| Goal | Description |
|------|-------------|
| Local-LLM Optimization | First-class support for LM Studio, Ollama, and other local providers. Graceful degradation, not afterthought. |
| Context Management | Intelligent token budgeting, compression, and windowing. Never hit context limits unexpectedly. |
| Multi-API Support | Unified interface for OpenAI, Anthropic, and local OpenAI-compatible endpoints. |
| Tool Calling | Extensible tool system with built-in essentials + MCP support. |

### 2.2 Secondary Goals (P1)

| Goal | Description |
|------|-------------|
| Multi-Agent Orchestration | Spawn specialized sub-agents, parallel execution, structured handoffs. |
| Codebase Intelligence | Lazy-loaded indexing with incremental updates. No "file too large" crashes. |
| Semi-Formal Reasoning | Structured agent thinking — plans, reflections, chain-of-thought. |

### 2.3 Future Goals (P2)

| Goal | Description |
|------|-------------|
| TUI Mode | Rich terminal interface with panels for context, agents, files. |
| Self-Modification | Agents capable of improving tuffman's own codebase. |
| Voice Interface | Local STT/TTS integration. |

---

## 3. Architecture Principles

1. **Local-First** — Design for 8GB-16GB VRAM, slow inference, and context windows under 128K.
2. **Lazy Over Eager** — Don't load, index, or transmit what isn't needed.
3. **Graceful Degradation** — Partial results beat total failure. Fallback chains are explicit.
4. **Explicit Over Implicit** — User controls model routing, context strategy, token budgets.
5. **Small & Inspectable** — SQLite and JSON configs can be opened, queried, version-controlled.
6. **Discovery Over Enumeration** — Tool schemas are large; expose a discovery layer so models pull only what they need.

---

## 4. Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Single binary, fast, excellent stdlib |
| Config | JSON | Simple, ubiquitous, easy to generate/parse |
| Storage | SQLite | Portable, tool-wrappable, zero-config |
| Protocol | OpenAI-compatible | De facto standard; covers most local tools |

---

## 5. Core Modules

### 5.1 Context Engine

The heart of tuffman. Manages the precious resource of limited context windows.

**Features:**
- Real-time token accounting with configurable hard/soft limits
- Sliding window with smart eviction (preserve system prompts, tool schemas)
- Auto-compression of stale conversation history
- Hierarchical context tiers:
  - **Hot** — In-context, directly sent to model
  - **Warm** — Summarized, condensed representation
  - **Cold** — Indexed, searchable on demand

### 5.2 Model Router

Abstracts provider differences and optimizes for local constraints.

**Features:**
- Capability probing (context length, tool support, JSON mode, system prompt handling)
- Task-to-model routing rules (simple → small/fast, complex → large/slow)
- Quantization-aware selection
- Fallback chains with user-defined policies
- VRAM budgeting when running multiple local models

### 5.3 Agent Orchestrator

Manages the lifecycle and interactions of multiple agents.

**Features:**
- Agent registry with capability declarations
- Parent-child spawning with context inheritance (relevant slices, not everything)
- Parallel fan-out for independent subtasks
- Structured handoff protocol between agents
- Agent specialization: coder, reviewer, architect, tester, explorer

### 5.4 Codebase Indexer

Intelligent exploration without overwhelming context.

**Features:**
- Multi-level index stored in SQLite:
  - File level: path, type, size, modification time
  - Symbol level: functions, classes, imports (via tree-sitter)
  - Semantic level: embeddings for similarity search (optional, local)
- Lazy loading: index first, fetch content on demand
- Incremental updates: watch filesystem, update only changed files
- Query interface: path glob, symbol search, semantic similarity

### 5.5 Tool System

Extensible capabilities for agents. Designed to minimize context bloat through lazy loading.

**Discovery-First Architecture:**

Instead of loading all tool schemas into context upfront, the system exposes a single discovery tool. The LLM queries this to find tools relevant to its current task, loading only what it needs.

**Discovery Tool:**
- `tool_search` — Search available tools by keyword, category, or capability
  - Query: `"file operations"` → Returns `file_read`, `file_write`, `file_append`
  - Query: `"git"` → Returns git-related tools (if installed)
  - Query: `"list all"` → Returns categorized summary (names only, no schemas)
- `tool_describe` — Fetch full schema for specific tools by name
  - Called after discovery to load schemas on-demand
  - Returns: parameters, return type, examples, error cases

**Benefits:**
- 50 tools with rich schemas → ~1 discovery tool in context initially
- Schemas loaded only when the model explicitly needs them
- Natural fit for local LLMs with limited context windows

**Built-in Tools (Loaded on Demand):**
- `file_read` — Read file contents with line ranges
- `file_write` — Write or append to files
- `shell_exec` — Execute shell commands with timeouts
- `search_grep` — Text search across codebase
- `search_semantic` — Embedding-based similarity search
- `web_fetch` — Fetch and extract web content

**MCP Integration:**
- Client for Model Context Protocol servers
- MCP tools registered in the same discovery index
- Result caching to avoid redundant calls

### 5.6 Configuration System

**Layers (merge, don't replace):**
1. System: `/etc/tuffman/config.json` (Linux), `/Library/tuffman/config.json` (macOS)
2. User: `~/.config/tuffman/config.json`
3. Project: `.tuffman/config.json`

**Features:**
- JSON schema validation
- Environment variable interpolation: `"api_key": "${OPENAI_API_KEY}"`
- Provider definitions with capabilities
- Agent templates with system prompts and tool sets
- Token budget defaults

### 5.7 Storage Layer

**SQLite Databases:**

| Database | Purpose | Location |
|----------|---------|----------|
| `config.db` | Settings, provider configs, agent templates | User config dir |
| `index.db` | Codebase index per project | `.tuffman/index.db` |
| `conversations.db` | Conversation history, context archives | `.tuffman/conversations.db` |

**Practices:**
- Schema migrations from day one
- WAL mode for concurrent reads
- Separate databases for independent concerns

---

## 6. Project Structure

```
project/
├── .tuffman/
│   ├── config.json          # Project-specific settings
│   ├── index.db             # Codebase index
│   ├── conversations.db     # Session history
│   └── cache/               # Tool result cache, embeddings
└── [source files...]
```

---

## 7. User Experience

### 7.1 CLI Interface

```bash
# Initialize tuffman in a project
tuffman init

# Start interactive session
tuffman chat

# Run with specific agent
tuffman chat --agent code-reviewer

# One-shot command
tuffman run "refactor the auth module" --agent architect
```

### 7.2 Streaming & Progress

- All responses stream to terminal (essential for local LLM latency)
- Progress indicators show: current agent, action, token usage
- Token budget display: `context: 3,247 / 8,192 tokens (40%)`

### 7.3 Interrupt & Resume

- Ctrl-C saves conversation state gracefully
- `tuffman resume` to continue interrupted session
- Explicit checkpointing: `@checkpoint` in chat

---

## 8. Development Stages

### Stage 1: Foundation (MVP)

Build the basic building blocks:

1. **Project skeleton** — Go module, CLI framework, logging
2. **Configuration** — JSON loading, validation, layering
3. **Storage** — SQLite setup, migrations, basic CRUD
4. **Provider clients** — Unified interface, OpenAI and Ollama implementations
5. **Tool registry** — Registration, discovery, execution framework
6. **Tool discovery system** — `tool_search` and `tool_describe` for lazy schema loading
7. **Basic conversation loop** — System prompt + messages + tool results

**Deliverable:** Working chat with tools against local Ollama.

### Stage 2: Context Intelligence

Add the local-LLM optimization features:

1. Token counting and budgeting
2. Sliding window with eviction
3. Conversation summarization
4. Hierarchical context (hot/warm/cold)

**Deliverable:** 32K context feels like 128K through smart management.

### Stage 3: Agent Orchestration

Multi-agent capabilities:

1. Agent registry and spawning
2. Context inheritance between agents
3. Parallel sub-agent execution
4. Structured handoff protocol

**Deliverable:** Complex tasks split across specialized agents automatically.

### Stage 4: Codebase Intelligence

Deep codebase understanding:

1. File-level and symbol-level indexing
2. Incremental updates
3. Semantic search with local embeddings
4. Integration with agent context

**Deliverable:** Agents explore large codebases without token exhaustion.

### Stage 5: Polish

User experience refinements:

1. TUI mode (optional)
2. Configuration wizard
3. Performance profiling
4. Documentation and examples

---

## 9. Open Questions

| Question | Status | Notes |
|----------|--------|-------|
| Token counting strategy? | Open | tiktoken for OpenAI, estimate for others, or count server-side? |
| Embedding provider? | Open | Local (all-MiniLM), or pluggable? |
| Tree-sitter integration? | Open | Native Go binding or WASM? |
| Agent communication format? | Open | XML tags, JSON blocks, or custom protocol? |

---

## 10. Brand & Vibe

**Name:** tuffman — tough enough to run local, smart enough to manage context.

**Mascot:** TBD. Ideas: scrappy mechanic, resourceful wilderness guide, minimalist architect.

**Tone:** Capable, resourceful, no-nonsense. tuffman respects your hardware and your time.

---

## Appendix A: Glossary

| Term | Definition |
|------|------------|
| Context Window | Maximum tokens a model can process in one request |
| MCP | Model Context Protocol — standard for tool servers |
| Hot/Warm/Cold | Tiers of context accessibility and detail |
| Fan-out/Fan-in | Spawning parallel tasks, then aggregating results |
| WAL | Write-Ahead Logging — SQLite mode for concurrency |
| Tool Discovery | Pattern where models query for tools instead of receiving all schemas upfront |
| Lazy Loading | Deferring loading of resources until they are explicitly needed |
