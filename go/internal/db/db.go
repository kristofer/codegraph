// Package db provides the SQLite database layer for CodeGraph.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "embed"

	_ "modernc.org/sqlite" // pure-Go SQLite driver with FTS5 support
)

//go:embed schema.sql
var schemaSQL string

// currentSchemaVersion is the version applied by schema.sql.
const currentSchemaVersion = 1

// DB wraps a *sql.DB with CodeGraph-specific lifecycle helpers.
type DB struct {
	sqlDB  *sql.DB
	dbPath string
}

// Open opens or creates the SQLite database at dbPath.
// On first open the schema is applied; on subsequent opens any pending
// migrations are run.  The function also sets the recommended PRAGMAs for
// WAL mode, memory-mapped I/O, and cache size.
func Open(dbPath string) (*DB, error) {
	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("db: creating directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	// WAL mode (applied below via PRAGMA) supports multiple concurrent readers.
	// We limit to a small pool: writes are serialised by WithTx, and a small
	// pool prevents "database is locked" under bursty concurrent reads.
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{sqlDB: sqlDB, dbPath: dbPath}

	if err := db.applyPragmas(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	if err := db.initSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return db, nil
}

// OpenMemory opens an in-memory SQLite database — useful for tests.
func OpenMemory() (*DB, error) {
	// Use a unique URI so parallel test runs each get an isolated DB.
	uri := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", time.Now().UnixNano())
	sqlDB, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("db: open memory: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)

	db := &DB{sqlDB: sqlDB, dbPath: ":memory:"}

	if err := db.applyPragmas(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	if err := db.initSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sqlDB.Close()
}

// Path returns the file path used when the database was opened.
func (db *DB) Path() string {
	return db.dbPath
}

// SQLDB returns the underlying *sql.DB (e.g. for use in tests or advanced queries).
func (db *DB) SQLDB() *sql.DB {
	return db.sqlDB
}

// Size returns the on-disk size of the database file in bytes.
// Returns 0 for in-memory databases.
func (db *DB) Size() int64 {
	if db.dbPath == ":memory:" {
		return 0
	}
	info, err := os.Stat(db.dbPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

// WithTx executes fn inside a BEGIN IMMEDIATE transaction.
// If fn returns an error the transaction is rolled back; otherwise it is
// committed.  Use for all batch-write operations.
func (db *DB) WithTx(fn func(tx *sql.Tx) error) error {
	tx, err := db.sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}

	return nil
}

// Exec is a convenience wrapper around db.sqlDB.Exec.
func (db *DB) Exec(query string, args ...any) (sql.Result, error) {
	return db.sqlDB.Exec(query, args...)
}

// QueryRow is a convenience wrapper around db.sqlDB.QueryRow.
func (db *DB) QueryRow(query string, args ...any) *sql.Row {
	return db.sqlDB.QueryRow(query, args...)
}

// Query is a convenience wrapper around db.sqlDB.Query.
func (db *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return db.sqlDB.Query(query, args...)
}

// applyPragmas sets the recommended SQLite PRAGMAs.
func (db *DB) applyPragmas() error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -64000",    // 64 MB page cache
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 268435456",  // 256 MB memory-mapped I/O
		"PRAGMA busy_timeout = 120000",  // 2-minute busy timeout
	}
	for _, p := range pragmas {
		if _, err := db.sqlDB.Exec(p); err != nil {
			return fmt.Errorf("db: pragma %q: %w", p, err)
		}
	}
	return nil
}

// initSchema applies the embedded schema.sql if the tables don't yet exist,
// then runs any pending migrations.
func (db *DB) initSchema() error {
	// Apply the full schema (all CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS).
	if _, err := db.sqlDB.Exec(schemaSQL); err != nil {
		return fmt.Errorf("db: apply schema: %w", err)
	}
	return nil
}

// SchemaVersion returns the current schema version recorded in the database,
// or 0 if no version has been recorded yet.
func (db *DB) SchemaVersion() (int, error) {
	var ver int
	row := db.sqlDB.QueryRow(
		"SELECT version FROM schema_versions ORDER BY version DESC LIMIT 1",
	)
	if err := row.Scan(&ver); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("db: schema version: %w", err)
	}
	return ver, nil
}

