package core

import (
	"fmt"
	"os"
	"path/filepath"

	"smate/internal/runtime"
	"smate/internal/store"
	"smate/internal/task"
)

// Restart gives an existing task a new container over the workspace it already
// has. The sandbox is the workspace, not the container: /workspace is a host
// directory, so a container killed from outside — or one whose memory cap turned
// out too low — costs the environment and never the work.
//
// Everything that builds the snapshot (archive, secrets, baseline) is
// deliberately not repeated: repeating it would overwrite what the agent wrote.
// What is re-read is the container's shape — limits, harness env, mounts — which
// is the point of the command: it is how a raised limits.memory reaches a task
// that was killed for hitting the old one.
func Restart(s *store.Store, id string) (t task.Task, warnings []string, err error) {
	t, err = Resolve(s, id)
	if err != nil {
		return t, nil, err
	}
	if t.Status == task.StatusCleaned {
		return t, nil, fmt.Errorf("task %s is cleaned — its workspace is gone, start a new task", t.ID)
	}
	ws := s.Workspace(t.ID)
	if _, err := os.Stat(ws); err != nil {
		return t, nil, fmt.Errorf("workspace of task %s is gone (%s) — nothing to restart into", t.ID, ws)
	}
	if runtime.Running(t.Container()) {
		return t, nil, fmt.Errorf("container of task %s is already running — smate shell %s to get in", t.ID, t.ID)
	}
	// The image is the one recorded at start: the same snapshot in a different
	// environment is a different task, and --image on restart would hide that.
	if _, built := runtime.InspectImage(t.Image); !built {
		return t, nil, fmt.Errorf("image %s of task %s is gone — build it again (smate build <name>)", t.Image, t.ID)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		return t, nil, err
	}
	warnings = inj.Warnings
	cfg, _, err := store.LoadConfig(t.Repo)
	if err != nil {
		return t, warnings, err
	}
	resolved, err := resolveMounts(cfg.Mounts, t.Repo)
	if err != nil {
		return t, warnings, err
	}
	inj.Mounts = append(inj.Mounts, resolved.Bind...)
	// Copied mounts are not copied again: they were placed in the snapshot at
	// start and may have been edited since, and a mount is not worth silently
	// undoing an hour of work.
	if len(resolved.Copy) > 0 {
		warnings = append(warnings,
			"mounts copied under /workspace are left as they are in the workspace, not copied again")
	}

	// A stopped container still owns the name, and docker run would refuse it.
	if err := runtime.Remove(t.Container()); err != nil {
		return t, warnings, err
	}
	abs, err := filepath.Abs(ws)
	if err != nil {
		return t, warnings, fmt.Errorf("resolve absolute workspace path: %w", err)
	}
	if err := runtime.Run(t.Container(), t.Image, abs, inj.Env, inj.Mounts, inj.Limits); err != nil {
		return t, warnings, err
	}
	return t, warnings, nil
}
