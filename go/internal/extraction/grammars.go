// Package extraction provides AST-based code extraction using tree-sitter.
package extraction

import (
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/svelte"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/kristofer/codegraph/internal/types"
)

var extensionMap = map[string]types.Language{
	".ts":     types.TypeScript,
	".tsx":    types.TSX,
	".js":     types.JavaScript,
	".mjs":    types.JavaScript,
	".cjs":    types.JavaScript,
	".jsx":    types.JSX,
	".py":     types.Python,
	".pyw":    types.Python,
	".go":     types.Go,
	".rs":     types.Rust,
	".java":   types.Java,
	".c":      types.C,
	".h":      types.C,
	".cpp":    types.CPP,
	".cc":     types.CPP,
	".cxx":    types.CPP,
	".hpp":    types.CPP,
	".hxx":    types.CPP,
	".cs":     types.CSharp,
	".php":    types.PHP,
	".rb":     types.Ruby,
	".rake":   types.Ruby,
	".swift":  types.Swift,
	".kt":     types.Kotlin,
	".kts":    types.Kotlin,
	".dart":   types.Dart,
	".svelte": types.Svelte,
	".liquid": types.Liquid,
	".pas":    types.Pascal,
	".dpr":    types.Pascal,
	".dpk":    types.Pascal,
	".lpr":    types.Pascal,
	".dfm":    types.Pascal,
	".fmx":    types.Pascal,
}

// GrammarRegistry holds lazily-initialized tree-sitter grammars.
type GrammarRegistry struct {
	mu       sync.Mutex
	grammars map[types.Language]*sitter.Language
	once     map[types.Language]*sync.Once
}

// NewGrammarRegistry creates a new GrammarRegistry.
func NewGrammarRegistry() *GrammarRegistry {
	return &GrammarRegistry{
		grammars: make(map[types.Language]*sitter.Language),
		once:     make(map[types.Language]*sync.Once),
	}
}

// Get returns the tree-sitter language for lang, or nil if not supported.
func (r *GrammarRegistry) Get(lang types.Language) *sitter.Language {
	r.mu.Lock()
	if _, ok := r.once[lang]; !ok {
		r.once[lang] = &sync.Once{}
	}
	once := r.once[lang]
	r.mu.Unlock()

	once.Do(func() {
		var l *sitter.Language
		switch lang {
		case types.TypeScript:
			l = tstypescript.GetLanguage()
		case types.TSX:
			l = tsx.GetLanguage()
		case types.JavaScript, types.JSX:
			l = javascript.GetLanguage()
		case types.Python:
			l = python.GetLanguage()
		case types.Go:
			l = golang.GetLanguage()
		case types.Rust:
			l = rust.GetLanguage()
		case types.Java:
			l = java.GetLanguage()
		case types.C:
			l = c.GetLanguage()
		case types.CPP:
			l = cpp.GetLanguage()
		case types.CSharp:
			l = csharp.GetLanguage()
		case types.PHP:
			l = php.GetLanguage()
		case types.Ruby:
			l = ruby.GetLanguage()
		case types.Swift:
			l = swift.GetLanguage()
		case types.Kotlin:
			l = kotlin.GetLanguage()
		case types.Svelte:
			l = svelte.GetLanguage()
		}
		if l != nil {
			r.mu.Lock()
			r.grammars[lang] = l
			r.mu.Unlock()
		}
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.grammars[lang]
}

// DetectLanguage returns the Language for a file path.
// source is optional and used for C/C++ header heuristics.
func DetectLanguage(path string, source []byte) types.Language {
	ext := strings.ToLower(filepath.Ext(path))
	lang, ok := extensionMap[ext]
	if !ok {
		return types.Unknown
	}
	if ext == ".h" && source != nil && looksLikeCPP(source) {
		return types.CPP
	}
	return lang
}

// IsLanguageSupported returns true if a tree-sitter grammar is available for lang.
func IsLanguageSupported(lang types.Language) bool {
	switch lang {
	case types.TypeScript, types.TSX, types.JavaScript, types.JSX,
		types.Python, types.Go, types.Rust, types.Java, types.C, types.CPP,
		types.CSharp, types.PHP, types.Ruby, types.Swift, types.Kotlin, types.Svelte:
		return true
	}
	return false
}

func looksLikeCPP(source []byte) bool {
	s := string(source)
	keywords := []string{"namespace ", "class ", "template<", "template <", "#pragma once", "::"}
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
