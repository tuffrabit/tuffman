package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tuffrabit/tuffman/internal/config"
	"github.com/tuffrabit/tuffman/internal/indexer"
	"github.com/tuffrabit/tuffman/internal/mcp"
	"github.com/tuffrabit/tuffman/internal/storage"
	"github.com/tuffrabit/tuffman/internal/watcher"
)

// OutputFormat represents the output format type
type OutputFormat string

const (
	FormatText OutputFormat = "text"
	FormatJSON OutputFormat = "json"
)

// Global format flag (set during argument parsing)
var globalFormat = FormatText

// ErrorResult represents an error response in JSON format
type ErrorResult struct {
	Error string `json:"Error"`
}

// Execute runs the CLI
func Execute(ctx context.Context) error {
	if len(os.Args) < 2 {
		return printUsage()
	}

	// Check for global flags before command
	args := os.Args[2:]
	cmd := os.Args[1]

	// Handle global flags
	filteredArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 < len(args) {
				globalFormat = OutputFormat(args[i+1])
				if globalFormat != FormatJSON && globalFormat != FormatText {
					return handleError(fmt.Errorf("invalid format: %s (must be 'text' or 'json')", globalFormat))
				}
				i++
			}
		default:
			filteredArgs = append(filteredArgs, args[i])
		}
	}

	var err error
	switch cmd {
	case "index":
		err = runIndex(ctx, filteredArgs)
	case "watch":
		err = runWatch(ctx, filteredArgs)
	case "stats":
		err = runStats(ctx, filteredArgs)
	case "symbols":
		err = runSymbols(ctx, filteredArgs)
	case "map":
		err = runMap(ctx, filteredArgs)
	case "inspect":
		err = runInspect(ctx, filteredArgs)
	case "refs":
		err = runRefs(ctx, filteredArgs)
	case "read":
		err = runRead(ctx, filteredArgs)
	case "server":
		err = runServer(ctx, filteredArgs)
	case "help", "--help", "-h":
		err = printUsage()
	default:
		err = fmt.Errorf("unknown command: %s", cmd)
	}

	if err != nil {
		return handleError(err)
	}
	return nil
}

// handleError returns an error, converting it to JSON if in JSON mode
func handleError(err error) error {
	if globalFormat == FormatJSON {
		// Print JSON error to stdout and return nil (so main doesn't print text error)
		printJSON(ErrorResult{Error: err.Error()})
		return nil
	}
	return err
}

func printUsage() error {
	fmt.Println(`tuffman - AI Agent Orchestrator

Usage:
  tuffman <command> [arguments] [global flags]

Commands:
  index [path]           Index a codebase (defaults to current directory)
  watch [path]           Watch for changes and continuously index
  stats                  Show indexing statistics
  symbols <query>        Search for symbols by name
  map [--depth N]        Display repository structure
  inspect <symbol_id>    Show symbol details and references
  refs <symbol_id>       Show incoming/outgoing references
  read <file> [range]    Read file contents (with optional line range)
  server                 Run as MCP server

Global Flags:
  --format <type>        Output format: text or json (default: text)
  --config <path>        Path to config file (overrides default locations)

Server Flags:
  --path <path>          Root path to index (default: current directory)
  --transport <type>     MCP transport: stdio (default)
  --no-watch             Disable auto-watch

Examples:
  tuffman index                   # Index current directory
  tuffman index ./src             # Index specific directory
  tuffman index --config ./custom.json  # Use custom config
  tuffman watch                   # Watch current directory
  tuffman watch ./src             # Watch specific directory
  tuffman stats                   # Show database statistics
  tuffman symbols "Handler"       # Search for symbols containing "Handler"
  tuffman symbols "Handler" --format json
  tuffman map                     # Show repository structure
  tuffman map --depth 2           # Show structure 2 levels deep
  tuffman inspect "main.go#main#1" # Show symbol details
  tuffman refs "main.go#main#1"   # Show outgoing references
  tuffman refs "main.go#main#1" --direction in  # Show incoming references
  tuffman read main.go            # Read entire file
  tuffman read main.go:10:20      # Read lines 10-20
  tuffman server                  # Run MCP server (stdio)
  tuffman server --config ~/.config/tuffman/config.json`)
	return nil
}

// getDBPath returns the path to the index database
func getDBPath() (string, error) {
	// Try to find .tuffman directory from current directory upward
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		tuffmanDir := filepath.Join(dir, ".tuffman")
		if info, err := os.Stat(tuffmanDir); err == nil && info.IsDir() {
			return filepath.Join(tuffmanDir, "index.db"), nil
		}

		// Go up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// No .tuffman directory found, create in current directory
	return filepath.Join(cwd, ".tuffman", "index.db"), nil
}

// openDB opens the database, creating the .tuffman directory if needed
func openDB() (*storage.DB, error) {
	dbPath, err := getDBPath()
	if err != nil {
		return nil, fmt.Errorf("finding database path: %w", err)
	}

	// Ensure .tuffman directory exists
	tuffmanDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(tuffmanDir, 0755); err != nil {
		return nil, fmt.Errorf("creating .tuffman directory: %w", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	return db, nil
}

// IndexResult represents the result of an index operation for JSON output
type IndexResult struct {
	Root           string `json:"Root"`
	FilesIndexed   int64  `json:"FilesIndexed"`
	SymbolsIndexed int64  `json:"SymbolsIndexed"`
	Success        bool   `json:"Success"`
	Error          string `json:"Error,omitempty"`
}

func runIndex(ctx context.Context, args []string) error {
	root := "."
	configPath := ""

	// Parse args and flags
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "--") {
			root = args[i]
		}
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: root, Success: false, Error: fmt.Sprintf("resolving path: %v", err)})
		}
		return fmt.Errorf("resolving path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absRoot); err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: absRoot, Success: false, Error: fmt.Sprintf("path does not exist: %s", absRoot)})
		}
		return fmt.Errorf("path does not exist: %s", absRoot)
	}

	// Load configuration
	loader := config.NewLoader(absRoot)
	if configPath != "" {
		loader.SetOverridePath(configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: absRoot, Success: false, Error: fmt.Sprintf("loading config: %v", err)})
		}
		return fmt.Errorf("loading config: %w", err)
	}

	// Open database
	db, err := openDB()
	if err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: absRoot, Success: false, Error: fmt.Sprintf("opening database: %v", err)})
		}
		return err
	}
	defer db.Close()

	// Create indexer using config
	idxConfig := cfg.ToIndexerConfig(absRoot)
	idx := indexer.New(db, idxConfig)

	if globalFormat != FormatJSON {
		fmt.Printf("Indexing %s...\n", absRoot)
	}

	// Run indexing
	if err := idx.Index(ctx); err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: absRoot, Success: false, Error: fmt.Sprintf("indexing failed: %v", err)})
		}
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Show stats
	fileCount, symbolCount, err := idx.Stats()
	if err != nil {
		if globalFormat == FormatJSON {
			return printJSON(IndexResult{Root: absRoot, Success: false, Error: fmt.Sprintf("getting stats: %v", err)})
		}
		return fmt.Errorf("getting stats: %w", err)
	}

	if globalFormat == FormatJSON {
		return printJSON(IndexResult{
			Root:           absRoot,
			FilesIndexed:   fileCount,
			SymbolsIndexed: symbolCount,
			Success:        true,
		})
	}

	fmt.Printf("\nIndexed %d files, %d symbols\n", fileCount, symbolCount)
	return nil
}

func runStats(ctx context.Context, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	fileCount, symbolCount, err := db.Stats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
	}

	if globalFormat == FormatJSON {
		result := map[string]interface{}{
			"Files":   fileCount,
			"Symbols": symbolCount,
		}
		return printJSON(result)
	}

	fmt.Printf("Database Statistics:\n")
	fmt.Printf("  Files:   %d\n", fileCount)
	fmt.Printf("  Symbols: %d\n", symbolCount)

	return nil
}

// printJSON prints a value as JSON
func printJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func runSymbols(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: tuffman symbols <query>")
	}

	query := args[0]

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	symbols, err := db.SearchSymbols(query, "")
	if err != nil {
		return fmt.Errorf("searching symbols: %w", err)
	}

	if globalFormat == FormatJSON {
		result := map[string]interface{}{
			"Query":   query,
			"Count":   len(symbols),
			"Symbols": symbols,
		}
		return printJSON(result)
	}

	if len(symbols) == 0 {
		fmt.Println("No symbols found")
		return nil
	}

	fmt.Printf("Found %d symbols:\n\n", len(symbols))

	for _, sym := range symbols {
		fmt.Printf("%s (%s)\n", sym.Name, sym.Kind)
		fmt.Printf("  ID: %s\n", sym.ID)
		fmt.Printf("  File: %s:%d\n", sym.FileID, sym.LineStart)
		if sym.Signature != "" {
			fmt.Printf("  Signature: %s\n", sym.Signature)
		}
		if sym.Receiver != "" {
			fmt.Printf("  Receiver: %s\n", sym.Receiver)
		}
		if sym.Doc != "" {
			doc := sym.Doc
			if len(doc) > 80 {
				doc = doc[:77] + "..."
			}
			fmt.Printf("  Doc: %s\n", doc)
		}
		fmt.Println()
	}

	return nil
}

// WatchEvent represents a watch event for JSON output
type WatchEvent struct {
	Type        string `json:"Type"` // "indexed", "deleted", "error", "full_reindex", "info"
	Path        string `json:"Path,omitempty"`
	Error       string `json:"Error,omitempty"`
	FileCount   int64  `json:"FileCount,omitempty"`
	SymbolCount int64  `json:"SymbolCount,omitempty"`
	Message     string `json:"Message,omitempty"`
}

// jsonWatchHandler wraps the indexer to output JSON events
type jsonWatchHandler struct {
	idx *indexer.Indexer
}

func (h *jsonWatchHandler) IndexFiles(paths []string) error {
	err := h.idx.IndexFiles(paths)
	for _, path := range paths {
		event := WatchEvent{Type: "indexed", Path: path}
		if err != nil {
			event.Type = "error"
			event.Error = err.Error()
		}
		printJSONLine(event)
	}
	return err
}

func (h *jsonWatchHandler) DeleteFile(path string) error {
	err := h.idx.DeleteFile(path)
	event := WatchEvent{Type: "deleted", Path: path}
	if err != nil {
		event.Type = "error"
		event.Error = err.Error()
	}
	printJSONLine(event)
	return err
}

func (h *jsonWatchHandler) IndexAll(ctx context.Context) error {
	printJSONLine(WatchEvent{Type: "info", Message: "Starting full re-index"})
	err := h.idx.IndexAll(ctx)
	if err != nil {
		printJSONLine(WatchEvent{Type: "error", Error: err.Error()})
		return err
	}
	fileCount, symbolCount, _ := h.idx.Stats()
	printJSONLine(WatchEvent{
		Type:        "full_reindex",
		FileCount:   fileCount,
		SymbolCount: symbolCount,
	})
	return nil
}

// printJSONLine prints a JSON object followed by a newline (for streaming)
func printJSONLine(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

func runWatch(ctx context.Context, args []string) error {
	root := "."
	configPath := ""

	// Parse args and flags
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "--") {
			root = args[i]
		}
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		if globalFormat == FormatJSON {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("resolving path: %v", err)})
			return nil
		}
		return fmt.Errorf("resolving path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absRoot); err != nil {
		if globalFormat == FormatJSON {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("path does not exist: %s", absRoot)})
			return nil
		}
		return fmt.Errorf("path does not exist: %s", absRoot)
	}

	// Load configuration
	loader := config.NewLoader(absRoot)
	if configPath != "" {
		loader.SetOverridePath(configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		if globalFormat == FormatJSON {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("loading config: %v", err)})
			return nil
		}
		return fmt.Errorf("loading config: %w", err)
	}

	// Open database
	db, err := openDB()
	if err != nil {
		if globalFormat == FormatJSON {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("opening database: %v", err)})
			return nil
		}
		return err
	}
	defer db.Close()

	// Create indexer using config
	idxConfig := cfg.ToIndexerConfig(absRoot)
	idx := indexer.New(db, idxConfig)

	isJSONMode := globalFormat == FormatJSON

	if !isJSONMode {
		fmt.Printf("Indexing %s...\n", absRoot)
	}

	// Run initial full index
	if err := idx.Index(ctx); err != nil {
		if isJSONMode {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("initial indexing failed: %v", err)})
			return nil
		}
		return fmt.Errorf("initial indexing failed: %w", err)
	}

	// Show stats
	fileCount, symbolCount, err := idx.Stats()
	if err != nil {
		if isJSONMode {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("getting stats: %v", err)})
			return nil
		}
		return fmt.Errorf("getting stats: %w", err)
	}

	if isJSONMode {
		printJSONLine(WatchEvent{
			Type:        "info",
			Message:     "Initial index complete",
			FileCount:   fileCount,
			SymbolCount: symbolCount,
		})
		printJSONLine(WatchEvent{Type: "info", Message: "Watching for changes"})
	} else {
		fmt.Printf("\nInitial index complete: %d files, %d symbols\n", fileCount, symbolCount)
		fmt.Println("\nWatching for changes... (Press Ctrl-C to stop)")
	}

	// Create watcher using config
	watcherConfig := cfg.ToWatcherConfig(idxConfig)

	var handler watcher.Handler = idx
	if isJSONMode {
		handler = &jsonWatchHandler{idx: idx}
	}

	w, err := watcher.New(watcherConfig, handler)
	if err != nil {
		if isJSONMode {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("creating watcher: %v", err)})
			return nil
		}
		return fmt.Errorf("creating watcher: %w", err)
	}

	// Start watching
	if err := w.Start(ctx); err != nil {
		if isJSONMode {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("starting watcher: %v", err)})
			return nil
		}
		return fmt.Errorf("starting watcher: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	if isJSONMode {
		printJSONLine(WatchEvent{Type: "info", Message: "Shutting down watcher"})
	} else {
		fmt.Println("\nShutting down watcher...")
	}

	if err := w.Stop(); err != nil {
		if isJSONMode {
			printJSONLine(WatchEvent{Type: "error", Error: fmt.Sprintf("stopping watcher: %v", err)})
			return nil
		}
		return fmt.Errorf("stopping watcher: %w", err)
	}

	return nil
}

func runMap(ctx context.Context, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Parse depth flag
	maxDepth := 3
	for i := 0; i < len(args); i++ {
		if args[i] == "--depth" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &maxDepth)
			break
		}
	}

	// Get language stats
	langStats, err := db.GetFileLanguageStats()
	if err != nil {
		return fmt.Errorf("getting language stats: %w", err)
	}

	// Get directory stats
	dirStats, err := db.GetDirectoryStats()
	if err != nil {
		return fmt.Errorf("getting directory stats: %w", err)
	}

	totalFiles, totalSymbols, err := db.Stats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
	}

	if globalFormat == FormatJSON {
		result := map[string]interface{}{
			"Root":         ".",
			"Languages":    langStats,
			"Directories":  dirStats,
			"TotalFiles":   totalFiles,
			"TotalSymbols": totalSymbols,
		}
		return printJSON(result)
	}

	fmt.Println("Repository Structure")
	fmt.Println("===================")
	fmt.Println()

	// Print language summary
	if len(langStats) > 0 {
		fmt.Println("Languages:")
		for lang, count := range langStats {
			fmt.Printf("  %s: %d files\n", lang, count)
		}
		fmt.Println()
	}

	// Print directory tree
	fmt.Println("Directory Tree:")
	printDirectoryTree(dirStats, "", maxDepth, 0)

	fmt.Println()
	fmt.Printf("Total: %d files, %d symbols\n", totalFiles, totalSymbols)

	return nil
}

// printDirectoryTree prints a tree structure of directories
func printDirectoryTree(stats map[string]struct{ Files, Symbols int64 }, prefix string, maxDepth, currentDepth int) {
	if currentDepth >= maxDepth {
		if len(stats) > 0 {
			fmt.Printf("%s%s...\n", prefix, "├── ")
		}
		return
	}

	// Get sorted directory names
	dirs := make([]string, 0, len(stats))
	for dir := range stats {
		dirs = append(dirs, dir)
	}
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			if dirs[i] > dirs[j] {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}

	for i, dir := range dirs {
		isLast := i == len(dirs)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		stat := stats[dir]
		displayName := dir
		if dir == "." {
			displayName = "(root)"
		}
		fmt.Printf("%s%s%s (%d files, %d symbols)\n", prefix, connector, displayName, stat.Files, stat.Symbols)
	}
}

func runInspect(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: tuffman inspect <symbol_id>")
	}

	symbolID := args[0]

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Get symbol
	sym, err := db.GetSymbol(symbolID)
	if err != nil {
		return fmt.Errorf("getting symbol: %w", err)
	}
	if sym == nil {
		return fmt.Errorf("symbol not found: %s", symbolID)
	}

	// Get outgoing references
	outgoing, err := db.GetReferencesFrom(symbolID)
	if err != nil {
		return fmt.Errorf("getting outgoing references: %w", err)
	}

	// Get incoming references
	incoming, err := db.GetReferencesTo(symbolID)
	if err != nil {
		return fmt.Errorf("getting incoming references: %w", err)
	}

	if globalFormat == FormatJSON {
		result := map[string]interface{}{
			"Symbol":   sym,
			"Outgoing": outgoing,
			"Incoming": incoming,
		}
		return printJSON(result)
	}

	// Print symbol details
	fmt.Println("Symbol Details")
	fmt.Println("=============")
	fmt.Println()
	fmt.Printf("ID:         %s\n", sym.ID)
	fmt.Printf("Name:       %s\n", sym.Name)
	fmt.Printf("Kind:       %s\n", sym.Kind)
	fmt.Printf("Language:   %s\n", sym.Language)
	fmt.Printf("File:       %s:%d\n", sym.FileID, sym.LineStart)
	if sym.Receiver != "" {
		fmt.Printf("Receiver:   %s\n", sym.Receiver)
	}
	if sym.Signature != "" {
		fmt.Println()
		fmt.Println("Signature:")
		fmt.Printf("  %s\n", sym.Signature)
	}
	if sym.Doc != "" {
		fmt.Println()
		fmt.Println("Documentation:")
		for _, line := range strings.Split(sym.Doc, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}

	// Print outgoing references
	fmt.Println()
	fmt.Printf("Outgoing References (%d):\n", len(outgoing))
	if len(outgoing) == 0 {
		fmt.Println("  (none)")
	} else {
		// Group by kind
		calls := 0
		imports := 0
		resolved := 0
		for _, ref := range outgoing {
			switch ref.Kind {
			case "call":
				calls++
			case "import":
				imports++
			}
			if ref.TargetID != nil {
				resolved++
			}
		}
		fmt.Printf("  Calls: %d, Imports: %d\n", calls, imports)
		fmt.Printf("  Resolved: %d/%d\n", resolved, len(outgoing))
		fmt.Println()
		fmt.Println("  Top calls:")
		count := 0
		for _, ref := range outgoing {
			if ref.Kind == "call" && count < 10 {
				status := "?"
				if ref.TargetID != nil {
					status = "✓"
				}
				fmt.Printf("    [%s] %s (line %d)\n", status, ref.TargetName, ref.Line)
				count++
			}
		}
	}

	// Print incoming references
	fmt.Println()
	fmt.Printf("Incoming References (%d):\n", len(incoming))
	if len(incoming) == 0 {
		fmt.Println("  (none)")
	} else {
		count := 0
		for _, ref := range incoming {
			if count >= 10 {
				fmt.Printf("  ... and %d more\n", len(incoming)-count)
				break
			}
			fmt.Printf("  From: %s (line %d)\n", ref.SourceID, ref.Line)
			count++
		}
	}

	return nil
}

// RefTreeNode represents a node in the reference tree for JSON output
type RefTreeNode struct {
	SymbolID   string        `json:"SymbolID"`
	Name       string        `json:"Name"`
	Kind       string        `json:"Kind"`
	FileID     string        `json:"FileID"`
	Line       int           `json:"Line"`
	Resolved   bool          `json:"Resolved"`
	Children   []RefTreeNode `json:"Children,omitempty"`
	IsCycle    bool          `json:"IsCycle,omitempty"`
	DepthLimit bool          `json:"DepthLimit,omitempty"`
}

// IncomingRef represents a single incoming reference for JSON output
type IncomingRef struct {
	SourceID   string `json:"SourceID"`
	SourceName string `json:"SourceName"`
	SourceKind string `json:"SourceKind"`
	SourceFile string `json:"SourceFile"`
	Line       int    `json:"Line"`
	Kind       string `json:"Kind"`
}

// RefsResult represents the complete refs command result for JSON output
type RefsResult struct {
	SymbolID   string        `json:"SymbolID"`
	SymbolName string        `json:"SymbolName"`
	SymbolKind string        `json:"SymbolKind"`
	Direction  string        `json:"Direction"`
	Outgoing   []RefTreeNode `json:"Outgoing,omitempty"`
	Incoming   []IncomingRef `json:"Incoming,omitempty"`
}

func runRefs(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: tuffman refs <symbol_id> [--direction in|out]")
	}

	symbolID := args[0]
	direction := "out" // default

	// Parse flags
	for i := 1; i < len(args); i++ {
		if args[i] == "--direction" && i+1 < len(args) {
			direction = args[i+1]
			if direction != "in" && direction != "out" && direction != "both" {
				return fmt.Errorf("invalid direction: %s (must be 'in', 'out', or 'both')", direction)
			}
			break
		}
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Verify symbol exists
	sym, err := db.GetSymbol(symbolID)
	if err != nil {
		return fmt.Errorf("getting symbol: %w", err)
	}
	if sym == nil {
		return fmt.Errorf("symbol not found: %s", symbolID)
	}

	if globalFormat == FormatJSON {
		result := RefsResult{
			SymbolID:   symbolID,
			SymbolName: sym.Name,
			SymbolKind: sym.Kind,
			Direction:  direction,
		}

		if direction == "out" || direction == "both" {
			result.Outgoing = buildOutgoingTree(db, symbolID, 0, 10, make(map[string]bool))
		}

		if direction == "in" || direction == "both" {
			result.Incoming = buildIncomingList(db, symbolID)
		}

		return printJSON(result)
	}

	// Text output
	fmt.Printf("References for: %s (%s)\n", sym.Name, sym.Kind)
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	if direction == "out" || direction == "both" {
		fmt.Println("Outgoing References (calls this symbol makes):")
		fmt.Println()
		if err := showOutgoingRefs(db, symbolID, 0, 3, make(map[string]bool)); err != nil {
			return err
		}
		fmt.Println()
	}

	if direction == "in" || direction == "both" {
		fmt.Println("Incoming References (symbols that call this):")
		fmt.Println()
		if err := showIncomingRefs(db, symbolID); err != nil {
			return err
		}
	}

	return nil
}

// buildOutgoingTree builds the hierarchical tree of outgoing references for JSON
func buildOutgoingTree(db *storage.DB, symbolID string, depth, maxDepth int, visited map[string]bool) []RefTreeNode {
	if depth > maxDepth {
		return nil
	}

	// Cycle detection
	if visited[symbolID] {
		return []RefTreeNode{{
			SymbolID: symbolID,
			IsCycle:  true,
		}}
	}

	visited[symbolID] = true
	defer delete(visited, symbolID)

	outgoing, err := db.GetReferencesFrom(symbolID)
	if err != nil {
		return nil
	}

	// Filter to just calls
	var calls []*storage.Reference
	for _, ref := range outgoing {
		if ref.Kind == "call" {
			calls = append(calls, ref)
		}
	}

	if len(calls) == 0 {
		return nil
	}

	var nodes []RefTreeNode
	for _, ref := range calls {
		node := RefTreeNode{
			SymbolID: ref.TargetName,
			Name:     ref.TargetName,
			Line:     ref.Line,
			Resolved: ref.TargetID != nil,
		}

		if ref.TargetID != nil {
			node.SymbolID = *ref.TargetID
			targetSym, _ := db.GetSymbol(*ref.TargetID)
			if targetSym != nil {
				node.Name = targetSym.Name
				node.Kind = targetSym.Kind
				node.FileID = targetSym.FileID
				// Recurse
				node.Children = buildOutgoingTree(db, *ref.TargetID, depth+1, maxDepth, visited)
			}
		}

		if depth >= maxDepth && len(node.Children) == 0 && node.Resolved {
			node.DepthLimit = true
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// buildIncomingList builds the list of incoming references for JSON
func buildIncomingList(db *storage.DB, symbolID string) []IncomingRef {
	incoming, err := db.GetReferencesTo(symbolID)
	if err != nil {
		return nil
	}

	var refs []IncomingRef
	for _, ref := range incoming {
		r := IncomingRef{
			SourceID: ref.SourceID,
			Line:     ref.Line,
			Kind:     ref.Kind,
		}

		sourceSym, _ := db.GetSymbol(ref.SourceID)
		if sourceSym != nil {
			r.SourceName = sourceSym.Name
			r.SourceKind = sourceSym.Kind
			r.SourceFile = sourceSym.FileID
		}

		refs = append(refs, r)
	}

	return refs
}

// showOutgoingRefs shows outgoing references with call chain tracing
func showOutgoingRefs(db *storage.DB, symbolID string, depth, maxDepth int, visited map[string]bool) error {
	if depth > maxDepth {
		return nil
	}

	// Cycle detection
	if visited[symbolID] {
		indent := strings.Repeat("  ", depth)
		fmt.Printf("%s[cycle detected]\n", indent)
		return nil
	}
	visited[symbolID] = true
	defer delete(visited, symbolID)

	outgoing, err := db.GetReferencesFrom(symbolID)
	if err != nil {
		return fmt.Errorf("getting outgoing references: %w", err)
	}

	// Filter to just calls
	var calls []*storage.Reference
	for _, ref := range outgoing {
		if ref.Kind == "call" {
			calls = append(calls, ref)
		}
	}

	if len(calls) == 0 {
		indent := strings.Repeat("  ", depth)
		fmt.Printf("%s(no outgoing calls)\n", indent)
		return nil
	}

	// Show calls
	for _, ref := range calls {
		indent := strings.Repeat("  ", depth)
		status := "?"
		if ref.TargetID != nil {
			status = "✓"
		}
		fmt.Printf("%s[%s] %s", indent, status, ref.TargetName)
		if ref.TargetID != nil {
			// Show the actual symbol name if resolved
			targetSym, _ := db.GetSymbol(*ref.TargetID)
			if targetSym != nil && targetSym.Name != ref.TargetName {
				fmt.Printf(" -> %s", targetSym.Name)
			}
			// Recurse if not too deep
			if depth < maxDepth-1 {
				fmt.Println()
				if err := showOutgoingRefs(db, *ref.TargetID, depth+1, maxDepth, visited); err != nil {
					return err
				}
				continue
			}
		}
		fmt.Println()
	}

	return nil
}

// showIncomingRefs shows incoming references
func showIncomingRefs(db *storage.DB, symbolID string) error {
	incoming, err := db.GetReferencesTo(symbolID)
	if err != nil {
		return fmt.Errorf("getting incoming references: %w", err)
	}

	if len(incoming) == 0 {
		fmt.Println("  (no incoming references)")
		fmt.Println()
		fmt.Println("Note: This may be because:")
		fmt.Println("  - No other symbols reference this one")
		fmt.Println("  - References exist but couldn't be resolved")
		fmt.Println("  - The referencing files haven't been indexed yet")
		return nil
	}

	// Group by source
	bySource := make(map[string][]*storage.Reference)
	for _, ref := range incoming {
		bySource[ref.SourceID] = append(bySource[ref.SourceID], ref)
	}

	// Show grouped references
	for sourceID, refs := range bySource {
		sourceSym, _ := db.GetSymbol(sourceID)
		if sourceSym != nil {
			fmt.Printf("  From %s (%s):\n", sourceSym.Name, sourceSym.Kind)
			fmt.Printf("    File: %s:%d\n", sourceSym.FileID, sourceSym.LineStart)
		} else {
			fmt.Printf("  From %s:\n", sourceID)
		}
		for _, ref := range refs {
			fmt.Printf("    Line %d (%s)\n", ref.Line, ref.Kind)
		}
		fmt.Println()
	}

	return nil
}

// runRead reads file contents with optional line range
func runRead(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: tuffman read <file>[:start_line[:end_line]]")
	}

	arg := args[0]
	filePath, startLine, endLine, err := parseReadArg(arg)
	if err != nil {
		return err
	}

	// Try to find the file relative to project root or as absolute path
	db, err := openDB()
	if err != nil {
		// Try reading directly if no DB
		content, readErr := readFileContent(filePath, startLine, endLine)
		if readErr != nil {
			return fmt.Errorf("no index found and cannot read file: %w", err)
		}
		fmt.Print(content)
		return nil
	}
	defer db.Close()

	// First try: treat filePath as file ID (relative path)
	file, err := db.GetFile(filePath)
	if err != nil {
		return fmt.Errorf("looking up file: %w", err)
	}

	// Second try: treat as absolute path
	if file == nil {
		file, err = db.GetFileByAbsolutePath(filePath)
		if err != nil {
			return fmt.Errorf("looking up file: %w", err)
		}
	}

	// Third try: resolve relative to current directory
	if file == nil {
		absPath, err := filepath.Abs(filePath)
		if err == nil {
			file, err = db.GetFileByAbsolutePath(absPath)
			if err != nil {
				return fmt.Errorf("looking up file: %w", err)
			}
		}
	}

	var absolutePath string
	if file != nil {
		absolutePath = file.AbsolutePath
	} else {
		// Try reading directly
		absolutePath = filePath
	}

	content, err := readFileContent(absolutePath, startLine, endLine)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if globalFormat == FormatJSON {
		result := map[string]interface{}{
			"File":         filePath,
			"AbsolutePath": absolutePath,
			"StartLine":    startLine,
			"EndLine":      endLine,
			"Content":      content,
		}
		return printJSON(result)
	}

	fmt.Print(content)
	return nil
}

// parseReadArg parses file path with optional line range
// Formats: file.go, file.go:10, file.go:10:20
func parseReadArg(arg string) (filePath string, startLine, endLine int, err error) {
	// Check for line range
	parts := strings.Split(arg, ":")
	filePath = parts[0]
	startLine = 1
	endLine = -1 // -1 means end of file

	if len(parts) >= 2 {
		startLine, err = strconv.Atoi(parts[1])
		if err != nil || startLine < 1 {
			return "", 0, 0, fmt.Errorf("invalid start line: %s", parts[1])
		}
	}

	if len(parts) >= 3 {
		endLine, err = strconv.Atoi(parts[2])
		if err != nil || endLine < 1 {
			return "", 0, 0, fmt.Errorf("invalid end line: %s", parts[2])
		}
	}

	return filePath, startLine, endLine, nil
}

// readFileContent reads file content with optional line range
func readFileContent(filePath string, startLine, endLine int) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")

	// Adjust line numbers to 0-indexed
	startIdx := startLine - 1
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(lines) {
		return "", fmt.Errorf("start line %d exceeds file length (%d lines)", startLine, len(lines))
	}

	endIdx := endLine - 1
	if endLine == -1 || endIdx >= len(lines) {
		endIdx = len(lines) - 1
	}
	if endIdx < startIdx {
		return "", fmt.Errorf("end line %d is before start line %d", endLine, startLine)
	}

	selectedLines := lines[startIdx : endIdx+1]
	return strings.Join(selectedLines, "\n") + "\n", nil
}

// runServer runs the MCP server
func runServer(ctx context.Context, args []string) error {
	root := "."
	transport := ""
	noWatch := false
	configPath := ""

	// Parse flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		case "--transport":
			if i+1 < len(args) {
				transport = args[i+1]
				i++
			}
		case "--no-watch":
			noWatch = true
		case "--config":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		}
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absRoot); err != nil {
		return fmt.Errorf("path does not exist: %s", absRoot)
	}

	// Load configuration
	loader := config.NewLoader(absRoot)
	if configPath != "" {
		loader.SetOverridePath(configPath)
	}
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply CLI overrides to config
	if transport == "" {
		transport = cfg.GetMCPTransport()
	}

	// Open database
	dbPath, err := getDBPath()
	if err != nil {
		// Try to use path relative to root
		dbPath = filepath.Join(absRoot, ".tuffman", "index.db")
	}

	tuffmanDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(tuffmanDir, 0755); err != nil {
		return fmt.Errorf("creating .tuffman directory: %w", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Create server config
	serverConfig := &mcp.ServerConfig{
		Root:      absRoot,
		Transport: transport,
		NoWatch:   noWatch,
		DB:        db,
		Config:    cfg,
	}

	server, err := mcp.NewServer(serverConfig)
	if err != nil {
		return fmt.Errorf("creating MCP server: %w", err)
	}

	return server.Run(ctx)
}
