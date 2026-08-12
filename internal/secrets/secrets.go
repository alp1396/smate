// Package secrets is the single interpreter of the denylist declared in
// <repo>/.smate.yml, read in three places: cutting paths out of the snapshot,
// excluding them from the patch series, and rejecting them during validation.
//
// The rules are deliberately narrow — literal paths and directories, nothing else.
// When each place had its own idea of what an entry meant, `*.env` was cut by none
// of them while git excluded it from the patch: the file stayed in the sandbox and
// the patch still looked clean.
package secrets

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// globChars are the pathspec metacharacters git would expand on its own. We do
// not, so accepting them would recreate the disagreement this package removes.
const globChars = `*?[`

// Normalize validates the denylist against the snapshot in root and returns the
// cleaned entries. Every rejection is loud and happens at start: a denylist that
// quietly matches nothing leaves its author sure of a protection that is not there.
func Normalize(list []string, root string) ([]string, error) {
	out := make([]string, 0, len(list))
	for _, raw := range list {
		clean, err := entry(raw)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(clean))); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("secrets: %s is not in the snapshot — an entry that matches nothing protects nothing", raw)
			}
			return nil, fmt.Errorf("secrets: check %s: %w", raw, err)
		}
		out = append(out, clean)
	}
	return out, nil
}

// Match reports whether p is one of the entries or sits inside a listed directory.
// Paths are slash-separated, as they appear in a patch and in git output.
func Match(p string, list []string) (string, bool) {
	clean := path.Clean(strings.TrimSpace(p))
	for _, s := range list {
		s = path.Clean(strings.TrimSpace(s))
		if clean == s || strings.HasPrefix(clean, s+"/") {
			return s, true
		}
	}
	return "", false
}

// entry cleans and checks one declaration, without touching the filesystem.
func entry(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	switch {
	case p == "":
		return "", fmt.Errorf("secrets: empty path")
	case filepath.IsAbs(p) || strings.HasPrefix(p, "/"):
		return "", fmt.Errorf("secrets: absolute path is not allowed: %s", raw)
	case strings.ContainsAny(p, globChars):
		return "", fmt.Errorf("secrets: masks are not supported: %s — list the file or its directory explicitly", raw)
	}
	clean := path.Clean(filepath.ToSlash(p))
	switch {
	case clean == "..", strings.HasPrefix(clean, "../"):
		return "", fmt.Errorf("secrets: path escapes the repository root: %s", raw)
	case clean == ".":
		return "", fmt.Errorf("secrets: path points at the repository root: %s", raw)
	}
	return clean, nil
}
