package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/tuffrabit/tuffman/internal/config"
	"github.com/tuffrabit/tuffman/internal/indexer"
	"github.com/tuffrabit/tuffman/internal/storage"
	"github.com/tuffrabit/tuffman/internal/watcher"
)

const (
	// MCPProtocolVersion is the supported MCP protocol version
	MCPProtocolVersion = "2024-11-05"
	// JSONRPCVersion is the JSON-RPC version
	JSONRPCVersion = "2.0"
)

// ServerConfig holds server configuration
type ServerConfig struct {
	Root      string
	Transport string // "stdio" or "http"
	NoWatch   bool
	DB        *storage.DB
	Config    *config.Config // Loaded configuration
}

// Server represents an MCP server
type Server struct {
	config    *ServerConfig
	handler   *Handler
	indexer   *indexer.Indexer
	watcher   *watcher.Watcher
	transport Transport
	mu        sync.RWMutex
	started   bool
}

// Transport handles message I/O
type Transport interface {
	// Read reads the next request/notification from the transport
	Read() ([]byte, error)
	// Write writes a response to the transport
	Write(data []byte) error
	// Close closes the transport
	Close() error
}

// StdioTransport implements Transport using stdin/stdout
type StdioTransport struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport() *StdioTransport {
	return &StdioTransport{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

// Read reads a line from stdin
func (t *StdioTransport) Read() ([]byte, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line, nil
}

// Write writes data to stdout
func (t *StdioTransport) Write(data []byte) error {
	_, err := t.writer.Write(data)
	return err
}

// Close is a no-op for stdio (we don't close stdin/stdout)
func (t *StdioTransport) Close() error {
	return nil
}

// NewServer creates a new MCP server
func NewServer(serverConfig *ServerConfig) (*Server, error) {
	// Use provided config or create default
	cfg := serverConfig.Config
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Create indexer configuration from file config
	idxConfig := cfg.ToIndexerConfig(serverConfig.Root)

	// Create indexer
	idx := indexer.New(serverConfig.DB, idxConfig)

	// Create handler
	handler := NewHandler(serverConfig.DB, serverConfig.Root)

	// Create transport
	var transport Transport
	switch serverConfig.Transport {
	case "stdio":
		transport = NewStdioTransport()
	default:
		return nil, fmt.Errorf("unsupported transport: %s", serverConfig.Transport)
	}

	s := &Server{
		config:    serverConfig,
		indexer:   idx,
		transport: transport,
		handler:   handler,
	}

	// Initialize watcher if not disabled
	if !serverConfig.NoWatch {
		watcherConfig := cfg.ToWatcherConfig(idxConfig)
		w, err := watcher.New(watcherConfig, idx)
		if err != nil {
			return nil, fmt.Errorf("creating watcher: %w", err)
		}
		s.watcher = w
	}

	return s, nil
}

// Run starts the MCP server
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()

	// Get config for auto-index setting
	cfg := s.config.Config
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Do initial index if index is empty and auto-index is enabled
	fileCount, _, err := s.indexer.Stats()
	if err != nil {
		return fmt.Errorf("checking index status: %w", err)
	}

	if fileCount == 0 && cfg.ShouldAutoIndexOnStart() {
		fmt.Fprintf(os.Stderr, "Indexing %s...\n", s.config.Root)
		if err := s.indexer.Index(ctx); err != nil {
			return fmt.Errorf("initial indexing: %w", err)
		}
		fileCount, _, _ = s.indexer.Stats()
		fmt.Fprintf(os.Stderr, "Indexed %d files\n", fileCount)
	} else if fileCount == 0 {
		fmt.Fprintf(os.Stderr, "Auto-index disabled. Run 'tuffman index' to populate the index.\n")
	} else {
		fmt.Fprintf(os.Stderr, "Using existing index with %d files\n", fileCount)
	}

	// Start watcher if enabled
	if s.watcher != nil {
		if err := s.watcher.Start(ctx); err != nil {
			return fmt.Errorf("starting watcher: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Watching for changes...\n")
	}

	// Start processing messages
	return s.processMessages(ctx)
}

// processMessages handles incoming MCP messages
func (s *Server) processMessages(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read next message
		data, err := s.transport.Read()
		if err != nil {
			if err == io.EOF {
				return nil // Client closed connection
			}
			return fmt.Errorf("reading message: %w", err)
		}

		// Trim whitespace
		data = []byte(strings.TrimSpace(string(data)))
		if len(data) == 0 {
			continue
		}

		// Handle the message
		response, err := s.handleMessage(ctx, data)
		if err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Error handling message: %v\n", err)
			continue
		}

		// Send response if there is one
		if response != nil {
			responseData, err := json.Marshal(response)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling response: %v\n", err)
				continue
			}

			if err := s.transport.Write(append(responseData, '\n')); err != nil {
				return fmt.Errorf("writing response: %w", err)
			}
		}
	}
}

// handleMessage processes a single MCP message
func (s *Server) handleMessage(ctx context.Context, data []byte) (*Response, error) {
	// Try to parse as a request first
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		// Try to send a parse error response if we can extract an ID
		return &Response{
			JSONRPC: JSONRPCVersion,
			Error:   NewError(ParseError, fmt.Sprintf("Parse error: %v", err)),
		}, nil
	}

	// Validate JSON-RPC version
	if req.JSONRPC != JSONRPCVersion {
		return s.makeErrorResponse(&req, InvalidRequest, "Invalid JSON-RPC version"), nil
	}

	// Route to handler
	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, &req), nil
	case "initialized":
		// Notification, no response
		return nil, nil
	case "tools/list":
		return s.handleToolsList(ctx, &req), nil
	case "tools/call":
		return s.handleToolsCall(ctx, &req), nil
	case "$/cancelRequest":
		// Handle cancellation notification
		return nil, nil
	case "shutdown":
		return s.makeSuccessResponse(&req, struct{}{}), nil
	case "exit":
		// Exit notification
		return nil, nil
	default:
		return s.makeErrorResponse(&req, MethodNotFound, fmt.Sprintf("Method not found: %s", req.Method)), nil
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(ctx context.Context, req *Request) *Response {
	var initReq InitializeRequest
	if err := json.Unmarshal(req.Params, &initReq); err != nil {
		return s.makeErrorResponse(req, InvalidParams, fmt.Sprintf("Invalid params: %v", err))
	}

	// Build response
	result := InitializeResponse{
		ProtocolVersion: MCPProtocolVersion,
		ServerInfo: Implementation{
			Name:    "tuffman",
			Version: "0.6.0",
		},
		Capabilities: ServerCapabilities{
			Tools: &struct {
				ListChanged bool `json:"listChanged,omitempty"`
			}{
				ListChanged: false,
			},
		},
	}

	return s.makeSuccessResponse(req, result)
}

// handleToolsList handles the tools/list request
func (s *Server) handleToolsList(ctx context.Context, req *Request) *Response {
	result := struct {
		Tools []Tool `json:"tools"`
	}{
		Tools: GetTools(),
	}

	return s.makeSuccessResponse(req, result)
}

// handleToolsCall handles the tools/call request
func (s *Server) handleToolsCall(ctx context.Context, req *Request) *Response {
	var callReq ToolCallRequest
	if err := json.Unmarshal(req.Params, &callReq); err != nil {
		return s.makeErrorResponse(req, InvalidParams, fmt.Sprintf("Invalid params: %v", err))
	}

	// Call the handler
	result, err := s.handler.HandleToolCall(ctx, callReq.Name, callReq.Arguments)
	if err != nil {
		// Return error as tool result with isError flag
		return s.makeSuccessResponse(req, ToolCallResponse{
			Content: []ToolContent{
				NewTextContent(fmt.Sprintf("Error: %v", err)),
			},
			IsError: true,
		})
	}

	return s.makeSuccessResponse(req, result)
}

// makeSuccessResponse creates a success response
func (s *Server) makeSuccessResponse(req *Request, result interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
	}
}

// makeErrorResponse creates an error response
func (s *Server) makeErrorResponse(req *Request, code int, message string) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Error:   NewError(code, message),
	}
}

// Stop stops the server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.started = false

	if s.watcher != nil {
		if err := s.watcher.Stop(); err != nil {
			return err
		}
	}

	return s.transport.Close()
}
