package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"smate/internal/gitx"
	"smate/internal/store"
	"smate/internal/task"
)

// Apply drives the real git binary at both ends, so these tests do too: the
// interesting behaviour is what git does to a branch that is already there.

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, dir, content string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@localhost")
	writeFile(t, dir, "a.txt", content)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
}

func applyFixture(t *testing.T) (*store.Store, task.Task) {
	t.Helper()
	s := store.At(t.TempDir())

	repo := t.TempDir()
	gitInit(t, repo, "one\n")
	baseSHA, err := gitx.HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}

	tk := task.Task{ID: "t1", Repo: repo, Branch: "main", BaseSHA: baseSHA, Status: task.StatusActive}
	if err := s.Save(tk); err != nil {
		t.Fatal(err)
	}

	ws := s.Workspace(tk.ID)
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	gitInit(t, ws, "one\n")
	if tk.Baseline, err = gitx.HeadSHA(ws); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(tk); err != nil {
		t.Fatal(err)
	}
	sandboxCommit(t, ws, "two\n", "first iteration")
	return s, tk
}

func sandboxCommit(t *testing.T, ws, content, msg string) {
	t.Helper()
	writeFile(t, ws, "a.txt", content)
	git(t, ws, "add", "-A")
	git(t, ws, "commit", "-q", "-m", msg)
}

func fileIn(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Iterating on one task means importing under the same branch name again. The
// previous import is replaced — the sandbox can produce it again.
func TestApplyReplacesItsOwnPreviousImport(t *testing.T) {
	s, tk := applyFixture(t)

	tk, _, err := Apply(s, tk.ID)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if tk.Status != task.StatusDone {
		t.Errorf("status after apply = %s", tk.Status)
	}
	if tk.AppliedHead == "" {
		t.Error("apply did not record where the branch ended up")
	}
	if got := fileIn(t, tk.Repo, "a.txt"); got != "two\n" {
		t.Errorf("a.txt after the first import = %q", got)
	}

	sandboxCommit(t, s.Workspace(tk.ID), "three\n", "second iteration")
	tk, _, err = Apply(s, tk.ID)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if got := fileIn(t, tk.Repo, "a.txt"); got != "three\n" {
		t.Errorf("a.txt after the second import = %q", got)
	}

	// Rebuilt from the recorded base: both rounds, nothing from the import it
	// replaced.
	branch, err := gitx.CurrentBranch(tk.Repo)
	if err != nil || branch != tk.ID {
		t.Fatalf("left on branch %q (%v), want %s", branch, err, tk.ID)
	}
	out, err := exec.Command("git", "-C", tk.Repo, "log", "--format=%s", tk.BaseSHA+"..HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	subjects := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(subjects) != 2 || subjects[0] != "second iteration" || subjects[1] != "first iteration" {
		t.Errorf("commits over the base = %v, want both iterations and nothing else", subjects)
	}
}

// A branch under the task's name that smate did not create is somebody's work.
func TestApplyRefusesABranchItDidNotImport(t *testing.T) {
	s, tk := applyFixture(t)
	git(t, tk.Repo, "branch", tk.ID)

	_, _, err := Apply(s, tk.ID)
	if err == nil {
		t.Fatal("apply took over a branch it did not create")
	}
	if !strings.Contains(err.Error(), "did not put it there") {
		t.Errorf("error does not say why: %v", err)
	}
	if statusOf(t, s, tk.ID) != task.StatusRejected {
		t.Error("a refused import should leave the task REJECTED")
	}
}

// The previous import may be replaced; commits made on top of it may not — they
// exist nowhere else.
func TestApplyRefusesABranchWithCommitsOnTop(t *testing.T) {
	s, tk := applyFixture(t)
	tk, _, err := Apply(s, tk.ID)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}

	writeFile(t, tk.Repo, "a.txt", "two, fixed\n")
	git(t, tk.Repo, "commit", "-qam", "fix by hand")
	onTop, err := gitx.HeadSHA(tk.Repo)
	if err != nil {
		t.Fatal(err)
	}

	sandboxCommit(t, s.Workspace(tk.ID), "three\n", "second iteration")
	if _, _, err = Apply(s, tk.ID); err == nil {
		t.Fatal("apply threw away commits made on the branch")
	}
	if !strings.Contains(err.Error(), "did not come from smate") {
		t.Errorf("error does not say why: %v", err)
	}

	sha, ok, err := gitx.BranchSHA(tk.Repo, tk.ID)
	if err != nil || !ok {
		t.Fatalf("the branch is gone: ok=%v err=%v", ok, err)
	}
	if sha != onTop {
		t.Errorf("branch moved to %.8s, want %.8s", sha, onTop)
	}
}
