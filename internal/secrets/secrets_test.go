package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshot lays out a small tree to validate a denylist against.
func snapshot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{"file.md", "folder/file.md", "creds/a.env", ".env"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestNormalizeAcceptsPathsAndDirs(t *testing.T) {
	root := snapshot(t)
	got, err := Normalize([]string{"file.md", " folder/file.md ", "creds", "./.env"}, root)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	want := []string{"file.md", "folder/file.md", "creds", ".env"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeRejects(t *testing.T) {
	root := snapshot(t)
	cases := []struct{ entry, want string }{
		{"*.env", "masks are not supported"},
		{"creds/*", "masks are not supported"},
		{"conf?.yml", "masks are not supported"},
		{"[abc].md", "masks are not supported"},
		{"no/such.md", "not in the snapshot"},
		{"typo.md", "not in the snapshot"},
		{"", "empty path"},
		{"   ", "empty path"},
		{"/etc/passwd", "absolute path"},
		{"../outside.md", "escapes the repository root"},
		{"..", "escapes the repository root"},
		{".", "points at the repository root"},
	}
	for _, c := range cases {
		_, err := Normalize([]string{c.entry}, root)
		if err == nil {
			t.Errorf("entry %q was accepted", c.entry)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("entry %q: error %q does not mention %q", c.entry, err, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	list := []string{"file.md", "folder/file.md", "creds"}
	hit := []string{"file.md", "./file.md", "folder/file.md", "creds", "creds/a.env", "creds/deep/b.env"}
	miss := []string{"other.md", "folder/keep.md", "folder", "credsx", "credsx/a.env", "a/creds/b"}

	for _, p := range hit {
		if s, ok := Match(p, list); !ok {
			t.Errorf("%q should have matched", p)
		} else if s == "" {
			t.Errorf("%q matched but the entry was not reported", p)
		}
	}
	for _, p := range miss {
		if _, ok := Match(p, list); ok {
			t.Errorf("%q should not have matched", p)
		}
	}
}

// A mask never reaches Match — it is rejected at start — but Match must not
// pretend to understand one either.
func TestMatchDoesNotExpandMasks(t *testing.T) {
	if _, ok := Match("a.env", []string{"*.env"}); ok {
		t.Error("Match expanded a mask")
	}
}
