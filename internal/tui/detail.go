package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"smate/internal/artifacts"
	"smate/internal/core"
	"smate/internal/gitx"
	"smate/internal/store"
	"smate/internal/task"
)

type tab int

const tabCount = 4

const (
	tabActions tab = iota
	tabLogs
	tabDiff
	tabArtefacts
)

var tabNames = [tabCount]string{"actions", "logs", "diff", "artefacts"}

var (
	tabStyle       = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1).Foreground(accentColor)
)

// detailModel is one task's screen: tabs of plain scrollable text, each loaded
// from the same packages the CLI's own commands call.
type detailModel struct {
	task   task.Task
	tab    tab
	offset int // first visible line of the current tab

	content [tabCount]string
	err     error

	actions actionsModel

	width, height int
}

func newDetailModel(t task.Task, width, height int) detailModel {
	return detailModel{task: t, width: width, height: height, actions: newActionsModel(t, width)}
}

func (d detailModel) resize(width, height int) detailModel {
	d.width, d.height = width, height
	d.actions.width = width
	return d
}

func (d detailModel) nextTab() detailModel {
	d.tab = (d.tab + 1) % tabCount
	d.offset = 0
	return d
}

func (d detailModel) prevTab() detailModel {
	d.tab = (d.tab - 1 + tabCount) % tabCount
	d.offset = 0
	return d
}

type detailMsg struct {
	id   string // task ID the load was for, to drop a stale answer
	tab  tab
	text string
	err  error
}

func (d detailModel) reload(s *store.Store) tea.Cmd {
	if d.tab == tabActions {
		return loadActions(s, d.task)
	}
	t, tb := d.task, d.tab
	return func() tea.Msg {
		text, err := loadTab(s, t, tb)
		return detailMsg{id: t.ID, tab: tb, text: text, err: err}
	}
}

func (d detailModel) apply(msg detailMsg) detailModel {
	if msg.id != d.task.ID {
		return d // an answer for a task we have since left
	}
	d.err = msg.err
	if msg.err == nil {
		d.content[msg.tab] = msg.text
	}
	return d
}

func loadTab(s *store.Store, t task.Task, tb tab) (string, error) {
	switch tb {
	case tabLogs:
		screen, _, err := core.Logs(s, t.ID)
		return screen, err
	case tabDiff:
		diff, err := gitx.Diff(s.Workspace(t.ID), t.Baseline)
		if err != nil {
			return "", err
		}
		if diff == "" {
			return "(no changes yet)", nil
		}
		return highlightDiff(diff), nil
	case tabArtefacts:
		return loadArtefacts(s.Workspace(t.ID))
	default:
		return "", nil
	}
}

func loadArtefacts(workspace string) (string, error) {
	names, err := artifacts.ListMarkdown(workspace)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "(no .md artefacts yet)", nil
	}
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString("\n\n")
		}
		data, err := os.ReadFile(artifacts.Path(workspace, n))
		if err != nil {
			fmt.Fprintf(&b, "── %s ──\n(unreadable: %v)", n, err)
			continue
		}
		fmt.Fprintf(&b, "── %s ──\n%s", n, string(data))
	}
	return b.String(), nil
}

func (d detailModel) scroll(msg tea.KeyMsg) detailModel {
	lines := strings.Split(d.content[d.tab], "\n")
	max := len(lines) - d.height
	if max < 0 {
		max = 0
	}
	switch msg.String() {
	case "up", "k":
		d.offset--
	case "down", "j":
		d.offset++
	case "pgup":
		d.offset -= d.height
	case "pgdown":
		d.offset += d.height
	case "g", "home":
		d.offset = 0
	case "G", "end":
		d.offset = max
	}
	switch {
	case d.offset < 0:
		d.offset = 0
	case d.offset > max:
		d.offset = max
	}
	return d
}

// tabRow draws the tab line, dropping first the padding and then the styling as
// the panel narrows. It has to come out as one line: the height below is
// budgeted for exactly five lines of chrome, and a row that wraps eats a line of
// the body and pushes the panel past the bottom of the terminal.
func tabRow(current tab, width int) string {
	var padded, bare strings.Builder
	for i, name := range tabNames {
		style, narrow := tabStyle, tabStyle.UnsetPadding()
		if tab(i) == current {
			style, narrow = activeTabStyle, activeTabStyle.UnsetPadding()
		}
		padded.WriteString(style.Render(name) + " ")
		if i > 0 {
			bare.WriteString(" ")
		}
		bare.WriteString(narrow.Render(name))
	}
	switch {
	case lipgloss.Width(padded.String()) <= width:
		return padded.String()
	case lipgloss.Width(bare.String()) <= width:
		return bare.String()
	default:
		return helpStyle.Render(clip(strings.Join(tabNames[:], " "), width))
	}
}

func (d detailModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleStyle.Render(clip("task "+d.task.ID, d.width)))
	b.WriteString(tabRow(d.tab, d.width))
	b.WriteString("\n\n")

	help := "←/→ switch  ↑/↓ scroll  esc back  ctrl+c quit"
	switch {
	case d.tab == tabActions:
		b.WriteString(d.actions.view(d.height))
		help = "←/→ switch  ↑/↓ select  enter do  esc back  ctrl+c quit"
	case d.err != nil:
		b.WriteString(errStyle.Render(d.err.Error()))
	default:
		b.WriteString(d.visibleContent())
	}
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(clip(help, d.width)))

	// detailChromeH is the five fixed lines built above, wrapped around d.height of
	// scrollable body; +panelPadW/H compensate for how lipgloss counts padding.
	//
	// MaxWidth/MaxHeight are the backstop: a body line wider than the panel would
	// be wrapped by Render into a second line, and the panel would grow past the
	// terminal instead of the line being cut. Truncating is ANSI-aware, which is
	// what a coloured diff and an agent's own output need.
	body := lipgloss.NewStyle().MaxWidth(d.width).MaxHeight(d.height + detailChromeH).Render(b.String())
	return panelStyle.Width(d.width + panelPadW).Height(d.height + detailChromeH + panelPadH).Render(body)
}

func (d detailModel) visibleContent() string {
	lines := strings.Split(d.content[d.tab], "\n")
	start := d.offset
	if start > len(lines) {
		start = len(lines)
	}
	end := start + d.height
	if end > len(lines) {
		end = len(lines)
	}
	// A window cut out of a longer ANSI stream may end mid-color. Reset, so it
	// cannot bleed into the help line or the panel border below.
	return strings.Join(lines[start:end], "\n") + "\x1b[0m"
}
