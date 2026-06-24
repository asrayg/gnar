// Package store is Gnar's persistence layer: a single SQLite database accessed
// through the pure-Go ncruces driver. The connection is not safe for concurrent
// use, so every operation is serialized behind a mutex — fine for a personal
// memory store and far simpler than a connection pool.
package store

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/ncruces/go-sqlite3"
)

const schemaVersion = 1

// Store wraps a SQLite connection.
type Store struct {
	mu   sync.Mutex
	conn *sqlite3.Conn
	path string
}

// Open opens (creating if needed) the database at path and runs migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	conn, err := sqlite3.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{conn: conn, path: path}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	if err := s.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Close()
}

func (s *Store) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			id         INTEGER PRIMARY KEY,
			project    TEXT NOT NULL,
			kind       TEXT NOT NULL,
			title      TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL,
			tags       TEXT NOT NULL DEFAULT '[]',
			files      TEXT NOT NULL DEFAULT '[]',
			source     TEXT NOT NULL DEFAULT '',
			session    TEXT NOT NULL DEFAULT '',
			meta       TEXT NOT NULL DEFAULT '{}',
			pinned     INTEGER NOT NULL DEFAULT 0,
			archived   INTEGER NOT NULL DEFAULT 0,
			embedding  BLOB,
			embed_id   TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mem_project ON memories(project, archived)`,
		`CREATE INDEX IF NOT EXISTS idx_mem_kind    ON memories(project, kind, archived)`,
		`CREATE INDEX IF NOT EXISTS idx_mem_created ON memories(project, created_at)`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, q := range stmts {
		if err := s.conn.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w (%s)", err, q)
		}
	}
	// Record schema version.
	if err := s.setMetaLocked("schema_version", fmt.Sprint(schemaVersion)); err != nil {
		return err
	}
	return nil
}

// --- meta key/value ---

// GetMeta returns the value for key, and whether it was present.
func (s *Store) GetMeta(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getMetaLocked(key)
}

func (s *Store) getMetaLocked(key string) (string, bool, error) {
	stmt, _, err := s.conn.Prepare(`SELECT value FROM meta WHERE key = ?`)
	if err != nil {
		return "", false, err
	}
	defer stmt.Close()
	if err := stmt.BindText(1, key); err != nil {
		return "", false, err
	}
	if stmt.Step() {
		return stmt.ColumnText(0), true, nil
	}
	return "", false, stmt.Err()
}

// SetMeta upserts a meta key/value.
func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setMetaLocked(key, value)
}

func (s *Store) setMetaLocked(key, value string) error {
	stmt, _, err := s.conn.Prepare(
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if err := stmt.BindText(1, key); err != nil {
		return err
	}
	if err := stmt.BindText(2, value); err != nil {
		return err
	}
	return stmt.Exec()
}

// --- float32 blob (de)serialization (little-endian) ---

func serializeF32(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func deserializeF32(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
