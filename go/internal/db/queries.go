package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kristofer/codegraph/internal/types"
)

// Queries provides all prepared-statement CRUD operations on the knowledge
// graph database.  It mirrors the TypeScript QueryBuilder class.
type Queries struct {
	db *DB
}

// NewQueries returns a Queries instance backed by the given DB.
func NewQueries(db *DB) *Queries {
	return &Queries{db: db}
}

// ===========================================================================
// Node helpers
// ===========================================================================

// scanNode reads a node row from a *sql.Rows cursor.
func scanNode(rows *sql.Rows) (*types.Node, error) {
	var (
		n                                   types.Node
		docstring, signature, visibility    sql.NullString
		decoratorsJSON, typeParamsJSON      sql.NullString
		isExported, isAsync, isStatic, isAbstract int
	)
	err := rows.Scan(
		&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
		&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn,
		&docstring, &signature, &visibility,
		&isExported, &isAsync, &isStatic, &isAbstract,
		&decoratorsJSON, &typeParamsJSON,
		&n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if docstring.Valid {
		n.Docstring = &docstring.String
	}
	if signature.Valid {
		n.Signature = &signature.String
	}
	if visibility.Valid {
		v := types.Visibility(visibility.String)
		n.Visibility = &v
	}
	n.IsExported = isExported == 1
	n.IsAsync = isAsync == 1
	n.IsStatic = isStatic == 1
	n.IsAbstract = isAbstract == 1
	if decoratorsJSON.Valid && decoratorsJSON.String != "" {
		_ = json.Unmarshal([]byte(decoratorsJSON.String), &n.Decorators)
	}
	if typeParamsJSON.Valid && typeParamsJSON.String != "" {
		_ = json.Unmarshal([]byte(typeParamsJSON.String), &n.TypeParameters)
	}
	return &n, nil
}

// nodeColumns is the canonical SELECT column list for the nodes table.
const nodeColumns = `id, kind, name, qualified_name, file_path, language,
	start_line, end_line, start_column, end_column,
	docstring, signature, visibility,
	is_exported, is_async, is_static, is_abstract,
	decorators, type_parameters,
	updated_at`

// nodeArgs builds the INSERT/UPDATE parameter slice from a Node.
func nodeArgs(n *types.Node) []any {
	var decoratorsJSON, typeParamsJSON any
	if len(n.Decorators) > 0 {
		b, _ := json.Marshal(n.Decorators)
		decoratorsJSON = string(b)
	}
	if len(n.TypeParameters) > 0 {
		b, _ := json.Marshal(n.TypeParameters)
		typeParamsJSON = string(b)
	}

	var vis any
	if n.Visibility != nil {
		vis = string(*n.Visibility)
	}

	isExported, isAsync, isStatic, isAbstract := 0, 0, 0, 0
	if n.IsExported {
		isExported = 1
	}
	if n.IsAsync {
		isAsync = 1
	}
	if n.IsStatic {
		isStatic = 1
	}
	if n.IsAbstract {
		isAbstract = 1
	}

	updatedAt := n.UpdatedAt
	if updatedAt == 0 {
		updatedAt = time.Now().UnixMilli()
	}

	return []any{
		n.ID, string(n.Kind), n.Name, n.QualifiedName, n.FilePath, string(n.Language),
		n.StartLine, n.EndLine, n.StartColumn, n.EndColumn,
		nullableString(n.Docstring), nullableString(n.Signature), vis,
		isExported, isAsync, isStatic, isAbstract,
		decoratorsJSON, typeParamsJSON,
		updatedAt,
	}
}

// nullableString converts *string to a value accepted by database/sql.
func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// ===========================================================================
// Node Operations
// ===========================================================================

const upsertNodeSQL = `
INSERT INTO nodes (` + nodeColumns + `)
VALUES (?,?,?,?,?,?, ?,?,?,?, ?,?,?, ?,?,?,?, ?,?,?)
ON CONFLICT(id) DO UPDATE SET
	kind=excluded.kind,
	name=excluded.name,
	qualified_name=excluded.qualified_name,
	file_path=excluded.file_path,
	language=excluded.language,
	start_line=excluded.start_line,
	end_line=excluded.end_line,
	start_column=excluded.start_column,
	end_column=excluded.end_column,
	docstring=excluded.docstring,
	signature=excluded.signature,
	visibility=excluded.visibility,
	is_exported=excluded.is_exported,
	is_async=excluded.is_async,
	is_static=excluded.is_static,
	is_abstract=excluded.is_abstract,
	decorators=excluded.decorators,
	type_parameters=excluded.type_parameters,
	updated_at=excluded.updated_at
`

// UpsertNode inserts or updates a single node.
func (q *Queries) UpsertNode(node *types.Node) error {
	if node.ID == "" || string(node.Kind) == "" || node.Name == "" ||
		node.FilePath == "" || string(node.Language) == "" {
		return fmt.Errorf("db: UpsertNode: missing required fields (id=%q kind=%q name=%q)", node.ID, node.Kind, node.Name)
	}
	_, err := q.db.sqlDB.Exec(upsertNodeSQL, nodeArgs(node)...)
	if err != nil {
		return fmt.Errorf("db: UpsertNode %q: %w", node.ID, err)
	}
	return nil
}

// UpsertNodes inserts or updates multiple nodes inside a single transaction.
func (q *Queries) UpsertNodes(nodes []*types.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	return q.db.WithTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(upsertNodeSQL)
		if err != nil {
			return fmt.Errorf("db: UpsertNodes prepare: %w", err)
		}
		defer stmt.Close()
		for _, n := range nodes {
			if n.ID == "" || string(n.Kind) == "" || n.Name == "" ||
				n.FilePath == "" || string(n.Language) == "" {
				continue // skip invalid nodes, matching TypeScript behaviour
			}
			if _, err := stmt.Exec(nodeArgs(n)...); err != nil {
				return fmt.Errorf("db: UpsertNodes exec %q: %w", n.ID, err)
			}
		}
		return nil
	})
}

// GetNodeByID returns the node with the given ID, or (nil, nil) if not found.
func (q *Queries) GetNodeByID(id string) (*types.Node, error) {
	rows, err := q.db.sqlDB.Query(
		"SELECT "+nodeColumns+" FROM nodes WHERE id = ?", id,
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetNodeByID: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanNode(rows)
}

// GetNodesByFile returns all nodes in the given file, ordered by start_line.
func (q *Queries) GetNodesByFile(filePath string) ([]*types.Node, error) {
	rows, err := q.db.sqlDB.Query(
		"SELECT "+nodeColumns+" FROM nodes WHERE file_path = ? ORDER BY start_line", filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetNodesByFile: %w", err)
	}
	return collectNodes(rows)
}

// GetAllNodes returns every node in the database.
func (q *Queries) GetAllNodes() ([]*types.Node, error) {
	rows, err := q.db.sqlDB.Query("SELECT " + nodeColumns + " FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("db: GetAllNodes: %w", err)
	}
	return collectNodes(rows)
}

// DeleteNode deletes a node by ID.
func (q *Queries) DeleteNode(id string) error {
	_, err := q.db.sqlDB.Exec("DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("db: DeleteNode %q: %w", id, err)
	}
	return nil
}

func collectNodes(rows *sql.Rows) ([]*types.Node, error) {
	defer rows.Close()
	var nodes []*types.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("db: scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// ===========================================================================
// Search
// ===========================================================================

// SearchNodes searches nodes using FTS5 with a LIKE fallback.
// It mirrors the TypeScript searchNodes / searchNodesFTS / searchNodesLike logic.
func (q *Queries) SearchNodes(query string, opts types.SearchOptions) ([]*types.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := opts.Offset

	results, err := q.searchNodesFTS(query, opts)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 && len(query) >= 2 {
		results, err = q.searchNodesLike(query, opts)
		if err != nil {
			return nil, err
		}
	}

	// Sort and trim.
	sortSearchResults(results)
	if len(results) > limit+offset {
		results = results[offset : offset+limit]
	} else if offset < len(results) {
		results = results[offset:]
	} else {
		results = nil
	}

	return results, nil
}

func (q *Queries) searchNodesFTS(query string, opts types.SearchOptions) ([]*types.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	ftsLimit := limit * 5
	if ftsLimit < 100 {
		ftsLimit = 100
	}

	// Build FTS query: strip special chars, add prefix wildcard per term.
	clean := strings.NewReplacer(`"`, ``, `'`, ``, `*`, ``, `(`, ``, `)`, ``, `:`, ``, `^`, ``).Replace(query)
	var ftsTerms []string
	for _, term := range strings.Fields(clean) {
		upper := strings.ToUpper(term)
		if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			continue
		}
		ftsTerms = append(ftsTerms, `"`+term+`"*`)
	}
	if len(ftsTerms) == 0 {
		return nil, nil
	}
	ftsQuery := strings.Join(ftsTerms, " OR ")

	sqlStr := `
		SELECT ` + nodeColumns + `, bm25(nodes_fts, 0, 20, 5, 1, 2) as _score
		FROM nodes_fts
		JOIN nodes ON nodes_fts.id = nodes.id
		WHERE nodes_fts MATCH ?`
	args := []any{ftsQuery}

	sqlStr, args = appendKindFilter(sqlStr, args, "nodes", opts.Kinds)
	sqlStr, args = appendLanguageFilter(sqlStr, args, "nodes", opts.Languages)

	sqlStr += " ORDER BY _score LIMIT ?"
	args = append(args, ftsLimit)

	rows, err := q.db.sqlDB.Query(sqlStr, args...)
	if err != nil {
		// FTS query parse error — silently return empty.
		return nil, nil //nolint:nilerr
	}
	defer rows.Close()

	var results []*types.SearchResult
	for rows.Next() {
		var (
			n                                                   types.Node
			docstring, signature, visibility                    sql.NullString
			decoratorsJSON, typeParamsJSON                      sql.NullString
			isExported, isAsync, isStatic, isAbstract          int
			score                                               float64
		)
		err := rows.Scan(
			&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
			&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn,
			&docstring, &signature, &visibility,
			&isExported, &isAsync, &isStatic, &isAbstract,
			&decoratorsJSON, &typeParamsJSON,
			&n.UpdatedAt,
			&score,
		)
		if err != nil {
			return nil, fmt.Errorf("db: FTS scan: %w", err)
		}
		if docstring.Valid {
			n.Docstring = &docstring.String
		}
		if signature.Valid {
			n.Signature = &signature.String
		}
		if visibility.Valid {
			v := types.Visibility(visibility.String)
			n.Visibility = &v
		}
		n.IsExported = isExported == 1
		n.IsAsync = isAsync == 1
		n.IsStatic = isStatic == 1
		n.IsAbstract = isAbstract == 1
		if decoratorsJSON.Valid && decoratorsJSON.String != "" {
			_ = json.Unmarshal([]byte(decoratorsJSON.String), &n.Decorators)
		}
		if typeParamsJSON.Valid && typeParamsJSON.String != "" {
			_ = json.Unmarshal([]byte(typeParamsJSON.String), &n.TypeParameters)
		}
		// bm25 returns negative values; negate to get a positive score.
		if score < 0 {
			score = -score
		}
		node := n
		results = append(results, &types.SearchResult{Node: &node, Score: score})
	}
	return results, rows.Err()
}

func (q *Queries) searchNodesLike(query string, opts types.SearchOptions) ([]*types.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	contains := "%" + query + "%"
	startsWith := query + "%"

	sqlStr := `
		SELECT ` + nodeColumns + `,
			CASE
				WHEN name = ?              THEN 1.0
				WHEN name LIKE ?           THEN 0.9
				WHEN name LIKE ?           THEN 0.8
				WHEN qualified_name LIKE ? THEN 0.7
				ELSE 0.5
			END AS _score
		FROM nodes
		WHERE (name LIKE ? OR qualified_name LIKE ? OR name LIKE ?)`
	args := []any{query, startsWith, contains, contains, contains, contains, startsWith}

	sqlStr, args = appendKindFilter(sqlStr, args, "", opts.Kinds)
	sqlStr, args = appendLanguageFilter(sqlStr, args, "", opts.Languages)

	sqlStr += " ORDER BY _score DESC, length(name) ASC LIMIT ?"
	args = append(args, limit)

	rows, err := q.db.sqlDB.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("db: searchNodesLike: %w", err)
	}
	defer rows.Close()

	var results []*types.SearchResult
	for rows.Next() {
		var (
			n                                          types.Node
			docstring, signature, visibility           sql.NullString
			decoratorsJSON, typeParamsJSON             sql.NullString
			isExported, isAsync, isStatic, isAbstract int
			score                                      float64
		)
		err := rows.Scan(
			&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath, &n.Language,
			&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn,
			&docstring, &signature, &visibility,
			&isExported, &isAsync, &isStatic, &isAbstract,
			&decoratorsJSON, &typeParamsJSON,
			&n.UpdatedAt,
			&score,
		)
		if err != nil {
			return nil, fmt.Errorf("db: LIKE scan: %w", err)
		}
		if docstring.Valid {
			n.Docstring = &docstring.String
		}
		if signature.Valid {
			n.Signature = &signature.String
		}
		if visibility.Valid {
			v := types.Visibility(visibility.String)
			n.Visibility = &v
		}
		n.IsExported = isExported == 1
		n.IsAsync = isAsync == 1
		n.IsStatic = isStatic == 1
		n.IsAbstract = isAbstract == 1
		if decoratorsJSON.Valid && decoratorsJSON.String != "" {
			_ = json.Unmarshal([]byte(decoratorsJSON.String), &n.Decorators)
		}
		if typeParamsJSON.Valid && typeParamsJSON.String != "" {
			_ = json.Unmarshal([]byte(typeParamsJSON.String), &n.TypeParameters)
		}
		node := n
		results = append(results, &types.SearchResult{Node: &node, Score: score})
	}
	return results, rows.Err()
}

// sortSearchResults sorts results by descending score (in-place).
func sortSearchResults(results []*types.SearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

// appendKindFilter appends an IN clause for node kinds if kinds is non-empty.
// tablePrefix may be "nodes" (when joining) or "" (plain nodes query).
func appendKindFilter(sqlStr string, args []any, tablePrefix string, kinds []types.NodeKind) (string, []any) {
	if len(kinds) == 0 {
		return sqlStr, args
	}
	col := "kind"
	if tablePrefix != "" {
		col = tablePrefix + ".kind"
	}
	placeholders := strings.Repeat("?,", len(kinds))
	placeholders = placeholders[:len(placeholders)-1]
	sqlStr += " AND " + col + " IN (" + placeholders + ")"
	for _, k := range kinds {
		args = append(args, string(k))
	}
	return sqlStr, args
}

// appendLanguageFilter appends an IN clause for languages if langs is non-empty.
func appendLanguageFilter(sqlStr string, args []any, tablePrefix string, langs []types.Language) (string, []any) {
	if len(langs) == 0 {
		return sqlStr, args
	}
	col := "language"
	if tablePrefix != "" {
		col = tablePrefix + ".language"
	}
	placeholders := strings.Repeat("?,", len(langs))
	placeholders = placeholders[:len(placeholders)-1]
	sqlStr += " AND " + col + " IN (" + placeholders + ")"
	for _, l := range langs {
		args = append(args, string(l))
	}
	return sqlStr, args
}

// ===========================================================================
// Edge Operations
// ===========================================================================

// scanEdge reads an edge row from *sql.Rows.
func scanEdge(rows *sql.Rows) (*types.Edge, error) {
	var (
		e                        types.Edge
		metadataJSON, provenance sql.NullString
		line, col                sql.NullInt64
	)
	err := rows.Scan(
		&e.Source, &e.Target, &e.Kind,
		&metadataJSON, &line, &col, &provenance,
	)
	if err != nil {
		return nil, err
	}
	if metadataJSON.Valid && metadataJSON.String != "" {
		_ = json.Unmarshal([]byte(metadataJSON.String), &e.Metadata)
	}
	if line.Valid {
		v := int(line.Int64)
		e.Line = &v
	}
	if col.Valid {
		v := int(col.Int64)
		e.Column = &v
	}
	if provenance.Valid {
		p := types.Provenance(provenance.String)
		e.Provenance = &p
	}
	return &e, nil
}

const edgeColumns = "source, target, kind, metadata, line, col, provenance"

// InsertEdge inserts a single edge (duplicate silently ignored via OR IGNORE).
func (q *Queries) InsertEdge(e *types.Edge) error {
	var metaJSON any
	if len(e.Metadata) > 0 {
		b, _ := json.Marshal(e.Metadata)
		metaJSON = string(b)
	}
	var prov any
	if e.Provenance != nil {
		prov = string(*e.Provenance)
	}
	_, err := q.db.sqlDB.Exec(
		`INSERT OR IGNORE INTO edges (`+edgeColumns+`) VALUES (?,?,?,?,?,?,?)`,
		e.Source, e.Target, string(e.Kind), metaJSON, e.Line, e.Column, prov,
	)
	if err != nil {
		return fmt.Errorf("db: InsertEdge: %w", err)
	}
	return nil
}

// InsertEdgeBatch inserts multiple edges inside a single transaction.
func (q *Queries) InsertEdgeBatch(edges []*types.Edge) error {
	if len(edges) == 0 {
		return nil
	}
	return q.db.WithTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(
			`INSERT OR IGNORE INTO edges (` + edgeColumns + `) VALUES (?,?,?,?,?,?,?)`,
		)
		if err != nil {
			return fmt.Errorf("db: InsertEdgeBatch prepare: %w", err)
		}
		defer stmt.Close()
		for _, e := range edges {
			var metaJSON any
			if len(e.Metadata) > 0 {
				b, _ := json.Marshal(e.Metadata)
				metaJSON = string(b)
			}
			var prov any
			if e.Provenance != nil {
				prov = string(*e.Provenance)
			}
			if _, err := stmt.Exec(e.Source, e.Target, string(e.Kind), metaJSON, e.Line, e.Column, prov); err != nil {
				return fmt.Errorf("db: InsertEdgeBatch exec: %w", err)
			}
		}
		return nil
	})
}

// GetEdges returns edges connected to nodeID.
// direction controls whether outgoing (source), incoming (target), or both are
// returned.  An optional set of kinds filters by edge type.
func (q *Queries) GetEdges(nodeID string, direction types.EdgeDirection, kinds ...types.EdgeKind) ([]*types.Edge, error) {
	var (
		sqlStr string
		args   []any
	)
	switch direction {
	case types.EdgeDirectionOutgoing:
		sqlStr = "SELECT " + edgeColumns + " FROM edges WHERE source = ?"
		args = []any{nodeID}
	case types.EdgeDirectionIncoming:
		sqlStr = "SELECT " + edgeColumns + " FROM edges WHERE target = ?"
		args = []any{nodeID}
	default: // both
		sqlStr = "SELECT " + edgeColumns + " FROM edges WHERE source = ? OR target = ?"
		args = []any{nodeID, nodeID}
	}

	if len(kinds) > 0 {
		placeholders := strings.Repeat("?,", len(kinds))
		placeholders = placeholders[:len(placeholders)-1]
		sqlStr += " AND kind IN (" + placeholders + ")"
		for _, k := range kinds {
			args = append(args, string(k))
		}
	}

	rows, err := q.db.sqlDB.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("db: GetEdges: %w", err)
	}
	defer rows.Close()

	var edges []*types.Edge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, fmt.Errorf("db: GetEdges scan: %w", err)
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// ===========================================================================
// File Operations
// ===========================================================================

// UpsertFileRecord inserts or updates a file record.
func (q *Queries) UpsertFileRecord(f *types.FileRecord) error {
	var errJSON any
	if len(f.Errors) > 0 {
		b, _ := json.Marshal(f.Errors)
		errJSON = string(b)
	}
	_, err := q.db.sqlDB.Exec(`
		INSERT INTO files (path, content_hash, language, size, modified_at, indexed_at, node_count, errors)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET
			content_hash=excluded.content_hash,
			language=excluded.language,
			size=excluded.size,
			modified_at=excluded.modified_at,
			indexed_at=excluded.indexed_at,
			node_count=excluded.node_count,
			errors=excluded.errors`,
		f.Path, f.ContentHash, string(f.Language), f.Size,
		f.ModifiedAt, f.IndexedAt, f.NodeCount, errJSON,
	)
	if err != nil {
		return fmt.Errorf("db: UpsertFileRecord %q: %w", f.Path, err)
	}
	return nil
}

// GetFileByPath returns the file record for the given path, or (nil, nil) if not found.
func (q *Queries) GetFileByPath(filePath string) (*types.FileRecord, error) {
	rows, err := q.db.sqlDB.Query(
		"SELECT path, content_hash, language, size, modified_at, indexed_at, node_count, errors FROM files WHERE path = ?",
		filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetFileByPath: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanFile(rows)
}

// GetAllFiles returns every tracked file record.
func (q *Queries) GetAllFiles() ([]*types.FileRecord, error) {
	rows, err := q.db.sqlDB.Query(
		"SELECT path, content_hash, language, size, modified_at, indexed_at, node_count, errors FROM files ORDER BY path",
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetAllFiles: %w", err)
	}
	defer rows.Close()

	var files []*types.FileRecord
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, fmt.Errorf("db: GetAllFiles scan: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetAllFilePaths returns a lightweight list of tracked file paths.
func (q *Queries) GetAllFilePaths() ([]string, error) {
	rows, err := q.db.sqlDB.Query("SELECT path FROM files ORDER BY path")
	if err != nil {
		return nil, fmt.Errorf("db: GetAllFilePaths: %w", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteFile removes a file record and all its nodes (cascade).
// Foreign-key cascades delete the associated edges and unresolved refs.
func (q *Queries) DeleteFile(filePath string) error {
	return q.db.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM nodes WHERE file_path = ?", filePath); err != nil {
			return fmt.Errorf("db: DeleteFile nodes: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM files WHERE path = ?", filePath); err != nil {
			return fmt.Errorf("db: DeleteFile record: %w", err)
		}
		return nil
	})
}

func scanFile(rows *sql.Rows) (*types.FileRecord, error) {
	var (
		f        types.FileRecord
		errJSON  sql.NullString
		language string
	)
	err := rows.Scan(
		&f.Path, &f.ContentHash, &language, &f.Size,
		&f.ModifiedAt, &f.IndexedAt, &f.NodeCount, &errJSON,
	)
	if err != nil {
		return nil, err
	}
	f.Language = types.Language(language)
	if errJSON.Valid && errJSON.String != "" {
		_ = json.Unmarshal([]byte(errJSON.String), &f.Errors)
	}
	return &f, nil
}

// ===========================================================================
// Unresolved References
// ===========================================================================

// InsertUnresolvedRef stores a reference that could not be resolved during extraction.
func (q *Queries) InsertUnresolvedRef(ref *types.UnresolvedReference) error {
	var candidatesJSON any
	if len(ref.Candidates) > 0 {
		b, _ := json.Marshal(ref.Candidates)
		candidatesJSON = string(b)
	}
	filePath := ref.FilePath
	language := string(ref.Language)
	if language == "" {
		language = string(types.Unknown)
	}
	_, err := q.db.sqlDB.Exec(`
		INSERT INTO unresolved_refs (from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language)
		VALUES (?,?,?,?,?,?,?,?)`,
		ref.FromNodeID, ref.ReferenceName, string(ref.ReferenceKind),
		ref.Line, ref.Column, candidatesJSON, filePath, language,
	)
	if err != nil {
		return fmt.Errorf("db: InsertUnresolvedRef: %w", err)
	}
	return nil
}

// InsertUnresolvedRefsBatch inserts multiple unresolved references in a single transaction.
func (q *Queries) InsertUnresolvedRefsBatch(refs []*types.UnresolvedReference) error {
	if len(refs) == 0 {
		return nil
	}
	return q.db.WithTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`
			INSERT INTO unresolved_refs (from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language)
			VALUES (?,?,?,?,?,?,?,?)`)
		if err != nil {
			return fmt.Errorf("db: InsertUnresolvedRefsBatch prepare: %w", err)
		}
		defer stmt.Close()
		for _, ref := range refs {
			var candidatesJSON any
			if len(ref.Candidates) > 0 {
				b, _ := json.Marshal(ref.Candidates)
				candidatesJSON = string(b)
			}
			language := string(ref.Language)
			if language == "" {
				language = string(types.Unknown)
			}
			if _, err := stmt.Exec(
				ref.FromNodeID, ref.ReferenceName, string(ref.ReferenceKind),
				ref.Line, ref.Column, candidatesJSON, ref.FilePath, language,
			); err != nil {
				return fmt.Errorf("db: InsertUnresolvedRefsBatch exec: %w", err)
			}
		}
		return nil
	})
}

// GetAllUnresolvedRefs returns all unresolved references.
func (q *Queries) GetAllUnresolvedRefs() ([]*types.UnresolvedReference, error) {
	rows, err := q.db.sqlDB.Query(`
		SELECT from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language
		FROM unresolved_refs`)
	if err != nil {
		return nil, fmt.Errorf("db: GetAllUnresolvedRefs: %w", err)
	}
	return scanUnresolvedRefs(rows)
}

// GetUnresolvedRefsBatch returns a paginated slice of unresolved references.
func (q *Queries) GetUnresolvedRefsBatch(offset, limit int) ([]*types.UnresolvedReference, error) {
	rows, err := q.db.sqlDB.Query(`
		SELECT from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language
		FROM unresolved_refs LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("db: GetUnresolvedRefsBatch: %w", err)
	}
	return scanUnresolvedRefs(rows)
}

// GetUnresolvedRefsCount returns the total count without loading all rows.
func (q *Queries) GetUnresolvedRefsCount() (int, error) {
	var count int
	row := q.db.sqlDB.QueryRow("SELECT COUNT(*) FROM unresolved_refs")
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("db: GetUnresolvedRefsCount: %w", err)
	}
	return count, nil
}

// ClearUnresolvedRefs deletes all unresolved references (called after resolution).
func (q *Queries) ClearUnresolvedRefs() error {
	_, err := q.db.sqlDB.Exec("DELETE FROM unresolved_refs")
	if err != nil {
		return fmt.Errorf("db: ClearUnresolvedRefs: %w", err)
	}
	return nil
}

func scanUnresolvedRefs(rows *sql.Rows) ([]*types.UnresolvedReference, error) {
	defer rows.Close()
	var refs []*types.UnresolvedReference
	for rows.Next() {
		var (
			r              types.UnresolvedReference
			candidatesJSON sql.NullString
			refKind        string
			lang           string
		)
		err := rows.Scan(
			&r.FromNodeID, &r.ReferenceName, &refKind,
			&r.Line, &r.Column, &candidatesJSON, &r.FilePath, &lang,
		)
		if err != nil {
			return nil, fmt.Errorf("db: scan unresolved ref: %w", err)
		}
		r.ReferenceKind = types.EdgeKind(refKind)
		r.Language = types.Language(lang)
		if candidatesJSON.Valid && candidatesJSON.String != "" {
			_ = json.Unmarshal([]byte(candidatesJSON.String), &r.Candidates)
		}
		refs = append(refs, &r)
	}
	return refs, rows.Err()
}

// ===========================================================================
// Statistics
// ===========================================================================

// GetStats returns aggregate statistics about the knowledge graph.
func (q *Queries) GetStats() (*types.GraphStats, error) {
	stats := &types.GraphStats{
		NodesByKind:     make(map[types.NodeKind]int),
		EdgesByKind:     make(map[types.EdgeKind]int),
		FilesByLanguage: make(map[types.Language]int),
		LastUpdated:     time.Now().UnixMilli(),
		DBSizeBytes:     q.db.Size(),
	}

	row := q.db.sqlDB.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM nodes) AS node_count,
			(SELECT COUNT(*) FROM edges) AS edge_count,
			(SELECT COUNT(*) FROM files) AS file_count`)
	if err := row.Scan(&stats.NodeCount, &stats.EdgeCount, &stats.FileCount); err != nil {
		return nil, fmt.Errorf("db: GetStats counts: %w", err)
	}

	kindRows, err := q.db.sqlDB.Query("SELECT kind, COUNT(*) FROM nodes GROUP BY kind")
	if err != nil {
		return nil, fmt.Errorf("db: GetStats nodesByKind: %w", err)
	}
	defer kindRows.Close()
	for kindRows.Next() {
		var kind string
		var count int
		if err := kindRows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		stats.NodesByKind[types.NodeKind(kind)] = count
	}
	if err := kindRows.Err(); err != nil {
		return nil, err
	}

	edgeRows, err := q.db.sqlDB.Query("SELECT kind, COUNT(*) FROM edges GROUP BY kind")
	if err != nil {
		return nil, fmt.Errorf("db: GetStats edgesByKind: %w", err)
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var kind string
		var count int
		if err := edgeRows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		stats.EdgesByKind[types.EdgeKind(kind)] = count
	}
	if err := edgeRows.Err(); err != nil {
		return nil, err
	}

	langRows, err := q.db.sqlDB.Query("SELECT language, COUNT(*) FROM files GROUP BY language")
	if err != nil {
		return nil, fmt.Errorf("db: GetStats filesByLanguage: %w", err)
	}
	defer langRows.Close()
	for langRows.Next() {
		var lang string
		var count int
		if err := langRows.Scan(&lang, &count); err != nil {
			return nil, err
		}
		stats.FilesByLanguage[types.Language(lang)] = count
	}
	return stats, langRows.Err()
}

// ===========================================================================
// Project Metadata
// ===========================================================================

// GetMetadata returns the value for the given key, or "" if not set.
func (q *Queries) GetMetadata(key string) (string, error) {
	var value string
	row := q.db.sqlDB.QueryRow("SELECT value FROM project_metadata WHERE key = ?", key)
	if err := row.Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("db: GetMetadata %q: %w", key, err)
	}
	return value, nil
}

// SetMetadata upserts a key-value pair in project_metadata.
func (q *Queries) SetMetadata(key, value string) error {
	_, err := q.db.sqlDB.Exec(`
		INSERT INTO project_metadata (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("db: SetMetadata %q: %w", key, err)
	}
	return nil
}

// ===========================================================================
// Clear / Reset
// ===========================================================================

// Clear deletes all nodes, edges, files, and unresolved references.
func (q *Queries) Clear() error {
	return q.db.WithTx(func(tx *sql.Tx) error {
		for _, table := range []string{"unresolved_refs", "edges", "nodes", "files"} {
			if _, err := tx.Exec("DELETE FROM " + table); err != nil {
				return fmt.Errorf("db: Clear %s: %w", table, err)
			}
		}
		return nil
	})
}
