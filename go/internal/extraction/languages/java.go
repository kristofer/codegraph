package languages

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func javaConfig() *LangConfig {
	return &LangConfig{
		MethodTypes:    []string{"method_declaration", "constructor_declaration"},
		ClassTypes:     []string{"class_declaration"},
		InterfaceTypes: []string{"interface_declaration"},
		EnumTypes:      []string{"enum_declaration"},
		ImportTypes:    []string{"import_declaration"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "identifier", "type_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			return hasModifier(node, source, "public")
		},

		GetDocstring: func(node *sitter.Node, source []byte) *string {
			prev := node.PrevNamedSibling()
			if prev == nil || prev.IsNull() {
				return nil
			}
			if prev.Type() == "block_comment" || prev.Type() == "line_comment" {
				s := prev.Content(source)
				return &s
			}
			return nil
		},

		GetSignature: func(node *sitter.Node, source []byte) *string {
			sig := extractSignatureLine(node, source)
			if sig == "" {
				return nil
			}
			return &sig
		},

		IsAsync: func(node *sitter.Node, source []byte) bool {
			return false
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			return hasModifier(node, source, "static")
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			modifiers := getModifiers(node, source)
			switch {
			case strings.Contains(modifiers, "public"):
				v := types.VisibilityPublic
				return &v
			case strings.Contains(modifiers, "private"):
				v := types.VisibilityPrivate
				return &v
			case strings.Contains(modifiers, "protected"):
				v := types.VisibilityProtected
				return &v
			}
			v := types.VisibilityInternal
			return &v
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "scoped_identifier" {
					return child.Content(source)
				}
			}
			return ""
		},
	}
}

func hasModifier(node *sitter.Node, source []byte, mod string) bool {
	return strings.Contains(getModifiers(node, source), mod)
}

func getModifiers(node *sitter.Node, source []byte) string {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() == "modifiers" {
			return child.Content(source)
		}
	}
	return ""
}
