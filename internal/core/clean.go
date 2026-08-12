package core

import (
	"fmt"
	"os"

	"smate/internal/runtime"
	"smate/internal/store"
	"smate/internal/task"
)

type Cleaned struct {
	ID     string
	Purged bool
}

// Clean stops the container and frees disk space.
//
// An explicit id overrides every protection, ACTIVE included. Bulk mode only
// touches DONE, and with purge also CLEANED: ACTIVE is still in progress, and
// REJECTED can still be finished off via shell.
func Clean(s *store.Store, id string, purge bool) ([]Cleaned, error) {
	var targets []task.Task
	if id != "" {
		t, err := s.Load(id)
		if err != nil {
			return nil, err
		}
		targets = []task.Task{t}
	} else {
		all, err := s.List()
		if err != nil {
			return nil, err
		}
		for _, t := range all {
			if t.Status == task.StatusDone || (purge && t.Status == task.StatusCleaned) {
				targets = append(targets, t)
			}
		}
	}

	var done []Cleaned
	for _, t := range targets {
		if err := runtime.Remove(t.Container()); err != nil {
			return done, err
		}
		if purge {
			if err := os.RemoveAll(s.TaskDir(t.ID)); err != nil {
				return done, fmt.Errorf("remove task directory %s: %w", t.ID, err)
			}
			done = append(done, Cleaned{ID: t.ID, Purged: true})
			continue
		}
		if err := os.RemoveAll(s.Workspace(t.ID)); err != nil {
			return done, fmt.Errorf("remove workspace of task %s: %w", t.ID, err)
		}
		t.Status = task.StatusCleaned
		if err := s.Save(t); err != nil {
			return done, err
		}
		done = append(done, Cleaned{ID: t.ID})
	}
	return done, nil
}
