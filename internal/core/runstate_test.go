package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"smate/internal/artifacts"
)

func TestReadStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, artifacts.StatusName)

	// No file: the wrapper never got to write it. That is CUT OFF, not zero.
	if _, _, ok, err := readStatus(path); ok || err != nil {
		t.Errorf("missing status: ok=%v err=%v", ok, err)
	}

	mustWrite(t, path, "0\n")
	code, at, ok, err := readStatus(path)
	if err != nil || !ok || code != 0 {
		t.Errorf("exit 0: code=%d ok=%v err=%v", code, ok, err)
	}
	if at.IsZero() {
		t.Error("the finish time was not read")
	}

	mustWrite(t, path, "137\n")
	if code, _, ok, _ := readStatus(path); !ok || code != 137 {
		t.Errorf("exit 137: code=%d ok=%v", code, ok)
	}

	// Caught mid-write: not an error, the next look will see it whole.
	mustWrite(t, path, "")
	if _, _, ok, err := readStatus(path); ok || err != nil {
		t.Errorf("half-written status: ok=%v err=%v", ok, err)
	}
}

// Activity is the mtime of the raw log, which tmux touches on every byte the
// pane prints. Before the log exists, the start time stands in for it.
func TestSilence(t *testing.T) {
	ws := t.TempDir()
	meta := RunMeta{N: 1, Started: time.Now().Add(-10 * time.Minute)}
	if err := os.MkdirAll(artifacts.RunDir(ws, 1), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := silence(ws, meta); got < 9*time.Minute {
		t.Errorf("without a log the silence is measured from the start: %s", got)
	}

	log := artifacts.RunFile(ws, 1, artifacts.LogName)
	mustWrite(t, log, "output")
	if got := silence(ws, meta); got > time.Minute {
		t.Errorf("a fresh log means the run is alive: %s", got)
	}

	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(log, old, old); err != nil {
		t.Fatal(err)
	}
	if got := silence(ws, meta); got < sleepAfter {
		t.Errorf("a stale log should read as silence: %s", got)
	}
}

// A run that created its output and never wrote to it has not produced a result:
// this is the bar both the listing and the next role's inputs are held to.
func TestHasResult(t *testing.T) {
	ws := artefacts(t)
	one := []string{"coder.result.md"}
	if hasResult(ws, one) {
		t.Error("a missing artefact counts as a result")
	}
	mustWrite(t, artifacts.Path(ws, "coder.result.md"), "")
	if hasResult(ws, one) {
		t.Error("an empty artefact counts as a result")
	}
	mustWrite(t, artifacts.Path(ws, "coder.result.md"), "what I did\n")
	if !hasResult(ws, one) {
		t.Error("a written artefact does not count as a result")
	}
}

// Every declared output has to be there: half a result would let the next role
// read a plan that was never finished.
func TestHasResultWantsAllOutputs(t *testing.T) {
	ws := artefacts(t)
	both := []string{"task.md", "plan.md"}

	mustWrite(t, artifacts.Path(ws, "task.md"), "the task\n")
	if hasResult(ws, both) {
		t.Error("one output out of two counts as a result")
	}
	mustWrite(t, artifacts.Path(ws, "plan.md"), "the plan\n")
	if !hasResult(ws, both) {
		t.Error("both outputs written does not count as a result")
	}

	// Meta from before outputs were a list decodes to nothing declared. That is
	// not a run that quietly succeeded.
	if hasResult(ws, nil) {
		t.Error("a run that declares no outputs counts as a result")
	}
}

func TestTailLogReturnsTheEnd(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(artifacts.RunDir(ws, 1), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := tailLog(ws, 1); got != "" {
		t.Errorf("no log should read as empty, got %q", got)
	}
	body := make([]byte, 9*1024)
	for i := range body {
		body[i] = 'x'
	}
	copy(body[len(body)-4:], "tail")
	mustWrite(t, artifacts.RunFile(ws, 1, artifacts.LogName), string(body))

	got := tailLog(ws, 1)
	if len(got) != 8*1024 {
		t.Errorf("tail length = %d, want 8192", len(got))
	}
	if got[len(got)-4:] != "tail" {
		t.Errorf("the tail is not the end of the file: %q", got[len(got)-4:])
	}
}
