package resolution

import (
	"strings"

	"github.com/kristofer/codegraph/internal/types"
)

// MatchReference tries all name-matching strategies in order of confidence and
// returns the best result, or nil if nothing could be matched.
func MatchReference(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	if result := matchByFilePath(ref, ctx); result != nil {
		return result
	}
	if result := matchByQualifiedName(ref, ctx); result != nil {
		return result
	}
	if result := matchMethodCall(ref, ctx); result != nil {
		return result
	}
	if result := matchByExactName(ref, ctx); result != nil {
		return result
	}
	return matchFuzzy(ref, ctx)
}

// matchByFilePath resolves path-like references (e.g. "snippets/drawer.liquid")
// by matching the filename segment against file nodes.
func matchByFilePath(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	if !strings.Contains(ref.ReferenceName, "/") {
		return nil
	}
	parts := strings.Split(ref.ReferenceName, "/")
	fileName := parts[len(parts)-1]
	if fileName == "" {
		return nil
	}

	candidates := ctx.GetNodesByName(fileName)
	var fileNodes []*types.Node
	for _, n := range candidates {
		if n.Kind == types.NodeKindFile {
			fileNodes = append(fileNodes, n)
		}
	}
	if len(fileNodes) == 0 {
		return nil
	}

	// Prefer exact path match
	for _, n := range fileNodes {
		if n.QualifiedName == ref.ReferenceName || n.FilePath == ref.ReferenceName {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
				Confidence: 0.95, ResolvedBy: "file-path",
			}
		}
	}
	// Suffix match
	for _, n := range fileNodes {
		if strings.HasSuffix(n.QualifiedName, ref.ReferenceName) || strings.HasSuffix(n.FilePath, ref.ReferenceName) {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
				Confidence: 0.85, ResolvedBy: "file-path",
			}
		}
	}
	if len(fileNodes) == 1 {
		return &ResolvedRef{
			FromNodeID: ref.FromNodeID, TargetNodeID: fileNodes[0].ID,
			Confidence: 0.7, ResolvedBy: "file-path",
		}
	}
	return nil
}

// matchByQualifiedName resolves references that contain "::" or "." separators
// by looking up the qualified name directly.
func matchByQualifiedName(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	if !strings.Contains(ref.ReferenceName, "::") && !strings.Contains(ref.ReferenceName, ".") {
		return nil
	}

	candidates := ctx.GetNodesByQualifiedName(ref.ReferenceName)
	if len(candidates) == 1 {
		return &ResolvedRef{
			FromNodeID: ref.FromNodeID, TargetNodeID: candidates[0].ID,
			Confidence: 0.95, ResolvedBy: "qualified-name",
		}
	}

	// Partial match: try the last segment
	parts := strings.FieldsFunc(ref.ReferenceName, func(r rune) bool {
		return r == ':' || r == '.'
	})
	lastName := parts[len(parts)-1]
	if lastName != "" {
		for _, n := range ctx.GetNodesByName(lastName) {
			if strings.HasSuffix(n.QualifiedName, ref.ReferenceName) {
				return &ResolvedRef{
					FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
					Confidence: 0.85, ResolvedBy: "qualified-name",
				}
			}
		}
	}
	return nil
}

// matchMethodCall handles "obj.method" and "Class::method" patterns.
func matchMethodCall(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	var objectOrClass, methodName string

	if idx := strings.Index(ref.ReferenceName, "::"); idx > 0 {
		objectOrClass = ref.ReferenceName[:idx]
		methodName = ref.ReferenceName[idx+2:]
	} else if idx := strings.Index(ref.ReferenceName, "."); idx > 0 {
		objectOrClass = ref.ReferenceName[:idx]
		methodName = ref.ReferenceName[idx+1:]
	}

	if objectOrClass == "" || methodName == "" {
		return nil
	}

	// Strategy 1: direct class name match
	for _, classNode := range ctx.GetNodesByName(objectOrClass) {
		if classNode.Kind != types.NodeKindClass && classNode.Kind != types.NodeKindStruct && classNode.Kind != types.NodeKindInterface {
			continue
		}
		if classNode.Language != ref.Language {
			continue
		}
		for _, n := range ctx.GetNodesInFile(classNode.FilePath) {
			if n.Kind == types.NodeKindMethod && n.Name == methodName &&
				strings.Contains(n.QualifiedName, classNode.Name) {
				return &ResolvedRef{
					FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
					Confidence: 0.85, ResolvedBy: "qualified-name",
				}
			}
		}
	}

	// Strategy 2: capitalised receiver (e.g., permissionEngine → PermissionEngine)
	capitalized := strings.ToUpper(objectOrClass[:1]) + objectOrClass[1:]
	if capitalized != objectOrClass {
		for _, classNode := range ctx.GetNodesByName(capitalized) {
			if classNode.Kind != types.NodeKindClass && classNode.Kind != types.NodeKindStruct && classNode.Kind != types.NodeKindInterface {
				continue
			}
			if classNode.Language != ref.Language {
				continue
			}
			for _, n := range ctx.GetNodesInFile(classNode.FilePath) {
				if n.Kind == types.NodeKindMethod && n.Name == methodName &&
					strings.Contains(n.QualifiedName, classNode.Name) {
					return &ResolvedRef{
						FromNodeID: ref.FromNodeID, TargetNodeID: n.ID,
						Confidence: 0.8, ResolvedBy: "instance-method",
					}
				}
			}
		}
	}

	// Strategy 3: scan all methods with this name; score by word overlap with receiver
	if methodName != "" {
		methodCandidates := ctx.GetNodesByName(methodName)
		var methods []*types.Node
		for _, n := range methodCandidates {
			if n.Kind == types.NodeKindMethod {
				methods = append(methods, n)
			}
		}
		// Prefer same language
		var sameLang []*types.Node
		for _, n := range methods {
			if n.Language == ref.Language {
				sameLang = append(sameLang, n)
			}
		}
		target := methods
		if len(sameLang) > 0 {
			target = sameLang
		}
		if len(target) == 1 && target[0].Language == ref.Language {
			return &ResolvedRef{
				FromNodeID: ref.FromNodeID, TargetNodeID: target[0].ID,
				Confidence: 0.7, ResolvedBy: "instance-method",
			}
		}
		if len(target) > 1 {
			receiverWords := splitCamelCase(objectOrClass)
			var best *types.Node
			bestScore := 0
			for _, m := range target {
				classWords := splitCamelCase(m.QualifiedName)
				score := 0
				for _, rw := range receiverWords {
					for _, cw := range classWords {
						if strings.EqualFold(cw, rw) {
							score++
						}
					}
				}
				if m.Language == ref.Language {
					score++
				}
				if score > bestScore {
					bestScore = score
					best = m
				}
			}
			if best != nil && bestScore >= 2 {
				return &ResolvedRef{
					FromNodeID: ref.FromNodeID, TargetNodeID: best.ID,
					Confidence: 0.65, ResolvedBy: "instance-method",
				}
			}
		}
	}
	return nil
}

// matchByExactName resolves by exact simple name match.
func matchByExactName(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	candidates := ctx.GetNodesByName(ref.ReferenceName)
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		crossLang := candidates[0].Language != ref.Language
		conf := 0.9
		if crossLang {
			conf = 0.5
		}
		return &ResolvedRef{
			FromNodeID: ref.FromNodeID, TargetNodeID: candidates[0].ID,
			Confidence: conf, ResolvedBy: "exact-match",
		}
	}
	best := findBestMatch(ref, candidates)
	if best == nil {
		return nil
	}
	prox := computePathProximity(ref.FilePath, best.FilePath)
	conf := 0.7
	if prox < 30 {
		conf = 0.4
	}
	return &ResolvedRef{
		FromNodeID: ref.FromNodeID, TargetNodeID: best.ID,
		Confidence: conf, ResolvedBy: "exact-match",
	}
}

// matchFuzzy resolves by case-insensitive name match.
func matchFuzzy(ref *types.UnresolvedReference, ctx *ResolutionContext) *ResolvedRef {
	lowerName := strings.ToLower(ref.ReferenceName)
	candidates := ctx.GetNodesByLowerName(lowerName)

	callableKinds := map[types.NodeKind]bool{
		types.NodeKindFunction: true,
		types.NodeKindMethod:   true,
		types.NodeKindClass:    true,
	}
	var callable []*types.Node
	for _, n := range candidates {
		if callableKinds[n.Kind] {
			callable = append(callable, n)
		}
	}

	var sameLanguage []*types.Node
	for _, n := range callable {
		if n.Language == ref.Language {
			sameLanguage = append(sameLanguage, n)
		}
	}
	final := callable
	if len(sameLanguage) > 0 {
		final = sameLanguage
	}

	if len(final) == 1 {
		crossLang := final[0].Language != ref.Language
		conf := 0.5
		if crossLang {
			conf = 0.3
		}
		return &ResolvedRef{
			FromNodeID: ref.FromNodeID, TargetNodeID: final[0].ID,
			Confidence: conf, ResolvedBy: "fuzzy",
		}
	}
	return nil
}

// ===========================================================================
// Helpers
// ===========================================================================

// splitCamelCase splits a camelCase or PascalCase identifier into words.
func splitCamelCase(s string) []string {
	// Insert a space before each uppercase letter that follows a lowercase letter
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := runes[i-1]
			if prev >= 'a' && prev <= 'z' {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	raw := b.String()
	var words []string
	for _, w := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == '_' || r == '.' || r == ':' || r == '/' || r == '\\'
	}) {
		if len(w) > 1 {
			words = append(words, w)
		}
	}
	return words
}

// computePathProximity returns a score based on shared directory segments
// (higher = closer in the directory tree), capped at 80.
func computePathProximity(path1, path2 string) int {
	dir1 := strings.Split(path1, "/")
	dir1 = dir1[:len(dir1)-1]
	dir2 := strings.Split(path2, "/")
	dir2 = dir2[:len(dir2)-1]

	shared := 0
	for i := 0; i < len(dir1) && i < len(dir2); i++ {
		if dir1[i] == dir2[i] {
			shared++
		} else {
			break
		}
	}
	score := shared * 15
	if score > 80 {
		score = 80
	}
	return score
}

// findBestMatch scores multiple candidates and returns the best one.
func findBestMatch(ref *types.UnresolvedReference, candidates []*types.Node) *types.Node {
	bestScore := -1000
	var best *types.Node
	for _, c := range candidates {
		score := 0
		if c.FilePath == ref.FilePath {
			score += 100
		}
		score += computePathProximity(ref.FilePath, c.FilePath)
		if c.Language == ref.Language {
			score += 50
		} else {
			score -= 80
		}
		if ref.ReferenceKind == types.EdgeKindCalls {
			if c.Kind == types.NodeKindFunction || c.Kind == types.NodeKindMethod {
				score += 25
			}
		}
		if c.IsExported {
			score += 10
		}
		if c.FilePath == ref.FilePath && c.StartLine > 0 {
			dist := ref.Line - c.StartLine
			if dist < 0 {
				dist = -dist
			}
			add := 20 - dist/10
			if add < 0 {
				add = 0
			}
			score += add
		}
		if score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}
