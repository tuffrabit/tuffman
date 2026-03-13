package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tuffrabit/tuffman/internal/storage"
)

// Handler handles MCP tool calls
type Handler struct {
	db   *storage.DB
	root string
}

// NewHandler creates a new MCP handler
func NewHandler(db *storage.DB, root string) *Handler {
	return &Handler{
		db:   db,
		root: root,
	}
}

// HandleToolCall handles a tool call request
func (h *Handler) HandleToolCall(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	switch name {
	case ToolGetRepositoryMap:
		return h.handleGetRepositoryMap(ctx, arguments)
	case ToolSearchSymbols:
		return h.handleSearchSymbols(ctx, arguments)
	case ToolInspectSymbol:
		return h.handleInspectSymbol(ctx, arguments)
	case ToolGetReferences:
		return h.handleGetReferences(ctx, arguments)
	case ToolReadSource:
		return h.handleReadSource(ctx, arguments)
	case ToolGetIndexStatus:
		return h.handleGetIndexStatus(ctx, arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// GetRepositoryMapArgs represents arguments for get_repository_map
type GetRepositoryMapArgs struct {
	Path  string `json:"path,omitempty"`
	Depth int    `json:"depth,omitempty"`
}

func (h *Handler) handleGetRepositoryMap(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	var args GetRepositoryMapArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Use default depth if not specified
	if args.Depth == 0 {
		args.Depth = 3
	}

	// Get language stats
	langStats, err := h.db.GetFileLanguageStats()
	if err != nil {
		return nil, fmt.Errorf("getting language stats: %w", err)
	}

	// Get hierarchical directory tree
	dirTree, err := h.db.GetDirectoryTree(args.Depth)
	if err != nil {
		return nil, fmt.Errorf("getting directory tree: %w", err)
	}

	totalFiles, totalSymbols, err := h.db.Stats()
	if err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	// Get last indexed time
	lastIndexed, err := h.db.GetLastIndexedTime()
	if err != nil {
		return nil, fmt.Errorf("getting last indexed time: %w", err)
	}

	result := map[string]interface{}{
		"root":           args.Path,
		"languages":      langStats,
		"total_files":    totalFiles,
		"total_symbols":  totalSymbols,
		"last_indexed":   lastIndexed,
		"directory_tree": dirTree,
	}

	text := fmt.Sprintf("Repository has %d files with %d symbols across %d languages", totalFiles, totalSymbols, len(langStats))
	if lastIndexed != "" {
		text += fmt.Sprintf(" (last indexed: %s)", lastIndexed)
	}

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(text),
			NewJSONContent(result),
		},
	}, nil
}

// SearchSymbolsArgs represents arguments for search_symbols
type SearchSymbolsArgs struct {
	Query    string `json:"query"`
	Kind     string `json:"kind,omitempty"`
	Language string `json:"language,omitempty"`
}

func (h *Handler) handleSearchSymbols(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	var args SearchSymbolsArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	symbols, err := h.db.SearchSymbols(args.Query, args.Kind)
	if err != nil {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}

	// Filter by language if specified
	if args.Language != "" {
		filtered := make([]*storage.Symbol, 0, len(symbols))
		for _, sym := range symbols {
			if strings.EqualFold(sym.Language, args.Language) {
				filtered = append(filtered, sym)
			}
		}
		symbols = filtered
	}

	result := map[string]interface{}{
		"query":   args.Query,
		"count":   len(symbols),
		"symbols": symbols,
	}

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(fmt.Sprintf("Found %d symbols matching '%s'", len(symbols), args.Query)),
			NewJSONContent(result),
		},
	}, nil
}

// InspectSymbolArgs represents arguments for inspect_symbol
type InspectSymbolArgs struct {
	SymbolID string `json:"symbol_id"`
}

func (h *Handler) handleInspectSymbol(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	var args InspectSymbolArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if args.SymbolID == "" {
		return nil, fmt.Errorf("symbol_id is required")
	}

	sym, err := h.db.GetSymbol(args.SymbolID)
	if err != nil {
		return nil, fmt.Errorf("getting symbol: %w", err)
	}
	if sym == nil {
		return nil, fmt.Errorf("symbol not found: %s", args.SymbolID)
	}

	outgoing, err := h.db.GetReferencesFrom(args.SymbolID)
	if err != nil {
		return nil, fmt.Errorf("getting outgoing references: %w", err)
	}

	incoming, err := h.db.GetReferencesTo(args.SymbolID)
	if err != nil {
		return nil, fmt.Errorf("getting incoming references: %w", err)
	}

	result := map[string]interface{}{
		"symbol":   sym,
		"outgoing": outgoing,
		"incoming": incoming,
	}

	text := fmt.Sprintf("Symbol: %s (%s) in %s:%d\n", sym.Name, sym.Kind, sym.FileID, sym.LineStart)
	if sym.Signature != "" {
		text += fmt.Sprintf("Signature: %s\n", sym.Signature)
	}
	text += fmt.Sprintf("Outgoing references: %d, Incoming references: %d", len(outgoing), len(incoming))

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(text),
			NewJSONContent(result),
		},
	}, nil
}

// GetReferencesArgs represents arguments for get_references
type GetReferencesArgs struct {
	SymbolID  string `json:"symbol_id"`
	Direction string `json:"direction,omitempty"`
}

func (h *Handler) handleGetReferences(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	var args GetReferencesArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if args.SymbolID == "" {
		return nil, fmt.Errorf("symbol_id is required")
	}

	// Default to "out" if not specified
	if args.Direction == "" {
		args.Direction = "out"
	}

	// Verify symbol exists
	sym, err := h.db.GetSymbol(args.SymbolID)
	if err != nil {
		return nil, fmt.Errorf("getting symbol: %w", err)
	}
	if sym == nil {
		return nil, fmt.Errorf("symbol not found: %s", args.SymbolID)
	}

	result := map[string]interface{}{
		"symbol_id": args.SymbolID,
		"symbol":    sym,
		"direction": args.Direction,
	}

	var outgoing []*storage.Reference
	var incoming []*storage.Reference

	if args.Direction == "out" || args.Direction == "both" {
		outgoing, err = h.db.GetReferencesFrom(args.SymbolID)
		if err != nil {
			return nil, fmt.Errorf("getting outgoing references: %w", err)
		}
		result["outgoing"] = outgoing
		result["outgoing_count"] = len(outgoing)
	}

	if args.Direction == "in" || args.Direction == "both" {
		incoming, err = h.db.GetReferencesTo(args.SymbolID)
		if err != nil {
			return nil, fmt.Errorf("getting incoming references: %w", err)
		}
		result["incoming"] = incoming
		result["incoming_count"] = len(incoming)
	}

	text := fmt.Sprintf("References for %s (%s):\n", sym.Name, sym.Kind)
	if args.Direction == "out" || args.Direction == "both" {
		text += fmt.Sprintf("  Outgoing: %d references\n", len(outgoing))
	}
	if args.Direction == "in" || args.Direction == "both" {
		text += fmt.Sprintf("  Incoming: %d references\n", len(incoming))
	}

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(text),
			NewJSONContent(result),
		},
	}, nil
}

// ReadSourceArgs represents arguments for read_source
type ReadSourceArgs struct {
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

func (h *Handler) handleReadSource(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	var args ReadSourceArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if args.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	// Default values
	if args.StartLine == 0 {
		args.StartLine = 1
	}
	if args.EndLine == 0 {
		args.EndLine = -1
	}

	// Try to resolve the file path
	filePath := args.FilePath

	// First try: look up in database
	file, err := h.db.GetFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("looking up file: %w", err)
	}

	// Second try: absolute path lookup
	if file == nil {
		absPath, err := filepath.Abs(filePath)
		if err == nil {
			file, _ = h.db.GetFileByAbsolutePath(absPath)
		}
	}

	var absolutePath string
	if file != nil {
		absolutePath = file.AbsolutePath
	} else {
		// Try as relative to root or absolute
		absolutePath = filePath
		if !filepath.IsAbs(filePath) {
			absPath := filepath.Join(h.root, filePath)
			if _, err := os.Stat(absPath); err == nil {
				absolutePath = absPath
			}
		}
	}

	content, err := readFileContent(absolutePath, args.StartLine, args.EndLine)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// Count actual lines
	lines := strings.Split(content, "\n")
	actualEndLine := args.StartLine + len(lines) - 1
	if args.EndLine > 0 && args.EndLine < actualEndLine {
		actualEndLine = args.EndLine
	}

	result := map[string]interface{}{
		"file_path":     args.FilePath,
		"absolute_path": absolutePath,
		"start_line":    args.StartLine,
		"end_line":      actualEndLine,
		"line_count":    len(lines) - 1, // -1 because last split creates empty string
		"content":       content,
	}

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(fmt.Sprintf("File: %s (lines %d-%d):\n```\n%s```", args.FilePath, args.StartLine, actualEndLine, content)),
			NewJSONContent(result),
		},
	}, nil
}

// GetIndexStatusArgs represents arguments for get_index_status
type GetIndexStatusArgs struct {
	Path string `json:"path,omitempty"`
}

func (h *Handler) handleGetIndexStatus(ctx context.Context, arguments json.RawMessage) (*ToolCallResponse, error) {
	fileCount, symbolCount, err := h.db.Stats()
	if err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	// Get language stats
	langStats, err := h.db.GetFileLanguageStats()
	if err != nil {
		return nil, fmt.Errorf("getting language stats: %w", err)
	}

	// Get last indexed time
	lastIndexed, err := h.db.GetLastIndexedTime()
	if err != nil {
		return nil, fmt.Errorf("getting last indexed time: %w", err)
	}

	result := map[string]interface{}{
		"files":          fileCount,
		"symbols":        symbolCount,
		"languages":      langStats,
		"language_count": len(langStats),
		"last_indexed":   lastIndexed,
	}

	text := fmt.Sprintf("Index status: %d files, %d symbols indexed across %d languages", fileCount, symbolCount, len(langStats))
	if lastIndexed != "" {
		text += fmt.Sprintf(" (last indexed: %s)", lastIndexed)
	}

	return &ToolCallResponse{
		Content: []ToolContent{
			NewTextContent(text),
			NewJSONContent(result),
		},
	}, nil
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

// parseInt converts a string to int, returning default value on error
func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return i
}
