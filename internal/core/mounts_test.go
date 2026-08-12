package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWithFile lays out a repository root with one file, the way a
// gitignored credential would sit next to the tracked tree.
func repoWithFile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.env"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveMountsAcceptsRelativeAndAbsoluteHost(t *testing.T) {
	root := repoWithFile(t)
	abs := filepath.Join(root, "secret.env")

	got, err := resolveMounts([]string{"secret.env:/run/secret.env", abs + ":/run/abs.env"}, root)
	if err != nil {
		t.Fatalf("resolveMounts: %v", err)
	}
	if len(got.Bind) != 2 {
		t.Fatalf("got %d bind mounts, want 2", len(got.Bind))
	}
	if got.Bind[0].Host != abs || got.Bind[0].Container != "/run/secret.env" {
		t.Errorf("entry 0 = %+v", got.Bind[0])
	}
	if got.Bind[1].Host != abs || got.Bind[1].Container != "/run/abs.env" {
		t.Errorf("entry 1 = %+v", got.Bind[1])
	}
	if len(got.Copy) != 0 {
		t.Errorf("expected no copies, got %+v", got.Copy)
	}
}

func TestResolveMountsSplitsWorkspaceEntriesIntoCopy(t *testing.T) {
	root := repoWithFile(t)
	abs := filepath.Join(root, "secret.env")

	got, err := resolveMounts([]string{
		"secret.env:/workspace/secret.env",
		"secret.env:/workspace/nested/secret.env",
		"secret.env:/home/smate/.config/token",
	}, root)
	if err != nil {
		t.Fatalf("resolveMounts: %v", err)
	}
	if len(got.Bind) != 1 || got.Bind[0].Container != "/home/smate/.config/token" {
		t.Fatalf("bind mounts = %+v", got.Bind)
	}
	if len(got.Copy) != 2 {
		t.Fatalf("got %d copies, want 2: %+v", len(got.Copy), got.Copy)
	}
	if got.Copy[0].Host != abs || got.Copy[0].Rel != "secret.env" {
		t.Errorf("copy 0 = %+v", got.Copy[0])
	}
	if got.Copy[1].Host != abs || got.Copy[1].Rel != "nested/secret.env" {
		t.Errorf("copy 1 = %+v", got.Copy[1])
	}
}

func TestResolveMountsRejects(t *testing.T) {
	root := repoWithFile(t)
	cases := []struct{ entry, want string }{
		{"secret.env", "must be host:container"},
		{"secret.env:", "must be host:container"},
		{":/run/secret.env", "must be host:container"},
		{"secret.env:run/secret.env", "must be absolute"},
		{"secret.env:/workspace", "snapshot itself"},
		{"no-such-file:/run/x", "does not exist"},
	}
	for _, c := range cases {
		_, err := resolveMounts([]string{c.entry}, root)
		if err == nil {
			t.Errorf("entry %q was accepted", c.entry)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("entry %q: error %q does not mention %q", c.entry, err, c.want)
		}
	}
}

func TestResolveMountsEmpty(t *testing.T) {
	got, err := resolveMounts(nil, t.TempDir())
	if err != nil {
		t.Fatalf("resolveMounts: %v", err)
	}
	if len(got.Bind) != 0 || len(got.Copy) != 0 {
		t.Errorf("expected no mounts, got %+v", got)
	}
}

func TestApplyCopiesCopiesFilesAndDirsAndReportsExcludes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtureDir := filepath.Join(root, "fixtures")
	if err := os.MkdirAll(filepath.Join(fixtureDir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "sub", "a.dump"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	exclude, err := applyCopies(ws, []Copy{
		{Host: filepath.Join(root, "CLAUDE.md"), Rel: "CLAUDE.md"},
		{Host: fixtureDir, Rel: "fixtures"},
	})
	if err != nil {
		t.Fatalf("applyCopies: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("CLAUDE.md not copied: %v %q", err, got)
	}
	got, err = os.ReadFile(filepath.Join(ws, "fixtures", "sub", "a.dump"))
	if err != nil || string(got) != "data" {
		t.Fatalf("fixtures/sub/a.dump not copied: %v %q", err, got)
	}

	want := []string{"/CLAUDE.md", "/fixtures"}
	if len(exclude) != len(want) {
		t.Fatalf("exclude = %v, want %v", exclude, want)
	}
	for i := range want {
		if exclude[i] != want[i] {
			t.Errorf("exclude %d = %q, want %q", i, exclude[i], want[i])
		}
	}
}
