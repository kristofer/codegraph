// Package extraction provides AST-based code extraction using tree-sitter.
package extraction

import (
	"context"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/kristofer/codegraph/internal/types"
)

// parserPool pools tree-sitter parsers to reduce allocation overhead.
type parserPool struct {
	mu      sync.Mutex
	pool    []*sitter.Parser
	maxSize int
}

func newParserPool(maxSize int) *parserPool {
	return &parserPool{maxSize: maxSize}
}

func (p *parserPool) get() *sitter.Parser {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pool) > 0 {
		parser := p.pool[len(p.pool)-1]
		p.pool = p.pool[:len(p.pool)-1]
		return parser
	}
	return sitter.NewParser()
}

func (p *parserPool) put(parser *sitter.Parser) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pool) < p.maxSize {
		p.pool = append(p.pool, parser)
	}
}

// parserPoolMaxSize caps the number of idle parsers retained per pool.
// 32 is chosen to comfortably cover a typical NumCPU workload while
// avoiding unbounded memory growth on large machines.
const parserPoolMaxSize = 32

var globalParserPool = newParserPool(parserPoolMaxSize)
var globalRegistry = NewGrammarRegistry()

// ParseSource parses source code for the given language and returns a tree.
// Returns nil if the language is not supported.
func ParseSource(ctx context.Context, lang types.Language, source []byte) (*sitter.Tree, error) {
	grammar := globalRegistry.Get(lang)
	if grammar == nil {
		return nil, types.ErrLanguageUnsupported
	}

	parser := globalParserPool.get()
	defer globalParserPool.put(parser)

	parser.SetLanguage(grammar)
	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// ExtractFromSource parses and extracts all code symbols from a source file.
func ExtractFromSource(filePath string, source []byte, lang types.Language) *types.ExtractionResult {
	start := time.Now()

	if lang == types.Unknown {
		lang = DetectLanguage(filePath, source)
	}

	ctx := context.Background()

	if !IsLanguageSupported(lang) {
		// Return an empty result for unsupported languages
		return &types.ExtractionResult{
			Nodes: []*types.Node{},
			Edges: []*types.Edge{},
		}
	}

	tree, err := ParseSource(ctx, lang, source)
	if err != nil {
		return &types.ExtractionResult{
			Errors: []types.ExtractionError{
				{
					Message:  err.Error(),
					FilePath: filePath,
					Severity: types.SeverityError,
				},
			},
		}
	}

	cfg := GetConfig(lang)
	walker := NewWalker(cfg, lang, filePath, source)
	result := walker.Walk(tree)
	result.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return result
}
