package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smate/internal/artifacts"
	"smate/internal/gitx"
	"smate/internal/runtime"
	"smate/internal/store"
	"smate/internal/task"
)

// Start builds the task sandbox: a snapshot of the repository's current branch,
// secrets cut out of it, a throwaway git on top, then the container. The warnings
// are about harness configuration and do not stop the task.
func Start(s *store.Store, repo, id, image string) (t task.Task, warnings []string, err error) {
	if err := validateID(id); err != nil {
		return t, nil, err
	}
	ref, err := imageRef(s, image)
	if err != nil {
		return t, nil, err
	}
	inj, err := harnessInjection(s)
	if err != nil {
		return t, nil, err
	}
	warnings = inj.Warnings
	// As root the container is root too, which is what an agent CLI refuses to work
	// in unattended.
	if os.Getuid() == 0 {
		warnings = append(warnings,
			"smate is running as root, so the container will be root too — an agent CLI will refuse to work unattended in it")
	}
	if !gitx.IsRepo(repo) {
		return t, warnings, fmt.Errorf("%s is not a git repository", repo)
	}
	root, err := gitx.Root(repo)
	if err != nil {
		return t, warnings, err
	}
	branch, err := gitx.CurrentBranch(root)
	if err != nil {
		return t, warnings, err
	}
	sha, err := gitx.HeadSHA(root)
	if err != nil {
		return t, warnings, err
	}
	if s.Exists(id) {
		return t, warnings, fmt.Errorf("task %s already exists (%s)", id, s.TaskDir(id))
	}
	// The artefact directory is ours. If the project already tracks one, our files
	// would land among the project's and travel back as edits to them.
	tracked, err := gitx.TrackedUnder(root, artifacts.Dir)
	if err != nil {
		return t, warnings, err
	}
	if len(tracked) > 0 {
		return t, warnings, fmt.Errorf(
			"%s already tracks %s/ (%d files, e.g. %s) — smate keeps task artefacts there and cannot share it",
			root, artifacts.Dir, len(tracked), tracked[0])
	}

	cfg, _, err := store.LoadConfig(root)
	if err != nil {
		return t, warnings, err
	}
	resolved, err := resolveMounts(cfg.Mounts, root)
	if err != nil {
		return t, warnings, err
	}
	inj.Mounts = append(inj.Mounts, resolved.Bind...)

	cfg.Image = image
	if err := store.SaveConfig(root, cfg); err != nil {
		return t, warnings, err
	}

	// Everything below creates state: on failure the task directory goes, so no
	// half-built sandbox is left without its meta.json.
	defer func() {
		if err != nil {
			_ = os.RemoveAll(s.TaskDir(id))
		}
	}()

	ws := s.Workspace(id)
	if err = os.MkdirAll(ws, 0o700); err != nil {
		return t, warnings, fmt.Errorf("create workspace: %w", err)
	}
	if err = gitx.Archive(root, ws); err != nil {
		return t, warnings, err
	}
	cut, err := cutSecrets(ws, cfg.Secrets)
	if err != nil {
		return t, warnings, err
	}
	copied, err := applyCopies(ws, resolved.Copy)
	if err != nil {
		return t, warnings, err
	}
	if err = os.MkdirAll(artifacts.Root(ws), 0o700); err != nil {
		return t, warnings, fmt.Errorf("create artefact directory: %w", err)
	}
	ignore := append([]string{"/" + artifacts.Dir + "/"}, copied...)
	baseline, err := gitx.InitAndBaseline(ws, ignore)
	if err != nil {
		return t, warnings, err
	}

	t = task.Task{
		ID:        id,
		Repo:      root,
		Branch:    branch,
		BaseSHA:   sha,
		Baseline:  baseline,
		Secrets:   cut,
		Image:     ref,
		Status:    task.StatusActive,
		CreatedAt: time.Now().UTC(),
	}

	abs, err := filepath.Abs(ws)
	if err != nil {
		return t, warnings, fmt.Errorf("resolve absolute workspace path: %w", err)
	}
	if err = runtime.Run(t.Container(), ref, abs, inj.Env, inj.Mounts, inj.Limits); err != nil {
		return t, warnings, err
	}
	if err = s.Save(t); err != nil {
		return t, warnings, err
	}
	return t, warnings, nil
}

// validateID checks that the id works both as a directory and as a branch name.
func validateID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("task <id> is required")
	case id == "." || id == "..":
		return fmt.Errorf("invalid <id>: %s", id)
	case strings.ContainsAny(id, `/\ :~^?*[`):
		return fmt.Errorf(`invalid <id>: %s (no spaces or /\:~^?*[)`, id)
	case strings.HasPrefix(id, "-"):
		return fmt.Errorf("invalid <id>: %s (cannot start with a dash)", id)
	}
	return nil
}
