// Package tui is a terminal UI over the same task data the CLI shows. It is a
// client of internal/core, not a second implementation of it: every field on
// screen comes from a call the CLI already makes.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"smate"
	"smate/internal/core"
	"smate/internal/store"
	"smate/internal/task"
)

// refreshEvery is how often the active screen re-reads its data. Nothing is
// pushed — there is no daemon — so staying current means polling.
const refreshEvery = 2 * time.Second

const appName = "SMATE - Safe AI agent orchestration workflow"

const bannerH = 2

// Run starts the TUI and blocks until the user quits.
func Run(s *store.Store) error {
	// AdaptiveColor asks the terminal (OSC 11) and reads the reply off stdin. Left
	// to happen lazily inside View(), that query races bubbletea's own input loop
	// and times out to a guessed default; forcing it here caches the real answer.
	lipgloss.HasDarkBackground()

	_, err := tea.NewProgram(newModel(s), tea.WithAltScreen()).Run()
	return err
}

type screen int

const (
	screenList screen = iota
	screenDetail
)

type model struct {
	store  *store.Store
	screen screen
	err    error

	tasks  []core.TaskView
	cursor int

	width, height int
	detail        detailModel
}

func newModel(s *store.Store) model { return model{store: s} }

func (m model) Init() tea.Cmd {
	return tea.Batch(loadTasks(m.store), tick())
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type tasksMsg struct {
	tasks []core.TaskView
	err   error
}

func loadTasks(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		views, err := core.ListRuns(s)
		return tasksMsg{tasks: views, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detail = m.detail.resize(m.panelWidth(), m.contentHeight())
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.screen == screenList {
			return m.updateList(msg)
		}
		return m.updateDetail(msg)

	case tasksMsg:
		m.err = msg.err
		if msg.err == nil {
			m.tasks = msg.tasks
			if m.cursor >= len(m.tasks) {
				m.cursor = len(m.tasks) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
		return m, nil

	case detailMsg:
		m.detail = m.detail.apply(msg)
		return m, nil

	case actionsMsg:
		m.detail.actions = m.detail.actions.applyLoad(msg)
		return m, nil

	case connectReadyMsg:
		// Handing the terminal over has to happen here: tea.ExecProcess is a command
		// the program itself must return.
		if msg.id != m.detail.task.ID {
			return m, nil
		}
		if msg.err != nil {
			m.detail.actions = m.detail.actions.applyDone(actionDoneMsg{id: msg.id, err: msg.err})
			return m, nil
		}
		return m, execAttached(m.detail.task, msg.role, msg.note, msg.cmd, nil)

	case actionDoneMsg:
		m.detail.actions = m.detail.actions.applyDone(msg)
		if msg.gone {
			m.screen = screenList
		}
		// An action changes what both screens show, so both are re-read rather than
		// waiting up to refreshEvery for the next tick.
		return m, tea.Batch(m.detail.reload(m.store), loadTasks(m.store))

	case tickMsg:
		var cmd tea.Cmd
		if m.screen == screenList {
			cmd = loadTasks(m.store)
		} else {
			cmd = m.detail.reload(m.store)
		}
		return m, tea.Batch(cmd, tick())
	}
	return m, nil
}

const panelMaxWidth = 160

// marginW/marginH keep the framed panel off the edges of the terminal. On a
// wide screen lipgloss.Place leaves plenty on its own; on a small one the panel
// would otherwise be exactly the size of the terminal and its border would run
// into the edge. Both are per side, so twice as much comes off each dimension.
const (
	marginW = 2
	marginH = 1
)

// panelStyle's own border and padding, split out because lipgloss.Style.Width and
// .Height take a size that accounts for padding (subtracted internally) but not
// border (added after): a child that must come out exactly contentWidth wide has
// to be handed contentWidth+panelPadW. panelChromeW/H is what to subtract from the
// terminal size to get that content size in the first place.
const (
	panelBorderW, panelPadW = 2, 4
	panelBorderH, panelPadH = 2, 2
	panelChromeW            = panelBorderW + panelPadW
	panelChromeH            = panelBorderH + panelPadH
)

func (m model) panelWidth() int {
	w := m.width - panelChromeW - 2*marginW
	if w > panelMaxWidth {
		w = panelMaxWidth
	}
	if w < 20 {
		w = 20
	}
	return w
}

// outerPanelWidth is the panel's rendered width, border included — what the banner
// must match to stay centred over it rather than over the terminal.
func (m model) outerPanelWidth() int {
	return m.panelWidth() + panelChromeW
}

func (m model) panelHeight() int {
	h := m.height - panelChromeH - bannerH - 2*marginH
	if h < 5 {
		h = 5
	}
	return h
}

// detailChromeH is the detail screen's fixed chrome inside the panel: title, tabs,
// a spacer, then a spacer and the help line after the scrollable body.
const detailChromeH = 5

func (m model) contentHeight() int {
	h := m.panelHeight() - detailChromeH
	if h < 1 {
		h = 1
	}
	return h
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.tasks) == 0 {
			return m, nil
		}
		t := m.tasks[m.cursor].Task
		m.screen = screenDetail
		m.detail = newDetailModel(t, m.panelWidth(), m.contentHeight())
		return m, m.detail.reload(m.store)
	}
	return m, nil
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The actions tab sees every key first: while a note is being typed, "q" is a
	// letter and esc leaves the field, not the task.
	if m.detail.tab == tabActions {
		actions, cmd, handled := m.detail.actions.update(msg, m.store)
		m.detail.actions = actions
		if handled {
			return m, cmd
		}
	}

	switch msg.String() {
	case "esc", "q":
		m.screen = screenList
		return m, nil
	case "tab", "right":
		m.detail = m.detail.nextTab()
		return m, m.detail.reload(m.store)
	case "shift+tab", "left":
		m.detail = m.detail.prevTab()
		return m, m.detail.reload(m.store)
	}
	m.detail = m.detail.scroll(msg)
	return m, nil
}

// bannerText shortens the title rather than letting it wrap: a second banner
// line would push the panel past the bottom of the terminal.
func bannerText(width int) string {
	full := fmt.Sprintf("%s - v%s", appName, smate.Version())
	if lipgloss.Width(full) <= width {
		return full
	}
	if short := "SMate - v" + smate.Version(); lipgloss.Width(short) <= width {
		return short
	}
	return clip("SMate", width)
}

func (m model) View() string {
	var panel string
	if m.screen == screenDetail {
		panel = m.detail.View()
	} else {
		panel = m.listView()
	}
	banner := bannerStyle.Width(m.outerPanelWidth()).Render(bannerText(m.outerPanelWidth()))
	block := lipgloss.JoinVertical(lipgloss.Center, banner, "", panel)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}

var (
	borderColor = lipgloss.AdaptiveColor{Light: "252", Dark: "238"}
	accentColor = lipgloss.AdaptiveColor{Light: "27", Dark: "39"}
	selectionBG = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	helpStyle   = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	bannerStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Align(lipgloss.Center)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2)
)

// runStateColor picks the handful of hues a CI dashboard uses, so the states read
// at a glance.
func runStateColor(s core.RunState) lipgloss.AdaptiveColor {
	switch s {
	case core.StateWorking:
		return lipgloss.AdaptiveColor{Light: "27", Dark: "39"} // blue
	case core.StateSleep:
		return lipgloss.AdaptiveColor{Light: "136", Dark: "221"} // yellow
	case core.StateCutOff:
		return lipgloss.AdaptiveColor{Light: "166", Dark: "208"} // orange
	case core.StateFailed:
		return lipgloss.AdaptiveColor{Light: "160", Dark: "203"} // red
	case core.StateDone:
		return lipgloss.AdaptiveColor{Light: "28", Dark: "78"} // green
	default:
		return lipgloss.AdaptiveColor{Light: "245", Dark: "243"} // gray
	}
}

func taskStatusColor(s task.Status) lipgloss.AdaptiveColor {
	switch s {
	case task.StatusActive:
		return lipgloss.AdaptiveColor{Light: "27", Dark: "39"}
	case task.StatusDone:
		return lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
	case task.StatusRejected:
		return lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	default: // CLEANED
		return lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
	}
}

// rowMeta is what taskRow hands back alongside the printed cells, so the
// table's StyleFunc can color STATUS and RUN without reparsing them.
type rowMeta struct {
	status task.Status
	run    core.RunState
}

// taskRow mirrors the columns `smate list` prints, minus REPO, which does not earn
// its width on a narrow screen.
func taskRow(v core.TaskView) ([6]string, rowMeta) {
	run, role, result := string(v.Run.State), "-", "-"
	state := core.StateNone
	if v.HasRun {
		role = v.Run.Meta.Role
		run = fmt.Sprintf("%d %s", v.Run.Meta.N, v.Run.State)
		state = v.Run.State
		if v.Run.HasResult {
			result = strings.Join(v.Run.Meta.Outputs, " ")
		}
	}
	cells := [6]string{v.Task.ID, string(v.Task.Status), v.Task.Branch, run, role, result}
	return cells, rowMeta{status: v.Task.Status, run: state}
}

func (m model) listView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render(fmt.Sprintf("smate — %d task(s)", len(m.tasks))))
	if m.err != nil {
		fmt.Fprintf(&b, "%s\n\n", errStyle.Render(m.err.Error()))
	}

	if len(m.tasks) == 0 {
		b.WriteString("no tasks\n")
	} else {
		first, shown := m.taskWindow()
		view := m.tasks[first : first+shown]
		metas := make([]rowMeta, len(view))
		tbl := table.New().
			Border(lipgloss.NormalBorder()).
			BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
			BorderColumn(false).BorderRow(false).BorderHeader(true).
			BorderStyle(lipgloss.NewStyle().Foreground(borderColor)).
			Width(m.panelWidth()).
			Wrap(false).
			Headers("ID", "STATUS", "BRANCH", "RUN", "ROLE", "RESULT").
			StyleFunc(func(row, col int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == table.HeaderRow {
					return style.Bold(true)
				}
				if row == m.cursor-first {
					style = style.Background(selectionBG).Bold(true)
				}
				switch col {
				case 1:
					style = style.Foreground(taskStatusColor(metas[row].status))
				case 3:
					style = style.Foreground(runStateColor(metas[row].run))
				}
				return style
			})
		for i, v := range view {
			cells, meta := taskRow(v)
			metas[i] = meta
			tbl.Row(cells[:]...)
		}
		b.WriteString(tbl.String())
		b.WriteString("\n")
		if shown < len(m.tasks) {
			fmt.Fprintf(&b, "%s\n", helpStyle.Render(fmt.Sprintf("… %d more", len(m.tasks)-shown)))
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(clip("↑/↓ select  enter open  q quit", m.panelWidth())))

	body := lipgloss.NewStyle().MaxWidth(m.panelWidth()).MaxHeight(m.panelHeight()).Render(b.String())
	return panelStyle.Width(m.panelWidth() + panelPadW).Render(body)
}

// listChromeH is what the list spends on something other than task rows: the
// title and the blank under it, the table's header and its rule, the blank
// above the help line, the help line, and the line saying how many rows did not
// fit.
const listChromeH = 7

// taskWindow is the slice of tasks the panel has room for, kept around the
// cursor: the list does not scroll, so without this a long list would push the
// panel past the bottom of the terminal.
func (m model) taskWindow() (first, shown int) {
	room := m.panelHeight() - listChromeH
	if m.err != nil {
		room -= 2
	}
	if room < 1 {
		room = 1
	}
	if room >= len(m.tasks) {
		return 0, len(m.tasks)
	}
	first = m.cursor - room/2
	if first < 0 {
		first = 0
	}
	if first > len(m.tasks)-room {
		first = len(m.tasks) - room
	}
	return first, room
}
