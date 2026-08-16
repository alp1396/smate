package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"smate/internal/store"
)

// OpenIDECmd is the editor opened on the task's workspace, checks and all, left
// unstarted: the warnings that come with it are about editing a file while a run
// writes to it, and they are worth reading before the editor is on screen rather
// than after it closes.
//
// The workspace is an ordinary host directory with the sandbox's own throwaway
// git in it, so the editor's git panel shows exactly the diff against baseline
// that apply turns into a patch — no extra machinery is needed to see what
// changed. What the editor cannot show is the environment: the toolchain and the
// installed dependencies live in the container, so a language server here works
// off whatever the host happens to have.
func OpenIDECmd(s *store.Store, id string) (*exec.Cmd, []string, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return nil, nil, err
	}
	ws := s.Workspace(t.ID)
	if _, err := os.Stat(ws); err != nil {
		return nil, nil, fmt.Errorf("task %s has no workspace to open (cleaned tasks keep no files)", t.ID)
	}
	editor, err := resolveEditor(s)
	if err != nil {
		return nil, nil, err
	}

	// A live run writes into the same files from inside the container. Nothing is
	// blocked — sometimes fixing a file under a stuck agent is the point — but the
	// race is worth saying out loud.
	var warnings []string
	if r, ok, err := LastRun(s, t.ID); err == nil && ok {
		if r.State == StateWorking || r.State == StateSleep {
			warnings = append(warnings, fmt.Sprintf(
				"run %d (%s) is %s — your edits and its writes go to the same files",
				r.Meta.N, r.Meta.Role, r.State))
		}
	}

	args := append(append([]string{}, editor[1:]...), ws)
	return exec.Command(editor[0], args...), warnings, nil
}

// resolveEditor returns the editor command line, split into program and
// arguments: a configured editor is regularly `code -n` or `emacsclient -c`, and
// $EDITOR carries flags just as often.
func resolveEditor(s *store.Store) ([]string, error) {
	g, err := s.LoadGlobal()
	if err != nil {
		return nil, err
	}
	for _, candidate := range []string{g.Editor, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if fields := strings.Fields(candidate); len(fields) > 0 {
			return fields, nil
		}
	}
	// The last resort rather than the default: found on the machine, so it opens
	// something real, but never preferred over what the user said.
	if _, err := exec.LookPath("code"); err == nil {
		return []string{"code"}, nil
	}
	return nil, fmt.Errorf("no editor to open with — put `editor: <command>` in %s, or set $EDITOR", s.ConfigPath())
}
