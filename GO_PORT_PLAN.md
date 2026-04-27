# CodeGraph: TypeScript → Go Port Plan

## Overview

This document describes a phased plan for porting CodeGraph from TypeScript/Node.js to Go.
The goal is a standalone, high-performance binary that provides the same MCP server,
CLI, and library interfaces as the current npm package — while eliminating the Node.js
runtime dependency and dramatically improving indexing speed and memory efficiency.

### Why Go?

| Concern | TypeScript / Node.js | Go |
|---|---|---|
| **Distribution** | Requires Node.js runtime; npm install | Single static binary, zero dependencies |
| **Startup latency** | ~200–400 ms (V8 init + module load) | <5 ms |
| **WASM overhead** | tree-sitter grammars loaded as WASM into V8 | Native CGO bindings — no WASM layer |
| **Memory** | WASM heap never shrinks; worker recycling needed | GC-managed; no WASM heap growth |
| **Concurrency** | Worker threads (serialized grammar loads) | Goroutines + channels; parallel parsing |
| **SQLite** | WASM sqlite or native addon | `mattn/go-sqlite3` (CGO) or `modernc.org/sqlite` (pure Go) |
| **Binary size** | ~50 MB node_modules | ~15–25 MB single binary (grammars bundled or downloaded) |
| **Cross-compile** | Complex | `GOOS=windows GOARCH=amd64 go build` |

---

## Dependency Mapping

| TypeScript dependency | Go equivalent | Notes |
|---|---|---|
| `web-tree-sitter` (WASM) | `github.com/smacker/go-tree-sitter` | Native CGO bindings, much faster |
| `tree-sitter-wasms` | Per-language grammar packages (e.g. `go-tree-sitter/typescript`) | Compiled-in at build time |
| `node-sqlite3-wasm` / `better-sqlite3` | `modernc.org/sqlite` (pure Go) or `github.com/mattn/go-sqlite3` (CGO) | Keep same schema |
| `picomatch` | `github.com/bmatcuk/doublestar/v4` | Identical `**` glob semantics |
| `commander` (CLI) | `github.com/spf13/cobra` | Rich subcommand support |
| `@clack/prompts` (interactive TUI) | `github.com/charmbracelet/bubbletea` + `huh` | Native terminal UI |
| Node.js `fs.watch` | `github.com/fsnotify/fsnotify` | Cross-platform (FSEvents/inotify/ReadDirectoryChangesW) |
| `crypto` (hashing) | `crypto/sha256` (stdlib) | Built-in |
| MCP stdio transport | Custom Go implementation | JSON-RPC 2.0 over stdin/stdout |
| `vitest` (testing) | `testing` + `github.com/stretchr/testify` | Standard Go testing |

---

## Repository Layout (Target)

```
codegraph/                         ← existing TypeScript source stays untouched
go/                                ← all Go source lives here
├── cmd/
│   └── codegraph/
│       └── main.go                ← CLI entry point
├── internal/
│   ├── types/
│   │   └── types.go               ← NodeKind, EdgeKind, Language, Node, Edge, ...
│   ├── config/
│   │   └── config.go              ← CodeGraphConfig, defaults, load/save
│   ├── db/
│   │   ├── db.go                  ← Open/close SQLite, schema migration
│   │   ├── queries.go             ← Prepared-statement query layer
│   │   └── schema.sql             ← Same SQL schema as TypeScript version
│   ├── extraction/
│   │   ├── orchestrator.go        ← File scanning + parallel parse pipeline
│   │   ├── grammars.go            ← Language detection, grammar registry
│   │   ├── tree_sitter.go         ← go-tree-sitter wrapper
│   │   └── languages/
│   │       ├── typescript.go
│   │       ├── python.go
│   │       ├── go.go
│   │       ├── rust.go
│   │       ├── java.go
│   │       ├── csharp.go
│   │       ├── php.go
│   │       ├── ruby.go
│   │       ├── swift.go
│   │       ├── kotlin.go
│   │       ├── dart.go
│   │       ├── c_cpp.go
│   │       └── pascal.go
│   ├── resolution/
│   │   ├── resolver.go
│   │   ├── import_resolver.go
│   │   ├── name_matcher.go
│   │   └── frameworks/
│   │       ├── react.go
│   │       ├── express.go
│   │       ├── django.go
│   │       └── ...
│   ├── graph/
│   │   ├── traversal.go           ← BFS/DFS, impact radius, path finding
│   │   └── queries.go             ← High-level graph query helpers
│   ├── context/
│   │   ├── builder.go             ← TaskContext construction
│   │   └── formatter.go           ← Markdown / JSON output
│   ├── sync/
│   │   ├── sync.go                ← Incremental re-index
│   │   └── watcher.go             ← fsnotify-based file watcher
│   ├── mcp/
│   │   ├── server.go              ← MCP server (JSON-RPC 2.0, stdio)
│   │   └── tools.go               ← Tool definitions & handlers
│   └── codegraph/
│       └── codegraph.go           ← Public library API (mirrors src/index.ts)
├── go.mod
├── go.sum
└── Makefile
```

---

## Phase 1 — Project Scaffolding & Tooling

**Goal:** Establish the Go module, CI, build targets, and coding conventions.

### Task 1.1 — Initialize Go module

- Sub-task 1.1.1: Create `go/go.mod` with module path `github.com/kristofer/codegraph` (verify this matches the actual GitHub owner/org; update if the repository lives under a different username or organization)
- Sub-task 1.1.2: Add all direct dependencies to `go.mod` / `go.sum`
- Sub-task 1.1.3: Create `go/Makefile` with targets: `build`, `test`, `lint`, `clean`, `release`
- Sub-task 1.1.4: Add `.golangci.yml` linter configuration (`errcheck`, `govet`, `staticcheck`)

### Task 1.2 — CI pipeline

- Sub-task 1.2.1: Add GitHub Actions workflow `go/ci.yml` (build + test on Linux/macOS/Windows)
- Sub-task 1.2.2: Matrix over `GOOS: [linux, darwin, windows]`, `GOARCH: [amd64, arm64]`
- Sub-task 1.2.3: Upload release artifacts (static binaries) on tag push

### Task 1.3 — Directory layout

- Sub-task 1.3.1: Create skeleton packages under `go/internal/` and `go/cmd/codegraph/`
- Sub-task 1.3.2: Create stub `main.go` that prints version and exits 0
- Sub-task 1.3.3: Verify `go build ./...` succeeds with zero errors

**Tests:** `TestVersion` — smoke test that `main()` does not panic.

---

## Phase 2 — Core Types

**Goal:** Translate all TypeScript type definitions to idiomatic Go structs and constants.

### Task 2.1 — Enumerations as typed strings

- Sub-task 2.1.1: Define `NodeKind` as `type NodeKind string` + `const` block for all 23 values
- Sub-task 2.1.2: Define `EdgeKind` as `type EdgeKind string` + `const` block for 13 values
- Sub-task 2.1.3: Define `Language` as `type Language string` + `const` block for 20 values
- Sub-task 2.1.4: Add `String()` methods and `ParseNodeKind` / `ParseEdgeKind` helpers

### Task 2.2 — Core graph structs

- Sub-task 2.2.1: Translate `Node` interface → `Node` struct with proper Go field tags (json, db)
- Sub-task 2.2.2: Translate `Edge` interface → `Edge` struct
- Sub-task 2.2.3: Translate `FileRecord` → `FileRecord` struct
- Sub-task 2.2.4: Translate `UnresolvedReference` → `UnresolvedReference` struct
- Sub-task 2.2.5: Translate `GraphStats` → `GraphStats` struct

### Task 2.3 — Configuration

- Sub-task 2.3.1: Translate `CodeGraphConfig` → `Config` struct with JSON tags
- Sub-task 2.3.2: Port `DEFAULT_CONFIG` as `DefaultConfig()` constructor (same include/exclude lists)
- Sub-task 2.3.3: Implement `LoadConfig(dir string) (*Config, error)` and `SaveConfig`
- Sub-task 2.3.4: Implement config merging (file config overrides defaults)

### Task 2.4 — Error types

- Sub-task 2.4.1: Define `ExtractionError` struct implementing the `error` interface
- Sub-task 2.4.2: Define sentinel errors (`ErrNotInitialized`, `ErrLanguageUnsupported`, etc.)

**Tests:** `TestNodeKindRoundTrip`, `TestDefaultConfig`, `TestConfigLoadSave`

---

## Phase 3 — Database Layer

**Goal:** Replicate the SQLite schema and prepared-statement query layer in Go.

### Task 3.1 — Choose SQLite driver

- Sub-task 3.1.1: Evaluate `modernc.org/sqlite` (pure Go, CGO-free) vs `mattn/go-sqlite3` (CGO)
  - **Recommendation:** Use `modernc.org/sqlite` for zero-CGO cross-compilation; accept ~10% performance trade-off
- Sub-task 3.1.2: Add chosen driver to `go.mod`
- Sub-task 3.1.3: Verify FTS5 support in chosen driver (required for `nodes_fts`)

### Task 3.2 — Schema management

- Sub-task 3.2.1: Copy `src/db/schema.sql` verbatim to `go/internal/db/schema.sql`
- Sub-task 3.2.2: Embed schema with `//go:embed schema.sql`
- Sub-task 3.2.3: Implement `Open(dbPath string) (*DB, error)` that runs schema on first open
- Sub-task 3.2.4: Implement `Migrate(db *DB)` with version-based migrations

### Task 3.3 — Query layer

Port every method from `src/db/queries.ts` (the `QueryBuilder` class):

- Sub-task 3.3.1: `UpsertNode(node *Node) error`
- Sub-task 3.3.2: `UpsertFileRecord(f *FileRecord) error`
- Sub-task 3.3.3: `InsertEdge(e *Edge) error` / `InsertEdgeBatch(edges []*Edge) error`
- Sub-task 3.3.4: `GetNodeByID(id string) (*Node, error)`
- Sub-task 3.3.5: `GetNodesByFile(filePath string) ([]*Node, error)`
- Sub-task 3.3.6: `SearchNodes(query string, opts SearchOptions) ([]*SearchResult, error)` (FTS5)
- Sub-task 3.3.7: `GetEdges(nodeID string, direction EdgeDirection) ([]*Edge, error)`
- Sub-task 3.3.8: `DeleteFile(filePath string) error` (cascade to nodes/edges)
- Sub-task 3.3.9: `InsertUnresolvedRef(ref *UnresolvedReference) error`
- Sub-task 3.3.10: `GetAllUnresolvedRefs() ([]*UnresolvedReference, error)`
- Sub-task 3.3.11: `GetStats() (*GraphStats, error)`
- Sub-task 3.3.12: Use `BEGIN IMMEDIATE` transactions for batch inserts (WAL mode)

### Task 3.4 — Performance

- Sub-task 3.4.1: Enable WAL mode: `PRAGMA journal_mode=WAL`
- Sub-task 3.4.2: Enable memory-mapped I/O: `PRAGMA mmap_size=268435456`
- Sub-task 3.4.3: Use `PRAGMA synchronous=NORMAL` (safe with WAL)
- Sub-task 3.4.4: Benchmark batch node upsert at 10k nodes — target <500 ms

**Tests:** `TestDBOpen`, `TestNodeUpsertAndGet`, `TestFTSSearch`, `TestEdgeCascadeDelete`,
`TestBatchInsertPerformance`

---

## Phase 4 — AST Extraction Engine

**Goal:** Parse source files using native go-tree-sitter bindings (no WASM).

### Task 4.1 — Grammar registry

- Sub-task 4.1.1: Add `go-tree-sitter` and per-language grammar packages to `go.mod`
  - `github.com/smacker/go-tree-sitter`
  - Language-specific sub-packages (typescript, python, go, rust, java, c, cpp, etc.)
- Sub-task 4.1.2: Implement `GrammarRegistry` with `Get(lang Language) *sitter.Language`
- Sub-task 4.1.3: Port `EXTENSION_MAP` → `extensionMap` map for language detection
- Sub-task 4.1.4: Port `detectLanguage(path, source)` with the `.h` C vs C++ heuristic
- Sub-task 4.1.5: Implement `IsLanguageSupported(lang Language) bool`

### Task 4.2 — Parser wrapper

- Sub-task 4.2.1: Implement `ParseSource(lang Language, source []byte) (*sitter.Tree, error)`
- Sub-task 4.2.2: Use a `sync.Pool` of `*sitter.Parser` per language for goroutine safety
- Sub-task 4.2.3: Implement safe `ExtractFromSource(lang, path, source) (*ExtractionResult, error)`
  wrapping parser + visitor

### Task 4.3 — Language-specific extractors

Each extractor implements `Extractor` interface:
```go
type Extractor interface {
    Extract(tree *sitter.Tree, source []byte, filePath string) (*ExtractionResult, error)
}
```

Port each language file from `src/extraction/languages/`:

- Sub-task 4.3.1: TypeScript / TSX extractor (classes, functions, interfaces, imports, exports, decorators)
- Sub-task 4.3.2: JavaScript / JSX extractor
- Sub-task 4.3.3: Python extractor (classes, functions, decorators, type annotations)
- Sub-task 4.3.4: Go extractor (functions, methods, structs, interfaces, type aliases)
- Sub-task 4.3.5: Rust extractor (structs, enums, impls, traits, functions)
- Sub-task 4.3.6: Java extractor (classes, interfaces, methods, annotations)
- Sub-task 4.3.7: C# extractor
- Sub-task 4.3.8: PHP extractor
- Sub-task 4.3.9: Ruby extractor
- Sub-task 4.3.10: Swift extractor
- Sub-task 4.3.11: Kotlin extractor
- Sub-task 4.3.12: Dart extractor
- Sub-task 4.3.13: C / C++ extractor
- Sub-task 4.3.14: Pascal extractor (note: grammar may need bundling)

### Task 4.4 — Parallel extraction orchestrator

Port `src/extraction/index.ts` `ExtractionOrchestrator`:

- Sub-task 4.4.1: Implement `ScanFiles(root string, cfg *Config) ([]string, error)` with glob filtering
- Sub-task 4.4.2: Implement worker pool: `N = runtime.NumCPU()` goroutines, channel-based work queue
- Sub-task 4.4.3: Hash-based change detection: skip files whose SHA-256 matches stored hash
- Sub-task 4.4.4: Implement progress callback: `func(IndexProgress)`
- Sub-task 4.4.5: Implement `IndexAll(ctx, cfg, db, callback) (*IndexResult, error)`
- Sub-task 4.4.6: Implement `SyncFiles(ctx, cfg, db, changedFiles) (*SyncResult, error)`

**Tests:** `TestDetectLanguage`, `TestExtractTypeScript`, `TestExtractPython`, `TestExtractGo`,
`TestParallelIndexing`, `TestHashChangeDetection`

---

## Phase 5 — Reference Resolution

**Goal:** Resolve import/call references to concrete node IDs after full indexing.

### Task 5.1 — Import resolver

- Sub-task 5.1.1: Port `src/resolution/import-resolver.ts` → `internal/resolution/import_resolver.go`
  - Resolve relative imports (`./utils` → `src/utils.ts`)
  - Resolve module imports for known package managers
- Sub-task 5.1.2: Implement path normalization consistent with extraction

### Task 5.2 — Name matcher

- Sub-task 5.2.1: Port `src/resolution/name-matcher.ts` → `internal/resolution/name_matcher.go`
  - Fuzzy qualified-name matching with scoring
  - Handle language-specific naming conventions

### Task 5.3 — Framework-specific resolvers

Port each framework resolver from `src/resolution/frameworks/`:

- Sub-task 5.3.1: React patterns (component detection, hooks)
- Sub-task 5.3.2: Express patterns (route registration)
- Sub-task 5.3.3: Django / FastAPI patterns
- Sub-task 5.3.4: Laravel patterns
- Sub-task 5.3.5: Ruby on Rails patterns
- Sub-task 5.3.6: Go standard patterns (interface satisfaction)
- Sub-task 5.3.7: Java Spring patterns
- Sub-task 5.3.8: Swift / SwiftUI patterns
- Sub-task 5.3.9: Rust trait implementation patterns
- Sub-task 5.3.10: C# / .NET patterns
- Sub-task 5.3.11: Svelte component patterns

### Task 5.4 — Resolution orchestrator

- Sub-task 5.4.1: Port `ReferenceResolver` orchestrator
- Sub-task 5.4.2: Batch process `unresolved_refs` table after indexing
- Sub-task 5.4.3: Parallel resolution using goroutines (each language batch independent)
- Sub-task 5.4.4: Write resolved edges back to `edges` table with `provenance='heuristic'`

**Tests:** `TestImportResolution`, `TestNameMatcher`, `TestReactResolution`,
`TestUnresolvedRefLifecycle`

---

## Phase 6 — Graph Query Layer

**Goal:** BFS/DFS traversal, call graph construction, impact radius, and high-level helpers.

### Task 6.1 — Graph traverser

- Sub-task 6.1.1: Implement `GraphTraverser` struct with `*db.DB`
- Sub-task 6.1.2: Port `TraverseBFS(startID string, opts TraversalOptions) (*Subgraph, error)`
- Sub-task 6.1.3: Port `TraverseDFS(startID string, opts TraversalOptions) (*Subgraph, error)`
- Sub-task 6.1.4: Port `GetImpactRadius(nodeID string, depth int) (*Subgraph, error)`
  (reverse traversal — incoming `calls`/`imports` edges)
- Sub-task 6.1.5: Port `FindPath(fromID, toID string) ([]Node, error)` (BFS shortest path)

### Task 6.2 — High-level query helpers

- Sub-task 6.2.1: `GetCallers(nodeID string) ([]*Node, error)`
- Sub-task 6.2.2: `GetCallees(nodeID string) ([]*Node, error)`
- Sub-task 6.2.3: `GetCallGraph(rootID string, depth int) (*Subgraph, error)`
- Sub-task 6.2.4: `GetInheritanceChain(classID string) ([]*Node, error)`
- Sub-task 6.2.5: `GetFileStructure(filePath string) (*Subgraph, error)`

**Tests:** `TestBFSTraversal`, `TestDFSTraversal`, `TestImpactRadius`, `TestFindPath`,
`TestCallGraph`

---

## Phase 7 — Context Builder

**Goal:** Produce rich `TaskContext` from a natural-language query — identical output to
the TypeScript `ContextBuilder`.

### Task 7.1 — Symbol extraction from queries

- Sub-task 7.1.1: Port `extractSymbolsFromQuery` — CamelCase, snake_case, SCREAMING_SNAKE, acronyms
- Sub-task 7.1.2: Port `getStemVariants` for plurals / verb forms

### Task 7.2 — Context construction

- Sub-task 7.2.1: Implement `BuildContext(query string, opts BuildContextOptions) (*TaskContext, error)`
  - Step 1: Extract symbol names from query
  - Step 2: FTS5 search for each symbol
  - Step 3: Graph traversal from each entry point
  - Step 4: Score and deduplicate nodes
  - Step 5: Read source code snippets from disk for `CodeBlock`s

### Task 7.3 — Output formatters

- Sub-task 7.3.1: Port `FormatContextAsMarkdown(ctx *TaskContext) string`
- Sub-task 7.3.2: Port `FormatContextAsJSON(ctx *TaskContext) ([]byte, error)`

**Tests:** `TestExtractSymbols`, `TestBuildContext`, `TestMarkdownFormatter`

---

## Phase 8 — Sync and File Watcher

**Goal:** Incremental re-index on file change, with native OS file events.

### Task 8.1 — Incremental sync

- Sub-task 8.1.1: Implement `Sync(ctx, cfg, db) (*SyncResult, error)`
  — walk all tracked files, compare hash, re-index changed files only
- Sub-task 8.1.2: Implement `SyncFile(cfg, db, filePath string) error`
  — re-index a single file
- Sub-task 8.1.3: Handle file deletion: remove nodes/edges for deleted files

### Task 8.2 — File watcher

- Sub-task 8.2.1: Add `fsnotify/fsnotify` to `go.mod`
- Sub-task 8.2.2: Implement `Watcher` struct wrapping `*fsnotify.Watcher`
- Sub-task 8.2.3: Port debounce logic (2-second quiet window after last event)
- Sub-task 8.2.4: Filter events against config include/exclude patterns
- Sub-task 8.2.5: Implement `Watch(ctx, cfg, db, opts WatchOptions) error` (blocks until ctx cancelled)

**Tests:** `TestSync`, `TestSyncDetectsChanges`, `TestWatcherDebounce`

---

## Phase 9 — CLI

**Goal:** Full-featured `codegraph` binary with the same subcommands as the TypeScript version.

### Task 9.1 — Cobra skeleton

- Sub-task 9.1.1: Add `github.com/spf13/cobra` to `go.mod`
- Sub-task 9.1.2: Create `cmd/codegraph/main.go` with root command
- Sub-task 9.1.3: Wire `--version` flag returning `vX.Y.Z`

### Task 9.2 — Subcommands

Implement each subcommand to match the TypeScript CLI:

- Sub-task 9.2.1: `codegraph init [path] [--index]` — create `.codegraph/` directory + config
- Sub-task 9.2.2: `codegraph uninit [path] [--force]` — remove `.codegraph/`
- Sub-task 9.2.3: `codegraph index [path] [--force] [--quiet]` — full indexing
- Sub-task 9.2.4: `codegraph sync [path]` — incremental update
- Sub-task 9.2.5: `codegraph status [path]` — show graph stats table
- Sub-task 9.2.6: `codegraph query <search> [--kind] [--limit] [--json]` — symbol search
- Sub-task 9.2.7: `codegraph files [path] [--format] [--filter] [--max-depth] [--json]`
- Sub-task 9.2.8: `codegraph context <task> [--format] [--max-nodes]` — build context for AI
- Sub-task 9.2.9: `codegraph affected [files...] [--stdin] [--depth] [--filter] [--json]`
- Sub-task 9.2.10: `codegraph serve --mcp` — start MCP server

### Task 9.3 — Interactive installer (optional, Phase 2)

- Sub-task 9.3.1: Implement `codegraph install` using `github.com/charmbracelet/huh` for prompts
- Sub-task 9.3.2: Write `~/.claude.json` MCP server config
- Sub-task 9.3.3: Write `~/.claude/CLAUDE.md` global instructions

**Tests:** `TestCLIVersion`, `TestCLIInit`, `TestCLIIndex`, `TestCLIQuery` (use temp dirs)

---

## Phase 10 — MCP Server

**Goal:** Implement the Model Context Protocol server (JSON-RPC 2.0 over stdio) in Go.

### Task 10.1 — JSON-RPC 2.0 transport

- Sub-task 10.1.1: Implement `StdioTransport` — reads newline-delimited JSON from stdin, writes to stdout
- Sub-task 10.1.2: Implement `JsonRpcRequest` / `JsonRpcResponse` / `JsonRpcError` structs
- Sub-task 10.1.3: Implement dispatcher: route `method` to registered handlers
- Sub-task 10.1.4: Handle concurrent notification vs request processing with a mutex or single goroutine

### Task 10.2 — MCP protocol

- Sub-task 10.2.1: Implement `initialize` handler (return server info + capability list)
- Sub-task 10.2.2: Implement `tools/list` handler (return tool definitions)
- Sub-task 10.2.3: Implement `tools/call` dispatcher (route to tool handlers)
- Sub-task 10.2.4: Implement `notifications/initialized` (no-op, but required)

### Task 10.3 — MCP tools

Port each tool from `src/mcp/tools.ts`:

- Sub-task 10.3.1: `codegraph_search` — FTS5 symbol search
- Sub-task 10.3.2: `codegraph_context` — build task context (markdown or JSON)
- Sub-task 10.3.3: `codegraph_callers` — find callers of a symbol
- Sub-task 10.3.4: `codegraph_callees` — find callees of a symbol
- Sub-task 10.3.5: `codegraph_impact` — impact radius analysis
- Sub-task 10.3.6: `codegraph_node` — get symbol details + source code
- Sub-task 10.3.7: `codegraph_files` — indexed file structure
- Sub-task 10.3.8: `codegraph_status` — graph stats and health
- Sub-task 10.3.9: `codegraph_explore` — deep multi-hop exploration
- Sub-task 10.3.10: Port `getExploreBudget(fileCount)` scaling logic

### Task 10.4 — rootUri → project path resolution

- Sub-task 10.4.1: Port `fileUriToPath(uri)` — `file://` URI decoding including Windows paths
- Sub-task 10.4.2: Port `findNearestCodeGraphRoot(startPath)` — walk up to find `.codegraph/`

**Tests:** `TestMCPInitialize`, `TestMCPToolsList`, `TestMCPSearchTool`,
`TestMCPContextTool`, `TestFileUriToPath`

---

## Phase 11 — Public Library API

**Goal:** Expose an ergonomic Go package API mirroring `src/index.ts`'s `CodeGraph` class.

### Task 11.1 — `codegraph.CodeGraph` struct

```go
// internal/codegraph/codegraph.go

type CodeGraph struct {
    db      *db.DB
    config  *config.Config
    rootDir string
}

func Init(projectDir string) (*CodeGraph, error)
func Open(projectDir string) (*CodeGraph, error)
func (cg *CodeGraph) Close() error

func (cg *CodeGraph) IndexAll(ctx context.Context, cb func(IndexProgress)) (*IndexResult, error)
func (cg *CodeGraph) Sync(ctx context.Context) (*SyncResult, error)

func (cg *CodeGraph) SearchNodes(query string, opts SearchOptions) ([]*SearchResult, error)
func (cg *CodeGraph) GetNode(id string) (*Node, error)
func (cg *CodeGraph) GetCallers(id string) ([]*Node, error)
func (cg *CodeGraph) GetCallees(id string) ([]*Node, error)
func (cg *CodeGraph) GetImpactRadius(id string, depth int) (*Subgraph, error)
func (cg *CodeGraph) GetCallGraph(id string, depth int) (*Subgraph, error)

func (cg *CodeGraph) BuildContext(query string, opts BuildContextOptions) (*TaskContext, error)
func (cg *CodeGraph) GetStats() (*GraphStats, error)

func (cg *CodeGraph) Watch(ctx context.Context, opts WatchOptions) error
```

- Sub-task 11.1.1: Implement all methods above
- Sub-task 11.1.2: Ensure `Init` vs `Open` distinction (Init creates `.codegraph/`, Open requires it)
- Sub-task 11.1.3: Add `context.Context` cancellation support to long-running operations

**Tests:** `TestCodeGraphInitOpen`, `TestIndexAllAndSearch`, `TestBuildContextE2E`

---

## Phase 12 — Testing Coverage

**Goal:** ≥80% code coverage across all packages with idiomatic Go tests.

### Task 12.1 — Unit tests (co-located with implementation)

Each package has `*_test.go` files covering:

| Package | Key test cases |
|---|---|
| `types` | Round-trip parse/string for all enums |
| `config` | Load, save, merge, defaults |
| `db` | Open, schema, CRUD, FTS, cascade delete, WAL |
| `extraction/grammars` | Language detection, all extensions |
| `extraction/languages/typescript` | Class, function, interface, import extraction |
| `extraction/languages/python` | Class, function, decorator, type annotation |
| `extraction/languages/go` | Function, struct, interface, method |
| `extraction/languages/rust` | Struct, enum, impl, trait, fn |
| (all other languages) | Smoke test: parse valid source, ≥1 node extracted |
| `extraction` (orchestrator) | Scan, hash change detect, parallel index |
| `resolution` | Import resolve, name match, framework patterns |
| `graph` | BFS, DFS, impact radius, path finding |
| `context` | Symbol extraction, build context, formatters |
| `sync` | Sync detects add/modify/delete |
| `mcp` | JSON-RPC initialize, tools/list, tools/call |
| `codegraph` (library) | End-to-end: init → index → search → context |

### Task 12.2 — Integration tests

- Sub-task 12.2.1: Create `go/testdata/` with small, language-specific fixture projects
  (one directory per supported language)
- Sub-task 12.2.2: `TestFullIndexCycle_TypeScript` — index fixture, assert expected nodes/edges
- Sub-task 12.2.3: `TestFullIndexCycle_Python`
- Sub-task 12.2.4: `TestFullIndexCycle_Go`
- Sub-task 12.2.5: `TestMCPServerE2E` — spawn MCP server subprocess, send initialize + tools/call,
  assert JSON response

### Task 12.3 — Benchmarks

- Sub-task 12.3.1: `BenchmarkIndexFile_TypeScript` — single 1000-line TS file parse
- Sub-task 12.3.2: `BenchmarkFTSSearch` — search across 50k nodes
- Sub-task 12.3.3: `BenchmarkBFSTraversal` — BFS to depth 5 on 10k-node graph
- Sub-task 12.3.4: `BenchmarkContextBuild` — end-to-end context for a query

### Task 12.4 — Test helpers

- Sub-task 12.4.1: `internal/testutil/testutil.go` — `TempDB(t)`, `TempProject(t, files)` helpers
- Sub-task 12.4.2: `TempDB` opens an in-memory SQLite DB, runs schema, returns `*db.DB` and a cleanup func
- Sub-task 12.4.3: `TempProject` writes fixture files to `t.TempDir()`, returns project root

---

## Phase 13 — Performance Optimizations (Go-Specific)

These optimizations are possible only in Go and not feasible in the TypeScript version:

### Task 13.1 — Parallel parsing

- Sub-task 13.1.1: Use `runtime.NumCPU()` goroutines to parse files in parallel
  (tree-sitter Go bindings are goroutine-safe; each goroutine has its own `sitter.Parser`)
- Sub-task 13.1.2: Implement back-pressure: bounded semaphore limits concurrency to avoid OOM
- Sub-task 13.1.3: Target: index a 5,000-file TypeScript project in <30s (vs ~3–4 min in Node.js)

### Task 13.2 — Batch database writes

- Sub-task 13.2.1: Collect nodes and edges from all parallel parses, flush in a single transaction
- Sub-task 13.2.2: Use `PRAGMA cache_size=-65536` (64 MB page cache)
- Sub-task 13.2.3: Pre-compile all prepared statements at `DB.Open` time

### Task 13.3 — Memory efficiency

- Sub-task 13.3.1: Pool `[]byte` source buffers with `sync.Pool` to reduce GC pressure
- Sub-task 13.3.2: Avoid JSON intermediate representations — map tree-sitter nodes directly to Go structs
- Sub-task 13.3.3: Use `strings.Builder` for all string concatenation in formatters

### Task 13.4 — Single-binary distribution

- Sub-task 13.4.1: Embed Pascal grammar WASM (same as TypeScript version) using `//go:embed`
- Sub-task 13.4.2: Embed `schema.sql` using `//go:embed`
- Sub-task 13.4.3: Build release binaries with `-trimpath -ldflags="-s -w"` for minimal size
- Sub-task 13.4.4: Provide `install.sh` / `install.ps1` scripts for one-line install

---

## Phase 14 — Migration & Compatibility

**Goal:** Users can migrate from the npm package to the Go binary with zero friction.

### Task 14.1 — Data compatibility

- Sub-task 14.1.1: Use identical SQLite schema so existing `.codegraph/codegraph.db` files work
- Sub-task 14.1.2: Test opening a DB written by the TypeScript version from the Go binary

### Task 14.2 — Config compatibility

- Sub-task 14.2.1: Read the same `.codegraph/config.json` format
- Sub-task 14.2.2: Support all existing config keys; unknown keys are silently ignored

### Task 14.3 — MCP tool compatibility

- Sub-task 14.3.1: All 9 MCP tool names and input schemas remain identical
- Sub-task 14.3.2: Output format (markdown / JSON) is byte-for-byte compatible where
  used in automated pipelines

### Task 14.4 — Documentation

- Sub-task 14.4.1: Update `README.md` with Go installation instructions
- Sub-task 14.4.2: Add `MIGRATION.md` explaining differences and upgrade steps
- Sub-task 14.4.3: Deprecation notice in npm package pointing to Go binary

---

## Phased Delivery Schedule

| Phase | Deliverable | Effort |
|---|---|---|
| 1 | Scaffolding, CI, build system | 1–2 days |
| 2 | Core types | 1 day |
| 3 | Database layer | 2–3 days |
| 4 | AST extraction (all languages) | 5–7 days |
| 5 | Reference resolution | 3–4 days |
| 6 | Graph query layer | 2–3 days |
| 7 | Context builder | 2 days |
| 8 | Sync + file watcher | 1–2 days |
| 9 | CLI | 2–3 days |
| 10 | MCP server | 2–3 days |
| 11 | Public library API | 1 day |
| 12 | Test coverage | 3–5 days |
| 13 | Performance optimizations | 2–3 days |
| 14 | Migration & compatibility | 1–2 days |
| **Total** | | **~28–42 developer-days** |

---

## Go Testing Conventions

All tests follow these rules:

1. **Naming:** `TestXxx` for unit, `BenchmarkXxx` for benchmarks, `ExampleXxx` for docs
2. **Fixtures:** Created in `t.TempDir()` — automatically cleaned up; never written to repo root
3. **In-memory SQLite:** Use `file::memory:?cache=shared&mode=memory` DSN for fast DB tests
4. **Assertions:** Use `github.com/stretchr/testify/assert` and `require`
5. **Subtests:** Group related cases with `t.Run(name, func(t *testing.T) {...})`
6. **Table-driven:** Use slice-of-struct tables for parameterized cases
7. **No global state:** Each test creates its own DB instance; parallel tests safe
8. **Race detector:** Run tests with `-race` in CI
9. **Coverage target:** `go test -coverprofile=coverage.out ./...` ≥ 80% total

### Example test pattern (table-driven)

```go
func TestDetectLanguage(t *testing.T) {
    cases := []struct {
        path string
        want Language
    }{
        {"main.ts", TypeScript},
        {"index.js", JavaScript},
        {"main.go", Go},
        {"README.md", Unknown},
    }
    for _, tc := range cases {
        t.Run(tc.path, func(t *testing.T) {
            got := DetectLanguage(tc.path, nil)
            assert.Equal(t, tc.want, got)
        })
    }
}
```

---

## Key Technical Decisions

### tree-sitter bindings

Use `github.com/smacker/go-tree-sitter` which provides native Go (CGO) bindings.
Unlike the TypeScript version that loads WASM at runtime and is limited to single-threaded
grammar loading, the Go version can parse multiple files concurrently using one `sitter.Parser`
per goroutine (parsers are not goroutine-safe but parsers created in different goroutines are independent).

### SQLite driver choice

`modernc.org/sqlite` (pure Go, no CGO) is preferred because:
- Cross-compiles to all platforms with `GOOS`/`GOARCH` without a C toolchain
- FTS5 is included and fully functional
- Roughly equivalent query performance for the access patterns CodeGraph uses

Switch to `mattn/go-sqlite3` if benchmarks show >20% performance difference matters.

### Grammar bundling strategy

- Grammars for all languages are linked in via CGO at compile time (no runtime download)
- This produces a larger binary (~25–35 MB) but eliminates the WASM download step
- Pascal grammar: a pre-compiled native CGO grammar package does not currently exist for
  `go-tree-sitter`. For Phase 1, fall back to embedding the existing WASM grammar (as the
  TypeScript version already does) and executing it via a minimal Go WASM host, **or** write a
  lightweight regex/line-scanner Pascal extractor. A future phase should replace this with a
  proper native CGO grammar once the `go-tree-sitter` ecosystem provides one, removing the only
  remaining WASM dependency.

### Concurrency model

```
main goroutine
  └── Extractor goroutines (N = numCPU)
        ├── goroutine 1: parse + extract file A
        ├── goroutine 2: parse + extract file B
        └── ...
  └── DB writer goroutine (1)
        └── receives ExtractionResult from buffered channel
              └── writes batch every 100 results (one transaction)
```

This pipeline avoids write contention on SQLite while maximising parse parallelism.
