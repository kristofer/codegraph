// Package frameworks contains framework-specific reference resolvers.
package frameworks

import (
	"encoding/json"
	"strings"

	"github.com/kristofer/codegraph/internal/types"
)

// Ctx is an alias to avoid an import cycle; the resolution package passes its
// *ResolutionContext via the interface methods below.
type Ctx interface {
	GetNodesByName(name string) []*types.Node
	GetNodesByKind(kind types.NodeKind) []*types.Node
	GetNodesInFile(filePath string) []*types.Node
	GetAllFiles() []string
	ReadFile(filePath string) string
	FileExists(filePath string) bool
}

// ResolvedRef is the result type returned by framework resolvers.
type ResolvedRef struct {
	FromNodeID   string
	TargetNodeID string
	Confidence   float64
	ResolvedBy   string
}

// Resolver is implemented by every framework-specific resolver.
type Resolver interface {
	Name() string
	Detect(ctx Ctx) bool
	Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef
}

// ===========================================================================
// React
// ===========================================================================

// ReactResolver handles React and Next.js patterns.
type ReactResolver struct{}

func (r *ReactResolver) Name() string { return "react" }

func (r *ReactResolver) Detect(ctx Ctx) bool {
	if content := ctx.ReadFile("package.json"); content != "" {
		var pkg map[string]any
		if err := json.Unmarshal([]byte(content), &pkg); err == nil {
			deps := map[string]any{}
			if d, ok := pkg["dependencies"].(map[string]any); ok {
				for k, v := range d {
					deps[k] = v
				}
			}
			if d, ok := pkg["devDependencies"].(map[string]any); ok {
				for k, v := range d {
					deps[k] = v
				}
			}
			if _, ok := deps["react"]; ok {
				return true
			}
			if _, ok := deps["next"]; ok {
				return true
			}
		}
	}
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".jsx") || strings.HasSuffix(f, ".tsx") {
			return true
		}
	}
	return false
}

func (r *ReactResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	name := ref.ReferenceName
	// Component reference (PascalCase)
	if isPascalCase(name) && !isReactBuiltInType(name) {
		if id := findComponentNode(name, ctx); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.8, ResolvedBy: "framework"}
		}
	}
	// Hook reference (useXxx)
	if strings.HasPrefix(name, "use") && len(name) > 3 {
		if id := findByNameAndKind(name, ctx, types.NodeKindFunction); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.85, ResolvedBy: "framework"}
		}
	}
	// Context / Provider
	if strings.HasSuffix(name, "Context") || strings.HasSuffix(name, "Provider") {
		if id := findByNameAndKind(name, ctx, types.NodeKindVariable, types.NodeKindConstant, types.NodeKindFunction); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.8, ResolvedBy: "framework"}
		}
	}
	return nil
}

func isPascalCase(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func isReactBuiltInType(s string) bool {
	builtins := map[string]bool{
		"React": true, "Component": true, "PureComponent": true,
		"Fragment": true, "StrictMode": true, "Suspense": true,
		"String": true, "Number": true, "Boolean": true, "Object": true,
		"Array": true, "Promise": true, "Error": true, "Map": true, "Set": true,
	}
	return builtins[s]
}

func findComponentNode(name string, ctx Ctx) string {
	for _, n := range ctx.GetNodesByName(name) {
		if n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindClass || n.Kind == types.NodeKindComponent {
			return n.ID
		}
	}
	return ""
}

func findByNameAndKind(name string, ctx Ctx, kinds ...types.NodeKind) string {
	kindSet := make(map[types.NodeKind]bool, len(kinds))
	for _, k := range kinds {
		kindSet[k] = true
	}
	for _, n := range ctx.GetNodesByName(name) {
		if kindSet[n.Kind] {
			return n.ID
		}
	}
	return ""
}

// ===========================================================================
// Express
// ===========================================================================

// ExpressResolver handles Express.js route patterns.
type ExpressResolver struct{}

func (e *ExpressResolver) Name() string { return "express" }

func (e *ExpressResolver) Detect(ctx Ctx) bool {
	if content := ctx.ReadFile("package.json"); content != "" {
		var pkg map[string]any
		if err := json.Unmarshal([]byte(content), &pkg); err == nil {
			if d, ok := pkg["dependencies"].(map[string]any); ok {
				if _, has := d["express"]; has {
					return true
				}
			}
		}
	}
	return false
}

func (e *ExpressResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	name := ref.ReferenceName
	// Route handler patterns (get, post, put, delete)
	if strings.HasSuffix(name, "Handler") || strings.HasSuffix(name, "Middleware") || strings.HasSuffix(name, "Controller") {
		if id := findByNameAndKind(name, ctx, types.NodeKindFunction, types.NodeKindMethod, types.NodeKindClass); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.75, ResolvedBy: "framework"}
		}
	}
	return nil
}

// ===========================================================================
// Django / FastAPI / Flask
// ===========================================================================

// DjangoResolver handles Django view/model patterns.
type DjangoResolver struct{}

func (d *DjangoResolver) Name() string { return "django" }

func (d *DjangoResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, "settings.py") || strings.HasSuffix(f, "urls.py") || strings.HasSuffix(f, "models.py") {
			return true
		}
	}
	return false
}

func (d *DjangoResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	name := ref.ReferenceName
	// Django model references (PascalCase ending with common model suffixes)
	if isPascalCase(name) && (strings.HasSuffix(name, "View") || strings.HasSuffix(name, "Serializer") || strings.HasSuffix(name, "Form")) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.8, ResolvedBy: "framework"}
		}
	}
	return nil
}

// ===========================================================================
// Spring (Java)
// ===========================================================================

// SpringResolver handles Spring Framework patterns.
type SpringResolver struct{}

func (s *SpringResolver) Name() string { return "spring" }

func (s *SpringResolver) Detect(ctx Ctx) bool {
	content := ctx.ReadFile("pom.xml")
	if content == "" {
		content = ctx.ReadFile("build.gradle")
	}
	return strings.Contains(content, "spring")
}

func (s *SpringResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	name := ref.ReferenceName
	if isPascalCase(name) && (strings.HasSuffix(name, "Service") || strings.HasSuffix(name, "Repository") ||
		strings.HasSuffix(name, "Controller") || strings.HasSuffix(name, "Component")) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass, types.NodeKindInterface); id != "" {
			return &ResolvedRef{FromNodeID: ref.FromNodeID, TargetNodeID: id, Confidence: 0.8, ResolvedBy: "framework"}
		}
	}
	return nil
}

// ===========================================================================
// Go interfaces
// ===========================================================================

// GoResolver handles Go interface satisfaction patterns.
type GoResolver struct{}

func (g *GoResolver) Name() string { return "go" }

func (g *GoResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".go") {
			return true
		}
	}
	return false
}

func (g *GoResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.Go {
		return nil
	}
	name := ref.ReferenceName
	// Interface references
	if isPascalCase(name) {
		for _, n := range ctx.GetNodesByName(name) {
			if n.Kind == types.NodeKindInterface {
				return &ResolvedRef{
					FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
					Confidence: 0.8, ResolvedBy: "framework",
				}
			}
		}
	}
	return nil
}

// ===========================================================================
// Rust traits
// ===========================================================================

// RustResolver handles Rust trait implementation patterns.
type RustResolver struct{}

func (r *RustResolver) Name() string { return "rust" }

func (r *RustResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".rs") {
			return true
		}
	}
	return false
}

func (r *RustResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.Rust {
		return nil
	}
	name := ref.ReferenceName
	if isPascalCase(name) {
		for _, n := range ctx.GetNodesByName(name) {
			if n.Kind == types.NodeKindTrait || n.Kind == types.NodeKindStruct {
				return &ResolvedRef{
					FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
					Confidence: 0.75, ResolvedBy: "framework",
				}
			}
		}
	}
	return nil
}

// ===========================================================================
// Laravel (PHP)
// ===========================================================================

// LaravelResolver handles Laravel facade / model patterns.
type LaravelResolver struct{}

func (l *LaravelResolver) Name() string { return "laravel" }

func (l *LaravelResolver) Detect(ctx Ctx) bool {
	return ctx.FileExists("artisan") || ctx.FileExists("app/Http/Controllers")
}

func (l *LaravelResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.PHP {
		return nil
	}
	name := ref.ReferenceName
	if isPascalCase(name) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass); id != "" {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: id,
				Confidence: 0.75, ResolvedBy: "framework",
			}
		}
	}
	return nil
}

// ===========================================================================
// Rails (Ruby)
// ===========================================================================

// RailsResolver handles Ruby on Rails naming conventions.
type RailsResolver struct{}

func (r *RailsResolver) Name() string { return "rails" }

func (r *RailsResolver) Detect(ctx Ctx) bool {
	return ctx.FileExists("Gemfile") && ctx.FileExists("config/routes.rb")
}

func (r *RailsResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.Ruby {
		return nil
	}
	name := ref.ReferenceName
	if isPascalCase(name) && (strings.HasSuffix(name, "Controller") || strings.HasSuffix(name, "Mailer") || strings.HasSuffix(name, "Job")) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass); id != "" {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: id,
				Confidence: 0.8, ResolvedBy: "framework",
			}
		}
	}
	return nil
}

// ===========================================================================
// ASP.NET (C#)
// ===========================================================================

// AspNetResolver handles ASP.NET / C# patterns.
type AspNetResolver struct{}

func (a *AspNetResolver) Name() string { return "aspnet" }

func (a *AspNetResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".csproj") || strings.HasSuffix(f, ".sln") {
			return true
		}
	}
	return false
}

func (a *AspNetResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.CSharp {
		return nil
	}
	name := ref.ReferenceName
	if isPascalCase(name) && (strings.HasSuffix(name, "Controller") || strings.HasSuffix(name, "Service") || strings.HasSuffix(name, "Repository")) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass, types.NodeKindInterface); id != "" {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: id,
				Confidence: 0.8, ResolvedBy: "framework",
			}
		}
	}
	return nil
}

// ===========================================================================
// SwiftUI
// ===========================================================================

// SwiftUIResolver handles SwiftUI / UIKit patterns.
type SwiftUIResolver struct{}

func (s *SwiftUIResolver) Name() string { return "swiftui" }

func (s *SwiftUIResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".swift") {
			return true
		}
	}
	return false
}

func (s *SwiftUIResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	if ref.Language != types.Swift {
		return nil
	}
	name := ref.ReferenceName
	if isPascalCase(name) && (strings.HasSuffix(name, "View") || strings.HasSuffix(name, "ViewController") || strings.HasSuffix(name, "ViewModel")) {
		if id := findByNameAndKind(name, ctx, types.NodeKindClass, types.NodeKindStruct); id != "" {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: id,
				Confidence: 0.8, ResolvedBy: "framework",
			}
		}
	}
	return nil
}

// ===========================================================================
// Svelte
// ===========================================================================

// SvelteResolver handles Svelte component patterns.
type SvelteResolver struct{}

func (s *SvelteResolver) Name() string { return "svelte" }

func (s *SvelteResolver) Detect(ctx Ctx) bool {
	for _, f := range ctx.GetAllFiles() {
		if strings.HasSuffix(f, ".svelte") {
			return true
		}
	}
	return false
}

func (s *SvelteResolver) Resolve(ref *types.UnresolvedReference, ctx Ctx) *ResolvedRef {
	name := ref.ReferenceName
	if isPascalCase(name) && strings.HasSuffix(name, ".svelte") {
		if id := findByNameAndKind(name, ctx, types.NodeKindComponent, types.NodeKindFile); id != "" {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: id,
				Confidence: 0.85, ResolvedBy: "framework",
			}
		}
	}
	return nil
}

// ===========================================================================
// All returns the full list of built-in framework resolvers.
// ===========================================================================

// All returns all built-in framework resolvers.
func All() []Resolver {
	return []Resolver{
		&ReactResolver{},
		&ExpressResolver{},
		&DjangoResolver{},
		&SpringResolver{},
		&GoResolver{},
		&RustResolver{},
		&LaravelResolver{},
		&RailsResolver{},
		&AspNetResolver{},
		&SwiftUIResolver{},
		&SvelteResolver{},
	}
}
