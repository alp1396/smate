package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCutSecretsRemovesFileAndDir(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "file.md"), "secret")
	mustWrite(t, filepath.Join(ws, "folder", "file.md"), "secret")
	mustWrite(t, filepath.Join(ws, "folder", "keep.md"), "not a secret")
	mustWrite(t, filepath.Join(ws, "conf"), "stays")

	cut, err := cutSecrets(ws, []string{"file.md", "folder/file.md"})
	if err != nil {
		t.Fatalf("cutSecrets: %v", err)
	}
	if len(cut) != 2 {
		t.Errorf("normalized list = %v, want two entries", cut)
	}
	assertMissing(t, filepath.Join(ws, "file.md"))
	assertMissing(t, filepath.Join(ws, "folder", "file.md"))
	assertExists(t, filepath.Join(ws, "folder", "keep.md"))
	assertExists(t, filepath.Join(ws, "conf"))
}

func TestCutSecretsRemovesWholeDir(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "creds", "a.env"), "x")
	if _, err := cutSecrets(ws, []string{"creds"}); err != nil {
		t.Fatalf("cutSecrets: %v", err)
	}
	assertMissing(t, filepath.Join(ws, "creds"))
}

// A denylist that matches nothing protects nothing, and the author of it is sure
// of the opposite — so a typo and a mask both stop the task.
func TestCutSecretsRejectsWhatCannotProtect(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "a.env"), "x")

	if _, err := cutSecrets(ws, []string{"no/such.md"}); err == nil {
		t.Error("a missing path was accepted")
	}
	err := errText(cutSecrets(ws, []string{"*.env"}))
	if !strings.Contains(err, "masks are not supported") {
		t.Errorf("mask error = %q", err)
	}
	assertExists(t, filepath.Join(ws, "a.env")) // and nothing was cut
}

// Nothing is cut until the whole list is known to be good: half a denylist
// applied is worse than none, because the failure looks like a config error.
func TestCutSecretsValidatesBeforeCutting(t *testing.T) {
	ws := t.TempDir()
	mustWrite(t, filepath.Join(ws, "good.md"), "x")
	if _, err := cutSecrets(ws, []string{"good.md", "*.env"}); err == nil {
		t.Fatal("the list was accepted")
	}
	assertExists(t, filepath.Join(ws, "good.md"))
}

func TestCutSecretsRejectsEscapes(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(filepath.Dir(ws), "outside.md")
	mustWrite(t, outside, "someone else's")

	for _, p := range []string{"../outside.md", "/etc/passwd", "..", ".", "", "  "} {
		if _, err := cutSecrets(ws, []string{p}); err == nil {
			t.Errorf("path %q should have been rejected", p)
		}
	}
	assertExists(t, outside)
}

func errText(_ []string, err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("should have been cut: %s", path)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("should have stayed: %s (%v)", path, err)
	}
}
