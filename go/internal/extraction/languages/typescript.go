package languages

import (
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func typescriptConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_declaration", "function_expression", "generator_function_declaration"},
		MethodTypes:    []string{"method_definition"},
		ClassTypes:     []string{"class_declaration", "class"},
		InterfaceTypes: []string{"interface_declaration"},
		EnumTypes:      []string{"enum_declaration"},
		TypeAliasTypes: []string{"type_alias_declaration"},
		ImportTypes:    []string{"import_statement"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "identifier", "type_identifier", "property_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			parent := node.Parent()
			if parent == nil || parent.IsNull() {
				return false
			}
			return parent.Type() == "export_statement"
		},

		GetDocstring: tsGetDocstring,

		GetSignature: func(node *sitter.Node, source []byte) *string {
			// Extract up to the opening brace as signature
			sig := extractSignatureLine(node, source)
			if sig == "" {
				return nil
			}
			return &sig
		},

		IsAsync: func(node *sitter.Node, source []byte) bool {
			// Check for "async" keyword child
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				if ch.Type() == "async" {
					return true
				}
			}
			return false
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				if ch.Type() == "static" {
					return true
				}
			}
			return false
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			// TypeScript uses access modifiers on method_definition
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				switch ch.Type() {
				case "public":
					v := types.VisibilityPublic
					return &v
				case "private":
					v := types.VisibilityPrivate
					return &v
				case "protected":
					v := types.VisibilityProtected
					return &v
				}
			}
			return nil
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			// import_statement → from → string
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "string" {
					s := child.Content(source)
					return strings.Trim(s, `'"`)
				}
			}
			return ""
		},
	}
}

func javascriptConfig() *LangConfig {
	cfg := typescriptConfig()
	// JavaScript has the same node types as TypeScript for these constructs
	return cfg
}

// tsGetDocstring tries to extract a preceding block or line comment.
func tsGetDocstring(node *sitter.Node, source []byte) *string {
	prev := node.PrevNamedSibling()
	if prev == nil || prev.IsNull() {
		// Check parent's prev sibling (e.g. when wrapped in export_statement)
		parent := node.Parent()
		if parent != nil && !parent.IsNull() {
			prev = parent.PrevNamedSibling()
		}
	}
	if prev == nil || prev.IsNull() {
		return nil
	}
	t := prev.Type()
	if t == "comment" || t == "block_comment" || t == "line_comment" {
		s := prev.Content(source)
		return &s
	}
	return nil
}

// extractSignatureLine returns the first line of the node's source up to '{'.
func extractSignatureLine(node *sitter.Node, source []byte) string {
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(source) {
		end = uint32(len(source))
	}
	text := string(source[start:end])
	// Trim at opening brace
	if idx := strings.Index(text, "{"); idx > 0 {
		text = text[:idx]
	}
	// Take only first line
	if idx := strings.Index(text, "\n"); idx > 0 {
		text = text[:idx]
	}
	text = strings.TrimRightFunc(text, unicode.IsSpace)
	return text
}
