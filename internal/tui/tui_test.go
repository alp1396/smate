package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"smate/internal/core"
	"smate/internal/task"
)

// The tabs are a row, so the arrows that point along it move between them.
// tab/shift+tab keep working; they are just no longer the only way.
func TestArrowsMoveBetweenTabs(t *testing.T) {
	m := model{screen: screenDetail, detail: newDetailModel(task.Task{ID: "t1"}, 40, 10)}

	steps := []struct {
		key  tea.KeyType
		want tab
	}{
		{tea.KeyRight, tabLogs},
		{tea.KeyRight, tabDiff},
		{tea.KeyRight, tabArtefacts},
		{tea.KeyRight, tabActions}, // the row wraps
		{tea.KeyLeft, tabArtefacts},
		{tea.KeyLeft, tabDiff},
	}
	for _, s := range steps {
		next, _ := m.updateDetail(tea.KeyMsg{Type: s.key})
		m = next.(model)
		if m.detail.tab != s.want {
			t.Fatalf("%v landed on tab %d, want %d", s.key, m.detail.tab, s.want)
		}
	}
}

// Left and right belong to the tab row, not to the body, so they must not also
// be read as scrolling — and the keys that do scroll must still reach it.
func TestArrowsDoNotScrollTheBody(t *testing.T) {
	d := newDetailModel(task.Task{ID: "t1"}, 40, 1)
	d.tab = tabLogs
	d.content[tabLogs] = "one\ntwo\nthree\nfour"

	if got := d.scroll(tea.KeyMsg{Type: tea.KeyDown}).offset; got != 1 {
		t.Fatalf("down did not scroll: offset %d", got)
	}
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		if got := d.scroll(tea.KeyMsg{Type: k}).offset; got != 0 {
			t.Errorf("%v scrolled the body: offset %d", k, got)
		}
	}
}

// Nothing may touch the edge of the terminal: one blank line above and below,
// two blank columns either side. The check is on the rendered screen rather
// than on the size arithmetic, because it is the drawing that has to obey.
func TestScreenKeepsItsMargins(t *testing.T) {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// 30x14 is as small as it goes: below that the panel stops shrinking and the
	// margins are what gives way.
	sizes := []struct{ w, h int }{{30, 14}, {40, 14}, {60, 20}, {120, 40}}

	long := strings.Repeat("a line of output that is much wider than any of these panels\n", 40)
	var many []core.TaskView
	for i := 0; i < 40; i++ {
		many = append(many, core.TaskView{Task: task.Task{
			ID: fmt.Sprintf("task-%d", i), Status: task.StatusActive, Branch: "feature/something-long",
		}})
	}
	screens := []struct {
		name string
		m    model
	}{
		{"list", model{}},
		{"list/full", model{tasks: many, cursor: 30, err: errors.New("~/.smate/roles/coder/role.yml: outputs is empty")}},
	}
	for tb := tab(0); tb < tabCount; tb++ {
		d := newDetailModel(task.Task{ID: "a-task-with-a-long-name"}, 0, 0)
		d.tab = tb
		d.content[tb] = long
		screens = append(screens, struct {
			name string
			m    model
		}{"detail/" + tabNames[tb], model{screen: screenDetail, detail: d}})
	}

	for _, screen := range screens {
		for _, s := range sizes {
			m := screen.m
			m.width, m.height = s.w, s.h
			m.detail = m.detail.resize(m.panelWidth(), m.contentHeight())

			lines := strings.Split(m.View(), "\n")
			if len(lines) > s.h {
				t.Errorf("%s %dx%d: %d lines drawn", screen.name, s.w, s.h, len(lines))
			}
			if strings.TrimSpace(ansi.ReplaceAllString(lines[0], "")) != "" {
				t.Errorf("%s %dx%d: the first line is not blank: %q", screen.name, s.w, s.h, lines[0])
			}
			if last := lines[len(lines)-1]; strings.TrimSpace(ansi.ReplaceAllString(last, "")) != "" {
				t.Errorf("%s %dx%d: the last line is not blank: %q", screen.name, s.w, s.h, last)
			}
			for i, l := range lines {
				plain := strings.TrimRight(ansi.ReplaceAllString(l, ""), " ")
				if plain == "" {
					continue
				}
				left := len(plain) - len(strings.TrimLeft(plain, " "))
				right := s.w - len([]rune(plain))
				if left < marginW || right < marginW {
					t.Errorf("%s %dx%d: line %d runs into the edge (%d left, %d right): %q",
						screen.name, s.w, s.h, i, left, right, plain)
				}
			}
		}
	}
}

// The list does not scroll, so the window has to follow the cursor: the task
// under it is the one about to be opened, and it must be on screen.
func TestTaskWindowFollowsTheCursor(t *testing.T) {
	var tasks []core.TaskView
	for i := 0; i < 40; i++ {
		tasks = append(tasks, core.TaskView{Task: task.Task{ID: fmt.Sprintf("task-%d", i)}})
	}
	m := model{width: 80, height: 24, tasks: tasks}

	for _, cursor := range []int{0, 7, 30, 39} {
		m.cursor = cursor
		first, shown := m.taskWindow()
		if cursor < first || cursor >= first+shown {
			t.Errorf("cursor %d falls outside the window [%d, %d)", cursor, first, first+shown)
		}
		if want := fmt.Sprintf("task-%d ", cursor); !strings.Contains(m.listView(), want) {
			t.Errorf("cursor %d: %q is not on screen", cursor, want)
		}
	}

	// Everything fits: no window, no "… n more".
	m.tasks, m.cursor = tasks[:3], 0
	if first, shown := m.taskWindow(); first != 0 || shown != 3 {
		t.Errorf("a short list was windowed: first=%d shown=%d", first, shown)
	}
	if strings.Contains(m.listView(), "more") {
		t.Error("a short list claims there is more")
	}
}
