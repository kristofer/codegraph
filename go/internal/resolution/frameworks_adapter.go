package resolution

import (
	"github.com/kristofer/codegraph/internal/resolution/frameworks"
	"github.com/kristofer/codegraph/internal/types"
)

// frameworkAdapter wraps a frameworks.Resolver so it satisfies the
// FrameworkResolver interface used inside the resolution orchestrator.
//
// *ResolutionContext already satisfies frameworks.Ctx because it exposes all
// the same methods (GetNodesByName, GetNodesInFile, FileExists, ReadFile, …).
type frameworkAdapter struct {
	inner frameworks.Resolver
}

func (a *frameworkAdapter) Name() string { return a.inner.Name() }

func (a *frameworkAdapter) Detect(ctx *ResolutionContext) bool {
	return a.inner.Detect(ctx)
}

func (a *frameworkAdapter) Resolve(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	if result := a.inner.Resolve(ref, ctx); result != nil {
		return &ResolvedRef{
			FromNodeID:   result.FromNodeID,
			TargetNodeID: result.TargetNodeID,
			Confidence:   result.Confidence,
			ResolvedBy:   result.ResolvedBy,
		}
	}
	return nil
}

// DetectFrameworks detects which framework resolvers are active for the
// current project and returns them wrapped as FrameworkResolvers.
func DetectFrameworks(ctx *ResolutionContext) []FrameworkResolver {
	all := frameworks.All()
	var active []FrameworkResolver
	for _, fw := range all {
		func() {
			defer func() { recover() }() //nolint:errcheck // ignore panics in detect
			if fw.Detect(ctx) {
				active = append(active, &frameworkAdapter{inner: fw})
			}
		}()
	}
	return active
}
