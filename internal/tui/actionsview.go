package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"smate/internal/task"
)

// The menu is a two-column grid: a label, and a right-hand column saying which
// repository, container or run. The sentence explaining the selected row lives at
// the bottom, where it does not have to be cut to fit.
const actionLabelW = 20

var (
	dividerStyle = lipgloss.NewStyle().Foreground(borderColor)
	selectedRow  = lipgloss.NewStyle().Background(selectionBG).Bold(true)
	noteRow      = lipgloss.NewStyle().Faint(true)
	offRow       = lipgloss.NewStyle().Faint(true).Italic(true)
	promptStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "166", Dark: "208"})
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"})
	crumbStyle   = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
)

func (a actionsModel) view(height int) string {
	var lines []string

	if a.mode == modeRole {
		lines = append(lines, crumbStyle.Render("role "+a.role), "")
	}
	if a.err != nil {
		// Wrapped rather than clipped: a load error names a file and says what is
		// wrong with it. Line by line, so the height budget counts what is drawn.
		for _, l := range wrapTo(a.err.Error(), a.rowWidth()) {
			lines = append(lines, errStyle.Render(l))
		}
		lines = append(lines, "")
	}

	rows := a.rows()
	cursor := a.at()
	for i, r := range rows {
		lines = append(lines, a.renderRow(r, i == cursor && a.mode != modeConfirm))
	}

	lines = append(lines, "")
	lines = append(lines, a.footer(rows, cursor)...)

	// Pad first, clip second: a short menu still pushes the help line to the bottom,
	// a long one does not push it out of the panel.
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

func (a actionsModel) renderRow(r row, selected bool) string {
	if r.sep {
		return dividerStyle.Render(strings.Repeat("─", a.rowWidth()))
	}

	marker := "  "
	if selected {
		marker = "▸ "
	}
	label := pad(clip(r.label, actionLabelW), actionLabelW)

	// An inert row says why instead of what, and goes faint down to the label.
	note, noteStyled, labelStyled := r.note, noteRow, lipgloss.NewStyle()
	switch {
	case r.off != "":
		note, noteStyled, labelStyled = r.off, offRow, offRow
	case r.state != "":
		noteStyled = lipgloss.NewStyle().Foreground(runStateColor(r.state))
	}
	// Every row is one line: a note that wraps drags the menu out of its height
	// budget.
	note = clip(note, a.rowWidth()-len(marker)-actionLabelW-1)

	body := marker + labelStyled.Render(label)
	if note != "" {
		body += " " + noteStyled.Render(note)
	}
	if !selected {
		return body
	}
	// The highlight spans the row, so it is painted over a line already padded to
	// the full width.
	width := lipgloss.Width(marker + label)
	if note != "" {
		width += lipgloss.Width(" " + note)
	}
	if fill := a.rowWidth() - width; fill > 0 {
		body += strings.Repeat(" ", fill)
	}
	return selectedRow.Render(body)
}

func (a actionsModel) footer(rows []row, cursor int) []string {
	switch a.mode {
	case modeConfirm:
		return []string{
			promptStyle.Render(confirmQuestion(a.pending, a.task)),
			helpStyle.Render("y to go ahead · any other key to back out"),
		}
	case modeInput:
		return []string{
			a.input.View(),
			helpStyle.Render("enter to start the run · esc to back out"),
		}
	}

	var out []string
	if cursor >= 0 && cursor < len(rows) {
		out = append(out, noteRow.Render(clip(rows[cursor].hint, a.rowWidth())))
	} else {
		out = append(out, "")
	}
	switch {
	case a.status == "":
		out = append(out, "")
	case a.failed:
		out = append(out, errStyle.Render(clip(a.status, a.rowWidth())))
	default:
		out = append(out, okStyle.Render(clip(a.status, a.rowWidth())))
	}
	return out
}

func confirmQuestion(act action, t task.Task) string {
	switch act.kind {
	case actApply:
		return fmt.Sprintf("Import into %s as branch %s?", t.Repo, t.ID)
	case actClean:
		return fmt.Sprintf("Remove the container and workspace of %s? The sandbox is gone for good.", t.ID)
	case actStop:
		return fmt.Sprintf("Kill the running %s? Anything it has not written down is lost.", act.role)
	}
	return "Are you sure?"
}

func (a actionsModel) rowWidth() int {
	if a.width < 20 {
		return 20
	}
	return a.width
}

func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	out := []rune(s)
	for len(out) > 0 && lipgloss.Width(string(out)+"…") > n {
		out = out[:len(out)-1]
	}
	return string(out) + "…"
}

func wrapTo(s string, n int) []string {
	if n <= 0 {
		return []string{s}
	}
	wrapped := lipgloss.NewStyle().Width(n).Render(s)
	return strings.Split(strings.TrimRight(wrapped, "\n"), "\n")
}

func pad(s string, n int) string {
	if fill := n - lipgloss.Width(s); fill > 0 {
		return s + strings.Repeat(" ", fill)
	}
	return s
}
