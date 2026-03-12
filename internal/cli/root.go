package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tuffrabit/tuffman/internal/indexer"
	"github.com/tuffrabit/tuffman/internal/storage"
	"github.com/tuffrabit/tuffman/internal/watcher"
)

// Execute runs the CLI
func Execute(ctx context.Context) error {
	if len(os.Args) < 2 {
		return printUsage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "index":
		return runIndex(ctx, args)
	case "watch":
		return runWatch(ctx, args)
	case "stats":
		return runStats(ctx, args)
	case "symbols":
		return runSymbols(ctx, args)
	case "map":
		return runMap(ctx, args)
	case "inspect":
		return runInspect(ctx, args)
	case "refs":
		return runRefs(ctx, args)
	case "help", "--help", "-h":
		return printUsage()
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func printUsage() error {
	fmt.Println(`tuffman - AI Agent Orchestrator

Usage:
  tuffman <command> [arguments]

Commands:
  index [path]           Index a codebase (defaults to current directory)
  watch [path]           Watch for changes and continuously index
  stats                  Show indexing statistics
  symbols <query>        Search for symbols by name
  map [--depth N]        Display repository structure
  inspect <symbol_id>    Show symbol details and references
  refs <symbol_id>       Show incoming/outgoing references

Examples:
  tuffman index                   # Index current directory
  tuffman index ./src             # Index specific directory
  tuffman watch                   # Watch current directory
  tuffman watch ./src             # Watch specific directory
  tuffman stats                   # Show database statistics
  tuffman symbols "Handler"       # Search for symbols containing "Handler"
  tuffman map                     # Show repository structure
  tuffman map --depth 2           # Show structure 2 levels deep
  tuffman inspect "main.go#main#1" # Show symbol details
  tuffman refs "main.go#main#1"   # Show outgoing references
  tuffman refs "main.go#main#1" --direction in  # Show incoming references`)
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

func runIndex(ctx context.Context, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
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

	// Open database
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Create indexer
	config := indexer.DefaultConfig(absRoot)
	idx := indexer.New(db, config)

	fmt.Printf("Indexing %s...\n", absRoot)

	// Run indexing
	if err := idx.Index(ctx); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Show stats
	fileCount, symbolCount, err := idx.Stats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
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

	fmt.Printf("Database Statistics:\n")
	fmt.Printf("  Files:   %d\n", fileCount)
	fmt.Printf("  Symbols: %d\n", symbolCount)

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

	if len(symbols) == 0 {
		fmt.Println("No symbols found")
		return nil
	}

	fmt.Printf("Found %d symbols:\n\n", len(symbols))

	for _, sym := range symbols {
		fmt.Printf("%s (%s)\n", sym.Name, sym.Kind)
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

func runWatch(ctx context.Context, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
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

	// Open database
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Create indexer
	config := indexer.DefaultConfig(absRoot)
	idx := indexer.New(db, config)

	fmt.Printf("Indexing %s...\n", absRoot)

	// Run initial full index
	if err := idx.Index(ctx); err != nil {
		return fmt.Errorf("initial indexing failed: %w", err)
	}

	// Show stats
	fileCount, symbolCount, err := idx.Stats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
	}

	fmt.Printf("\nInitial index complete: %d files, %d symbols\n", fileCount, symbolCount)
	fmt.Println("\nWatching for changes... (Press Ctrl-C to stop)")

	// Create watcher
	watcherConfig := watcher.DefaultConfig(absRoot, config)
	w, err := watcher.New(watcherConfig, idx)
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}

	// Start watching
	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("starting watcher: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	fmt.Println("\nShutting down watcher...")
	if err := w.Stop(); err != nil {
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

	// Print totals
	totalFiles, totalSymbols, err := db.Stats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
	}
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
