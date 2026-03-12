package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tuffrabit/tuffman/internal/indexer"
	"github.com/tuffrabit/tuffman/internal/storage"
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
	case "stats":
		return runStats(ctx, args)
	case "symbols":
		return runSymbols(ctx, args)
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
  index [path]    Index a codebase (defaults to current directory)
  stats           Show indexing statistics
  symbols <query> Search for symbols by name

Examples:
  tuffman index              # Index current directory
  tuffman index ./src        # Index specific directory
  tuffman stats              # Show database statistics
  tuffman symbols "Handler"  # Search for symbols containing "Handler"`)
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
