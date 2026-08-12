package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const diffBannerRule = "-------------------------------------------------------"

// diffGutterWidth is the space reserved for a change marker, so a +/- always
// stands clear of the code it marks.
const diffGutterWidth = 4

var (
	// diffBannerStyle is the per-file heading inserted before each
	// "diff --git" line, running flush to the edge rather than through the gutter.
	diffBannerStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	// diffGitLineStyle is the raw diff --git line, demoted to metadata now
	// that the banner carries the per-file signal.
	diffGitLineStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	diffMetaStyle    = lipgloss.NewStyle().Faint(true)
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "80"})
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5f875f", Dark: "#87af87"})
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#af5f5f", Dark: "#af8787"})
	// diffAddSymbolStyle/diffDelSymbolStyle are that color eased toward the
	// page background: the same color family, not a jarring second one.
	diffAddSymbolStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7f9f7f", Dark: "#6c8c6c"})
	diffDelSymbolStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#bf7f7f", Dark: "#8c6c6c"})
)

// highlightDiff colors a unified diff line by line. chroma's diff lexer cannot do
// this: its "+.*"/"-.*" rules match "+++ b/x" with the same token as an ordinary
// added line, so a file header and its content render identically.
func highlightDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var b strings.Builder
	for i, line := range lines {
		switch {
		case line == "":
		case strings.HasPrefix(line, "diff --git "):
			if i > 0 {
				b.WriteString("\n")
			}
			if file := fileFromDiffGitLine(line); file != "" {
				b.WriteString(diffBannerStyle.Render(diffBannerRule))
				b.WriteString("\n")
				b.WriteString(diffBannerStyle.Render("FILE: /" + file))
				b.WriteString("\n\n")
			}
			b.WriteString(diffGutter(""))
			b.WriteString(diffGitLineStyle.Render(line))
		case strings.HasPrefix(line, "index "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			b.WriteString(diffGutter(""))
			b.WriteString(diffMetaStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(diffGutter(""))
			b.WriteString(diffHunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffGutter(diffAddSymbolStyle.Render("+")))
			b.WriteString(diffAddStyle.Render(line[1:]))
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffGutter(diffDelSymbolStyle.Render("-")))
			b.WriteString(diffDelStyle.Render(line[1:]))
		case strings.HasPrefix(line, " "):
			b.WriteString(diffGutter(""))
			b.WriteString(line[1:])
		default:
			b.WriteString(diffGutter(""))
			b.WriteString(line)
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func diffGutter(marker string) string {
	pad := diffGutterWidth - lipgloss.Width(marker)
	if pad < 0 {
		pad = 0
	}
	return marker + strings.Repeat(" ", pad)
}

func fileFromDiffGitLine(line string) string {
	const marker = " b/"
	idx := strings.LastIndex(line, marker)
	if idx == -1 {
		return ""
	}
	return line[idx+len(marker):]
}
