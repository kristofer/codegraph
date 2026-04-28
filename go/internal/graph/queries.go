package graph

import (
	"strings"

	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
)

// ===========================================================================
// GraphQueryManager
// ===========================================================================

// GraphQueryManager wraps GraphTraverser with higher-level query helpers.
type GraphQueryManager struct {
	queries   *db.Queries
	traverser *GraphTraverser
}

// NewGraphQueryManager creates a GraphQueryManager backed by q.
func NewGraphQueryManager(q *db.Queries) *GraphQueryManager {
	return &GraphQueryManager{
		queries:   q,
		traverser: NewGraphTraverser(q),
	}
}

// Traverser returns the underlying GraphTraverser.
func (g *GraphQueryManager) Traverser() *GraphTraverser { return g.traverser }

// ===========================================================================
// Node context
// ===========================================================================

// NodeContext contains a focal node together with its graph neighbourhood.
type NodeContext struct {
	Focal        *types.Node
	Ancestors    []*types.Node
	Children     []*types.Node
	IncomingRefs []NodeEdge
	OutgoingRefs []NodeEdge
	Types        []*types.Node
	Imports      []*types.Node
}

// NodeEdge pairs a node with the connecting edge.
type NodeEdge struct {
	Node *types.Node
	Edge *types.Edge
}

// GetContext returns the full neighbourhood for nodeID.
func (g *GraphQueryManager) GetContext(nodeID string) (*NodeContext, error) {
	focal, err := g.queries.GetNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if focal == nil {
		return nil, nil
	}

	ancestors, err := g.traverser.GetAncestors(nodeID)
	if err != nil {
		return nil, err
	}
	children, err := g.traverser.GetChildren(nodeID)
	if err != nil {
		return nil, err
	}

	// Incoming refs (skip contains)
	inEdges, err := g.queries.GetEdges(nodeID, types.EdgeDirectionIncoming)
	if err != nil {
		return nil, err
	}
	var incomingRefs []NodeEdge
	for _, e := range inEdges {
		if e.Kind == types.EdgeKindContains {
			continue
		}
		n, err := g.queries.GetNodeByID(e.Source)
		if err != nil {
			return nil, err
		}
		if n != nil {
			incomingRefs = append(incomingRefs, NodeEdge{Node: n, Edge: e})
		}
	}

	// Outgoing refs (skip contains)
	outEdges, err := g.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing)
	if err != nil {
		return nil, err
	}
	var outgoingRefs []NodeEdge
	var typeNodes []*types.Node
	for _, e := range outEdges {
		if e.Kind == types.EdgeKindContains {
			continue
		}
		n, err := g.queries.GetNodeByID(e.Target)
		if err != nil {
			return nil, err
		}
		if n == nil {
			continue
		}
		outgoingRefs = append(outgoingRefs, NodeEdge{Node: n, Edge: e})
		if e.Kind == types.EdgeKindTypeOf || e.Kind == types.EdgeKindReturns {
			typeNodes = append(typeNodes, n)
		}
	}

	// File imports
	var importNodes []*types.Node
	if fileNode := findAncestorByKind(ancestors, types.NodeKindFile); fileNode != nil {
		importEdges, err := g.queries.GetEdges(fileNode.ID, types.EdgeDirectionOutgoing, types.EdgeKindImports)
		if err != nil {
			return nil, err
		}
		for _, e := range importEdges {
			n, err := g.queries.GetNodeByID(e.Target)
			if err != nil {
				return nil, err
			}
			if n != nil {
				importNodes = append(importNodes, n)
			}
		}
	}

	return &NodeContext{
		Focal:        focal,
		Ancestors:    ancestors,
		Children:     children,
		IncomingRefs: incomingRefs,
		OutgoingRefs: outgoingRefs,
		Types:        typeNodes,
		Imports:      importNodes,
	}, nil
}

// ===========================================================================
// File dependency queries
// ===========================================================================

// GetFileDependencies returns all files that filePath imports from.
func (g *GraphQueryManager) GetFileDependencies(filePath string) ([]string, error) {
	fileNodes, err := g.queries.GetNodesByFile(filePath)
	if err != nil {
		return nil, err
	}
	var fileNode *types.Node
	for _, n := range fileNodes {
		if n.Kind == types.NodeKindFile {
			fileNode = n
			break
		}
	}
	if fileNode == nil {
		return nil, nil
	}

	deps := make(map[string]bool)
	importEdges, err := g.queries.GetEdges(fileNode.ID, types.EdgeDirectionOutgoing, types.EdgeKindImports)
	if err != nil {
		return nil, err
	}
	for _, e := range importEdges {
		target, err := g.queries.GetNodeByID(e.Target)
		if err != nil {
			return nil, err
		}
		if target != nil && target.FilePath != filePath {
			deps[target.FilePath] = true
		}
	}
	return mapKeys(deps), nil
}

// GetFileDependents returns all files that import from filePath.
func (g *GraphQueryManager) GetFileDependents(filePath string) ([]string, error) {
	fileNodes, err := g.queries.GetNodesByFile(filePath)
	if err != nil {
		return nil, err
	}

	dependents := make(map[string]bool)

	for _, n := range fileNodes {
		if n.Kind == types.NodeKindFile {
			inEdges, err := g.queries.GetEdges(n.ID, types.EdgeDirectionIncoming, types.EdgeKindImports)
			if err != nil {
				return nil, err
			}
			for _, e := range inEdges {
				src, err := g.queries.GetNodeByID(e.Source)
				if err != nil {
					return nil, err
				}
				if src != nil && src.FilePath != filePath {
					dependents[src.FilePath] = true
				}
			}
		}
		if n.IsExported {
			inEdges, err := g.queries.GetEdges(n.ID, types.EdgeDirectionIncoming, types.EdgeKindImports)
			if err != nil {
				return nil, err
			}
			for _, e := range inEdges {
				src, err := g.queries.GetNodeByID(e.Source)
				if err != nil {
					return nil, err
				}
				if src != nil && src.FilePath != filePath {
					dependents[src.FilePath] = true
				}
			}
		}
	}
	return mapKeys(dependents), nil
}

// GetExportedSymbols returns all exported nodes in filePath.
func (g *GraphQueryManager) GetExportedSymbols(filePath string) ([]*types.Node, error) {
	nodes, err := g.queries.GetNodesByFile(filePath)
	if err != nil {
		return nil, err
	}
	var exported []*types.Node
	for _, n := range nodes {
		if n.IsExported {
			exported = append(exported, n)
		}
	}
	return exported, nil
}

// ===========================================================================
// Module structure
// ===========================================================================

// GetModuleStructure returns a map of directory paths to the file paths they contain.
func (g *GraphQueryManager) GetModuleStructure() (map[string][]string, error) {
	files, err := g.queries.GetAllFiles()
	if err != nil {
		return nil, err
	}
	structure := make(map[string][]string)
	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		dir := strings.Join(parts[:len(parts)-1], "/")
		if dir == "" {
			dir = "."
		}
		structure[dir] = append(structure[dir], f.Path)
	}
	return structure, nil
}

// ===========================================================================
// Circular dependency detection
// ===========================================================================

// FindCircularDependencies returns import cycles as slices of file paths.
func (g *GraphQueryManager) FindCircularDependencies() ([][]string, error) {
	files, err := g.queries.GetAllFiles()
	if err != nil {
		return nil, err
	}

	var cycles [][]string
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	var dfs func(filePath string, path []string)
	dfs = func(filePath string, path []string) {
		if recursionStack[filePath] {
			start := indexOf(path, filePath)
			if start >= 0 {
				cycle := make([]string, len(path)-start)
				copy(cycle, path[start:])
				cycles = append(cycles, cycle)
			}
			return
		}
		if visited[filePath] {
			return
		}
		visited[filePath] = true
		recursionStack[filePath] = true
		deps, _ := g.GetFileDependencies(filePath)
		for _, dep := range deps {
			dfs(dep, append(path, filePath))
		}
		recursionStack[filePath] = false
	}

	for _, f := range files {
		if !visited[f.Path] {
			dfs(f.Path, nil)
		}
	}
	return cycles, nil
}

// ===========================================================================
// Node metrics
// ===========================================================================

// NodeMetrics holds complexity measures for a node.
type NodeMetrics struct {
	IncomingEdgeCount int
	OutgoingEdgeCount int
	CallCount         int
	CallerCount       int
	ChildCount        int
	Depth             int
}

// GetNodeMetrics returns complexity metrics for nodeID.
func (g *GraphQueryManager) GetNodeMetrics(nodeID string) (*NodeMetrics, error) {
	inEdges, err := g.queries.GetEdges(nodeID, types.EdgeDirectionIncoming)
	if err != nil {
		return nil, err
	}
	outEdges, err := g.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing)
	if err != nil {
		return nil, err
	}
	ancestors, err := g.traverser.GetAncestors(nodeID)
	if err != nil {
		return nil, err
	}

	m := &NodeMetrics{
		IncomingEdgeCount: len(inEdges),
		OutgoingEdgeCount: len(outEdges),
		Depth:             len(ancestors),
	}
	for _, e := range outEdges {
		if e.Kind == types.EdgeKindCalls {
			m.CallCount++
		}
		if e.Kind == types.EdgeKindContains {
			m.ChildCount++
		}
	}
	for _, e := range inEdges {
		if e.Kind == types.EdgeKindCalls {
			m.CallerCount++
		}
	}
	return m, nil
}

// ===========================================================================
// Dead code detection
// ===========================================================================

// FindDeadCode returns non-exported nodes of the given kinds with no incoming
// references (excluding contains edges).
func (g *GraphQueryManager) FindDeadCode(kinds []types.NodeKind) ([]*types.Node, error) {
	if len(kinds) == 0 {
		kinds = []types.NodeKind{types.NodeKindFunction, types.NodeKindMethod, types.NodeKindClass}
	}
	var deadCode []*types.Node
	for _, kind := range kinds {
		nodes, err := g.queries.GetNodesByKind(kind)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if n.IsExported {
				continue
			}
			inEdges, err := g.queries.GetEdges(n.ID, types.EdgeDirectionIncoming)
			if err != nil {
				return nil, err
			}
			refs := 0
			for _, e := range inEdges {
				if e.Kind != types.EdgeKindContains {
					refs++
				}
			}
			if refs == 0 {
				deadCode = append(deadCode, n)
			}
		}
	}
	return deadCode, nil
}

// ===========================================================================
// Qualified-name pattern matching
// ===========================================================================

// FindByQualifiedNamePattern returns nodes whose qualified_name matches the
// given glob-style pattern (supports * wildcard).
func (g *GraphQueryManager) FindByQualifiedNamePattern(pattern string) ([]*types.Node, error) {
	var result []*types.Node
	kinds := []types.NodeKind{
		types.NodeKindClass, types.NodeKindFunction, types.NodeKindMethod,
		types.NodeKindInterface, types.NodeKindTypeAlias, types.NodeKindVariable, types.NodeKindConstant,
	}
	for _, kind := range kinds {
		nodes, err := g.queries.GetNodesByKind(kind)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if globMatch(pattern, n.QualifiedName) {
				result = append(result, n)
			}
		}
	}
	return result, nil
}

// globMatch returns true when pattern (supporting * wildcard) matches s.
func globMatch(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.SplitN(pattern, "*", 2)
	prefix, suffix := parts[0], parts[1]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	if suffix == "" {
		return true
	}
	return strings.Contains(rest, suffix)
}

// ===========================================================================
// Helpers
// ===========================================================================

func findAncestorByKind(ancestors []*types.Node, kind types.NodeKind) *types.Node {
	for _, a := range ancestors {
		if a.Kind == kind {
			return a
		}
	}
	return nil
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
