// Package graph provides BFS/DFS traversal and graph query helpers.
package graph

import (
	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
)

// TraversalOptions controls graph traversal behaviour.
type TraversalOptions struct {
	// MaxDepth limits how many hops from the start node are visited.
	// 0 means unlimited.
	MaxDepth int
	// EdgeKinds restricts which edge types are followed. Empty = all kinds.
	EdgeKinds []types.EdgeKind
	// NodeKinds restricts which node kinds are included. Empty = all kinds.
	NodeKinds []types.NodeKind
	// Direction controls outgoing, incoming, or bidirectional traversal.
	Direction types.EdgeDirection
	// Limit is the maximum number of nodes to include (0 = 1000).
	Limit int
	// IncludeStart controls whether the start node is in the result.
	IncludeStart bool
}

func defaultOpts(opts TraversalOptions) TraversalOptions {
	if opts.Limit == 0 {
		opts.Limit = 1000
	}
	return opts
}

// ===========================================================================
// GraphTraverser
// ===========================================================================

// GraphTraverser runs BFS/DFS traversal on the code knowledge graph.
type GraphTraverser struct {
	queries *db.Queries
}

// NewGraphTraverser creates a GraphTraverser backed by q.
func NewGraphTraverser(q *db.Queries) *GraphTraverser {
	return &GraphTraverser{queries: q}
}

// TraverseBFS performs a breadth-first traversal from startID and returns the
// resulting subgraph.
func (t *GraphTraverser) TraverseBFS(startID string, opts TraversalOptions) (*types.Subgraph, error) {
	opts = defaultOpts(opts)
	startNode, err := t.queries.GetNodeByID(startID)
	if err != nil {
		return nil, err
	}
	if startNode == nil {
		return emptySubgraph(), nil
	}

	nodes := make(map[string]*types.Node)
	var edges []*types.Edge
	visited := make(map[string]bool)

	type step struct {
		nodeID string
		depth  int
	}

	if opts.IncludeStart {
		nodes[startNode.ID] = startNode
	}

	queue := []step{{nodeID: startID, depth: 0}}

	for len(queue) > 0 && len(nodes) < opts.Limit {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur.nodeID] {
			continue
		}
		visited[cur.nodeID] = true

		if cur.depth >= opts.MaxDepth && opts.MaxDepth > 0 {
			continue
		}

		adjEdges, err := t.getAdjacentEdges(cur.nodeID, opts.Direction, opts.EdgeKinds)
		if err != nil {
			return nil, err
		}
		// Sort: structural edges first (contains → calls → rest)
		sortEdgesByPriority(adjEdges)

		for _, e := range adjEdges {
			nextID := e.Target
			if e.Source == cur.nodeID {
				nextID = e.Target
			} else {
				nextID = e.Source
			}
			if visited[nextID] {
				continue
			}
			nextNode, err := t.queries.GetNodeByID(nextID)
			if err != nil {
				return nil, err
			}
			if nextNode == nil {
				continue
			}
			if len(opts.NodeKinds) > 0 && !containsKind(opts.NodeKinds, nextNode.Kind) {
				continue
			}
			nodes[nextNode.ID] = nextNode
			edges = append(edges, e)
			queue = append(queue, step{nodeID: nextID, depth: cur.depth + 1})
		}
	}

	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: []string{startID}}, nil
}

// TraverseDFS performs a depth-first traversal from startID and returns the
// resulting subgraph.
func (t *GraphTraverser) TraverseDFS(startID string, opts TraversalOptions) (*types.Subgraph, error) {
	opts = defaultOpts(opts)
	startNode, err := t.queries.GetNodeByID(startID)
	if err != nil {
		return nil, err
	}
	if startNode == nil {
		return emptySubgraph(), nil
	}

	nodes := make(map[string]*types.Node)
	var edges []*types.Edge
	visited := make(map[string]bool)

	if opts.IncludeStart {
		nodes[startNode.ID] = startNode
	}

	if err := t.dfsRecursive(startID, 0, opts, nodes, &edges, visited); err != nil {
		return nil, err
	}

	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: []string{startID}}, nil
}

func (t *GraphTraverser) dfsRecursive(
	nodeID string, depth int, opts TraversalOptions,
	nodes map[string]*types.Node, edges *[]*types.Edge, visited map[string]bool,
) error {
	if visited[nodeID] || len(nodes) >= opts.Limit {
		return nil
	}
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return nil
	}
	visited[nodeID] = true

	adjEdges, err := t.getAdjacentEdges(nodeID, opts.Direction, opts.EdgeKinds)
	if err != nil {
		return err
	}
	for _, e := range adjEdges {
		nextID := e.Target
		if e.Source != nodeID {
			nextID = e.Source
		}
		if visited[nextID] {
			continue
		}
		nextNode, err := t.queries.GetNodeByID(nextID)
		if err != nil {
			return err
		}
		if nextNode == nil {
			continue
		}
		if len(opts.NodeKinds) > 0 && !containsKind(opts.NodeKinds, nextNode.Kind) {
			continue
		}
		nodes[nextNode.ID] = nextNode
		*edges = append(*edges, e)
		if err := t.dfsRecursive(nextID, depth+1, opts, nodes, edges, visited); err != nil {
			return err
		}
	}
	return nil
}

// GetImpactRadius returns all nodes potentially affected by changing nodeID
// (reverse traversal — incoming edges, up to maxDepth hops).
func (t *GraphTraverser) GetImpactRadius(nodeID string, maxDepth int) (*types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	focalNode, err := t.queries.GetNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if focalNode == nil {
		return emptySubgraph(), nil
	}

	nodes := make(map[string]*types.Node)
	var edges []*types.Edge
	visited := make(map[string]bool)

	nodes[focalNode.ID] = focalNode

	if err := t.impactRecursive(nodeID, maxDepth, 0, nodes, &edges, visited); err != nil {
		return nil, err
	}

	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: []string{nodeID}}, nil
}

func (t *GraphTraverser) impactRecursive(
	nodeID string, maxDepth, depth int,
	nodes map[string]*types.Node, edges *[]*types.Edge, visited map[string]bool,
) error {
	if visited[nodeID] || depth >= maxDepth {
		return nil
	}
	visited[nodeID] = true

	// For container nodes, also expand into contained children
	focalNode, err := t.queries.GetNodeByID(nodeID)
	if err != nil {
		return err
	}
	if focalNode != nil && isContainerKind(focalNode.Kind) {
		containsEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing, types.EdgeKindContains)
		if err != nil {
			return err
		}
		for _, e := range containsEdges {
			childNode, err := t.queries.GetNodeByID(e.Target)
			if err != nil {
				return err
			}
			if childNode == nil || visited[childNode.ID] {
				continue
			}
			nodes[childNode.ID] = childNode
			*edges = append(*edges, e)
			if err := t.impactRecursive(childNode.ID, maxDepth, depth, nodes, edges, visited); err != nil {
				return err
			}
		}
	}

	incomingEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionIncoming)
	if err != nil {
		return err
	}
	for _, e := range incomingEdges {
		srcNode, err := t.queries.GetNodeByID(e.Source)
		if err != nil {
			return err
		}
		if srcNode == nil || nodes[srcNode.ID] != nil {
			continue
		}
		nodes[srcNode.ID] = srcNode
		*edges = append(*edges, e)
		if err := t.impactRecursive(srcNode.ID, maxDepth, depth+1, nodes, edges, visited); err != nil {
			return err
		}
	}
	return nil
}

// FindPath finds the shortest path (by edge count) from fromID to toID using
// BFS. Returns nil if no path exists.
func (t *GraphTraverser) FindPath(fromID, toID string, edgeKinds []types.EdgeKind) ([]*types.Node, error) {
	fromNode, err := t.queries.GetNodeByID(fromID)
	if err != nil {
		return nil, err
	}
	toNode, err := t.queries.GetNodeByID(toID)
	if err != nil {
		return nil, err
	}
	if fromNode == nil || toNode == nil {
		return nil, nil
	}

	type pathEntry struct {
		nodeID string
		path   []string
	}
	visited := make(map[string]bool)
	queue := []pathEntry{{nodeID: fromID, path: []string{fromID}}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.nodeID == toID {
			// Resolve node IDs to nodes
			result := make([]*types.Node, 0, len(cur.path))
			for _, id := range cur.path {
				n, err := t.queries.GetNodeByID(id)
				if err != nil {
					return nil, err
				}
				if n != nil {
					result = append(result, n)
				}
			}
			return result, nil
		}
		if visited[cur.nodeID] {
			continue
		}
		visited[cur.nodeID] = true

		var outgoing []*types.Edge
		if len(edgeKinds) > 0 {
			outgoing, err = t.queries.GetEdges(cur.nodeID, types.EdgeDirectionOutgoing, edgeKinds...)
		} else {
			outgoing, err = t.queries.GetEdges(cur.nodeID, types.EdgeDirectionOutgoing)
		}
		if err != nil {
			return nil, err
		}
		for _, e := range outgoing {
			if !visited[e.Target] {
				newPath := append(append([]string(nil), cur.path...), e.Target)
				queue = append(queue, pathEntry{nodeID: e.Target, path: newPath})
			}
		}
	}
	return nil, nil
}

// ===========================================================================
// Callers / Callees helpers
// ===========================================================================

// CallerResult pairs a caller node with the edge that links it.
type CallerResult struct {
	Node *types.Node
	Edge *types.Edge
}

// GetCallers returns nodes that call / import / reference nodeID.
func (t *GraphTraverser) GetCallers(nodeID string, maxDepth int) ([]*CallerResult, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	var result []*CallerResult
	visited := make(map[string]bool)
	if err := t.callersRecursive(nodeID, maxDepth, 0, &result, visited); err != nil {
		return nil, err
	}
	return result, nil
}

func (t *GraphTraverser) callersRecursive(
	nodeID string, maxDepth, depth int,
	result *[]*CallerResult, visited map[string]bool,
) error {
	if depth >= maxDepth || visited[nodeID] {
		return nil
	}
	visited[nodeID] = true

	inEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionIncoming,
		types.EdgeKindCalls, types.EdgeKindReferences, types.EdgeKindImports)
	if err != nil {
		return err
	}
	for _, e := range inEdges {
		callerNode, err := t.queries.GetNodeByID(e.Source)
		if err != nil {
			return err
		}
		if callerNode != nil && !visited[callerNode.ID] {
			*result = append(*result, &CallerResult{Node: callerNode, Edge: e})
			if err := t.callersRecursive(callerNode.ID, maxDepth, depth+1, result, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// CalleeResult pairs a callee node with the edge that links it.
type CalleeResult struct {
	Node *types.Node
	Edge *types.Edge
}

// GetCallees returns nodes called / imported / referenced by nodeID.
func (t *GraphTraverser) GetCallees(nodeID string, maxDepth int) ([]*CalleeResult, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	var result []*CalleeResult
	visited := make(map[string]bool)
	if err := t.calleesRecursive(nodeID, maxDepth, 0, &result, visited); err != nil {
		return nil, err
	}
	return result, nil
}

func (t *GraphTraverser) calleesRecursive(
	nodeID string, maxDepth, depth int,
	result *[]*CalleeResult, visited map[string]bool,
) error {
	if depth >= maxDepth || visited[nodeID] {
		return nil
	}
	visited[nodeID] = true

	outEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing,
		types.EdgeKindCalls, types.EdgeKindReferences, types.EdgeKindImports)
	if err != nil {
		return err
	}
	for _, e := range outEdges {
		calleeNode, err := t.queries.GetNodeByID(e.Target)
		if err != nil {
			return err
		}
		if calleeNode != nil && !visited[calleeNode.ID] {
			*result = append(*result, &CalleeResult{Node: calleeNode, Edge: e})
			if err := t.calleesRecursive(calleeNode.ID, maxDepth, depth+1, result, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetCallGraph returns a subgraph containing both callers and callees of nodeID
// up to the given depth.
func (t *GraphTraverser) GetCallGraph(nodeID string, depth int) (*types.Subgraph, error) {
	if depth <= 0 {
		depth = 2
	}
	focalNode, err := t.queries.GetNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if focalNode == nil {
		return emptySubgraph(), nil
	}

	nodes := make(map[string]*types.Node)
	var edges []*types.Edge
	nodes[focalNode.ID] = focalNode

	callers, err := t.GetCallers(nodeID, depth)
	if err != nil {
		return nil, err
	}
	for _, cr := range callers {
		nodes[cr.Node.ID] = cr.Node
		edges = append(edges, cr.Edge)
	}

	callees, err := t.GetCallees(nodeID, depth)
	if err != nil {
		return nil, err
	}
	for _, cr := range callees {
		nodes[cr.Node.ID] = cr.Node
		edges = append(edges, cr.Edge)
	}

	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: []string{nodeID}}, nil
}

// GetInheritanceChain returns the type hierarchy (ancestors + descendants) for a
// class or interface node.
func (t *GraphTraverser) GetInheritanceChain(classID string) (*types.Subgraph, error) {
	focalNode, err := t.queries.GetNodeByID(classID)
	if err != nil {
		return nil, err
	}
	if focalNode == nil {
		return emptySubgraph(), nil
	}

	nodes := make(map[string]*types.Node)
	var edges []*types.Edge
	nodes[focalNode.ID] = focalNode

	// Use separate visited sets so ancestors and descendants don't block each other.
	ancestorVisited := make(map[string]bool)
	if err := t.typeAncestors(classID, nodes, &edges, ancestorVisited); err != nil {
		return nil, err
	}
	descendantVisited := make(map[string]bool)
	if err := t.typeDescendants(classID, nodes, &edges, descendantVisited); err != nil {
		return nil, err
	}

	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: []string{classID}}, nil
}

func (t *GraphTraverser) typeAncestors(nodeID string, nodes map[string]*types.Node, edges *[]*types.Edge, visited map[string]bool) error {
	if visited[nodeID] {
		return nil
	}
	visited[nodeID] = true
	outEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing, types.EdgeKindExtends, types.EdgeKindImplements)
	if err != nil {
		return err
	}
	for _, e := range outEdges {
		parentNode, err := t.queries.GetNodeByID(e.Target)
		if err != nil {
			return err
		}
		if parentNode != nil && nodes[parentNode.ID] == nil {
			nodes[parentNode.ID] = parentNode
			*edges = append(*edges, e)
			if err := t.typeAncestors(parentNode.ID, nodes, edges, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *GraphTraverser) typeDescendants(nodeID string, nodes map[string]*types.Node, edges *[]*types.Edge, visited map[string]bool) error {
	if visited[nodeID] {
		return nil
	}
	visited[nodeID] = true
	inEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionIncoming, types.EdgeKindExtends, types.EdgeKindImplements)
	if err != nil {
		return err
	}
	for _, e := range inEdges {
		childNode, err := t.queries.GetNodeByID(e.Source)
		if err != nil {
			return err
		}
		if childNode != nil && nodes[childNode.ID] == nil {
			nodes[childNode.ID] = childNode
			*edges = append(*edges, e)
			if err := t.typeDescendants(childNode.ID, nodes, edges, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetFileStructure returns a subgraph of all nodes contained in the given file.
func (t *GraphTraverser) GetFileStructure(filePath string) (*types.Subgraph, error) {
	fileNodes, err := t.queries.GetNodesByFile(filePath)
	if err != nil {
		return nil, err
	}
	nodes := make(map[string]*types.Node, len(fileNodes))
	for _, n := range fileNodes {
		nodes[n.ID] = n
	}

	var edges []*types.Edge
	var roots []string
	for _, n := range fileNodes {
		if n.Kind == types.NodeKindFile {
			roots = append(roots, n.ID)
		}
		outEdges, err := t.queries.GetEdges(n.ID, types.EdgeDirectionOutgoing, types.EdgeKindContains)
		if err != nil {
			return nil, err
		}
		for _, e := range outEdges {
			if nodes[e.Target] != nil {
				edges = append(edges, e)
			}
		}
	}
	return &types.Subgraph{Nodes: nodes, Edges: edges, Roots: roots}, nil
}

// GetAncestors returns the containment hierarchy ancestors of nodeID
// (from immediate parent up to root).
func (t *GraphTraverser) GetAncestors(nodeID string) ([]*types.Node, error) {
	var ancestors []*types.Node
	visited := make(map[string]bool)
	currentID := nodeID

	for {
		if visited[currentID] {
			break
		}
		visited[currentID] = true

		containingEdges, err := t.queries.GetEdges(currentID, types.EdgeDirectionIncoming, types.EdgeKindContains)
		if err != nil {
			return nil, err
		}
		if len(containingEdges) == 0 {
			break
		}
		parentNode, err := t.queries.GetNodeByID(containingEdges[0].Source)
		if err != nil {
			return nil, err
		}
		if parentNode == nil {
			break
		}
		ancestors = append(ancestors, parentNode)
		currentID = parentNode.ID
	}
	return ancestors, nil
}

// GetChildren returns the immediate children of nodeID (via contains edges).
func (t *GraphTraverser) GetChildren(nodeID string) ([]*types.Node, error) {
	containsEdges, err := t.queries.GetEdges(nodeID, types.EdgeDirectionOutgoing, types.EdgeKindContains)
	if err != nil {
		return nil, err
	}
	var children []*types.Node
	for _, e := range containsEdges {
		child, err := t.queries.GetNodeByID(e.Target)
		if err != nil {
			return nil, err
		}
		if child != nil {
			children = append(children, child)
		}
	}
	return children, nil
}

// ===========================================================================
// Internal helpers
// ===========================================================================

func (t *GraphTraverser) getAdjacentEdges(nodeID string, dir types.EdgeDirection, kinds []types.EdgeKind) ([]*types.Edge, error) {
	if len(kinds) > 0 {
		return t.queries.GetEdges(nodeID, dir, kinds...)
	}
	return t.queries.GetEdges(nodeID, dir)
}

func sortEdgesByPriority(edges []*types.Edge) {
	priority := func(e *types.Edge) int {
		switch e.Kind {
		case types.EdgeKindContains:
			return 0
		case types.EdgeKindCalls:
			return 1
		default:
			return 2
		}
	}
	for i := 1; i < len(edges); i++ {
		for j := i; j > 0 && priority(edges[j]) < priority(edges[j-1]); j-- {
			edges[j], edges[j-1] = edges[j-1], edges[j]
		}
	}
}

func containsKind(kinds []types.NodeKind, kind types.NodeKind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

var containerKinds = map[types.NodeKind]bool{
	types.NodeKindClass:     true,
	types.NodeKindInterface: true,
	types.NodeKindStruct:    true,
	types.NodeKindTrait:     true,
	types.NodeKindProtocol:  true,
	types.NodeKindModule:    true,
	types.NodeKindEnum:      true,
}

func isContainerKind(k types.NodeKind) bool { return containerKinds[k] }

func emptySubgraph() *types.Subgraph {
	return &types.Subgraph{Nodes: make(map[string]*types.Node), Edges: nil, Roots: nil}
}

