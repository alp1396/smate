package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"smate/internal/core"
	"smate/internal/roles"
	"smate/internal/task"
)

// These tests drive the model directly: what is worth pinning down is which key
// produces which command, and that the irreversible three produce none on their
// own.

func testActions(t *testing.T, status task.Status, run core.RunInfo, hasRun bool) actionsModel {
	t.Helper()
	a := newActionsModel(task.Task{ID: "123", Repo: "/repo", Status: status}, 80)
	a.roles = []roles.Role{
		{Name: "planner", Outputs: []string{"task.md", "plan.md"}},
		{Name: "coder", Inputs: []string{"task.md"}, Outputs: []string{"coder.result.md"}},
	}
	a.harnesses = []core.HarnessInfo{
		{Name: "claude", Cmd: "claude"},
		{Name: "codex", Cmd: "codex", Missing: []string{"OPENAI_API_KEY"}},
	}
	a.run, a.hasRun, a.loaded = run, hasRun, true
	return a.settle()
}

func labels(rows []row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.sep {
			out = append(out, "──")
			continue
		}
		out = append(out, r.label)
	}
	return out
}

func rowNamed(t *testing.T, a actionsModel, label string) (row, int) {
	t.Helper()
	for i, r := range a.rows() {
		if r.label == label {
			return r, i
		}
	}
	t.Fatalf("no row %q in %v", label, labels(a.rows()))
	return row{}, -1
}

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestMenuShape(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	want := []string{
		"Apply changes", "Connect shell", "Open claude", "Open codex",
		"──", "planner", "coder", "──", "Clean",
	}
	if got := labels(a.rows()); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("menu = %v, want %v", got, want)
	}
	if a.cursor != 0 {
		t.Errorf("cursor starts at %d", a.cursor)
	}
}

func TestCursorStepsOverSeparators(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	for i := 0; i < len(a.rows())+2; i++ {
		a = a.move(1)
		if a.rows()[a.cursor].sep {
			t.Fatalf("cursor stopped on a separator at %d", a.cursor)
		}
	}
	if got := a.rows()[a.cursor].label; got != "Clean" {
		t.Errorf("moving down ends on %q, want Clean", got)
	}
	for i := 0; i < len(a.rows())+2; i++ {
		a = a.move(-1)
		if a.rows()[a.cursor].sep {
			t.Fatalf("cursor stopped on a separator at %d", a.cursor)
		}
	}
	if got := a.rows()[a.cursor].label; got != "Apply changes" {
		t.Errorf("moving up ends on %q, want Apply changes", got)
	}
}

func TestIrreversibleActionsAskFirst(t *testing.T) {
	for _, label := range []string{"Apply changes", "Clean"} {
		t.Run(label, func(t *testing.T) {
			a := testActions(t, task.StatusActive, core.RunInfo{}, false)
			_, i := rowNamed(t, a, label)
			a.cursor = i

			a, cmd, handled := a.enter(nil)
			if !handled {
				t.Fatal("enter was not handled")
			}
			if cmd != nil {
				t.Fatal("the action ran without being confirmed")
			}
			if a.mode != modeConfirm {
				t.Fatalf("mode = %v, want a confirm", a.mode)
			}

			back, cmd, _ := a.updateConfirm(key("n"), nil)
			if cmd != nil {
				t.Error("backing out ran the action anyway")
			}
			if back.mode != modeMenu {
				t.Errorf("mode after backing out = %v", back.mode)
			}
			if back.status != "cancelled" {
				t.Errorf("status after backing out = %q", back.status)
			}
		})
	}
}

// Attach and Stop mean nothing while the role is idle, and a row that means
// nothing says so rather than failing later in core.
func TestAttachAndStopFollowTheRun(t *testing.T) {
	idle := testActions(t, task.StatusActive, core.RunInfo{}, false)
	idle.mode, idle.role = modeRole, "coder"
	for _, label := range []string{"Attach", "Stop"} {
		r, i := rowNamed(t, idle, label)
		if r.off == "" {
			t.Errorf("%s is live while nothing is running", label)
		}
		idle.roleCursor = i
		after, cmd, _ := idle.enter(nil)
		if cmd != nil {
			t.Errorf("%s ran while nothing is running", label)
		}
		if !after.failed || !strings.Contains(after.status, "not running") {
			t.Errorf("%s did not say why it is inert: %q", label, after.status)
		}
	}

	live := testActions(t, task.StatusActive,
		core.RunInfo{Meta: core.RunMeta{N: 4, Role: "coder"}, State: core.StateWorking}, true)
	live.mode, live.role = modeRole, "coder"
	for _, label := range []string{"Attach", "Stop"} {
		if r, _ := rowNamed(t, live, label); r.off != "" {
			t.Errorf("%s is inert while the role is running: %s", label, r.off)
		}
	}
	live.role = "planner"
	if r, _ := rowNamed(t, live, "Attach"); r.off == "" {
		t.Error("attach offered for a role that is not the running one")
	}
}

// Connect is first, and unlike Attach and Stop it works from a standing start: it
// is what creates the run it hands you.
func TestConnectOpensARoleThatIsNotRunning(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	a.mode, a.role = modeRole, "coder"

	if got := labels(a.rows())[0]; got != "Connect" {
		t.Errorf("first role action = %q, want Connect", got)
	}
	r, i := rowNamed(t, a, "Connect")
	if r.off != "" {
		t.Errorf("Connect is inert while the role is idle: %s", r.off)
	}
	a.roleCursor = i

	after, cmd, handled := a.enter(nil)
	if !handled {
		t.Fatal("enter was not handled")
	}
	if cmd == nil {
		t.Fatal("Connect produced no command")
	}
	if after.mode != modeRole {
		t.Errorf("mode after Connect = %v, want the role screen", after.mode)
	}
	if after.status != "connecting…" {
		t.Errorf("status after Connect = %q", after.status)
	}
}

// core.Run refuses a role whose inputs are not there, so the row refuses first —
// and its two neighbours do not.
func TestRunWaitsForItsInputs(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	a.missing = map[string][]string{"coder": {"task.md"}}
	a.mode, a.role = modeRole, "coder"

	r, i := rowNamed(t, a, "Run")
	if r.off == "" {
		t.Fatal("Run is live while its input is missing")
	}
	if !strings.Contains(r.off, "task.md") {
		t.Errorf("Run does not name what it waits for: %q", r.off)
	}
	a.roleCursor = i
	after, cmd, _ := a.enter(nil)
	if cmd != nil {
		t.Error("Run started with its input missing")
	}
	if !after.failed || !strings.Contains(after.status, "task.md") {
		t.Errorf("pressing Run did not say why it is inert: %q", after.status)
	}

	for _, label := range []string{"Connect", "Run with message"} {
		if row, _ := rowNamed(t, a, label); row.off != "" {
			t.Errorf("%s is inert while an input is missing: %s", label, row.off)
		}
	}

	a.role = "planner"
	if row, _ := rowNamed(t, a, "Run"); row.off != "" {
		t.Errorf("Run is inert for a role that reads nothing: %s", row.off)
	}
}

func TestCleanedTaskOffersNothingToRun(t *testing.T) {
	a := testActions(t, task.StatusCleaned, core.RunInfo{}, false)
	for _, label := range []string{"Apply changes", "Connect shell", "Open claude", "Open codex", "planner", "coder"} {
		if r, _ := rowNamed(t, a, label); r.off == "" {
			t.Errorf("%s offered on a cleaned task", label)
		}
	}

	// Pressing a role opens nothing: there is no sandbox behind it.
	_, i := rowNamed(t, a, "coder")
	a.cursor = i
	after, cmd, _ := a.enter(nil)
	if cmd != nil || after.mode != modeMenu {
		t.Errorf("the role screen opened on a cleaned task (mode %v)", after.mode)
	}

	// ...and if the screen was already open when the task was cleaned, every
	// action on it says so rather than failing later in docker.
	a.mode, a.role = modeRole, "coder"
	for _, label := range []string{"Connect", "Run", "Run with message", "Attach", "Stop"} {
		if r, _ := rowNamed(t, a, label); r.off != cleanedOff {
			t.Errorf("%s on a cleaned task says %q", label, r.off)
		}
	}
}

// A harness opens straight away: the container is already set up, so there is
// nothing to undo and nothing to ask about.
func TestHarnessRowsOpenDirectly(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)

	r, _ := rowNamed(t, a, "Open claude")
	if r.act.kind != actHarness || r.act.name != "claude" {
		t.Errorf("row acts as %+v, want the claude harness", r.act)
	}
	if r.act.needsConfirm() {
		t.Error("opening a harness asks for confirmation")
	}
	if r.off != "" {
		t.Errorf("claude is inert: %s", r.off)
	}

	// A missing key is worth saying but is not a reason to refuse: a CLI may log
	// in interactively instead.
	missing, _ := rowNamed(t, a, "Open codex")
	if missing.off != "" {
		t.Errorf("codex was refused over a missing key: %s", missing.off)
	}
	if !strings.Contains(missing.note, "OPENAI_API_KEY") {
		t.Errorf("the missing key is not mentioned: %q", missing.note)
	}
}

// While a note is being typed the field owns the keyboard: "q" is a letter, and
// esc goes back to the role rather than out of the task.
func TestNoteFieldKeepsTheKeyboard(t *testing.T) {
	m := model{screen: screenDetail, detail: newDetailModel(task.Task{ID: "123"}, 60, 10)}
	m.detail.actions = testActions(t, task.StatusActive, core.RunInfo{}, false)
	m.detail.actions.mode, m.detail.actions.role = modeRole, "coder"
	_, i := rowNamed(t, m.detail.actions, "Run with message")
	m.detail.actions.roleCursor = i

	next, _, _ := m.detail.actions.enter(nil)
	m.detail.actions = next
	if m.detail.actions.mode != modeInput {
		t.Fatalf("mode = %v, want the note field", m.detail.actions.mode)
	}

	for _, k := range []tea.KeyMsg{key("q"), key("u"), key("i")} {
		updated, _ := m.updateDetail(k)
		m = updated.(model)
	}
	if m.screen != screenDetail {
		t.Fatal("typing left the task screen")
	}
	if got := m.detail.actions.input.Value(); got != "qui" {
		t.Errorf("the field got %q, want %q", got, "qui")
	}

	empty := m.detail.actions
	empty.input.SetValue("  ")
	empty, cmd, _ := empty.updateInput(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if cmd != nil {
		t.Error("an empty note started a run")
	}
	if !empty.failed {
		t.Error("an empty note was accepted quietly")
	}

	back, _, handled := m.detail.actions.update(tea.KeyMsg{Type: tea.KeyEsc}, nil)
	if !handled || back.mode != modeRole {
		t.Errorf("esc from the field went to mode %v (handled=%v)", back.mode, handled)
	}
}

// The panel is sized from the line count, so a line wider than the panel drags the
// menu out from under that budget. Long text is the normal case: a load error
// names a path.
func TestEveryLineFitsThePanel(t *testing.T) {
	const width = 60
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	a.width = width
	a.task.Repo = "/home/someone/very/long/path/to/a/working/repository"
	a.err = errors.New("/home/someone/.smate/roles/coder/role.yml: outputs is empty — " +
		"list the artefacts the role must leave behind, e.g. outputs: [result.md]")
	a.status, a.failed = strings.Repeat("a lot to say ", 12), true

	const height = 20
	for _, screen := range []struct {
		name  string
		model actionsModel
	}{
		{"menu", a},
		{"role", func() actionsModel { a.mode, a.role = modeRole, "coder"; return a }()},
	} {
		t.Run(screen.name, func(t *testing.T) {
			out := screen.model.view(height)
			lines := strings.Split(out, "\n")
			if len(lines) != height {
				t.Fatalf("view produced %d lines, want exactly %d", len(lines), height)
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > width {
					t.Errorf("line %d is %d columns wide, panel is %d: %q", i, w, width, l)
				}
			}
		})
	}
}

func TestRoleSectionAlwaysSaysSomething(t *testing.T) {
	cases := []struct {
		name  string
		setup func(actionsModel) actionsModel
		want  string
	}{
		{"not loaded yet", func(a actionsModel) actionsModel {
			a.roles, a.loaded = nil, false
			return a
		}, "reading"},
		{"failed to load", func(a actionsModel) actionsModel {
			a.roles, a.loaded, a.err = nil, false, errors.New("broken")
			return a
		}, "unavailable"},
		{"loaded but empty", func(a actionsModel) actionsModel {
			a.roles, a.loaded = nil, true
			return a
		}, "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := c.setup(testActions(t, task.StatusActive, core.RunInfo{}, false))
			rows := a.rows()
			for i := 0; i < len(rows)-1; i++ {
				if rows[i].sep && rows[i+1].sep {
					t.Fatalf("nothing between the separators: %v", labels(rows))
				}
			}
			var found bool
			for _, r := range rows {
				if strings.Contains(r.label, c.want) {
					found = true
					if r.off == "" {
						t.Errorf("%q looks like something to press", r.label)
					}
				}
			}
			if !found {
				t.Errorf("no row mentions %q: %v", c.want, labels(rows))
			}
		})
	}
}

func TestMenuLeavesNavigationAlone(t *testing.T) {
	a := testActions(t, task.StatusActive, core.RunInfo{}, false)
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRight}, {Type: tea.KeyLeft}, {Type: tea.KeyEsc}, key("q"),
	} {
		if _, _, handled := a.update(k, nil); handled {
			t.Errorf("the menu swallowed %v", k)
		}
	}
	a.mode, a.role = modeRole, "coder"
	if _, _, handled := a.update(tea.KeyMsg{Type: tea.KeyEsc}, nil); !handled {
		t.Error("esc did not leave the role screen")
	}
}
