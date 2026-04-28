// Package resolution resolves unresolved references after full indexing.
package resolution

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
)

// ===========================================================================
// Internal types (mirror TypeScript resolution/types.ts)
// ===========================================================================

// ResolvedRef is a successfully resolved reference.
type ResolvedRef struct {
	// FromNodeID is the source node that contained the reference.
	FromNodeID string
	// TargetNodeID is the resolved target node.
	TargetNodeID string
	// Confidence is a 0–1 score for how confident we are in the resolution.
	Confidence float64
	// ResolvedBy describes which strategy resolved the reference.
	ResolvedBy string
}

// ResolutionResult holds aggregate statistics from a resolution pass.
type ResolutionResult struct {
	Resolved   int
	Unresolved int
	Total      int
	ByMethod   map[string]int
}

// ===========================================================================
// ResolutionContext
// ===========================================================================

// ResolutionContext provides the graph-access primitives needed by resolution
// strategies.  It caches results to avoid redundant DB round-trips.
type ResolutionContext struct {
	ProjectRoot string

	queries *db.Queries

	mu              sync.Mutex
	nodeByFile      map[string][]*types.Node
	importMappings  map[string][]ImportMapping
	nameIndex       map[string][]*types.Node  // name → nodes
	lowerNameIndex  map[string][]*types.Node  // lower(name) → nodes
	qualNameIndex   map[string][]*types.Node  // qualified_name → nodes
	knownFiles      map[string]bool
	knownNames      map[string]bool
	cacheWarmed     bool
}

// NewResolutionContext creates a ResolutionContext backed by the given queries.
func NewResolutionContext(projectRoot string, q *db.Queries) *ResolutionContext {
	return &ResolutionContext{
		ProjectRoot:   projectRoot,
		queries:       q,
		nodeByFile:    make(map[string][]*types.Node),
		importMappings: make(map[string][]ImportMapping),
		nameIndex:     make(map[string][]*types.Node),
		lowerNameIndex: make(map[string][]*types.Node),
		qualNameIndex: make(map[string][]*types.Node),
		knownFiles:    make(map[string]bool),
		knownNames:    make(map[string]bool),
	}
}

// WarmCaches pre-fetches lightweight indexes to speed up resolution.
func (ctx *ResolutionContext) WarmCaches() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.cacheWarmed {
		return
	}
	if paths, err := ctx.queries.GetAllFilePaths(); err == nil {
		for _, p := range paths {
			ctx.knownFiles[p] = true
		}
	}
	if names, err := ctx.queries.GetAllNodeNames(); err == nil {
		for _, n := range names {
			ctx.knownNames[n] = true
		}
	}
	ctx.cacheWarmed = true
}

// GetNodesInFile returns all nodes in the given file (cached).
func (ctx *ResolutionContext) GetNodesInFile(filePath string) []*types.Node {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if nodes, ok := ctx.nodeByFile[filePath]; ok {
		return nodes
	}
	nodes, _ := ctx.queries.GetNodesByFile(filePath)
	ctx.nodeByFile[filePath] = nodes
	return nodes
}

// GetNodesByName returns all nodes with the given simple name (cached).
func (ctx *ResolutionContext) GetNodesByName(name string) []*types.Node {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if nodes, ok := ctx.nameIndex[name]; ok {
		return nodes
	}
	nodes, _ := ctx.queries.GetNodesByName(name)
	ctx.nameIndex[name] = nodes
	return nodes
}

// GetNodesByLowerName returns nodes whose lowercased name matches (cached).
func (ctx *ResolutionContext) GetNodesByLowerName(lower string) []*types.Node {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if nodes, ok := ctx.lowerNameIndex[lower]; ok {
		return nodes
	}
	nodes, _ := ctx.queries.GetNodesByLowerName(lower)
	ctx.lowerNameIndex[lower] = nodes
	return nodes
}

// GetNodesByQualifiedName returns nodes with the exact qualified name (cached).
func (ctx *ResolutionContext) GetNodesByQualifiedName(qualName string) []*types.Node {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if nodes, ok := ctx.qualNameIndex[qualName]; ok {
		return nodes
	}
	nodes, _ := ctx.queries.GetNodesByQualifiedName(qualName)
	ctx.qualNameIndex[qualName] = nodes
	return nodes
}

// GetNodesByKind returns all nodes of the given kind.
func (ctx *ResolutionContext) GetNodesByKind(kind types.NodeKind) []*types.Node {
	nodes, _ := ctx.queries.GetNodesByKind(kind)
	return nodes
}

// GetAllFiles returns all tracked file paths.
func (ctx *ResolutionContext) GetAllFiles() []string {
	paths, _ := ctx.queries.GetAllFilePaths()
	return paths
}

// FileExists checks whether a file is known (indexed or on disk).
func (ctx *ResolutionContext) FileExists(filePath string) bool {
	ctx.mu.Lock()
	known := ctx.knownFiles[filePath]
	normalized := filepath.ToSlash(filePath)
	knownNorm := ctx.knownFiles[normalized]
	ctx.mu.Unlock()
	if known || knownNorm {
		return true
	}
	return fileExistsOnDisk(ctx.ProjectRoot, filePath)
}

// ReadFile reads the content of a file relative to the project root.
// Returns "" on error.
func (ctx *ResolutionContext) ReadFile(filePath string) string {
	full := filepath.Join(ctx.ProjectRoot, filePath)
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	return string(data)
}

// GetImportMappings returns (and caches) the import mappings for a file.
func (ctx *ResolutionContext) GetImportMappings(filePath string, lang types.Language) []ImportMapping {
	ctx.mu.Lock()
	if m, ok := ctx.importMappings[filePath]; ok {
		ctx.mu.Unlock()
		return m
	}
	ctx.mu.Unlock()
	content := ctx.ReadFile(filePath)
	var mappings []ImportMapping
	if content != "" {
		mappings = ExtractImportMappings(content, lang)
	}
	ctx.mu.Lock()
	ctx.importMappings[filePath] = mappings
	ctx.mu.Unlock()
	return mappings
}

// HasAnyPossibleMatch returns true if any symbol with this exact name (or its
// parts for qualified names) exists in the known-names index.
func (ctx *ResolutionContext) HasAnyPossibleMatch(name string) bool {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if !ctx.cacheWarmed {
		return true // no index available; defer to full resolution
	}
	if ctx.knownNames[name] {
		return true
	}
	if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
		receiver := name[:dotIdx]
		member := name[dotIdx+1:]
		if ctx.knownNames[receiver] || ctx.knownNames[member] {
			return true
		}
		cap := strings.ToUpper(receiver[:1]) + receiver[1:]
		if ctx.knownNames[cap] {
			return true
		}
	}
	if colonIdx := strings.Index(name, "::"); colonIdx > 0 {
		if ctx.knownNames[name[:colonIdx]] || ctx.knownNames[name[colonIdx+2:]] {
			return true
		}
	}
	if slashIdx := strings.LastIndexByte(name, '/'); slashIdx > 0 {
		if ctx.knownNames[name[slashIdx+1:]] {
			return true
		}
	}
	return false
}

// ===========================================================================
// FrameworkResolver interface
// ===========================================================================

// FrameworkResolver is implemented by each language/framework-specific resolver.
type FrameworkResolver interface {
	Name() string
	Detect(ctx *ResolutionContext) bool
	Resolve(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef
}

// ===========================================================================
// ReferenceResolver — orchestrator
// ===========================================================================

// ReferenceResolver coordinates all resolution strategies.
type ReferenceResolver struct {
	ctx        *ResolutionContext
	queries    *db.Queries
	frameworks []FrameworkResolver
}

// NewReferenceResolver creates and initialises a ReferenceResolver.
func NewReferenceResolver(projectRoot string, q *db.Queries) *ReferenceResolver {
	ctx := NewResolutionContext(projectRoot, q)
	r := &ReferenceResolver{
		ctx:     ctx,
		queries: q,
	}
	r.frameworks = DetectFrameworks(ctx)
	return r
}

// GetDetectedFrameworks returns names of active framework resolvers.
func (r *ReferenceResolver) GetDetectedFrameworks() []string {
	names := make([]string, len(r.frameworks))
	for i, f := range r.frameworks {
		names[i] = f.Name()
	}
	return names
}

// ResolveOne attempts to resolve a single unresolved reference.
// Returns nil when the reference cannot be resolved or is to a known built-in.
func (r *ReferenceResolver) ResolveOne(ref *types.UnresolvedReference) *ResolvedRef {
	if isBuiltInOrExternal(ref) {
		return nil
	}
	if !r.ctx.HasAnyPossibleMatch(ref.ReferenceName) {
		return nil
	}

	var candidates []*ResolvedRef

	// Strategy 1: framework-specific resolution
	for _, fw := range r.frameworks {
		if result := fw.Resolve(ref, r.ctx); result != nil {
			if result.Confidence >= 0.9 {
				return result
			}
			candidates = append(candidates, result)
		}
	}

	// Strategy 2: import-based resolution
	if result := ResolveViaImport(ref, r.ctx); result != nil {
		if result.Confidence >= 0.9 {
			return result
		}
		candidates = append(candidates, result)
	}

	// Strategy 3: name-matching
	if result := MatchReference(ref, r.ctx); result != nil {
		candidates = append(candidates, result)
	}

	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Confidence > best.Confidence {
			best = c
		}
	}
	return best
}

// ResolveAndPersist processes all unresolved references in the database,
// writes resolved edges with provenance='heuristic', and removes resolved
// entries from unresolved_refs.
func (r *ReferenceResolver) ResolveAndPersist() (*ResolutionResult, error) {
	r.ctx.WarmCaches()

	stats := &ResolutionResult{ByMethod: make(map[string]int)}
	batchSize := 5000

	for {
		batch, err := r.queries.GetUnresolvedRefsBatch(0, batchSize)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		var edges []*types.Edge
		var resolvedKeys []db.ResolvedRefKey
		var unresolvedKeys []db.ResolvedRefKey

		for _, ref := range batch {
			result := r.ResolveOne(ref)
			stats.Total++
			if result != nil {
				stats.Resolved++
				stats.ByMethod[result.ResolvedBy]++

				prov := types.ProvenanceHeuristic
				edge := &types.Edge{
					Source:     ref.FromNodeID,
					Target:     result.TargetNodeID,
					Kind:       ref.ReferenceKind,
					Provenance: &prov,
					Line:       &ref.Line,
					Column:     &ref.Column,
					Metadata:   map[string]any{"confidence": result.Confidence, "resolvedBy": result.ResolvedBy},
				}
				edges = append(edges, edge)
				resolvedKeys = append(resolvedKeys, db.ResolvedRefKey{
					FromNodeID:    ref.FromNodeID,
					ReferenceName: ref.ReferenceName,
					ReferenceKind: ref.ReferenceKind,
				})
			} else {
				stats.Unresolved++
				unresolvedKeys = append(unresolvedKeys, db.ResolvedRefKey{
					FromNodeID:    ref.FromNodeID,
					ReferenceName: ref.ReferenceName,
					ReferenceKind: ref.ReferenceKind,
				})
			}
		}

		if len(edges) > 0 {
			if err := r.queries.InsertEdgeBatch(edges); err != nil {
				return nil, err
			}
		}
		// Delete all processed refs (both resolved and unresolved) so the next
		// batch fetch doesn't return the same rows.
		allKeys := append(resolvedKeys, unresolvedKeys...)
		if len(allKeys) > 0 {
			if err := r.queries.DeleteSpecificResolvedRefs(allKeys); err != nil {
				return nil, err
			}
		}
		// If nothing was resolved and nothing was deleted we'd loop forever.
		if len(resolvedKeys) == 0 && len(unresolvedKeys) == len(batch) {
			break
		}
	}
	return stats, nil
}

// ===========================================================================
// Built-in/external symbol filter
// ===========================================================================

var jsBuiltIns = map[string]bool{
	"console": true, "window": true, "document": true, "global": true, "process": true,
	"Promise": true, "Array": true, "Object": true, "String": true, "Number": true,
	"Boolean": true, "Date": true, "Math": true, "JSON": true, "RegExp": true,
	"Error": true, "Map": true, "Set": true, "setTimeout": true, "setInterval": true,
	"clearTimeout": true, "clearInterval": true, "fetch": true, "require": true,
	"module": true, "exports": true, "__dirname": true, "__filename": true,
}

var reactHooks = map[string]bool{
	"useState": true, "useEffect": true, "useContext": true, "useReducer": true,
	"useCallback": true, "useMemo": true, "useRef": true, "useLayoutEffect": true,
	"useImperativeHandle": true, "useDebugValue": true,
}

var pythonBuiltIns = map[string]bool{
	"print": true, "len": true, "range": true, "str": true, "int": true,
	"float": true, "list": true, "dict": true, "set": true, "tuple": true,
	"open": true, "input": true, "type": true, "isinstance": true,
	"hasattr": true, "getattr": true, "setattr": true,
	"super": true, "self": true, "cls": true, "None": true, "True": true, "False": true,
}

var pythonBuiltInTypes = map[string]bool{
	"list": true, "dict": true, "set": true, "tuple": true, "str": true,
	"int": true, "float": true, "bool": true, "bytes": true, "bytearray": true,
	"frozenset": true, "object": true, "super": true,
}

var pythonBuiltInMethods = map[string]bool{
	"append": true, "extend": true, "insert": true, "remove": true, "pop": true,
	"clear": true, "sort": true, "reverse": true, "copy": true, "update": true,
	"keys": true, "values": true, "items": true, "get": true, "add": true,
	"discard": true, "union": true, "intersection": true, "difference": true,
	"split": true, "join": true, "strip": true, "replace": true, "lower": true,
	"upper": true, "startswith": true, "endswith": true, "find": true, "index": true,
	"count": true, "encode": true, "decode": true, "format": true,
	"read": true, "write": true, "readline": true, "close": true,
}

var goStdlibPackages = map[string]bool{
	"fmt": true, "os": true, "io": true, "net": true, "http": true, "log": true,
	"math": true, "sort": true, "sync": true, "time": true, "path": true,
	"bytes": true, "strings": true, "strconv": true, "errors": true,
	"context": true, "json": true, "regexp": true, "reflect": true,
	"runtime": true, "testing": true, "flag": true, "bufio": true,
	"filepath": true, "unicode": true, "atomic": true, "rand": true, "ioutil": true,
}

var goBuiltIns = map[string]bool{
	"make": true, "new": true, "len": true, "cap": true, "append": true,
	"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
	"print": true, "println": true, "error": true, "nil": true, "true": true, "false": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "string": true, "bool": true, "byte": true, "rune": true, "any": true,
}

var pascalBuiltIns = map[string]bool{
	"System": true, "SysUtils": true, "Classes": true, "WriteLn": true, "Write": true,
	"ReadLn": true, "Read": true, "Inc": true, "Dec": true, "Length": true,
	"SetLength": true, "High": true, "Low": true, "Assigned": true, "FreeAndNil": true,
	"Format": true, "IntToStr": true, "StrToInt": true, "Trim": true, "UpperCase": true,
	"LowerCase": true, "Pos": true, "Copy": true, "Now": true, "True": true, "False": true,
}

var pascalUnitPrefixes = []string{
	"System.", "Winapi.", "Vcl.", "Fmx.", "Data.", "Datasnap.",
	"Soap.", "Xml.", "Web.", "REST.", "FireDAC.", "IBX.",
	"IdHTTP", "IdTCP", "IdSSL",
}

func isBuiltInOrExternal(ref *types.UnresolvedReference) bool {
	name := ref.ReferenceName
	lang := ref.Language
	isJSTS := lang == types.TypeScript || lang == types.JavaScript || lang == types.TSX || lang == types.JSX

	if isJSTS && jsBuiltIns[name] {
		return true
	}
	if isJSTS && (strings.HasPrefix(name, "console.") || strings.HasPrefix(name, "Math.") || strings.HasPrefix(name, "JSON.")) {
		return true
	}
	if isJSTS && reactHooks[name] {
		return true
	}
	if lang == types.Python {
		if pythonBuiltIns[name] {
			return true
		}
		if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
			recv := name[:dotIdx]
			meth := name[dotIdx+1:]
			if pythonBuiltInTypes[recv] {
				return true
			}
			if pythonBuiltInMethods[meth] {
				return true
			}
		}
		if pythonBuiltInMethods[name] {
			return true
		}
	}
	if lang == types.Go {
		if dotIdx := strings.IndexByte(name, '.'); dotIdx > 0 {
			if goStdlibPackages[name[:dotIdx]] {
				return true
			}
		}
		if goBuiltIns[name] {
			return true
		}
	}
	if lang == types.Pascal {
		for _, p := range pascalUnitPrefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		if pascalBuiltIns[name] {
			return true
		}
	}
	return false
}

