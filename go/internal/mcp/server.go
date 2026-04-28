// Package mcp implements the Model Context Protocol server (JSON-RPC 2.0, stdio).
package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/kristofer/codegraph/internal/config"
	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/graph"
)

// Project wraps a database connection for a single CodeGraph project.
type Project struct {
	rootDir   string
	db        *db.DB
	queries   *db.Queries
	traverser *graph.GraphTraverser
}

// OpenProject opens an existing CodeGraph project (requires .codegraph/codegraph.db).
func OpenProject(projectRoot string) (*Project, error) {
	dbPath := filepath.Join(projectRoot, config.DirName, "codegraph.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("mcp: no codegraph.db found at %s: %w", dbPath, err)
	}
	d, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("mcp: open db: %w", err)
	}
	q := db.NewQueries(d)
	return &Project{
		rootDir:   projectRoot,
		db:        d,
		queries:   q,
		traverser: graph.NewGraphTraverser(q),
	}, nil
}

// Close closes the project's database connection.
func (p *Project) Close() error { return p.db.Close() }

// Server is the MCP server that speaks JSON-RPC 2.0 over stdio.
type Server struct {
	transport    *StdioTransport
	projectPath  string // hint path (may not be a project root yet)
	project      *Project
	projectCache map[string]*Project
	mu           sync.Mutex
}

// NewServer creates a new MCP server. projectPath is a hint for the default project.
func NewServer(projectPath string) *Server {
	return &Server{
		transport:    NewStdioTransport(os.Stdin, os.Stdout, os.Stderr),
		projectPath:  projectPath,
		projectCache: make(map[string]*Project),
	}
}

// newServerWithTransport creates a Server using a custom transport (for testing).
func newServerWithTransport(t *StdioTransport, projectPath string) *Server {
	return &Server{
		transport:    t,
		projectPath:  projectPath,
		projectCache: make(map[string]*Project),
	}
}

// Start blocks reading JSON-RPC requests from stdin until stdin is closed.
func (s *Server) Start() error {
	s.transport.Start(s.handleBytes)
	return nil
}

// Stop closes the server and all cached projects.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projectCache {
		_ = p.Close()
	}
	s.projectCache = make(map[string]*Project)
	if s.project != nil {
		_ = s.project.Close()
		s.project = nil
	}
}

// handleBytes parses a single JSON-RPC line and dispatches it.
func (s *Server) handleBytes(data []byte) {
	var req JsonRpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.transport.SendError(json.RawMessage(`null`), ParseError, "Parse error: invalid JSON")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.transport.SendError(req.ID, InvalidRequest, "Invalid Request: not a valid JSON-RPC 2.0 message")
		return
	}

	// Notifications (no ID) — just process, don't respond
	isRequest := req.ID != nil
	if !isRequest {
		// Handle notification silently (e.g. "initialized")
		return
	}

	// Recover from panics in handlers to prevent server crash
	defer func() {
		if r := recover(); r != nil {
			s.transport.logf("mcp: panic in handler for %q: %v\n%s\n", req.Method, r, debug.Stack())
			s.transport.SendError(req.ID, InternalError, fmt.Sprintf("internal error: %v", r))
		}
	}()

	switch req.Method {
	case "initialize":
		s.handleInitialize(req.ID, req.Params)
	case "initialized":
		// no-op notification (already filtered above, but handle just in case)
	case "tools/list":
		s.handleToolsList(req.ID)
	case "tools/call":
		s.handleToolsCall(req.ID, req.Params)
	case "ping":
		s.transport.SendResult(req.ID, map[string]interface{}{})
	default:
		s.transport.SendError(req.ID, MethodNotFound, "Method not found: "+req.Method)
	}
}

// tryInitProject tries to find and open the nearest CodeGraph project.
func (s *Server) tryInitProject(projectPath string) {
	root := findNearestCodeGraphRoot(projectPath)
	if root == "" {
		s.mu.Lock()
		s.projectPath = projectPath
		s.mu.Unlock()
		return
	}
	p, err := OpenProject(root)
	if err != nil {
		s.transport.logf("[CodeGraph MCP] Failed to open project at %s: %v\n", root, err)
		return
	}
	s.mu.Lock()
	if s.project != nil {
		_ = s.project.Close()
	}
	s.project = p
	s.projectPath = root
	s.mu.Unlock()
}

// getProject returns the Project for the given optional projectPath arg.
func (s *Server) getProject(projectPathArg string) (*Project, error) {
	if projectPathArg == "" {
		s.mu.Lock()
		p := s.project
		pp := s.projectPath
		s.mu.Unlock()
		if p == nil {
			// Try lazy init
			if pp != "" {
				s.tryInitProject(pp)
				s.mu.Lock()
				p = s.project
				s.mu.Unlock()
			}
			if p == nil {
				return nil, fmt.Errorf("CodeGraph not initialized for this project. Run 'codegraph init' first.")
			}
		}
		return p, nil
	}

	s.mu.Lock()
	cached, ok := s.projectCache[projectPathArg]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	root := findNearestCodeGraphRoot(projectPathArg)
	if root == "" {
		return nil, fmt.Errorf("CodeGraph not initialized in %s. Run 'codegraph init' in that project first.", projectPathArg)
	}

	s.mu.Lock()
	cached, ok = s.projectCache[root]
	s.mu.Unlock()
	if ok {
		s.mu.Lock()
		s.projectCache[projectPathArg] = cached
		s.mu.Unlock()
		return cached, nil
	}

	p, err := OpenProject(root)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.projectCache[root] = p
	if projectPathArg != root {
		s.projectCache[projectPathArg] = p
	}
	s.mu.Unlock()
	return p, nil
}

// handleInitialize handles the MCP initialize request.
func (s *Server) handleInitialize(id json.RawMessage, params json.RawMessage) {
	var p struct {
		RootURI          string `json:"rootUri"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	_ = json.Unmarshal(params, &p)

	projectPath := s.projectPath
	if p.RootURI != "" {
		if pp, err := fileUriToPath(p.RootURI); err == nil {
			projectPath = pp
		}
	} else if len(p.WorkspaceFolders) > 0 {
		if pp, err := fileUriToPath(p.WorkspaceFolders[0].URI); err == nil {
			projectPath = pp
		}
	}
	if projectPath == "" {
		projectPath, _ = os.Getwd()
	}

	s.tryInitProject(projectPath)

	s.transport.SendResult(id, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "codegraph", "version": "0.1.0"},
	})
}

// handleToolsList handles the tools/list request.
func (s *Server) handleToolsList(id json.RawMessage) {
	s.mu.Lock()
	p := s.project
	s.mu.Unlock()
	s.transport.SendResult(id, map[string]interface{}{
		"tools": getTools(p),
	})
}

// handleToolsCall handles the tools/call request.
func (s *Server) handleToolsCall(id json.RawMessage, params json.RawMessage) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		s.transport.SendError(id, InvalidParams, "Missing or invalid tool name")
		return
	}

	// Validate tool exists
	found := false
	for _, t := range toolDefs {
		if t.Name == p.Name {
			found = true
			break
		}
	}
	if !found {
		s.transport.SendError(id, InvalidParams, "Unknown tool: "+p.Name)
		return
	}

	args := p.Arguments
	if args == nil {
		args = make(map[string]interface{})
	}

	// Determine which project to use
	projectPathArg, _ := args["projectPath"].(string)
	proj, err := s.getProject(projectPathArg)
	if err != nil {
		s.transport.SendResult(id, errorResult(err.Error()))
		return
	}

	result := s.executeTool(p.Name, args, proj)
	s.transport.SendResult(id, result)
}

// executeTool dispatches a tool call, recovering from panics.
func (s *Server) executeTool(name string, args map[string]interface{}, p *Project) (result ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			s.transport.logf("mcp: panic in tool %q: %v\n%s\n", name, r, debug.Stack())
			result = errorResult(fmt.Sprintf("tool execution failed: %v", r))
		}
	}()
	switch name {
	case "codegraph_search":
		return executeSearch(p, args)
	case "codegraph_context":
		return executeContext(p, args)
	case "codegraph_callers":
		return executeCallers(p, args)
	case "codegraph_callees":
		return executeCallees(p, args)
	case "codegraph_impact":
		return executeImpact(p, args)
	case "codegraph_node":
		return executeNode(p, args)
	case "codegraph_explore":
		return executeExplore(p, args)
	case "codegraph_status":
		return executeStatus(p)
	case "codegraph_files":
		return executeFiles(p, args)
	default:
		return errorResult("Unknown tool: " + name)
	}
}

// findNearestCodeGraphRoot walks up from startPath looking for a directory
// that contains a .codegraph/codegraph.db file.
func findNearestCodeGraphRoot(startPath string) string {
	current := filepath.Clean(startPath)
	for {
		dbPath := filepath.Join(current, config.DirName, "codegraph.db")
		if _, err := os.Stat(dbPath); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

// fileUriToPath converts a file:// URI to a filesystem path.
func fileUriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		p := strings.TrimPrefix(uri, "file:///")
		p = strings.TrimPrefix(p, "file://")
		return filepath.FromSlash(p), nil
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return u.Path, nil
	}
	// On Windows: /C:/path → C:/path (only for valid ASCII drive letters)
	if len(p) > 2 && p[0] == '/' && p[2] == ':' &&
		((p[1] >= 'A' && p[1] <= 'Z') || (p[1] >= 'a' && p[1] <= 'z')) {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}
