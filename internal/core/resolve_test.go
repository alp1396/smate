package core

import (
	"testing"

	"smate/internal/store"
	"smate/internal/task"
)

func TestResolveSingleActive(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "done", task.StatusDone)
	seed(t, s, "active", task.StatusActive)

	got, err := Resolve(s, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != "active" {
		t.Errorf("got %s, want active", got.ID)
	}
}

func TestResolveExplicitID(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "done", task.StatusDone)
	seed(t, s, "active", task.StatusActive)

	got, err := Resolve(s, "done")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != "done" {
		t.Errorf("got %s, want done", got.ID)
	}
}

func TestResolveNeedsIDWhenAmbiguous(t *testing.T) {
	s := store.At(t.TempDir())
	if _, err := Resolve(s, ""); err == nil {
		t.Error("no active tasks should be an error")
	}

	seed(t, s, "a", task.StatusActive)
	seed(t, s, "b", task.StatusActive)
	if _, err := Resolve(s, ""); err == nil {
		t.Error("several active tasks should be an error")
	}
}
