package store

import (
	"github.com/asraygopa/gnar/internal/model"
)

// Counts returns the total memory count and a per-kind breakdown (excluding archived).
func (s *Store) Counts() (total int, byKind map[model.Kind]int, projects int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byKind = map[model.Kind]int{}

	stmt, _, err := s.conn.Prepare(
		`SELECT kind, COUNT(*) FROM memories WHERE archived = 0 GROUP BY kind`)
	if err != nil {
		return 0, nil, 0, err
	}
	for stmt.Step() {
		k := model.Kind(stmt.ColumnText(0))
		c := int(stmt.ColumnInt64(1))
		byKind[k] = c
		total += c
	}
	if err := stmt.Err(); err != nil {
		stmt.Close()
		return 0, nil, 0, err
	}
	stmt.Close()

	pst, _, err := s.conn.Prepare(`SELECT COUNT(DISTINCT project) FROM memories WHERE archived = 0`)
	if err != nil {
		return 0, nil, 0, err
	}
	defer pst.Close()
	if pst.Step() {
		projects = int(pst.ColumnInt64(0))
	}
	return total, byKind, projects, pst.Err()
}

// TopProjects returns the projects with the most memories.
func (s *Store) TopProjects(limit int) ([]model.ProjectCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	stmt, _, err := s.conn.Prepare(
		`SELECT project, COUNT(*) c FROM memories WHERE archived = 0
		 GROUP BY project ORDER BY c DESC LIMIT ?`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	if err := stmt.BindInt64(1, int64(limit)); err != nil {
		return nil, err
	}
	var out []model.ProjectCount
	for stmt.Step() {
		out = append(out, model.ProjectCount{
			Project: stmt.ColumnText(0),
			Count:   int(stmt.ColumnInt64(1)),
		})
	}
	return out, stmt.Err()
}

// AllForReindex returns every non-archived memory (with text needed to re-embed),
// ordered by id. Embeddings are not loaded.
func (s *Store) AllForReindex() ([]model.Memory, error) {
	return s.List(Query{IncludeArchived: false, OrderBy: "created_asc"})
}
