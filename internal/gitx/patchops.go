package gitx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func IsClean(dir string) (bool, error) {
	out, err := run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// CommitAll commits the whole work tree and returns the commit hash.
//
// No pathspec: the artefact directory is kept out by the ignore list written at
// start, and an exclude pathspec still counts as mentioning the path — git would
// refuse the whole add with "the following paths are ignored".
func CommitAll(dir, msg string) (string, error) {
	if _, err := run(dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := run(dir, "commit", "-q", "-m", msg); err != nil {
		return "", err
	}
	return HeadSHA(dir)
}

func Diff(dir, base string) (string, error) {
	return run(dir, "diff", base)
}

func ChangedFiles(dir, base string) ([]string, error) {
	out, err := run(dir, "diff", "--name-only", base, "HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// FormatPatch writes the patch series from base to HEAD into out. Excluded paths
// never reach it: git drops their changes and any commit left with nothing else,
// which keeps us out of the business of cutting up patch text ourselves.
//
// The :(literal) magic makes git read an exclusion the way the rest of smate does;
// without it git expands wildcards by its own rules and the three places that read
// the denylist stop agreeing.
func FormatPatch(dir, base, out string, exclude []string) error {
	args := []string{"-C", dir, "format-patch", base, "--stdout"}
	if len(exclude) > 0 {
		args = append(args, "--", ".")
		for _, p := range exclude {
			args = append(args, Exclude(p))
		}
	}
	cmd := exec.Command("git", args...)
	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	defer f.Close()

	var errb strings.Builder
	cmd.Stdout, cmd.Stderr = f, &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git format-patch: %s", strings.TrimSpace(errb.String()))
	}
	return nil
}

func Exclude(p string) string { return ":(exclude,literal)" + p }

func Literal(p string) string { return ":(literal)" + p }

// ApplyCheck verifies the patch applies to the current tree, changing nothing.
func ApplyCheck(dir, patchFile string) error {
	_, err := run(dir, "apply", "--check", patchFile)
	return err
}

func SwitchNew(dir, branch, at string) error {
	_, err := run(dir, "switch", "-c", branch, at)
	return err
}

func Switch(dir, branch string) error {
	_, err := run(dir, "switch", branch)
	return err
}

// SwitchDetach checks out a commit without a branch. Standing off the branch is
// what makes it deletable.
func SwitchDetach(dir, at string) error {
	_, err := run(dir, "switch", "--detach", at)
	return err
}

// BranchSHA returns the commit a branch points at; ok is false when there is no
// such branch — an unknown ref is an answer, not a failure.
func BranchSHA(dir, branch string) (sha string, ok bool, err error) {
	out, err := run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return "", false, nil
	}
	return out, true, nil
}

// DeleteBranch drops a branch without the merged check, to roll back a failed
// import.
func DeleteBranch(dir, branch string) error {
	_, err := run(dir, "branch", "-D", branch)
	return err
}

func Am(dir, patchFile string) error {
	_, err := run(dir, "am", patchFile)
	return err
}

func AmAbort(dir string) error {
	_, err := run(dir, "am", "--abort")
	return err
}
