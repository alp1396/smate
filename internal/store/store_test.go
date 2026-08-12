package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"smate/internal/task"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	s := At(t.TempDir())
	want := task.Task{
		ID:        "123",
		Repo:      "/home/u/proj",
		Branch:    "main",
		BaseSHA:   "aaaabbbbccccdddd",
		Baseline:  "1111222233334444",
		Secrets:   []string{"secret.txt", "creds"},
		Image:     "myorg/dev:latest",
		Status:    task.StatusActive,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	got.CreatedAt, want.CreatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadMissing(t *testing.T) {
	s := At(t.TempDir())
	if _, err := s.Load("no-such-task"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListEmptyAndSorted(t *testing.T) {
	s := At(t.TempDir())
	tasks, err := s.List()
	if err != nil {
		t.Fatalf("List on an empty store: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %d", len(tasks))
	}
	// Newest first, and ids that sort like strings must not decide it: 1000 was
	// started after 999 and belongs above it.
	day := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"999", "1000", "7"} {
		created := day.Add(time.Duration(i) * time.Hour)
		if err := s.Save(task.Task{ID: id, Status: task.StatusActive, CreatedAt: created}); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	tasks, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := []string{tasks[0].ID, tasks[1].ID, tasks[2].ID}
	want := []string{"7", "1000", "999"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task order: got %v, want %v", got, want)
		}
	}
}

// Tasks saved before creation time was recorded — and two started in the same
// second — must still come out in a stable order rather than shuffling between
// two listings.
func TestListWithoutTimestampsIsStable(t *testing.T) {
	s := At(t.TempDir())
	for _, id := range []string{"b2", "a1", "c3"} {
		if err := s.Save(task.Task{ID: id, Status: task.StatusActive}); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	tasks, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := []string{tasks[0].ID, tasks[1].ID, tasks[2].ID}
	want := []string{"a1", "b2", "c3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task order: got %v, want %v", got, want)
		}
	}
}
