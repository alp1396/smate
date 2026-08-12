package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"smate/internal/artifacts"
	"smate/internal/roles"
)

func artefacts(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.MkdirAll(artifacts.Root(ws), 0o700); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestMissingInputs(t *testing.T) {
	ws := artefacts(t)
	mustWrite(t, artifacts.Path(ws, "request.md"), "do the thing")
	mustWrite(t, artifacts.Path(ws, "coder.result.md"), "")

	for _, inputs := range [][]string{nil, {"request.md"}} {
		missing, err := missingInputs(ws, inputs)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 0 {
			t.Errorf("inputs %v reported missing: %v", inputs, missing)
		}
	}

	// An empty artefact is not a result, so it is not an input either.
	missing, err := missingInputs(ws, []string{"request.md", "coder.result.md", "plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 || missing[0] != "coder.result.md" || missing[1] != "plan.md" {
		t.Errorf("missing = %v, want [coder.result.md plan.md]", missing)
	}
}

func TestConsumeCommand(t *testing.T) {
	ws := artefacts(t)
	if err := os.MkdirAll(artifacts.RunDir(ws, 1), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, artifacts.Path(ws, artifacts.CommandName), "look at the storage layer\n")

	note, err := consumeCommand(ws, 1, "and nothing else")
	if err != nil {
		t.Fatalf("consumeCommand: %v", err)
	}
	if note != ".smate/runs/1/command.md" {
		t.Errorf("note path = %q", note)
	}
	assertMissing(t, artifacts.Path(ws, artifacts.CommandName))
	body, err := os.ReadFile(artifacts.RunFile(ws, 1, artifacts.CommandName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"storage layer", "and nothing else"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the note lost %q: %q", want, body)
		}
	}
}

func TestConsumeCommandWithNothingToSay(t *testing.T) {
	ws := artefacts(t)
	if err := os.MkdirAll(artifacts.RunDir(ws, 1), 0o700); err != nil {
		t.Fatal(err)
	}
	note, err := consumeCommand(ws, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if note != "" {
		t.Errorf("note = %q, want none", note)
	}
	assertMissing(t, artifacts.RunFile(ws, 1, artifacts.CommandName))
}

// A file that is not there is not mentioned: otherwise the agent spends a turn on
// it and starts guessing what it said.
func TestBuildPrompt(t *testing.T) {
	role := roles.Role{Name: "reviewer", Inputs: []string{"request.md"}, Outputs: []string{"reviewer.result.md", "notes.md"}}

	quiet := buildPrompt(role, ".smate/roles/reviewer/AGENTS.md", "")
	if strings.Contains(quiet, "command.md") {
		t.Errorf("a note that does not exist is mentioned: %q", quiet)
	}
	for _, want := range []string{"reviewer", ".smate/roles/reviewer/AGENTS.md", ".smate/request.md", ".smate/reviewer.result.md", ".smate/notes.md"} {
		if !strings.Contains(quiet, want) {
			t.Errorf("prompt does not mention %q: %q", want, quiet)
		}
	}
	loud := buildPrompt(role, ".smate/roles/reviewer/AGENTS.md", ".smate/runs/2/command.md")
	if !strings.Contains(loud, ".smate/runs/2/command.md") {
		t.Errorf("the note is not passed on: %q", loud)
	}
}

// A connected role is pointed at the same files but told the opposite thing at
// the end: read yourself in, then wait for the human who opened you.
func TestBuildConnectPrompt(t *testing.T) {
	role := roles.Role{Name: "reviewer", Inputs: []string{"coder.result.md"}, Outputs: []string{"reviewer.result.md"}}
	prompt := buildConnectPrompt(role, ".smate/roles/reviewer/AGENTS.md", "")

	for _, want := range []string{"reviewer", ".smate/roles/reviewer/AGENTS.md", ".smate/coder.result.md", ".smate/reviewer.result.md", "wait"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("connect prompt does not mention %q: %q", want, prompt)
		}
	}
	// The run's ending — nobody is watching — is the one thing it must not say.
	if strings.Contains(prompt, "Nobody is watching") {
		t.Errorf("connect prompt tells the agent it is alone: %q", prompt)
	}
	if run := buildPrompt(role, ".smate/roles/reviewer/AGENTS.md", ""); run == prompt {
		t.Error("connect and run are given the same prompt")
	}
}

// The prompt is a sentence from a human and travels through a shell. It must
// arrive as one argument, quotes and all.
func TestWriteScriptQuotesThePrompt(t *testing.T) {
	ws := artefacts(t)
	if err := os.MkdirAll(artifacts.RunDir(ws, 3), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeScript(ws, 3, "claude --model opus", "don't `touch` $HOME; rm -rf /"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(artifacts.RunFile(ws, 3, artifacts.ScriptName))
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if !strings.Contains(script, `'don'\''t `+"`touch`"+` $HOME; rm -rf /'`) {
		t.Errorf("the prompt was not quoted as one argument:\n%s", script)
	}
	if !strings.Contains(script, "claude --model opus '") {
		t.Errorf("the role command is missing:\n%s", script)
	}
	if !strings.Contains(script, "echo $? > .smate/runs/3/status") {
		t.Errorf("the exit code is not recorded:\n%s", script)
	}
}

func TestRunNumbering(t *testing.T) {
	ws := artefacts(t)
	n, err := nextRun(ws)
	if err != nil || n != 1 {
		t.Fatalf("first run = %d (%v), want 1", n, err)
	}
	for _, name := range []string{"1", "2", "10", "notarun"} {
		if err := os.MkdirAll(filepath.Join(artifacts.RunsRoot(ws), name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	n, err = nextRun(ws)
	if err != nil || n != 11 {
		t.Fatalf("next run = %d (%v), want 11", n, err)
	}

	// The state is computed from the file written at start, not from role.yml as
	// it looks now.
	if err := writeRunMeta(ws, RunMeta{N: 10, Role: "coder", Outputs: []string{"coder.result.md"}}); err != nil {
		t.Fatal(err)
	}
	m, ok, err := lastRunMeta(ws)
	if err != nil || !ok {
		t.Fatalf("lastRun: ok=%v err=%v", ok, err)
	}
	if m.N != 10 || m.Role != "coder" {
		t.Errorf("lastRun = %+v", m)
	}
}

// Only the refusals are tested: they are decided before docker is asked anything.
func TestCloseStaleRefusesWhatIsNotDone(t *testing.T) {
	ws := artefacts(t)
	if err := os.MkdirAll(artifacts.RunDir(ws, 1), 0o700); err != nil {
		t.Fatal(err)
	}
	prev := RunMeta{N: 1, Role: "coder", Outputs: []string{"coder.result.md"}, Started: time.Now().Add(-time.Hour)}

	// Silent for an hour with nothing written: stuck, not finished.
	closed, err := closeStale("smate-test", ws, prev)
	if err != nil || closed {
		t.Errorf("closed a run without a result: %v (%v)", closed, err)
	}

	// Result written but still printing: it may be doing something after the
	// report.
	mustWrite(t, artifacts.Path(ws, prev.Outputs[0]), "what I did\n")
	mustWrite(t, artifacts.RunFile(ws, 1, artifacts.LogName), "still going\n")
	closed, err = closeStale("smate-test", ws, prev)
	if err != nil || closed {
		t.Errorf("closed a run that was still printing: %v (%v)", closed, err)
	}
}

func TestSessionName(t *testing.T) {
	if got := Session(7); got != "run-7" {
		t.Errorf("Session(7) = %q", got)
	}
}
