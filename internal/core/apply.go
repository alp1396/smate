package core

import (
	"fmt"
	"os"
	"strings"

	"smate/internal/gitx"
	"smate/internal/patch"
	"smate/internal/store"
	"smate/internal/task"
)

// ErrNothingToApply means the sandbox holds no changes over the baseline.
var ErrNothingToApply = fmt.Errorf("no changes over the baseline")

// Apply produces a patch series from the sandbox, validates it and imports it
// into the working repository as a branch named after the task. A failed check
// writes nothing and marks the task REJECTED, which can be finished off via
// shell and applied again.
func Apply(s *store.Store, id string) (task.Task, patch.Report, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return t, patch.Report{}, err
	}
	ws := s.Workspace(t.ID)

	clean, err := gitx.IsClean(ws)
	if err != nil {
		return t, patch.Report{}, err
	}
	if !clean {
		if _, err := gitx.CommitAll(ws, "apply snapshot"); err != nil {
			return t, patch.Report{}, err
		}
	}
	head, err := gitx.HeadSHA(ws)
	if err != nil {
		return t, patch.Report{}, err
	}
	if head == t.Baseline {
		return t, patch.Report{}, ErrNothingToApply
	}

	// 2. Cut paths the agent touched: not imported, but reported.
	changed, err := gitx.ChangedFiles(ws, t.Baseline)
	if err != nil {
		return t, patch.Report{}, err
	}
	var cut []string
	for _, f := range changed {
		if _, ok := patch.MatchSecret(f, t.Secrets); ok {
			cut = append(cut, f)
		}
	}

	// 3. From the recorded baseline, not from whatever history the container
	//    ended up with.
	patchFile := s.PatchPath(t.ID)
	if err := gitx.FormatPatch(ws, t.Baseline, patchFile, t.Secrets); err != nil {
		return t, patch.Report{}, err
	}
	data, err := os.ReadFile(patchFile)
	if err != nil {
		return t, patch.Report{}, fmt.Errorf("read %s: %w", patchFile, err)
	}
	if len(data) == 0 {
		return t, patch.Report{Cut: cut}, ErrNothingToApply
	}

	rep, err := patch.Validate(data, t.Secrets)
	if err != nil {
		return t, patch.Report{}, reject(s, t, fmt.Errorf("patch validation: %w", err))
	}
	rep.Cut = cut

	// 5. The keys handed to the container must not travel back in the patch.
	keys, err := s.LoadEnv()
	if err != nil {
		return t, patch.Report{}, err
	}
	if leaked := patch.ScanValues(data, keys); len(leaked) > 0 {
		return t, patch.Report{}, reject(s, t, fmt.Errorf(
			"the patch contains the value of %s — refusing to import a key into the repository",
			strings.Join(leaked, ", ")))
	}

	// 6. First guard: the working copy is clean.
	repoClean, err := gitx.IsClean(t.Repo)
	if err != nil {
		return t, patch.Report{}, err
	}
	if !repoClean {
		return t, patch.Report{}, reject(s, t, fmt.Errorf(
			"%s has uncommitted changes — the import must not mix with your work", t.Repo))
	}

	// 7. Second guard: what sits on the task's branch. Our own previous import is
	//    replaced — the sandbox can produce it again — but a branch that has moved
	//    since holds commits that exist nowhere else. Asked here rather than at the
	//    switch below, so the refusal does not arrive late.
	prevHead, branchExists, err := gitx.BranchSHA(t.Repo, t.ID)
	if err != nil {
		return t, patch.Report{}, err
	}
	if branchExists {
		switch {
		case t.AppliedHead == "":
			return t, patch.Report{}, reject(s, t, fmt.Errorf(
				"branch %s already exists in %s and smate did not put it there — rename or delete it first",
				t.ID, t.Repo))
		case prevHead != t.AppliedHead:
			return t, patch.Report{}, reject(s, t, fmt.Errorf(
				"branch %s in %s has commits that did not come from smate — delete it yourself if what is on it is spent",
				t.ID, t.Repo))
		}
	}

	// 8. Stand on the commit the snapshot was taken from, so several tasks cut
	//    from one commit import in any order. Detached, because the previous
	//    import may still hold that branch name and git will not delete the branch
	//    it is on. Where to return is taken from the repository rather than the
	//    task, and never the branch about to be replaced.
	here, err := gitx.CurrentBranch(t.Repo)
	if err != nil || here == t.ID {
		here = t.Branch
	}
	if err := gitx.SwitchDetach(t.Repo, t.BaseSHA); err != nil {
		return t, patch.Report{}, err
	}

	// 9. The series must apply to that tree. Asked before the previous import is
	//    dropped: a patch that does not apply must not cost the branch.
	if err := gitx.ApplyCheck(t.Repo, patchFile); err != nil {
		_ = gitx.Switch(t.Repo, here)
		return t, patch.Report{}, reject(s, t, fmt.Errorf("patch does not apply to %s: %w", t.Repo, err))
	}

	if branchExists {
		if err := gitx.DeleteBranch(t.Repo, t.ID); err != nil {
			_ = gitx.Switch(t.Repo, here)
			return t, patch.Report{}, err
		}
	}
	if err := gitx.SwitchNew(t.Repo, t.ID, t.BaseSHA); err != nil {
		_ = gitx.Switch(t.Repo, here)
		return t, patch.Report{}, err
	}

	if err := gitx.Am(t.Repo, patchFile); err != nil {
		rollback(t, here)
		return t, patch.Report{}, reject(s, t, fmt.Errorf("git am: %w", err))
	}

	// 12. Where the branch ended up, so the next import can tell this one from
	//     anything committed on top of it.
	if t.AppliedHead, err = gitx.HeadSHA(t.Repo); err != nil {
		return t, patch.Report{}, err
	}
	t.Status = task.StatusDone
	if err := s.Save(t); err != nil {
		return t, rep, err
	}
	return t, rep, nil
}

func reject(s *store.Store, t task.Task, cause error) error {
	t.Status = task.StatusRejected
	if err := s.Save(t); err != nil {
		return fmt.Errorf("%w (and the status could not be written: %v)", cause, err)
	}
	return cause
}

func rollback(t task.Task, back string) {
	_ = gitx.AmAbort(t.Repo)
	if err := gitx.Switch(t.Repo, back); err == nil {
		_ = gitx.DeleteBranch(t.Repo, t.ID)
	}
}
