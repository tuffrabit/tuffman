package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tuffrabit/tuffman/internal/storage"
)

// Language represents a supported programming language
type Language string

const (
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangJavaScript Language = "javascript"
	LangTypeScript Language = "typescript"
	LangUnknown    Language = "unknown"
)

// Config holds indexer configuration
type Config struct {
	// Root is the root directory to index (the path provided by user)
	Root string

	// ProjectRoot is the git root directory (used as base for file IDs)
	ProjectRoot string

	// ExcludePatterns are glob patterns for files/directories to exclude
	ExcludePatterns []string

	// IncludeExtensions maps file extensions to languages
	IncludeExtensions map[string]Language
}

// DefaultConfig returns a default configuration with git root detection
func DefaultConfig(root string) *Config {
	// Resolve absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	// Find git root
	gitRoot := findGitRoot(absRoot)
	if gitRoot == "" {
		// No git root found, use the provided root
		gitRoot = absRoot
	}

	return &Config{
		Root:        absRoot,
		ProjectRoot: gitRoot,
		ExcludePatterns: []string{
			".git",
			".tuffman",
			"node_modules",
			"vendor",
			"__pycache__",
			"*.log",
			"*.tmp",
			"*.swp",
			".DS_Store",
		},
		IncludeExtensions: map[string]Language{
			".go":  LangGo,
			".py":  LangPython,
			".js":  LangJavaScript,
			".mjs": LangJavaScript,
			".ts":  LangTypeScript,
			".tsx": LangTypeScript,
			".mts": LangTypeScript,
		},
	}
}

// findGitRoot searches for .git directory starting from path and walking up
func findGitRoot(path string) string {
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}

	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return dir
		}

		// Go up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// Indexer handles codebase indexing
type Indexer struct {
	db     *storage.DB
	config *Config
}

// New creates a new Indexer
func New(db *storage.DB, config *Config) *Indexer {
	return &Indexer{
		db:     db,
		config: config,
	}
}

// Index performs a full index of the codebase
func (idx *Indexer) Index(ctx context.Context) error {
	return filepath.WalkDir(idx.config.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip excluded paths
		if idx.shouldExclude(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files
		if d.IsDir() {
			return nil
		}

		// Check if file has supported extension
		ext := strings.ToLower(filepath.Ext(path))
		lang, supported := idx.config.IncludeExtensions[ext]
		if !supported {
			return nil
		}

		// Index the file
		if err := idx.indexFile(path, lang); err != nil {
			// Log error but continue with other files
			fmt.Fprintf(os.Stderr, "Warning: failed to index %s: %v\n", path, err)
		}

		return nil
	})
}

// IndexFile indexes a single file (used for incremental updates)
func (idx *Indexer) IndexFile(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	lang, supported := idx.config.IncludeExtensions[ext]
	if !supported {
		return fmt.Errorf("unsupported file extension: %s", ext)
	}

	return idx.indexFile(path, lang)
}

// shouldExclude checks if a path should be excluded from indexing
func (idx *Indexer) shouldExclude(path string, d fs.DirEntry) bool {
	rel, err := filepath.Rel(idx.config.ProjectRoot, path)
	if err != nil {
		return true
	}

	base := filepath.Base(path)

	for _, pattern := range idx.config.ExcludePatterns {
		// Direct match for file/directory name
		if base == pattern {
			return true
		}

		// Glob match for patterns with wildcards
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}

		// Check if any path component matches excluded directory
		if d.IsDir() && strings.Contains(rel, string(filepath.Separator)+pattern+string(filepath.Separator)) {
			return true
		}
		if strings.HasPrefix(rel, pattern+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// indexFile indexes a single file and its symbols
func (idx *Indexer) indexFile(path string, lang Language) error {
	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	// Compute relative path from project root as file ID
	relPath, err := filepath.Rel(idx.config.ProjectRoot, path)
	if err != nil {
		return fmt.Errorf("computing relative path: %w", err)
	}

	// Use forward slashes for consistency
	relPath = filepath.ToSlash(relPath)

	// Delete existing symbols and references for this file (incremental update)
	if err := idx.db.DeleteSymbolsForFile(relPath); err != nil {
		return fmt.Errorf("deleting old symbols: %w", err)
	}
	if err := idx.db.DeleteReferencesForFile(relPath); err != nil {
		return fmt.Errorf("deleting old references: %w", err)
	}

	// Parse file based on language
	symbols, refs, err := idx.parseFile(path, lang)
	if err != nil {
		return fmt.Errorf("parsing file: %w", err)
	}

	// Save file record
	file := &storage.File{
		ID:           relPath,
		AbsolutePath: path,
		Language:     string(lang),
		SizeBytes:    info.Size(),
		Mtime:        info.ModTime().Unix(),
		IndexedAt:    time.Now().Unix(),
	}

	if err := idx.db.SaveFile(file); err != nil {
		return fmt.Errorf("saving file: %w", err)
	}

	// Build symbol ID map for reference resolution
	symbolMap := make(map[string]string) // name -> symbol ID

	// Save symbols
	for _, sym := range symbols {
		sym.FileID = relPath
		sym.Language = string(lang)
		if err := idx.db.SaveSymbol(sym); err != nil {
			return fmt.Errorf("saving symbol %s: %w", sym.Name, err)
		}
		// Map symbol name to its ID for reference resolution
		symbolMap[sym.Name] = sym.ID
	}

	// Save references with resolution
	for _, ref := range refs {
		// Try to resolve the target
		if targetID, ok := symbolMap[ref.TargetName]; ok {
			ref.TargetID = &targetID
		} else {
			// Try heuristic resolution: look up in database
			targetSyms, err := idx.db.FindSymbolByName(ref.TargetName, relPath)
			if err == nil && len(targetSyms) > 0 {
				ref.TargetID = &targetSyms[0].ID
			}
		}
		if err := idx.db.SaveReference(ref); err != nil {
			return fmt.Errorf("saving reference: %w", err)
		}
	}

	return nil
}

// parseFile parses a file and extracts symbols and references
func (idx *Indexer) parseFile(path string, lang Language) ([]*storage.Symbol, []*storage.Reference, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading file: %w", err)
	}

	switch lang {
	case LangGo:
		return idx.parseGo(content, path)
	case LangPython:
		syms, err := idx.parsePythonWithVariables(content, path)
		return syms, nil, err
	case LangJavaScript:
		syms, err := idx.parseJavaScript(content, path)
		return syms, nil, err
	case LangTypeScript:
		// Use TSX parser for .tsx files, regular TypeScript for .ts files
		if strings.HasSuffix(strings.ToLower(path), ".tsx") {
			syms, err := idx.parseTSX(content, path)
			return syms, nil, err
		}
		syms, err := idx.parseTypeScript(content, path)
		return syms, nil, err
	default:
		return nil, nil, fmt.Errorf("unsupported language: %s", lang)
	}
}

// Stats returns indexing statistics
func (idx *Indexer) Stats() (fileCount, symbolCount int64, err error) {
	return idx.db.Stats()
}

// IndexFiles indexes a batch of files (implements watcher.Handler)
func (idx *Indexer) IndexFiles(paths []string) error {
	for _, path := range paths {
		// Determine language from extension
		ext := strings.ToLower(filepath.Ext(path))
		lang, supported := idx.config.IncludeExtensions[ext]
		if !supported {
			continue
		}

		// Skip if file no longer exists
		if _, err := os.Stat(path); err != nil {
			continue
		}

		if err := idx.indexFile(path, lang); err != nil {
			return fmt.Errorf("indexing %s: %w", path, err)
		}
	}
	return nil
}

// DeleteFile removes a file from the index (implements watcher.Handler)
// The path parameter is the absolute path to the file
func (idx *Indexer) DeleteFile(path string) error {
	// First, try to find the file by absolute path
	file, err := idx.db.GetFileByAbsolutePath(path)
	if err != nil {
		return fmt.Errorf("looking up file: %w", err)
	}

	// If found, delete by ID (relative path)
	if file != nil {
		return idx.db.DeleteFile(file.ID)
	}

	// If not found by absolute path, the path might already be a relative path
	// Try computing relative path from project root
	relPath, err := filepath.Rel(idx.config.ProjectRoot, path)
	if err != nil {
		return fmt.Errorf("computing relative path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)

	return idx.db.DeleteFile(relPath)
}

// IndexAll performs a full re-index (implements watcher.Handler)
func (idx *Indexer) IndexAll(ctx context.Context) error {
	return idx.Index(ctx)
}

// Test comment added
