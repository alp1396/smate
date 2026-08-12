package core

import (
	"os"
	"path/filepath"
	"testing"

	"smate/internal/store"
	"smate/internal/task"
)

// Clean needs no docker here: removing a container that does not exist is a
// no-op by design, so only the protection matrix and the disk work are tested.

func seed(t *testing.T, s *store.Store, id string, status task.Status) {
	t.Helper()
	if err := os.MkdirAll(s.Workspace(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Workspace(id), "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(task.Task{ID: id, Status: status}); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T, s *store.Store, id string) task.Status {
	t.Helper()
	got, err := s.Load(id)
	if err != nil {
		t.Fatalf("Load %s: %v", id, err)
	}
	return got.Status
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("should have been removed: %s", path)
	}
}

func assertThere(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("should have stayed: %s (%v)", path, err)
	}
}

func TestCleanBulkTouchesOnlyDone(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "done", task.StatusDone)
	seed(t, s, "active", task.StatusActive)
	seed(t, s, "rejected", task.StatusRejected)

	done, err := Clean(s, "", false)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(done) != 1 || done[0].ID != "done" || done[0].Purged {
		t.Fatalf("cleaned: got %v, want [{done false}]", done)
	}
	assertGone(t, s.Workspace("done"))
	if got := statusOf(t, s, "done"); got != task.StatusCleaned {
		t.Errorf("status of done: got %s, want CLEANED", got)
	}

	for _, id := range []string{"active", "rejected"} {
		assertThere(t, s.Workspace(id))
	}
	if got := statusOf(t, s, "active"); got != task.StatusActive {
		t.Errorf("active must stay untouched, got %s", got)
	}
	if got := statusOf(t, s, "rejected"); got != task.StatusRejected {
		t.Errorf("rejected must stay untouched, got %s", got)
	}
}

func TestCleanBulkPurgeAlsoTakesCleaned(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "done", task.StatusDone)
	seed(t, s, "cleaned", task.StatusCleaned)
	seed(t, s, "active", task.StatusActive)

	done, err := Clean(s, "", true)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(done) != 2 {
		t.Fatalf("expected two tasks purged, got %v", done)
	}
	assertGone(t, s.TaskDir("done"))
	assertGone(t, s.TaskDir("cleaned"))
	assertThere(t, s.TaskDir("active"))
}

func TestCleanExplicitIDOverridesProtection(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "active", task.StatusActive)

	if _, err := Clean(s, "active", false); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	assertGone(t, s.Workspace("active"))
	if got := statusOf(t, s, "active"); got != task.StatusCleaned {
		t.Errorf("status: got %s, want CLEANED", got)
	}
}

func TestCleanNonPurgeKeepsPatch(t *testing.T) {
	s := store.At(t.TempDir())
	seed(t, s, "done", task.StatusDone)
	if err := os.WriteFile(s.PatchPath("done"), []byte("patch"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Clean(s, "done", false); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	assertThere(t, s.PatchPath("done"))

	if _, err := Clean(s, "done", true); err != nil {
		t.Fatalf("Clean purge: %v", err)
	}
	assertGone(t, s.TaskDir("done"))
}
