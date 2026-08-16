package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"smate/internal/core"
	"smate/internal/roles"
	"smate/internal/store"
	"smate/internal/task"
)

// The actions tab is the one place in the TUI that does something rather than
// shows something, so it is deliberately the slowest to act: everything that
// cannot be taken back asks first.

type actionMode int

const (
	modeMenu    actionMode = iota
	modeRole               // one role's four actions
	modeConfirm            // a yes/no over something irreversible
	modeInput              // the note for "Run with message"
)

type actionKind int

const (
	actApply actionKind = iota
	actShell
	actOpenIDE
	actClean
	actConnect
	actRun
	actRunNote
	actAttach
	actStop
	actHarness
)

type action struct {
	kind actionKind
	role string
	name string
}

// needsConfirm marks the actions that cannot be taken back: apply writes to the
// real repository, clean destroys the sandbox, stop kills work in progress.
func (a action) needsConfirm() bool {
	switch a.kind {
	case actApply, actClean, actStop:
		return true
	}
	return false
}

// row is one line of a menu. A separator is drawn but never landed on.
type row struct {
	sep   bool
	label string
	note  string        // the inline right-hand column
	state core.RunState // colors note when it is a run state
	hint  string        // the footer line, shown while this row is selected
	off   string        // non-empty: the row is inert, and this says why
	act   action
	role  string // non-empty: enter opens this role's screen instead of acting
}

type actionsModel struct {
	task      task.Task
	roles     []roles.Role
	harnesses []core.HarnessInfo
	missing   map[string][]string // role name → the inputs it is waiting for
	run       core.RunInfo
	hasRun    bool
	loaded    bool

	mode       actionMode
	cursor     int
	role       string // whose screen is open in modeRole
	roleCursor int

	pending action // what the confirm or the note is for
	input   textinput.Model

	status string // what the last action did
	failed bool   // ...and whether it went wrong
	err    error  // a failure to load the tab at all

	width int
}

func newActionsModel(t task.Task, width int) actionsModel {
	return actionsModel{task: t, width: width}
}

// roleRun is the last run of the task when it belongs to this role. There is one
// run at a time, so a role is either the current one or idle.
func (a actionsModel) roleRun(name string) (core.RunInfo, bool) {
	if a.hasRun && a.run.Meta.Role == name {
		return a.run, true
	}
	return core.RunInfo{}, false
}

func (a actionsModel) roleLive(name string) bool {
	r, ok := a.roleRun(name)
	return ok && (r.State == core.StateWorking || r.State == core.StateSleep)
}

func (a actionsModel) menu() []row {
	rows := []row{{
		label: "Apply changes",
		note:  "→ " + a.task.Repo,
		hint:  fmt.Sprintf("check the patch and import it as branch %s of %s", a.task.ID, a.task.Repo),
		act:   action{kind: actApply},
	}, {
		label: "Connect shell",
		note:  a.task.Container(),
		hint:  "open a bash prompt inside the container; leaving it does not stop anything",
		act:   action{kind: actShell},
	}}

	// The harnesses sit with the shell: it is the same act — take the terminal into
	// the container — and their keys and state went in when the task started.
	for _, h := range a.harnesses {
		rows = append(rows, a.harnessRow(h))
	}
	// The editor closes that group from the other side: also a way in, but to the
	// workspace on the host rather than to the container.
	rows = append(rows, row{
		label: "Open in editor",
		note:  "workspace on the host",
		hint:  "open the task's workspace in your editor; its git panel shows the diff apply will import",
		act:   action{kind: actOpenIDE},
	}, row{sep: true})

	// The role section always has a line in it: two separators with nothing between
	// them read as a broken menu rather than as an empty library.
	switch {
	case a.err != nil:
		rows = append(rows, row{
			label: "(roles unavailable)",
			off:   "the library did not load",
			hint:  "fix the file named above, or run `smate roles reset --all` to restore the bundled roles",
		})
	case !a.loaded:
		rows = append(rows, row{label: "(reading the role library…)", off: "one moment"})
	case len(a.roles) == 0:
		rows = append(rows, row{label: "(the role library is empty)", off: "nothing to run"})
	}
	for _, r := range a.roles {
		rows = append(rows, a.roleRow(r))
	}

	// A cleaned task has neither container nor workspace, so every row above is
	// inert — the roles included: opening one leads to Connect and Run, and both
	// need the sandbox. Clean, below, is left alone. A row that already says why
	// it is off keeps its own reason.
	if a.task.Status == task.StatusCleaned {
		for i := range rows {
			if !rows[i].sep && rows[i].off == "" {
				rows[i].off = cleanedOff
			}
		}
	}

	return append(rows, row{sep: true}, row{
		label: "Clean",
		note:  "container + workspace",
		hint:  "stop the container and drop the sandbox; the task itself stays in the list",
		act:   action{kind: actClean},
	})
}

func (a actionsModel) harnessRow(h core.HarnessInfo) row {
	out := row{
		label: "Open " + h.Name,
		note:  h.Cmd,
		hint:  fmt.Sprintf("run `%s` in the container and hand it the terminal", h.Cmd),
		act:   action{kind: actHarness, name: h.Name},
	}
	if len(h.Missing) > 0 {
		// Not inert: a CLI that logs in interactively needs no key at all.
		out.note = h.Cmd + "  (no " + strings.Join(h.Missing, ", ") + ")"
		out.hint = fmt.Sprintf("run `%s` — note that %s is not in env.yml (smate config key %s)",
			h.Cmd, strings.Join(h.Missing, ", "), h.Missing[0])
	}
	return out
}

func (a actionsModel) roleRow(r roles.Role) row {
	out := row{
		label: r.Name,
		role:  r.Name,
		hint:  fmt.Sprintf("%s → %s   (enter for connect, run, attach, stop)", inputsOf(r), strings.Join(r.Outputs, " ")),
	}
	if run, ok := a.roleRun(r.Name); ok {
		out.note = fmt.Sprintf("run %d  %s", run.Meta.N, run.State)
		out.state = run.State
		return out
	}
	out.note, out.state = "idle", core.StateNone
	return out
}

func inputsOf(r roles.Role) string {
	if len(r.Inputs) == 0 {
		return "(reads nothing)"
	}
	return strings.Join(r.Inputs, " ")
}

const cleanedOff = "the sandbox has been cleaned"

func (a actionsModel) roleMenu() []row {
	rows := []row{{
		// First, because opening a role to talk to it is the gentler half of what a
		// role is for: the same performer as Run, with the human in the loop.
		label: "Connect",
		note:  "hand over the terminal",
		hint:  "start the role and step straight into it; it reads its instructions and waits for you",
		act:   action{kind: actConnect, role: a.role},
	}, {
		label: "Run",
		note:  "detached",
		hint:  "start the role in the container, detached — the TUI comes straight back",
		act:   action{kind: actRun, role: a.role},
	}, {
		label: "Run with message",
		note:  "detached, with a note",
		hint:  "start it with a note; the agent reads the note before anything else",
		act:   action{kind: actRunNote, role: a.role},
	}, {
		label: "Attach",
		hint:  "step into the live run; ctrl+b then d leaves it running",
		act:   action{kind: actAttach, role: a.role},
	}, {
		label: "Stop",
		hint:  "kill the session; the task and everything written so far stay",
		act:   action{kind: actStop, role: a.role},
	}}

	// The menu should never offer the way in here, but the screen may have been
	// open while the task was cleaned from somewhere else.
	if a.task.Status == task.StatusCleaned {
		for i := range rows {
			rows[i].off = cleanedOff
		}
		return rows
	}

	// Run is what core.Run refuses without every input, so the row refuses first.
	// Its neighbours stay live: a note is a statement of the task in itself, and
	// Connect puts a human at the terminal.
	if miss := a.missing[a.role]; len(miss) > 0 {
		for i, r := range rows {
			if r.act.kind == actRun {
				rows[i].off = strings.Join(miss, ", ") + " is missing"
				rows[i].hint = fmt.Sprintf("%s needs %s — run the role that writes it, or start this one with a message",
					a.role, strings.Join(miss, ", "))
			}
		}
	}

	// Attach and Stop are found by what they do rather than by where they sit: the
	// menu has grown a row above them once already.
	run, live := a.roleRun(a.role)
	for i, r := range rows {
		if r.act.kind != actAttach && r.act.kind != actStop {
			continue
		}
		if !live || (run.State != core.StateWorking && run.State != core.StateSleep) {
			rows[i].off = a.role + " is not running"
			continue
		}
		rows[i].note = fmt.Sprintf("run %d  %s", run.Meta.N, run.State)
		rows[i].state = run.State
	}
	return rows
}

func (a actionsModel) rows() []row {
	if a.mode == modeRole {
		return a.roleMenu()
	}
	return a.menu()
}

func (a actionsModel) at() int {
	if a.mode == modeRole {
		return a.roleCursor
	}
	return a.cursor
}

func (a actionsModel) moveTo(i int) actionsModel {
	if a.mode == modeRole {
		a.roleCursor = i
		return a
	}
	a.cursor = i
	return a
}

func (a actionsModel) move(delta int) actionsModel {
	rows := a.rows()
	i := a.at()
	for n := 0; n < len(rows); n++ {
		i += delta
		if i < 0 || i >= len(rows) {
			return a
		}
		if !rows[i].sep {
			return a.moveTo(i)
		}
	}
	return a
}

func (a actionsModel) settle() actionsModel {
	rows := a.rows()
	i := a.at()
	if i >= len(rows) {
		i = len(rows) - 1
	}
	if i < 0 {
		i = 0
	}
	for i < len(rows) && rows[i].sep {
		i++
	}
	return a.moveTo(i)
}

func (a actionsModel) update(msg tea.KeyMsg, s *store.Store) (actionsModel, tea.Cmd, bool) {
	switch a.mode {
	case modeInput:
		return a.updateInput(msg, s)
	case modeConfirm:
		return a.updateConfirm(msg, s)
	}

	switch msg.String() {
	case "up", "k":
		return a.move(-1), nil, true
	case "down", "j":
		return a.move(1), nil, true
	case "enter":
		return a.enter(s)
	case "esc":
		if a.mode == modeRole {
			a.mode, a.role = modeMenu, ""
			return a, nil, true
		}
	}
	return a, nil, false
}

func (a actionsModel) updateInput(msg tea.KeyMsg, s *store.Store) (actionsModel, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		a.mode = modeRole
		return a, nil, true
	case "enter":
		if strings.TrimSpace(a.input.Value()) == "" {
			a.status, a.failed = "a note with nothing in it is not a task — esc to go back", true
			return a, nil, true
		}
		a.mode = modeRole
		next, cmd := a.fire(a.pending, s)
		return next, cmd, true
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd, true
}

func (a actionsModel) updateConfirm(msg tea.KeyMsg, s *store.Store) (actionsModel, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "Y", "enter":
		a.mode = a.modeBehindConfirm()
		next, cmd := a.fire(a.pending, s)
		return next, cmd, true
	default:
		a.mode = a.modeBehindConfirm()
		a.status, a.failed = "cancelled", false
		return a, nil, true
	}
}

func (a actionsModel) modeBehindConfirm() actionMode {
	if a.pending.role != "" {
		return modeRole
	}
	return modeMenu
}

func (a actionsModel) enter(s *store.Store) (actionsModel, tea.Cmd, bool) {
	rows := a.rows()
	i := a.at()
	if i < 0 || i >= len(rows) {
		return a, nil, true
	}
	r := rows[i]
	switch {
	case r.off != "":
		a.status, a.failed = r.off, true
		return a, nil, true
	case r.role != "":
		a.mode, a.role, a.roleCursor = modeRole, r.role, 0
		a.status = ""
		return a, nil, true
	}

	a.status, a.failed = "", false
	if r.act.needsConfirm() {
		a.pending, a.mode = r.act, modeConfirm
		return a, nil, true
	}
	if r.act.kind == actRunNote {
		a.pending, a.mode = r.act, modeInput
		a.input = noteInput(a.width)
		return a, textinput.Blink, true
	}
	next, cmd := a.fire(r.act, s)
	return next, cmd, true
}

// fire hands the action to core. Nothing here waits: every command returns a
// message, so the screen stays answerable while apply talks to git.
func (a actionsModel) fire(act action, s *store.Store) (actionsModel, tea.Cmd) {
	a.status, a.failed = "", false
	t := a.task
	switch act.kind {
	case actApply:
		a.status = "applying…"
		return a, doApply(s, t)
	case actShell:
		cmd, err := core.ShellCmd(s, t.ID)
		return a, execAttached(t, "the shell", "", cmd, err)
	case actOpenIDE:
		cmd, warnings, err := core.OpenIDECmd(s, t.ID)
		return a, execAttached(t, "the editor", strings.Join(warnings, "; "), cmd, err)
	case actClean:
		a.status = "cleaning…"
		return a, doClean(s, t)
	case actConnect:
		// Two steps: preparing the session talks to docker, and only Update can hand
		// over the terminal.
		a.status = "connecting…"
		return a, doConnect(s, t, act.role)
	case actRun:
		return a, doRun(s, t, act.role, "")
	case actRunNote:
		return a, doRun(s, t, act.role, a.input.Value())
	case actHarness:
		cmd, err := core.HarnessCmd(s, t.ID, act.name)
		return a, execAttached(t, act.name, "", cmd, err)
	case actAttach:
		cmd, err := core.AttachCmd(s, t.ID)
		return a, execAttached(t, "the run", "", cmd, err)
	case actStop:
		return a, doStop(s, t)
	}
	return a, nil
}

func noteInput(width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "what this run should do"
	ti.CharLimit = 2000
	ti.Width = width - 6
	ti.Focus()
	return ti
}

type actionsMsg struct {
	id        string
	roles     []roles.Role
	harnesses []core.HarnessInfo
	missing   map[string][]string
	run       core.RunInfo
	hasRun    bool
	err       error
}

type actionDoneMsg struct {
	id   string
	text string
	err  error
	gone bool // the sandbox is gone with it: there is nothing left to show
}

func loadActions(s *store.Store, t task.Task) tea.Cmd {
	return func() tea.Msg {
		m := actionsMsg{id: t.ID}
		// A config.yml that will not parse must not read as an empty role library.
		hs, err := core.Harnesses(s)
		if err != nil {
			m.err = err
			return m
		}
		m.harnesses = hs
		rs, err := core.Roles(s)
		if err != nil {
			m.err = err
			return m
		}
		m.roles = rs
		missing, err := core.MissingInputs(s, t.ID, rs)
		if err != nil {
			m.err = err
			return m
		}
		m.missing = missing
		run, ok, err := core.LastRun(s, t.ID)
		if err != nil {
			m.err = err
			return m
		}
		m.run, m.hasRun = run, ok
		return m
	}
}

func (a actionsModel) applyLoad(msg actionsMsg) actionsModel {
	if msg.id != a.task.ID {
		return a
	}
	a.err = msg.err
	if msg.err == nil {
		a.roles, a.harnesses, a.missing = msg.roles, msg.harnesses, msg.missing
		a.run, a.hasRun, a.loaded = msg.run, msg.hasRun, true
	}
	return a.settle()
}

func (a actionsModel) applyDone(msg actionDoneMsg) actionsModel {
	if msg.id != a.task.ID {
		return a
	}
	if msg.err != nil {
		a.status, a.failed = msg.err.Error(), true
		return a
	}
	a.status, a.failed = msg.text, false
	return a
}

func doApply(s *store.Store, t task.Task) tea.Cmd {
	return func() tea.Msg {
		out, rep, err := core.Apply(s, t.ID)
		switch {
		case errors.Is(err, core.ErrNothingToApply):
			return actionDoneMsg{id: t.ID, text: "nothing to import — the sandbox matches the baseline"}
		case err != nil:
			return actionDoneMsg{id: t.ID, err: err}
		}
		text := fmt.Sprintf("imported %d file(s) into branch %s of %s", len(rep.Files), out.ID, out.Repo)
		if len(rep.Cut) > 0 {
			text += fmt.Sprintf(" — %d cut as secrets", len(rep.Cut))
		}
		return actionDoneMsg{id: t.ID, text: text}
	}
}

func doRun(s *store.Store, t task.Task, role, note string) tea.Cmd {
	return func() tea.Msg {
		meta, warnings, err := core.Run(s, t.ID, role, note, false)
		if err != nil {
			return actionDoneMsg{id: t.ID, err: err}
		}
		text := fmt.Sprintf("run %d started: %s", meta.N, role)
		if len(warnings) > 0 {
			text += " — " + strings.Join(warnings, "; ")
		}
		return actionDoneMsg{id: t.ID, text: text}
	}
}

// connectReadyMsg is a role prepared and waiting on its session; what comes back
// is the command, since only the screen above can start it.
type connectReadyMsg struct {
	id   string
	role string
	cmd  *exec.Cmd
	note string // what the preflight waved through, said after the session ends
	err  error
}

func doConnect(s *store.Store, t task.Task, role string) tea.Cmd {
	return func() tea.Msg {
		meta, warnings, cmd, err := core.Connect(s, t.ID, role, "")
		if err != nil {
			return connectReadyMsg{id: t.ID, role: role, err: err}
		}
		note := fmt.Sprintf("run %d", meta.N)
		if len(warnings) > 0 {
			note += " — " + strings.Join(warnings, "; ")
		}
		return connectReadyMsg{id: t.ID, role: role, cmd: cmd, note: note}
	}
}

func doStop(s *store.Store, t task.Task) tea.Cmd {
	return func() tea.Msg {
		r, err := core.Stop(s, t.ID)
		if err != nil {
			return actionDoneMsg{id: t.ID, err: err}
		}
		return actionDoneMsg{id: t.ID, text: fmt.Sprintf("run %d (%s) stopped", r.Meta.N, r.Meta.Role)}
	}
}

func doClean(s *store.Store, t task.Task) tea.Cmd {
	return func() tea.Msg {
		if _, err := core.Clean(s, t.ID, false); err != nil {
			return actionDoneMsg{id: t.ID, err: err}
		}
		return actionDoneMsg{id: t.ID, text: "task " + t.ID + " cleaned", gone: true}
	}
}

// execAttached gives the terminal to an interactive command and takes it back.
// tea.ExecProcess is the only way to do that from inside the alt screen.
//
// note is said on the way back: while the command owns the terminal there is no
// screen to say anything on.
func execAttached(t task.Task, what, note string, cmd *exec.Cmd, err error) tea.Cmd {
	if err != nil {
		return func() tea.Msg { return actionDoneMsg{id: t.ID, err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionDoneMsg{id: t.ID, err: err}
		}
		text := "back from " + what
		if note != "" {
			text += " (" + note + ")"
		}
		return actionDoneMsg{id: t.ID, text: text}
	})
}
