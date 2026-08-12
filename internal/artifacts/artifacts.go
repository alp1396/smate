// Package artifacts owns the layout of the task's artefact directory, the
// .smate/ inside the workspace, through which roles exchange files. It is kept
// out of the throwaway git and out of the patch series.
//
// Paths come in two flavours: host paths for the control plane, and the
// container-side ones written into commands and prompts, where the workspace is
// always /workspace.
package artifacts

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const Dir = ".smate"

const RequestName = "request.md"

// CommandName is the note for the next run. It is consumed when a run starts.
const CommandName = "command.md"

const StatusName = "status"

// LogName is the raw pane stream of a run: forensics, and an mtime that tells
// us the run is still alive.
const LogName = "log"

const ClosedName = "closed"

const MetaName = "run.json"

const ScriptName = "run.sh"

const RoleName = "AGENTS.md"

// Layout of one run, for reference:
//
//	.smate/runs/<n>/run.json     what the run was
//	.smate/runs/<n>/run.sh       the wrapper it was started through
//	.smate/runs/<n>/log          the raw pane stream
//	.smate/runs/<n>/status       the exit code, once it is over
//	.smate/runs/<n>/command.md   the consumed note, if there was one

func Root(workspace string) string { return filepath.Join(workspace, Dir) }

func Path(workspace, name string) string { return filepath.Join(Root(workspace), name) }

func ListMarkdown(workspace string) ([]string, error) {
	entries, err := os.ReadDir(Root(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func RunsRoot(workspace string) string { return filepath.Join(Root(workspace), "runs") }

func RunDir(workspace string, n int) string {
	return filepath.Join(RunsRoot(workspace), strconv.Itoa(n))
}

func RunFile(workspace string, n int, name string) string {
	return filepath.Join(RunDir(workspace, n), name)
}

func RoleDir(workspace, role string) string {
	return filepath.Join(Root(workspace), "roles", role)
}

const ContainerRoot = "/workspace"

// In is a path inside the artefact directory as the container sees it: relative
// to the workspace root, which is where every command we start there runs. This
// is the form that goes into prompts.
func In(elem ...string) string {
	return path.Join(append([]string{Dir}, elem...)...)
}

func RunIn(n int, name string) string {
	return In("runs", strconv.Itoa(n), name)
}

func RunInAbs(n int, name string) string {
	return path.Join(ContainerRoot, RunIn(n, name))
}
