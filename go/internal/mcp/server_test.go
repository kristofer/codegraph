package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kristofer/codegraph/internal/config"
	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestProject creates a temporary project directory with an initialized
// codegraph.db and returns a *Project and the db for data seeding.
func newTestProject(t *testing.T) (*Project, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	cgDir := filepath.Join(dir, config.DirName)
	require.NoError(t, os.MkdirAll(cgDir, 0o755))

	dbPath := filepath.Join(cgDir, "codegraph.db")
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	q := db.NewQueries(d)
	p := &Project{
		rootDir: dir,
		db:      d,
		queries: q,
	}
	return p, d, dir
}

// sendRequest writes a JSON-RPC request line to the writer and returns
// the parsed response from the reader.
func sendRequest(t *testing.T, w io.Writer, r *bufio.Reader, id interface{}, method string, params interface{}) map[string]interface{} {
	t.Helper()
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = w.Write(append(data, '\n'))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &resp))
	return resp
}

// newTestServer creates a Server backed by pipe-connected stdin/stdout for testing.
// It returns (server, stdinWriter, stdoutReader, projectDir).
func newTestServerWithPipes(t *testing.T) (*Server, io.WriteCloser, *bufio.Reader, string) {
	t.Helper()
	// Create temp project dir with db
	_, _, dir := newTestProject(t)

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrW, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)

	transport := NewStdioTransport(stdinR, stdoutW, stderrW)
	srv := newServerWithTransport(transport, dir)

	// Run server in background
	go func() {
		srv.Start()
		_ = stdoutW.Close()
	}()

	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdinR.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrW.Close()
		srv.Stop()
	})

	return srv, stdinW, bufio.NewReader(stdoutR), dir
}

// =============================================================================
// Unit tests
// =============================================================================

func TestFileUriToPath(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantSub  string // substring that should appear in result
	}{
		{
			name:    "linux absolute",
			uri:     "file:///home/user/project",
			wantSub: "home",
		},
		{
			name:    "encoded space",
			uri:     "file:///home/user/my%20project",
			wantSub: "my project",
		},
		{
			name:    "fallback non-standard",
			uri:     "file://localhost/some/path",
			wantSub: "some",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fileUriToPath(tc.uri)
			require.NoError(t, err)
			assert.Contains(t, got, tc.wantSub)
		})
	}
}

func TestFindNearestCodeGraphRoot(t *testing.T) {
	dir := t.TempDir()
	cgDir := filepath.Join(dir, config.DirName)
	require.NoError(t, os.MkdirAll(cgDir, 0o755))

	// No db yet → should not find
	assert.Empty(t, findNearestCodeGraphRoot(dir))

	// Create the db file
	dbPath := filepath.Join(cgDir, "codegraph.db")
	f, err := os.Create(dbPath)
	require.NoError(t, err)
	_ = f.Close()

	// Now should find
	got := findNearestCodeGraphRoot(dir)
	assert.Equal(t, dir, got)

	// Walking up from a child dir should find the parent
	sub := filepath.Join(dir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	got = findNearestCodeGraphRoot(sub)
	assert.Equal(t, dir, got)
}

func TestGetExploreBudget(t *testing.T) {
	assert.Equal(t, 1, getExploreBudget(0))
	assert.Equal(t, 1, getExploreBudget(499))
	assert.Equal(t, 2, getExploreBudget(500))
	assert.Equal(t, 2, getExploreBudget(4999))
	assert.Equal(t, 3, getExploreBudget(5000))
	assert.Equal(t, 4, getExploreBudget(15000))
	assert.Equal(t, 5, getExploreBudget(25000))
}

// =============================================================================
// Integration tests via pipes
// =============================================================================

func TestMCPInitialize(t *testing.T) {
	_, w, r, dir := newTestServerWithPipes(t)

	resp := sendRequest(t, w, r, 1, "initialize", map[string]interface{}{
		"rootUri": "file://" + filepath.ToSlash(dir),
	})

	assert.Equal(t, "2.0", resp["jsonrpc"])
	assert.Equal(t, float64(1), resp["id"])
	assert.Nil(t, resp["error"])

	result, ok := resp["result"].(map[string]interface{})
	require.True(t, ok, "expected result map")
	assert.Equal(t, "2024-11-05", result["protocolVersion"])
	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "codegraph", serverInfo["name"])
}

func TestMCPToolsList(t *testing.T) {
	_, w, r, _ := newTestServerWithPipes(t)

	resp := sendRequest(t, w, r, 2, "tools/list", nil)

	assert.Nil(t, resp["error"])
	result, ok := resp["result"].(map[string]interface{})
	require.True(t, ok)

	toolsRaw, ok := result["tools"].([]interface{})
	require.True(t, ok)
	assert.Len(t, toolsRaw, 9, "expected exactly 9 tools")

	names := make([]string, len(toolsRaw))
	for i, toolRaw := range toolsRaw {
		tool, ok := toolRaw.(map[string]interface{})
		require.True(t, ok)
		names[i], _ = tool["name"].(string)
	}
	assert.Contains(t, names, "codegraph_search")
	assert.Contains(t, names, "codegraph_context")
	assert.Contains(t, names, "codegraph_callers")
	assert.Contains(t, names, "codegraph_callees")
	assert.Contains(t, names, "codegraph_impact")
	assert.Contains(t, names, "codegraph_node")
	assert.Contains(t, names, "codegraph_explore")
	assert.Contains(t, names, "codegraph_status")
	assert.Contains(t, names, "codegraph_files")
}

func TestMCPPing(t *testing.T) {
	_, w, r, _ := newTestServerWithPipes(t)

	resp := sendRequest(t, w, r, 3, "ping", nil)
	assert.Nil(t, resp["error"])
	_, ok := resp["result"].(map[string]interface{})
	assert.True(t, ok)
}

func TestMCPMethodNotFound(t *testing.T) {
	_, w, r, _ := newTestServerWithPipes(t)

	resp := sendRequest(t, w, r, 4, "no_such_method", nil)
	assert.NotNil(t, resp["error"])
	errObj, ok := resp["error"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(MethodNotFound), errObj["code"])
}

func TestMCPSearchTool(t *testing.T) {
	p, _, dir := newTestProject(t)

	// Seed a node
	node := &types.Node{
		ID:            "test-node-1",
		Kind:          types.NodeKindFunction,
		Name:          "myTestFunction",
		QualifiedName: "src/main.go::myTestFunction",
		FilePath:      "src/main.go",
		Language:      types.Go,
		StartLine:     10,
		EndLine:       20,
	}
	require.NoError(t, p.queries.UpsertNode(node))
	_ = p.Close()

	_, w, r, _ := newTestServerWithPipesFromDir(t, dir)

	resp := sendRequest(t, w, r, 5, "tools/call", map[string]interface{}{
		"name":      "codegraph_search",
		"arguments": map[string]interface{}{"query": "myTestFunction"},
	})

	assert.Nil(t, resp["error"])
	result, ok := resp["result"].(map[string]interface{})
	require.True(t, ok)
	content, ok := result["content"].([]interface{})
	require.True(t, ok)
	require.Len(t, content, 1)
	item, ok := content[0].(map[string]interface{})
	require.True(t, ok)
	text, _ := item["text"].(string)
	assert.Contains(t, text, "myTestFunction")
}

func TestMCPStatusTool(t *testing.T) {
	_, w, r, _ := newTestServerWithPipes(t)

	resp := sendRequest(t, w, r, 6, "tools/call", map[string]interface{}{
		"name":      "codegraph_status",
		"arguments": map[string]interface{}{},
	})

	assert.Nil(t, resp["error"])
	result, ok := resp["result"].(map[string]interface{})
	require.True(t, ok)
	content, ok := result["content"].([]interface{})
	require.True(t, ok)
	require.Len(t, content, 1)
	item, ok := content[0].(map[string]interface{})
	require.True(t, ok)
	text, _ := item["text"].(string)
	assert.Contains(t, text, "CodeGraph Status")
}

func TestMCPContextTool(t *testing.T) {
	p, _, dir := newTestProject(t)
	node := &types.Node{
		ID:            "ctx-node-1",
		Kind:          types.NodeKindFunction,
		Name:          "contextFunction",
		QualifiedName: "src/foo.go::contextFunction",
		FilePath:      "src/foo.go",
		Language:      types.Go,
		StartLine:     1,
		EndLine:       5,
	}
	require.NoError(t, p.queries.UpsertNode(node))
	_ = p.Close()

	_, w, r, _ := newTestServerWithPipesFromDir(t, dir)

	resp := sendRequest(t, w, r, 7, "tools/call", map[string]interface{}{
		"name":      "codegraph_context",
		"arguments": map[string]interface{}{"task": "contextFunction"},
	})

	assert.Nil(t, resp["error"])
	result, ok := resp["result"].(map[string]interface{})
	require.True(t, ok)
	content, ok := result["content"].([]interface{})
	require.True(t, ok)
	require.Len(t, content, 1)
	item, ok := content[0].(map[string]interface{})
	require.True(t, ok)
	text, _ := item["text"].(string)
	assert.Contains(t, text, "contextFunction")
}

// newTestServerWithPipesFromDir creates a server that targets a specific project dir.
func newTestServerWithPipesFromDir(t *testing.T, dir string) (*Server, io.WriteCloser, *bufio.Reader, string) {
	t.Helper()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrW, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)

	transport := NewStdioTransport(stdinR, stdoutW, stderrW)
	srv := newServerWithTransport(transport, dir)

	// Pre-init the project
	srv.tryInitProject(dir)

	go func() {
		srv.Start()
		_ = stdoutW.Close()
	}()

	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdinR.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrW.Close()
		srv.Stop()
	})

	return srv, stdinW, bufio.NewReader(stdoutR), dir
}
