package runtime

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// A run lives inside a tmux session in the task container. tmux is what gives the
// agent a PTY that outlives the call that started it: `docker exec -it` always
// starts a new process, and `docker attach` only reaches PID 1, so neither can
// bring a human back to a running agent.

func HasTmux(container string) bool {
	return exec.Command("docker", "exec", container, "sh", "-c", "command -v tmux").Run() == nil
}

// StartSession starts a detached tmux session running one shell command.
//
// -u forces UTF-8 regardless of the locale: this is where the tmux server is born,
// and get it wrong here and every glyph the agent draws is an underscore in the
// buffer forever.
func StartSession(container, session, command string) error {
	return tmux(container, "-u", "new-session", "-d", "-s", session, command)
}

// PipePane copies everything the pane prints into a file in the container: raw
// forensics, and an mtime that is the cheapest sign the run is alive.
func PipePane(container, session, path string) error {
	return tmux(container, "pipe-pane", "-o", "-t", session, "cat >> "+path)
}

func HasSession(container, session string) bool {
	return tmux(container, "has-session", "-t", session) == nil
}

func CapturePane(container, session string) (string, error) {
	out, err := output(container, "capture-pane", "-p", "-t", session)
	if err != nil {
		return "", err
	}
	return out, nil
}

func Clients(container, session string) int {
	out, err := output(container, "list-clients", "-t", session)
	if err != nil || strings.TrimSpace(out) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(out), "\n"))
}

func KillSession(container, session string) error {
	return tmux(container, "kill-session", "-t", session)
}

func AttachSessionCmd(container, session string) *exec.Cmd {
	return exec.Command("docker", "exec", "-it", container, "tmux", "-u", "attach", "-t", session)
}

func tmux(container string, args ...string) error {
	full := append([]string{"exec", container, "tmux"}, args...)
	cmd := exec.Command("docker", full...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("tmux %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

func output(container string, args ...string) (string, error) {
	full := append([]string{"exec", container, "tmux"}, args...)
	cmd := exec.Command("docker", full...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// ShellQuote wraps a string so /bin/sh reads it as one literal argument: the
// prompt is a sentence and must not turn into shell syntax.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
