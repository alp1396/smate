package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"smate/internal/artifacts"
	"smate/internal/runtime"
	"smate/internal/store"
	"smate/internal/task"
)

// RunState is what a run looks like from the outside. There is no daemon to
// record the moment a run ends, so the state is computed on demand from the
// session, the exit code file and the mtime of the log.
type RunState string

const (
	StateNone    RunState = "NOT RUN"  // the task has no runs
	StateWorking RunState = "WORKING"  // session alive, output recent
	StateSleep   RunState = "SLEEP"    // session alive, silent for a while, nobody attached
	StateCutOff  RunState = "CUT OFF"  // session gone, no exit code: OOM, kill, docker rm
	StateFailed  RunState = "FAILED"   // session gone, exit code not 0
	StateDone    RunState = "FINISHED" // session gone, exit code 0
)

// sleepAfter is how long a live session may stay silent before it is flagged.
// Not a timeout: nothing is killed, the mark invites you to attach.
const sleepAfter = time.Minute

type RunInfo struct {
	Meta      RunMeta
	State     RunState
	Attached  bool          // a human is sitting in the session
	Silent    time.Duration // how long the run has printed nothing
	Exit      int           // exit code, valid when the run is FAILED or FINISHED
	HasResult bool          // every declared output exists and is not empty
	Finished  time.Time     // mtime of the exit code file, when there is one
}

func LastRun(s *store.Store, id string) (RunInfo, bool, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return RunInfo{}, false, err
	}
	ws := s.Workspace(t.ID)
	meta, ok, err := lastRunMeta(ws)
	if err != nil || !ok {
		return RunInfo{}, false, err
	}
	r := RunInfo{Meta: meta, Exit: -1, HasResult: hasResult(ws, meta.Outputs)}

	// "Finished" is about the process, "has a result" about the artefact: exit 0
	// with nothing written is the usual outcome of a weak prompt.
	statusPath := artifacts.RunFile(ws, meta.N, artifacts.StatusName)
	code, finished, hasStatus, err := readStatus(statusPath)
	if err != nil {
		return RunInfo{}, false, err
	}

	if runtime.HasSession(t.Container(), Session(meta.N)) {
		r.Attached = runtime.Clients(t.Container(), Session(meta.N)) > 0
		r.Silent = silence(ws, meta)
		if r.Silent > sleepAfter && !r.Attached {
			r.State = StateSleep
		} else {
			r.State = StateWorking
		}
		return r, true, nil
	}

	switch {
	case !hasStatus:
		r.State = StateCutOff
	case code == 0:
		r.State, r.Exit, r.Finished = StateDone, code, finished
	default:
		r.State, r.Exit, r.Finished = StateFailed, code, finished
	}
	return r, true, nil
}

// hasResult wants every declared output present and non-empty: empty is what a run
// that gave up leaves behind, and half the outputs would feed the next role an
// unfinished plan. A run declaring nothing has no result to show.
func hasResult(ws string, outputs []string) bool {
	if len(outputs) == 0 {
		return false
	}
	for _, out := range outputs {
		info, err := os.Stat(artifacts.Path(ws, out))
		if err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}

// silence is how long the run has printed nothing, measured by the mtime
// pipe-pane touches on every byte. A harness that spins a progress indicator
// keeps it moving, so this catches what waits in silence, not everything that
// waits.
func silence(ws string, meta RunMeta) time.Duration {
	info, err := os.Stat(artifacts.RunFile(ws, meta.N, artifacts.LogName))
	if err != nil {
		return time.Since(meta.Started)
	}
	return time.Since(info.ModTime())
}

// readStatus reads the exit code the wrapper wrote. A half-written file is not
// an error: the next call will see it whole.
func readStatus(path string) (code int, at time.Time, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		return 0, time.Time{}, false, nil
	}
	if info, err := os.Stat(path); err == nil {
		at = info.ModTime()
	}
	return code, at, true, nil
}

type TaskView struct {
	Task   task.Task
	Run    RunInfo
	HasRun bool
}

func ListRuns(s *store.Store) ([]TaskView, error) {
	tasks, err := s.List()
	if err != nil {
		return nil, err
	}
	views := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		v := TaskView{Task: t, Run: RunInfo{State: StateNone}}
		if _, err := os.Stat(s.Workspace(t.ID)); err == nil {
			r, ok, err := LastRun(s, t.ID)
			if err != nil {
				return nil, err
			}
			v.Run, v.HasRun = r, ok
			if !ok {
				v.Run = RunInfo{State: StateNone}
			}
		}
		views = append(views, v)
	}
	return views, nil
}

// Logs returns the current screen of the task's live-or-last run. The raw log is
// not shown: a TUI harness paints over itself, and the stream is mostly escape
// sequences.
func Logs(s *store.Store, id string) (string, RunInfo, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return "", RunInfo{}, err
	}
	r, ok, err := LastRun(s, t.ID)
	if err != nil {
		return "", RunInfo{}, err
	}
	if !ok {
		return "", RunInfo{}, fmt.Errorf("task %s has not run a role yet", t.ID)
	}
	screen, err := runtime.CapturePane(t.Container(), Session(r.Meta.N))
	if err != nil {
		return tailLog(s.Workspace(t.ID), r.Meta.N), r, nil
	}
	return screen, r, nil
}

func tailLog(ws string, n int) string {
	data, err := os.ReadFile(artifacts.RunFile(ws, n, artifacts.LogName))
	if err != nil {
		return ""
	}
	const tail = 8 * 1024
	if len(data) > tail {
		data = data[len(data)-tail:]
	}
	return string(data)
}

func Attach(s *store.Store, id string) error {
	cmd, err := AttachCmd(s, id)
	if err != nil {
		return err
	}
	return runtime.RunAttached(cmd)
}

// AttachCmd is what Attach would run, checks and all, left unstarted for a
// caller that must give up the terminal first — the TUI.
func AttachCmd(s *store.Store, id string) (*exec.Cmd, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return nil, err
	}
	r, ok, err := LastRun(s, t.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("task %s has not run a role yet — smate shell %s to work by hand", t.ID, t.ID)
	}
	if r.State != StateWorking && r.State != StateSleep {
		return nil, fmt.Errorf("run %d of task %s is %s — nothing to attach to (smate logs %s)", r.Meta.N, t.ID, r.State, t.ID)
	}
	return runtime.AttachSessionCmd(t.Container(), Session(r.Meta.N)), nil
}

// Stop kills the session of the live run. The task is left alone.
func Stop(s *store.Store, id string) (RunInfo, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return RunInfo{}, err
	}
	r, ok, err := LastRun(s, t.ID)
	if err != nil {
		return RunInfo{}, err
	}
	if !ok {
		return RunInfo{}, fmt.Errorf("task %s has not run a role yet", t.ID)
	}
	if r.State != StateWorking && r.State != StateSleep {
		return r, fmt.Errorf("run %d of task %s is already %s", r.Meta.N, t.ID, r.State)
	}
	if err := runtime.KillSession(t.Container(), Session(r.Meta.N)); err != nil {
		return r, err
	}
	return r, nil
}
