package languages

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func rustConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_item"},
		StructTypes:    []string{"struct_item"},
		EnumTypes:      []string{"enum_item"},
		TraitTypes:     []string{"trait_item"},
		ImportTypes:    []string{"use_declaration"},
		TypeAliasTypes: []string{"type_item"},
		ConstantTypes:  []string{"const_item"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "identifier", "type_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			// Check for visibility_modifier with "pub"
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "visibility_modifier" {
					return strings.Contains(child.Content(source), "pub")
				}
			}
			// Check non-named children too
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "visibility_modifier" {
					return strings.Contains(child.Content(source), "pub")
				}
			}
			return false
		},

		GetDocstring: func(node *sitter.Node, source []byte) *string {
			prev := node.PrevNamedSibling()
			if prev == nil || prev.IsNull() {
				return nil
			}
			if prev.Type() == "line_comment" || prev.Type() == "block_comment" {
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
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				if ch.Type() == "async" {
					return true
				}
			}
			return false
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			return false
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "visibility_modifier" {
					content := child.Content(source)
					if strings.Contains(content, "pub") {
						v := types.VisibilityPublic
						return &v
					}
				}
			}
			v := types.VisibilityPrivate
			return &v
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			return nodeContent(firstNamedChildOfTypes(node, "scoped_identifier", "identifier", "use_wildcard"), source)
		},
	}
}
