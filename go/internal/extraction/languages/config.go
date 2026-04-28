// Package languages provides tree-sitter language configurations for extraction.
package languages

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

// LangConfig holds tree-sitter node-type configuration and helper callbacks for
// one programming language.
type LangConfig struct {
	// FunctionTypes are tree-sitter node types that represent functions.
	FunctionTypes []string
	// MethodTypes are tree-sitter node types that represent methods.
	// When a type appears in both FunctionTypes and MethodTypes the Walker
	// decides based on nesting context (e.g. Python function_definition).
	MethodTypes []string
	// ClassTypes are tree-sitter node types that represent classes.
	ClassTypes []string
	// InterfaceTypes are tree-sitter node types that represent interfaces/protocols.
	InterfaceTypes []string
	// StructTypes are tree-sitter node types that represent struct types.
	StructTypes []string
	// TraitTypes are tree-sitter node types that represent traits.
	TraitTypes []string
	// EnumTypes are tree-sitter node types that represent enumerations.
	EnumTypes []string
	// TypeAliasTypes are tree-sitter node types that represent type aliases.
	TypeAliasTypes []string
	// ImportTypes are tree-sitter node types that represent imports.
	ImportTypes []string
	// ConstantTypes are tree-sitter node types that represent constants.
	ConstantTypes []string

	// GetName extracts the symbol name from an AST node.
	GetName func(node *sitter.Node, source []byte) string
	// IsExported returns true when the node represents an exported symbol.
	IsExported func(node *sitter.Node, source []byte) bool
	// GetDocstring extracts the optional doc comment for a node.
	GetDocstring func(node *sitter.Node, source []byte) *string
	// GetSignature extracts the declaration signature for a node.
	GetSignature func(node *sitter.Node, source []byte) *string
	// IsAsync returns true when the function/method is async.
	IsAsync func(node *sitter.Node, source []byte) bool
	// IsStatic returns true when the method is static.
	IsStatic func(node *sitter.Node, source []byte) bool
	// GetVisibility returns the visibility modifier for a node.
	GetVisibility func(node *sitter.Node, source []byte) *types.Visibility
	// GetReceiverType returns the receiver type name for Go methods. If the
	// function_declaration has no receiver the empty string is returned.
	GetReceiverType func(node *sitter.Node, source []byte) string
	// GetImportPath returns the module path from an import node.
	GetImportPath func(node *sitter.Node, source []byte) string
}

// GetConfig returns the LangConfig for lang, or nil if no config is registered.
func GetConfig(lang types.Language) *LangConfig {
	switch lang {
	case types.TypeScript, types.TSX:
		return typescriptConfig()
	case types.JavaScript, types.JSX:
		return javascriptConfig()
	case types.Python:
		return pythonConfig()
	case types.Go:
		return goConfig()
	case types.Rust:
		return rustConfig()
	case types.Java:
		return javaConfig()
	case types.CSharp:
		return csharpConfig()
	case types.PHP:
		return phpConfig()
	case types.Ruby:
		return rubyConfig()
	case types.Swift:
		return swiftConfig()
	case types.Kotlin:
		return kotlinConfig()
	case types.C, types.CPP:
		return cCppConfig()
	case types.Svelte:
		return svelteConfig()
	}
	return nil
}

// --- shared helpers ---

// firstNamedChildOfTypes returns the first named child whose type is in `types`.
func firstNamedChildOfTypes(node *sitter.Node, typeNames ...string) *sitter.Node {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		for _, t := range typeNames {
			if child.Type() == t {
				return child
			}
		}
	}
	return nil
}

// hasNamedChildOfType returns true if the node has a named child with the given type.
func hasNamedChildOfType(node *sitter.Node, typeName string) bool {
	return firstNamedChildOfTypes(node, typeName) != nil
}

// nodeContent returns the source text for a node, or "" if nil.
func nodeContent(node *sitter.Node, source []byte) string {
	if node == nil || node.IsNull() {
		return ""
	}
	return node.Content(source)
}

// getNameField uses ChildByFieldName("name") and falls back to scanning named children.
func getNameField(node *sitter.Node, source []byte, fallbackTypes ...string) string {
	n := node.ChildByFieldName("name")
	if n != nil && !n.IsNull() {
		return n.Content(source)
	}
	if len(fallbackTypes) > 0 {
		child := firstNamedChildOfTypes(node, fallbackTypes...)
		if child != nil {
			return child.Content(source)
		}
	}
	return ""
}
