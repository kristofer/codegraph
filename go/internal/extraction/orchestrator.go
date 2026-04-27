// Package extraction provides AST-based code extraction using tree-sitter.
package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kristofer/codegraph/internal/config"
	"github.com/kristofer/codegraph/internal/types"
)

// DB is the persistence interface used by the Orchestrator.
type DB interface {
	UpsertFile(ctx context.Context, rec *types.FileRecord) error
	UpsertNode(ctx context.Context, n *types.Node) error
	UpsertEdge(ctx context.Context, e *types.Edge) error
	UpsertUnresolvedRef(ctx context.Context, ref *types.UnresolvedReference) error
	GetFile(ctx context.Context, path string) (*types.FileRecord, error)
	DeleteNodesForFile(ctx context.Context, filePath string) error
}

// IndexResult summarises a full or incremental index run.
type IndexResult struct {
	FilesProcessed int
	FilesSkipped   int
	NodesExtracted int
	EdgesExtracted int
	Errors         []types.ExtractionError
	DurationMs     float64
}

// Orchestrator coordinates file scanning, hashing, and parallel extraction.
type Orchestrator struct {
	cfg      *config.Config
	db       DB
	registry *GrammarRegistry
	workers  int
}

// NewOrchestrator creates an Orchestrator with the given config and DB.
func NewOrchestrator(cfg *config.Config, db DB) *Orchestrator {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	return &Orchestrator{
		cfg:      cfg,
		db:       db,
		registry: NewGrammarRegistry(),
		workers:  workers,
	}
}

// HashContent returns the hex-encoded SHA-256 hash of data.
func HashContent(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ScanFiles walks rootDir and returns relative paths of all files matching the
// config include patterns (and not matching exclude patterns).
func (o *Orchestrator) ScanFiles(ctx context.Context, rootDir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Apply exclude patterns first
		for _, pattern := range o.cfg.Exclude {
			if matchesGlob(pattern, rel) {
				return nil
			}
		}

		// Apply include patterns
		for _, pattern := range o.cfg.Include {
			if matchesGlob(pattern, rel) {
				files = append(files, rel)
				return nil
			}
		}
		return nil
	})

	return files, err
}

// IndexAll indexes all matching files in the configured root directory.
// Files whose content hash matches the stored record are skipped.
func (o *Orchestrator) IndexAll(ctx context.Context) (*IndexResult, error) {
	start := time.Now()
	result := &IndexResult{}

	rootDir := o.cfg.RootDir
	files, err := o.ScanFiles(ctx, rootDir)
	if err != nil {
		return result, err
	}

	type work struct {
		path string
	}
	type outcome struct {
		filePath string
		res      *types.ExtractionResult
		rec      *types.FileRecord
		skipped  bool
	}

	workCh := make(chan work, len(files))
	outCh := make(chan outcome, len(files))

	for _, f := range files {
		workCh <- work{path: f}
	}
	close(workCh)

	var wg sync.WaitGroup
	for i := 0; i < o.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workCh {
				if ctx.Err() != nil {
					return
				}
				absPath := filepath.Join(rootDir, w.path)
				out := o.processFile(ctx, absPath, w.path)
				outCh <- out
			}
		}()
	}

	go func() {
		wg.Wait()
		close(outCh)
	}()

	for out := range outCh {
		if out.skipped {
			result.FilesSkipped++
			continue
		}
		result.FilesProcessed++
		if out.rec != nil && o.db != nil {
			if uErr := o.db.UpsertFile(ctx, out.rec); uErr != nil {
				result.Errors = append(result.Errors, types.ExtractionError{
					Message:  uErr.Error(),
					FilePath: out.filePath,
					Severity: types.SeverityError,
				})
			}
		}
		if out.res != nil {
			result.Errors = append(result.Errors, out.res.Errors...)
			result.NodesExtracted += len(out.res.Nodes)
			result.EdgesExtracted += len(out.res.Edges)
			if o.db != nil {
				if delErr := o.db.DeleteNodesForFile(ctx, out.filePath); delErr != nil {
					result.Errors = append(result.Errors, types.ExtractionError{
						Message:  delErr.Error(),
						FilePath: out.filePath,
						Severity: types.SeverityError,
					})
				} else {
					for _, n := range out.res.Nodes {
						if uErr := o.db.UpsertNode(ctx, n); uErr != nil {
							result.Errors = append(result.Errors, types.ExtractionError{
								Message:  uErr.Error(),
								FilePath: out.filePath,
								Severity: types.SeverityWarning,
							})
						}
					}
					for _, e := range out.res.Edges {
						if uErr := o.db.UpsertEdge(ctx, e); uErr != nil {
							result.Errors = append(result.Errors, types.ExtractionError{
								Message:  uErr.Error(),
								FilePath: out.filePath,
								Severity: types.SeverityWarning,
							})
						}
					}
					for _, ref := range out.res.UnresolvedReferences {
						if uErr := o.db.UpsertUnresolvedRef(ctx, ref); uErr != nil {
							result.Errors = append(result.Errors, types.ExtractionError{
								Message:  uErr.Error(),
								FilePath: out.filePath,
								Severity: types.SeverityWarning,
							})
						}
					}
				}
			}
		}
	}

	result.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return result, nil
}

// processFile reads, hashes, and extracts from a single file.
func (o *Orchestrator) processFile(ctx context.Context, absPath, relPath string) struct {
	filePath string
	res      *types.ExtractionResult
	rec      *types.FileRecord
	skipped  bool
} {
	type result struct {
		filePath string
		res      *types.ExtractionResult
		rec      *types.FileRecord
		skipped  bool
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return result{filePath: relPath, res: &types.ExtractionResult{
			Errors: []types.ExtractionError{{
				Message: err.Error(), FilePath: relPath, Severity: types.SeverityError,
			}},
		}}
	}

	if o.cfg.MaxFileSize > 0 && int64(len(data)) > o.cfg.MaxFileSize {
		return result{filePath: relPath, skipped: true}
	}

	hash := HashContent(data)
	lang := DetectLanguage(relPath, data)

	// Change detection
	if o.db != nil {
		if existing, gErr := o.db.GetFile(ctx, relPath); gErr == nil && existing != nil {
			if existing.ContentHash == hash {
				return result{filePath: relPath, skipped: true}
			}
		}
	}

	info, statErr := os.Stat(absPath)
	var modTime int64
	if statErr == nil {
		modTime = info.ModTime().UnixMilli()
	}

	extractResult := ExtractFromSource(relPath, data, lang)

	rec := &types.FileRecord{
		Path:        relPath,
		ContentHash: hash,
		Language:    lang,
		Size:        int64(len(data)),
		ModifiedAt:  modTime,
		IndexedAt:   time.Now().UnixMilli(),
		NodeCount:   len(extractResult.Nodes),
	}

	return result{filePath: relPath, res: extractResult, rec: rec}
}

// matchesGlob returns true when relPath matches the glob pattern.
// Handles `**/` prefix patterns common in .gitignore-style configs.
func matchesGlob(pattern, relPath string) bool {
	pattern = filepath.ToSlash(pattern)

	// Exact match
	if pattern == relPath {
		return true
	}

	// No double-star: use standard glob
	if !strings.Contains(pattern, "**") {
		m, _ := filepath.Match(pattern, relPath)
		return m
	}

	// Split on ** and check each segment
	parts := strings.Split(pattern, "**/")

	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]

		// Pattern like "**/foo/**": check if path contains /foo/
		if strings.HasSuffix(suffix, "/**") {
			dir := strings.TrimSuffix(suffix, "/**")
			segment := prefix + dir
			return pathContainsSegment(relPath, segment)
		}

		// Pattern like "**/*.ts": check extension/filename
		if prefix == "" {
			if m, _ := filepath.Match(suffix, filepath.Base(relPath)); m {
				return true
			}
			// Also match against sub-paths
			pathParts := strings.Split(relPath, "/")
			for i := range pathParts {
				sub := strings.Join(pathParts[i:], "/")
				if m, _ := filepath.Match(suffix, sub); m {
					return true
				}
			}
			return false
		}

		// Pattern like "prefix/**/*.ts"
		if !strings.HasPrefix(relPath, prefix) {
			return false
		}
		rest := relPath[len(prefix):]
		m, _ := filepath.Match(suffix, filepath.Base(rest))
		return m
	}

	// Generic: recursive double-star match
	return matchDoublestar(pattern, relPath)
}

func pathContainsSegment(path, segment string) bool {
	if segment == "" {
		return true
	}
	return strings.Contains("/"+path+"/", "/"+segment+"/") ||
		strings.HasPrefix(path+"/", segment+"/") ||
		strings.HasPrefix("/"+path, "/"+segment)
}

// matchDoublestar is a recursive matcher for ** patterns.
func matchDoublestar(pattern, path string) bool {
	if pattern == "" {
		return path == ""
	}
	if pattern == "**" {
		return true
	}

	pParts := strings.SplitN(pattern, "/", 2)
	pathParts := strings.SplitN(path, "/", 2)

	pHead := pParts[0]
	pTail := ""
	if len(pParts) == 2 {
		pTail = pParts[1]
	}

	if pHead == "**" {
		if matchDoublestar(pTail, path) {
			return true
		}
		if len(pathParts) == 2 {
			return matchDoublestar(pattern, pathParts[1])
		}
		return false
	}

	if path == "" {
		return false
	}

	pathHead := pathParts[0]
	pathTail := ""
	if len(pathParts) == 2 {
		pathTail = pathParts[1]
	}

	m, _ := filepath.Match(pHead, pathHead)
	if !m {
		return false
	}

	if pTail == "" && pathTail == "" {
		return true
	}
	return matchDoublestar(pTail, pathTail)
}
