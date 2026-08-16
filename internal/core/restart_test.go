package core

import (
	"os"
	"strings"
	"testing"

	"smate/internal/store"
	"smate/internal/task"
)

// Restart is mostly docker work, but its guards are not: they are what keeps the
// command from promising a container over a workspace that is not there.

func TestRestartRefusesCleaned(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "gone", task.StatusCleaned)

	_, _, err := Restart(s, "gone")
	if err == nil || !strings.Contains(err.Error(), "cleaned") {
		t.Fatalf("restart of a cleaned task: got %v, want a refusal naming it", err)
	}
}

func TestRestartRefusesMissingWorkspace(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "bare", task.StatusActive)
	if err := os.RemoveAll(s.Workspace("bare")); err != nil {
		t.Fatal(err)
	}

	_, _, err := Restart(s, "bare")
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("restart without a workspace: got %v, want a refusal naming it", err)
	}
}
