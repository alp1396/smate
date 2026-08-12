package core

import (
	"fmt"
	"os/exec"
	"strings"

	"smate/internal/runtime"
	"smate/internal/store"
	"smate/internal/task"
)

func Shell(s *store.Store, id string) error {
	cmd, err := ShellCmd(s, id)
	if err != nil {
		return err
	}
	return runtime.RunAttached(cmd)
}

// ShellCmd is what Shell would run, checks and all, left unstarted for a caller
// that must give up the terminal first — the TUI.
func ShellCmd(s *store.Store, id string) (*exec.Cmd, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return nil, err
	}
	if !runtime.Running(t.Container()) {
		return nil, fmt.Errorf("container of task %s is not running", t.ID)
	}
	return runtime.ShellCmd(t.Container()), nil
}

func Resolve(s *store.Store, id string) (task.Task, error) {
	if id != "" {
		return s.Load(id)
	}
	tasks, err := s.List()
	if err != nil {
		return task.Task{}, err
	}
	var active []task.Task
	for _, t := range tasks {
		if t.Status == task.StatusActive {
			active = append(active, t)
		}
	}
	switch len(active) {
	case 1:
		return active[0], nil
	case 0:
		return task.Task{}, fmt.Errorf("no active tasks — pass an <id>")
	default:
		var ids []string
		for _, t := range active {
			ids = append(ids, t.ID)
		}
		return task.Task{}, fmt.Errorf("several active tasks (%s) — pass an <id>", strings.Join(ids, ", "))
	}
}
