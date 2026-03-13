package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/tuffrabit/tuffman/internal/config"
	"github.com/tuffrabit/tuffman/internal/storage"
)

// ClientConfig holds client-only MCP server configuration
type ClientConfig struct {
	Root      string
	Transport string
	DB        *storage.DB
	Config    *config.Config
}

// ClientServer is a read-only MCP server for querying an existing index.
// It does not perform any indexing or file watching - it assumes a daemon
// or manual index operation has already populated the database.
type ClientServer struct {
	config    *ClientConfig
	handler   *Handler
	transport Transport
	mu        sync.RWMutex
	started   bool
}

// NewClientServer creates a new read-only MCP client server
func NewClientServer(clientConfig *ClientConfig) (*ClientServer, error) {
	// Use provided config or create default
	cfg := clientConfig.Config
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Create handler
	handler := NewHandler(clientConfig.DB, clientConfig.Root)

	// Create transport
	var transport Transport
	switch clientConfig.Transport {
	case "stdio":
		transport = NewStdioTransport()
	default:
		return nil, fmt.Errorf("unsupported transport: %s", clientConfig.Transport)
	}

	s := &ClientServer{
		config:    clientConfig,
		handler:   handler,
		transport: transport,
	}

	return s, nil
}

// Run starts the MCP client server
func (s *ClientServer) Run(ctx context.Context) error {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()

	// Start processing messages
	return s.processMessages(ctx)
}

// processMessages handles incoming MCP messages
func (s *ClientServer) processMessages(ctx context.Context) error {
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
func (s *ClientServer) handleMessage(ctx context.Context, data []byte) (*Response, error) {
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
func (s *ClientServer) handleInitialize(ctx context.Context, req *Request) *Response {
	var initReq InitializeRequest
	if err := json.Unmarshal(req.Params, &initReq); err != nil {
		return s.makeErrorResponse(req, InvalidParams, fmt.Sprintf("Invalid params: %v", err))
	}

	// Build response
	result := InitializeResponse{
		ProtocolVersion: MCPProtocolVersion,
		ServerInfo: Implementation{
			Name:    "tuffman-mcp-client",
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
func (s *ClientServer) handleToolsList(ctx context.Context, req *Request) *Response {
	result := struct {
		Tools []Tool `json:"tools"`
	}{
		Tools: GetTools(),
	}

	return s.makeSuccessResponse(req, result)
}

// handleToolsCall handles the tools/call request
func (s *ClientServer) handleToolsCall(ctx context.Context, req *Request) *Response {
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
func (s *ClientServer) makeSuccessResponse(req *Request, result interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Result:  result,
	}
}

// makeErrorResponse creates an error response
func (s *ClientServer) makeErrorResponse(req *Request, code int, message string) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Error:   NewError(code, message),
	}
}

// Stop stops the server
func (s *ClientServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.started = false
	return s.transport.Close()
}
