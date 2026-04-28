package graph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
)

// ===========================================================================
// Helpers
// ===========================================================================

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func sampleNode(id, name, filePath string, kind types.NodeKind) *types.Node {
	return &types.Node{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: filePath + "::" + name,
		FilePath:      filePath,
		Language:      types.TypeScript,
		StartLine:     1,
		EndLine:       10,
		UpdatedAt:     time.Now().UnixMilli(),
	}
}

func insertNode(t *testing.T, q *db.Queries, n *types.Node) {
	t.Helper()
	require.NoError(t, q.UpsertNode(n))
}

func insertEdge(t *testing.T, q *db.Queries, src, tgt string, kind types.EdgeKind) {
	t.Helper()
	require.NoError(t, q.InsertEdge(&types.Edge{Source: src, Target: tgt, Kind: kind}))
}

// buildCallChain inserts:
//
//	a → b → c  (calls edges)
func buildCallChain(t *testing.T, q *db.Queries) {
	insertNode(t, q, sampleNode("a", "funcA", "src/a.ts", types.NodeKindFunction))
	insertNode(t, q, sampleNode("b", "funcB", "src/b.ts", types.NodeKindFunction))
	insertNode(t, q, sampleNode("c", "funcC", "src/c.ts", types.NodeKindFunction))
	insertEdge(t, q, "a", "b", types.EdgeKindCalls)
	insertEdge(t, q, "b", "c", types.EdgeKindCalls)
}

// ===========================================================================
// GraphTraverser — BFS
// ===========================================================================

func TestTraverseBFS_Chain(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.TraverseBFS("a", TraversalOptions{
		Direction:    types.EdgeDirectionOutgoing,
		IncludeStart: true,
	})
	require.NoError(t, err)
	require.NotNil(t, sg)

	// All three nodes should be reachable from a
	assert.Contains(t, sg.Nodes, "a")
	assert.Contains(t, sg.Nodes, "b")
	assert.Contains(t, sg.Nodes, "c")
	assert.Len(t, sg.Edges, 2)
}

func TestTraverseBFS_MaxDepth(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.TraverseBFS("a", TraversalOptions{
		Direction:    types.EdgeDirectionOutgoing,
		MaxDepth:     1,
		IncludeStart: true,
	})
	require.NoError(t, err)

	// With maxDepth=1, only a and b are visited; c is beyond depth
	assert.Contains(t, sg.Nodes, "a")
	assert.Contains(t, sg.Nodes, "b")
	assert.NotContains(t, sg.Nodes, "c")
}

func TestTraverseBFS_NotFound(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.TraverseBFS("nonexistent", TraversalOptions{IncludeStart: true})
	require.NoError(t, err)
	assert.Empty(t, sg.Nodes)
}

// ===========================================================================
// GraphTraverser — DFS
// ===========================================================================

func TestTraverseDFS_Chain(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.TraverseDFS("a", TraversalOptions{
		Direction:    types.EdgeDirectionOutgoing,
		IncludeStart: true,
	})
	require.NoError(t, err)

	assert.Contains(t, sg.Nodes, "a")
	assert.Contains(t, sg.Nodes, "b")
	assert.Contains(t, sg.Nodes, "c")
}

// ===========================================================================
// GraphTraverser — GetCallers / GetCallees
// ===========================================================================

func TestGetCallees(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	callees, err := traverser.GetCallees("a", 2)
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, cr := range callees {
		ids[cr.Node.ID] = true
	}
	assert.True(t, ids["b"], "b should be a callee of a")
	assert.True(t, ids["c"], "c should be a transitive callee of a with depth 2")
}

func TestGetCallers(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	callers, err := traverser.GetCallers("b", 1)
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, cr := range callers {
		ids[cr.Node.ID] = true
	}
	assert.True(t, ids["a"], "a should be a caller of b")
}

// ===========================================================================
// GraphTraverser — GetCallGraph
// ===========================================================================

func TestGetCallGraph(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.GetCallGraph("b", 1)
	require.NoError(t, err)

	// b should be the focal node with a as caller and c as callee
	assert.Contains(t, sg.Nodes, "a")
	assert.Contains(t, sg.Nodes, "b")
	assert.Contains(t, sg.Nodes, "c")
}

// ===========================================================================
// GraphTraverser — GetImpactRadius
// ===========================================================================

func TestGetImpactRadius(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.GetImpactRadius("c", 3)
	require.NoError(t, err)

	// Changing c impacts b (which calls c) and a (which calls b)
	assert.Contains(t, sg.Nodes, "c")
	assert.Contains(t, sg.Nodes, "b")
	assert.Contains(t, sg.Nodes, "a")
}

// ===========================================================================
// GraphTraverser — FindPath
// ===========================================================================

func TestFindPath(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	traverser := NewGraphTraverser(q)

	// Path from a to c through b
	path, err := traverser.FindPath("a", "c", nil)
	require.NoError(t, err)
	require.NotNil(t, path)
	assert.Equal(t, 3, len(path), "path should have 3 nodes: a→b→c")

	// No path in reverse direction
	revPath, err := traverser.FindPath("c", "a", nil)
	require.NoError(t, err)
	assert.Nil(t, revPath)
}

// ===========================================================================
// GraphTraverser — GetAncestors / GetChildren
// ===========================================================================

func TestGetAncestorsAndChildren(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	file := sampleNode("file1", "main.ts", "src/main.ts", types.NodeKindFile)
	cls := sampleNode("cls1", "MyClass", "src/main.ts", types.NodeKindClass)
	meth := sampleNode("mth1", "doWork", "src/main.ts", types.NodeKindMethod)
	insertNode(t, q, file)
	insertNode(t, q, cls)
	insertNode(t, q, meth)
	insertEdge(t, q, "file1", "cls1", types.EdgeKindContains)
	insertEdge(t, q, "cls1", "mth1", types.EdgeKindContains)

	traverser := NewGraphTraverser(q)

	ancestors, err := traverser.GetAncestors("mth1")
	require.NoError(t, err)
	require.Len(t, ancestors, 2)
	assert.Equal(t, "cls1", ancestors[0].ID)
	assert.Equal(t, "file1", ancestors[1].ID)

	children, err := traverser.GetChildren("cls1")
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "mth1", children[0].ID)
}

// ===========================================================================
// GraphTraverser — GetInheritanceChain
// ===========================================================================

func TestGetInheritanceChain(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	base := sampleNode("base", "BaseClass", "src/base.ts", types.NodeKindClass)
	child := sampleNode("child", "ChildClass", "src/child.ts", types.NodeKindClass)
	grandchild := sampleNode("grand", "GrandChildClass", "src/grand.ts", types.NodeKindClass)
	insertNode(t, q, base)
	insertNode(t, q, child)
	insertNode(t, q, grandchild)
	insertEdge(t, q, "child", "base", types.EdgeKindExtends)
	insertEdge(t, q, "grand", "child", types.EdgeKindExtends)

	traverser := NewGraphTraverser(q)
	sg, err := traverser.GetInheritanceChain("child")
	require.NoError(t, err)

	assert.Contains(t, sg.Nodes, "base")
	assert.Contains(t, sg.Nodes, "child")
	assert.Contains(t, sg.Nodes, "grand")
}

// ===========================================================================
// GraphQueryManager — GetContext
// ===========================================================================

func TestGetContext(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	file := sampleNode("file1", "main.ts", "src/main.ts", types.NodeKindFile)
	fn := sampleNode("fn1", "myFunc", "src/main.ts", types.NodeKindFunction)
	caller := sampleNode("caller", "callerFunc", "src/main.ts", types.NodeKindFunction)
	insertNode(t, q, file)
	insertNode(t, q, fn)
	insertNode(t, q, caller)
	insertEdge(t, q, "file1", "fn1", types.EdgeKindContains)
	insertEdge(t, q, "caller", "fn1", types.EdgeKindCalls)

	gqm := NewGraphQueryManager(q)
	ctx, err := gqm.GetContext("fn1")
	require.NoError(t, err)
	require.NotNil(t, ctx)
	assert.Equal(t, "fn1", ctx.Focal.ID)
	require.Len(t, ctx.IncomingRefs, 1)
	assert.Equal(t, "caller", ctx.IncomingRefs[0].Node.ID)
}

// ===========================================================================
// GraphQueryManager — GetFileDependencies / GetFileDependents
// ===========================================================================

func TestFileDependencies(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	fileA := sampleNode("fileA", "a.ts", "src/a.ts", types.NodeKindFile)
	fileB := sampleNode("fileB", "b.ts", "src/b.ts", types.NodeKindFile)
	insertNode(t, q, fileA)
	insertNode(t, q, fileB)
	insertEdge(t, q, "fileA", "fileB", types.EdgeKindImports)

	gqm := NewGraphQueryManager(q)

	deps, err := gqm.GetFileDependencies("src/a.ts")
	require.NoError(t, err)
	assert.Contains(t, deps, "src/b.ts")

	dependents, err := gqm.GetFileDependents("src/b.ts")
	require.NoError(t, err)
	assert.Contains(t, dependents, "src/a.ts")
}

// ===========================================================================
// GraphQueryManager — FindDeadCode
// ===========================================================================

func TestFindDeadCode(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	// A function that is never called (dead code)
	dead := sampleNode("dead", "unusedFunc", "src/a.ts", types.NodeKindFunction)
	dead.IsExported = false
	insertNode(t, q, dead)

	// A function that IS called
	live := sampleNode("live", "usedFunc", "src/a.ts", types.NodeKindFunction)
	live.IsExported = false
	insertNode(t, q, live)

	caller := sampleNode("caller", "main", "src/main.ts", types.NodeKindFunction)
	insertNode(t, q, caller)
	insertEdge(t, q, "caller", "live", types.EdgeKindCalls)

	gqm := NewGraphQueryManager(q)
	deadCode, err := gqm.FindDeadCode(nil)
	require.NoError(t, err)

	deadIDs := make(map[string]bool)
	for _, n := range deadCode {
		deadIDs[n.ID] = true
	}
	assert.True(t, deadIDs["dead"], "unusedFunc should be dead code")
	assert.False(t, deadIDs["live"], "usedFunc should NOT be dead code")
}

// ===========================================================================
// GraphQueryManager — GetNodeMetrics
// ===========================================================================

func TestGetNodeMetrics(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)
	buildCallChain(t, q)

	gqm := NewGraphQueryManager(q)
	m, err := gqm.GetNodeMetrics("b")
	require.NoError(t, err)
	require.NotNil(t, m)

	// b has 1 incoming call (from a) and 1 outgoing call (to c)
	assert.Equal(t, 1, m.CallerCount)
	assert.Equal(t, 1, m.CallCount)
}

// ===========================================================================
// GraphQueryManager — FindByQualifiedNamePattern
// ===========================================================================

func TestFindByQualifiedNamePattern(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	insertNode(t, q, sampleNode("fn1", "getUser", "src/users.ts", types.NodeKindFunction))
	insertNode(t, q, sampleNode("fn2", "getPost", "src/posts.ts", types.NodeKindFunction))
	insertNode(t, q, sampleNode("fn3", "createUser", "src/users.ts", types.NodeKindFunction))

	gqm := NewGraphQueryManager(q)
	results, err := gqm.FindByQualifiedNamePattern("src/users.ts::*")
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, n := range results {
		ids[n.ID] = true
	}
	assert.True(t, ids["fn1"])
	assert.True(t, ids["fn3"])
	assert.False(t, ids["fn2"])
}

// ===========================================================================
// GraphQueryManager — FindCircularDependencies
// ===========================================================================

func TestFindCircularDependencies(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	// Create file records (needed for GetAllFiles)
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path:      "src/a.ts",
		Language:  types.TypeScript,
		IndexedAt: time.Now().UnixMilli(),
	}))
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path:      "src/b.ts",
		Language:  types.TypeScript,
		IndexedAt: time.Now().UnixMilli(),
	}))

	// Create file nodes
	fileA := sampleNode("fileA", "a.ts", "src/a.ts", types.NodeKindFile)
	fileB := sampleNode("fileB", "b.ts", "src/b.ts", types.NodeKindFile)
	insertNode(t, q, fileA)
	insertNode(t, q, fileB)

	// a → b → a (cycle)
	insertEdge(t, q, "fileA", "fileB", types.EdgeKindImports)
	insertEdge(t, q, "fileB", "fileA", types.EdgeKindImports)

	gqm := NewGraphQueryManager(q)
	cycles, err := gqm.FindCircularDependencies()
	require.NoError(t, err)
	assert.NotEmpty(t, cycles)
}

// ===========================================================================
// GraphQueryManager — GetModuleStructure
// ===========================================================================

func TestGetModuleStructure(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	for _, path := range []string{"src/a.ts", "src/b.ts", "lib/c.ts"} {
		require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
			Path:      path,
			Language:  types.TypeScript,
			IndexedAt: time.Now().UnixMilli(),
		}))
	}

	gqm := NewGraphQueryManager(q)
	structure, err := gqm.GetModuleStructure()
	require.NoError(t, err)

	assert.Contains(t, structure, "src")
	assert.Contains(t, structure, "lib")
	assert.Len(t, structure["src"], 2)
	assert.Len(t, structure["lib"], 1)
}
