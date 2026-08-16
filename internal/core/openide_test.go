package core

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"smate/internal/store"
	"smate/internal/task"
)

// What the user configured wins over the environment, and a command line with
// flags survives as a command line: `code -n` is a normal thing to configure.
func TestResolveEditorOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		editor string
		visual string
		env    string
		want   []string
	}{
		{name: "config wins", editor: "code -n", visual: "vi", env: "nano", want: []string{"code", "-n"}},
		{name: "visual next", visual: "vi", env: "nano", want: []string{"vi"}},
		{name: "editor last", env: "nano", want: []string{"nano"}},
		{name: "blank config is no config", editor: "   ", env: "nano", want: []string{"nano"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := store.At(t.TempDir())
			if err := s.SaveGlobal(store.Global{Editor: tc.editor}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.env)

			got, err := resolveEditor(s)
			if err != nil {
				t.Fatalf("resolveEditor: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// With nothing configured and no editor on the machine, the refusal has to say
// where to put one: an editor is not something to guess at.
func TestResolveEditorWithoutAnythingToOpenWith(t *testing.T) {
	if _, err := exec.LookPath("code"); err == nil {
		t.Skip("this machine has code in PATH, which is the intended last resort")
	}
	s := store.At(t.TempDir())
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	_, err := resolveEditor(s)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), s.ConfigPath()) {
		t.Errorf("the error should name %s, got: %v", s.ConfigPath(), err)
	}
}

// The editor is opened on the workspace directory, which is what carries the
// sandbox's git and therefore the diff.
func TestOpenIDECmdOpensTheWorkspace(t *testing.T) {
	s := storeWithTask(t, "123")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "myeditor --wait")

	cmd, warnings, err := OpenIDECmd(s, "123")
	if err != nil {
		t.Fatalf("OpenIDECmd: %v", err)
	}
	want := []string{"myeditor", "--wait", s.Workspace("123")}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
	if len(warnings) > 0 {
		t.Errorf("a task with no runs should warn about nothing, got %v", warnings)
	}
}

// A cleaned task keeps no files, so there is nothing to open and the message
// says so rather than handing the editor a path that is not there.
func TestOpenIDECmdWithoutAWorkspace(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.Save(task.Task{ID: "gone", Status: task.StatusCleaned, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "myeditor")

	if _, _, err := OpenIDECmd(s, "gone"); err == nil {
		t.Fatal("expected a refusal for a task with no workspace")
	}
}

func storeWithTask(t *testing.T, id string) *store.Store {
	t.Helper()
	s := store.At(t.TempDir())
	if err := s.Save(task.Task{ID: id, Status: task.StatusActive, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Workspace(id), 0o700); err != nil {
		t.Fatal(err)
	}
	return s
}
