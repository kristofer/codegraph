package languages

import (
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func goConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes: []string{"function_declaration"},
		MethodTypes:   []string{"method_declaration"},
		// Go structs/interfaces are inside type_spec; handled via StructTypes/InterfaceTypes
		StructTypes:    []string{"type_declaration"},
		InterfaceTypes: []string{},
		ImportTypes:    []string{"import_declaration"},
		ConstantTypes:  []string{"const_declaration"},

		GetName: goGetName,

		IsExported: func(node *sitter.Node, source []byte) bool {
			name := goGetName(node, source)
			if name == "" {
				return false
			}
			r := []rune(name)
			return len(r) > 0 && unicode.IsUpper(r[0])
		},

		GetDocstring: func(node *sitter.Node, source []byte) *string {
			prev := node.PrevNamedSibling()
			if prev == nil || prev.IsNull() {
				return nil
			}
			if prev.Type() == "comment" {
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
			return false // Go has no async/await
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			return false // Go has no static keyword
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			name := goGetName(node, source)
			if name == "" {
				return nil
			}
			r := []rune(name)
			if len(r) > 0 && unicode.IsUpper(r[0]) {
				v := types.VisibilityPublic
				return &v
			}
			v := types.VisibilityPrivate
			return &v
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			// import_declaration may have import_spec or import_spec_list
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				switch child.Type() {
				case "import_spec":
					pathNode := child.ChildByFieldName("path")
					if pathNode != nil && !pathNode.IsNull() {
						s := pathNode.Content(source)
						if len(s) >= 2 {
							s = s[1 : len(s)-1] // strip quotes
						}
						return s
					}
					// Fall back to first string literal child
					for j := 0; j < int(child.NamedChildCount()); j++ {
						grandchild := child.NamedChild(j)
						if grandchild.Type() == "interpreted_string_literal" {
							s := grandchild.Content(source)
							if len(s) >= 2 {
								s = s[1 : len(s)-1]
							}
							return s
						}
					}
				case "import_spec_list":
					// Return the first path
					for j := 0; j < int(child.NamedChildCount()); j++ {
						spec := child.NamedChild(j)
						if spec.Type() == "import_spec" {
							for k := 0; k < int(spec.NamedChildCount()); k++ {
								gch := spec.NamedChild(k)
								if gch.Type() == "interpreted_string_literal" {
									s := gch.Content(source)
									if len(s) >= 2 {
										s = s[1 : len(s)-1]
									}
									return s
								}
							}
						}
					}
				case "interpreted_string_literal":
					s := child.Content(source)
					if len(s) >= 2 {
						s = s[1 : len(s)-1]
					}
					return s
				}
			}
			return ""
		},
	}
}

// goGetName extracts the name from Go nodes.
// For type_declaration it returns the name from the inner type_spec.
// For method_declaration it uses the field_identifier (method name).
func goGetName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "type_declaration":
		// type_declaration → type_spec → name field
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "type_spec" {
				n := child.ChildByFieldName("name")
				if n != nil && !n.IsNull() {
					return n.Content(source)
				}
				// fallback
				for j := 0; j < int(child.NamedChildCount()); j++ {
					gc := child.NamedChild(j)
					if gc.Type() == "type_identifier" {
						return gc.Content(source)
					}
				}
			}
		}
	case "method_declaration":
		// method_declaration: receiver field_identifier params ...
		n := node.ChildByFieldName("name")
		if n != nil && !n.IsNull() {
			return n.Content(source)
		}
		return firstNamedChildContent(node, source, "field_identifier")
	}
	return getNameField(node, source, "identifier", "type_identifier", "field_identifier")
}

func firstNamedChildContent(node *sitter.Node, source []byte, typeName string) string {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() == typeName {
			return child.Content(source)
		}
	}
	return ""
}
