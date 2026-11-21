package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const budgetFile = "budget.json"

// Store keeps the budget in a single JSON file: <dir>/budget.json.
type Store struct {
	dir string
	mu  sync.Mutex
}

func (s *Store) path() string { return filepath.Join(s.dir, budgetFile) }

// Migrate converts a data directory from the old multi-account layout
// (<dir>/<user>.json plus .session_key) to the single-user layout.
// If budget.json is missing and exactly one <user>.json exists, it is renamed;
// if several exist, Migrate refuses to guess and returns an error.
func (s *Store) Migrate() error {
	if err := os.Remove(filepath.Join(s.dir, ".session_key")); err == nil {
		log.Print("migrate: removed obsolete .session_key")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if _, err := os.Stat(s.path()); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	old, err := filepath.Glob(filepath.Join(s.dir, "*.json"))
	if err != nil {
		return err
	}
	switch len(old) {
	case 0:
		return nil
	case 1:
		if err := os.Rename(old[0], s.path()); err != nil {
			return err
		}
		log.Printf("migrate: renamed %s to %s", filepath.Base(old[0]), budgetFile)
		return nil
	default:
		names := make([]string, len(old))
		for i, p := range old {
			names[i] = filepath.Base(p)
		}
		return fmt.Errorf("migrate: found several account files in %s (%s); rename the one to keep to %s and move the others away",
			s.dir, strings.Join(names, ", "), budgetFile)
	}
}

func (s *Store) load() (*Budget, error) {
	b := &Budget{NextID: 1}
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	return b, json.Unmarshal(data, b)
}

func (s *Store) Load() (*Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Update loads the budget, applies fn and saves the result if fn succeeds.
func (s *Store) Update(fn func(*Budget) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(b); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}
