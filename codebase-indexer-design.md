# Codebase Indexer Design Document

## Problem Statement

RAG (Retrieval-Augmented Generation) fails for code exploration because it treats code as documents to be matched by similarity. Code exploration is fundamentally **graph traversal**: "I'm at `authMiddleware`, where is it used? → `apiRouter` → what routes does that register?" 

This is particularly problematic for hardware-constrained local LLMs with small context windows. We need a tool that enables efficient (context-efficient and speed-efficient) codebase exploration with deterministic results.

## Core Concept: The Code Oracle

A stateless, query-optimized indexing layer that speaks in relationships rather than text similarity. It provides progressive disclosure of codebase structure, allowing an AI agent to navigate without loading entire files into context.

## Architecture

### 1. Indexing Pipeline

```
Source Files → Tree-sitter Parser → AST Traversal → SQLite Database
```

**Parser Layer**: Use `github.com/tree-sitter/go-tree-sitter` with language-specific bindings. Parse source files into ASTs without full compilation (fast, handles malformed code).

**Extraction Layer**: Manual AST traversal to extract:
- **Symbols**: functions, methods, classes, structs, interfaces, static/global variables
- **References**: call sites, imports, inheritance relationships

**Storage Layer**: SQLite with two core tables:

```sql
-- Symbols (the "what")
CREATE TABLE symbols (
    id TEXT PRIMARY KEY,        -- uri#name#line
    uri TEXT NOT NULL,
    language TEXT,
    kind TEXT,                  -- function, method, class, struct, interface, variable
    name TEXT NOT NULL,
    signature TEXT,             -- parameters, return type
    doc TEXT,                   -- docstring/comment
    line INTEGER,
    receiver TEXT               -- for methods (e.g., "*User")
);

-- References (the "how they connect")
CREATE TABLE references (
    id INTEGER PRIMARY KEY,
    source_id TEXT,             -- who is referencing
    target_name TEXT,           -- unresolved call target
    target_id TEXT,             -- resolved when possible
    kind TEXT,                  -- call, import, inherit, implement
    line INTEGER
);
```

### 2. Query Interface (MCP-Style)

Tools designed for progressive disclosure:

| Tool | Returns | Use Case |
|------|---------|----------|
| `get_repository_map(depth)` | Condensed tree: directories → files → top-level symbols | 10kft view, plan exploration |
| `search_symbols(query, kind?)` | Matching symbols with location info | Find entry points |
| `inspect_symbol(id)` | Definition + immediate relationships (calls X, called by Y) | Build mental model |
| `get_neighbors(id, direction, type?)` | Connected symbols via references | Graph traversal |
| `read_source(uri, range?)` | Actual source code | Deep dive when needed |

**Key insight**: Return signatures and relationships (~100 tokens), not full source (~500+ tokens).

### 3. Extraction Strategy: Start Simple

**Phase 1: Manual Traversal (70-80% coverage)**

Hardcoded AST walkers for each language, looking for ~5 node types:
- Function declarations
- Method declarations  
- Class/struct declarations
- Interface declarations
- Call expressions

```go
// Simplified extraction logic
func walk(node *sitter.Node) {
    switch node.Type() {
    case "function_declaration":
        extractFunction(node)
    case "call_expression":
        extractCall(node)
    // ... recurse into children
    }
}
```

Skip initially: variables (scoping is complex), complex inheritance resolution, dynamic dispatch.

**Phase 2: SCM-Based Overrides (when needed)**

Support `.scm` query files for language-specific customizations:
- Decorator-heavy Python (Django, Flask)
- Framework-specific patterns (React hooks, Vue composables)

Loaded dynamically from:
- `~/.config/orchestrator/extractors/`
- `./.orchestrator/extractors/`

**Phase 3: Go Plugins (complex cases)**

For multi-region files (Vue SFC with template + script + style), compiled as `.so` files and loaded via `plugin.Open()`.

### 4. Language Support Strategy

**Compile-time (MVP)**: Import Go bindings for core languages:
- Go (dogfood)
- Python
- JavaScript/TypeScript
- Rust
- Java

**Runtime (future)**: Load grammars dynamically via `purego` from `.so` files.

Note: Tree-sitter grammars compile to C shared libraries. `purego` enables runtime loading on Linux/macOS. Windows would require RPC-based or WASM-based alternatives.

## Key Design Principles

### Context Efficiency
The primary goal is minimizing tokens needed for codebase navigation. The tool returns structured relationships (JSON/SQL results) rather than raw source code. The LLM decides when to pull full source via `read_source()`.

### Determinism
Query results are exact, not probabilistic. "Who calls `verifyToken`?" returns a complete, verifiable set of call sites. The agent builds confidence in its mental model through factual responses.

### Incremental Updates
Store file modification times. On re-index, only parse changed files, delete their symbols/refs, and re-insert. SQLite transactions make this fast enough for pre-commit hooks.

### Extensibility Without Recompilation
Users can tune extraction without forking the tool:
1. SCM files for pattern tweaks
2. Go plugins for complex multi-language files
3. (Future) Runtime grammar loading for new languages

## Implementation Roadmap

### MVP (Week 1-2)
- [ ] SQLite schema and basic storage layer
- [ ] Manual AST traversal extractor for Go
- [ ] Manual AST traversal extractor for Python
- [ ] Manual AST traversal extractor for JavaScript
- [ ] `get_repository_map()` implementation
- [ ] `search_symbols()` implementation
- [ ] `inspect_symbol()` implementation
- [ ] CLI command to generate index

### Validation (Week 3)
- [ ] Test on large Python/Django codebase
- [ ] Test on Vue.js codebase
- [ ] Measure: indexing speed, database size, query latency
- [ ] Identify coverage gaps (what does the simple extractor miss?)

### Enhancement (Week 4+)
- [ ] SCM file loader for custom extractors
- [ ] Incremental update support
- [ ] Additional language bindings as needed
- [ ] (Optional) Purego runtime grammar loading

## Comparison to Prior Art

| Tool | Approach | Difference |
|------|----------|------------|
| **LSP** | Stateful, incremental, IDE-optimized | Designed for editing, not exploration; requires running language servers |
| **Sourcegraph/Cody** | SCIP + embeddings | Cloud-centric, heavy infrastructure |
| **Glean (Meta)** | Graph facts in RocksDB | Production-grade but complex (Haskell-based) |
| **Aider's repo map** | Tree-sitter extraction | Similar concept; this tool adds structured storage and query interface |
| **Kythe (Google)** | Universal graph format | Very robust but complex protobuf-based toolchain |

This tool occupies the space between: simpler than Glean/Kythe, more structured than Aider's map, local-only unlike Sourcegraph.

## Technical Considerations

### Tree-sitter vs LSP
Tree-sitter gives **syntactic** understanding (AST structure). LSP gives **semantic** understanding (resolves imports, overloads, inheritance). 

Tradeoff: Tree-sitter is faster, handles malformed code, requires no per-language server. But call resolution is heuristic-based (match by name within scope). For exploration use cases, "good enough" resolution is acceptable—perfect accuracy can be deferred to the `read_source()` escape hatch.

### Security (Plugin Model)
If supporting Go plugins (`.so` files):
- Validate file permissions (reject world-writable plugins)
- Load only from project-local or user-config directories
- Consider code signing for shared environments

### Platform Limitations
- **Go plugins**: Linux, FreeBSD, macOS only (no Windows)
- **Purego runtime loading**: Same platform limitations
- **SCM files**: Work everywhere

Primary target: macOS and Linux development environments.

## Open Questions

1. **Call Resolution Strategy**: Heuristic name-matching within file is simplest. Import-aware resolution adds complexity—when is it necessary?

2. **Multi-Language Files**: Vue SFCs, literate programming files. Extract as separate language regions or unified graph?

3. **Index Invalidation**: File-watcher based incremental updates vs. manual `index` command vs. git-hook integration?

4. **Symbol Signatures**: How much type information to extract? Just parameter names, or full type annotations? (Impacts token count vs. usefulness.)

## References

- [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) - Go bindings for tree-sitter
- [Tree-sitter queries](https://tree-sitter.github.io/tree-sitter/using-parsers#pattern-matching-with-queries) - SCM file syntax
- [SCIP](https://github.com/sourcegraph/scip) - Sourcegraph's indexing format
- [Aider repo map](https://aider.chat/docs/repomap.html) - Prior art for tree-sitter based code mapping
- [Purego](https://github.com/ebitengine/purego) - Runtime library loading for Go
