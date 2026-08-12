package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"smate/internal/artifacts"
	"smate/internal/roles"
	"smate/internal/runtime"
	"smate/internal/store"
)

// RunMeta is what a run was, written once at start: role.yml may be edited
// later, and what a finished run is judged by must not change underneath it.
type RunMeta struct {
	N       int       `json:"n"`
	Role    string    `json:"role"`
	Cmd     string    `json:"cmd"`
	Outputs []string  `json:"outputs"`
	Started time.Time `json:"started"`
}

// Session is the tmux session name of run n.
func Session(n int) string { return "run-" + strconv.Itoa(n) }

// Run starts a role in the task container and returns immediately. The run sits
// in a tmux session, so it outlives the call and can be attached to later.
func Run(s *store.Store, id, roleName, message string, force bool) (RunMeta, []string, error) {
	meta, warnings, _, err := startRole(s, id, roleName, message, force, false)
	return meta, warnings, err
}

// Connect prepares a role exactly as Run does and returns the command that hands
// this terminal to it, unstarted — the caller may have to give up the terminal
// first. The agent is told to read its role and wait: the human drives.
func Connect(s *store.Store, id, roleName, message string) (RunMeta, []string, *exec.Cmd, error) {
	return startRole(s, id, roleName, message, false, true)
}

// startRole is the preparation both Run and Connect are made of. connect changes
// three things — the prompt, whether a missing input refuses, whether the
// outputs are cleared — and nothing else: a connected role must be the same
// performer as a run of it.
func startRole(s *store.Store, id, roleName, message string, force, connect bool) (RunMeta, []string, *exec.Cmd, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}
	role, err := LoadRole(s, roleName)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}
	ws := s.Workspace(t.ID)
	container := t.Container()
	var warnings []string

	// The preflight order matters: the outputs are deleted last (step 7), so a run
	// that trips over something trivial has not destroyed the previous result.

	// 1. Inputs. A note or a human at the terminal replaces them: both are a
	//    statement of the task in themselves.
	missing, err := missingInputs(ws, role.Inputs)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}
	if len(missing) > 0 {
		absent := strings.Join(missing, ", ")
		switch {
		case connect:
			warnings = append(warnings, fmt.Sprintf("%s is missing — the role is yours to steer", absent))
		case message != "":
			warnings = append(warnings, fmt.Sprintf("%s is missing — going by your note instead", absent))
		case force:
			warnings = append(warnings, fmt.Sprintf("%s is missing — started anyway (--force)", absent))
		default:
			return RunMeta{}, nil, nil, fmt.Errorf(
				"%s is missing from %s/ — run the role that produces it, pass -m with what to do, or --force",
				absent, artifacts.Dir)
		}
	}

	if !runtime.Running(container) {
		return RunMeta{}, nil, nil, fmt.Errorf("container of task %s is not running (smate start it again or use another task)", t.ID)
	}
	if !runtime.HasTmux(container) {
		return RunMeta{}, nil, nil, fmt.Errorf(
			"image %s has no tmux, and a detached run needs it — rebuild the environment: smate build base && smate build <stack>",
			t.Image)
	}
	// 3. One run at a time: two roles in one workspace overwrite each other's
	//    artefacts.
	prev, hasPrev, err := lastRunMeta(ws)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}
	if hasPrev && runtime.HasSession(container, Session(prev.N)) {
		closed, err := closeStale(container, ws, prev)
		if err != nil {
			return RunMeta{}, nil, nil, err
		}
		if !closed {
			return RunMeta{}, nil, nil, fmt.Errorf(
				"run %d (%s) is still alive — smate attach %s to see it, or smate stop %s",
				prev.N, prev.Role, t.ID, t.ID)
		}
		warnings = append(warnings, fmt.Sprintf(
			"run %d (%s) was still sitting in its session with %s written — closed it",
			prev.N, prev.Role, strings.Join(prev.Outputs, ", ")))
	}

	n, err := nextRun(ws)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}
	if err := os.MkdirAll(artifacts.RunDir(ws, n), 0o700); err != nil {
		return RunMeta{}, nil, nil, fmt.Errorf("create run directory: %w", err)
	}

	// 5. The instructions, copied rather than referenced: what the role went by
	//    stays visible after the library changes.
	instructions, err := copyInstructions(s, ws, role.Name)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}

	// 6. The note, consumed: one written for the coder must not reach the
	//    reviewer an hour later and be carried out.
	note, err := consumeCommand(ws, n, message)
	if err != nil {
		return RunMeta{}, nil, nil, err
	}

	// 7. The outputs, all of them, so a failed run cannot pass yesterday's
	//    artefacts off as its result. A connected role keeps them: opening a role
	//    is also how one goes in to read the last report and ask about it.
	if !connect {
		for _, out := range role.Outputs {
			if err := os.RemoveAll(artifacts.Path(ws, out)); err != nil {
				return RunMeta{}, nil, nil, fmt.Errorf("clear the previous %s: %w", out, err)
			}
		}
	}

	meta := RunMeta{N: n, Role: role.Name, Cmd: role.Cmd, Outputs: role.Outputs, Started: time.Now().UTC()}
	if err := writeRunMeta(ws, meta); err != nil {
		return RunMeta{}, nil, nil, err
	}
	prompt := buildPrompt(role, instructions, note)
	if connect {
		prompt = buildConnectPrompt(role, instructions, note)
	}
	if err := writeScript(ws, n, role.Cmd, prompt); err != nil {
		return RunMeta{}, nil, nil, err
	}

	// 8. The session, and the pipe whose mtime is what liveness is judged by.
	//    Connect starts detached here too and attaches after, so closing the
	//    terminal does not kill the agent.
	if err := runtime.StartSession(container, Session(n), "sh "+artifacts.RunInAbs(n, artifacts.ScriptName)); err != nil {
		return RunMeta{}, nil, nil, err
	}
	if err := runtime.PipePane(container, Session(n), artifacts.RunInAbs(n, artifacts.LogName)); err != nil {
		return RunMeta{}, nil, nil, err
	}
	if !connect {
		return meta, warnings, nil, nil
	}
	return meta, warnings, runtime.AttachSessionCmd(container, Session(n)), nil
}

// closeStale puts down a previous run that has plainly finished and is only
// sitting in its session waiting to be talked to; reports whether it did.
//
// An interactive harness never exits on its own, so without this it would block
// the next role forever. No exit code is written on its behalf — it was never
// waited for — only a note saying why it was closed.
func closeStale(container, ws string, prev RunMeta) (bool, error) {
	switch {
	case !hasResult(ws, prev.Outputs):
		return false, nil
	case silence(ws, prev) <= sleepAfter:
		return false, nil
	case runtime.Clients(container, Session(prev.N)) > 0:
		return false, nil
	}
	if err := runtime.KillSession(container, Session(prev.N)); err != nil {
		return false, err
	}
	note := fmt.Sprintf("closed on the next run: %s was written and the session had been silent\n", strings.Join(prev.Outputs, ", "))
	path := artifacts.RunFile(ws, prev.N, artifacts.ClosedName)
	if err := os.WriteFile(path, []byte(note), 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func MissingInputs(s *store.Store, id string, rs []roles.Role) (map[string][]string, error) {
	t, err := Resolve(s, id)
	if err != nil {
		return nil, err
	}
	ws := s.Workspace(t.ID)
	out := map[string][]string{}
	for _, r := range rs {
		missing, err := missingInputs(ws, r.Inputs)
		if err != nil {
			return nil, err
		}
		if len(missing) > 0 {
			out[r.Name] = missing
		}
	}
	return out, nil
}

// An empty file counts as missing, the same bar a run's own result is held to.
func missingInputs(ws string, inputs []string) ([]string, error) {
	var missing []string
	for _, in := range inputs {
		info, err := os.Stat(artifacts.Path(ws, in))
		switch {
		case errors.Is(err, fs.ErrNotExist), err == nil && info.Size() == 0:
			missing = append(missing, in)
		case err != nil:
			return nil, fmt.Errorf("check input %s: %w", in, err)
		}
	}
	return missing, nil
}

func copyInstructions(s *store.Store, ws, role string) (string, error) {
	src := roles.AgentsPath(s.RolesDir(), role)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read the instructions of role %s: %w", role, err)
	}
	dir := artifacts.RoleDir(ws, role)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, artifacts.RoleName), data, 0o600); err != nil {
		return "", fmt.Errorf("copy the instructions of role %s: %w", role, err)
	}
	return artifacts.In("roles", role, artifacts.RoleName), nil
}

func consumeCommand(ws string, n int, message string) (string, error) {
	pending := artifacts.Path(ws, artifacts.CommandName)
	data, err := os.ReadFile(pending)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", artifacts.CommandName, err)
	}
	body := strings.TrimRight(string(data), "\n")
	if message != "" {
		if body != "" {
			body += "\n\n"
		}
		body += message
	}
	if strings.TrimSpace(body) == "" {
		return "", nil
	}
	dst := artifacts.RunFile(ws, n, artifacts.CommandName)
	if err := os.WriteFile(dst, []byte(body+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", dst, err)
	}
	if err := os.Remove(pending); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("consume %s: %w", pending, err)
	}
	return artifacts.RunIn(n, artifacts.CommandName), nil
}

func buildPrompt(role roles.Role, instructions, note string) string {
	return promptHead(role, instructions, note) + fmt.Sprintf(
		"Write your result to %s when you are done — every one of them, or the run does not count as having produced anything. Nobody is watching the terminal.",
		outputList(role))
}

func buildConnectPrompt(role roles.Role, instructions, note string) string {
	return promptHead(role, instructions, note) + fmt.Sprintf(
		"Then stop and wait: a human is at this terminal and will tell you what to do. Say in one line that you have read your role, and do not start working before they ask. When they do ask, and the work is done, write your result to %s.",
		outputList(role))
}

// promptHead is what both prompts open with. A note is mentioned only when there
// is one: otherwise the agent hunts for a file that does not exist.
func promptHead(role roles.Role, instructions, note string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are running as the %s role. Read %s first — it describes your role.\n", role.Name, instructions)
	if note != "" {
		fmt.Fprintf(&b, "Read %s next: it is the human's note for this run and takes precedence.\n", note)
	}
	if len(role.Inputs) > 0 {
		ins := make([]string, 0, len(role.Inputs))
		for _, in := range role.Inputs {
			ins = append(ins, artifacts.In(in))
		}
		fmt.Fprintf(&b, "Your inputs: %s\n", strings.Join(ins, ", "))
	}
	return b.String()
}

func outputList(role roles.Role) string {
	outs := make([]string, 0, len(role.Outputs))
	for _, out := range role.Outputs {
		outs = append(outs, artifacts.In(out))
	}
	return strings.Join(outs, ", ")
}

// writeScript writes the wrapper the run is started through. It records the
// exit code because docker exec -d does not report it; whether the run produced
// a result is still decided outside, by the artefact. A file rather than sh -c
// keeps the quoting to one round through tmux.
func writeScript(ws string, n int, cmd, prompt string) error {
	body := fmt.Sprintf(`#!/bin/sh
# Written by smate for run %d. The exit code goes into %s.
cd /workspace || exit 1
%s %s
echo $? > %s
`, n, artifacts.StatusName, cmd, runtime.ShellQuote(prompt), artifacts.RunIn(n, artifacts.StatusName))
	path := artifacts.RunFile(ws, n, artifacts.ScriptName)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeRunMeta(ws string, m RunMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run meta: %w", err)
	}
	path := artifacts.RunFile(ws, m.N, artifacts.MetaName)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readRunMeta(ws string, n int) (RunMeta, error) {
	path := artifacts.RunFile(ws, n, artifacts.MetaName)
	data, err := os.ReadFile(path)
	if err != nil {
		return RunMeta{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m RunMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return RunMeta{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

func runNumbers(ws string) ([]int, error) {
	entries, err := os.ReadDir(artifacts.RunsRoot(ws))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the runs of the task: %w", err)
	}
	var ns []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(e.Name()); err == nil {
			ns = append(ns, n)
		}
	}
	sort.Ints(ns)
	return ns, nil
}

func nextRun(ws string) (int, error) {
	ns, err := runNumbers(ws)
	if err != nil {
		return 0, err
	}
	if len(ns) == 0 {
		return 1, nil
	}
	return ns[len(ns)-1] + 1, nil
}

func lastRunMeta(ws string) (RunMeta, bool, error) {
	ns, err := runNumbers(ws)
	if err != nil || len(ns) == 0 {
		return RunMeta{}, false, err
	}
	m, err := readRunMeta(ws, ns[len(ns)-1])
	if err != nil {
		return RunMeta{}, false, err
	}
	return m, true, nil
}
