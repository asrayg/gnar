package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/asraygopa/gnar/internal/model"
	"github.com/ncruces/go-sqlite3"
)

// memCols is the canonical column list/order for scanning a Memory (+embedding).
const memCols = `id, project, kind, title, content, tags, files, source, session, meta, pinned, embedding, embed_id, created_at, updated_at`

// SearchRow is a memory plus its stored embedding (for in-Go ranking).
type SearchRow struct {
	Memory    model.Memory
	Embedding []float32
	EmbedID   string // embedder identity that produced Embedding
}

// Insert stores a new memory (with optional embedding) and returns its id.
func (s *Store) Insert(m model.Memory, embedding []float32, embedID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertLocked(m, embedding, embedID)
}

// insertLocked is the body of Insert; the caller must already hold s.mu.
func (s *Store) insertLocked(m model.Memory, embedding []float32, embedID string) (int64, error) {
	stmt, _, err := s.conn.Prepare(`
		INSERT INTO memories
			(project, kind, title, content, tags, files, source, session, meta,
			 pinned, archived, embedding, embed_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	bind := bindCtx{stmt: stmt}
	bind.text(1, m.Project)
	bind.text(2, string(m.Kind))
	bind.text(3, m.Title)
	bind.text(4, m.Content)
	bind.text(5, jsonArray(m.Tags))
	bind.text(6, jsonArray(m.Files))
	bind.text(7, m.Source)
	bind.text(8, m.Session)
	bind.text(9, jsonObject(m.Meta))
	bind.boolean(10, m.Pinned)
	bind.blobOrNull(11, serializeF32(embedding))
	bind.text(12, embedID)
	bind.int64(13, m.CreatedAt.Unix())
	bind.int64(14, m.UpdatedAt.Unix())
	if bind.err != nil {
		return 0, bind.err
	}
	if err := stmt.Exec(); err != nil {
		return 0, err
	}
	return s.conn.LastInsertRowID(), nil
}

// Get returns a memory by id (archived or not). ok is false if not found.
func (s *Store) Get(id int64) (model.Memory, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`SELECT ` + memCols + ` FROM memories WHERE id = ?`)
	if err != nil {
		return model.Memory{}, false, err
	}
	defer stmt.Close()
	if err := stmt.BindInt64(1, id); err != nil {
		return model.Memory{}, false, err
	}
	if stmt.Step() {
		m, _, _ := scanMemory(stmt)
		return m, true, nil
	}
	return model.Memory{}, false, stmt.Err()
}

// Update overwrites the mutable fields of an existing memory (by m.ID).
func (s *Store) Update(m model.Memory, embedding []float32, embedID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`
		UPDATE memories SET
			kind = ?, title = ?, content = ?, tags = ?, files = ?,
			source = ?, session = ?, meta = ?, pinned = ?,
			embedding = ?, embed_id = ?, updated_at = ?
		WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	bind := bindCtx{stmt: stmt}
	bind.text(1, string(m.Kind))
	bind.text(2, m.Title)
	bind.text(3, m.Content)
	bind.text(4, jsonArray(m.Tags))
	bind.text(5, jsonArray(m.Files))
	bind.text(6, m.Source)
	bind.text(7, m.Session)
	bind.text(8, jsonObject(m.Meta))
	bind.boolean(9, m.Pinned)
	bind.blobOrNull(10, serializeF32(embedding))
	bind.text(11, embedID)
	bind.int64(12, m.UpdatedAt.Unix())
	bind.int64(13, m.ID)
	if bind.err != nil {
		return bind.err
	}
	return stmt.Exec()
}

// ExistsSimilar reports whether a memory with the same project, kind, and content
// already exists in ANY state (archived or not). Used to de-duplicate imports so
// re-importing is idempotent and a soft-deleted memory is not resurrected as a
// live duplicate.
func (s *Store) ExistsSimilar(project string, kind model.Kind, content string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.existsSimilarLocked(project, kind, content)
}

// existsSimilarLocked is the body of ExistsSimilar; the caller must hold s.mu.
func (s *Store) existsSimilarLocked(project string, kind model.Kind, content string) (bool, error) {
	stmt, _, err := s.conn.Prepare(
		`SELECT 1 FROM memories WHERE project = ? AND kind = ? AND content = ? LIMIT 1`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	bind := bindCtx{stmt: stmt}
	bind.text(1, project)
	bind.text(2, string(kind))
	bind.text(3, content)
	if bind.err != nil {
		return false, bind.err
	}
	found := stmt.Step()
	return found, stmt.Err()
}

// GetEmbedding returns the stored embedding and embedder id for a memory.
func (s *Store) GetEmbedding(id int64) ([]float32, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`SELECT embedding, embed_id FROM memories WHERE id = ?`)
	if err != nil {
		return nil, "", err
	}
	defer stmt.Close()
	if err := stmt.BindInt64(1, id); err != nil {
		return nil, "", err
	}
	if stmt.Step() {
		var emb []float32
		if stmt.ColumnType(0) != sqlite3.NULL {
			emb = deserializeF32(stmt.ColumnBlob(0, nil))
		}
		return emb, stmt.ColumnText(1), nil
	}
	return nil, "", stmt.Err()
}

// UpdateEmbedding sets only the embedding/embed_id for a memory (used by reindex).
func (s *Store) UpdateEmbedding(id int64, embedding []float32, embedID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`UPDATE memories SET embedding = ?, embed_id = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	bind := bindCtx{stmt: stmt}
	bind.blobOrNull(1, serializeF32(embedding))
	bind.text(2, embedID)
	bind.int64(3, id)
	if bind.err != nil {
		return bind.err
	}
	return stmt.Exec()
}

// SetArchived marks a memory archived (soft delete) or restores it.
func (s *Store) SetArchived(id int64, archived bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`UPDATE memories SET archived = ? WHERE id = ?`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	if err := stmt.BindBool(1, archived); err != nil {
		return false, err
	}
	if err := stmt.BindInt64(2, id); err != nil {
		return false, err
	}
	if err := stmt.Exec(); err != nil {
		return false, err
	}
	return s.conn.Changes() > 0, nil
}

// Delete permanently removes a memory. Returns whether a row was deleted.
func (s *Store) Delete(id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stmt, _, err := s.conn.Prepare(`DELETE FROM memories WHERE id = ?`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	if err := stmt.BindInt64(1, id); err != nil {
		return false, err
	}
	if err := stmt.Exec(); err != nil {
		return false, err
	}
	return s.conn.Changes() > 0, nil
}

// Query filters memories for listing/search.
type Query struct {
	Project         string       // exact project key; empty means all projects
	Kinds           []model.Kind // restrict to these kinds (empty = any)
	Tags            []string     // require any of these tags (matched in SQL before LIMIT)
	IncludeArchived bool
	PinnedOnly      bool
	Limit           int    // 0 = no limit
	OrderBy         string // "created_desc" (default), "created_asc"
}

// List returns memories matching q (without embeddings).
func (s *Store) List(q Query) ([]model.Memory, error) {
	rows, err := s.search(q, false)
	if err != nil {
		return nil, err
	}
	out := make([]model.Memory, len(rows))
	for i := range rows {
		out[i] = rows[i].Memory
	}
	return out, nil
}

// Candidates returns memories (with embeddings) for in-Go ranking.
func (s *Store) Candidates(q Query) ([]SearchRow, error) {
	return s.search(q, true)
}

func (s *Store) search(q Query, withEmbedding bool) ([]SearchRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var where []string
	var args []any
	if q.Project != "" {
		where = append(where, "project = ?")
		args = append(args, q.Project)
	}
	if !q.IncludeArchived {
		where = append(where, "archived = 0")
	}
	if q.PinnedOnly {
		where = append(where, "pinned = 1")
	}
	if len(q.Kinds) > 0 {
		ph := make([]string, len(q.Kinds))
		for i, k := range q.Kinds {
			ph[i] = "?"
			args = append(args, string(k))
		}
		where = append(where, "kind IN ("+strings.Join(ph, ",")+")")
	}
	if len(q.Tags) > 0 {
		// Tags are stored as a JSON array of lowercased strings, e.g. ["a","b"].
		// Match any requested tag with a quote-delimited LIKE so the candidate
		// cap (LIMIT) applies to tag-matching rows, not the unfiltered recent set.
		ors := make([]string, 0, len(q.Tags))
		for _, t := range q.Tags {
			ors = append(ors, `tags LIKE ? ESCAPE '\'`)
			args = append(args, `%"`+escapeLike(strings.ToLower(strings.TrimSpace(t)))+`"%`)
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	sql := `SELECT ` + memCols + ` FROM memories`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	switch q.OrderBy {
	case "created_asc":
		sql += " ORDER BY created_at ASC, id ASC"
	default:
		sql += " ORDER BY created_at DESC, id DESC"
	}
	if q.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", q.Limit)
	}

	stmt, _, err := s.conn.Prepare(sql)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for i, a := range args {
		if err := stmt.BindText(i+1, a.(string)); err != nil {
			return nil, err
		}
	}
	var out []SearchRow
	for stmt.Step() {
		m, emb, embID := scanMemory(stmt)
		row := SearchRow{Memory: m}
		if withEmbedding {
			row.Embedding = emb
			row.EmbedID = embID
		}
		out = append(out, row)
	}
	return out, stmt.Err()
}

// scanMemory reads a row in memCols order into a Memory, its embedding, and the
// embedder id that produced that embedding.
func scanMemory(stmt *sqlite3.Stmt) (model.Memory, []float32, string) {
	m := model.Memory{
		ID:      stmt.ColumnInt64(0),
		Project: stmt.ColumnText(1),
		Kind:    model.Kind(stmt.ColumnText(2)),
		Title:   stmt.ColumnText(3),
		Content: stmt.ColumnText(4),
		Source:  stmt.ColumnText(7),
		Session: stmt.ColumnText(8),
		Pinned:  stmt.ColumnInt64(10) != 0,
	}
	m.Tags = parseJSONArray(stmt.ColumnText(5))
	m.Files = parseJSONArray(stmt.ColumnText(6))
	m.Meta = parseJSONObject(stmt.ColumnText(9))
	var emb []float32
	if stmt.ColumnType(11) != sqlite3.NULL {
		emb = deserializeF32(stmt.ColumnBlob(11, nil))
	}
	m.CreatedAt = time.Unix(stmt.ColumnInt64(13), 0)
	m.UpdatedAt = time.Unix(stmt.ColumnInt64(14), 0)
	return m, emb, stmt.ColumnText(12)
}

// --- small binding/JSON helpers ---

type bindCtx struct {
	stmt *sqlite3.Stmt
	err  error
}

func (b *bindCtx) text(i int, v string) {
	if b.err == nil {
		b.err = b.stmt.BindText(i, v)
	}
}
func (b *bindCtx) int64(i int, v int64) {
	if b.err == nil {
		b.err = b.stmt.BindInt64(i, v)
	}
}
func (b *bindCtx) boolean(i int, v bool) {
	if b.err == nil {
		b.err = b.stmt.BindBool(i, v)
	}
}
func (b *bindCtx) blobOrNull(i int, v []byte) {
	if b.err != nil {
		return
	}
	if v == nil {
		b.err = b.stmt.BindNull(i)
	} else {
		b.err = b.stmt.BindBlob(i, v)
	}
}

// escapeLike escapes LIKE wildcards so a tag value is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func jsonArray(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func jsonObject(v map[string]any) string {
	if len(v) == 0 {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func parseJSONArray(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func parseJSONObject(s string) map[string]any {
	if s == "" || s == "{}" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
