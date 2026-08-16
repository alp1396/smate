package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smate/internal/roles"
	"smate/internal/store"
)

// Opening a harness by hand needs a command, and config.yml is not obliged to
// carry one: the keys are named after the binaries.
func TestHarnessesListsWhatIsConfigured(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{Harness: map[string]store.Harness{
		"claude":   {Set: map[string]string{"CLAUDE_CONFIG_DIR": "/home/smate/.claude"}},
		"codex":    {Env: []string{"OPENAI_API_KEY"}},
		"opencode": {Env: []string{"OPENROUTER_API_KEY"}, Cmd: "opencode run --tui"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEnv(map[string]string{"OPENAI_API_KEY": "sk-test"}); err != nil {
		t.Fatal(err)
	}

	list, err := Harnesses(s)
	if err != nil {
		t.Fatalf("Harnesses: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d harnesses, want 3", len(list))
	}
	// Name order, so the menu does not reshuffle itself between reloads.
	if list[0].Name != "claude" || list[1].Name != "codex" || list[2].Name != "opencode" {
		t.Errorf("out of order: %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
	if list[0].Cmd != "claude" {
		t.Errorf("cmd without a config entry = %q, want the harness name", list[0].Cmd)
	}
	if list[2].Cmd != "opencode run --tui" {
		t.Errorf("configured cmd was ignored: %q", list[2].Cmd)
	}

	// A key that env.yml has is not missing; one it does not have is — and
	// either way the harness is still listed, because a CLI that logs in
	// interactively needs no key at all.
	if len(list[1].Missing) != 0 {
		t.Errorf("codex reported missing %v with the key set", list[1].Missing)
	}
	if len(list[2].Missing) != 1 || list[2].Missing[0] != "OPENROUTER_API_KEY" {
		t.Errorf("opencode missing = %v, want [OPENROUTER_API_KEY]", list[2].Missing)
	}
	if len(list[0].Missing) != 0 {
		t.Errorf("a harness that asks for no keys reported %v", list[0].Missing)
	}
}

func TestHarnessCmdRejectsAnUnknownName(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{Harness: map[string]store.Harness{"claude": {}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := HarnessCmd(s, "nope", "claude"); err == nil {
		t.Error("HarnessCmd accepted a task that does not exist")
	}
}

func TestHarnessInjection(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{Harness: map[string]store.Harness{
		"claude": {
			State: "claude",
			Mount: "/home/smate/.claude",
			Set:   map[string]string{"CLAUDE_CONFIG_DIR": "/home/smate/.claude"},
		},
		"codex":    {Env: []string{"OPENAI_API_KEY"}},
		"opencode": {Env: []string{"OPENROUTER_API_KEY"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEnv(map[string]string{"OPENAI_API_KEY": "sk-test"}); err != nil {
		t.Fatal(err)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		t.Fatalf("harnessInjection: %v", err)
	}

	if inj.Env["OPENAI_API_KEY"] != "sk-test" {
		t.Errorf("configured key was not injected: %v", inj.Env)
	}
	if inj.Env["CLAUDE_CONFIG_DIR"] != "/home/smate/.claude" {
		t.Errorf("literal value was not injected: %v", inj.Env)
	}
	if _, ok := inj.Env["OPENROUTER_API_KEY"]; ok {
		t.Error("a key that is not set must not be injected")
	}
	if len(inj.Warnings) != 1 {
		t.Errorf("expected one warning about the missing key, got %v", inj.Warnings)
	}

	if len(inj.Mounts) != 1 || inj.Mounts[0].Container != "/home/smate/.claude" {
		t.Fatalf("state mount: %v", inj.Mounts)
	}
	if inj.Mounts[0].Host != s.HarnessDir("claude") {
		t.Errorf("mount host: got %s, want %s", inj.Mounts[0].Host, s.HarnessDir("claude"))
	}
	// The state directory has to exist before docker mounts it.
	if _, err := os.Stat(inj.Mounts[0].Host); err != nil {
		t.Errorf("state directory was not created: %v", err)
	}
	// And it has to hold the settings that let a run proceed without being asked.
	if _, err := os.Stat(filepath.Join(inj.Mounts[0].Host, "settings.json")); err != nil {
		t.Errorf("harness defaults were not seeded: %v", err)
	}
}

func TestHarnessInjectionStateWithoutMount(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{Harness: map[string]store.Harness{
		"claude": {State: "claude"},
	}}); err != nil {
		t.Fatal(err)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		t.Fatalf("harnessInjection: %v", err)
	}
	if len(inj.Mounts) != 0 {
		t.Errorf("nothing can be mounted without a target: %v", inj.Mounts)
	}
	if len(inj.Warnings) != 1 {
		t.Errorf("expected a warning, got %v", inj.Warnings)
	}
}

func TestCacheInjectionDefaultsHostUnderStoreRoot(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{Cache: map[string]store.Cache{
		"go-mod": {Mount: "/home/smate/go/pkg/mod"},
	}}); err != nil {
		t.Fatal(err)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		t.Fatalf("harnessInjection: %v", err)
	}
	if len(inj.Mounts) != 1 || inj.Mounts[0].Container != "/home/smate/go/pkg/mod" {
		t.Fatalf("cache mount: %v", inj.Mounts)
	}
	if inj.Mounts[0].Host != s.CacheDir("go-mod") {
		t.Errorf("mount host: got %s, want %s", inj.Mounts[0].Host, s.CacheDir("go-mod"))
	}
	if _, err := os.Stat(inj.Mounts[0].Host); err != nil {
		t.Errorf("cache directory was not created: %v", err)
	}
}

func TestCacheInjectionExplicitHost(t *testing.T) {
	s := store.At(t.TempDir())
	host := filepath.Join(t.TempDir(), "real-go-cache")
	if err := s.SaveGlobal(store.Global{Cache: map[string]store.Cache{
		"go-mod": {Host: host, Mount: "/home/smate/go/pkg/mod"},
	}}); err != nil {
		t.Fatal(err)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		t.Fatalf("harnessInjection: %v", err)
	}
	if len(inj.Mounts) != 1 || inj.Mounts[0].Host != host {
		t.Fatalf("cache mount: %v, want host %s", inj.Mounts, host)
	}
	if _, err := os.Stat(host); err != nil {
		t.Errorf("explicit host directory was not created: %v", err)
	}
}

func TestCacheInjectionRejects(t *testing.T) {
	cases := []struct {
		name  string
		cache store.Cache
		want  string
	}{
		{"no-mount", store.Cache{}, "nothing to mount"},
		{"relative-mount", store.Cache{Mount: "relative/path"}, "must be absolute"},
		{"under-workspace", store.Cache{Mount: "/workspace/cache"}, "under /workspace"},
		{"is-workspace", store.Cache{Mount: "/workspace"}, "under /workspace"},
		{"relative-host", store.Cache{Host: "relative", Mount: "/home/smate/cache"}, "host must be absolute"},
	}
	for _, c := range cases {
		s := store.At(t.TempDir())
		if err := s.SaveGlobal(store.Global{Cache: map[string]store.Cache{c.name: c.cache}}); err != nil {
			t.Fatal(err)
		}
		inj, err := harnessInjection(s)
		if err != nil {
			t.Fatalf("%s: harnessInjection: %v", c.name, err)
		}
		if len(inj.Mounts) != 0 {
			t.Errorf("%s: expected no mount, got %v", c.name, inj.Mounts)
		}
		if len(inj.Warnings) != 1 || !strings.Contains(inj.Warnings[0], c.want) {
			t.Errorf("%s: warnings = %v, want one containing %q", c.name, inj.Warnings, c.want)
		}
	}
}

func TestHarnessInjectionEmptyConfig(t *testing.T) {
	s := store.At(t.TempDir())
	if err := s.SaveGlobal(store.Global{}); err != nil {
		t.Fatal(err)
	}

	inj, err := harnessInjection(s)
	if err != nil {
		t.Fatalf("harnessInjection: %v", err)
	}
	if len(inj.Env) != 0 || len(inj.Mounts) != 0 || len(inj.Warnings) != 0 {
		t.Errorf("an empty config must inject nothing: %+v", inj)
	}
}

// The role names values, the harness spells them: the same role.yml under a
// different harness has to come out as that CLI's own flags.
func TestRoleCmdLineRendersTheHarnessFlags(t *testing.T) {
	claude := store.Harness{ModelFlag: "--model {}"}
	codex := store.Harness{Cmd: "codex exec", ModelFlag: "-m {}", EffortFlag: "-c model_reasoning_effort={}"}

	cases := []struct {
		name     string
		harness  string
		h        store.Harness
		role     roles.Role
		want     string
		warnings int
	}{
		{
			name: "model only", harness: "claude", h: claude,
			role: roles.Role{Name: "coder", Model: "claude-opus-5"},
			want: "claude --model 'claude-opus-5'",
		},
		{
			name: "model and effort", harness: "codex", h: codex,
			role: roles.Role{Name: "coder", Model: "gpt-5-codex", Effort: "high"},
			want: "codex exec -m 'gpt-5-codex' -c model_reasoning_effort='high'",
		},
		{
			name: "nothing named", harness: "codex", h: codex,
			role: roles.Role{Name: "coder"},
			want: "codex exec",
		},
		{
			// claude has no effort flag: the run still happens, on the default.
			name: "effort the harness cannot express", harness: "claude", h: claude,
			role: roles.Role{Name: "coder", Model: "claude-opus-5", Effort: "high"},
			want: "claude --model 'claude-opus-5'", warnings: 1,
		},
		{
			name: "flag template without a placeholder", harness: "claude",
			h:    store.Harness{ModelFlag: "--model"},
			role: roles.Role{Name: "coder", Model: "claude-opus-5"},
			want: "claude", warnings: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, warnings := roleCmdLine(c.harness, c.h, c.role)
			if got != c.want {
				t.Errorf("cmd = %q, want %q", got, c.want)
			}
			if len(warnings) != c.warnings {
				t.Errorf("warnings = %v, want %d of them", warnings, c.warnings)
			}
		})
	}
}
