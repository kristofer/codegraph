// Package types defines the core data types for the CodeGraph knowledge graph.
package types

import (
	"fmt"
	"strings"
)

// =============================================================================
// NodeKind
// =============================================================================

// NodeKind represents the type of a code symbol node in the knowledge graph.
type NodeKind string

const (
	NodeKindFile       NodeKind = "file"
	NodeKindModule     NodeKind = "module"
	NodeKindClass      NodeKind = "class"
	NodeKindStruct     NodeKind = "struct"
	NodeKindInterface  NodeKind = "interface"
	NodeKindTrait      NodeKind = "trait"
	NodeKindProtocol   NodeKind = "protocol"
	NodeKindFunction   NodeKind = "function"
	NodeKindMethod     NodeKind = "method"
	NodeKindProperty   NodeKind = "property"
	NodeKindField      NodeKind = "field"
	NodeKindVariable   NodeKind = "variable"
	NodeKindConstant   NodeKind = "constant"
	NodeKindEnum       NodeKind = "enum"
	NodeKindEnumMember NodeKind = "enum_member"
	NodeKindTypeAlias  NodeKind = "type_alias"
	NodeKindNamespace  NodeKind = "namespace"
	NodeKindParameter  NodeKind = "parameter"
	NodeKindImport     NodeKind = "import"
	NodeKindExport     NodeKind = "export"
	NodeKindRoute      NodeKind = "route"
	NodeKindComponent  NodeKind = "component"
)

// allNodeKinds is the exhaustive list used for validation.
var allNodeKinds = []NodeKind{
	NodeKindFile, NodeKindModule, NodeKindClass, NodeKindStruct,
	NodeKindInterface, NodeKindTrait, NodeKindProtocol, NodeKindFunction,
	NodeKindMethod, NodeKindProperty, NodeKindField, NodeKindVariable,
	NodeKindConstant, NodeKindEnum, NodeKindEnumMember, NodeKindTypeAlias,
	NodeKindNamespace, NodeKindParameter, NodeKindImport, NodeKindExport,
	NodeKindRoute, NodeKindComponent,
}

// String returns the string representation of the NodeKind.
func (k NodeKind) String() string { return string(k) }

// ParseNodeKind parses a string into a NodeKind.
// Returns an error if the string does not match any known NodeKind.
func ParseNodeKind(s string) (NodeKind, error) {
	k := NodeKind(strings.ToLower(s))
	for _, v := range allNodeKinds {
		if v == k {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown NodeKind %q", s)
}

// =============================================================================
// EdgeKind
// =============================================================================

// EdgeKind represents the type of relationship between two nodes.
type EdgeKind string

const (
	EdgeKindContains    EdgeKind = "contains"    // Parent contains child (file→class, class→method)
	EdgeKindCalls       EdgeKind = "calls"        // Function/method calls another
	EdgeKindImports     EdgeKind = "imports"      // File imports from another
	EdgeKindExports     EdgeKind = "exports"      // File exports a symbol
	EdgeKindExtends     EdgeKind = "extends"      // Class/interface extends another
	EdgeKindImplements  EdgeKind = "implements"   // Class implements interface
	EdgeKindReferences  EdgeKind = "references"   // Generic reference to another symbol
	EdgeKindTypeOf      EdgeKind = "type_of"      // Variable/parameter has type
	EdgeKindReturns     EdgeKind = "returns"      // Function returns type
	EdgeKindInstantiates EdgeKind = "instantiates" // Creates instance of class
	EdgeKindOverrides   EdgeKind = "overrides"    // Method overrides parent method
	EdgeKindDecorates   EdgeKind = "decorates"    // Decorator applied to symbol
)

var allEdgeKinds = []EdgeKind{
	EdgeKindContains, EdgeKindCalls, EdgeKindImports, EdgeKindExports,
	EdgeKindExtends, EdgeKindImplements, EdgeKindReferences, EdgeKindTypeOf,
	EdgeKindReturns, EdgeKindInstantiates, EdgeKindOverrides, EdgeKindDecorates,
}

// String returns the string representation of the EdgeKind.
func (k EdgeKind) String() string { return string(k) }

// ParseEdgeKind parses a string into an EdgeKind.
// Returns an error if the string does not match any known EdgeKind.
func ParseEdgeKind(s string) (EdgeKind, error) {
	k := EdgeKind(strings.ToLower(s))
	for _, v := range allEdgeKinds {
		if v == k {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown EdgeKind %q", s)
}

// =============================================================================
// Language
// =============================================================================

// Language represents a programming language.
type Language string

const (
	TypeScript Language = "typescript"
	JavaScript Language = "javascript"
	TSX        Language = "tsx"
	JSX        Language = "jsx"
	Python     Language = "python"
	Go         Language = "go"
	Rust       Language = "rust"
	Java       Language = "java"
	C          Language = "c"
	CPP        Language = "cpp"
	CSharp     Language = "csharp"
	PHP        Language = "php"
	Ruby       Language = "ruby"
	Swift      Language = "swift"
	Kotlin     Language = "kotlin"
	Dart       Language = "dart"
	Svelte     Language = "svelte"
	Liquid     Language = "liquid"
	Pascal     Language = "pascal"
	Unknown    Language = "unknown"
)

var allLanguages = []Language{
	TypeScript, JavaScript, TSX, JSX, Python, Go, Rust, Java, C, CPP,
	CSharp, PHP, Ruby, Swift, Kotlin, Dart, Svelte, Liquid, Pascal, Unknown,
}

// String returns the string representation of the Language.
func (l Language) String() string { return string(l) }

// ParseLanguage parses a string into a Language.
// Returns Unknown if the string does not match any known language.
func ParseLanguage(s string) Language {
	lang := Language(strings.ToLower(s))
	for _, v := range allLanguages {
		if v == lang {
			return v
		}
	}
	return Unknown
}

// =============================================================================
// Visibility
// =============================================================================

// Visibility represents symbol access modifiers.
type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityPrivate   Visibility = "private"
	VisibilityProtected Visibility = "protected"
	VisibilityInternal  Visibility = "internal"
)

// =============================================================================
// Provenance
// =============================================================================

// Provenance describes how an edge was created.
type Provenance string

const (
	ProvenanceTreeSitter Provenance = "tree-sitter"
	ProvenanceSCIP       Provenance = "scip"
	ProvenanceHeuristic  Provenance = "heuristic"
)

// =============================================================================
// Node
// =============================================================================

// Node represents a code symbol in the knowledge graph.
type Node struct {
	// ID is a unique identifier (hash of file path + qualified name).
	ID string `json:"id" db:"id"`

	// Kind is the type of code element.
	Kind NodeKind `json:"kind" db:"kind"`

	// Name is the simple name (e.g., "calculateTotal").
	Name string `json:"name" db:"name"`

	// QualifiedName is the fully qualified name (e.g., "src/utils.ts::MathHelper.calculateTotal").
	QualifiedName string `json:"qualifiedName" db:"qualified_name"`

	// FilePath is the file path relative to project root.
	FilePath string `json:"filePath" db:"file_path"`

	// Language is the programming language.
	Language Language `json:"language" db:"language"`

	// StartLine is the starting line number (1-indexed).
	StartLine int `json:"startLine" db:"start_line"`

	// EndLine is the ending line number (1-indexed).
	EndLine int `json:"endLine" db:"end_line"`

	// StartColumn is the starting column (0-indexed).
	StartColumn int `json:"startColumn" db:"start_column"`

	// EndColumn is the ending column (0-indexed).
	EndColumn int `json:"endColumn" db:"end_column"`

	// Docstring is the documentation string if present.
	Docstring *string `json:"docstring,omitempty" db:"docstring"`

	// Signature is the function/method signature.
	Signature *string `json:"signature,omitempty" db:"signature"`

	// Visibility is the access modifier.
	Visibility *Visibility `json:"visibility,omitempty" db:"visibility"`

	// IsExported indicates whether the symbol is exported.
	IsExported bool `json:"isExported,omitempty" db:"is_exported"`

	// IsAsync indicates whether the symbol is async.
	IsAsync bool `json:"isAsync,omitempty" db:"is_async"`

	// IsStatic indicates whether the symbol is static.
	IsStatic bool `json:"isStatic,omitempty" db:"is_static"`

	// IsAbstract indicates whether the symbol is abstract.
	IsAbstract bool `json:"isAbstract,omitempty" db:"is_abstract"`

	// Decorators are annotations applied to the symbol.
	Decorators []string `json:"decorators,omitempty" db:"-"`

	// TypeParameters are generic type parameters.
	TypeParameters []string `json:"typeParameters,omitempty" db:"-"`

	// UpdatedAt is when the node was last updated (Unix milliseconds).
	UpdatedAt int64 `json:"updatedAt" db:"updated_at"`
}

// =============================================================================
// Edge
// =============================================================================

// Edge represents a relationship between two nodes.
type Edge struct {
	// Source is the source node ID.
	Source string `json:"source" db:"source"`

	// Target is the target node ID.
	Target string `json:"target" db:"target"`

	// Kind is the type of relationship.
	Kind EdgeKind `json:"kind" db:"kind"`

	// Metadata holds additional context about the relationship.
	Metadata map[string]any `json:"metadata,omitempty" db:"-"`

	// Line is the line number where the relationship occurs (e.g., call site).
	Line *int `json:"line,omitempty" db:"line"`

	// Column is the column number where the relationship occurs.
	Column *int `json:"column,omitempty" db:"col"`

	// Provenance describes how this edge was created.
	Provenance *Provenance `json:"provenance,omitempty" db:"provenance"`
}

// =============================================================================
// FileRecord
// =============================================================================

// FileRecord holds metadata about a tracked source file.
type FileRecord struct {
	// Path is the file path relative to project root.
	Path string `json:"path" db:"path"`

	// ContentHash is used for change detection.
	ContentHash string `json:"contentHash" db:"content_hash"`

	// Language is the detected programming language.
	Language Language `json:"language" db:"language"`

	// Size is the file size in bytes.
	Size int64 `json:"size" db:"size"`

	// ModifiedAt is the last modification timestamp (Unix milliseconds).
	ModifiedAt int64 `json:"modifiedAt" db:"modified_at"`

	// IndexedAt is when the file was last indexed (Unix milliseconds).
	IndexedAt int64 `json:"indexedAt" db:"indexed_at"`

	// NodeCount is the number of nodes extracted from this file.
	NodeCount int `json:"nodeCount" db:"node_count"`

	// Errors holds any extraction errors for this file.
	Errors []ExtractionError `json:"errors,omitempty" db:"-"`
}

// =============================================================================
// UnresolvedReference
// =============================================================================

// UnresolvedReference is a reference that could not be resolved during extraction.
type UnresolvedReference struct {
	// FromNodeID is the ID of the node containing the reference.
	FromNodeID string `json:"fromNodeId" db:"from_node_id"`

	// ReferenceName is the name being referenced.
	ReferenceName string `json:"referenceName" db:"reference_name"`

	// ReferenceKind is the type of reference (call, type, import, etc.).
	ReferenceKind EdgeKind `json:"referenceKind" db:"reference_kind"`

	// Line is the location of the reference.
	Line int `json:"line" db:"line"`

	// Column is the column of the reference.
	Column int `json:"column" db:"col"`

	// FilePath is the file path where the reference occurs.
	FilePath string `json:"filePath,omitempty" db:"file_path"`

	// Language is the language of the source file.
	Language Language `json:"language,omitempty" db:"language"`

	// Candidates holds possible qualified names it might resolve to.
	Candidates []string `json:"candidates,omitempty" db:"-"`
}

// =============================================================================
// ExtractionResult
// =============================================================================

// ExtractionResult holds the result of parsing a source file.
type ExtractionResult struct {
	// Nodes are the extracted code symbols.
	Nodes []*Node `json:"nodes"`

	// Edges are the extracted relationships.
	Edges []*Edge `json:"edges"`

	// UnresolvedReferences are references that could not be resolved yet.
	UnresolvedReferences []*UnresolvedReference `json:"unresolvedReferences"`

	// Errors holds any errors during extraction.
	Errors []ExtractionError `json:"errors"`

	// DurationMs is the extraction duration in milliseconds.
	DurationMs float64 `json:"durationMs"`
}

// =============================================================================
// GraphStats
// =============================================================================

// GraphStats holds statistics about the knowledge graph.
type GraphStats struct {
	// NodeCount is the total number of nodes.
	NodeCount int `json:"nodeCount"`

	// EdgeCount is the total number of edges.
	EdgeCount int `json:"edgeCount"`

	// FileCount is the number of tracked files.
	FileCount int `json:"fileCount"`

	// NodesByKind holds node counts grouped by kind.
	NodesByKind map[NodeKind]int `json:"nodesByKind"`

	// EdgesByKind holds edge counts grouped by kind.
	EdgesByKind map[EdgeKind]int `json:"edgesByKind"`

	// FilesByLanguage holds file counts grouped by language.
	FilesByLanguage map[Language]int `json:"filesByLanguage"`

	// DBSizeBytes is the database size in bytes.
	DBSizeBytes int64 `json:"dbSizeBytes"`

	// LastUpdated is when the graph was last updated (Unix milliseconds).
	LastUpdated int64 `json:"lastUpdated"`
}

// =============================================================================
// Subgraph
// =============================================================================

// Subgraph is a subset of the knowledge graph.
type Subgraph struct {
	// Nodes holds the nodes in this subgraph, keyed by ID.
	Nodes map[string]*Node `json:"nodes"`

	// Edges are the edges in this subgraph.
	Edges []*Edge `json:"edges"`

	// Roots are the root node IDs (entry points).
	Roots []string `json:"roots"`
}

// =============================================================================
// Context / TaskContext
// =============================================================================

// SearchResult is a search result with relevance scoring.
type SearchResult struct {
	// Node is the matching node.
	Node *Node `json:"node"`

	// Score is the relevance score (0–1).
	Score float64 `json:"score"`

	// Highlights are matched text snippets for highlighting.
	Highlights []string `json:"highlights,omitempty"`
}

// CodeBlock is a block of source code with its context.
type CodeBlock struct {
	// Content is the source code.
	Content string `json:"content"`

	// FilePath is the file path.
	FilePath string `json:"filePath"`

	// StartLine is the starting line.
	StartLine int `json:"startLine"`

	// EndLine is the ending line.
	EndLine int `json:"endLine"`

	// Language is the language for syntax highlighting.
	Language Language `json:"language"`

	// Node is the associated node if extracted.
	Node *Node `json:"node,omitempty"`
}

// TaskContext is the full context for a task, ready for an AI assistant.
type TaskContext struct {
	// Query is the original query/task.
	Query string `json:"query"`

	// Subgraph is the relevant subgraph.
	Subgraph *Subgraph `json:"subgraph"`

	// EntryPoints are the entry-point nodes from semantic search.
	EntryPoints []*Node `json:"entryPoints"`

	// CodeBlocks are source code blocks extracted from key nodes.
	CodeBlocks []*CodeBlock `json:"codeBlocks"`

	// RelatedFiles are files involved in this context.
	RelatedFiles []string `json:"relatedFiles"`

	// Summary is a brief summary of the context.
	Summary string `json:"summary"`

	// Stats holds statistics about the context.
	Stats TaskContextStats `json:"stats"`
}

// TaskContextStats holds statistics about a TaskContext.
type TaskContextStats struct {
	NodeCount      int `json:"nodeCount"`
	EdgeCount      int `json:"edgeCount"`
	FileCount      int `json:"fileCount"`
	CodeBlockCount int `json:"codeBlockCount"`
	TotalCodeSize  int `json:"totalCodeSize"`
}
