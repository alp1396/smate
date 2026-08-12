// Package store owns the on-disk layout of the control plane (~/.smate).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"smate/internal/task"
)

var ErrNotFound = errors.New("task not found")

type Store struct {
	root string // ~/.smate
}

func New() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return At(filepath.Join(home, ".smate")), nil
}

// At opens the control plane in the given directory. Used by tests.
func At(root string) *Store { return &Store{root: root} }

func (s *Store) TaskDir(id string) string { return filepath.Join(s.root, "tasks", id) }

// Workspace is the snapshot directory mounted into the container.
func (s *Store) Workspace(id string) string { return filepath.Join(s.TaskDir(id), "workspace") }

func (s *Store) ImagesDir() string { return filepath.Join(s.root, "images") }

func (s *Store) ImageDir(name string) string { return filepath.Join(s.ImagesDir(), name) }

func (s *Store) RolesDir() string { return filepath.Join(s.root, "roles") }

func (s *Store) PatchPath(id string) string { return filepath.Join(s.TaskDir(id), "result.patch") }

func (s *Store) metaPath(id string) string { return filepath.Join(s.TaskDir(id), "meta.json") }

func (s *Store) Exists(id string) bool {
	_, err := os.Stat(s.TaskDir(id))
	return err == nil
}

func (s *Store) Save(t task.Task) error {
	if err := os.MkdirAll(s.TaskDir(t.ID), 0o700); err != nil {
		return fmt.Errorf("create task directory %s: %w", t.ID, err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encode meta of task %s: %w", t.ID, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(s.metaPath(t.ID), data, 0o600); err != nil {
		return fmt.Errorf("write meta of task %s: %w", t.ID, err)
	}
	return nil
}

func (s *Store) Load(id string) (task.Task, error) {
	data, err := os.ReadFile(s.metaPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return task.Task{}, fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	if err != nil {
		return task.Task{}, fmt.Errorf("read meta of task %s: %w", id, err)
	}
	var t task.Task
	if err := json.Unmarshal(data, &t); err != nil {
		return task.Task{}, fmt.Errorf("parse meta of task %s: %w", id, err)
	}
	return t, nil
}

// List returns every task, newest first. By creation time rather than by id:
// ids are ticket numbers and sort like strings, so "1000" would land above
// "999".
func (s *Store) List() ([]task.Task, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "tasks"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task list: %w", err)
	}
	var tasks []task.Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := s.Load(e.Name())
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if !tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}
