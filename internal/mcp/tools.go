package mcp

// ToolNames defines the available tool names
const (
	ToolGetRepositoryMap = "get_repository_map"
	ToolSearchSymbols    = "search_symbols"
	ToolInspectSymbol    = "inspect_symbol"
	ToolGetReferences    = "get_references"
	ToolReadSource       = "read_source"
	ToolGetIndexStatus   = "get_index_status"
)

// GetTools returns the list of available tools
func GetTools() []Tool {
	return []Tool{
		{
			Name:        ToolGetRepositoryMap,
			Description: "Get a high-level overview of the codebase structure including language distribution and directory tree. Use this to understand project organization before diving into specific symbols.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"path": {
						Type:        "string",
						Description: "Root path to analyze (default: current working directory)",
					},
					"depth": {
						Type:        "integer",
						Description: "Maximum depth for directory tree (default: 3)",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        ToolSearchSymbols,
			Description: "Search for symbols (functions, classes, methods, variables) by name pattern. Returns matching symbols with their location and metadata. Use this to find specific code elements when you know part of the name.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query": {
						Type:        "string",
						Description: "Search pattern (partial match, case-sensitive)",
					},
					"kind": {
						Type:        "string",
						Description: "Filter by symbol kind (e.g., 'function', 'struct', 'class', 'method')",
					},
					"language": {
						Type:        "string",
						Description: "Filter by programming language (e.g., 'go', 'python', 'javascript')",
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        ToolInspectSymbol,
			Description: "Get detailed information about a specific symbol including its signature, documentation, file location, and references. Use this when you have a symbol ID from search_symbols and need full details.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"symbol_id": {
						Type:        "string",
						Description: "Unique symbol identifier (format: file_path#symbol_name#line_number)",
					},
				},
				Required: []string{"symbol_id"},
			},
		},
		{
			Name:        ToolGetReferences,
			Description: "Get incoming or outgoing references (dependencies) for a symbol. Shows which symbols call this one (incoming) or which symbols this one calls (outgoing). Use this to understand code relationships and dependencies.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"symbol_id": {
						Type:        "string",
						Description: "Unique symbol identifier",
					},
					"direction": {
						Type:        "string",
						Description: "Reference direction: 'in' for callers, 'out' for callees, 'both' for all",
						Enum:        []string{"in", "out", "both"},
					},
				},
				Required: []string{"symbol_id"},
			},
		},
		{
			Name:        ToolReadSource,
			Description: "Read source code from a file with optional line range. Use this to view the actual implementation when you need to see the code content.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"file_path": {
						Type:        "string",
						Description: "Path to the file (relative to project root or absolute)",
					},
					"start_line": {
						Type:        "integer",
						Description: "Starting line number (1-indexed, default: 1)",
					},
					"end_line": {
						Type:        "integer",
						Description: "Ending line number (1-indexed, -1 means end of file)",
					},
				},
				Required: []string{"file_path"},
			},
		},
		{
			Name:        ToolGetIndexStatus,
			Description: "Get the current indexing status including number of files and symbols indexed, and last update time. Use this to check if the index is ready and up to date.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"path": {
						Type:        "string",
						Description: "Path to check status for (default: current working directory)",
					},
				},
				Required: []string{},
			},
		},
	}
}
