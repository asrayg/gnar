package store

import (
	"fmt"

	"github.com/asrayg/gnar/internal/model"
)

// Tx is a write transaction scope. Its methods run against the same connection
// without re-acquiring the store mutex (RunInTx holds it for the whole
// transaction), so they MUST only be called from within the RunInTx callback.
type Tx struct {
	s *Store
}

// Insert adds a memory within the transaction.
func (tx *Tx) Insert(m model.Memory, embedding []float32, embedID string) (int64, error) {
	return tx.s.insertLocked(m, embedding, embedID)
}

// ExistsSimilar checks for a duplicate within the transaction.
func (tx *Tx) ExistsSimilar(project string, kind model.Kind, content string) (bool, error) {
	return tx.s.existsSimilarLocked(project, kind, content)
}

// RunInTx runs fn inside a single SQLite transaction with all-or-nothing
// semantics: if fn returns an error (or panics) the transaction is rolled back
// and the store is left unchanged. The store mutex is held for the duration, so
// fn must not perform slow work (e.g. network calls) — prepare that beforehand.
func (s *Store) RunInTx(fn func(*Tx) error) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.conn.Exec("BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Roll back on early return or panic; ignore rollback error so we
			// don't mask the original failure.
			_ = s.conn.Exec("ROLLBACK")
		}
	}()

	if err := fn(&Tx{s: s}); err != nil {
		return err
	}
	if err := s.conn.Exec("COMMIT"); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}
