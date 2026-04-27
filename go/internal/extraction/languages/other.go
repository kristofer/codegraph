package languages

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

func csharpConfig() *LangConfig {
	return &LangConfig{
		MethodTypes:    []string{"method_declaration", "constructor_declaration"},
		ClassTypes:     []string{"class_declaration"},
		InterfaceTypes: []string{"interface_declaration"},
		StructTypes:    []string{"struct_declaration"},
		EnumTypes:      []string{"enum_declaration"},
		ImportTypes:    []string{"using_directive"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			return csharpHasModifier(node, source, "public")
		},

		GetDocstring: func(node *sitter.Node, source []byte) *string {
			prev := node.PrevNamedSibling()
			if prev == nil || prev.IsNull() {
				return nil
			}
			if strings.Contains(prev.Type(), "comment") {
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
			return csharpHasModifier(node, source, "async")
		},

		IsStatic: func(node *sitter.Node, source []byte) bool {
			return csharpHasModifier(node, source, "static")
		},

		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility {
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				switch ch.Content(source) {
				case "public":
					v := types.VisibilityPublic
					return &v
				case "private":
					v := types.VisibilityPrivate
					return &v
				case "protected":
					v := types.VisibilityProtected
					return &v
				case "internal":
					v := types.VisibilityInternal
					return &v
				}
			}
			return nil
		},

		GetImportPath: func(node *sitter.Node, source []byte) string {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "qualified_name" || child.Type() == "identifier" || child.Type() == "name" {
					return child.Content(source)
				}
			}
			return ""
		},
	}
}

func csharpHasModifier(node *sitter.Node, source []byte, mod string) bool {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Content(source) == mod {
			return true
		}
	}
	return false
}

func phpConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_definition"},
		MethodTypes:    []string{"method_declaration"},
		ClassTypes:     []string{"class_declaration"},
		InterfaceTypes: []string{"interface_declaration"},
		ImportTypes:    []string{"namespace_use_declaration"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "name", "identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			return true // PHP top-level is accessible
		},

		GetDocstring:  func(node *sitter.Node, source []byte) *string { return nil },
		GetSignature:  func(node *sitter.Node, source []byte) *string { return nil },
		IsAsync:       func(node *sitter.Node, source []byte) bool { return false },
		IsStatic:      func(node *sitter.Node, source []byte) bool { return false },
		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility { return nil },
		GetImportPath: func(node *sitter.Node, source []byte) string { return "" },
	}
}

func rubyConfig() *LangConfig {
	return &LangConfig{
		MethodTypes: []string{"method", "singleton_method"},
		ClassTypes:  []string{"class"},
		ImportTypes: []string{"call"}, // require/require_relative appear as calls in Ruby AST

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "name", "identifier")
		},

		IsExported:    func(node *sitter.Node, source []byte) bool { return true },
		GetDocstring:  func(node *sitter.Node, source []byte) *string { return nil },
		GetSignature:  func(node *sitter.Node, source []byte) *string { return nil },
		IsAsync:       func(node *sitter.Node, source []byte) bool { return false },
		IsStatic:      func(node *sitter.Node, source []byte) bool { return false },
		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility { return nil },
		GetImportPath: func(node *sitter.Node, source []byte) string { return "" },
	}
}

func swiftConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_declaration"},
		MethodTypes:    []string{"function_declaration"},
		ClassTypes:     []string{"class_declaration"},
		StructTypes:    []string{"struct_declaration"},
		InterfaceTypes: []string{"protocol_declaration"},
		EnumTypes:      []string{"enum_declaration"},
		ImportTypes:    []string{"import_declaration"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "name", "identifier", "type_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			for i := 0; i < int(node.ChildCount()); i++ {
				ch := node.Child(i)
				if ch.Content(source) == "public" || ch.Content(source) == "open" {
					return true
				}
			}
			return false
		},

		GetDocstring:  func(node *sitter.Node, source []byte) *string { return nil },
		GetSignature:  func(node *sitter.Node, source []byte) *string { return nil },
		IsAsync:       func(node *sitter.Node, source []byte) bool { return false },
		IsStatic:      func(node *sitter.Node, source []byte) bool { return false },
		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility { return nil },
		GetImportPath: func(node *sitter.Node, source []byte) string { return "" },
	}
}

func kotlinConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_declaration"},
		MethodTypes:    []string{"function_declaration"},
		ClassTypes:     []string{"class_declaration"},
		InterfaceTypes: []string{"interface_declaration"},
		ImportTypes:    []string{"import_header"},

		GetName: func(node *sitter.Node, source []byte) string {
			return getNameField(node, source, "simple_identifier", "identifier", "type_identifier")
		},

		IsExported:    func(node *sitter.Node, source []byte) bool { return true },
		GetDocstring:  func(node *sitter.Node, source []byte) *string { return nil },
		GetSignature:  func(node *sitter.Node, source []byte) *string { return nil },
		IsAsync:       func(node *sitter.Node, source []byte) bool { return false },
		IsStatic:      func(node *sitter.Node, source []byte) bool { return false },
		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility { return nil },
		GetImportPath: func(node *sitter.Node, source []byte) string { return "" },
	}
}

func cCppConfig() *LangConfig {
	return &LangConfig{
		FunctionTypes:  []string{"function_definition"},
		ClassTypes:     []string{"class_specifier"},
		StructTypes:    []string{"struct_specifier"},
		EnumTypes:      []string{"enum_specifier"},
		ImportTypes:    []string{"preproc_include"},
		TypeAliasTypes: []string{"type_definition"},

		GetName: func(node *sitter.Node, source []byte) string {
			// function_definition has a declarator → identifier
			decl := node.ChildByFieldName("declarator")
			if decl != nil && !decl.IsNull() {
				return extractDeclaratorName(decl, source)
			}
			return getNameField(node, source, "name", "identifier", "type_identifier")
		},

		IsExported: func(node *sitter.Node, source []byte) bool {
			return true
		},

		GetDocstring:  func(node *sitter.Node, source []byte) *string { return nil },
		GetSignature:  func(node *sitter.Node, source []byte) *string { return nil },
		IsAsync:       func(node *sitter.Node, source []byte) bool { return false },
		IsStatic:      func(node *sitter.Node, source []byte) bool { return false },
		GetVisibility: func(node *sitter.Node, source []byte) *types.Visibility { return nil },
		GetImportPath: func(node *sitter.Node, source []byte) string {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "string_literal" || child.Type() == "system_lib_string" {
					s := child.Content(source)
					s = strings.Trim(s, `"<>`)
					return s
				}
			}
			return ""
		},
	}
}

func extractDeclaratorName(node *sitter.Node, source []byte) string {
	if node == nil || node.IsNull() {
		return ""
	}
	t := node.Type()
	if t == "identifier" {
		return node.Content(source)
	}
	// function_declarator, pointer_declarator, etc. - recurse
	decl := node.ChildByFieldName("declarator")
	if decl != nil && !decl.IsNull() {
		return extractDeclaratorName(decl, source)
	}
	return getNameField(node, source, "identifier")
}

func svelteConfig() *LangConfig {
	// Svelte uses embedded JS/TS - reuse JS config for script blocks
	cfg := javascriptConfig()
	return cfg
}
