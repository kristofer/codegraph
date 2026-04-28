package resolution

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kristofer/codegraph/internal/types"
)

// extensionResolution maps a language to the ordered list of extensions/suffixes
// to try when resolving import paths — mirrors import-resolver.ts.
var extensionResolution = map[types.Language][]string{
	types.TypeScript: {".ts", ".tsx", ".d.ts", ".js", ".jsx", "/index.ts", "/index.tsx", "/index.js"},
	types.JavaScript: {".js", ".jsx", ".mjs", ".cjs", "/index.js", "/index.jsx"},
	types.TSX:        {".tsx", ".ts", ".d.ts", ".js", ".jsx", "/index.tsx", "/index.ts", "/index.js"},
	types.JSX:        {".jsx", ".js", "/index.jsx", "/index.js"},
	types.Python:     {".py", "/__init__.py"},
	types.Go:         {".go"},
	types.Rust:       {".rs", "/mod.rs"},
	types.Java:       {".java"},
	types.CSharp:     {".cs"},
	types.PHP:        {".php"},
	types.Ruby:       {".rb"},
}

// commonAliases maps import path prefixes to their filesystem equivalents.
var commonAliases = [][2]string{
	{"@/", "src/"},
	{"~/", "src/"},
	{"@src/", "src/"},
	{"src/", "src/"},
	{"@app/", "app/"},
	{"app/", "app/"},
}

// ResolveImportPath resolves an import path to a file path relative to the
// project root. Returns "" when the import cannot be resolved or is external.
func ResolveImportPath(importPath, fromFile string, lang types.Language, ctx *ResolutionContext) string {
	if isExternalImport(importPath, lang) {
		return ""
	}

	projectRoot := ctx.ProjectRoot
	fromDir := filepath.Dir(filepath.Join(projectRoot, fromFile))
	exts := extensionResolution[lang]

	if strings.HasPrefix(importPath, ".") {
		return resolveRelativeImport(importPath, fromDir, projectRoot, exts, ctx)
	}
	return resolveAliasedImport(importPath, exts, ctx)
}

func isExternalImport(importPath string, lang types.Language) bool {
	if strings.HasPrefix(importPath, ".") {
		return false
	}
	switch lang {
	case types.TypeScript, types.JavaScript, types.TSX, types.JSX:
		nodeBuiltins := map[string]bool{
			"fs": true, "path": true, "os": true, "crypto": true,
			"http": true, "https": true, "url": true, "util": true,
			"events": true, "stream": true, "child_process": true, "buffer": true,
		}
		if nodeBuiltins[importPath] {
			return true
		}
		if !strings.HasPrefix(importPath, "@/") &&
			!strings.HasPrefix(importPath, "~/") &&
			!strings.HasPrefix(importPath, "src/") {
			return true
		}
	case types.Python:
		stdLibs := map[string]bool{
			"os": true, "sys": true, "json": true, "re": true, "math": true,
			"datetime": true, "collections": true, "typing": true,
			"pathlib": true, "logging": true,
		}
		base := strings.SplitN(importPath, ".", 2)[0]
		if stdLibs[base] {
			return true
		}
	case types.Go:
		if !strings.HasPrefix(importPath, ".") && !strings.Contains(importPath, "/internal/") {
			return true
		}
	}
	return false
}

func resolveRelativeImport(importPath, fromDir, projectRoot string, exts []string, ctx *ResolutionContext) string {
	basePath := filepath.Join(fromDir, importPath)
	rel, err := filepath.Rel(projectRoot, basePath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)

	for _, ext := range exts {
		candidate := rel + ext
		if ctx.FileExists(candidate) {
			return candidate
		}
	}
	if ctx.FileExists(rel) {
		return rel
	}
	return ""
}

func resolveAliasedImport(importPath string, exts []string, ctx *ResolutionContext) string {
	for _, alias := range commonAliases {
		prefix, replacement := alias[0], alias[1]
		if strings.HasPrefix(importPath, prefix) {
			resolved := replacement + importPath[len(prefix):]
			for _, ext := range exts {
				if ctx.FileExists(resolved + ext) {
					return resolved + ext
				}
			}
			if ctx.FileExists(resolved) {
				return resolved
			}
		}
	}
	for _, ext := range exts {
		if ctx.FileExists(importPath + ext) {
			return importPath + ext
		}
	}
	return ""
}

// ===========================================================================
// Import Mapping Extraction
// ===========================================================================

// ImportMapping holds a single import binding inside a source file.
type ImportMapping struct {
	LocalName    string
	ExportedName string
	Source       string
	IsDefault    bool
	IsNamespace  bool
}

// ExtractImportMappings parses the text of a source file and returns all
// import bindings it declares.
func ExtractImportMappings(content string, lang types.Language) []ImportMapping {
	switch lang {
	case types.TypeScript, types.JavaScript, types.TSX, types.JSX:
		return extractJSImports(content)
	case types.Python:
		return extractPythonImports(content)
	case types.Go:
		return extractGoImports(content)
	case types.PHP:
		return extractPHPImports(content)
	}
	return nil
}

// jsImportRe matches:  import [default,] { named } [* as ns] from '...'
var jsImportRe = regexp.MustCompile(
	`import\s+(?:(\w+)\s*,?\s*)?(?:\{([^}]+)\})?\s*(?:(\*)\s+as\s+(\w+))?\s*from\s*['"]([^'"]+)['"]`,
)
var jsRequireRe = regexp.MustCompile(
	`(?:const|let|var)\s+(?:(\w+)|\{([^}]+)\})\s*=\s*require\(['"]([^'"]+)['"]\)`,
)

func extractJSImports(content string) []ImportMapping {
	var mappings []ImportMapping
	for _, m := range jsImportRe.FindAllStringSubmatch(content, -1) {
		defaultImport, namedImports, star, nsAlias, source :=
			m[1], m[2], m[3], m[4], m[5]
		if defaultImport != "" {
			mappings = append(mappings, ImportMapping{
				LocalName: defaultImport, ExportedName: "default",
				Source: source, IsDefault: true,
			})
		}
		if namedImports != "" {
			for _, part := range strings.Split(namedImports, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if idx := strings.Index(part, " as "); idx >= 0 {
					orig := strings.TrimSpace(part[:idx])
					local := strings.TrimSpace(part[idx+4:])
					mappings = append(mappings, ImportMapping{
						LocalName: local, ExportedName: orig, Source: source,
					})
				} else {
					mappings = append(mappings, ImportMapping{
						LocalName: part, ExportedName: part, Source: source,
					})
				}
			}
		}
		if star != "" && nsAlias != "" {
			mappings = append(mappings, ImportMapping{
				LocalName: nsAlias, ExportedName: "*", Source: source, IsNamespace: true,
			})
		}
	}
	for _, m := range jsRequireRe.FindAllStringSubmatch(content, -1) {
		defaultName, destructured, source := m[1], m[2], m[3]
		if defaultName != "" {
			mappings = append(mappings, ImportMapping{
				LocalName: defaultName, ExportedName: "default",
				Source: source, IsDefault: true,
			})
		}
		if destructured != "" {
			for _, part := range strings.Split(destructured, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if idx := strings.Index(part, ":"); idx >= 0 {
					orig := strings.TrimSpace(part[:idx])
					local := strings.TrimSpace(part[idx+1:])
					mappings = append(mappings, ImportMapping{
						LocalName: local, ExportedName: orig, Source: source,
					})
				} else {
					mappings = append(mappings, ImportMapping{
						LocalName: part, ExportedName: part, Source: source,
					})
				}
			}
		}
	}
	return mappings
}

var pyFromImportRe = regexp.MustCompile(`from\s+([\w.]+)\s+import\s+([^\n#]+)`)
var pyImportRe = regexp.MustCompile(`(?m)^import\s+([\w.]+)(?:\s+as\s+(\w+))?`)

func extractPythonImports(content string) []ImportMapping {
	var mappings []ImportMapping
	for _, m := range pyFromImportRe.FindAllStringSubmatch(content, -1) {
		source, imports := m[1], m[2]
		for _, name := range strings.Split(imports, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if idx := strings.Index(name, " as "); idx >= 0 {
				orig := strings.TrimSpace(name[:idx])
				local := strings.TrimSpace(name[idx+4:])
				mappings = append(mappings, ImportMapping{
					LocalName: local, ExportedName: orig, Source: source,
				})
			} else if name != "*" {
				mappings = append(mappings, ImportMapping{
					LocalName: name, ExportedName: name, Source: source,
				})
			}
		}
	}
	for _, m := range pyImportRe.FindAllStringSubmatch(content, -1) {
		source, alias := m[1], m[2]
		localName := alias
		if localName == "" {
			parts := strings.Split(source, ".")
			localName = parts[len(parts)-1]
		}
		mappings = append(mappings, ImportMapping{
			LocalName: localName, ExportedName: "*", Source: source, IsNamespace: true,
		})
	}
	return mappings
}

var goSingleImportRe = regexp.MustCompile(`import\s+(?:(\w+)\s+)?["']([^"']+)["']`)
var goBlockImportRe = regexp.MustCompile(`(?s)import\s*\(\s*([^)]+)\s*\)`)
var goBlockLineRe = regexp.MustCompile(`(?:(\w+)\s+)?["']([^"']+)["']`)

func extractGoImports(content string) []ImportMapping {
	var mappings []ImportMapping
	for _, m := range goSingleImportRe.FindAllStringSubmatch(content, -1) {
		alias, source := m[1], m[2]
		parts := strings.Split(source, "/")
		pkgName := alias
		if pkgName == "" {
			pkgName = parts[len(parts)-1]
		}
		mappings = append(mappings, ImportMapping{
			LocalName: pkgName, ExportedName: "*", Source: source, IsNamespace: true,
		})
	}
	for _, bm := range goBlockImportRe.FindAllStringSubmatch(content, -1) {
		block := bm[1]
		for _, lm := range goBlockLineRe.FindAllStringSubmatch(block, -1) {
			alias, source := lm[1], lm[2]
			parts := strings.Split(source, "/")
			pkgName := alias
			if pkgName == "" {
				pkgName = parts[len(parts)-1]
			}
			mappings = append(mappings, ImportMapping{
				LocalName: pkgName, ExportedName: "*", Source: source, IsNamespace: true,
			})
		}
	}
	return mappings
}

var phpUseRe = regexp.MustCompile(`use\s+([\w\\]+)(?:\s+as\s+(\w+))?;`)

func extractPHPImports(content string) []ImportMapping {
	var mappings []ImportMapping
	for _, m := range phpUseRe.FindAllStringSubmatch(content, -1) {
		fullPath, alias := m[1], m[2]
		parts := strings.Split(fullPath, "\\")
		className := parts[len(parts)-1]
		localName := alias
		if localName == "" {
			localName = className
		}
		mappings = append(mappings, ImportMapping{
			LocalName: localName, ExportedName: className, Source: fullPath,
		})
	}
	return mappings
}

// ===========================================================================
// ResolveViaImport
// ===========================================================================

// ResolveViaImport attempts to resolve ref by looking up import mappings in
// ref's file and then finding the exported symbol in the resolved target file.
func ResolveViaImport(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	content := ctx.ReadFile(ref.FilePath)
	if content == "" {
		return nil
	}
	imports := ExtractImportMappings(content, ref.Language)
	if len(imports) == 0 {
		return nil
	}

	for _, imp := range imports {
		if imp.LocalName != ref.ReferenceName &&
			!strings.HasPrefix(ref.ReferenceName, imp.LocalName+".") {
			continue
		}

		resolvedPath := ResolveImportPath(imp.Source, ref.FilePath, ref.Language, ctx)
		if resolvedPath == "" {
			continue
		}

		nodesInFile := ctx.GetNodesInFile(resolvedPath)
		var target *types.Node

		if imp.IsDefault {
			for _, n := range nodesInFile {
				if n.IsExported && (n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindClass) {
					target = n
					break
				}
			}
		} else if imp.IsNamespace {
			memberName := strings.TrimPrefix(ref.ReferenceName, imp.LocalName+".")
			for _, n := range nodesInFile {
				if n.Name == memberName && n.IsExported {
					target = n
					break
				}
			}
		} else {
			for _, n := range nodesInFile {
				if n.Name == imp.ExportedName && n.IsExported {
					target = n
					break
				}
			}
		}

		if target != nil {
			return &ResolvedRef{
				FromNodeID:   ref.FromNodeID,
				TargetNodeID: target.ID,
				Confidence:   0.9,
				ResolvedBy:   "import",
			}
		}
	}
	return nil
}

// fileExistsOnDisk checks whether a file exists on the local filesystem
// relative to projectRoot. Used as a fallback when a file is not yet indexed.
func fileExistsOnDisk(projectRoot, filePath string) bool {
	full := filepath.Join(projectRoot, filePath)
	_, err := os.Stat(full)
	return err == nil
}
