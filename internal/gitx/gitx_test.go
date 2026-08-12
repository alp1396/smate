package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the real git binary: the wrappers are thin, and mocking git
// would only test the mock.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := run(dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	mustRun(t, dir, "init", "-q", "-b", "main")
	mustRun(t, dir, "config", "user.name", "test")
	mustRun(t, dir, "config", "user.email", "test@localhost")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "sub/b.txt", "two\n")
	mustRun(t, dir, "add", "-A")
	mustRun(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestArchiveTakesTrackedFilesOnly(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "untracked.txt", "not committed\n")

	dest := t.TempDir()
	if err := Archive(repo, dest); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dest, "a.txt")); err != nil || string(got) != "one\n" {
		t.Errorf("a.txt: got %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub/b.txt")); err != nil {
		t.Errorf("sub/b.txt is missing from the snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Error(".git must never reach the snapshot")
	}
	if _, err := os.Stat(filepath.Join(dest, "untracked.txt")); !os.IsNotExist(err) {
		t.Error("untracked files must not reach the snapshot")
	}
}

func TestInitAndBaseline(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")

	baseline, err := InitAndBaseline(ws, nil)
	if err != nil {
		t.Fatalf("InitAndBaseline: %v", err)
	}
	if len(baseline) < 7 {
		t.Fatalf("baseline hash looks wrong: %q", baseline)
	}
	head, err := HeadSHA(ws)
	if err != nil || head != baseline {
		t.Errorf("HEAD %q != baseline %q (%v)", head, baseline, err)
	}
	clean, err := IsClean(ws)
	if err != nil || !clean {
		t.Errorf("the tree must be clean right after baseline (clean=%v, %v)", clean, err)
	}
}

func TestIsCleanAndChangedFiles(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")
	baseline, err := InitAndBaseline(ws, nil)
	if err != nil {
		t.Fatal(err)
	}

	write(t, ws, "b.txt", "two\n")
	if clean, err := IsClean(ws); err != nil || clean {
		t.Errorf("a new file must make the tree dirty (clean=%v, %v)", clean, err)
	}
	if _, err := CommitAll(ws, "add b"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	changed, err := ChangedFiles(ws, baseline)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if len(changed) != 1 || changed[0] != "b.txt" {
		t.Errorf("changed files: got %v, want [b.txt]", changed)
	}
}

// The exclusion keeps secrets out of the series: both the change and a commit made
// of nothing else have to disappear.
func TestFormatPatchExcludesSecrets(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")
	write(t, ws, "secret.txt", "real secret\n")
	baseline, err := InitAndBaseline(ws, nil)
	if err != nil {
		t.Fatal(err)
	}

	write(t, ws, "a.txt", "one changed\n")
	write(t, ws, "secret.txt", "agent secret\n")
	if _, err := CommitAll(ws, "touch both"); err != nil {
		t.Fatal(err)
	}
	write(t, ws, "secret.txt", "agent secret again\n")
	if _, err := CommitAll(ws, "touch the secret only"); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "series.patch")

	if err := FormatPatch(ws, baseline, out, nil); err != nil {
		t.Fatalf("FormatPatch without excludes: %v", err)
	}
	full := read(t, out)
	if !strings.Contains(full, "secret.txt") || strings.Count(full, "Subject: ") != 2 {
		t.Fatalf("expected both commits and the secret without excludes, got:\n%s", full)
	}

	if err := FormatPatch(ws, baseline, out, []string{"secret.txt"}); err != nil {
		t.Fatalf("FormatPatch with excludes: %v", err)
	}
	filtered := read(t, out)
	if strings.Contains(filtered, "secret.txt") {
		t.Error("an excluded path leaked into the series")
	}
	if !strings.Contains(filtered, "a.txt") {
		t.Error("the regular change is missing from the series")
	}
	if n := strings.Count(filtered, "Subject: "); n != 1 {
		t.Errorf("expected 1 commit left, got %d:\n%s", n, filtered)
	}
}

// git must not expand a mask on its own, or it excludes files the rest of smate
// keeps.
func TestFormatPatchTakesExclusionsLiterally(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")
	write(t, ws, "conf.env", "value\n")
	baseline, err := InitAndBaseline(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, ws, "conf.env", "changed\n")
	if _, err := CommitAll(ws, "touch the env"); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "series.patch")
	if err := FormatPatch(ws, baseline, out, []string{"*.env"}); err != nil {
		t.Fatalf("FormatPatch: %v", err)
	}
	if !strings.Contains(read(t, out), "conf.env") {
		t.Error("git expanded the mask — the exclusion was not taken literally")
	}
}

func TestInitAndBaselineIgnoresArtifacts(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")
	write(t, ws, ".smate/request.md", "the task\n")
	if _, err := InitAndBaseline(ws, []string{"/.smate/"}); err != nil {
		t.Fatal(err)
	}

	tracked, err := TrackedUnder(ws, ".smate")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 0 {
		t.Errorf("artefacts were committed into the baseline: %v", tracked)
	}
	clean, err := IsClean(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("the artefact directory shows up as a change")
	}
}

// The uncommitted snapshot on apply must not pick up artefacts. The ignore list
// has to do it alone: an exclude pathspec next to an ignored path makes git refuse
// the add outright.
func TestCommitAllLeavesArtifactsAlone(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	write(t, ws, "a.txt", "one\n")
	if _, err := InitAndBaseline(ws, []string{"/.smate/"}); err != nil {
		t.Fatal(err)
	}
	write(t, ws, "a.txt", "two\n")
	write(t, ws, ".smate/runs/1/log", "noise\n")
	if _, err := CommitAll(ws, "apply snapshot"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	tracked, err := TrackedUnder(ws, ".smate")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 0 {
		t.Errorf("artefacts were staged: %v", tracked)
	}
}

func TestTrackedUnderFindsAClaimedDirectory(t *testing.T) {
	repo := newRepo(t)
	got, err := TrackedUnder(repo, ".smate")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a repository without artefacts reported %v", got)
	}

	write(t, repo, ".smate/notes.md", "the project's own\n")
	mustRun(t, repo, "add", "-A")
	mustRun(t, repo, "commit", "-q", "-m", "project keeps .smate")
	got, err = TrackedUnder(repo, ".smate")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != ".smate/notes.md" {
		t.Errorf("TrackedUnder = %v, want [.smate/notes.md]", got)
	}
}

// Two tasks from one commit: the second branch must still start there after the
// first import moved HEAD.
func TestSwitchNewStartsAtTheGivenCommit(t *testing.T) {
	repo := newRepo(t)
	base, err := HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := SwitchNew(repo, "task-a", base); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "a.txt", "changed by a\n")
	if _, err := CommitAll(repo, "a"); err != nil {
		t.Fatal(err)
	}
	if err := SwitchNew(repo, "task-b", base); err != nil {
		t.Fatalf("second branch from the recorded base: %v", err)
	}
	head, err := HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}
	if head != base {
		t.Errorf("task-b starts at %.8s, want the recorded base %.8s", head, base)
	}
}

// A branch that is not there is an answer, not an error — apply leans on that to
// tell a first import from a repeat one.
func TestBranchSHAReportsAbsence(t *testing.T) {
	repo := newRepo(t)
	base, err := HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}

	sha, ok, err := BranchSHA(repo, "task-a")
	if err != nil {
		t.Fatalf("BranchSHA on a missing branch: %v", err)
	}
	if ok || sha != "" {
		t.Errorf("a branch that does not exist reported as %q (ok=%v)", sha, ok)
	}

	if err := SwitchNew(repo, "task-a", base); err != nil {
		t.Fatal(err)
	}
	sha, ok, err = BranchSHA(repo, "task-a")
	if err != nil || !ok {
		t.Fatalf("BranchSHA on an existing branch: ok=%v err=%v", ok, err)
	}
	if sha != base {
		t.Errorf("BranchSHA = %.8s, want %.8s", sha, base)
	}

	// It moves with the branch: that is what makes a commit on top detectable.
	write(t, repo, "a.txt", "changed by hand\n")
	if _, err := CommitAll(repo, "by hand"); err != nil {
		t.Fatal(err)
	}
	moved, _, err := BranchSHA(repo, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if moved == base {
		t.Error("BranchSHA did not follow the branch")
	}
}

// A branch cannot be deleted while it is checked out, and apply replaces the
// branch it may well be standing on.
func TestSwitchDetachFreesTheBranch(t *testing.T) {
	repo := newRepo(t)
	base, err := HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := SwitchNew(repo, "task-a", base); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBranch(repo, "task-a"); err == nil {
		t.Fatal("deleted the branch that was checked out")
	}

	if err := SwitchDetach(repo, base); err != nil {
		t.Fatalf("SwitchDetach: %v", err)
	}
	if _, err := CurrentBranch(repo); err == nil {
		t.Error("still on a branch after detaching")
	}
	if err := DeleteBranch(repo, "task-a"); err != nil {
		t.Errorf("DeleteBranch after detaching: %v", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
