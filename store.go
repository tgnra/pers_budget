package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Store keeps each account's budget in its own JSON file: <dir>/<user>.json.
type Store struct {
	dir string
	mu  sync.Mutex
}

func (s *Store) path(user string) string { return filepath.Join(s.dir, user+".json") }

func (s *Store) load(user string) (*Budget, error) {
	b := &Budget{NextID: 1}
	data, err := os.ReadFile(s.path(user))
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	return b, json.Unmarshal(data, b)
}

func (s *Store) Load(user string) (*Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(user)
}

// Update loads the user's budget, applies fn and saves the result if fn succeeds.
func (s *Store) Update(user string, fn func(*Budget) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.load(user)
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
	tmp := s.path(user) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(user))
}
