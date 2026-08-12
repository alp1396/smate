// Package gitx wraps the git CLI. Everything here runs host-side, on the
// trusted side of the sandbox boundary.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
}

func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func Root(dir string) (string, error) {
	return run(dir, "rev-parse", "--show-toplevel")
}

// CurrentBranch returns the checked out branch. A detached HEAD is an error: the
// task needs a branch to record.
func CurrentBranch(dir string) (string, error) {
	name, err := run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve current branch (detached HEAD?): %w", err)
	}
	return name, nil
}

func HeadSHA(dir string) (string, error) {
	return run(dir, "rev-parse", "HEAD")
}

func Archive(repo, dest string) error {
	git := exec.Command("git", "-C", repo, "archive", "--format=tar", "HEAD")
	tar := exec.Command("tar", "-x", "-f", "-", "-C", dest)

	pipe, err := git.StdoutPipe()
	if err != nil {
		return fmt.Errorf("archive: open pipe: %w", err)
	}
	tar.Stdin = pipe

	var gitErr, tarErr bytes.Buffer
	git.Stderr = &gitErr
	tar.Stderr = &tarErr

	if err := git.Start(); err != nil {
		return fmt.Errorf("archive: start git: %w", err)
	}
	if err := tar.Start(); err != nil {
		_ = git.Process.Kill()
		_ = git.Wait()
		return fmt.Errorf("archive: start tar: %w", err)
	}
	tarWait := tar.Wait()
	gitWait := git.Wait()

	if gitWait != nil {
		return fmt.Errorf("archive: git archive: %s", strings.TrimSpace(gitErr.String()))
	}
	if tarWait != nil {
		return fmt.Errorf("archive: tar: %s", strings.TrimSpace(tarErr.String()))
	}
	return nil
}

// InitAndBaseline creates a fresh throwaway repository over dir, commits its whole
// content and returns the hash of that baseline commit.
//
// The ignore patterns are written before the baseline, so the artefact directory
// is invisible to the agent's own `git add -A`. That is hygiene, not protection:
// what keeps artefacts out of the series is the exclusion at patch time.
func InitAndBaseline(dir string, ignore []string) (string, error) {
	if _, err := run(dir, "init", "-q", "-b", "main"); err != nil {
		return "", err
	}
	if err := setIdentity(dir); err != nil {
		return "", err
	}
	if err := writeExclude(dir, ignore); err != nil {
		return "", err
	}
	if _, err := run(dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := run(dir, "commit", "-q", "-m", "baseline"); err != nil {
		return "", err
	}
	return HeadSHA(dir)
}

// writeExclude fills .git/info/exclude, the untracked ignore list. Not .gitignore:
// that file is tracked and would show up in the patch as an edit to the project.
func writeExclude(dir string, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}
	path := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	body := "# written by smate\n" + strings.Join(patterns, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func TrackedUnder(dir, prefix string) ([]string, error) {
	out, err := run(dir, "ls-files", "--", Literal(prefix))
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// setIdentity writes the committer identity into the sandbox-local config. The
// host user's identity is reused — commits made through an agent are still
// authored by that person — with a service identity as the fallback.
func setIdentity(dir string) error {
	name := hostConfig("user.name")
	email := hostConfig("user.email")
	if name == "" || email == "" {
		name, email = "smate", "smate@localhost"
	}
	if _, err := run(dir, "config", "user.name", name); err != nil {
		return err
	}
	_, err := run(dir, "config", "user.email", email)
	return err
}

func hostConfig(key string) string {
	out, err := exec.Command("git", "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
