package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNodeKindRoundTrip verifies that every NodeKind can be serialized to
// string and parsed back without loss of information.
func TestNodeKindRoundTrip(t *testing.T) {
	cases := []NodeKind{
		NodeKindFile, NodeKindModule, NodeKindClass, NodeKindStruct,
		NodeKindInterface, NodeKindTrait, NodeKindProtocol, NodeKindFunction,
		NodeKindMethod, NodeKindProperty, NodeKindField, NodeKindVariable,
		NodeKindConstant, NodeKindEnum, NodeKindEnumMember, NodeKindTypeAlias,
		NodeKindNamespace, NodeKindParameter, NodeKindImport, NodeKindExport,
		NodeKindRoute, NodeKindComponent,
	}
	for _, kind := range cases {
		t.Run(kind.String(), func(t *testing.T) {
			got, err := ParseNodeKind(kind.String())
			require.NoError(t, err)
			assert.Equal(t, kind, got)
		})
	}
}

// TestParseNodeKindUnknown verifies that an unknown string returns an error.
func TestParseNodeKindUnknown(t *testing.T) {
	_, err := ParseNodeKind("not_a_kind")
	assert.Error(t, err)
}

// TestEdgeKindRoundTrip verifies that every EdgeKind serializes and parses correctly.
func TestEdgeKindRoundTrip(t *testing.T) {
	cases := []EdgeKind{
		EdgeKindContains, EdgeKindCalls, EdgeKindImports, EdgeKindExports,
		EdgeKindExtends, EdgeKindImplements, EdgeKindReferences, EdgeKindTypeOf,
		EdgeKindReturns, EdgeKindInstantiates, EdgeKindOverrides, EdgeKindDecorates,
	}
	for _, kind := range cases {
		t.Run(kind.String(), func(t *testing.T) {
			got, err := ParseEdgeKind(kind.String())
			require.NoError(t, err)
			assert.Equal(t, kind, got)
		})
	}
}

// TestParseEdgeKindUnknown verifies that an unknown string returns an error.
func TestParseEdgeKindUnknown(t *testing.T) {
	_, err := ParseEdgeKind("not_an_edge")
	assert.Error(t, err)
}

// TestLanguageRoundTrip verifies that every Language serializes and parses correctly.
func TestLanguageRoundTrip(t *testing.T) {
	cases := []struct {
		input string
		want  Language
	}{
		{"typescript", TypeScript},
		{"javascript", JavaScript},
		{"tsx", TSX},
		{"jsx", JSX},
		{"python", Python},
		{"go", Go},
		{"rust", Rust},
		{"java", Java},
		{"c", C},
		{"cpp", CPP},
		{"csharp", CSharp},
		{"php", PHP},
		{"ruby", Ruby},
		{"swift", Swift},
		{"kotlin", Kotlin},
		{"dart", Dart},
		{"svelte", Svelte},
		{"liquid", Liquid},
		{"pascal", Pascal},
		{"unknown", Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ParseLanguage(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseLanguageUnknown verifies that an unrecognised string returns Unknown.
func TestParseLanguageUnknown(t *testing.T) {
	got := ParseLanguage("brainfuck")
	assert.Equal(t, Unknown, got)
}

// TestExtractionErrorMessage verifies the error message formatting.
func TestExtractionErrorMessage(t *testing.T) {
	line := 42
	col := 7

	t.Run("with file and line", func(t *testing.T) {
		e := &ExtractionError{
			Message:  "unexpected token",
			FilePath: "src/main.ts",
			Line:     &line,
			Column:   &col,
			Severity: SeverityError,
		}
		assert.Contains(t, e.Error(), "src/main.ts")
		assert.Contains(t, e.Error(), "42")
		assert.Contains(t, e.Error(), "unexpected token")
	})

	t.Run("with file only", func(t *testing.T) {
		e := &ExtractionError{
			Message:  "parse failure",
			FilePath: "src/main.ts",
			Severity: SeverityWarning,
		}
		assert.Contains(t, e.Error(), "src/main.ts")
		assert.Contains(t, e.Error(), "parse failure")
	})

	t.Run("message only", func(t *testing.T) {
		e := &ExtractionError{
			Message:  "generic error",
			Severity: SeverityError,
		}
		assert.Equal(t, "generic error", e.Error())
	})
}
