// Package mcp implements the Model Context Protocol server (JSON-RPC 2.0, stdio).
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// JSON-RPC 2.0 error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// JsonRpcRequest is a JSON-RPC 2.0 request.
type JsonRpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // string, number, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JsonRpcResponse is a JSON-RPC 2.0 response.
type JsonRpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JsonRpcError   `json:"error,omitempty"`
}

// JsonRpcError is a JSON-RPC 2.0 error object.
type JsonRpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// StdioTransport handles JSON-RPC 2.0 communication over stdin/stdout.
type StdioTransport struct {
	mu     sync.Mutex
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewStdioTransport creates a StdioTransport using the given streams.
func NewStdioTransport(stdin io.Reader, stdout io.Writer, stderr io.Writer) *StdioTransport {
	return &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

// Start reads newline-delimited JSON messages from stdin and calls handler for each.
// It blocks until stdin is closed or returns an error.
func (t *StdioTransport) Start(handler func([]byte)) {
	scanner := bufio.NewScanner(t.stdin)
	// Allow up to 16 MB lines (large tool outputs can be big)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		handler([]byte(line))
	}
}

// SendResponse serializes and writes a JSON-RPC response to stdout.
// Concurrent calls are serialized with a mutex so responses don't interleave.
func (t *StdioTransport) SendResponse(resp *JsonRpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Should never happen with well-formed responses
		t.logf("mcp: marshal error: %v\n", err)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.stdout.Write(data)
	_, _ = t.stdout.Write([]byte("\n"))
}

// SendResult sends a successful JSON-RPC response with the given result.
func (t *StdioTransport) SendResult(id json.RawMessage, result interface{}) {
	t.SendResponse(&JsonRpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// SendError sends a JSON-RPC error response.
func (t *StdioTransport) SendError(id json.RawMessage, code int, message string) {
	t.SendResponse(&JsonRpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JsonRpcError{
			Code:    code,
			Message: message,
		},
	})
}

func (t *StdioTransport) logf(format string, args ...interface{}) {
	if t.stderr != nil {
		_, _ = fmt.Fprintf(t.stderr, format, args...)
	}
}
