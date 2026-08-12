package patch

import (
	"strings"
	"testing"
)

const goodPatch = `From abc123 Mon Sep 17 00:00:00 2001
From: smate <smate@localhost>
Subject: [PATCH] a change

---
 src/main.go | 2 +-
 1 file changed

diff --git a/src/main.go b/src/main.go
index 1111111..2222222 100644
--- a/src/main.go
+++ b/src/main.go
@@ -1,1 +1,1 @@
-old
+new
--
2.43.0
`

func TestValidateAcceptsNormalPatch(t *testing.T) {
	rep, err := Validate([]byte(goodPatch), nil)
	if err != nil {
		t.Fatalf("a normal patch was rejected: %v", err)
	}
	if len(rep.Files) != 1 || rep.Files[0] != "src/main.go" {
		t.Errorf("files: got %v, want [src/main.go]", rep.Files)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", rep.Warnings)
	}
}

func TestValidateAcceptsPathWithSpaces(t *testing.T) {
	p := "diff --git a/doc/my file.md b/doc/my file.md\n--- a/doc/my file.md\n+++ b/doc/my file.md\n"
	rep, err := Validate([]byte(p), nil)
	if err != nil {
		t.Fatalf("a path with a space was rejected: %v", err)
	}
	if rep.Files[0] != "doc/my file.md" {
		t.Errorf("path parsed wrong: %q", rep.Files[0])
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]string{
		"path traversal":      "diff --git a/../../etc/passwd b/../../etc/passwd\n",
		"absolute path":       "diff --git a//etc/passwd b//etc/passwd\n",
		".git at the root":    "diff --git a/.git/config b/.git/config\n",
		".git deeper down":    "diff --git a/sub/.git/hooks/pre-commit b/sub/.git/hooks/pre-commit\n",
		"new symlink":         "diff --git a/link b/link\nnew file mode 120000\n",
		"turned into symlink": "diff --git a/f b/f\nold mode 100644\nnew mode 120000\n",
		"rename out of tree":  "diff --git a/a b/a\nrename from a\nrename to ../../b\n",
		"quoted header":       "diff --git \"a/we\\tird\" \"b/we\\tird\"\n",
		"no diffs at all":     "just text with no diffs\n",
	}
	for name, p := range cases {
		if _, err := Validate([]byte(p), nil); err == nil {
			t.Errorf("%s: the patch should have been rejected", name)
		}
	}
}

func TestValidateRejectsSecretPaths(t *testing.T) {
	secrets := []string{"secret.txt", "creds"}
	for _, p := range []string{
		"diff --git a/secret.txt b/secret.txt\n",
		"diff --git a/creds/aws.env b/creds/aws.env\n",
		"diff --git a/creds b/creds\n",
	} {
		if _, err := Validate([]byte(p), secrets); err == nil {
			t.Errorf("%q: a cut path should have been rejected", p)
		}
	}
	// A similar but different path still passes.
	ok := "diff --git a/secret.txt.md b/secret.txt.md\n"
	if _, err := Validate([]byte(ok), secrets); err != nil {
		t.Errorf("%q should not count as a secret: %v", ok, err)
	}
}

// Artefacts are excluded when the series is produced; a path from .smate/ in the
// patch means that did not hold, and it is refused rather than imported.
func TestValidateRejectsArtifactPaths(t *testing.T) {
	for _, p := range []string{
		"diff --git a/.smate/coder.result.md b/.smate/coder.result.md\n",
		"diff --git a/.smate/runs/1/log b/.smate/runs/1/log\n",
		"diff --git a/sub/.smate/x b/sub/.smate/x\n",
	} {
		if _, err := Validate([]byte(p), nil); err == nil {
			t.Errorf("%q: an artefact path should have been rejected", p)
		}
	}
	ok := "diff --git a/.smate.yml b/.smate.yml\n"
	if _, err := Validate([]byte(ok), nil); err != nil {
		t.Errorf("%q is the project config, not an artefact: %v", ok, err)
	}
}

func TestMatchSecret(t *testing.T) {
	secrets := []string{"secret.txt", "creds", "a/b"}
	for _, p := range []string{"secret.txt", "creds", "creds/aws.env", "creds/sub/x", "a/b/c"} {
		if _, ok := MatchSecret(p, secrets); !ok {
			t.Errorf("%q should match", p)
		}
	}
	for _, p := range []string{"secret.txt.md", "credsx", "credsx/y", "a/bc", "b/creds"} {
		if _, ok := MatchSecret(p, secrets); ok {
			t.Errorf("%q should not match", p)
		}
	}
	if _, ok := MatchSecret("anything", nil); ok {
		t.Error("nothing should match an empty denylist")
	}
}

func TestValidateWarnsOnExecBit(t *testing.T) {
	p := "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n"
	rep, err := Validate([]byte(p), nil)
	if err != nil {
		t.Fatalf("an +x bit change must not block: %v", err)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "run.sh") {
		t.Errorf("expected a warning about run.sh, got %v", rep.Warnings)
	}
}
