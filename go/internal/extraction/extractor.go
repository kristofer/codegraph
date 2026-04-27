// Package extraction provides AST-based code extraction using tree-sitter.
package extraction

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/extraction/languages"
	"github.com/kristofer/codegraph/internal/types"
)

// GetConfig returns the language configuration for lang.
func GetConfig(lang types.Language) *languages.LangConfig {
	return languages.GetConfig(lang)
}

// generateNodeID creates a deterministic 16-character node ID.
// The 16-character (64-bit) prefix matches the TypeScript implementation
// (createHash('sha256').digest('hex').substring(0, 16)) and provides a
// sufficiently low collision probability for typical codebases.
func generateNodeID(filePath string, kind types.NodeKind, name string, line int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)))
	return hex.EncodeToString(h[:])[:16]
}

// nodeExtra holds extra metadata gathered during AST traversal.
type nodeExtra struct {
	signature  *string
	docstring  *string
	visibility *types.Visibility
	isExported bool
	isAsync    bool
	isStatic   bool
}

// Walker traverses a tree-sitter AST and builds an ExtractionResult.
type Walker struct {
	cfg        *languages.LangConfig
	lang       types.Language
	filePath   string
	source     []byte
	nodes      []*types.Node
	edges      []*types.Edge
	unresolved []*types.UnresolvedReference
	errors     []types.ExtractionError
	nodeStack  []string // stack of node IDs (innermost last)
	nodeByID   map[string]*types.Node
}

// NewWalker creates a Walker for the given language, file path, and source.
func NewWalker(cfg *languages.LangConfig, lang types.Language, filePath string, source []byte) *Walker {
	return &Walker{
		cfg:      cfg,
		lang:     lang,
		filePath: filePath,
		source:   source,
		nodeByID: make(map[string]*types.Node),
	}
}

// Walk traverses the parse tree and returns the ExtractionResult.
func (w *Walker) Walk(tree *sitter.Tree) *types.ExtractionResult {
	root := tree.RootNode()
	fileName := filepath.Base(w.filePath)
	fileNode := w.createNode(types.NodeKindFile, fileName, root, nodeExtra{isExported: true})
	w.nodeStack = append(w.nodeStack, fileNode.ID)

	for i := 0; i < int(root.NamedChildCount()); i++ {
		w.visitNode(root.NamedChild(i))
	}

	return &types.ExtractionResult{
		Nodes:                w.nodes,
		Edges:                w.edges,
		UnresolvedReferences: w.unresolved,
		Errors:               w.errors,
	}
}

// visitNode dispatches based on the AST node type.
func (w *Walker) visitNode(node *sitter.Node) {
	if node == nil || node.IsNull() {
		return
	}
	nodeType := node.Type()
	cfg := w.cfg

	if cfg == nil {
		w.visitChildren(node)
		return
	}

	switch {
	case containsStr(cfg.ImportTypes, nodeType):
		w.extractImport(node)
	case containsStr(cfg.FunctionTypes, nodeType):
		if w.isInsideClass() {
			w.extractMethod(node)
		} else {
			w.extractFunction(node)
		}
	case containsStr(cfg.MethodTypes, nodeType) && !containsStr(cfg.FunctionTypes, nodeType):
		w.extractMethod(node)
	case containsStr(cfg.ClassTypes, nodeType):
		w.extractClass(node)
	case containsStr(cfg.InterfaceTypes, nodeType):
		w.extractInterface(node)
	case containsStr(cfg.StructTypes, nodeType):
		w.extractStruct(node)
	case containsStr(cfg.TraitTypes, nodeType):
		w.extractTrait(node)
	case containsStr(cfg.EnumTypes, nodeType):
		w.extractEnum(node)
	case containsStr(cfg.TypeAliasTypes, nodeType):
		w.extractTypeAlias(node)
	case containsStr(cfg.ConstantTypes, nodeType):
		w.extractConstant(node)
	case nodeType == "export_statement":
		// TypeScript/JS: recurse into declaration, marking exported
		w.visitExportStatement(node)
	case nodeType == "impl_item":
		// Rust: visit impl body, treating functions as methods
		w.visitImplItem(node)
	case nodeType == "decorated_definition":
		// Python: pass through decorated definitions
		w.visitDecoratedDefinition(node)
	default:
		w.visitChildren(node)
	}
}

// --- special node handlers ---

func (w *Walker) visitExportStatement(node *sitter.Node) {
	// Recurse into the declaration child; the extract* functions will detect
	// the export_statement parent to set IsExported.
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		w.visitNode(child)
	}
}

func (w *Walker) visitImplItem(node *sitter.Node) {
	// Rust impl block: push a virtual class scope so function_items become methods
	implName := w.getImplName(node)
	if implName != "" {
		n := w.createNode(types.NodeKindStruct, implName+"_impl", node, nodeExtra{})
		w.nodeStack = append(w.nodeStack, n.ID)
		defer func() { w.nodeStack = w.nodeStack[:len(w.nodeStack)-1] }()
	}
	body := node.ChildByFieldName("body")
	if body == nil || body.IsNull() {
		w.visitChildren(node)
		return
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		w.visitNode(body.NamedChild(i))
	}
}

func (w *Walker) getImplName(node *sitter.Node) string {
	n := node.ChildByFieldName("type")
	if n != nil && !n.IsNull() {
		return n.Content(w.source)
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch.Type() == "type_identifier" {
			return ch.Content(w.source)
		}
	}
	return ""
}

func (w *Walker) visitDecoratedDefinition(node *sitter.Node) {
	// Python decorated_definition wraps a function_definition or class_definition
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() != "decorator" {
			w.visitNode(child)
		}
	}
}

// --- extraction functions ---

func (w *Walker) extractFunction(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		w.visitChildren(node)
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindFunction, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractMethod(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		w.visitChildren(node)
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindMethod, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractClass(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		w.visitChildren(node)
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindClass, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractInterface(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		w.visitChildren(node)
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindInterface, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractStruct(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		w.visitChildren(node)
		return
	}
	// For Go: type_declaration may be struct OR interface; check the inner type_spec
	if w.lang == types.Go {
		w.extractGoTypeDecl(node)
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindStruct, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractGoTypeDecl(node *sitter.Node) {
	// Go type_declaration → (type_spec)+
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() != "type_spec" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil || nameNode.IsNull() {
			// fallback
			for j := 0; j < int(child.NamedChildCount()); j++ {
				gc := child.NamedChild(j)
				if gc.Type() == "type_identifier" {
					nameNode = gc
					break
				}
			}
		}
		if nameNode == nil || nameNode.IsNull() {
			continue
		}
		name := nameNode.Content(w.source)

		// Determine kind from the type field
		typeNode := child.ChildByFieldName("type")
		kind := types.NodeKindTypeAlias
		if typeNode != nil && !typeNode.IsNull() {
			switch typeNode.Type() {
			case "struct_type":
				kind = types.NodeKindStruct
			case "interface_type":
				kind = types.NodeKindInterface
			}
		}
		extra := nodeExtra{
			isExported: len(name) > 0 && unicode.IsUpper(rune(name[0])),
		}
		if w.cfg != nil && w.cfg.GetVisibility != nil {
			extra.visibility = w.cfg.GetVisibility(node, w.source)
		}
		n := w.createNode(kind, name, child, extra)
		w.nodeStack = append(w.nodeStack, n.ID)
		// visit body of struct/interface
		if typeNode != nil && !typeNode.IsNull() {
			w.visitChildren(typeNode)
		}
		w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
	}
}

func (w *Walker) extractTrait(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		return
	}
	extra := w.buildExtra(node)
	n := w.createNode(types.NodeKindTrait, name, node, extra)
	w.nodeStack = append(w.nodeStack, n.ID)
	w.visitBody(node)
	w.nodeStack = w.nodeStack[:len(w.nodeStack)-1]
}

func (w *Walker) extractEnum(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		return
	}
	extra := w.buildExtra(node)
	w.createNode(types.NodeKindEnum, name, node, extra)
}

func (w *Walker) extractTypeAlias(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		return
	}
	extra := w.buildExtra(node)
	w.createNode(types.NodeKindTypeAlias, name, node, extra)
}

func (w *Walker) extractImport(node *sitter.Node) {
	var importPath string
	if w.cfg != nil && w.cfg.GetImportPath != nil {
		importPath = w.cfg.GetImportPath(node, w.source)
	}
	if importPath == "" {
		importPath = node.Content(w.source)
	}
	name := importPath
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		name = "import"
	}
	line := int(node.StartPoint().Row) + 1
	col := int(node.StartPoint().Column)
	fromID := w.currentNodeID()

	w.unresolved = append(w.unresolved, &types.UnresolvedReference{
		FromNodeID:    fromID,
		ReferenceName: importPath,
		ReferenceKind: types.EdgeKindImports,
		Line:          line,
		Column:        col,
		FilePath:      w.filePath,
		Language:      w.lang,
	})

	w.createNode(types.NodeKindImport, name, node, nodeExtra{})
}

func (w *Walker) extractConstant(node *sitter.Node) {
	name := w.getName(node)
	if name == "" {
		return
	}
	extra := w.buildExtra(node)
	w.createNode(types.NodeKindConstant, name, node, extra)
}

// --- helpers ---

func (w *Walker) createNode(kind types.NodeKind, name string, node *sitter.Node, extra nodeExtra) *types.Node {
	line := int(node.StartPoint().Row) + 1
	id := generateNodeID(w.filePath, kind, name, line)

	qualified := w.buildQualifiedName(name)

	n := &types.Node{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: qualified,
		FilePath:      w.filePath,
		Language:      w.lang,
		StartLine:     line,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartColumn:   int(node.StartPoint().Column),
		EndColumn:     int(node.EndPoint().Column),
		Docstring:     extra.docstring,
		Signature:     extra.signature,
		Visibility:    extra.visibility,
		IsExported:    extra.isExported,
		IsAsync:       extra.isAsync,
		IsStatic:      extra.isStatic,
		UpdatedAt:     time.Now().UnixMilli(),
	}

	w.nodes = append(w.nodes, n)
	w.nodeByID[id] = n

	if len(w.nodeStack) > 0 {
		parentID := w.nodeStack[len(w.nodeStack)-1]
		w.edges = append(w.edges, &types.Edge{
			Source: parentID,
			Target: id,
			Kind:   types.EdgeKindContains,
		})
	}

	return n
}

func (w *Walker) buildQualifiedName(name string) string {
	var parts []string
	for _, id := range w.nodeStack {
		if n, ok := w.nodeByID[id]; ok && n.Kind != types.NodeKindFile {
			parts = append(parts, n.Name)
		}
	}
	parts = append(parts, name)
	return strings.Join(parts, "::")
}

func (w *Walker) buildExtra(node *sitter.Node) nodeExtra {
	extra := nodeExtra{}
	if w.cfg == nil {
		return extra
	}
	if w.cfg.IsExported != nil {
		extra.isExported = w.cfg.IsExported(node, w.source)
	}
	if w.cfg.IsAsync != nil {
		extra.isAsync = w.cfg.IsAsync(node, w.source)
	}
	if w.cfg.IsStatic != nil {
		extra.isStatic = w.cfg.IsStatic(node, w.source)
	}
	if w.cfg.GetVisibility != nil {
		extra.visibility = w.cfg.GetVisibility(node, w.source)
	}
	if w.cfg.GetDocstring != nil {
		extra.docstring = w.cfg.GetDocstring(node, w.source)
	}
	if w.cfg.GetSignature != nil {
		extra.signature = w.cfg.GetSignature(node, w.source)
	}
	return extra
}

func (w *Walker) getName(node *sitter.Node) string {
	if w.cfg != nil && w.cfg.GetName != nil {
		return w.cfg.GetName(node, w.source)
	}
	return ""
}

func (w *Walker) currentNodeID() string {
	if len(w.nodeStack) > 0 {
		return w.nodeStack[len(w.nodeStack)-1]
	}
	return ""
}

func (w *Walker) isInsideClass() bool {
	for _, id := range w.nodeStack {
		if n, ok := w.nodeByID[id]; ok {
			if n.Kind == types.NodeKindClass || n.Kind == types.NodeKindStruct {
				return true
			}
		}
	}
	return false
}

func (w *Walker) visitChildren(node *sitter.Node) {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		w.visitNode(node.NamedChild(i))
	}
}

// visitBody visits the "body" field child, or if missing falls back to visitChildren.
func (w *Walker) visitBody(node *sitter.Node) {
	body := node.ChildByFieldName("body")
	if body != nil && !body.IsNull() {
		w.visitChildren(body)
		return
	}
	// For languages without a named "body" field, visit all named children
	// but skip the name node to avoid re-processing.
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		t := child.Type()
		// Skip name nodes
		if t == "identifier" || t == "type_identifier" || t == "property_identifier" ||
			t == "field_identifier" || t == "formal_parameters" || t == "parameters" ||
			t == "type_annotation" || t == "type_parameters" {
			continue
		}
		w.visitNode(child)
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
