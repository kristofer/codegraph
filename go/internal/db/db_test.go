package db

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristofer/codegraph/internal/types"
)

// ===========================================================================
// Helpers
// ===========================================================================

// openTestDB opens a fresh in-memory database for use in a single test.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// sampleNode builds a minimal valid Node for testing.
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

// ===========================================================================
// TestDBOpen
// ===========================================================================

// TestDBOpen verifies that OpenMemory succeeds and the schema is applied.
func TestDBOpen(t *testing.T) {
	db := openTestDB(t)
	assert.NotNil(t, db.SQLDB())

	// Schema version should be 1 (set by schema.sql INSERT).
	ver, err := db.SchemaVersion()
	require.NoError(t, err)
	assert.Equal(t, currentSchemaVersion, ver)
}

// TestDBOpenFile verifies that Open creates a file-backed database.
func TestDBOpenFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/test.db"

	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	assert.Equal(t, dbPath, db.Path())

	ver, err := db.SchemaVersion()
	require.NoError(t, err)
	assert.Equal(t, currentSchemaVersion, ver)

	// Reopen — should not fail.
	db.Close()
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()
}

// ===========================================================================
// TestNodeUpsertAndGet
// ===========================================================================

func TestNodeUpsertAndGet(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	t.Run("insert and retrieve by id", func(t *testing.T) {
		node := sampleNode("node1", "myFunc", "src/main.ts", types.NodeKindFunction)
		require.NoError(t, q.UpsertNode(node))

		got, err := q.GetNodeByID("node1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "node1", got.ID)
		assert.Equal(t, types.NodeKindFunction, got.Kind)
		assert.Equal(t, "myFunc", got.Name)
		assert.Equal(t, "src/main.ts", got.FilePath)
		assert.Equal(t, types.TypeScript, got.Language)
	})

	t.Run("upsert updates existing node", func(t *testing.T) {
		node := sampleNode("node2", "MyClass", "src/cls.ts", types.NodeKindClass)
		require.NoError(t, q.UpsertNode(node))

		node.Name = "MyClassRenamed"
		node.StartLine = 5
		require.NoError(t, q.UpsertNode(node))

		got, err := q.GetNodeByID("node2")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "MyClassRenamed", got.Name)
		assert.Equal(t, 5, got.StartLine)
	})

	t.Run("not found returns nil", func(t *testing.T) {
		got, err := q.GetNodeByID("does-not-exist")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("optional fields round-trip", func(t *testing.T) {
		doc := "This is a docstring"
		sig := "func myFunc() int"
		vis := types.VisibilityPublic
		node := &types.Node{
			ID:             "node3",
			Kind:           types.NodeKindMethod,
			Name:           "calculate",
			QualifiedName:  "src/math.ts::Math.calculate",
			FilePath:       "src/math.ts",
			Language:       types.TypeScript,
			StartLine:      20,
			EndLine:        30,
			Docstring:      &doc,
			Signature:      &sig,
			Visibility:     &vis,
			IsExported:     true,
			IsAsync:        true,
			Decorators:     []string{"@override"},
			TypeParameters: []string{"T", "U"},
			UpdatedAt:      time.Now().UnixMilli(),
		}
		require.NoError(t, q.UpsertNode(node))

		got, err := q.GetNodeByID("node3")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.Docstring)
		assert.Equal(t, doc, *got.Docstring)
		require.NotNil(t, got.Signature)
		assert.Equal(t, sig, *got.Signature)
		require.NotNil(t, got.Visibility)
		assert.Equal(t, vis, *got.Visibility)
		assert.True(t, got.IsExported)
		assert.True(t, got.IsAsync)
		assert.Equal(t, []string{"@override"}, got.Decorators)
		assert.Equal(t, []string{"T", "U"}, got.TypeParameters)
	})

	t.Run("missing required fields returns error", func(t *testing.T) {
		bad := &types.Node{ID: "bad", Kind: types.NodeKindFunction} // missing Name, FilePath, Language
		err := q.UpsertNode(bad)
		assert.Error(t, err)
	})
}

// TestGetNodesByFile verifies retrieval of all nodes in a file.
func TestGetNodesByFile(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	file := "src/service.ts"
	nodes := []*types.Node{
		sampleNode("n1", "ServiceClass", file, types.NodeKindClass),
		sampleNode("n2", "doWork", file, types.NodeKindMethod),
		sampleNode("n3", "helper", file, types.NodeKindFunction),
	}
	require.NoError(t, q.UpsertNodes(nodes))

	// Add a node in a different file — should not appear.
	require.NoError(t, q.UpsertNode(sampleNode("other", "OtherFunc", "src/other.ts", types.NodeKindFunction)))

	got, err := q.GetNodesByFile(file)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	ids := make([]string, len(got))
	for i, n := range got {
		ids[i] = n.ID
	}
	assert.ElementsMatch(t, []string{"n1", "n2", "n3"}, ids)
}

// ===========================================================================
// TestEdgeInsertAndGet
// ===========================================================================

func TestEdgeInsertAndGet(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	// Insert prerequisite nodes (edges reference them via FK).
	a := sampleNode("a", "A", "src/a.ts", types.NodeKindFunction)
	b := sampleNode("b", "B", "src/b.ts", types.NodeKindFunction)
	require.NoError(t, q.UpsertNodes([]*types.Node{a, b}))

	edge := &types.Edge{
		Source: "a",
		Target: "b",
		Kind:   types.EdgeKindCalls,
	}
	require.NoError(t, q.InsertEdge(edge))

	t.Run("outgoing edges", func(t *testing.T) {
		edges, err := q.GetEdges("a", types.EdgeDirectionOutgoing)
		require.NoError(t, err)
		require.Len(t, edges, 1)
		assert.Equal(t, "b", edges[0].Target)
		assert.Equal(t, types.EdgeKindCalls, edges[0].Kind)
	})

	t.Run("incoming edges", func(t *testing.T) {
		edges, err := q.GetEdges("b", types.EdgeDirectionIncoming)
		require.NoError(t, err)
		require.Len(t, edges, 1)
		assert.Equal(t, "a", edges[0].Source)
	})

	t.Run("both directions", func(t *testing.T) {
		edges, err := q.GetEdges("a", types.EdgeDirectionBoth)
		require.NoError(t, err)
		assert.Len(t, edges, 1)
	})

	t.Run("inserting same edge twice creates two rows (no unique constraint)", func(t *testing.T) {
		// The edges table uses AUTOINCREMENT PK; there is no UNIQUE(source,target,kind)
		// constraint, so two identical inserts produce two rows — matching the TypeScript schema.
		require.NoError(t, q.InsertEdge(edge))
		edges, err := q.GetEdges("a", types.EdgeDirectionOutgoing)
		require.NoError(t, err)
		assert.Len(t, edges, 2) // first from initial insert + this one
	})

	t.Run("batch insert", func(t *testing.T) {
		c := sampleNode("c", "C", "src/c.ts", types.NodeKindFunction)
		require.NoError(t, q.UpsertNode(c))

		batch := []*types.Edge{
			{Source: "a", Target: "c", Kind: types.EdgeKindCalls},
			{Source: "b", Target: "c", Kind: types.EdgeKindCalls},
		}
		require.NoError(t, q.InsertEdgeBatch(batch))

		edges, err := q.GetEdges("c", types.EdgeDirectionIncoming)
		require.NoError(t, err)
		assert.Len(t, edges, 2)
	})
}

// ===========================================================================
// TestEdgeCascadeDelete
// ===========================================================================

// TestEdgeCascadeDelete verifies that deleting a file removes its nodes and
// their associated edges via cascade.
func TestEdgeCascadeDelete(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	// Nodes in two files.
	n1 := sampleNode("n1", "Foo", "src/foo.ts", types.NodeKindFunction)
	n2 := sampleNode("n2", "Bar", "src/bar.ts", types.NodeKindFunction)
	require.NoError(t, q.UpsertNodes([]*types.Node{n1, n2}))

	// File records.
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path: "src/foo.ts", ContentHash: "abc", Language: types.TypeScript, Size: 100,
		ModifiedAt: time.Now().UnixMilli(), IndexedAt: time.Now().UnixMilli(),
	}))
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path: "src/bar.ts", ContentHash: "def", Language: types.TypeScript, Size: 200,
		ModifiedAt: time.Now().UnixMilli(), IndexedAt: time.Now().UnixMilli(),
	}))

	// Edge between the two nodes.
	require.NoError(t, q.InsertEdge(&types.Edge{Source: "n1", Target: "n2", Kind: types.EdgeKindCalls}))

	// Delete foo.ts — n1 should disappear along with the edge.
	require.NoError(t, q.DeleteFile("src/foo.ts"))

	got, err := q.GetNodeByID("n1")
	require.NoError(t, err)
	assert.Nil(t, got, "node n1 should have been deleted")

	// n2 should still exist.
	got, err = q.GetNodeByID("n2")
	require.NoError(t, err)
	assert.NotNil(t, got, "node n2 should still exist")

	// The edge from n1→n2 should have been cascade-deleted.
	edges, err := q.GetEdges("n1", types.EdgeDirectionOutgoing)
	require.NoError(t, err)
	assert.Empty(t, edges, "edge should have been cascade-deleted")

	// The file record for foo.ts should be gone.
	f, err := q.GetFileByPath("src/foo.ts")
	require.NoError(t, err)
	assert.Nil(t, f)
}

// ===========================================================================
// TestFileRecord
// ===========================================================================

func TestFileRecord(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	f := &types.FileRecord{
		Path:        "src/api.ts",
		ContentHash: "sha256:abc123",
		Language:    types.TypeScript,
		Size:        4096,
		ModifiedAt:  time.Now().UnixMilli(),
		IndexedAt:   time.Now().UnixMilli(),
		NodeCount:   3,
	}
	require.NoError(t, q.UpsertFileRecord(f))

	got, err := q.GetFileByPath("src/api.ts")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "src/api.ts", got.Path)
	assert.Equal(t, "sha256:abc123", got.ContentHash)
	assert.Equal(t, types.TypeScript, got.Language)
	assert.Equal(t, int64(4096), got.Size)
	assert.Equal(t, 3, got.NodeCount)

	// Upsert with updated hash.
	f.ContentHash = "sha256:def456"
	require.NoError(t, q.UpsertFileRecord(f))

	got2, err := q.GetFileByPath("src/api.ts")
	require.NoError(t, err)
	assert.Equal(t, "sha256:def456", got2.ContentHash)

	// Not found returns nil.
	missing, err := q.GetFileByPath("nonexistent.ts")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// ===========================================================================
// TestUnresolvedRefs
// ===========================================================================

func TestUnresolvedRefs(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	// Need a parent node (FK).
	n := sampleNode("caller", "callerFn", "src/a.ts", types.NodeKindFunction)
	require.NoError(t, q.UpsertNode(n))

	ref := &types.UnresolvedReference{
		FromNodeID:    "caller",
		ReferenceName: "SomeService",
		ReferenceKind: types.EdgeKindCalls,
		Line:          42,
		Column:        8,
		FilePath:      "src/a.ts",
		Language:      types.TypeScript,
		Candidates:    []string{"services/some.ts::SomeService"},
	}
	require.NoError(t, q.InsertUnresolvedRef(ref))

	refs, err := q.GetAllUnresolvedRefs()
	require.NoError(t, err)
	require.Len(t, refs, 1)

	got := refs[0]
	assert.Equal(t, "caller", got.FromNodeID)
	assert.Equal(t, "SomeService", got.ReferenceName)
	assert.Equal(t, types.EdgeKindCalls, got.ReferenceKind)
	assert.Equal(t, 42, got.Line)
	assert.Equal(t, 8, got.Column)
	assert.Equal(t, types.TypeScript, got.Language)
	assert.Equal(t, []string{"services/some.ts::SomeService"}, got.Candidates)

	count, err := q.GetUnresolvedRefsCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Batch insert.
	batch := []*types.UnresolvedReference{
		{FromNodeID: "caller", ReferenceName: "OtherService", ReferenceKind: types.EdgeKindImports, Line: 5, Column: 0, FilePath: "src/a.ts"},
		{FromNodeID: "caller", ReferenceName: "ThirdService", ReferenceKind: types.EdgeKindImports, Line: 6, Column: 0, FilePath: "src/a.ts"},
	}
	require.NoError(t, q.InsertUnresolvedRefsBatch(batch))

	count, err = q.GetUnresolvedRefsCount()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// GetUnresolvedRefsBatch pagination.
	page, err := q.GetUnresolvedRefsBatch(0, 2)
	require.NoError(t, err)
	assert.Len(t, page, 2)

	// Clear.
	require.NoError(t, q.ClearUnresolvedRefs())
	count, err = q.GetUnresolvedRefsCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ===========================================================================
// TestFTSSearch
// ===========================================================================

func TestFTSSearch(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	nodes := []*types.Node{
		{
			ID: "auth1", Kind: types.NodeKindFunction, Name: "authenticate",
			QualifiedName: "src/auth.ts::authenticate", FilePath: "src/auth.ts",
			Language: types.TypeScript, StartLine: 1, EndLine: 10, UpdatedAt: time.Now().UnixMilli(),
		},
		{
			ID: "auth2", Kind: types.NodeKindClass, Name: "AuthService",
			QualifiedName: "src/auth.ts::AuthService", FilePath: "src/auth.ts",
			Language: types.TypeScript, StartLine: 15, EndLine: 50, UpdatedAt: time.Now().UnixMilli(),
		},
		{
			ID: "user1", Kind: types.NodeKindFunction, Name: "createUser",
			QualifiedName: "src/users.ts::createUser", FilePath: "src/users.ts",
			Language: types.TypeScript, StartLine: 1, EndLine: 20, UpdatedAt: time.Now().UnixMilli(),
		},
	}
	require.NoError(t, q.UpsertNodes(nodes))

	t.Run("FTS prefix search", func(t *testing.T) {
		results, err := q.SearchNodes("auth", types.SearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results, "should find nodes matching 'auth' prefix")

		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.Node.ID
		}
		assert.Contains(t, ids, "auth1")
		assert.Contains(t, ids, "auth2")
	})

	t.Run("FTS full word", func(t *testing.T) {
		results, err := q.SearchNodes("createUser", types.SearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Equal(t, "user1", results[0].Node.ID)
	})

	t.Run("no results returns empty slice", func(t *testing.T) {
		results, err := q.SearchNodes("xyzzy_not_found", types.SearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("kind filter", func(t *testing.T) {
		results, err := q.SearchNodes("auth", types.SearchOptions{
			Limit: 10,
			Kinds: []types.NodeKind{types.NodeKindClass},
		})
		require.NoError(t, err)
		for _, r := range results {
			assert.Equal(t, types.NodeKindClass, r.Node.Kind)
		}
	})
}

// ===========================================================================
// TestGetStats
// ===========================================================================

func TestGetStats(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	nodes := []*types.Node{
		sampleNode("f1", "funcA", "src/a.ts", types.NodeKindFunction),
		sampleNode("f2", "funcB", "src/a.ts", types.NodeKindFunction),
		sampleNode("c1", "ClassX", "src/b.ts", types.NodeKindClass),
	}
	require.NoError(t, q.UpsertNodes(nodes))
	require.NoError(t, q.InsertEdge(&types.Edge{Source: "f1", Target: "f2", Kind: types.EdgeKindCalls}))
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path: "src/a.ts", ContentHash: "hash1", Language: types.TypeScript,
		Size: 100, ModifiedAt: time.Now().UnixMilli(), IndexedAt: time.Now().UnixMilli(),
	}))
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path: "src/b.ts", ContentHash: "hash2", Language: types.TypeScript,
		Size: 200, ModifiedAt: time.Now().UnixMilli(), IndexedAt: time.Now().UnixMilli(),
	}))

	stats, err := q.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats.NodeCount)
	assert.Equal(t, 1, stats.EdgeCount)
	assert.Equal(t, 2, stats.FileCount)
	assert.Equal(t, 2, stats.NodesByKind[types.NodeKindFunction])
	assert.Equal(t, 1, stats.NodesByKind[types.NodeKindClass])
	assert.Equal(t, 1, stats.EdgesByKind[types.EdgeKindCalls])
	assert.Equal(t, 2, stats.FilesByLanguage[types.TypeScript])
}

// ===========================================================================
// TestClear
// ===========================================================================

func TestClear(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	require.NoError(t, q.UpsertNode(sampleNode("n1", "foo", "src/a.ts", types.NodeKindFunction)))
	require.NoError(t, q.UpsertFileRecord(&types.FileRecord{
		Path: "src/a.ts", ContentHash: "h", Language: types.TypeScript,
		Size: 10, ModifiedAt: time.Now().UnixMilli(), IndexedAt: time.Now().UnixMilli(),
	}))

	require.NoError(t, q.Clear())

	nodes, err := q.GetAllNodes()
	require.NoError(t, err)
	assert.Empty(t, nodes)

	files, err := q.GetAllFiles()
	require.NoError(t, err)
	assert.Empty(t, files)
}

// ===========================================================================
// TestMetadata
// ===========================================================================

func TestMetadata(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	// Missing key returns "".
	val, err := q.GetMetadata("missing_key")
	require.NoError(t, err)
	assert.Equal(t, "", val)

	// Set and retrieve.
	require.NoError(t, q.SetMetadata("version", "1.0.0"))
	val, err = q.GetMetadata("version")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", val)

	// Update.
	require.NoError(t, q.SetMetadata("version", "2.0.0"))
	val, err = q.GetMetadata("version")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", val)
}

// ===========================================================================
// TestWithTx
// ===========================================================================

func TestWithTx(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	t.Run("commit on success", func(t *testing.T) {
		err := db.WithTx(func(_ *sql.Tx) error {
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("rollback on error leaves db unchanged", func(t *testing.T) {
		// Attempt to insert a node inside a transaction that fails.
		n := sampleNode("txnode", "txFunc", "src/tx.ts", types.NodeKindFunction)
		args := nodeArgs(n)
		_ = db.WithTx(func(tx *sql.Tx) error {
			_, _ = tx.Exec(upsertNodeSQL, args...)
			return fmt.Errorf("forced error")
		})

		// Node should not exist because the transaction was rolled back.
		got, err := q.GetNodeByID("txnode")
		require.NoError(t, err)
		assert.Nil(t, got, "transaction should have been rolled back")
	})
}

// ===========================================================================
// TestBatchInsertPerformance / BenchmarkBatchUpsert
// ===========================================================================

// TestBatchInsertCorrectness verifies that UpsertNodes correctly stores a
// large batch of nodes.
func TestBatchInsertCorrectness(t *testing.T) {
	db := openTestDB(t)
	q := NewQueries(db)

	const count = 100
	nodes := make([]*types.Node, count)
	for i := range nodes {
		nodes[i] = sampleNode(
			fmt.Sprintf("batch-node-%d", i),
			fmt.Sprintf("func%d", i),
			fmt.Sprintf("src/file%d.ts", i%10),
			types.NodeKindFunction,
		)
	}

	require.NoError(t, q.UpsertNodes(nodes))

	stats, err := q.GetStats()
	require.NoError(t, err)
	assert.Equal(t, count, stats.NodeCount)
}

// BenchmarkBatchUpsert measures the throughput of UpsertNodes for 10k nodes.
// Run with: go test -bench=BenchmarkBatchUpsert -benchtime=1x ./internal/db/...
// Target from GO_PORT_PLAN Task 3.4.4: <500 ms for 10k nodes on modern hardware.
func BenchmarkBatchUpsert(b *testing.B) {
	const count = 10_000
	nodes := make([]*types.Node, count)
	for i := range nodes {
		nodes[i] = sampleNode(
			fmt.Sprintf("bench-node-%d", i),
			fmt.Sprintf("func%d", i),
			fmt.Sprintf("src/file%d.ts", i%100),
			types.NodeKindFunction,
		)
	}

	b.ResetTimer()
	for range b.N {
		db, err := OpenMemory()
		if err != nil {
			b.Fatal(err)
		}
		q := NewQueries(db)
		if err := q.UpsertNodes(nodes); err != nil {
			db.Close()
			b.Fatal(err)
		}
		db.Close()
	}
	b.ReportAllocs()
}
