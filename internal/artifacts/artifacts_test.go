package artifacts

import (
	"path/filepath"
	"testing"
)

func TestHostPaths(t *testing.T) {
	ws := filepath.Join("tmp", "task", "workspace")
	cases := []struct{ got, want string }{
		{Root(ws), filepath.Join(ws, ".smate")},
		{Path(ws, RequestName), filepath.Join(ws, ".smate", "request.md")},
		{RunDir(ws, 3), filepath.Join(ws, ".smate", "runs", "3")},
		{RunFile(ws, 3, StatusName), filepath.Join(ws, ".smate", "runs", "3", "status")},
		{RoleDir(ws, "coder"), filepath.Join(ws, ".smate", "roles", "coder")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// Container-side paths are what goes into prompts and commands: slashes, and
// relative to the workspace unless the working directory is not ours to assume.
func TestContainerPaths(t *testing.T) {
	if got := In(RequestName); got != ".smate/request.md" {
		t.Errorf("In = %q", got)
	}
	if got := In("roles", "coder", RoleName); got != ".smate/roles/coder/AGENTS.md" {
		t.Errorf("In = %q", got)
	}
	if got := RunIn(2, LogName); got != ".smate/runs/2/log" {
		t.Errorf("RunIn = %q", got)
	}
	if got := RunInAbs(2, LogName); got != "/workspace/.smate/runs/2/log" {
		t.Errorf("RunInAbs = %q", got)
	}
}
