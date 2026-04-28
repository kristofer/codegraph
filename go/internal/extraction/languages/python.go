package languages

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func pythonConfig() *LangConfig {
	return &LangConfig{
		// function_definition appears as both top-level functions AND methods inside classes.
		// The Walker decides based on nesting context.
		FunctionTypes:  []string{"function_definition"},
		MethodTypes:    []string{"function_definition"},
		ClassTypes:     []string{"class_definition"},
		ImportTypes:    []string{"import_statement", "import_from_statement"},
		ConstantTypes:  []string{},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "identifier", "type_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			// Python has no export keyword; anything at module level is "exported"
			parent := node.Parent()
			if parent == nil || parent.IsNull() {
				return true
			}
			// If parent is module (top-level), exported
			return parent.Type() == "module"
		},

		GetDocstring: func(node *sitter.Node, source []byte) *string {
			// First statement in body may be a string (docstring)
			body := node.ChildByFieldName("body")
			if body == nil || body.IsNull() {
				return nil
			}
			if body.NamedChildCount() == 0 {
				return nil
			}
			first := body.NamedChild(0)
			if first == nil || first.IsNull() {
				return nil
			}
			// expression_statement containing a string
			if first.Type() == "expression_statement" && first.NamedChildCount() > 0 {
				s := first.NamedChild(0)
				if s != nil && !s.IsNull() && s.Type() == "string" {
					content := s.Content(source)
					return &content
				}
			}
			return nil
		},

		GetSignature: func(node *sitter.Node, source []byte) *string {
			sig := extractSignatureLine(node, source)
			if sig == "" {
				return nil
			}
			// Strip trailing colon for Python
			sig = strings.TrimRight(sig, " \t:")
			return &sig
		},

		IsAsync: func(node *sitter.Node, source []byte) bool {
			// In tree-sitter-python, async functions have an "async" keyword child
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				if ch.Type() == "async" {
					return true
				}
			}
			return false
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			// Static methods have @staticmethod decorator; we check parent decorated_definition
			parent := node.Parent()
			if parent == nil || parent.IsNull() {
				return false
			}
			if parent.Type() == "decorated_definition" {
				for i := 0; i < int(parent.NamedChildCount()); i++ {
					child := parent.NamedChild(i)
					if child.Type() == "decorator" {
						content := child.Content(source)
						if strings.Contains(content, "staticmethod") {
							return true
						}
					}
				}
			}
			return false
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			name := getNameField(node, source, "identifier")
			if strings.HasPrefix(name, "__") && !strings.HasSuffix(name, "__") {
				v := types.VisibilityPrivate
				return &v
			}
			if strings.HasPrefix(name, "_") {
				v := types.VisibilityProtected
				return &v
			}
			v := types.VisibilityPublic
			return &v
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			// import_from_statement: from <module> import <names>
			moduleNode := node.ChildByFieldName("module_name")
			if moduleNode != nil && !moduleNode.IsNull() {
				return moduleNode.Content(source)
			}
			// import_statement: import <names>
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "dotted_name" || child.Type() == "aliased_import" {
					return child.Content(source)
				}
			}
			return ""
		},
	}
}
