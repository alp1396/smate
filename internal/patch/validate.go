// Package patch produces and validates patch series.
//
// Patch content comes out of the container, so it is untrusted input. Parsing
// is fail-closed: anything that does not parse unambiguously is rejected.
package patch

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"strings"

	"smate/internal/artifacts"
	"smate/internal/secrets"
)

type Report struct {
	Files    []string // paths touched by the series
	Warnings []string // non-blocking notes, e.g. an added +x bit
	Cut      []string // secret paths the agent touched; filled in by core
}

const symlinkMode = "120000"

// Validate checks a patch series. An error means the import is refused.
//
// cut holds the paths cut from the snapshot at start; their changes are already
// excluded when the series is produced, so checking them here is a second line of
// defence in case that did not hold.
func Validate(data []byte, cut []string) (Report, error) {
	var rep Report
	seen := map[string]bool{}

	addPath := func(p string) error {
		if err := checkPath(p); err != nil {
			return err
		}
		if s, ok := MatchSecret(p, cut); ok {
			return fmt.Errorf("patch touches cut path %s (secrets: %s)", p, s)
		}
		if !seen[p] {
			seen[p] = true
			rep.Files = append(rep.Files, p)
		}
		return nil
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var current string // last path seen in diff --git, used in mode messages

	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			a, b, err := parseDiffGit(line)
			if err != nil {
				return Report{}, err
			}
			if err := addPath(a); err != nil {
				return Report{}, err
			}
			if err := addPath(b); err != nil {
				return Report{}, err
			}
			current = b

		case strings.HasPrefix(line, "rename from "), strings.HasPrefix(line, "rename to "),
			strings.HasPrefix(line, "copy from "), strings.HasPrefix(line, "copy to "):
			if err := addPath(line[strings.LastIndex(line, " ")+1:]); err != nil {
				return Report{}, err
			}

		case strings.HasPrefix(line, "new mode "), strings.HasPrefix(line, "new file mode "),
			strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "deleted file mode "):
			mode := line[strings.LastIndex(line, " ")+1:]
			if mode == symlinkMode {
				return Report{}, fmt.Errorf("patch touches a symlink: %s", current)
			}
			if mode == "100755" && !strings.HasPrefix(line, "old mode") {
				rep.Warnings = append(rep.Warnings, "+x bit set on "+current)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Report{}, fmt.Errorf("read patch: %w", err)
	}
	if len(rep.Files) == 0 {
		return Report{}, fmt.Errorf("patch changes no files")
	}
	return rep, nil
}

// parseDiffGit parses a `diff --git a/PATH b/PATH` line.
// Paths may contain spaces, so the split relies on both halves having equal
// length. The quoted form (`"a/..."`) is refused rather than unescaped.
func parseDiffGit(line string) (string, string, error) {
	rest := strings.TrimPrefix(line, "diff --git ")
	if strings.HasPrefix(rest, `"`) {
		return "", "", fmt.Errorf("unparseable patch header: %s", line)
	}
	// rest == "a/" + p + " b/" + p  =>  len(rest) == 2*len(p)+5
	n := (len(rest) - 5)
	if n <= 0 || n%2 != 0 {
		return "", "", fmt.Errorf("unparseable patch header: %s", line)
	}
	n /= 2
	a, b := rest[:2+n], rest[2+n:]
	if !strings.HasPrefix(a, "a/") || !strings.HasPrefix(b, " b/") {
		return "", "", fmt.Errorf("unparseable patch header: %s", line)
	}
	return a[2:], b[3:], nil
}

// MatchSecret reports whether a path falls under one of the cut paths. The rule
// lives in internal/secrets — this is the same matcher the cutting uses, not a
// second reading of the list.
func MatchSecret(p string, list []string) (string, bool) {
	return secrets.Match(p, list)
}

// checkPath rejects path traversal, anything inside .git and anything inside the
// artefact directory — a path from there means the exclusion at patch time did not
// hold.
func checkPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path in patch")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("absolute path in patch: %s", p)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path escapes the repository root: %s", p)
	}
	for _, seg := range strings.Split(clean, "/") {
		switch seg {
		case ".git":
			return fmt.Errorf("path inside .git is not allowed: %s", p)
		case artifacts.Dir:
			return fmt.Errorf("path inside %s is not allowed: %s (task artefacts are not imported)", artifacts.Dir, p)
		}
	}
	return nil
}
