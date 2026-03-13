package mcp

import (
	"encoding/json"
	"fmt"
)

// MCP Protocol Types - Based on Model Context Protocol 2024-11-05 spec
// https://modelcontextprotocol.io/specification/2024-11-05/

// JSON-RPC 2.0 Message Types

// Request represents a JSON-RPC request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response
type Response struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      interface{}  `json:"id,omitempty"`
	Result  interface{}  `json:"result,omitempty"`
	Error   *ErrorObject `json:"error,omitempty"`
}

// Notification represents a JSON-RPC notification (no ID)
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ErrorObject represents a JSON-RPC error
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
	ServerError    = -32000
)

// NewError creates a new JSON-RPC error
func NewError(code int, message string) *ErrorObject {
	return &ErrorObject{
		Code:    code,
		Message: message,
	}
}

// MCP Protocol Types

// InitializeRequest represents the initialize request params
type InitializeRequest struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// InitializeResponse represents the initialize response result
type InitializeResponse struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// Implementation represents client/server info
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities represents client capabilities
type ClientCapabilities struct {
	// Experimental capabilities
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	// Roots capability (optional)
	Roots *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"roots,omitempty"`
	// Sampling capability (optional)
	Sampling map[string]interface{} `json:"sampling,omitempty"`
}

// ServerCapabilities represents server capabilities
type ServerCapabilities struct {
	// Experimental capabilities
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	// Logging capability
	Logging map[string]interface{} `json:"logging,omitempty"`
	// Prompts capability (not implemented)
	Prompts *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"prompts,omitempty"`
	// Resources capability (optional)
	Resources *struct {
		Subscribe   bool `json:"subscribe,omitempty"`
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"resources,omitempty"`
	// Tools capability
	Tools *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"tools,omitempty"`
}

// Tool Types

// Tool represents an available tool
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema represents the JSON Schema for tool input
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// Property represents a JSON Schema property
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolCallRequest represents a tools/call request params
type ToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolCallResponse represents a tools/call response result
type ToolCallResponse struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent represents content in a tool response
type ToolContent struct {
	Type string `json:"type"`
	// For type="text"
	Text string `json:"text,omitempty"`
	// For type="json" (MCP extension for structured data)
	JSON interface{} `json:"json,omitempty"`
	// For type="image" (not implemented)
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	// For type="resource" (not implemented)
	Resource *ResourceContent `json:"resource,omitempty"`
}

// ResourceContent represents embedded resource content
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// NewTextContent creates a text content item
func NewTextContent(text string) ToolContent {
	return ToolContent{
		Type: "text",
		Text: text,
	}
}

// NewJSONContent creates a JSON content item
func NewJSONContent(data interface{}) ToolContent {
	return ToolContent{
		Type: "json",
		JSON: data,
	}
}

// Error helpers

// Error returns an error response
func (r *Response) ErrorResponse(code int, message string) *Response {
	r.Error = NewError(code, message)
	r.Result = nil
	return r
}

// String returns a string representation of the ID
func (r *Request) StringID() string {
	if r.ID == nil {
		return ""
	}
	switch id := r.ID.(type) {
	case string:
		return id
	case float64:
		return fmt.Sprintf("%d", int64(id))
	case int:
		return fmt.Sprintf("%d", id)
	case int64:
		return fmt.Sprintf("%d", id)
	default:
		return fmt.Sprintf("%v", id)
	}
}

// IsNotification returns true if the request is a notification (no ID)
func (r *Request) IsNotification() bool {
	return r.ID == nil
}
