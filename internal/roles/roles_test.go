package roles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledIsPlannerCoderAndReviewer(t *testing.T) {
	names, err := Bundled()
	if err != nil {
		t.Fatalf("Bundled: %v", err)
	}
	if len(names) != 3 || names[0] != "coder" || names[1] != "planner" || names[2] != "reviewer" {
		t.Fatalf("bundled roles = %v, want [coder planner reviewer]", names)
	}
}

func TestSeedWritesDefaultsOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roles")

	if err := Seed(dir); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	r, err := Load(dir, "reviewer")
	if err != nil {
		t.Fatalf("Load reviewer: %v", err)
	}
	if len(r.Outputs) != 1 || r.Outputs[0] != "reviewer.result.md" || len(r.Inputs) != 1 {
		t.Errorf("unexpected default reviewer: %+v", r)
	}
	// The bundled roles name a model and leave the flag to the harness.
	if r.Model == "" {
		t.Error("default reviewer names no model")
	}
	if _, err := os.Stat(AgentsPath(dir, "reviewer")); err != nil {
		t.Errorf("reviewer instructions not seeded: %v", err)
	}

	// planner leaves two artefacts behind, and the run counts only when both are
	// there — the reason outputs is a list at all.
	p, err := Load(dir, "planner")
	if err != nil {
		t.Fatalf("Load planner: %v", err)
	}
	if len(p.Outputs) != 2 || p.Outputs[0] != "task.md" || p.Outputs[1] != "plan.md" {
		t.Errorf("planner outputs = %v, want [task.md plan.md]", p.Outputs)
	}
	if len(p.Inputs) != 0 {
		t.Errorf("planner should read nothing, got inputs %v", p.Inputs)
	}

	// An existing library is left alone, including a deliberate removal.
	if err := os.RemoveAll(filepath.Join(dir, "reviewer")); err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir); err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	if _, err := Load(dir, "reviewer"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Seed brought a removed role back: %v", err)
	}

	if err := Reset(dir, "reviewer"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err := Load(dir, "reviewer"); err != nil {
		t.Errorf("Reset did not restore the role: %v", err)
	}
	if err := Reset(dir, "architect"); err == nil {
		t.Error("Reset of a role that is not bundled should fail")
	}
}

func TestResetDiscardsLocalEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roles")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AgentsPath(dir, "coder"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Reset(dir, "coder"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(AgentsPath(dir, "coder"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "mine" {
		t.Error("Reset kept the local edit")
	}
}

func TestLoadRejectsBrokenDefinitions(t *testing.T) {
	cases := []struct {
		name string
		yml  string
		want string
	}{
		{"no harness", "model: claude-opus-5\noutputs: [x.md]\n", "harness is empty"},
		// The role used to carry the whole command line. A file left from then runs
		// the wrong model silently unless it is stopped and told where it moved.
		{"cmd from before", "harness: claude\ncmd: claude --model x\noutputs: [x.md]\n", "cmd is no longer read"},
		{"no outputs", "harness: claude\n", "outputs is empty"},
		// The key was `output` before it took a list. A role file left from then
		// must say so rather than look like a role that declares nothing.
		{"singular output key", "harness: claude\noutput: x.md\n", "outputs is empty"},
		{"output in inputs", "harness: claude\ninputs: [x.md]\noutputs: [x.md]\n", "deleted at the start"},
		{"second output in inputs", "harness: claude\ninputs: [y.md]\noutputs: [x.md, y.md]\n", "deleted at the start"},
		{"path in output", "harness: claude\noutputs: [sub/x.md]\n", "without a path"},
		{"escaping input", "harness: claude\ninputs: [../../etc/passwd]\noutputs: [x.md]\n", "without a path"},
		{"not yaml", "harness: [\n", "parse"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			roleDir := filepath.Join(dir, "r")
			if err := os.MkdirAll(roleDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(roleDir, ConfigName), []byte(c.yml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir, "r")
			if err == nil {
				t.Fatalf("Load accepted %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
			if !strings.Contains(err.Error(), ConfigName) {
				t.Errorf("error %q does not name the file", err)
			}
		})
	}
}

func TestLoadAllStopsOnABrokenRole(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roles")
	if err := Seed(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coder", ConfigName), []byte("cmd: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAll(dir); err == nil {
		t.Fatal("LoadAll ignored a broken role")
	}
}

func TestLoadMissingRole(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := Load(dir, ""); err == nil {
		t.Error("Load accepted an empty name")
	}
}

// The library is read in the order of the work, not of the alphabet — and a
// role nobody numbered waits at the end rather than jumping the queue.
func TestLoadAllFollowsTheOrderField(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, ConfigName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const rest = "harness: claude\noutputs: [out.md]\n"
	write("reviewer", "order: 30\n"+rest)
	write("planner", "order: 10\n"+rest)
	write("coder", "order: 20\n"+rest)
	write("zulu", rest)  // unnumbered
	write("alpha", rest) // unnumbered

	all, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var got []string
	for _, r := range all {
		got = append(got, r.Name)
	}
	want := []string{"planner", "coder", "reviewer", "alpha", "zulu"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("role order = %v, want %v", got, want)
	}
}

func TestBundledRolesLeaveRoomBetweenThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roles")
	if err := Seed(dir); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	all, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	want := map[string]int{"planner": 10, "coder": 20, "reviewer": 30}
	for _, r := range all {
		if want[r.Name] != r.Order {
			t.Errorf("%s order = %d, want %d", r.Name, r.Order, want[r.Name])
		}
	}
	if all[0].Name != "planner" || all[2].Name != "reviewer" {
		t.Errorf("bundled order = %v", all)
	}
}
